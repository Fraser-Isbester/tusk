package rules_test

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/rules"
)

func eventKinds(v rules.Violation) []rules.EventKind {
	kinds := make([]rules.EventKind, 0, len(v.Events))
	for _, e := range v.Events {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}

func compile(cfg rules.RuleConfig, readonly bool) rules.Rule {
	GinkgoHelper()
	built, err := rules.BuildRules([]rules.RuleConfig{cfg}, readonly)
	Expect(err).NotTo(HaveOccurred())
	return built[0]
}

func activeQuery(pid int) db.Query {
	return db.Query{ResourceBase: db.ResourceBase{PID: pid, State: "active"}, Duration: time.Minute}
}

// Lifecycle semantics of the engine's audit log. The engine is built with a nil
// *db.DB throughout: only the log action (which ignores the DB) or dry-run rules
// are used, so any accidental real execution would panic the spec.
var _ = Describe("Engine lifecycle", func() {
	var (
		engine *rules.Engine
		ctx    = context.Background()
	)

	BeforeEach(func() {
		engine = rules.NewEngine(nil, nil, 5*time.Minute, 100)
	})

	single := func() rules.Violation {
		GinkgoHelper()
		vs := engine.RecentViolations()
		Expect(vs).To(HaveLen(1))
		return vs[0]
	}

	It("acts on every evaluation when no cooldown is configured", func() {
		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "query", When: "true", Action: "log",
		}, false)})
		snap := rules.Snapshot{Queries: []db.Query{activeQuery(1)}}

		for range 3 {
			engine.Evaluate(ctx, snap)
		}
		Expect(eventKinds(single())).To(Equal([]rules.EventKind{
			rules.EventDetected,
			rules.EventAction, rules.EventSent,
			rules.EventAction, rules.EventSent,
			rules.EventAction, rules.EventSent,
		}))
	})

	It("acts once per cooldown window and records the cooldown", func() {
		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "query", When: "true", Action: "log", Cooldown: "1h",
		}, false)})
		snap := rules.Snapshot{Queries: []db.Query{activeQuery(1)}}

		for range 3 {
			engine.Evaluate(ctx, snap)
		}
		v := single()
		Expect(eventKinds(v)).To(Equal([]rules.EventKind{
			rules.EventDetected, rules.EventAction, rules.EventCooldown, rules.EventSent,
		}))
		Expect(v.Events[3].Message).To(Equal("executed log on PID 1"))
	})

	It("never executes a dry-run rule, and says so in the log", func() {
		// terminate + nil DB: executing would panic.
		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "query", When: "true", Action: "terminate", DryRun: true,
		}, false)})
		engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{activeQuery(1)}})

		v := single()
		Expect(v.DryRun).To(BeTrue())
		Expect(eventKinds(v)).To(Equal([]rules.EventKind{rules.EventDetected, rules.EventAction}))
		Expect(v.Events[1].Message).To(ContainSubstring("dry-run=true"))
	})

	It("never executes when the profile is readonly, even if the rule is not dry-run", func() {
		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "transaction", When: "true", Action: "terminate",
		}, true)})
		Expect(func() {
			engine.Evaluate(ctx, rules.Snapshot{Transactions: []db.Transaction{{ResourceBase: db.ResourceBase{PID: 1}}}})
		}).NotTo(Panic())
		Expect(single().DryRun).To(BeTrue())
	})

	It("treats an expression that errors at runtime as a non-match", func() {
		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "query", When: "int(app) > 0", Action: "log",
		}, false)})
		q := activeQuery(1)
		q.App = "not-a-number"
		Expect(func() { engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{q}}) }).NotTo(Panic())
		Expect(engine.RecentViolations()).To(BeEmpty())
	})

	It("matches on sqlcommenter tags and blocking info", func() {
		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "query", Action: "log", DryRun: true,
			When: "route.startsWith('/admin') && framework == 'rails' && blocked_by == 99",
		}, false)})
		hit := activeQuery(1)
		hit.Comment = db.SQLComment{Route: "/admin/users", Framework: "rails"}
		hit.BlockedBy = 99
		miss := activeQuery(2)
		miss.Comment = db.SQLComment{Route: "/public", Framework: "rails"}
		miss.BlockedBy = 99

		engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{hit, miss}})
		Expect(single().PID).To(Equal(1))
	})

	It("refreshes the resource snapshot on each re-match", func() {
		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "query", When: "true", Action: "log", DryRun: true,
		}, false)})
		q := activeQuery(1)
		engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{q}})
		q.Duration = 2 * time.Hour
		engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{q}})

		v := single()
		Expect(v.QuerySnap).NotTo(BeNil())
		Expect(v.QuerySnap.Duration).To(Equal(2 * time.Hour))
	})

	It("keeps a violation open while its PID appears under any resource", func() {
		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "query", When: "true", Action: "log", DryRun: true,
		}, false)})
		engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{activeQuery(5)}})
		// Next tick the backend is no longer a "query" but is still blocking a lock.
		engine.Evaluate(ctx, rules.Snapshot{Locks: []db.Lock{{BlockedPID: 6, BlockingPID: 5}}})
		Expect(engine.ViolatedPIDs()).To(HaveKey(5))

		engine.Evaluate(ctx, rules.Snapshot{})
		v := single()
		Expect(v.Active).To(BeFalse())
		Expect(v.LastEvent().Kind).To(Equal(rules.EventClosed))
	})

	It("opens a new violation when a PID re-matches after its previous one closed", func() {
		// MarkActive used to recompute Active for every stored violation, closed
		// ones included, so a closed violation was silently resurrected (no
		// event) whenever its PID showed up again — including a recycled PID
		// belonging to an unrelated backend — and got a second "closed" event
		// later.
		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "query", When: "true", Action: "log", DryRun: true,
		}, false)})
		engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{activeQuery(5)}})
		engine.Evaluate(ctx, rules.Snapshot{})
		engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{activeQuery(5)}})

		vs := engine.RecentViolations()
		Expect(vs).To(HaveLen(2))
		Expect(vs[0].Active).To(BeTrue()) // newest first
		Expect(vs[1].Active).To(BeFalse())
	})

	Describe("ManualAction", func() {
		BeforeEach(func() {
			engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
				Name: "copilot", Resource: "query", When: "true", Action: "log", DryRun: true,
			}, false)})
		})

		It("reports an unknown rule", func() {
			Expect(engine.ManualAction("nope", 1)).To(Equal(`rule "nope" not found`))
		})

		It("reports a PID with no violation for that rule", func() {
			Expect(engine.ManualAction("copilot", 1)).To(Equal("violation not found"))
		})

		It("executes a dry-run rule's action on demand and appends to its log", func() {
			engine.Evaluate(ctx, rules.Snapshot{Queries: []db.Query{activeQuery(3)}})
			Expect(engine.ManualAction("copilot", 3)).To(Equal("executed log on PID 3"))

			v := single()
			Expect(eventKinds(v)).To(Equal([]rules.EventKind{
				rules.EventDetected, rules.EventAction, rules.EventAction, rules.EventSent,
			}))
			Expect(v.Events[2].Message).To(ContainSubstring("manual trigger"))
		})
	})

	It("is safe to read violations while evaluating (run with -race)", func() {
		// The engine appends Events while the TUI reads RecentViolations from
		// another goroutine; every append has to hold the store's mutex.

		engine.UpdateRules([]rules.Rule{compile(rules.RuleConfig{
			Name: "r", Resource: "query", When: "true", Action: "log",
		}, false)})
		snap := rules.Snapshot{Queries: []db.Query{activeQuery(1)}}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 200 {
				engine.Evaluate(ctx, snap)
			}
		}()
		go func() {
			defer wg.Done()
			for range 200 {
				for _, v := range engine.RecentViolations() {
					_ = len(v.Events)
				}
			}
		}()
		wg.Wait()
	})
})
