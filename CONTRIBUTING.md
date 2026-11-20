# Contributing to Tusk

## Prerequisites

- Go 1.26+
- [Task](https://taskfile.dev) (`brew install go-task`)
- Docker (for the local Postgres dev database)

## Setup

```bash
git clone https://github.com/fraser-isbester/tusk.git
cd tusk
task db:up      # start local Postgres
task build      # compile the binary
task run        # build + run against dev profile
```

## Testing

```bash
task test             # unit tests; integration specs skip without a database
task test:integration # everything, against the docker-compose Postgres (task db:up)
task test:cover       # run tests with coverage report
task check            # run vet + fmt check + tests
```

Integration specs (Ginkgo label `integration`) run against a real server when
`TUSK_TEST_DSN` is set to a superuser URL, e.g.
`postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable`. Each
suite creates and drops its own database and tags its sessions with a unique
`application_name`, so a shared dev server is safe. CI sets
`TUSK_TEST_REQUIRE_DB=1` so a missing database fails instead of skipping.

Specs that reproduce a confirmed, unfixed bug call `knownbug.Skip(...)` and are
skipped by default. `task test:known-bugs` (or `TUSK_KNOWN_BUGS=1`) runs them;
they should fail until fixed. Delete the skip in the PR that fixes the bug.

## Submitting changes

1. Fork the repo and create a feature branch
2. Make your changes
3. Run `task check` to verify everything passes
4. Open a pull request against `main`

For larger changes, open an issue first to discuss the approach.
