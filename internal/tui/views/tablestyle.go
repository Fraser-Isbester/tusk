package views

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

// defaultTableStyles returns k9s-inspired table styles.
func defaultTableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = lipgloss.NewStyle().
		Foreground(theme.ColorDim).
		Bold(true).
		Padding(0, 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorDim).
		BorderBottom(true)
	s.Selected = lipgloss.NewStyle().
		Foreground(theme.ColorSelectedFg).
		Background(theme.ColorSelectedBg).
		Padding(0, 1)
	s.Cell = lipgloss.NewStyle().
		Foreground(theme.ColorFg).
		Padding(0, 1)
	return s
}
