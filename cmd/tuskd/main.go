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
		RunE: func(cmd *cobra.Command, args []string) error {
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

			connStr := profile.ConnectionString()
			database, err := db.New(connStr)
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
			return runDaemon(engine, database, intervalFlag)
		},
	}

	rootCmd.Flags().StringVarP(&profileFlag, "profile", "P", "", "connection profile name from config file")
	rootCmd.Flags().DurationVarP(&intervalFlag, "interval", "i", 2*time.Second, "polling interval")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runDaemon(engine *rules.Engine, database *db.DB, interval time.Duration) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	evaluate(ctx, engine, database)

	for {
		select {
		case <-ctx.Done():
			log.Println("tuskd: shutting down")
			return nil
		case <-ticker.C:
			evaluate(ctx, engine, database)
		}
	}
}

func evaluate(ctx context.Context, engine *rules.Engine, database *db.DB) {
	queries, _ := database.GetActiveQueries(ctx)
	txns, _ := database.GetTransactions(ctx)
	locks, _ := database.GetLocks(ctx)
	engine.Evaluate(ctx, rules.Snapshot{
		Queries:      queries,
		Transactions: txns,
		Locks:        locks,
	})
}
