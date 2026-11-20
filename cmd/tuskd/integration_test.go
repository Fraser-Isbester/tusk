package main

import (
	"context"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/pgtest"
	"github.com/fraser-isbester/tusk/internal/rules"
)

// countingSource is safe for concurrent use, unlike fakeSource.
type countingSource struct{ calls atomic.Int32 }

func (c *countingSource) GetActiveQueries(context.Context) ([]db.Query, error) {
	c.calls.Add(1)
	return nil, nil
}
func (c *countingSource) GetTransactions(context.Context) ([]db.Transaction, error) { return nil, nil }
func (c *countingSource) GetLocks(context.Context) ([]db.Lock, error)               { return nil, nil }

var _ = Describe("runDaemon polling", func() {
	It("re-evaluates on every tick until canceled", func() {
		src := &countingSource{}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- runDaemon(ctx, rules.NewEngine(nil, nil, time.Minute, 10), src, 10*time.Millisecond) }()

		Eventually(src.calls.Load).WithTimeout(2 * time.Second).Should(BeNumerically(">=", 3))
		cancel()
		Eventually(done).WithTimeout(2 * time.Second).Should(Receive(BeNil()))
	})
})

var _ = Describe("tuskd against PostgreSQL", Label("integration"), func() {
	It("enforces a terminate rule on a live idle-in-transaction session, then shuts down cleanly", func() {
		dsn, required := pgtest.Resolve()
		if dsn == "" {
			if required {
				Fail(pgtest.EnvDSN + " must be set when " + pgtest.EnvRequire + " is set")
			}
			Skip(pgtest.EnvDSN + " not set")
		}
		ctx := context.Background()
		tmp, err := pgtest.CreateDatabase(ctx, dsn)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(tmp.Drop(context.Background())).To(Succeed()) })

		database, err := db.New(tmp.DSN)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(database.Close)

		app := pgtest.UniqueName("tuskd_victim")
		victim, err := pgtest.Client(ctx, tmp.DSN, app)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = victim.Close(context.Background()) })
		_, err = victim.Exec(ctx, "BEGIN; SELECT 1")
		Expect(err).NotTo(HaveOccurred())
		pid := int(victim.PgConn().PID())

		// Same path as main(): YAML-shaped config -> BuildRules -> Engine.
		compiled, err := rules.BuildRules([]rules.RuleConfig{{
			Name: "kill-idle-in-txn", Resource: "transaction", Action: "terminate",
			When: "app == '" + app + "' && state == 'idle in transaction'",
		}}, false)
		Expect(err).NotTo(HaveOccurred())
		engine := rules.NewEngine(compiled, database, 5*time.Minute, 1000)

		dctx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- runDaemon(dctx, engine, database, 50*time.Millisecond) }()
		DeferCleanup(cancel)

		Eventually(func() ([]db.Transaction, error) { return database.GetTransactions(ctx) }).
			WithTimeout(10 * time.Second).Should(Not(ContainElement(HaveField("PID", pid))))
		_, err = victim.Exec(ctx, "SELECT 1")
		Expect(err).To(HaveOccurred())

		cancel()
		Eventually(done).WithTimeout(5 * time.Second).Should(Receive(BeNil()))

		vs := engine.RecentViolations()
		Expect(vs).NotTo(BeEmpty())
		Expect(vs[0].PID).To(Equal(pid))
		Expect(vs[0].Events).To(ContainElement(HaveField("Kind", rules.EventSent)))
	})
})
