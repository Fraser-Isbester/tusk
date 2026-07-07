package main

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/rules"
)

// fakeSource is a test snapshotter returning canned data (and optional errors).
type fakeSource struct {
	queries  []db.Query
	txns     []db.Transaction
	locks    []db.Lock
	queryErr error
	txnErr   error
	lockErr  error
	calls    int
}

func (f *fakeSource) GetActiveQueries(_ context.Context) ([]db.Query, error) {
	f.calls++
	return f.queries, f.queryErr
}
func (f *fakeSource) GetTransactions(_ context.Context) ([]db.Transaction, error) {
	return f.txns, f.txnErr
}
func (f *fakeSource) GetLocks(_ context.Context) ([]db.Lock, error) {
	return f.locks, f.lockErr
}

func mustRule(name, resource, expr, action string) rules.Rule {
	built, err := rules.BuildRules([]rules.RuleConfig{
		{Name: name, Resource: resource, When: expr, Action: action, DryRun: true},
	}, false)
	Expect(err).NotTo(HaveOccurred())
	return built[0]
}

var _ = Describe("evaluate", func() {
	var engine *rules.Engine

	BeforeEach(func() {
		// nil db is safe: every rule is dry-run, so no action touches the DB.
		engine = rules.NewEngine(nil, nil, 5*time.Minute, 100)
		engine.UpdateRules([]rules.Rule{
			mustRule("slow-query", "query", "duration > duration('1s')", "cancel"),
			mustRule("long-tx", "transaction", "xact_duration > duration('30s')", "log"),
		})
	})

	It("records violations from the fetched snapshot", func() {
		src := &fakeSource{
			queries: []db.Query{
				{ResourceBase: db.ResourceBase{PID: 42, State: "active"}, Duration: 5 * time.Second},
			},
		}
		evaluate(context.Background(), engine, src)

		violations := engine.RecentViolations()
		Expect(violations).To(HaveLen(1))
		Expect(violations[0].RuleName).To(Equal("slow-query"))
		Expect(violations[0].PID).To(Equal(42))
	})

	It("continues evaluating healthy resources when one fetch errors", func() {
		src := &fakeSource{
			queryErr: errors.New("connection reset"),
			txns: []db.Transaction{
				{ResourceBase: db.ResourceBase{PID: 10, State: "idle in transaction"}, XactDuration: 60 * time.Second},
			},
		}
		// Must not panic despite the query fetch error; the transaction still matches.
		Expect(func() { evaluate(context.Background(), engine, src) }).NotTo(Panic())

		violations := engine.RecentViolations()
		Expect(violations).To(HaveLen(1))
		Expect(violations[0].RuleName).To(Equal("long-tx"))
		Expect(violations[0].PID).To(Equal(10))
	})
})

var _ = Describe("runDaemon", func() {
	It("evaluates once immediately and returns when the context is canceled", func() {
		engine := rules.NewEngine(nil, nil, 5*time.Minute, 100)
		engine.UpdateRules([]rules.Rule{mustRule("r", "query", "true", "log")})
		src := &fakeSource{
			queries: []db.Query{{ResourceBase: db.ResourceBase{PID: 1, State: "active"}}},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already canceled: the loop should do its initial evaluate, then exit.

		done := make(chan error, 1)
		go func() { done <- runDaemon(ctx, engine, src, time.Hour) }()

		Eventually(done, 2*time.Second).Should(Receive(BeNil()))
		Expect(src.calls).To(BeNumerically(">=", 1)) // initial evaluate ran
		Expect(engine.ViolatedPIDs()).To(HaveKey(1))
	})
})
