package rules_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/rules"
)

var _ = Describe("ViolationStore", func() {
	var store *rules.ViolationStore

	BeforeEach(func() {
		store = rules.NewViolationStore(5*time.Minute, 100)
	})

	makeViolation := func(ruleName string, pid int) rules.Violation {
		return rules.Violation{
			RuleName:     ruleName,
			ResourceType: rules.ResourceQuery,
			PID:          pid,
			Expression:   "duration > duration('5s')",
			ActionName:   "cancel",
			Active:       true,
			Events: []rules.ViolationEvent{
				{Time: time.Now(), Kind: rules.EventDetected, Message: "detected"},
			},
		}
	}

	Describe("RecordOrUpdate", func() {
		It("records a new violation and allows action", func() {
			v := makeViolation("slow-query", 100)
			result, shouldAct := store.RecordOrUpdate(v, 0)
			Expect(shouldAct).To(BeTrue())
			Expect(result.RuleName).To(Equal("slow-query"))
			Expect(result.PID).To(Equal(100))
		})

		It("returns an existing active violation on subsequent calls", func() {
			v := makeViolation("slow-query", 100)
			first, _ := store.RecordOrUpdate(v, 0)
			second, shouldAct := store.RecordOrUpdate(v, 0)
			Expect(shouldAct).To(BeTrue())
			Expect(second).To(BeIdenticalTo(first))
		})

		It("respects cooldown and suppresses duplicate actions", func() {
			v := makeViolation("slow-query", 100)
			_, shouldAct := store.RecordOrUpdate(v, 10*time.Minute)
			Expect(shouldAct).To(BeTrue())

			_, shouldAct = store.RecordOrUpdate(v, 10*time.Minute)
			Expect(shouldAct).To(BeFalse())
		})

		It("allows action after cooldown expires", func() {
			shortStore := rules.NewViolationStore(5*time.Minute, 100)
			v := makeViolation("slow-query", 100)
			// Use zero cooldown so there's no suppression
			_, shouldAct := shortStore.RecordOrUpdate(v, 0)
			Expect(shouldAct).To(BeTrue())
			_, shouldAct = shortStore.RecordOrUpdate(v, 0)
			Expect(shouldAct).To(BeTrue())
		})

		It("tracks different rule+PID combinations independently", func() {
			v1 := makeViolation("rule-a", 100)
			v2 := makeViolation("rule-b", 100)
			v3 := makeViolation("rule-a", 200)

			r1, act1 := store.RecordOrUpdate(v1, 10*time.Minute)
			r2, act2 := store.RecordOrUpdate(v2, 10*time.Minute)
			r3, act3 := store.RecordOrUpdate(v3, 10*time.Minute)

			Expect(act1).To(BeTrue())
			Expect(act2).To(BeTrue())
			Expect(act3).To(BeTrue())
			Expect(r1.RuleName).To(Equal("rule-a"))
			Expect(r2.RuleName).To(Equal("rule-b"))
			Expect(r3.PID).To(Equal(200))
		})

		It("enforces maxSize by evicting oldest violations", func() {
			smallStore := rules.NewViolationStore(5*time.Minute, 3)
			for i := 1; i <= 5; i++ {
				v := makeViolation("rule", i)
				smallStore.RecordOrUpdate(v, 0)
			}
			recent := smallStore.Recent()
			Expect(recent).To(HaveLen(3))
		})
	})

	Describe("MarkActive", func() {
		It("marks violations as inactive when PID disappears", func() {
			v := makeViolation("rule", 100)
			store.RecordOrUpdate(v, 0)

			store.MarkActive(map[int]bool{200: true}) // PID 100 not present
			pids := store.ViolatedPIDs()
			Expect(pids).To(BeEmpty())
		})

		It("keeps violations active when PID is present", func() {
			v := makeViolation("rule", 100)
			store.RecordOrUpdate(v, 0)

			store.MarkActive(map[int]bool{100: true})
			pids := store.ViolatedPIDs()
			Expect(pids).To(HaveKey(100))
		})

		It("appends a closed event when a violation becomes inactive", func() {
			v := makeViolation("rule", 100)
			store.RecordOrUpdate(v, 0)

			store.MarkActive(map[int]bool{}) // close it
			recent := store.Recent()
			Expect(recent).To(HaveLen(1))
			lastEvt := recent[0].LastEvent()
			Expect(lastEvt.Kind).To(Equal(rules.EventClosed))
			Expect(lastEvt.Message).To(Equal("target PID gone"))
		})
	})

	Describe("Recent", func() {
		It("returns violations in newest-first order", func() {
			for i := 1; i <= 3; i++ {
				v := makeViolation("rule", i)
				store.RecordOrUpdate(v, 0)
			}
			recent := store.Recent()
			Expect(recent).To(HaveLen(3))
			Expect(recent[0].PID).To(Equal(3))
			Expect(recent[2].PID).To(Equal(1))
		})

		It("returns empty slice when no violations exist", func() {
			recent := store.Recent()
			Expect(recent).To(BeEmpty())
		})
	})

	Describe("ViolatedPIDs", func() {
		It("returns only active violations", func() {
			v1 := makeViolation("rule", 100)
			v2 := makeViolation("rule", 200)
			store.RecordOrUpdate(v1, 0)
			store.RecordOrUpdate(v2, 0)

			store.MarkActive(map[int]bool{100: true}) // 200 goes inactive
			pids := store.ViolatedPIDs()
			Expect(pids).To(HaveLen(1))
			Expect(pids).To(HaveKey(100))
		})
	})
})

var _ = Describe("Violation", func() {
	Describe("LastEvent", func() {
		It("returns the most recent event", func() {
			v := rules.Violation{
				Events: []rules.ViolationEvent{
					{Kind: rules.EventDetected, Message: "first"},
					{Kind: rules.EventAction, Message: "second"},
				},
			}
			Expect(v.LastEvent().Message).To(Equal("second"))
		})

		It("returns a zero event when empty", func() {
			v := rules.Violation{}
			Expect(v.LastEvent()).To(Equal(rules.ViolationEvent{}))
		})
	})

	Describe("CreatedAt", func() {
		It("returns the time of the first event", func() {
			t := time.Now()
			v := rules.Violation{
				Events: []rules.ViolationEvent{
					{Time: t, Kind: rules.EventDetected},
					{Time: t.Add(time.Second), Kind: rules.EventAction},
				},
			}
			Expect(v.CreatedAt()).To(Equal(t))
		})

		It("returns zero time when no events", func() {
			v := rules.Violation{}
			Expect(v.CreatedAt()).To(Equal(time.Time{}))
		})
	})
})
