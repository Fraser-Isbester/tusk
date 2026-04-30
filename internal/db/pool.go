package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool and provides query helpers for Tusk.
type DB struct {
	pool *pgxpool.Pool
}

// New creates a new connection pool to the PostgreSQL instance identified by
// connString. The pool is limited to 3 connections and each connection sets a
// 2-second statement_timeout on creation.
func New(connString string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parsing connection string: %w", err)
	}

	cfg.MaxConns = 5

	cfg.ConnConfig.RuntimeParams["application_name"] = "tusk"

	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET statement_timeout = '5s'")
		return err
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Close releases all connections in the pool.
func (d *DB) Close() {
	d.pool.Close()
}

// Pool returns the underlying pgxpool.Pool.
func (d *DB) Pool() *pgxpool.Pool {
	return d.pool
}
