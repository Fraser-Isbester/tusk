package theme

import (
	"github.com/gdamore/tcell/v2"
)

// k9s-inspired color palette using tcell colors.
var (
	ColorLogo      = tcell.NewRGBColor(0x00, 0xD7, 0xFF) // cyan
	ColorLabel     = tcell.NewRGBColor(0xD7, 0x87, 0x00) // orange labels
	ColorValue     = tcell.ColorWhite
	ColorDim       = tcell.NewRGBColor(0x58, 0x58, 0x58)
	ColorFg        = tcell.NewRGBColor(0xD0, 0xD0, 0xD0)
	ColorGreen     = tcell.NewRGBColor(0x00, 0xD7, 0x00)
	ColorYellow    = tcell.NewRGBColor(0xFF, 0xD7, 0x00)
	ColorRed       = tcell.NewRGBColor(0xFF, 0x5F, 0x5F)
	ColorHeaderBg  = tcell.NewRGBColor(0x30, 0x30, 0x30)
	ColorBorder    = tcell.NewRGBColor(0x44, 0x44, 0x44)
	ColorSelectedBg = tcell.NewRGBColor(0x00, 0x5F, 0xAF)
	ColorSelectedFg = tcell.ColorWhite
	ColorTableHeader = tcell.NewRGBColor(0xD7, 0x87, 0x00) // orange headers

	// tcell styles
	SelectedStyle = tcell.StyleDefault.
		Foreground(ColorSelectedFg).
		Background(ColorSelectedBg).
		Attributes(tcell.AttrBold)
)
