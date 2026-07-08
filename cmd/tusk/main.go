package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/fraser-isbester/tusk/internal/config"
	"github.com/fraser-isbester/tusk/internal/connect"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui"
)

// session is a validated, live connection plus everything the TUI needs to
// render it. The closer tears down any tunnel and must run after db.Close.
type session struct {
	db          *db.DB
	closeTunnel func() error
	profileName string
	color       string
	connUser    string
	readonly    bool
	ruleConfigs []rules.RuleConfig
}

func main() {
	var profileFlag string

	rootCmd := &cobra.Command{
		Use:   "tusk [connection-string]",
		Short: "Tusk — a terminal UI for PostgreSQL administration",
		Long:  "A k9s-style terminal UI for real-time PostgreSQL management.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Try to bring up a connection from args/profile. If we can't
			// (unresolved profile, unreachable DB), drop into the setup screen
			// instead of exiting — get people into the TUI ASAP.
			sess, reason := establish(cfg, profileFlag, args)
			if sess == nil {
				res, ok := tui.RunSetup(cfg, reason)
				if !ok {
					return nil // user quit setup
				}
				sess = &session{
					db:          res.DB,
					closeTunnel: res.Closer,
					profileName: res.ProfileName,
					color:       res.Profile.Color,
					connUser:    res.Profile.User,
					readonly:    res.Profile.Readonly,
					ruleConfigs: res.Profile.Rules,
				}
			}
			defer sess.db.Close()
			defer func() { _ = sess.closeTunnel() }()

			// Query the actual connection user if the profile didn't set one.
			if sess.connUser == "" {
				var user string
				if err := sess.db.Pool().QueryRow(context.Background(), "SELECT current_user").Scan(&user); err == nil {
					sess.connUser = user
				}
			}

			var engine *rules.Engine
			if len(sess.ruleConfigs) > 0 {
				compiled, err := rules.BuildRules(sess.ruleConfigs, sess.readonly)
				if err != nil {
					return fmt.Errorf("compiling rules: %w", err)
				}
				engine = rules.NewEngine(compiled, sess.db, 5*time.Minute, 1000)
			}

			app := tui.NewApp(sess.db, cfg, sess.profileName, sess.color, sess.connUser, sess.readonly, engine)
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

// establish attempts to open and validate a connection. It returns a live
// session on success, or (nil, reason) when the caller should fall back to the
// interactive setup screen — reason explains why (shown to the user).
func establish(cfg *config.Config, profileFlag string, args []string) (*session, string) {
	if len(args) > 0 {
		return connectProfile(config.Profile{URL: args[0]}, "cli")
	}

	profile, err := cfg.ResolveProfile(profileFlag)
	if err != nil {
		return nil, err.Error()
	}
	name := profileFlag
	if name == "" {
		name = cfg.DefaultProfile
	}
	return connectProfile(profile, name)
}

// connectProfile opens any tunnel, builds the pool, and validates it with a
// ping. On any failure it tears everything down and returns a reason string so
// the caller can offer setup.
func connectProfile(profile config.Profile, name string) (*session, string) {
	dsn, closer, err := connect.Open(context.Background(), profile)
	if err != nil {
		return nil, err.Error()
	}
	database, err := db.New(dsn)
	if err != nil {
		_ = closer()
		return nil, err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := database.Pool().Ping(ctx); err != nil {
		database.Close()
		_ = closer()
		return nil, fmt.Sprintf("could not connect to %q: %s", name, err)
	}

	return &session{
		db:          database,
		closeTunnel: closer,
		profileName: name,
		color:       profile.Color,
		connUser:    profile.User,
		readonly:    profile.Readonly,
		ruleConfigs: profile.Rules,
	}, ""
}
