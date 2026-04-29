package rules

import (
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
)

// RuleConfig is the YAML representation of a rule.
type RuleConfig struct {
	Name     string `yaml:"name"`
	Enabled  *bool  `yaml:"enabled"`
	DryRun   bool   `yaml:"dry_run"`
	Resource string `yaml:"resource"`
	When     string `yaml:"when"`
	Action   string `yaml:"action"`
	Cooldown string `yaml:"cooldown"`
}

// BuildRules compiles rule configs into executable rules.
// If readonly is true, all rules are forced to dry-run mode.
func BuildRules(configs []RuleConfig, readonly bool) ([]Rule, error) {
	var rules []Rule
	for _, cfg := range configs {
		rt := ResourceType(cfg.Resource)
		env, err := EnvForResource(rt)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", cfg.Name, err)
		}

		ast, issues := env.Compile(cfg.When)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("rule %q: compiling expression: %w", cfg.Name, issues.Err())
		}

		// Verify the expression evaluates to bool
		if ast.OutputType() != cel.BoolType {
			return nil, fmt.Errorf("rule %q: expression must return bool, got %s", cfg.Name, ast.OutputType())
		}

		prg, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("rule %q: creating program: %w", cfg.Name, err)
		}

		action, err := resolveAction(cfg.Action)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", cfg.Name, err)
		}

		var cooldown time.Duration
		if cfg.Cooldown != "" {
			cooldown, err = time.ParseDuration(cfg.Cooldown)
			if err != nil {
				return nil, fmt.Errorf("rule %q: parsing cooldown: %w", cfg.Name, err)
			}
		}

		enabled := true
		if cfg.Enabled != nil {
			enabled = *cfg.Enabled
		}

		dryRun := cfg.DryRun || readonly

		rules = append(rules, Rule{
			Name:       cfg.Name,
			Enabled:    enabled,
			Resource:   rt,
			Program:    prg,
			Expression: cfg.When,
			Action:     action,
			Cooldown:   cooldown,
			DryRun:     dryRun,
		})
	}
	return rules, nil
}

func resolveAction(name string) (Executor, error) {
	switch name {
	case "terminate":
		return &TerminateAction{}, nil
	case "cancel":
		return &CancelAction{}, nil
	case "log":
		return &LogAction{}, nil
	default:
		return nil, fmt.Errorf("unknown action: %s", name)
	}
}
