package rules

import (
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/db"
)

// Breach records that a rule fired against a resource.
type Breach struct {
	RuleName     string
	ResourceType ResourceType // which resource type triggered this
	PID          int
	Resource     string // human-readable resource summary
	Expression   string // the CEL expression that fired
	Action       string // action name (terminate, cancel, log)
	Timestamp    time.Time
	Actioned     bool   // false if dry-run or cooldown-suppressed
	Active       bool   // true if the target PID is still present
	Error        string // action error message, if any

	// Snapshots capture the resource state at breach time so detail views
	// work even after the PID is gone.
	QuerySnap       *db.Query
	TransactionSnap *db.Transaction
	LockSnap        *db.Lock
}

type breachKey struct {
	RuleName string
	PID      int
}

// BreachStore retains breaches with TTL eviction and cooldown dedup.
type BreachStore struct {
	mu         sync.Mutex
	breaches   []Breach
	lastAction map[breachKey]time.Time
	ttl        time.Duration
	maxSize    int
}

// NewBreachStore creates a new breach store.
func NewBreachStore(ttl time.Duration, maxSize int) *BreachStore {
	return &BreachStore{
		lastAction: make(map[breachKey]time.Time),
		ttl:        ttl,
		maxSize:    maxSize,
	}
}

// Record adds or updates a breach. If the same rule+PID already has an active
// breach, the existing entry is refreshed rather than creating a duplicate.
// Returns true if the action should fire (not in cooldown).
func (s *BreachStore) Record(b Breach, cooldown time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := breachKey{RuleName: b.RuleName, PID: b.PID}

	// If this rule+PID already has an active breach, update it in place
	for i := len(s.breaches) - 1; i >= 0; i-- {
		existing := &s.breaches[i]
		if existing.RuleName == b.RuleName && existing.PID == b.PID && existing.Active {
			existing.Timestamp = b.Timestamp
			// Update snapshot to latest state
			existing.QuerySnap = b.QuerySnap
			existing.TransactionSnap = b.TransactionSnap
			existing.LockSnap = b.LockSnap
			// Check cooldown against original action time
			if last, ok := s.lastAction[key]; ok && cooldown > 0 {
				if time.Since(last) < cooldown {
					return false
				}
			}
			s.lastAction[key] = b.Timestamp
			return true
		}
	}

	// New breach -- append
	s.breaches = append(s.breaches, b)
	if len(s.breaches) > s.maxSize {
		s.breaches = s.breaches[len(s.breaches)-s.maxSize:]
	}

	s.lastAction[key] = b.Timestamp
	return true
}

// Prune removes breaches older than TTL and stale cooldown entries.
func (s *BreachStore) Prune() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.ttl)

	// Prune breaches
	n := 0
	for _, b := range s.breaches {
		if b.Timestamp.After(cutoff) {
			s.breaches[n] = b
			n++
		}
	}
	s.breaches = s.breaches[:n]

	// Prune stale cooldown entries
	for key, t := range s.lastAction {
		if t.Before(cutoff) {
			delete(s.lastAction, key)
		}
	}
}

// Recent returns breaches within the TTL window, newest first.
func (s *BreachStore) Recent() []Breach {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.ttl)
	var result []Breach
	for i := len(s.breaches) - 1; i >= 0; i-- {
		if s.breaches[i].Timestamp.After(cutoff) {
			result = append(result, s.breaches[i])
		}
	}
	return result
}

// MarkActive updates the Active flag on all breaches based on which PIDs
// are still present in the current snapshot.
func (s *BreachStore) MarkActive(activePIDs map[int]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.breaches {
		s.breaches[i].Active = activePIDs[s.breaches[i].PID]
	}
}

// BreachedPIDs returns a map of PIDs currently in active breach (most recent per PID).
// Only returns breaches where the target PID is still present.
func (s *BreachStore) BreachedPIDs() map[int]Breach {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.ttl)
	result := make(map[int]Breach)
	for i := len(s.breaches) - 1; i >= 0; i-- {
		b := s.breaches[i]
		if b.Timestamp.Before(cutoff) {
			continue
		}
		if !b.Active {
			continue
		}
		if _, exists := result[b.PID]; !exists {
			result[b.PID] = b
		}
	}
	return result
}
