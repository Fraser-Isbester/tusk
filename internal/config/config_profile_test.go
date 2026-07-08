package config_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/config"
	"github.com/fraser-isbester/tusk/internal/rules"
)

var _ = Describe("SaveProfile", func() {
	var cfgPath string

	BeforeEach(func() {
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		cfgPath = filepath.Join(home, ".config", "tusk", "config.yaml")
	})

	read := func() string {
		data, err := os.ReadFile(cfgPath) //nolint:gosec // test-controlled temp path
		Expect(err).NotTo(HaveOccurred())
		return string(data)
	}

	It("creates a clean profile and round-trips through Load", func() {
		err := config.SaveProfile("staging", config.Profile{
			User:     "readonly",
			Database: "appdb",
			Connect: &config.ConnectConfig{
				Via:        "kube-port-forward",
				Context:    "gke_proj_staging",
				Namespace:  "databases",
				Target:     "svc/postgres",
				RemotePort: 5432,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		out := read()
		Expect(out).To(ContainSubstring("default_profile: staging"))
		Expect(out).To(ContainSubstring("via: kube-port-forward"))
		Expect(out).To(ContainSubstring("target: svc/postgres"))
		// omitempty keeps unset fields out of the file.
		Expect(out).NotTo(ContainSubstring("host:"))
		Expect(out).NotTo(ContainSubstring("password"))
		Expect(out).NotTo(ContainSubstring("readonly:"))
		Expect(out).NotTo(ContainSubstring("local_port"))

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		p, err := cfg.ResolveProfile("staging")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.User).To(Equal("readonly"))
		Expect(p.Connect).NotTo(BeNil())
		Expect(p.Connect.Via).To(Equal("kube-port-forward"))
		Expect(p.Connect.Target).To(Equal("svc/postgres"))
	})

	It("preserves comments and other profiles", func() {
		Expect(os.MkdirAll(filepath.Dir(cfgPath), 0o750)).To(Succeed())
		original := "# my config\ndefault_profile: dev\nprofiles:\n  dev:\n    url: \"postgres://localhost/dev\"\n"
		Expect(os.WriteFile(cfgPath, []byte(original), 0o600)).To(Succeed())

		Expect(config.SaveProfile("prod", config.Profile{URL: "postgres://prod/db"})).To(Succeed())

		out := read()
		Expect(out).To(ContainSubstring("# my config"))
		Expect(out).To(ContainSubstring("default_profile: dev")) // not overwritten
		Expect(out).To(ContainSubstring("postgres://localhost/dev"))
		Expect(out).To(ContainSubstring("postgres://prod/db"))
	})

	It("does not drop rules when re-saving connection details", func() {
		enabled := true
		Expect(config.SaveProfileRules("dev", []rules.RuleConfig{
			{Name: "r1", Resource: "query", When: "true", Action: "log", Enabled: &enabled},
		})).To(Succeed())

		// Re-save the profile with new connection info but no rules.
		Expect(config.SaveProfile("dev", config.Profile{URL: "postgres://new/db"})).To(Succeed())

		out := read()
		Expect(out).To(ContainSubstring("postgres://new/db"))
		Expect(out).To(ContainSubstring("name: r1")) // rules preserved

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		p, err := cfg.ResolveProfile("dev")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.Rules).To(HaveLen(1))
		Expect(p.URL).To(Equal("postgres://new/db"))
	})
})
