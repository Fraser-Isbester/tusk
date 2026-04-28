package components

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FilterMsg is sent when the user presses Enter to apply the filter.
type FilterMsg struct {
	Value string
}

// FilterCancelMsg is sent when the user presses Esc to cancel filtering.
type FilterCancelMsg struct{}

// Filter is a filter input overlay that appears at the bottom of the screen.
type Filter struct {
	input  textinput.Model
	active bool
	width  int
}

// NewFilter creates a new Filter component.
func NewFilter() Filter {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "filter..."
	ti.CharLimit = 256
	return Filter{
		input: ti,
	}
}

// Activate shows the filter input and focuses it.
func (f *Filter) Activate() {
	f.active = true
	f.input.SetValue("")
	f.input.Focus()
}

// Active returns true if the filter input is currently visible.
func (f Filter) Active() bool {
	return f.active
}

// Value returns the current filter text.
func (f Filter) Value() string {
	return f.input.Value()
}

// Init implements tea.Model.
func (f Filter) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (f Filter) Update(msg tea.Msg) (Filter, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.width = msg.Width
	case tea.KeyMsg:
		if !f.active {
			return f, nil
		}
		switch msg.Type {
		case tea.KeyEnter:
			value := f.input.Value()
			f.active = false
			f.input.Blur()
			return f, func() tea.Msg {
				return FilterMsg{Value: value}
			}
		case tea.KeyEscape:
			f.active = false
			f.input.Blur()
			return f, func() tea.Msg {
				return FilterCancelMsg{}
			}
		}
	}

	if f.active {
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		return f, cmd
	}

	return f, nil
}

// View implements tea.Model.
func (f Filter) View() string {
	if !f.active {
		return ""
	}

	w := f.width
	if w <= 0 {
		w = 80
	}

	style := lipgloss.NewStyle().
		Background(lipgloss.Color("#3C3C3C")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Width(w).
		Padding(0, 1)

	return style.Render(f.input.View())
}
