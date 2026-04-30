# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Live views for queries, transactions, sessions, locks, tables, and indexes with 2s auto-refresh
- CEL-based rules engine for policy enforcement against live database state
- Violation tracking with timestamped audit log (detected, action, cooldown, closed)
- Copilot mode: rules run in dry-run by default with manual action firing from Activity pane
- Split-pane detail views with formatted SQL, query history, and lock blocker/blocked side-by-side
- Interactive Activity pane showing lock contention and rule violations per PID
- Column sorting via Shift+letter key bindings
- Row filtering with `/` in any view
- Command prompt with `:` for view navigation
- Help panel with `h` key
- Profile-based configuration via `~/.config/tusk/config.yaml`
- Stable query identity using `(PID, BackendStart)` for sessions and `(PID, XactStart)` for transactions
- SQLcommenter tag extraction (app, route, controller, action, framework)
- CI pipeline with lint, test, vulnerability check, and module tidy
- Ginkgo/Gomega test suites for config, db, and rules packages
