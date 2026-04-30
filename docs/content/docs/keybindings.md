---
title: Key Bindings
weight: 4
---

Tusk uses keyboard-driven navigation inspired by k9s and vim.

## Global keys

| Key | Action |
|-----|--------|
| `Left` / `Right` | Switch between views |
| `:` | Open command prompt (type a view name, e.g. `:queries`) |
| `/` | Open filter input |
| `h` | Show help overlay |
| `q` | Quit |
| `Ctrl+C` | Quit |

## Table views

| Key | Action |
|-----|--------|
| `Enter` | Drill into detail view for the selected row |
| `Esc` | Go back to the previous view / clear filter |
| `Shift+letter` | Sort by column (e.g. `Shift+D` for duration) |

## Query and transaction views

| Key | Action |
|-----|--------|
| `c` | Cancel query (`pg_cancel_backend`) |
| `t` | Terminate backend (`pg_terminate_backend`) |

## Lock view

| Key | Action |
|-----|--------|
| `t` | Terminate the blocking backend |

## Detail views

| Key | Action |
|-----|--------|
| `Tab` | Cycle focus to the next pane |
| `Shift+Tab` | Cycle focus to the previous pane |
| `Esc` | Return to the parent list view |
| `c` | Cancel query (in query/transaction detail) |
| `t` | Terminate backend (in query/transaction detail) |

## Filter

| Key | Action |
|-----|--------|
| `/` | Open filter input |
| `Enter` | Apply filter and return focus to the table |
| `Esc` | Clear filter and close filter input |

Filter text is matched case-insensitively across all visible columns. The filter persists until cleared with `Esc`.
