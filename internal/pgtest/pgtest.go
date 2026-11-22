// Package pgtest provisions throwaway PostgreSQL databases and client sessions
// for integration tests. It is imported only from _test files.
//
// Integration tests run when TUSK_TEST_DSN points at a server the tests may
// create databases and roles on (e.g. postgres://postgres:postgres@localhost:5432/postgres).
// When it is unset they skip, unless TUSK_TEST_REQUIRE_DB is set (CI), in which
// case they fail — so a misconfigured pipeline can't silently go green.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"

	"github.com/jackc/pgx/v5"
)

const (
	EnvDSN     = "TUSK_TEST_DSN"
	EnvRequire = "TUSK_TEST_REQUIRE_DB"
)

// Resolve returns the admin DSN, or "" when integration tests are not
// configured. required reports whether an empty DSN must fail rather than skip.
func Resolve() (dsn string, required bool) {
	return os.Getenv(EnvDSN), os.Getenv(EnvRequire) != ""
}

// UniqueName returns prefix plus a random suffix, safe as an unquoted identifier.
// pg_stat_activity and roles are cluster-wide, so every test that observes them
// tags its sessions with a unique application_name to avoid cross-talk.
func UniqueName(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// TempDB is a database created for one suite and dropped afterwards.
type TempDB struct {
	Name     string
	DSN      string
	adminDSN string
}

// CreateDatabase creates a fresh database on the server behind adminDSN.
func CreateDatabase(ctx context.Context, adminDSN string) (*TempDB, error) {
	name := UniqueName("tusk_test")
	// Retarget first: a DSN we can't rewrite must fail before a database
	// exists that nobody holds a handle to drop.
	dsn, err := WithDatabase(adminDSN, name)
	if err != nil {
		return nil, err
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return nil, fmt.Errorf("connecting as admin: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// template0, not the default template1: a shared server's template1 may
	// carry extensions (pg_stat_statements) or tables that break the specs'
	// assumptions about an empty database.
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE template0"); err != nil {
		return nil, fmt.Errorf("creating %s: %w", name, err)
	}
	return &TempDB{Name: name, DSN: dsn, adminDSN: adminDSN}, nil
}

// Drop force-drops the database, terminating any sessions a failed test left behind.
func (t *TempDB) Drop(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, t.adminDSN)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+t.Name+" WITH (FORCE)")
	return err
}

// WithDatabase rewrites the database in a URL-form DSN.
func WithDatabase(dsn, database string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", fmt.Errorf("%s must be a postgres:// URL", EnvDSN)
	}
	u.Path = "/" + database
	// pgx lets dbname/database query parameters override the path, so a
	// retained one would point the tests back at the original database.
	q := u.Query()
	q.Del("dbname")
	q.Del("database")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Client opens a dedicated connection that plays the role of an application
// backend. Unlike db.New's pool it has no statement_timeout, so it can hold
// locks or sleep for as long as a test needs.
func Client(ctx context.Context, dsn, appName string) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.RuntimeParams["application_name"] = appName
	return pgx.ConnectConfig(ctx, cfg)
}

// Background runs sql on conn in a goroutine and delivers its result, then
// closes the channel so later receives (e.g. in cleanup) never block. Use it
// for statements expected to block (lock waits, pg_sleep).
func Background(ctx context.Context, conn *pgx.Conn, sql string) <-chan error {
	ch := make(chan error, 1)
	go func() {
		_, err := conn.Exec(ctx, sql)
		ch <- err
		close(ch)
	}()
	return ch
}
