# Tusk
A postgres admin TUI, built with Go inspired by k9s. This is not a Database client, but rather a tool for managing and monitoring Postgres databases at scale.

## Core Types

### Core: Runtime
- Connection: A connection to a Postgres database
- Transaction: A transaction within a connection
- Query: A SQL query executed within a transaction
- Role: A database role (user or group)

### Core: Schema
- Database: A Postgres database
- Schema: A schema within a database
- Table: A table within a schema
- Index: An index on a table

### Tusk
- TuskRule: A rule that defines a condition and an action to be taken when the condition is met.
- TuskRuleEvaluation: The result of evaluating a TuskRule, including whether the condition was met and any actions taken.
