package views

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/evertras/bubble-table/table"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

// NoBorder removes all table borders for a clean k9s-style look.
var NoBorder = table.Border{
	Top: " ", Bottom: " ", Left: " ", Right: " ",
	TopLeft: " ", TopRight: " ", BottomLeft: " ", BottomRight: " ",
	TopJunction: " ", BottomJunction: " ", LeftJunction: " ", RightJunction: " ",
	InnerJunction: " ", InnerDivider: " ",
}

var HeaderStyle = lipgloss.NewStyle().
	Foreground(theme.ColorTableHeader).
	Bold(true)

var HighlightStyle = lipgloss.NewStyle().
	Foreground(theme.ColorSelectedFg).
	Background(theme.ColorSelectedBg).
	Bold(true)
