package rules

import (
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
)

// ResourceType identifies the kind of Postgres resource a rule targets.
type ResourceType string

const (
	ResourceQuery       ResourceType = "query"
	ResourceTransaction ResourceType = "transaction"
	ResourceLock        ResourceType = "lock"
)

// Rule binds a CEL expression to a resource type, an action, and cooldown config.
type Rule struct {
	Name       string
	Enabled    bool
	Resource   ResourceType
	Program    cel.Program
	Expression string
	Action     Executor
	Cooldown   time.Duration
	DryRun     bool
}

// QueryEnv returns a CEL environment for evaluating rules against db.Query resources.
func QueryEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("pid", cel.IntType),
		cel.Variable("user", cel.StringType),
		cel.Variable("app", cel.StringType),
		cel.Variable("database", cel.StringType),
		cel.Variable("client_addr", cel.StringType),
		cel.Variable("state", cel.StringType),
		cel.Variable("duration", cel.DurationType),
		cel.Variable("wait_event_type", cel.StringType),
		cel.Variable("wait_event", cel.StringType),
		cel.Variable("query", cel.StringType),
		cel.Variable("route", cel.StringType),
		cel.Variable("controller", cel.StringType),
		cel.Variable("action_name", cel.StringType),
		cel.Variable("framework", cel.StringType),
		cel.Variable("blocked_by", cel.IntType),
		cel.Variable("query_id", cel.IntType),
	)
}

// TransactionEnv returns a CEL environment for evaluating rules against db.Transaction resources.
func TransactionEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("pid", cel.IntType),
		cel.Variable("user", cel.StringType),
		cel.Variable("app", cel.StringType),
		cel.Variable("database", cel.StringType),
		cel.Variable("state", cel.StringType),
		cel.Variable("xact_duration", cel.DurationType),
		cel.Variable("query_duration", cel.DurationType),
		cel.Variable("query", cel.StringType),
		cel.Variable("lock_count", cel.IntType),
	)
}

// LockEnv returns a CEL environment for evaluating rules against db.Lock resources.
func LockEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("blocked_pid", cel.IntType),
		cel.Variable("blocking_pid", cel.IntType),
		cel.Variable("blocked_user", cel.StringType),
		cel.Variable("blocking_user", cel.StringType),
		cel.Variable("blocked_app", cel.StringType),
		cel.Variable("blocking_app", cel.StringType),
		cel.Variable("lock_type", cel.StringType),
		cel.Variable("mode", cel.StringType),
		cel.Variable("wait_duration", cel.DurationType),
	)
}

// EnvForResource returns the appropriate CEL environment for the given resource type.
func EnvForResource(rt ResourceType) (*cel.Env, error) {
	switch rt {
	case ResourceQuery:
		return QueryEnv()
	case ResourceTransaction:
		return TransactionEnv()
	case ResourceLock:
		return LockEnv()
	default:
		return nil, fmt.Errorf("unknown resource type: %s", rt)
	}
}
