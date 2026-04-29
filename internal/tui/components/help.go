package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpBinding represents a single key binding displayed in the help overlay.
type HelpBinding struct {
	Key         string
	Description string
}

// Help is an overlay that displays key bindings in a centered box.
type Help struct {
	bindings []HelpBinding
	visible  bool
	width    int
	height   int
}

// NewHelp creates a new Help component.
func NewHelp() Help {
	return Help{}
}

// SetBindings sets the key bindings to display.
func (h *Help) SetBindings(bindings []HelpBinding) {
	h.bindings = bindings
}

// Toggle switches the visibility of the help overlay.
func (h *Help) Toggle() {
	h.visible = !h.visible
}

// Visible returns whether the help overlay is currently shown.
func (h Help) Visible() bool {
	return h.visible
}

// Init implements tea.Model.
func (h Help) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (h Help) Update(msg tea.Msg) (Help, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width = msg.Width
		h.height = msg.Height
	case tea.KeyMsg:
		if h.visible {
			switch msg.String() {
			case "?", "esc", "q":
				h.visible = false
				return h, nil
			}
		}
	}
	return h, nil
}

// View implements tea.Model.
func (h Help) View() string {
	if !h.visible {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#D7D7FF")).
		Align(lipgloss.Center)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFF00")).
		Width(12).
		Align(lipgloss.Right)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		PaddingLeft(2)

	var lines []string
	lines = append(lines, titleStyle.Render("Key Bindings"))
	lines = append(lines, "")

	for _, b := range h.bindings {
		line := fmt.Sprintf("%s  %s",
			keyStyle.Render(b.Key),
			descStyle.Render(b.Description),
		)
		lines = append(lines, line)
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Faint(true).Render("Press ? or Esc to close"))

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5F5FD7")).
		Padding(1, 3)

	box := boxStyle.Render(content)

	w := h.width
	ht := h.height
	if w <= 0 {
		w = 80
	}
	if ht <= 0 {
		ht = 24
	}

	return lipgloss.Place(w, ht, lipgloss.Center, lipgloss.Center, box)
}
