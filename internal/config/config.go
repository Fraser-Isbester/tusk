package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/fraser-isbester/tusk/internal/rules"
)

// Config holds all configuration profiles for Tusk.
type Config struct {
	Profiles       map[string]Profile `yaml:"profiles"`
	DefaultProfile string             `yaml:"default_profile"`
}

// Profile represents a single PostgreSQL connection profile.
type Profile struct {
	Host            string             `yaml:"host"`
	Port            int                `yaml:"port"`
	User            string             `yaml:"user"`
	Password        string             `yaml:"password"`
	Database        string             `yaml:"database"`
	SSLMode         string             `yaml:"sslmode"`
	URL             string             `yaml:"url"`
	Readonly        bool               `yaml:"readonly"`
	Color           string             `yaml:"color"`
	RefreshInterval time.Duration      `yaml:"refresh_interval"`
	Rules           []rules.RuleConfig `yaml:"rules"`
}

// ConnectionString returns a PostgreSQL connection string for this profile.
// If URL is set directly, it is returned as-is. Otherwise, the string is
// assembled from the individual fields.
func (p Profile) ConnectionString() string {
	if p.URL != "" {
		return p.URL
	}

	host := p.Host
	if host == "" {
		host = "localhost"
	}
	port := p.Port
	if port == 0 {
		port = 5432
	}
	user := p.User
	if user == "" {
		user = "postgres"
	}
	db := p.Database
	if db == "" {
		db = "postgres"
	}
	sslmode := p.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}

	if p.Password != "" {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			url.PathEscape(user), url.PathEscape(p.Password), host, port, db, sslmode)
	}
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=%s",
		user, host, port, db, sslmode)
}

// configPath returns the default configuration file path (~/.config/tusk/config.yaml).
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".config", "tusk", "config.yaml"), nil
}

// Load reads the Tusk configuration. It tries, in order:
//  1. ~/.config/tusk/config.yaml
//  2. DATABASE_URL environment variable
//  3. PG* environment variables (PGHOST, PGPORT, PGUSER, PGPASSWORD, PGDATABASE)
//  4. Sensible defaults (localhost:5432, user postgres)
func Load() (*Config, error) {
	// Try config file first.
	cfgPath, err := configPath()
	if err == nil {
		data, readErr := os.ReadFile(cfgPath) //nolint:gosec // path from configPath(), not user input
		if readErr == nil {
			cfg := &Config{}
			if parseErr := yaml.Unmarshal(data, cfg); parseErr != nil {
				return nil, fmt.Errorf("parsing config %s: %w", cfgPath, parseErr)
			}
			applyProfileDefaults(cfg)
			return cfg, nil
		}
	}

	// Fall back to environment / defaults.
	profile := profileFromEnv()
	cfg := &Config{
		Profiles:       map[string]Profile{"default": profile},
		DefaultProfile: "default",
	}
	return cfg, nil
}

// profileFromEnv builds a Profile from environment variables, falling back to
// sensible defaults when the variables are not set.
func profileFromEnv() Profile {
	p := Profile{
		Host:            envOrDefault("PGHOST", "localhost"),
		User:            envOrDefault("PGUSER", "postgres"),
		Password:        os.Getenv("PGPASSWORD"),
		Database:        envOrDefault("PGDATABASE", "postgres"),
		SSLMode:         envOrDefault("PGSSLMODE", "disable"),
		Port:            5432,
		RefreshInterval: 2 * time.Second,
	}

	if url := os.Getenv("DATABASE_URL"); url != "" {
		p.URL = url
	}

	if portStr := os.Getenv("PGPORT"); portStr != "" {
		if v, err := strconv.Atoi(portStr); err == nil {
			p.Port = v
		}
	}

	return p
}

// ResolveProfile returns the named profile from the config. If name is empty,
// the default profile is used. If no profiles are configured, a profile is
// built from environment variables and defaults.
func (c *Config) ResolveProfile(name string) (Profile, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		name = "default"
	}
	p, ok := c.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("profile %q not found", name)
	}
	return p, nil
}

// applyProfileDefaults fills in zero-value fields with sensible defaults for
// every profile in the config.
func applyProfileDefaults(cfg *Config) {
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	for name, p := range cfg.Profiles {
		if p.Port == 0 {
			p.Port = 5432
		}
		if p.RefreshInterval == 0 {
			p.RefreshInterval = 2 * time.Second
		}
		cfg.Profiles[name] = p
	}
	if cfg.DefaultProfile == "" && len(cfg.Profiles) > 0 {
		// If there is exactly one profile, make it the default.
		if len(cfg.Profiles) == 1 {
			for k := range cfg.Profiles {
				cfg.DefaultProfile = k
			}
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
