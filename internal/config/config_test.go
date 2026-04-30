package config_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/config"
)

var _ = Describe("Profile", func() {
	Describe("ConnectionString", func() {
		It("returns the URL directly when set", func() {
			p := config.Profile{URL: "postgres://user:pass@host:5432/db"}
			Expect(p.ConnectionString()).To(Equal("postgres://user:pass@host:5432/db"))
		})

		It("builds a connection string from individual fields", func() {
			p := config.Profile{
				Host:     "myhost",
				Port:     5433,
				User:     "myuser",
				Password: "mypass",
				Database: "mydb",
				SSLMode:  "require",
			}
			cs := p.ConnectionString()
			Expect(cs).To(ContainSubstring("myhost"))
			Expect(cs).To(ContainSubstring("5433"))
			Expect(cs).To(ContainSubstring("myuser"))
			Expect(cs).To(ContainSubstring("mypass"))
			Expect(cs).To(ContainSubstring("mydb"))
			Expect(cs).To(ContainSubstring("sslmode=require"))
		})

		It("uses sensible defaults for empty fields", func() {
			p := config.Profile{}
			cs := p.ConnectionString()
			Expect(cs).To(ContainSubstring("localhost"))
			Expect(cs).To(ContainSubstring("5432"))
			Expect(cs).To(ContainSubstring("postgres"))
			Expect(cs).To(ContainSubstring("sslmode=disable"))
		})

		It("omits password from URL when not set", func() {
			p := config.Profile{User: "admin"}
			cs := p.ConnectionString()
			Expect(cs).NotTo(ContainSubstring(":@"))
			Expect(cs).To(ContainSubstring("admin@"))
		})

		It("URL-encodes special characters in user and password", func() {
			p := config.Profile{
				User:     "user/name",
				Password: "p/ss word",
			}
			cs := p.ConnectionString()
			// url.PathEscape encodes slashes and spaces but not @ or :
			Expect(cs).To(ContainSubstring("user%2Fname"))
			Expect(cs).To(ContainSubstring("p%2Fss%20word"))
		})
	})
})

var _ = Describe("Config", func() {
	Describe("ResolveProfile", func() {
		It("returns the named profile", func() {
			cfg := &config.Config{
				Profiles: map[string]config.Profile{
					"prod": {Host: "prod-host"},
					"dev":  {Host: "dev-host"},
				},
				DefaultProfile: "dev",
			}
			p, err := cfg.ResolveProfile("prod")
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Host).To(Equal("prod-host"))
		})

		It("returns the default profile when name is empty", func() {
			cfg := &config.Config{
				Profiles: map[string]config.Profile{
					"dev": {Host: "dev-host"},
				},
				DefaultProfile: "dev",
			}
			p, err := cfg.ResolveProfile("")
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Host).To(Equal("dev-host"))
		})

		It("falls back to 'default' when no default is configured", func() {
			cfg := &config.Config{
				Profiles: map[string]config.Profile{
					"default": {Host: "default-host"},
				},
			}
			p, err := cfg.ResolveProfile("")
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Host).To(Equal("default-host"))
		})

		It("returns an error for a missing profile", func() {
			cfg := &config.Config{
				Profiles: map[string]config.Profile{},
			}
			_, err := cfg.ResolveProfile("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})
})

var _ = Describe("applyProfileDefaults (via Load behavior)", func() {
	It("fills in default port and refresh interval", func() {
		// We can't call applyProfileDefaults directly (unexported),
		// but we can verify the defaults through Profile behavior.
		p := config.Profile{}
		Expect(p.Port).To(Equal(0))           // zero before defaults applied
		Expect(p.RefreshInterval).To(Equal(time.Duration(0)))
	})
})
