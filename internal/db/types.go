package db

import "time"

// ServerInfo holds high-level PostgreSQL server metadata.
type ServerInfo struct {
	Version        string
	Uptime         time.Duration
	MaxConnections int
}

// DatabaseStats holds activity counters for a single database.
type DatabaseStats struct {
	XactCommit    int64
	XactRollback  int64
	BlksHit       int64
	BlksRead      int64
	CacheHitRatio float64
}

// ActiveQuery represents a single in-flight backend from pg_stat_activity.
type ActiveQuery struct {
	PID           int
	User          string
	AppName       string
	ClientAddr    string
	State         string
	WaitEventType string
	WaitEvent     string
	Duration      time.Duration
	Query         string
	Comment       SQLComment
}

// ConnectionGroup is an aggregated view of connections sharing the same
// user, application, and state.
type ConnectionGroup struct {
	User    string
	AppName string
	State   string
	Count   int
}

// DatabaseInfo describes a single non-template database.
type DatabaseInfo struct {
	Name  string
	Size  int64
	Owner string
}

// RoleInfo describes a PostgreSQL role and its key privileges.
type RoleInfo struct {
	Name          string
	IsSuperuser   bool
	CanCreateRole bool
	CanCreateDB   bool
	CanLogin      bool
	ConnLimit     int
}

// TableInfo holds per-table statistics from pg_stat_user_tables.
type TableInfo struct {
	Schema         string
	Name           string
	TotalSize      int64
	LiveTuples     int64
	DeadTuples     int64
	SeqScan        int64
	IdxScan        int64
	LastVacuum     *time.Time
	LastAutoVacuum *time.Time
}

// SlowQuery holds a row from pg_stat_statements ordered by total execution time.
type SlowQuery struct {
	QueryID   int64
	Query     string
	Calls     int64
	TotalTime float64
	MeanTime  float64
	Rows      int64
	HitRatio  float64
}

// Transaction represents an active transaction from pg_stat_activity.
type Transaction struct {
	PID           int
	User          string
	AppName       string
	State         string
	XactDuration  time.Duration
	QueryDuration time.Duration
	Query         string
}

// LockInfo describes a blocked lock and the backend blocking it.
type LockInfo struct {
	BlockedPID    int
	BlockingPID   int
	BlockedUser   string
	BlockingUser  string
	BlockedApp    string
	BlockingApp   string
	LockType      string
	Mode          string
	WaitDuration  time.Duration
	BlockedQuery  string
	BlockingQuery string
}

// IndexInfo holds per-index statistics from pg_stat_user_indexes.
type IndexInfo struct {
	Schema    string
	Table     string
	IndexName string
	Scans     int64
	TupRead   int64
	TupFetch  int64
	Size      int64
}
