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
    a.pid,
    COALESCE(a.usename, '(system)'),
    COALESCE(a.application_name, ''),
    COALESCE(a.datname, ''),
    COALESCE(a.client_addr::text, ''),
    COALESCE(a.state, ''),
    a.backend_start,
    COALESCE(a.wait_event_type, ''),
    COALESCE(a.wait_event, ''),
    COALESCE(EXTRACT(EPOCH FROM (clock_timestamp() - a.query_start)), 0) AS duration_sec,
    a.query_start,
    COALESCE(a.query, ''),
    COALESCE(a.query_id, 0),
    COALESCE((SELECT bl.pid FROM pg_locks blocked
              JOIN pg_locks bl ON bl.locktype = blocked.locktype
                AND bl.database IS NOT DISTINCT FROM blocked.database
                AND bl.relation IS NOT DISTINCT FROM blocked.relation
                AND bl.page IS NOT DISTINCT FROM blocked.page
                AND bl.tuple IS NOT DISTINCT FROM blocked.tuple
                AND bl.pid <> blocked.pid
              WHERE blocked.pid = a.pid AND NOT blocked.granted
              LIMIT 1), 0) AS blocked_by
FROM pg_stat_activity a
WHERE a.pid <> pg_backend_pid()
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
    COALESCE(usename, '(system)'),
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

const queryCheckPgStatStatements = `SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements'`

const querySlowQueries = `
SELECT queryid, left(query, 200), calls, total_exec_time, mean_exec_time,
       rows,
       CASE WHEN shared_blks_hit + shared_blks_read = 0 THEN 0
            ELSE round(shared_blks_hit::numeric / (shared_blks_hit + shared_blks_read), 4)
       END AS hit_ratio
FROM pg_stat_statements
WHERE userid <> 0
ORDER BY total_exec_time DESC
LIMIT 50
`

const queryTransactions = `
SELECT a.pid, COALESCE(a.usename, '(system)'), COALESCE(a.application_name, ''),
       COALESCE(a.datname, ''),
       COALESCE(a.state, ''),
       a.backend_start,
       a.xact_start,
       COALESCE(EXTRACT(EPOCH FROM (now() - a.xact_start)), 0),
       COALESCE(EXTRACT(EPOCH FROM (now() - a.query_start)), 0),
       COALESCE(a.query, ''),
       (SELECT count(*) FROM pg_locks WHERE pid = a.pid) AS lock_count
FROM pg_stat_activity a
WHERE a.xact_start IS NOT NULL AND a.pid <> pg_backend_pid()
ORDER BY a.xact_start ASC
`

const queryLocks = `
SELECT blocked.pid, blocking.pid,
       COALESCE(ba.usename, ''), COALESCE(bka.usename, ''),
       COALESCE(ba.application_name, ''), COALESCE(bka.application_name, ''),
       blocked.locktype, blocked.mode,
       COALESCE(EXTRACT(EPOCH FROM (now() - ba.query_start)), 0),
       COALESCE(left(ba.query, 200), ''), COALESCE(left(bka.query, 200), '')
FROM pg_locks blocked
JOIN pg_locks blocking ON blocking.locktype = blocked.locktype
  AND blocking.database IS NOT DISTINCT FROM blocked.database
  AND blocking.relation IS NOT DISTINCT FROM blocked.relation
  AND blocking.page IS NOT DISTINCT FROM blocked.page
  AND blocking.tuple IS NOT DISTINCT FROM blocked.tuple
  AND blocking.pid <> blocked.pid
JOIN pg_stat_activity ba ON ba.pid = blocked.pid
JOIN pg_stat_activity bka ON bka.pid = blocking.pid
WHERE NOT blocked.granted
`

const queryIndexes = `
SELECT schemaname, relname, indexrelname, idx_scan, idx_tup_read, idx_tup_fetch,
       pg_relation_size(indexrelid)
FROM pg_stat_user_indexes
ORDER BY idx_scan ASC
`

const queryTableStats = `
SELECT
    schemaname,
    relname,
    pg_total_relation_size(quote_ident(schemaname) || '.' || quote_ident(relname)) AS total_size,
    n_live_tup,
    n_dead_tup,
    seq_scan,
    idx_scan,
    last_vacuum,
    last_autovacuum,
    last_analyze
FROM pg_stat_user_tables
WHERE schemaname = $1 AND relname = $2
`

const queryTableColumns = `
SELECT column_name, data_type, is_nullable, COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2
ORDER BY ordinal_position
`

const queryTablePrimaryKeys = `
SELECT a.attname
FROM pg_index i
JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
WHERE i.indrelid = (quote_ident($1) || '.' || quote_ident($2))::regclass
  AND i.indisprimary
`

const queryTableIndexes = `
SELECT indexrelname, idx_scan, pg_relation_size(indexrelid)
FROM pg_stat_user_indexes
WHERE schemaname = $1 AND relname = $2
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
func (d *DB) GetActiveQueries(ctx context.Context) ([]Query, error) {
	rows, err := d.pool.Query(ctx, queryActiveQueries)
	if err != nil {
		return nil, fmt.Errorf("querying active queries: %w", err)
	}
	defer rows.Close()

	var queries []Query
	for rows.Next() {
		var (
			q           Query
			durationSec float64
			queryStart  sql.NullTime
		)
		if err := rows.Scan(
			&q.PID,
			&q.User,
			&q.App,
			&q.Database,
			&q.ClientAddr,
			&q.State,
			&q.BackendStart,
			&q.WaitEventType,
			&q.WaitEvent,
			&durationSec,
			&queryStart,
			&q.QueryText,
			&q.QueryID,
			&q.BlockedBy,
		); err != nil {
			return nil, fmt.Errorf("scanning active query row: %w", err)
		}
		q.Duration = time.Duration(durationSec * float64(time.Second))
		if queryStart.Valid {
			q.QueryStart = queryStart.Time
		}
		q.Comment = ParseSQLComment(q.QueryText)
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
		if err := rows.Scan(&g.User, &g.App, &g.State, &g.Count); err != nil {
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

// GetSlowQueries returns the top 50 queries by total execution time from
// pg_stat_statements. Returns an empty slice if the extension is not installed.
func (d *DB) GetSlowQueries(ctx context.Context) ([]SlowQuery, error) {
	var exists int
	err := d.pool.QueryRow(ctx, queryCheckPgStatStatements).Scan(&exists)
	if err != nil {
		return []SlowQuery{}, nil
	}

	rows, err := d.pool.Query(ctx, querySlowQueries)
	if err != nil {
		return nil, fmt.Errorf("querying slow queries: %w", err)
	}
	defer rows.Close()

	var queries []SlowQuery
	for rows.Next() {
		var q SlowQuery
		if err := rows.Scan(
			&q.QueryID,
			&q.Query,
			&q.Calls,
			&q.TotalTime,
			&q.MeanTime,
			&q.Rows,
			&q.HitRatio,
		); err != nil {
			return nil, fmt.Errorf("scanning slow query row: %w", err)
		}
		queries = append(queries, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating slow query rows: %w", err)
	}
	return queries, nil
}

// GetTransactions returns all active transactions from pg_stat_activity.
func (d *DB) GetTransactions(ctx context.Context) ([]Transaction, error) {
	rows, err := d.pool.Query(ctx, queryTransactions)
	if err != nil {
		return nil, fmt.Errorf("querying transactions: %w", err)
	}
	defer rows.Close()

	var txns []Transaction
	for rows.Next() {
		var (
			t            Transaction
			xactSec      float64
			querySec     float64
		)
		if err := rows.Scan(
			&t.PID,
			&t.User,
			&t.App,
			&t.Database,
			&t.State,
			&t.BackendStart,
			&t.XactStart,
			&xactSec,
			&querySec,
			&t.QueryText,
			&t.LockCount,
		); err != nil {
			return nil, fmt.Errorf("scanning transaction row: %w", err)
		}
		t.XactDuration = time.Duration(xactSec * float64(time.Second))
		t.QueryDuration = time.Duration(querySec * float64(time.Second))
		txns = append(txns, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating transaction rows: %w", err)
	}
	return txns, nil
}

// GetLocks returns information about blocked locks and their blockers.
func (d *DB) GetLocks(ctx context.Context) ([]Lock, error) {
	rows, err := d.pool.Query(ctx, queryLocks)
	if err != nil {
		return nil, fmt.Errorf("querying locks: %w", err)
	}
	defer rows.Close()

	var locks []Lock
	for rows.Next() {
		var (
			l       Lock
			waitSec float64
		)
		if err := rows.Scan(
			&l.BlockedPID,
			&l.BlockingPID,
			&l.BlockedUser,
			&l.BlockingUser,
			&l.BlockedApp,
			&l.BlockingApp,
			&l.LockType,
			&l.Mode,
			&waitSec,
			&l.BlockedQuery,
			&l.BlockingQuery,
		); err != nil {
			return nil, fmt.Errorf("scanning lock row: %w", err)
		}
		l.WaitDuration = time.Duration(waitSec * float64(time.Second))
		locks = append(locks, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating lock rows: %w", err)
	}
	return locks, nil
}

// GetIndexes returns per-index statistics from pg_stat_user_indexes.
func (d *DB) GetIndexes(ctx context.Context) ([]IndexInfo, error) {
	rows, err := d.pool.Query(ctx, queryIndexes)
	if err != nil {
		return nil, fmt.Errorf("querying indexes: %w", err)
	}
	defer rows.Close()

	var indexes []IndexInfo
	for rows.Next() {
		var idx IndexInfo
		if err := rows.Scan(
			&idx.Schema,
			&idx.Table,
			&idx.IndexName,
			&idx.Scans,
			&idx.TupRead,
			&idx.TupFetch,
			&idx.Size,
		); err != nil {
			return nil, fmt.Errorf("scanning index row: %w", err)
		}
		indexes = append(indexes, idx)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating index rows: %w", err)
	}
	return indexes, nil
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

// GetTableDetail returns detailed information about a single table including
// columns, indexes, and statistics.
func (d *DB) GetTableDetail(ctx context.Context, schema, name string) (*TableDetail, error) {
	// Get table stats
	row := d.pool.QueryRow(ctx, queryTableStats, schema, name)
	var (
		td             TableDetail
		lastVacuum     sql.NullTime
		lastAutoVacuum sql.NullTime
		lastAnalyze    sql.NullTime
	)
	if err := row.Scan(
		&td.Schema, &td.Name, &td.TotalSize,
		&td.LiveTuples, &td.DeadTuples,
		&td.SeqScan, &td.IdxScan,
		&lastVacuum, &lastAutoVacuum, &lastAnalyze,
	); err != nil {
		return nil, fmt.Errorf("querying table stats for %s.%s: %w", schema, name, err)
	}
	if lastVacuum.Valid {
		td.LastVacuum = &lastVacuum.Time
	}
	if lastAutoVacuum.Valid {
		td.LastAutoVacuum = &lastAutoVacuum.Time
	}
	if lastAnalyze.Valid {
		td.LastAnalyze = &lastAnalyze.Time
	}

	// Get columns
	colRows, err := d.pool.Query(ctx, queryTableColumns, schema, name)
	if err != nil {
		return nil, fmt.Errorf("querying columns for %s.%s: %w", schema, name, err)
	}
	defer colRows.Close()

	for colRows.Next() {
		var (
			c          ColumnInfo
			isNullable string
		)
		if err := colRows.Scan(&c.Name, &c.DataType, &isNullable, &c.Default); err != nil {
			return nil, fmt.Errorf("scanning column row: %w", err)
		}
		c.Nullable = isNullable == "YES"
		td.Columns = append(td.Columns, c)
	}
	if err := colRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating column rows: %w", err)
	}

	// Get primary key columns
	pkRows, err := d.pool.Query(ctx, queryTablePrimaryKeys, schema, name)
	if err == nil {
		defer pkRows.Close()
		pkSet := make(map[string]bool)
		for pkRows.Next() {
			var attname string
			if err := pkRows.Scan(&attname); err == nil {
				pkSet[attname] = true
			}
		}
		for i := range td.Columns {
			if pkSet[td.Columns[i].Name] {
				td.Columns[i].IsPrimary = true
			}
		}
	}

	// Get indexes
	idxRows, err := d.pool.Query(ctx, queryTableIndexes, schema, name)
	if err != nil {
		return nil, fmt.Errorf("querying indexes for %s.%s: %w", schema, name, err)
	}
	defer idxRows.Close()

	for idxRows.Next() {
		var idx IndexInfo
		if err := idxRows.Scan(&idx.IndexName, &idx.Scans, &idx.Size); err != nil {
			return nil, fmt.Errorf("scanning index row: %w", err)
		}
		idx.Schema = schema
		idx.Table = name
		td.Indexes = append(td.Indexes, idx)
	}
	if err := idxRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating index rows: %w", err)
	}

	return &td, nil
}
