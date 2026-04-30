package rules_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/rules"
)

var _ = Describe("Engine", func() {
	var engine *rules.Engine

	buildRule := func(name, resource, expr, action string) rules.Rule {
		configs := []rules.RuleConfig{
			{Name: name, Resource: resource, When: expr, Action: action, DryRun: true},
		}
		built, err := rules.BuildRules(configs, false)
		Expect(err).NotTo(HaveOccurred())
		return built[0]
	}

	BeforeEach(func() {
		// Engine with nil db is fine — all rules are dry-run so no DB calls happen
		engine = rules.NewEngine(nil, nil, 5*time.Minute, 100)
	})

	Describe("Evaluate", func() {
		It("detects a query that matches a rule", func() {
			r := buildRule("slow-query", "query", "duration > duration('1s')", "cancel")
			engine.UpdateRules([]rules.Rule{r})

			snap := rules.Snapshot{
				Queries: []db.Query{
					{
						ResourceBase: db.ResourceBase{PID: 42, State: "active"},
						Duration:     5 * time.Second,
						QueryText:    "SELECT pg_sleep(5)",
					},
				},
			}
			engine.Evaluate(context.Background(), snap)

			violations := engine.RecentViolations()
			Expect(violations).To(HaveLen(1))
			Expect(violations[0].RuleName).To(Equal("slow-query"))
			Expect(violations[0].PID).To(Equal(42))
		})

		It("does not fire for queries that don't match", func() {
			r := buildRule("slow-query", "query", "duration > duration('10s')", "cancel")
			engine.UpdateRules([]rules.Rule{r})

			snap := rules.Snapshot{
				Queries: []db.Query{
					{
						ResourceBase: db.ResourceBase{PID: 42, State: "active"},
						Duration:     1 * time.Second,
					},
				},
			}
			engine.Evaluate(context.Background(), snap)
			Expect(engine.RecentViolations()).To(BeEmpty())
		})

		It("skips disabled rules", func() {
			r := buildRule("slow-query", "query", "true", "log")
			r.Enabled = false
			engine.UpdateRules([]rules.Rule{r})

			snap := rules.Snapshot{
				Queries: []db.Query{
					{ResourceBase: db.ResourceBase{PID: 1, State: "active"}},
				},
			}
			engine.Evaluate(context.Background(), snap)
			Expect(engine.RecentViolations()).To(BeEmpty())
		})

		It("detects a transaction that matches a rule", func() {
			r := buildRule("long-tx", "transaction", "xact_duration > duration('30s')", "log")
			engine.UpdateRules([]rules.Rule{r})

			snap := rules.Snapshot{
				Transactions: []db.Transaction{
					{
						ResourceBase: db.ResourceBase{PID: 10, State: "idle in transaction"},
						XactDuration: 60 * time.Second,
					},
				},
			}
			engine.Evaluate(context.Background(), snap)

			violations := engine.RecentViolations()
			Expect(violations).To(HaveLen(1))
			Expect(violations[0].RuleName).To(Equal("long-tx"))
			Expect(violations[0].PID).To(Equal(10))
		})

		It("detects a lock that matches a rule", func() {
			r := buildRule("long-lock", "lock", "wait_duration > duration('5s')", "terminate")
			engine.UpdateRules([]rules.Rule{r})

			snap := rules.Snapshot{
				Locks: []db.Lock{
					{
						BlockedPID:   20,
						BlockingPID:  21,
						WaitDuration: 15 * time.Second,
						LockType:     "relation",
						Mode:         "AccessExclusiveLock",
					},
				},
			}
			engine.Evaluate(context.Background(), snap)

			violations := engine.RecentViolations()
			Expect(violations).To(HaveLen(1))
			Expect(violations[0].PID).To(Equal(20))
		})

		It("marks violations as closed when PID disappears from snapshot", func() {
			r := buildRule("rule", "query", "true", "log")
			engine.UpdateRules([]rules.Rule{r})

			// First tick — PID 42 present
			snap1 := rules.Snapshot{
				Queries: []db.Query{
					{ResourceBase: db.ResourceBase{PID: 42, State: "active"}},
				},
			}
			engine.Evaluate(context.Background(), snap1)
			Expect(engine.ViolatedPIDs()).To(HaveKey(42))

			// Second tick — PID 42 gone
			snap2 := rules.Snapshot{}
			engine.Evaluate(context.Background(), snap2)
			Expect(engine.ViolatedPIDs()).To(BeEmpty())
		})
	})

	Describe("Rules management", func() {
		It("returns a copy of rules via Rules()", func() {
			r := buildRule("r1", "query", "true", "log")
			engine.UpdateRules([]rules.Rule{r})

			result := engine.Rules()
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("r1"))
		})

		It("counts only enabled rules via RuleCount()", func() {
			r1 := buildRule("r1", "query", "true", "log")
			r2 := buildRule("r2", "query", "true", "log")
			r2.Enabled = false
			engine.UpdateRules([]rules.Rule{r1, r2})

			Expect(engine.RuleCount()).To(Equal(1))
		})

		It("hot-swaps rules via UpdateRules()", func() {
			r1 := buildRule("old", "query", "true", "log")
			engine.UpdateRules([]rules.Rule{r1})
			Expect(engine.Rules()[0].Name).To(Equal("old"))

			r2 := buildRule("new", "query", "true", "log")
			engine.UpdateRules([]rules.Rule{r2})
			Expect(engine.Rules()[0].Name).To(Equal("new"))
		})
	})
})
