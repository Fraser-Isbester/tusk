---
title: Getting Started
weight: 1
---

## Install

### From source

```bash
go install github.com/fraser-isbester/tusk/cmd/tusk@latest
```

### Pre-built binaries

Download from [GitHub Releases](https://github.com/fraser-isbester/tusk/releases).

## Usage

```bash
# Direct connection
tusk 'postgres://user:pass@localhost:5432/mydb'

# Using a profile
tusk -P production
```

## Configuration

Tusk reads its configuration from `~/.config/tusk/config.yaml`. A configuration file defines one or more connection profiles, each with optional rules.

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

### Profile fields

Profiles can specify a connection either as a full `url` or with individual fields (`host`, `port`, `user`, `password`, `database`, `sslmode`). Additional profile-level options:

| Field | Description |
|-------|-------------|
| `readonly` | Forces all rules in this profile to dry-run mode |
| `color` | Profile accent color for the header |
| `refresh_interval` | Polling interval (default `2s`) |

### Rule fields

| Field | Description |
|-------|-------------|
| `name` | Human-readable identifier for the rule |
| `resource` | `query`, `transaction`, or `lock` |
| `when` | CEL expression evaluated against the resource fields |
| `action` | `terminate`, `cancel`, or `log` |
| `cooldown` | Minimum interval between action firings per PID |
| `dry_run` | Record violations but don't execute the action |

### CEL expression fields

Each resource type exposes different fields to CEL expressions:

**Query**: `pid`, `user`, `app`, `database`, `state`, `duration`, `wait_event_type`, `wait_event`, `query`, `blocked_by`, `query_id`, `route`, `controller`, `action_name`, `framework`

**Transaction**: `pid`, `user`, `app`, `database`, `state`, `xact_duration`, `query_duration`, `query`, `lock_count`

**Lock**: `blocked_pid`, `blocking_pid`, `blocked_user`, `blocking_user`, `blocked_app`, `blocking_app`, `lock_type`, `mode`, `wait_duration`
