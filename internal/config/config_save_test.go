package config_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/config"
	"github.com/fraser-isbester/tusk/internal/rules"
)

var _ = Describe("SaveProfileRules", func() {
	var (
		home    string
		cfgPath string
	)

	boolPtr := func(b bool) *bool { return &b }

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		cfgPath = filepath.Join(home, ".config", "tusk", "config.yaml")
	})

	readConfig := func() string {
		data, err := os.ReadFile(cfgPath) //nolint:gosec // test-controlled path under a temp HOME
		Expect(err).NotTo(HaveOccurred())
		return string(data)
	}

	It("creates the config file when it does not exist", func() {
		err := config.SaveProfileRules("dev", []rules.RuleConfig{
			{Name: "r1", Resource: "query", When: "true", Action: "log"},
		})
		Expect(err).NotTo(HaveOccurred())

		out := readConfig()
		Expect(out).To(ContainSubstring("default_profile: dev"))
		Expect(out).To(ContainSubstring("name: r1"))

		// Round-trips back through Load.
		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		p, err := cfg.ResolveProfile("dev")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.Rules).To(HaveLen(1))
		Expect(p.Rules[0].Name).To(Equal("r1"))
	})

	It("preserves comments and unrelated keys in an existing file", func() {
		Expect(os.MkdirAll(filepath.Dir(cfgPath), 0o750)).To(Succeed())
		original := `# my tusk config
default_profile: prod

profiles:
  prod:
    # production database
    url: "postgres://monitor@db.example.com:5432/prod?sslmode=require"
    readonly: true
    rules:
      - name: old-rule
        resource: query
        when: "true"
        action: log
`
		Expect(os.WriteFile(cfgPath, []byte(original), 0o600)).To(Succeed())

		err := config.SaveProfileRules("prod", []rules.RuleConfig{
			{Name: "new-rule", Resource: "transaction", When: "xact_duration > duration('5m')", Action: "terminate", Cooldown: "5m", DryRun: true},
		})
		Expect(err).NotTo(HaveOccurred())

		out := readConfig()
		Expect(out).To(ContainSubstring("# my tusk config"))
		Expect(out).To(ContainSubstring("# production database"))
		Expect(out).To(ContainSubstring(`url: "postgres://monitor@db.example.com:5432/prod?sslmode=require"`))
		Expect(out).To(ContainSubstring("readonly: true"))
		Expect(out).To(ContainSubstring("name: new-rule"))
		Expect(out).NotTo(ContainSubstring("old-rule")) // rules replaced, not appended
	})

	It("omits injected defaults and never writes an env-derived password", func() {
		// A password present in the environment must not leak into the file,
		// and applyProfileDefaults values (port, refresh_interval) must not be
		// baked in — SaveProfileRules only touches the rules subtree.
		GinkgoT().Setenv("PGPASSWORD", "supersecret")

		Expect(os.MkdirAll(filepath.Dir(cfgPath), 0o750)).To(Succeed())
		original := "default_profile: dev\nprofiles:\n  dev:\n    host: localhost\n"
		Expect(os.WriteFile(cfgPath, []byte(original), 0o600)).To(Succeed())

		err := config.SaveProfileRules("dev", []rules.RuleConfig{
			{Name: "r1", Resource: "query", When: "true", Action: "log", Enabled: boolPtr(true)},
		})
		Expect(err).NotTo(HaveOccurred())

		out := readConfig()
		Expect(out).NotTo(ContainSubstring("supersecret"))
		Expect(out).NotTo(ContainSubstring("password"))
		Expect(out).NotTo(ContainSubstring("port:"))
		Expect(out).NotTo(ContainSubstring("refresh_interval"))
		// enabled:true was explicitly set, so it is written; false/empty fields are omitted.
		Expect(out).To(ContainSubstring("enabled: true"))
		Expect(out).NotTo(ContainSubstring("dry_run"))
		Expect(out).NotTo(ContainSubstring("cooldown"))
	})

	It("adds, edits, and removes rules across saves", func() {
		Expect(config.SaveProfileRules("dev", []rules.RuleConfig{
			{Name: "a", Resource: "query", When: "true", Action: "log"},
			{Name: "b", Resource: "query", When: "false", Action: "log"},
		})).To(Succeed())

		// Replace with a single edited rule.
		Expect(config.SaveProfileRules("dev", []rules.RuleConfig{
			{Name: "a", Resource: "query", When: "duration > duration('1s')", Action: "cancel"},
		})).To(Succeed())

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		p, err := cfg.ResolveProfile("dev")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.Rules).To(HaveLen(1))
		Expect(p.Rules[0].Name).To(Equal("a"))
		Expect(p.Rules[0].Action).To(Equal("cancel"))
		Expect(strings.Contains(p.Rules[0].When, "duration")).To(BeTrue())
	})
})
