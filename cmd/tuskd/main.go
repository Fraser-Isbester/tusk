// Command tuskd is the Tusk daemon: it continuously polls PostgreSQL and
// evaluates the rules engine without any TUI dependencies. It reuses the same
// config, database, and rules packages as the tusk TUI, so a profile's rules
// behave identically whether enforced interactively or headless.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/fraser-isbester/tusk/internal/config"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/rules"
)

func main() {
	var profileFlag string
	var intervalFlag time.Duration

	rootCmd := &cobra.Command{
		Use:   "tuskd",
		Short: "Tusk daemon — continuously monitors PostgreSQL and enforces rules",
		Long:  "A lightweight daemon that polls PostgreSQL and evaluates rules without any TUI dependencies.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if intervalFlag <= 0 {
				return fmt.Errorf("interval must be positive, got %s", intervalFlag)
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			profile, err := cfg.ResolveProfile(profileFlag)
			if err != nil {
				return fmt.Errorf("resolving profile: %w", err)
			}

			profileName := profileFlag
			if profileName == "" {
				profileName = cfg.DefaultProfile
			}

			if len(profile.Rules) == 0 {
				return fmt.Errorf("no rules configured in profile %q", profileName)
			}

			database, err := db.New(profile.ConnectionString())
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer database.Close()

			compiled, err := rules.BuildRules(profile.Rules, profile.Readonly)
			if err != nil {
				return fmt.Errorf("compiling rules: %w", err)
			}

			engine := rules.NewEngine(compiled, database, 5*time.Minute, 1000)

			log.Printf("tuskd: profile=%s rules=%d interval=%s", profileName, engine.RuleCount(), intervalFlag)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return runDaemon(ctx, engine, database, intervalFlag)
		},
	}

	rootCmd.Flags().StringVarP(&profileFlag, "profile", "P", "", "connection profile name from config file")
	rootCmd.Flags().DurationVarP(&intervalFlag, "interval", "i", 2*time.Second, "polling interval")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// snapshotter fetches the live database state that the engine evaluates against.
// *db.DB satisfies it; a fake is used in tests. Keeping the daemon's evaluate
// loop behind this interface makes it testable without a real database.
type snapshotter interface {
	GetActiveQueries(ctx context.Context) ([]db.Query, error)
	GetTransactions(ctx context.Context) ([]db.Transaction, error)
	GetLocks(ctx context.Context) ([]db.Lock, error)
}

// runDaemon polls the database on interval and evaluates rules until ctx is
// canceled (the caller wires ctx to SIGINT/SIGTERM).
func runDaemon(ctx context.Context, engine *rules.Engine, source snapshotter, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	evaluate(ctx, engine, source)

	for {
		select {
		case <-ctx.Done():
			log.Println("tuskd: shutting down")
			return nil
		case <-ticker.C:
			evaluate(ctx, engine, source)
		}
	}
}

// evaluate fetches one snapshot and runs the engine against it. Fetch errors for
// a single resource are logged and that resource is skipped for the tick — a
// transient database blip should not crash the daemon.
func evaluate(ctx context.Context, engine *rules.Engine, source snapshotter) {
	queries, err := source.GetActiveQueries(ctx)
	if err != nil {
		log.Printf("tuskd: fetch queries: %v", err)
	}
	txns, err := source.GetTransactions(ctx)
	if err != nil {
		log.Printf("tuskd: fetch transactions: %v", err)
	}
	locks, err := source.GetLocks(ctx)
	if err != nil {
		log.Printf("tuskd: fetch locks: %v", err)
	}

	engine.Evaluate(ctx, rules.Snapshot{
		Queries:      queries,
		Transactions: txns,
		Locks:        locks,
	})
}
