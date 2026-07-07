[![CI](https://github.com/Fraser-Isbester/tusk/actions/workflows/ci.yml/badge.svg)](https://github.com/Fraser-Isbester/tusk/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/fraser-isbester/tusk)](https://goreportcard.com/report/github.com/fraser-isbester/tusk)

# Tusk

A k9s-style terminal UI for real-time PostgreSQL monitoring and management. This is Not a database client, it's a tool for observing and acting on live database activity.

<p align="center">
  <img src="assets/tusk-queries-view.png" width="700" alt="Queries view" />
  <br/>
  <img src="assets/tusk-tx-detail.png" width="700" alt="Transaction detail" />
</p>

## Features

- **Live views** — Queries, transactions, sessions, locks, tables, indexes with 2s auto-refresh
- **Rules engine** — Define policy rules in YAML using [CEL](https://cel.dev/) expressions that evaluate against live database state
- **Violation tracking** — Audit log of rule violations with timestamped event lifecycle (detected → action → cooldown → closed)
- **Auto Remediation** — Automatically take actions on rule violations based on defined policies
- **Split-pane detail views** — Query detail with formatted SQL, transaction detail with query history, lock detail with blocker/blocked side-by-side
- **Interactive Activity pane** — Shows lock contention and rule violations per PID; Enter on a blocking PID navigates to its detail
- **Tab navigation** — Arrow keys cycle views, Tab cycles panes within detail views
- **Column sorting** — Shift+letter toggles sort on any column
- **Filtering** — `/` to filter any view, persists visually until cleared with Esc

## Install

**From source:**
```bash
go install github.com/fraser-isbester/tusk/cmd/tusk@latest
```

**Pre-built binaries:** download from [GitHub Releases](https://github.com/fraser-isbester/tusk/releases).

## Usage

```bash
# Direct connection
tusk 'postgres://user:pass@localhost:5432/mydb'

# Using a profile
tusk -P production
```

## Daemon

`tuskd` runs a profile's rules engine headlessly — the same rules, evaluation, and
auto-remediation as the TUI, with no terminal UI. Use it to enforce rules continuously
in the background (e.g. under systemd).

**Install:**
```bash
go install github.com/fraser-isbester/tusk/cmd/tuskd@latest
```
(`tuskd` is also included in the [release archives](https://github.com/fraser-isbester/tusk/releases) alongside `tusk`.)

**Run:**
```bash
# Evaluate the profile's rules every 2s (default)
tuskd -P production

# Custom polling interval
tuskd -P production -i 5s
```

`tuskd` requires the selected profile to define at least one rule; it reads the same
`~/.config/tusk/config.yaml` as `tusk`, honors `readonly`/`dry_run`, and shuts down
cleanly on `SIGINT`/`SIGTERM`.

**systemd unit** (`/etc/systemd/system/tuskd.service`):
```ini
[Unit]
Description=Tusk rules daemon
After=network.target

[Service]
ExecStart=/usr/local/bin/tuskd -P production
Restart=on-failure
User=tusk

[Install]
WantedBy=multi-user.target
```

## Configuration

`~/.config/tusk/config.yaml`

```yaml
default_profile: dev

profiles:
  dev:
    url: "postgres://postgres:postgres@localhost:5432/mydb?sslmode=disable"
    rules:
      - name: kill-idle-in-txn
        resource: transaction
        when: "state == 'idle in transaction' && xact_duration > duration('5m')"
        action: terminate
        cooldown: 5m
        dry_run: true

      - name: long-queries
        resource: query
        when: "state == 'active' && duration > duration('30s')"
        action: cancel
        cooldown: 1m
        dry_run: true

  production:
    host: db.example.com
    port: 5432
    user: monitor
    password: 'secret'
    database: prod
    readonly: true
    rules:
      - name: idle-in-txn-5m
        resource: transaction
        when: "state == 'idle in transaction' && xact_duration > duration('5m')"
        action: terminate
        cooldown: 5m
        dry_run: true
```

### Rule fields

| Field | Description |
|-------|-------------|
| `resource` | `query`, `transaction`, or `lock` |
| `when` | CEL expression evaluated against the resource fields |
| `action` | `terminate`, `cancel`, or `log` |
| `cooldown` | Minimum interval between action firings per PID |
| `dry_run` | Record violations but don't execute the action |
| `readonly` | Profile-level: forces all rules to dry-run |

### CEL expression fields

**Query**: `pid`, `user`, `app`, `database`, `state`, `duration`, `wait_event_type`, `wait_event`, `query`, `blocked_by`, `query_id`, `route`, `controller`, `action_name`, `framework`

**Transaction**: `pid`, `user`, `app`, `database`, `state`, `xact_duration`, `query_duration`, `query`, `lock_count`

**Lock**: `blocked_pid`, `blocking_pid`, `blocked_user`, `blocking_user`, `blocked_app`, `blocking_app`, `lock_type`, `mode`, `wait_duration`

## Views

| Command | Description |
|---------|-------------|
| `:queries` | Active queries with duration, wait events, blocking info |
| `:transactions` | Active transactions sorted by age with lock counts |
| `:sessions` | Connections grouped by user, app, state |
| `:tables` | Table sizes, row counts, dead tuple %, vacuum stats |
| `:locks` | Blocked/blocking lock pairs |
| `:indexes` | Index scan counts and sizes |
| `:rules` | Configured rules with violation counts |
| `:violations` | Violation audit log with event timeline |

## Key bindings

| Key | Action |
|-----|--------|
| `←` `→` | Switch views |
| `:` | Command prompt |
| `/` | Filter |
| `Tab` | Cycle panes in detail view |
| `Enter` | Drill into detail / fire action |
| `Esc` | Back / clear filter |
| `Shift+letter` | Sort by column |
| `c` | Cancel query (in query detail) |
| `t` | Terminate backend (in query/transaction detail) |
| `q` | Quit |

## Development

```bash
task db:up        # Start local Postgres in Docker
task run          # Build and run against local dev DB
task loadtest     # Generate realistic load (queries, transactions, locks, idle-in-txn)
```

### Testing
```bash
task test         # Run all tests with Ginkgo
task test:cover   # Run tests with coverage report
```
