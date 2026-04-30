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
task test           # run all tests
task test:cover     # run tests with coverage report
task check          # run vet + fmt check + tests
```

## Submitting changes

1. Fork the repo and create a feature branch
2. Make your changes
3. Run `task check` to verify everything passes
4. Open a pull request against `main`

For larger changes, open an issue first to discuss the approach.
