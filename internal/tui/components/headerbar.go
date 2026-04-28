package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

type HeaderBar struct {
	width          int
	breadcrumb     []string
	version        string
	connections    int
	maxConnections int
}

func NewHeaderBar() HeaderBar {
	return HeaderBar{}
}

func (h *HeaderBar) SetBreadcrumb(parts []string) {
	h.breadcrumb = parts
}

func (h *HeaderBar) SetServerInfo(version string, connections int, maxConnections int) {
	h.version = version
	h.connections = connections
	h.maxConnections = maxConnections
}

func (h HeaderBar) Init() tea.Cmd { return nil }

func (h HeaderBar) Update(msg tea.Msg) (HeaderBar, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		h.width = msg.Width
	}
	return h, nil
}

func (h HeaderBar) View() string {
	w := h.width
	if w <= 0 {
		w = 80
	}

	logo := theme.Logo.Render(" Tusk ")
	crumb := ""
	if len(h.breadcrumb) > 0 {
		crumb = strings.Join(h.breadcrumb, " > ")
	}
	info := ""
	if h.version != "" {
		info = fmt.Sprintf("%s | %d/%d conns", h.version, h.connections, h.maxConnections)
	}

	logoW := lipgloss.Width(logo)
	infoW := len(info)
	gap := w - logoW - len(crumb) - infoW - 2
	if gap < 1 {
		gap = 1
	}

	line := logo + " " + crumb + strings.Repeat(" ", gap) + info
	return theme.Header.Width(w).Render(line)
}
