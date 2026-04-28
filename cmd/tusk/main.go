package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/fraser-isbester/tusk/internal/config"
	"github.com/fraser-isbester/tusk/internal/db"
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
			var readonly bool

			if len(args) > 0 {
				// Direct connection string from CLI argument.
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
				readonly = profile.Readonly
			}

			database, err := db.New(connStr)
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer database.Close()

			app := tui.NewApp(database, profileName, profileColor, readonly)
			p := tea.NewProgram(app, tea.WithAltScreen())

			if _, err := p.Run(); err != nil {
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
