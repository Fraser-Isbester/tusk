# Agents

## Project

Tusk is a k9s-style terminal UI for PostgreSQL monitoring, written in Go. It connects to live Postgres instances and provides real-time views of queries, transactions, sessions, locks, tables, and indexes. It includes a CEL-based rules engine for policy enforcement with a copilot-style manual action system.

## Architecture

```
cmd/tusk/main.go          Entry point, CLI flags, profile resolution, engine setup
internal/config/           YAML config with profiles, connection strings, rule definitions
internal/db/               PostgreSQL connectivity (pgx pool), query methods, data types
internal/rules/            CEL rules engine, violation store, action executors
internal/tui/              TUI application (tview), views, detail panes, navigation
internal/tui/theme/        Color palette and styles
internal/tui/views/        Individual view implementations (queries, transactions, etc.)
scripts/                   Load testing and seed data
```

## Key types

- `db.ResourceBase` — shared fields for all pg_stat_activity resources (PID, User, App, Database, State, BackendStart)
- `db.Query` — active query with duration, wait events, SQLcommenter tags, BlockedBy, QueryID, QueryStart
- `db.Transaction` — active transaction with XactStart, XactDuration, QueryDuration, LockCount
- `rules.Violation` — rule violation with timestamped event log (detected → action → cooldown → closed)
- `rules.Engine` — evaluates CEL rules against snapshots every 2s, manages violation store

## Conventions

- Views implement the `tui.View` interface: `Table()`, `Start()`, `Stop()`, `ItemCount()`, `SetFilter()`
- Detail views return `*tview.Flex` with split panes (Info, Query/Queries, Activity)
- Navigation uses a view stack with Esc to go back; `Navigator` callback for drill-down
- Tab/Shift-Tab cycles focus between panes in detail views
- All table views save/restore selection across refresh cycles
- `Stop()` methods guard against double-close with `v.done = nil` after close
- Query identity: `(PID, BackendStart)` for sessions, `(PID, XactStart)` for transactions
- SQL formatting is display-only via `formatSQL()` before `highlightSQL()`

## Rules engine

Rules are defined in YAML config per-profile using CEL expressions. The engine evaluates them every 2s against a snapshot of queries, transactions, and locks. Violations are recorded with an append-only event log. Actions (terminate, cancel, log) can be automatic or manually triggered from the Activity pane.

CEL environments are typed per resource (`QueryEnv`, `TransactionEnv`, `LockEnv`) with fields mapped from the Go structs to `map[string]any` activations.

## Build and test

```bash
task build          # go build -o tusk ./cmd/tusk
task run            # build + run with dev profile
task db:up          # start local Postgres in Docker
task loadtest       # generate realistic load
task test           # run all tests with Ginkgo
task check          # run vet + fmt check + tests
./tusk -P <profile> # run with a named profile
./tusk '<dsn>'      # run with a direct connection string
```

## CI requirements — MUST run before every push

Before pushing any branch or opening a PR, always run these checks locally and fix all issues:

```bash
go build ./...          # must compile
gofmt -l .              # must produce no output (all files formatted)
golangci-lint run ./... # must report 0 issues
go test ./...           # must pass
```

Never push code that hasn't passed these checks. CI runs golangci-lint v2 with gofmt, goimports, and gosec enabled.

## Dependencies

- `rivo/tview` + `gdamore/tcell` — TUI framework
- `jackc/pgx/v5` — PostgreSQL driver and connection pool
- `google/cel-go` — Common Expression Language for rule evaluation
- `spf13/cobra` — CLI framework
- `gopkg.in/yaml.v3` — config parsing
