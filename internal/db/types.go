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
