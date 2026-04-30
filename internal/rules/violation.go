package rules

import (
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/db"
)

// EventKind identifies a type of violation lifecycle event.
type EventKind string

const (
	EventDetected EventKind = "violation detected"
	EventAction   EventKind = "action triggered"
	EventSent     EventKind = "action sent"
	EventCooldown EventKind = "cooldown set"
	EventClosed   EventKind = "violation closed"
	EventError    EventKind = "error"
)

// ViolationEvent is a timestamped entry in a violation's audit log.
type ViolationEvent struct {
	Time    time.Time
	Kind    EventKind
	Message string
}

// Violation records that a rule fired against a resource, with a full
// lifecycle audit log of events.
type Violation struct {
	RuleName     string
	ResourceType ResourceType
	PID          int
	Expression   string
	ActionName   string
	Active       bool
	DryRun       bool
	Events       []ViolationEvent

	QuerySnap       *db.Query
	TransactionSnap *db.Transaction
	LockSnap        *db.Lock
}

// LastEvent returns the most recent event, or a zero event if empty.
func (v *Violation) LastEvent() ViolationEvent {
	if len(v.Events) == 0 {
		return ViolationEvent{}
	}
	return v.Events[len(v.Events)-1]
}

// CreatedAt returns the timestamp of the first event.
func (v *Violation) CreatedAt() time.Time {
	if len(v.Events) == 0 {
		return time.Time{}
	}
	return v.Events[0].Time
}

type violationKey struct {
	RuleName string
	PID      int
}

// ViolationStore retains violations with TTL eviction and cooldown dedup.
type ViolationStore struct {
	mu         sync.Mutex
	violations []Violation
	lastAction map[violationKey]time.Time
	ttl        time.Duration
	maxSize    int
}

// NewViolationStore creates a new violation store.
func NewViolationStore(ttl time.Duration, maxSize int) *ViolationStore {
	return &ViolationStore{
		lastAction: make(map[violationKey]time.Time),
		ttl:        ttl,
		maxSize:    maxSize,
	}
}

// RecordOrUpdate finds an existing active violation for the same rule+PID
// and returns it for event appending, or creates a new one.
// Returns the violation pointer and whether the action should fire (not in cooldown).
func (s *ViolationStore) RecordOrUpdate(v Violation, cooldown time.Duration) (*Violation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := violationKey{RuleName: v.RuleName, PID: v.PID}

	// Find existing active violation for this rule+PID
	for i := len(s.violations) - 1; i >= 0; i-- {
		existing := &s.violations[i]
		if existing.RuleName == v.RuleName && existing.PID == v.PID && existing.Active {
			// Update snapshot
			existing.QuerySnap = v.QuerySnap
			existing.TransactionSnap = v.TransactionSnap
			existing.LockSnap = v.LockSnap
			// Check cooldown
			if last, ok := s.lastAction[key]; ok && cooldown > 0 {
				if time.Since(last) < cooldown {
					return existing, false
				}
			}
			s.lastAction[key] = time.Now()
			return existing, true
		}
	}

	// New violation
	s.violations = append(s.violations, v)
	if len(s.violations) > s.maxSize {
		s.violations = s.violations[len(s.violations)-s.maxSize:]
	}
	s.lastAction[key] = time.Now()
	return &s.violations[len(s.violations)-1], true
}

// Prune removes violations whose last event is older than TTL.
func (s *ViolationStore) Prune() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.ttl)
	n := 0
	for _, v := range s.violations {
		// Never prune active violations — they're still relevant
		if v.Active || v.LastEvent().Time.After(cutoff) || v.CreatedAt().After(cutoff) {
			s.violations[n] = v
			n++
		}
	}
	s.violations = s.violations[:n]

	for key, t := range s.lastAction {
		if t.Before(cutoff) {
			delete(s.lastAction, key)
		}
	}
}

// Recent returns violations within the TTL window, newest first.
func (s *ViolationStore) Recent() []Violation {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.ttl)
	var result []Violation
	for i := len(s.violations) - 1; i >= 0; i-- {
		v := s.violations[i]
		if v.CreatedAt().After(cutoff) || v.LastEvent().Time.After(cutoff) {
			result = append(result, v)
		}
	}
	return result
}

// MarkActive updates the Active flag and appends a closed event for
// violations whose PID is no longer present.
func (s *ViolationStore) MarkActive(activePIDs map[int]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.violations {
		v := &s.violations[i]
		wasActive := v.Active
		v.Active = activePIDs[v.PID]
		if wasActive && !v.Active {
			v.Events = append(v.Events, ViolationEvent{
				Time:    time.Now(),
				Kind:    EventClosed,
				Message: "target PID gone",
			})
		}
	}
}

// ViolatedPIDs returns a map of PIDs currently in active violation.
func (s *ViolationStore) ViolatedPIDs() map[int]Violation {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make(map[int]Violation)
	for i := len(s.violations) - 1; i >= 0; i-- {
		v := s.violations[i]
		if !v.Active {
			continue
		}
		if _, exists := result[v.PID]; !exists {
			result[v.PID] = v
		}
	}
	return result
}
