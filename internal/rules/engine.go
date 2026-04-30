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

// Engine evaluates rules against snapshots and manages breach history.
type Engine struct {
	rules    []Rule
	database *db.DB
	breaches *BreachStore
	mu       sync.RWMutex
}

// NewEngine creates a new rules engine.
func NewEngine(rules []Rule, database *db.DB, breachTTL time.Duration, maxBreaches int) *Engine {
	return &Engine{
		rules:    rules,
		database: database,
		breaches: NewBreachStore(breachTTL, maxBreaches),
	}
}

// Evaluate runs all rules against the snapshot. Returns breaches from this tick.
func (e *Engine) Evaluate(ctx context.Context, snap Snapshot) []Breach {
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	var tickBreaches []Breach

	for i := range rules {
		rule := &rules[i]
		if !rule.Enabled {
			continue
		}

		switch rule.Resource {
		case ResourceQuery:
			for _, q := range snap.Queries {
				activation := queryActivation(q)
				if b, ok := e.evaluate(ctx, rule, activation, q.PID); ok {
					qCopy := q
					b.QuerySnap = &qCopy
					tickBreaches = append(tickBreaches, b)
				}
			}
		case ResourceTransaction:
			for _, t := range snap.Transactions {
				activation := transactionActivation(t)
				if b, ok := e.evaluate(ctx, rule, activation, t.PID); ok {
					tCopy := t
					b.TransactionSnap = &tCopy
					tickBreaches = append(tickBreaches, b)
				}
			}
		case ResourceLock:
			for _, l := range snap.Locks {
				activation := lockActivation(l)
				if b, ok := e.evaluate(ctx, rule, activation, l.BlockedPID); ok {
					lCopy := l
					b.LockSnap = &lCopy
					tickBreaches = append(tickBreaches, b)
				}
			}
		}
	}

	// Build set of all active PIDs from the snapshot so we can mark
	// which breaches are still active vs completed.
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
	e.breaches.MarkActive(activePIDs)

	e.breaches.Prune()
	return tickBreaches
}

func (e *Engine) evaluate(ctx context.Context, rule *Rule, activation map[string]any, pid int) (Breach, bool) {
	out, _, err := rule.Program.Eval(activation)
	if err != nil {
		return Breach{}, false
	}

	breached, ok := out.Value().(bool)
	if !ok || !breached {
		return Breach{}, false
	}

	breach := Breach{
		RuleName:     rule.Name,
		ResourceType: rule.Resource,
		PID:          pid,
		Resource:     fmt.Sprintf("pid=%d", pid),
		Expression:   rule.Expression,
		Action:       rule.Action.Name(),
		Timestamp:    time.Now(),
		Active:       true,
	}

	shouldAct := e.breaches.Record(breach, rule.Cooldown)
	if shouldAct && !rule.DryRun {
		if err := rule.Action.Execute(ctx, e.database, pid); err != nil {
			breach.Error = err.Error()
			log.Printf("[rules] action %s failed for PID %d: %v", rule.Action.Name(), pid, err)
		} else {
			breach.Actioned = true
		}
	}

	return breach, true
}

// RecentBreaches returns breach history within TTL, newest first.
func (e *Engine) RecentBreaches() []Breach {
	return e.breaches.Recent()
}

// BreachedPIDs returns a map of PIDs currently in breach.
func (e *Engine) BreachedPIDs() map[int]Breach {
	return e.breaches.BreachedPIDs()
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
