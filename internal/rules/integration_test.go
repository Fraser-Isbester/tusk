package rules_test

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/pgtest"
	"github.com/fraser-isbester/tusk/internal/rules"
)

// End-to-end remediation: live snapshot -> CEL match -> action against the
// real backend. Every rule is scoped to a unique application_name so a shared
// test server's other sessions can never be canceled or terminated.
var _ = Describe("Engine against PostgreSQL", Label("integration"), Ordered, ContinueOnFailure, func() {
	var (
		ctx      context.Context
		tmp      *pgtest.TempDB
		database *db.DB
	)

	BeforeAll(func() {
		ctx = context.Background()
		dsn, required := pgtest.Resolve()
		if dsn == "" {
			if required {
				Fail(pgtest.EnvDSN + " must be set when " + pgtest.EnvRequire + " is set")
			}
			Skip(pgtest.EnvDSN + " not set")
		}
		var err error
		tmp, err = pgtest.CreateDatabase(ctx, dsn)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(tmp.Drop(context.Background())).To(Succeed()) })
		database, err = db.New(tmp.DSN)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(database.Close)
	})

	client := func(app string) (*pgx.Conn, int) {
		conn, err := pgtest.Client(ctx, tmp.DSN, app)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = conn.Close(context.Background()) })
		return conn, int(conn.PgConn().PID())
	}

	snapshot := func() rules.Snapshot {
		GinkgoHelper()
		qs, err := database.GetActiveQueries(ctx)
		Expect(err).NotTo(HaveOccurred())
		txns, err := database.GetTransactions(ctx)
		Expect(err).NotTo(HaveOccurred())
		locks, err := database.GetLocks(ctx)
		Expect(err).NotTo(HaveOccurred())
		return rules.Snapshot{Queries: qs, Transactions: txns, Locks: locks}
	}

	engineFor := func(cfg rules.RuleConfig) *rules.Engine {
		built, err := rules.BuildRules([]rules.RuleConfig{cfg}, false)
		Expect(err).NotTo(HaveOccurred())
		return rules.NewEngine(built, database, 5*time.Minute, 100)
	}

	// sleeping starts a long pg_sleep on a fresh session and waits until tusk sees it.
	sleeping := func(app string) (int, <-chan error) {
		c, pid := client(app)
		done := pgtest.Background(ctx, c, "SELECT pg_sleep(30)")
		DeferCleanup(func() { _ = database.CancelQuery(context.Background(), pid); <-done })
		Eventually(func() ([]db.Query, error) { return database.GetActiveQueries(ctx) }).
			WithTimeout(5 * time.Second).
			Should(ContainElement(And(HaveField("PID", pid), HaveField("WaitEvent", "PgSleep"))))
		return pid, done
	}

	sqlState := func(err error) string {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return pgErr.Code
		}
		return ""
	}

	It("cancels a matching long-running query", func() {
		app := pgtest.UniqueName("rule_cancel")
		pid, done := sleeping(app)
		engine := engineFor(rules.RuleConfig{
			Name: "long", Resource: "query", Action: "cancel",
			When: "app == '" + app + "' && state == 'active' && duration > duration('1ms')",
		})

		engine.Evaluate(ctx, snapshot())

		var err error
		Eventually(done).WithTimeout(5 * time.Second).Should(Receive(&err))
		Expect(sqlState(err)).To(Equal("57014"))
		v := engine.ViolatedPIDs()[pid]
		Expect(eventKinds(v)).To(Equal([]rules.EventKind{rules.EventDetected, rules.EventAction, rules.EventSent}))
	})

	It("terminates an idle-in-transaction session", func() {
		app := pgtest.UniqueName("rule_term")
		c, pid := client(app)
		_, err := c.Exec(ctx, "BEGIN; SELECT 1")
		Expect(err).NotTo(HaveOccurred())

		engine := engineFor(rules.RuleConfig{
			Name: "idle-tx", Resource: "transaction", Action: "terminate",
			When: "app == '" + app + "' && state == 'idle in transaction'",
		})
		Eventually(func() map[int]rules.Violation {
			engine.Evaluate(ctx, snapshot())
			return engine.ViolatedPIDs()
		}).WithTimeout(5 * time.Second).Should(HaveKey(pid))

		Eventually(func() ([]db.Transaction, error) { return database.GetTransactions(ctx) }).
			WithTimeout(5 * time.Second).Should(Not(ContainElement(HaveField("PID", pid))))
		_, err = c.Exec(ctx, "SELECT 1")
		Expect(err).To(HaveOccurred())

		// Next tick the backend is gone, so the violation closes.
		engine.Evaluate(ctx, snapshot())
		Expect(engine.ViolatedPIDs()).NotTo(HaveKey(pid))
		Expect(engine.RecentViolations()[0].LastEvent().Kind).To(Equal(rules.EventClosed))
	})

	It("leaves a matching query running under dry-run, until manually actioned", func() {
		app := pgtest.UniqueName("rule_dry")
		pid, done := sleeping(app)
		engine := engineFor(rules.RuleConfig{
			Name: "copilot", Resource: "query", Action: "cancel", DryRun: true,
			When: "app == '" + app + "' && state == 'active'",
		})

		engine.Evaluate(ctx, snapshot())
		Expect(engine.ViolatedPIDs()).To(HaveKey(pid))
		Consistently(done).WithTimeout(500 * time.Millisecond).ShouldNot(Receive())

		Expect(engine.ManualAction("copilot", pid)).To(Equal("executed cancel on PID " + strconv.Itoa(pid)))
		var err error
		Eventually(done).WithTimeout(5 * time.Second).Should(Receive(&err))
		Expect(sqlState(err)).To(Equal("57014"))
	})

	It("records a failed action in the violation log", func() {
		engine := engineFor(rules.RuleConfig{Name: "ghost", Resource: "query", When: "true", Action: "terminate"})
		// A PID that is not a backend: pg_terminate_backend returns false.
		engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{activeQuery(2147483000)}})

		v := engine.RecentViolations()
		Expect(v).To(HaveLen(1))
		Expect(v[0].LastEvent().Kind).To(Equal(rules.EventError))
		Expect(v[0].LastEvent().Message).To(ContainSubstring("returned false"))
	})
})
