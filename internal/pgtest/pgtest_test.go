package pgtest

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestWithDatabaseOverridesQueryParams(t *testing.T) {
	for _, dsn := range []string{
		"postgres://u:p@localhost:5432/dev?sslmode=disable",
		"postgres://u:p@localhost:5432/postgres?dbname=dev&sslmode=disable",
		"postgresql://u:p@localhost/?database=dev",
	} {
		got, err := WithDatabase(dsn, "tusk_test_x")
		if err != nil {
			t.Fatalf("%s: %v", dsn, err)
		}
		cfg, err := pgx.ParseConfig(got)
		if err != nil {
			t.Fatalf("%s -> %s: %v", dsn, got, err)
		}
		if cfg.Database != "tusk_test_x" {
			t.Errorf("%s -> %s: database %q, want tusk_test_x", dsn, got, cfg.Database)
		}
	}
}
