package config_test

import (
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/config"
)

var _ = Describe("Load", func() {
	var home string

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		for _, k := range []string{"DATABASE_URL", "PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSSLMODE"} {
			GinkgoT().Setenv(k, "") // envOrDefault treats empty as unset
		}
	})

	writeConfig := func(body string) {
		dir := filepath.Join(home, ".config", "tusk")
		Expect(os.MkdirAll(dir, 0o750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600)).To(Succeed())
	}

	Context("with no config file", func() {
		It("builds a single default profile with localhost defaults", func() {
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.DefaultProfile).To(Equal("default"))
			p, err := cfg.ResolveProfile("")
			Expect(err).NotTo(HaveOccurred())
			Expect(p.ConnectionString()).To(Equal("postgres://postgres@localhost:5432/postgres?sslmode=disable"))
			Expect(p.RefreshInterval).To(Equal(2 * time.Second))
		})

		It("honors libpq PG* variables", func() {
			GinkgoT().Setenv("PGHOST", "db.internal")
			GinkgoT().Setenv("PGPORT", "6543")
			GinkgoT().Setenv("PGUSER", "monitor")
			GinkgoT().Setenv("PGPASSWORD", "s3cret")
			GinkgoT().Setenv("PGDATABASE", "app")
			GinkgoT().Setenv("PGSSLMODE", "require")

			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			p, _ := cfg.ResolveProfile("")
			Expect(p.ConnectionString()).To(Equal("postgres://monitor:s3cret@db.internal:6543/app?sslmode=require"))
		})

		It("ignores a non-numeric PGPORT", func() {
			GinkgoT().Setenv("PGPORT", "not-a-port")
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Profiles["default"].Port).To(Equal(5432))
		})

		It("prefers DATABASE_URL over PG* variables", func() {
			GinkgoT().Setenv("PGHOST", "ignored")
			GinkgoT().Setenv("DATABASE_URL", "postgres://u@elsewhere:5432/x")
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Profiles["default"].ConnectionString()).To(Equal("postgres://u@elsewhere:5432/x"))
		})
	})

	Context("with a config file", func() {
		It("does not fall back to the environment", func() {
			GinkgoT().Setenv("DATABASE_URL", "postgres://env@host/db")
			writeConfig("profiles:\n  only:\n    host: filehost\n")
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Profiles).To(HaveLen(1))
			Expect(cfg.Profiles).To(HaveKey("only"))
		})

		It("applies port/refresh defaults and makes a lone profile the default", func() {
			writeConfig("profiles:\n  only:\n    host: filehost\n")
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.DefaultProfile).To(Equal("only"))
			p, err := cfg.ResolveProfile("")
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Port).To(Equal(5432))
			Expect(p.RefreshInterval).To(Equal(2 * time.Second))
		})

		It("does not guess a default among several profiles", func() {
			writeConfig("profiles:\n  a: {host: x}\n  b: {host: y}\n")
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.DefaultProfile).To(BeEmpty())
			_, err = cfg.ResolveProfile("")
			Expect(err).To(MatchError(ContainSubstring(`"default" not found`)))
		})

		It("parses rules and connect blocks", func() {
			writeConfig(`
default_profile: prod
profiles:
  prod:
    readonly: true
    refresh_interval: 5s
    connect:
      via: kube-port-forward
      namespace: db
      target: svc/postgres
    rules:
      - name: idle
        resource: transaction
        when: "xact_duration > duration('5m')"
        action: terminate
        cooldown: 1m
`)
			cfg, err := config.Load()
			Expect(err).NotTo(HaveOccurred())
			p := cfg.Profiles["prod"]
			Expect(p.Readonly).To(BeTrue())
			Expect(p.RefreshInterval).To(Equal(5 * time.Second))
			Expect(p.Connect).To(Equal(&config.ConnectConfig{Via: "kube-port-forward", Namespace: "db", Target: "svc/postgres"}))
			Expect(p.Rules).To(HaveLen(1))
			Expect(p.Rules[0].Cooldown).To(Equal("1m"))
		})

		It("reports a malformed file instead of silently using defaults", func() {
			writeConfig("profiles: [not, a, map]\n")
			_, err := config.Load()
			Expect(err).To(MatchError(ContainSubstring("parsing config")))
		})
	})
})

var _ = Describe("Profile.DSN", func() {
	// Whatever tusk builds must parse back to the same credentials in pgx;
	// passwords routinely contain URL-reserved characters.
	DescribeTable("round-trips credentials through pgx",
		func(user, password, database string) {
			p := config.Profile{User: user, Password: password, Database: database}
			cc, err := pgx.ParseConfig(p.DSN("db.example.com", 5432))
			Expect(err).NotTo(HaveOccurred())
			Expect(cc.User).To(Equal(user))
			Expect(cc.Password).To(Equal(password))
			Expect(cc.Database).To(Equal(database))
			Expect(cc.Host).To(Equal("db.example.com"))
		},
		Entry("plain", "monitor", "secret", "app"),
		Entry("@ and : in password", "monitor", "p@ss:w0rd", "app"),
		Entry("/ ? # % in password", "monitor", "a/b?c#d%e", "app"),
		Entry("space and + in password", "monitor", "two words+plus", "app"),
		Entry("special user", "svc@corp", "x", "app"),
	)

	DescribeTable("round-trips identifiers without a password",
		func(user, database string) {
			cc, err := pgx.ParseConfig(config.Profile{User: user, Database: database}.DSN("h", 5432))
			Expect(err).NotTo(HaveOccurred())
			Expect(cc.User).To(Equal(user))
			Expect(cc.Database).To(Equal(database))
		},
		Entry("user with /", "team/svc", "app"),
		Entry("database with ?", "monitor", "weird?db"),
	)
})
