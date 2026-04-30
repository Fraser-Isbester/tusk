---
title: Rules Engine
weight: 3
---

Tusk includes a CEL-based rules engine that evaluates policy rules against live database state every 2 seconds. Rules are defined per-profile in the YAML configuration.

## Rule fields

| Field | Description |
|-------|-------------|
| `name` | Human-readable identifier for the rule |
| `resource` | `query`, `transaction`, or `lock` |
| `when` | CEL expression evaluated against the resource fields |
| `action` | `terminate`, `cancel`, or `log` |
| `cooldown` | Minimum interval between action firings per PID |
| `dry_run` | Record violations but don't execute the action |

## CEL expression fields

Each resource type exposes a different set of fields to CEL expressions.

### Query

`pid`, `user`, `app`, `database`, `state`, `duration`, `wait_event_type`, `wait_event`, `query`, `blocked_by`, `query_id`, `route`, `controller`, `action_name`, `framework`

### Transaction

`pid`, `user`, `app`, `database`, `state`, `xact_duration`, `query_duration`, `query`, `lock_count`

### Lock

`blocked_pid`, `blocking_pid`, `blocked_user`, `blocking_user`, `blocked_app`, `blocking_app`, `lock_type`, `mode`, `wait_duration`

## Example rules

```yaml
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
```

## Violation lifecycle

When a rule's CEL expression matches a resource, a violation is recorded and progresses through a lifecycle:

1. **Detected** -- The rule expression matched a resource. A violation is created with a timestamped event.
2. **Action** -- The configured action is triggered (or logged if `dry_run: true`).
3. **Cooldown** -- If a `cooldown` is set, no further actions fire for that PID until the cooldown expires. The violation remains visible.
4. **Closed** -- When the PID is no longer present in the database snapshot, the violation is marked as closed.

Violations are visible in the `:violations` view with their full event timeline.

## Dry-run and copilot mode

All rules default to `dry_run: true`. In this mode, violations are recorded and displayed but no action is executed against the database. This allows you to observe what would happen before enabling automatic actions.

From the Activity pane in query or transaction detail views, you can manually fire an action by pressing `Enter` on a violation row. This "copilot" workflow lets you review each violation before taking action.

## Readonly profiles

Setting `readonly: true` at the profile level forces all rules in that profile to dry-run mode, regardless of their individual `dry_run` setting. This is useful for monitoring profiles that should never modify the database.
