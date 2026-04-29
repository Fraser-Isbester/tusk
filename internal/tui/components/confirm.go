package components

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConfirmResultMsg is sent when the user responds to a confirmation dialog.
type ConfirmResultMsg struct {
	Confirmed bool
	Context   interface{}
}

// ConfirmDialog is an overlay dialog that asks the user to confirm an action.
type ConfirmDialog struct {
	message string
	context interface{}
	active  bool
	width   int
	height  int
}

// NewConfirmDialog creates a new confirmation dialog with the given message
// and context. The context is returned in the ConfirmResultMsg so the caller
// can identify what was being confirmed.
func NewConfirmDialog(message string, context interface{}) ConfirmDialog {
	return ConfirmDialog{
		message: message,
		context: context,
		active:  true,
	}
}

// Active returns whether the dialog is currently visible.
func (c ConfirmDialog) Active() bool {
	return c.active
}

// Init implements tea.Model.
func (c ConfirmDialog) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (c ConfirmDialog) Update(msg tea.Msg) (ConfirmDialog, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
	case tea.KeyMsg:
		if !c.active {
			return c, nil
		}
		switch msg.String() {
		case "y", "Y":
			c.active = false
			return c, func() tea.Msg {
				return ConfirmResultMsg{Confirmed: true, Context: c.context}
			}
		case "n", "N", "esc", "enter":
			c.active = false
			return c, func() tea.Msg {
				return ConfirmResultMsg{Confirmed: false, Context: c.context}
			}
		}
	}
	return c, nil
}

// View implements tea.Model.
func (c ConfirmDialog) View() string {
	if !c.active {
		return ""
	}

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FFFF00")).
		Padding(1, 3).
		Align(lipgloss.Center)

	content := fmt.Sprintf("%s [y/N]", c.message)
	dialog := dialogStyle.Render(content)

	w := c.width
	h := c.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, dialog)
}
