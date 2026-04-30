package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/fraser-isbester/tusk/internal/config"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui"
)

func main() {
	var profileFlag string

	rootCmd := &cobra.Command{
		Use:   "tusk [connection-string]",
		Short: "Tusk — a terminal UI for PostgreSQL administration",
		Long:  "A k9s-style terminal UI for real-time PostgreSQL management.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			var connStr string
			var profileName string
			var profileColor string
			var connUser string
			var readonly bool
			var ruleConfigs []rules.RuleConfig

			if len(args) > 0 {
				connStr = args[0]
				profileName = "cli"
			} else {
				profile, err := cfg.ResolveProfile(profileFlag)
				if err != nil {
					return fmt.Errorf("resolving profile: %w", err)
				}
				connStr = profile.ConnectionString()
				profileName = profileFlag
				if profileName == "" {
					profileName = cfg.DefaultProfile
				}
				profileColor = profile.Color
				connUser = profile.User
				readonly = profile.Readonly
				ruleConfigs = profile.Rules
			}

			database, err := db.New(connStr)
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer database.Close()

			// Query actual connection user if not set from profile
			if connUser == "" {
				var user string
				if err := database.Pool().QueryRow(context.Background(), "SELECT current_user").Scan(&user); err == nil {
					connUser = user
				}
			}

			var engine *rules.Engine
			if len(ruleConfigs) > 0 {
				compiled, err := rules.BuildRules(ruleConfigs, readonly)
				if err != nil {
					return fmt.Errorf("compiling rules: %w", err)
				}
				engine = rules.NewEngine(compiled, database, 5*time.Minute, 1000)
			}

			app := tui.NewApp(database, profileName, profileColor, connUser, readonly, engine)
			if err := app.Run(); err != nil {
				return fmt.Errorf("running tusk: %w", err)
			}
			return nil
		},
	}

	rootCmd.Flags().StringVarP(&profileFlag, "profile", "P", "", "connection profile name from config file")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
