package db

import (
	"sync"
	"time"
)

// QueryHistoryEntry records a query observed for a backend at a point in time.
type QueryHistoryEntry struct {
	Query     string
	State     string
	Timestamp time.Time
}

// sessionKey uniquely identifies a backend session (PID can be recycled).
type sessionKey struct {
	PID          int
	BackendStart time.Time
}

// QueryHistory tracks the sequence of queries observed per backend session.
// Each session keeps a ring buffer of the last N distinct queries.
type QueryHistory struct {
	mu      sync.Mutex
	history map[sessionKey][]QueryHistoryEntry
	maxSize int
}

// NewQueryHistory creates a new query history tracker.
func NewQueryHistory(maxPerSession int) *QueryHistory {
	return &QueryHistory{
		history: make(map[sessionKey][]QueryHistoryEntry),
		maxSize: maxPerSession,
	}
}

// Record adds a query observation for a backend session.
// Only records if the query text differs from the last recorded entry.
func (h *QueryHistory) Record(pid int, backendStart time.Time, query, state string) {
	if query == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	key := sessionKey{PID: pid, BackendStart: backendStart}
	entries := h.history[key]

	if len(entries) > 0 && entries[len(entries)-1].Query == query {
		return
	}

	entry := QueryHistoryEntry{
		Query:     query,
		State:     state,
		Timestamp: time.Now(),
	}

	entries = append(entries, entry)
	if len(entries) > h.maxSize {
		entries = entries[len(entries)-h.maxSize:]
	}

	h.history[key] = entries
}

// Get returns the query history for a backend session (oldest first).
func (h *QueryHistory) Get(pid int, backendStart time.Time) []QueryHistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	key := sessionKey{PID: pid, BackendStart: backendStart}
	entries := h.history[key]
	if len(entries) == 0 {
		return nil
	}

	result := make([]QueryHistoryEntry, len(entries))
	copy(result, entries)
	return result
}

// RecordAll records observations from a slice of active queries.
func (h *QueryHistory) RecordAll(queries []Query) {
	for _, q := range queries {
		h.Record(q.PID, q.BackendStart, q.QueryText, q.State)
	}
}

// RecordTransactions records observations from a slice of transactions.
// Uses XactStart as the session key so each transaction gets its own history.
func (h *QueryHistory) RecordTransactions(txns []Transaction) {
	for _, t := range txns {
		h.Record(t.PID, t.XactStart, t.QueryText, t.State)
	}
}

// Cleanup removes entries for sessions that are no longer active.
func (h *QueryHistory) Cleanup(activeKeys map[sessionKey]bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for key := range h.history {
		if !activeKeys[key] {
			delete(h.history, key)
		}
	}
}
