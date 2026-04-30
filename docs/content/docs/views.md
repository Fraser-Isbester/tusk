---
title: Views
weight: 2
---

Tusk provides eight resource views, each accessible via the command prompt (`:`) or arrow-key navigation.

## Resource views

| Command | Description |
|---------|-------------|
| `:queries` | Active queries with duration, wait events, blocking info, and rule violation indicators |
| `:transactions` | Active transactions sorted by age with lock counts and transaction duration |
| `:sessions` | Connections grouped by user, application, and state |
| `:tables` | Table sizes, row counts, dead tuple percentages, and vacuum statistics |
| `:locks` | Blocked/blocking lock pairs with wait duration and lock type |
| `:indexes` | Index scan counts, sizes, and usage statistics |
| `:rules` | Configured rules with violation counts and current status |
| `:violations` | Violation audit log with timestamped event timeline |

All views auto-refresh every 2 seconds. The active view is highlighted in the tab bar, and the item count is shown in parentheses.

## Detail views

Pressing `Enter` on a row in most views opens a detail view with split panes showing additional information.

### Query detail

Shows formatted and syntax-highlighted SQL, query metadata (PID, user, app, database, state, duration, wait events), and an Activity pane displaying lock contention and rule violations for the query's PID. Press `c` to cancel the query or `t` to terminate the backend.

### Transaction detail

Displays transaction metadata (PID, user, state, transaction duration, query duration, lock count) alongside the current query text. Includes a query history pane showing previous queries executed within the same transaction, and an Activity pane with violations.

### Lock detail

Shows the blocked and blocking backends side-by-side with their respective queries, users, applications, and lock information (lock type, mode, wait duration).

### Table detail

Displays detailed table statistics including size, row estimates, dead tuples, sequential vs. index scan ratios, and vacuum/analyze timestamps.

## Pane navigation

Detail views contain multiple panes. Use `Tab` to move focus to the next pane and `Shift+Tab` to move to the previous pane. The currently focused pane is highlighted with a brighter border.
