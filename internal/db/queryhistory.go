package db

import (
	"sync"
	"time"
)

// QueryHistoryEntry records a query observed for a PID at a point in time.
type QueryHistoryEntry struct {
	Query     string
	State     string
	Timestamp time.Time
}

// QueryHistory tracks the sequence of queries observed per PID.
// Each PID keeps a ring buffer of the last N distinct queries.
type QueryHistory struct {
	mu      sync.Mutex
	history map[int][]QueryHistoryEntry
	maxSize int
}

// NewQueryHistory creates a new query history tracker.
func NewQueryHistory(maxPerPID int) *QueryHistory {
	return &QueryHistory{
		history: make(map[int][]QueryHistoryEntry),
		maxSize: maxPerPID,
	}
}

// Record adds a query observation for a PID. Only records if the query
// text differs from the last recorded entry (avoids duplicates from polling).
func (h *QueryHistory) Record(pid int, query, state string) {
	if query == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	entries := h.history[pid]

	// Skip if same as last entry
	if len(entries) > 0 && entries[len(entries)-1].Query == query {
		return
	}

	entry := QueryHistoryEntry{
		Query:     query,
		State:     state,
		Timestamp: time.Now(),
	}

	entries = append(entries, entry)

	// Ring buffer: keep only the last maxSize entries
	if len(entries) > h.maxSize {
		entries = entries[len(entries)-h.maxSize:]
	}

	h.history[pid] = entries
}

// Get returns the query history for a PID (oldest first).
func (h *QueryHistory) Get(pid int) []QueryHistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	entries := h.history[pid]
	if len(entries) == 0 {
		return nil
	}

	// Return a copy
	result := make([]QueryHistoryEntry, len(entries))
	copy(result, entries)
	return result
}

// RecordAll records observations from a slice of active queries.
func (h *QueryHistory) RecordAll(queries []Query) {
	for _, q := range queries {
		h.Record(q.PID, q.QueryText, q.State)
	}
}

// RecordTransactions records observations from a slice of transactions.
func (h *QueryHistory) RecordTransactions(txns []Transaction) {
	for _, t := range txns {
		h.Record(t.PID, t.QueryText, t.State)
	}
}

// Cleanup removes entries for PIDs that are no longer active.
func (h *QueryHistory) Cleanup(activePIDs map[int]bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for pid := range h.history {
		if !activePIDs[pid] {
			delete(h.history, pid)
		}
	}
}
