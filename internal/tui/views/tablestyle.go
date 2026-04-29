package views

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

func defaultTableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(theme.ColorTableHeader).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(theme.ColorSelectedFg).
		Background(theme.ColorSelectedBg)
	return s
}

var TableBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(theme.ColorBorder)
