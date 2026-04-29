package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// SQL constants
// ---------------------------------------------------------------------------

const queryServerInfo = `
SELECT
    version(),
    pg_postmaster_start_time(),
    current_setting('max_connections')::int
`

const queryDatabaseStats = `
SELECT
    xact_commit,
    xact_rollback,
    blks_hit,
    blks_read,
    CASE WHEN blks_hit + blks_read = 0 THEN 0
         ELSE round(blks_hit::numeric / (blks_hit + blks_read), 4)
    END AS cache_hit_ratio
FROM pg_stat_database
WHERE datname = current_database()
`

const queryActiveQueries = `
SELECT
    pid,
    COALESCE(usename, ''),
    COALESCE(application_name, ''),
    COALESCE(client_addr::text, ''),
    COALESCE(state, ''),
    COALESCE(wait_event_type, ''),
    COALESCE(wait_event, ''),
    COALESCE(EXTRACT(EPOCH FROM (clock_timestamp() - query_start)), 0) AS duration_sec,
    COALESCE(query, '')
FROM pg_stat_activity
WHERE pid <> pg_backend_pid()
ORDER BY duration_sec DESC
`

const queryTables = `
SELECT
    schemaname,
    relname,
    pg_total_relation_size(quote_ident(schemaname) || '.' || quote_ident(relname)) AS total_size,
    n_live_tup,
    n_dead_tup,
    seq_scan,
    idx_scan,
    last_vacuum,
    last_autovacuum
FROM pg_stat_user_tables
ORDER BY pg_total_relation_size(quote_ident(schemaname) || '.' || quote_ident(relname)) DESC
`

const queryConnections = `
SELECT
    COALESCE(usename, ''),
    COALESCE(application_name, ''),
    COALESCE(state, ''),
    count(*) AS cnt
FROM pg_stat_activity
GROUP BY usename, application_name, state
ORDER BY cnt DESC
`

const queryDatabases = `
SELECT
    datname,
    pg_database_size(datname) AS size,
    r.rolname AS owner
FROM pg_database d
JOIN pg_roles r ON r.oid = d.datdba
WHERE NOT datistemplate
ORDER BY size DESC
`

const queryRoles = `
SELECT
    rolname,
    rolsuper,
    rolcreaterole,
    rolcreatedb,
    rolcanlogin,
    rolconnlimit
FROM pg_roles
ORDER BY rolname
`

const queryCancelBackend = `SELECT pg_cancel_backend($1)`
const queryTerminateBackend = `SELECT pg_terminate_backend($1)`

// ---------------------------------------------------------------------------
// Query methods
// ---------------------------------------------------------------------------

// GetServerInfo returns the server version, uptime, and max_connections.
func (d *DB) GetServerInfo(ctx context.Context) (*ServerInfo, error) {
	row := d.pool.QueryRow(ctx, queryServerInfo)

	var (
		version   string
		startTime time.Time
		maxConns  int
	)
	if err := row.Scan(&version, &startTime, &maxConns); err != nil {
		return nil, fmt.Errorf("querying server info: %w", err)
	}

	return &ServerInfo{
		Version:        version,
		Uptime:         time.Since(startTime),
		MaxConnections: maxConns,
	}, nil
}

// GetDatabaseStats returns activity counters for the current database.
func (d *DB) GetDatabaseStats(ctx context.Context) (*DatabaseStats, error) {
	row := d.pool.QueryRow(ctx, queryDatabaseStats)

	var s DatabaseStats
	if err := row.Scan(
		&s.XactCommit,
		&s.XactRollback,
		&s.BlksHit,
		&s.BlksRead,
		&s.CacheHitRatio,
	); err != nil {
		return nil, fmt.Errorf("querying database stats: %w", err)
	}
	return &s, nil
}

// GetActiveQueries returns all active backends from pg_stat_activity,
// excluding the caller's own connection.
func (d *DB) GetActiveQueries(ctx context.Context) ([]ActiveQuery, error) {
	rows, err := d.pool.Query(ctx, queryActiveQueries)
	if err != nil {
		return nil, fmt.Errorf("querying active queries: %w", err)
	}
	defer rows.Close()

	var queries []ActiveQuery
	for rows.Next() {
		var (
			q          ActiveQuery
			durationSec float64
		)
		if err := rows.Scan(
			&q.PID,
			&q.User,
			&q.AppName,
			&q.ClientAddr,
			&q.State,
			&q.WaitEventType,
			&q.WaitEvent,
			&durationSec,
			&q.Query,
		); err != nil {
			return nil, fmt.Errorf("scanning active query row: %w", err)
		}
		q.Duration = time.Duration(durationSec * float64(time.Second))
		queries = append(queries, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating active query rows: %w", err)
	}
	return queries, nil
}

// GetTables returns per-table statistics from pg_stat_user_tables.
func (d *DB) GetTables(ctx context.Context) ([]TableInfo, error) {
	rows, err := d.pool.Query(ctx, queryTables)
	if err != nil {
		return nil, fmt.Errorf("querying tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var (
			t              TableInfo
			lastVacuum     sql.NullTime
			lastAutoVacuum sql.NullTime
		)
		if err := rows.Scan(
			&t.Schema,
			&t.Name,
			&t.TotalSize,
			&t.LiveTuples,
			&t.DeadTuples,
			&t.SeqScan,
			&t.IdxScan,
			&lastVacuum,
			&lastAutoVacuum,
		); err != nil {
			return nil, fmt.Errorf("scanning table row: %w", err)
		}
		if lastVacuum.Valid {
			t.LastVacuum = &lastVacuum.Time
		}
		if lastAutoVacuum.Valid {
			t.LastAutoVacuum = &lastAutoVacuum.Time
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating table rows: %w", err)
	}
	return tables, nil
}

// GetConnections returns connections grouped by user, application, and state.
func (d *DB) GetConnections(ctx context.Context) ([]ConnectionGroup, error) {
	rows, err := d.pool.Query(ctx, queryConnections)
	if err != nil {
		return nil, fmt.Errorf("querying connections: %w", err)
	}
	defer rows.Close()

	var groups []ConnectionGroup
	for rows.Next() {
		var g ConnectionGroup
		if err := rows.Scan(&g.User, &g.AppName, &g.State, &g.Count); err != nil {
			return nil, fmt.Errorf("scanning connection group row: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating connection group rows: %w", err)
	}
	return groups, nil
}

// GetDatabases returns all non-template databases with their sizes.
func (d *DB) GetDatabases(ctx context.Context) ([]DatabaseInfo, error) {
	rows, err := d.pool.Query(ctx, queryDatabases)
	if err != nil {
		return nil, fmt.Errorf("querying databases: %w", err)
	}
	defer rows.Close()

	var dbs []DatabaseInfo
	for rows.Next() {
		var db DatabaseInfo
		if err := rows.Scan(&db.Name, &db.Size, &db.Owner); err != nil {
			return nil, fmt.Errorf("scanning database row: %w", err)
		}
		dbs = append(dbs, db)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating database rows: %w", err)
	}
	return dbs, nil
}

// GetRoles returns all roles and their key privileges.
func (d *DB) GetRoles(ctx context.Context) ([]RoleInfo, error) {
	rows, err := d.pool.Query(ctx, queryRoles)
	if err != nil {
		return nil, fmt.Errorf("querying roles: %w", err)
	}
	defer rows.Close()

	var roles []RoleInfo
	for rows.Next() {
		var r RoleInfo
		if err := rows.Scan(
			&r.Name,
			&r.IsSuperuser,
			&r.CanCreateRole,
			&r.CanCreateDB,
			&r.CanLogin,
			&r.ConnLimit,
		); err != nil {
			return nil, fmt.Errorf("scanning role row: %w", err)
		}
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating role rows: %w", err)
	}
	return roles, nil
}

// CancelQuery sends pg_cancel_backend for the given PID.
func (d *DB) CancelQuery(ctx context.Context, pid int) error {
	var ok bool
	if err := d.pool.QueryRow(ctx, queryCancelBackend, pid).Scan(&ok); err != nil {
		return fmt.Errorf("cancelling query for pid %d: %w", pid, err)
	}
	if !ok {
		return fmt.Errorf("pg_cancel_backend(%d) returned false", pid)
	}
	return nil
}

// TerminateBackend sends pg_terminate_backend for the given PID.
func (d *DB) TerminateBackend(ctx context.Context, pid int) error {
	var ok bool
	if err := d.pool.QueryRow(ctx, queryTerminateBackend, pid).Scan(&ok); err != nil {
		return fmt.Errorf("terminating backend for pid %d: %w", pid, err)
	}
	if !ok {
		return fmt.Errorf("pg_terminate_backend(%d) returned false", pid)
	}
	return nil
}
