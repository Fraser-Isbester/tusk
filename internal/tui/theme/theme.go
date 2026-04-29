package theme

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// k9s-inspired color palette — brighter, more differentiated
var (
	ColorLogo      = lipgloss.Color("#00D7FF") // cyan
	ColorHeaderBg  = lipgloss.Color("#5F5FD7") // purple header bg
	ColorHeaderFg  = lipgloss.Color("#FFFFFF")
	ColorCrumbsBg  = lipgloss.Color("#303030")
	ColorCrumbsFg  = lipgloss.Color("#FFFFFF")
	ColorCrumbsHi  = lipgloss.Color("#00D7FF")
	ColorStatusBg  = lipgloss.Color("#303030")
	ColorStatusFg  = lipgloss.Color("#AFAFAF")
	ColorHintKey   = lipgloss.Color("#00D7FF")
	ColorHintLabel = lipgloss.Color("#808080")

	// Data colors — more distinct like k9s
	ColorLabel    = lipgloss.Color("#D78700") // orange for labels (like k9s "Context:")
	ColorValue    = lipgloss.Color("#FFFFFF") // bright white for values
	ColorGreen    = lipgloss.Color("#00D700") // slightly softer green
	ColorYellow   = lipgloss.Color("#FFD700") // gold-yellow
	ColorRed      = lipgloss.Color("#FF5F5F") // softer red
	ColorDim      = lipgloss.Color("#585858")
	ColorFg       = lipgloss.Color("#D0D0D0") // default text
	ColorFgBright = lipgloss.Color("#FFFFFF") // bright white

	// Table
	ColorTableHeader = lipgloss.Color("#D78700") // orange header like k9s
	ColorSelectedBg  = lipgloss.Color("#005FAF") // darker blue for selection (like k9s)
	ColorSelectedFg  = lipgloss.Color("#FFFFFF")
	ColorBorder      = lipgloss.Color("#444444")
)

var Header = lipgloss.NewStyle().
	Background(ColorHeaderBg).
	Foreground(ColorHeaderFg)

var Logo = lipgloss.NewStyle().
	Background(ColorHeaderBg).
	Foreground(ColorLogo).
	Bold(true)

var Crumbs = lipgloss.NewStyle().
	Background(ColorCrumbsBg).
	Foreground(ColorCrumbsFg)

var CrumbsHighlight = lipgloss.NewStyle().
	Background(ColorCrumbsBg).
	Foreground(ColorCrumbsHi).
	Bold(true)

var Status = lipgloss.NewStyle().
	Background(ColorStatusBg).
	Foreground(ColorStatusFg)

var HintKey = lipgloss.NewStyle().
	Foreground(ColorHintKey).
	Bold(true)

var HintLabel = lipgloss.NewStyle().
	Foreground(ColorHintLabel)

var TableHeader = lipgloss.NewStyle().
	Foreground(ColorTableHeader).
	Bold(true)

var TableRow = lipgloss.NewStyle().
	Foreground(ColorFg)

var TableSelected = lipgloss.NewStyle().
	Background(ColorSelectedBg).
	Foreground(ColorSelectedFg).
	Bold(true)

func DurationStyle(d time.Duration) lipgloss.Style {
	switch {
	case d < time.Second:
		return lipgloss.NewStyle().Foreground(ColorGreen)
	case d < 5*time.Second:
		return lipgloss.NewStyle().Foreground(ColorYellow)
	default:
		return lipgloss.NewStyle().Foreground(ColorRed)
	}
}

func ProfileColor(color string) lipgloss.Color {
	switch color {
	case "red":
		return ColorRed
	case "yellow":
		return ColorYellow
	case "green":
		return ColorGreen
	default:
		return ColorDim
	}
}
