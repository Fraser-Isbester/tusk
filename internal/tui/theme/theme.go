package theme

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// k9s-inspired color palette
var (
	ColorLogo       = lipgloss.Color("#00D7FF") // cyan for the logo
	ColorHeaderBg   = lipgloss.Color("#5F5FD7") // purple header bg
	ColorHeaderFg   = lipgloss.Color("#FFFFFF")
	ColorCrumbsBg   = lipgloss.Color("#303030") // dark grey crumbs bar
	ColorCrumbsFg   = lipgloss.Color("#FFFFFF")
	ColorCrumbsHi   = lipgloss.Color("#00D7FF") // resource name highlight
	ColorStatusBg   = lipgloss.Color("#303030")
	ColorStatusFg   = lipgloss.Color("#AFAFAF")
	ColorHintKey    = lipgloss.Color("#00D7FF") // key hint color
	ColorHintLabel  = lipgloss.Color("#808080")
	ColorGreen      = lipgloss.Color("#00FF00")
	ColorYellow     = lipgloss.Color("#FFFF00")
	ColorRed        = lipgloss.Color("#FF0000")
	ColorDim        = lipgloss.Color("#585858")
	ColorFg         = lipgloss.Color("#D0D0D0")
	ColorSelectedBg = lipgloss.Color("#5F5FD7")
	ColorSelectedFg = lipgloss.Color("#FFFFFF")
)

// Header line: full-width purple bar
var Header = lipgloss.NewStyle().
	Background(ColorHeaderBg).
	Foreground(ColorHeaderFg)

// Logo text in header
var Logo = lipgloss.NewStyle().
	Background(ColorHeaderBg).
	Foreground(ColorLogo).
	Bold(true)

// Crumbs bar: the line below the header showing resource(count)
var Crumbs = lipgloss.NewStyle().
	Background(ColorCrumbsBg).
	Foreground(ColorCrumbsFg)

var CrumbsHighlight = lipgloss.NewStyle().
	Background(ColorCrumbsBg).
	Foreground(ColorCrumbsHi).
	Bold(true)

// Status bar at bottom
var Status = lipgloss.NewStyle().
	Background(ColorStatusBg).
	Foreground(ColorStatusFg)

// Key hints in status bar
var HintKey = lipgloss.NewStyle().
	Foreground(ColorHintKey).
	Bold(true)

var HintLabel = lipgloss.NewStyle().
	Foreground(ColorHintLabel)

// Table styles
var TableHeader = lipgloss.NewStyle().
	Foreground(ColorDim).
	Bold(true)

var TableRow = lipgloss.NewStyle().
	Foreground(ColorFg)

var TableSelected = lipgloss.NewStyle().
	Background(ColorSelectedBg).
	Foreground(ColorSelectedFg).
	Bold(true)

// Duration colors
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

// Profile color for status bar border/indicator
func ProfileColor(color string) lipgloss.Color {
	switch color {
	case "red":
		return lipgloss.Color("#FF0000")
	case "yellow":
		return lipgloss.Color("#FFFF00")
	case "green":
		return lipgloss.Color("#00FF00")
	default:
		return ColorDim
	}
}
