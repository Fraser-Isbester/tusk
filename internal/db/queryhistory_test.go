package db_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
)

var _ = Describe("QueryHistory", func() {
	var (
		h            *db.QueryHistory
		backendStart time.Time
		pid          int
	)

	BeforeEach(func() {
		h = db.NewQueryHistory(5)
		backendStart = time.Now()
		pid = 42
	})

	Describe("Record and Get", func() {
		It("records a new query and retrieves it", func() {
			h.Record(pid, backendStart, "SELECT 1", "active")
			entries := h.Get(pid, backendStart)
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].Query).To(Equal("SELECT 1"))
			Expect(entries[0].State).To(Equal("active"))
		})

		It("does not record empty queries", func() {
			h.Record(pid, backendStart, "", "active")
			entries := h.Get(pid, backendStart)
			Expect(entries).To(BeNil())
		})

		It("deduplicates consecutive identical queries", func() {
			h.Record(pid, backendStart, "SELECT 1", "active")
			h.Record(pid, backendStart, "SELECT 1", "active")
			h.Record(pid, backendStart, "SELECT 1", "active")
			entries := h.Get(pid, backendStart)
			Expect(entries).To(HaveLen(1))
		})

		It("records different queries in order", func() {
			h.Record(pid, backendStart, "SELECT 1", "active")
			h.Record(pid, backendStart, "SELECT 2", "active")
			h.Record(pid, backendStart, "SELECT 3", "idle")
			entries := h.Get(pid, backendStart)
			Expect(entries).To(HaveLen(3))
			Expect(entries[0].Query).To(Equal("SELECT 1"))
			Expect(entries[2].Query).To(Equal("SELECT 3"))
		})

		It("enforces the max size ring buffer", func() {
			for i := 1; i <= 8; i++ {
				h.Record(pid, backendStart, "Q"+string(rune('0'+i)), "active")
			}
			entries := h.Get(pid, backendStart)
			Expect(entries).To(HaveLen(5))
			// Should keep the last 5
			Expect(entries[0].Query).To(Equal("Q4"))
			Expect(entries[4].Query).To(Equal("Q8"))
		})

		It("returns nil for an unknown session", func() {
			entries := h.Get(999, time.Now())
			Expect(entries).To(BeNil())
		})

		It("isolates sessions with different PIDs", func() {
			h.Record(1, backendStart, "Q-A", "active")
			h.Record(2, backendStart, "Q-B", "active")
			Expect(h.Get(1, backendStart)).To(HaveLen(1))
			Expect(h.Get(2, backendStart)).To(HaveLen(1))
			Expect(h.Get(1, backendStart)[0].Query).To(Equal("Q-A"))
		})

		It("isolates sessions with the same PID but different backend start times", func() {
			t1 := time.Now()
			t2 := t1.Add(time.Second)
			h.Record(pid, t1, "old-session", "active")
			h.Record(pid, t2, "new-session", "active")
			Expect(h.Get(pid, t1)[0].Query).To(Equal("old-session"))
			Expect(h.Get(pid, t2)[0].Query).To(Equal("new-session"))
		})

		It("returns a copy so mutations don't affect internal state", func() {
			h.Record(pid, backendStart, "SELECT 1", "active")
			entries := h.Get(pid, backendStart)
			entries[0].Query = "MUTATED"
			Expect(h.Get(pid, backendStart)[0].Query).To(Equal("SELECT 1"))
		})
	})

	Describe("RecordAll", func() {
		It("records queries from a slice of Query structs", func() {
			queries := []db.Query{
				{ResourceBase: db.ResourceBase{PID: 1, BackendStart: backendStart, State: "active"}, QueryText: "Q1"},
				{ResourceBase: db.ResourceBase{PID: 2, BackendStart: backendStart, State: "idle"}, QueryText: "Q2"},
			}
			h.RecordAll(queries)
			Expect(h.Get(1, backendStart)).To(HaveLen(1))
			Expect(h.Get(2, backendStart)).To(HaveLen(1))
		})
	})

	Describe("RecordTransactions", func() {
		It("records queries from transactions using XactStart as key", func() {
			xactStart := time.Now()
			txns := []db.Transaction{
				{ResourceBase: db.ResourceBase{PID: pid, State: "idle in transaction"}, XactStart: xactStart, QueryText: "BEGIN"},
			}
			h.RecordTransactions(txns)
			Expect(h.Get(pid, xactStart)).To(HaveLen(1))
			Expect(h.Get(pid, xactStart)[0].Query).To(Equal("BEGIN"))
		})
	})
})
