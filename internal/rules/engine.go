package rules

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/db"
)

// Snapshot holds the data fetched in one polling tick.
type Snapshot struct {
	Queries      []db.Query
	Transactions []db.Transaction
	Locks        []db.Lock
}

// Engine evaluates rules against snapshots and manages violation history.
type Engine struct {
	rules      []Rule
	database   *db.DB
	violations *ViolationStore
	mu         sync.RWMutex
}

// NewEngine creates a new rules engine.
func NewEngine(rules []Rule, database *db.DB, violationTTL time.Duration, maxViolations int) *Engine {
	return &Engine{
		rules:      rules,
		database:   database,
		violations: NewViolationStore(violationTTL, maxViolations),
	}
}

// Evaluate runs all rules against the snapshot.
func (e *Engine) Evaluate(ctx context.Context, snap Snapshot) {
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	for i := range rules {
		rule := &rules[i]
		if !rule.Enabled {
			continue
		}

		switch rule.Resource {
		case ResourceQuery:
			for _, q := range snap.Queries {
				activation := queryActivation(q)
				if matched, _, _ := rule.Program.Eval(activation); matched != nil {
					if b, ok := matched.Value().(bool); ok && b {
						qCopy := q
						e.recordViolation(ctx, rule, q.PID, &qCopy, nil, nil)
					}
				}
			}
		case ResourceTransaction:
			for _, t := range snap.Transactions {
				activation := transactionActivation(t)
				if matched, _, _ := rule.Program.Eval(activation); matched != nil {
					if b, ok := matched.Value().(bool); ok && b {
						tCopy := t
						e.recordViolation(ctx, rule, t.PID, nil, &tCopy, nil)
					}
				}
			}
		case ResourceLock:
			for _, l := range snap.Locks {
				activation := lockActivation(l)
				if matched, _, _ := rule.Program.Eval(activation); matched != nil {
					if b, ok := matched.Value().(bool); ok && b {
						lCopy := l
						e.recordViolation(ctx, rule, l.BlockedPID, nil, nil, &lCopy)
					}
				}
			}
		}
	}

	// Mark violations as active/closed based on current snapshot PIDs.
	activePIDs := make(map[int]bool, len(snap.Queries)+len(snap.Transactions)+len(snap.Locks))
	for _, q := range snap.Queries {
		activePIDs[q.PID] = true
	}
	for _, t := range snap.Transactions {
		activePIDs[t.PID] = true
	}
	for _, l := range snap.Locks {
		activePIDs[l.BlockedPID] = true
		activePIDs[l.BlockingPID] = true
	}
	e.violations.MarkActive(activePIDs)
	e.violations.Prune()
}

func (e *Engine) recordViolation(ctx context.Context, rule *Rule, pid int, qSnap *db.Query, tSnap *db.Transaction, lSnap *db.Lock) {
	now := time.Now()

	v := Violation{
		RuleName:        rule.Name,
		ResourceType:    rule.Resource,
		PID:             pid,
		Expression:      rule.Expression,
		ActionName:      rule.Action.Name(),
		Active:          true,
		DryRun:          rule.DryRun,
		QuerySnap:       qSnap,
		TransactionSnap: tSnap,
		LockSnap:        lSnap,
		Events: []ViolationEvent{
			{Time: now, Kind: EventDetected, Message: fmt.Sprintf("PID %d matched rule %q", pid, rule.Name)},
		},
	}

	existing, shouldAct := e.violations.RecordOrUpdate(v, rule.Cooldown)

	if !shouldAct {
		// In cooldown — no action events to append
		return
	}

	dryRunStr := ""
	if rule.DryRun {
		dryRunStr = " dry-run=true"
	}
	existing.Events = append(existing.Events, ViolationEvent{
		Time:    time.Now(),
		Kind:    EventAction,
		Message: fmt.Sprintf("triggering action `%s`%s", rule.Action.Name(), dryRunStr),
	})

	if rule.DryRun {
		return
	}

	// Execute the action
	if err := rule.Action.Execute(ctx, e.database, pid); err != nil {
		existing.Events = append(existing.Events, ViolationEvent{
			Time:    time.Now(),
			Kind:    EventError,
			Message: fmt.Sprintf("action failed: %s", err.Error()),
		})
		log.Printf("[rules] action %s failed for PID %d: %v", rule.Action.Name(), pid, err)
	} else {
		existing.Events = append(existing.Events, ViolationEvent{
			Time:    time.Now(),
			Kind:    EventSent,
			Message: fmt.Sprintf("executed %s on PID %d", rule.Action.Name(), pid),
		})
	}

	if rule.Cooldown > 0 {
		existing.Events = append(existing.Events, ViolationEvent{
			Time:    time.Now(),
			Kind:    EventCooldown,
			Message: fmt.Sprintf("cooldown %s", rule.Cooldown),
		})
	}
}

// RecentViolations returns violation history within TTL, newest first.
func (e *Engine) RecentViolations() []Violation {
	return e.violations.Recent()
}

// ViolatedPIDs returns a map of PIDs currently in active violation.
func (e *Engine) ViolatedPIDs() map[int]Violation {
	return e.violations.ViolatedPIDs()
}

// Rules returns a snapshot of the configured rules.
func (e *Engine) Rules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]Rule, len(e.rules))
	copy(result, e.rules)
	return result
}

// UpdateRules hot-swaps the rule set.
func (e *Engine) UpdateRules(rules []Rule) {
	e.mu.Lock()
	e.rules = rules
	e.mu.Unlock()
}

// RuleCount returns the number of active rules.
func (e *Engine) RuleCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	count := 0
	for _, r := range e.rules {
		if r.Enabled {
			count++
		}
	}
	return count
}

// --- Activation builders ---

func queryActivation(q db.Query) map[string]any {
	return map[string]any{
		"pid":             int64(q.PID),
		"user":            q.User,
		"app":             q.App,
		"database":        q.Database,
		"client_addr":     q.ClientAddr,
		"state":           q.State,
		"duration":        q.Duration,
		"wait_event_type": q.WaitEventType,
		"wait_event":      q.WaitEvent,
		"query":           q.QueryText,
		"route":           q.Comment.Route,
		"controller":      q.Comment.Controller,
		"action_name":     q.Comment.Action,
		"framework":       q.Comment.Framework,
		"blocked_by":      int64(q.BlockedBy),
		"query_id":        q.QueryID,
	}
}

func transactionActivation(t db.Transaction) map[string]any {
	return map[string]any{
		"pid":            int64(t.PID),
		"user":           t.User,
		"app":            t.App,
		"database":       t.Database,
		"state":          t.State,
		"xact_duration":  t.XactDuration,
		"query_duration": t.QueryDuration,
		"query":          t.QueryText,
		"lock_count":     int64(t.LockCount),
	}
}

func lockActivation(l db.Lock) map[string]any {
	return map[string]any{
		"blocked_pid":   int64(l.BlockedPID),
		"blocking_pid":  int64(l.BlockingPID),
		"blocked_user":  l.BlockedUser,
		"blocking_user": l.BlockingUser,
		"blocked_app":   l.BlockedApp,
		"blocking_app":  l.BlockingApp,
		"lock_type":     l.LockType,
		"mode":          l.Mode,
		"wait_duration": l.WaitDuration,
	}
}
