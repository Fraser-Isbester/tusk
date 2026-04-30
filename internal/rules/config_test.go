package rules_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/rules"
)

var _ = Describe("BuildRules", func() {
	It("compiles a valid query rule", func() {
		configs := []rules.RuleConfig{
			{
				Name:     "slow-query",
				Resource: "query",
				When:     "duration > duration('5s')",
				Action:   "cancel",
				Cooldown: "30s",
			},
		}
		result, err := rules.BuildRules(configs, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Name).To(Equal("slow-query"))
		Expect(result[0].Enabled).To(BeTrue())
		Expect(result[0].Resource).To(Equal(rules.ResourceQuery))
		Expect(result[0].DryRun).To(BeFalse())
		Expect(result[0].Action.Name()).To(Equal("cancel"))
	})

	It("compiles a valid transaction rule", func() {
		configs := []rules.RuleConfig{
			{
				Name:     "long-tx",
				Resource: "transaction",
				When:     "xact_duration > duration('60s')",
				Action:   "log",
			},
		}
		result, err := rules.BuildRules(configs, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Resource).To(Equal(rules.ResourceTransaction))
	})

	It("compiles a valid lock rule", func() {
		configs := []rules.RuleConfig{
			{
				Name:     "long-lock",
				Resource: "lock",
				When:     "wait_duration > duration('10s')",
				Action:   "terminate",
			},
		}
		result, err := rules.BuildRules(configs, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Resource).To(Equal(rules.ResourceLock))
		Expect(result[0].Action.Name()).To(Equal("terminate"))
	})

	It("defaults enabled to true when not specified", func() {
		configs := []rules.RuleConfig{
			{Name: "r", Resource: "query", When: "true", Action: "log"},
		}
		result, err := rules.BuildRules(configs, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result[0].Enabled).To(BeTrue())
	})

	It("respects explicit enabled=false", func() {
		f := false
		configs := []rules.RuleConfig{
			{Name: "r", Enabled: &f, Resource: "query", When: "true", Action: "log"},
		}
		result, err := rules.BuildRules(configs, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result[0].Enabled).To(BeFalse())
	})

	It("forces dry-run when readonly is true", func() {
		configs := []rules.RuleConfig{
			{Name: "r", Resource: "query", When: "true", Action: "cancel"},
		}
		result, err := rules.BuildRules(configs, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(result[0].DryRun).To(BeTrue())
	})

	It("preserves dry-run from config even without readonly", func() {
		configs := []rules.RuleConfig{
			{Name: "r", DryRun: true, Resource: "query", When: "true", Action: "cancel"},
		}
		result, err := rules.BuildRules(configs, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result[0].DryRun).To(BeTrue())
	})

	It("returns an error for an unknown resource type", func() {
		configs := []rules.RuleConfig{
			{Name: "r", Resource: "bogus", When: "true", Action: "log"},
		}
		_, err := rules.BuildRules(configs, false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown resource type"))
	})

	It("returns an error for an invalid CEL expression", func() {
		configs := []rules.RuleConfig{
			{Name: "r", Resource: "query", When: "not_a_valid_expression(((", Action: "log"},
		}
		_, err := rules.BuildRules(configs, false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("compiling expression"))
	})

	It("returns an error when expression does not return bool", func() {
		configs := []rules.RuleConfig{
			{Name: "r", Resource: "query", When: "pid", Action: "log"},
		}
		_, err := rules.BuildRules(configs, false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must return bool"))
	})

	It("returns an error for an unknown action", func() {
		configs := []rules.RuleConfig{
			{Name: "r", Resource: "query", When: "true", Action: "explode"},
		}
		_, err := rules.BuildRules(configs, false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown action"))
	})

	It("returns an error for an invalid cooldown duration", func() {
		configs := []rules.RuleConfig{
			{Name: "r", Resource: "query", When: "true", Action: "log", Cooldown: "not-a-duration"},
		}
		_, err := rules.BuildRules(configs, false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("parsing cooldown"))
	})

	It("compiles multiple rules", func() {
		configs := []rules.RuleConfig{
			{Name: "r1", Resource: "query", When: "true", Action: "log"},
			{Name: "r2", Resource: "lock", When: "wait_duration > duration('1s')", Action: "terminate"},
		}
		result, err := rules.BuildRules(configs, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(2))
	})
})

var _ = Describe("EnvForResource", func() {
	It("returns an env for query resources", func() {
		env, err := rules.EnvForResource(rules.ResourceQuery)
		Expect(err).NotTo(HaveOccurred())
		Expect(env).NotTo(BeNil())
	})

	It("returns an env for transaction resources", func() {
		env, err := rules.EnvForResource(rules.ResourceTransaction)
		Expect(err).NotTo(HaveOccurred())
		Expect(env).NotTo(BeNil())
	})

	It("returns an env for lock resources", func() {
		env, err := rules.EnvForResource(rules.ResourceLock)
		Expect(err).NotTo(HaveOccurred())
		Expect(env).NotTo(BeNil())
	})

	It("returns an error for an unknown resource type", func() {
		_, err := rules.EnvForResource(rules.ResourceType("unknown"))
		Expect(err).To(HaveOccurred())
	})
})
