package tui

import "strings"

// Command represents a colon-command with a canonical name, aliases, and the
// view it maps to.
type Command struct {
	Name     string
	Aliases  []string
	ViewName string
}

// CommandRegistry holds all registered colon-commands.
type CommandRegistry struct {
	commands []Command
}

// NewCommandRegistry creates a CommandRegistry populated with the MVP commands.
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: []Command{
			{Name: "queries", Aliases: []string{"query", "q"}, ViewName: "queries"},
			{Name: "tables", Aliases: []string{"table", "tbl"}, ViewName: "tables"},
			{Name: "connections", Aliases: []string{"conn", "conns"}, ViewName: "connections"},
			{Name: "db", Aliases: []string{"database", "databases"}, ViewName: "db"},
			{Name: "roles", Aliases: []string{"role", "users"}, ViewName: "roles"},
			{Name: "slow", Aliases: []string{"slowqueries"}, ViewName: "slow"},
			{Name: "transactions", Aliases: []string{"txn", "tx"}, ViewName: "transactions"},
			{Name: "locks", Aliases: []string{"lock"}, ViewName: "locks"},
			{Name: "indexes", Aliases: []string{"idx", "index"}, ViewName: "indexes"},
		},
	}
}

// Match performs a fuzzy-prefix match on the input against all command names
// and aliases. It returns the view name of the matched command and true if a
// unique match is found. If the input is ambiguous or matches nothing, it
// returns ("", false).
//
// Examples:
//
//	"q"    -> ("queries", true)
//	"t"    -> ("tables", true)
//	"dash" -> ("dashboard", true)
func (r *CommandRegistry) Match(input string) (string, bool) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return "", false
	}

	var matched *Command
	matchCount := 0

	for i := range r.commands {
		cmd := &r.commands[i]
		if matchesPrefix(cmd.Name, input) {
			matched = cmd
			matchCount++
			continue
		}
		for _, alias := range cmd.Aliases {
			if matchesPrefix(alias, input) {
				matched = cmd
				matchCount++
				break
			}
		}
	}

	// Deduplicate: multiple aliases of the same command should count as one match.
	// Re-check with uniqueness by view name.
	if matchCount > 1 {
		seen := make(map[string]struct{})
		for i := range r.commands {
			cmd := &r.commands[i]
			if matchesPrefix(cmd.Name, input) {
				seen[cmd.ViewName] = struct{}{}
			}
			for _, alias := range cmd.Aliases {
				if matchesPrefix(alias, input) {
					seen[cmd.ViewName] = struct{}{}
					break
				}
			}
		}
		if len(seen) == 1 {
			return matched.ViewName, true
		}
		return "", false
	}

	if matchCount == 1 {
		return matched.ViewName, true
	}

	return "", false
}

// matchesPrefix returns true if candidate starts with prefix (case-insensitive).
func matchesPrefix(candidate, prefix string) bool {
	return len(candidate) >= len(prefix) && strings.EqualFold(candidate[:len(prefix)], prefix)
}
