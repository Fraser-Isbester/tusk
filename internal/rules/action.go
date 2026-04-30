package rules

import (
	"context"
	"log"

	"github.com/fraser-isbester/tusk/internal/db"
)

// Executor is the interface for rule actions.
type Executor interface {
	Execute(ctx context.Context, database *db.DB, pid int) error
	Name() string
}

// TerminateAction calls pg_terminate_backend.
type TerminateAction struct{}

func (a *TerminateAction) Execute(ctx context.Context, database *db.DB, pid int) error {
	return database.TerminateBackend(ctx, pid)
}

func (a *TerminateAction) Name() string { return "terminate" }

// CancelAction calls pg_cancel_backend.
type CancelAction struct{}

func (a *CancelAction) Execute(ctx context.Context, database *db.DB, pid int) error {
	return database.CancelQuery(ctx, pid)
}

func (a *CancelAction) Name() string { return "cancel" }

// LogAction writes a log line to stderr.
type LogAction struct{}

func (a *LogAction) Execute(_ context.Context, _ *db.DB, pid int) error {
	log.Printf("[rules] violation on PID %d", pid)
	return nil
}

func (a *LogAction) Name() string { return "log" }
