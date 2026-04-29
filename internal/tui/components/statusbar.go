package components

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

type Action struct {
	Key   string
	Label string
}

type StatusBar struct {
	width           int
	actions         []Action
	viewName        string
	count           int
	refreshInterval time.Duration
	paused          bool
	profileColor    string
}

func NewStatusBar() StatusBar {
	return StatusBar{}
}

func (s *StatusBar) SetActions(actions []Action)     { s.actions = actions }
func (s *StatusBar) SetProfileColor(color string)    { s.profileColor = color }
func (s *StatusBar) SetInfo(viewName string, count int, refreshInterval time.Duration, paused bool) {
	s.viewName = viewName
	s.count = count
	s.refreshInterval = refreshInterval
	s.paused = paused
}

func (s StatusBar) Init() tea.Cmd { return nil }

func (s StatusBar) Update(msg tea.Msg) (StatusBar, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		s.width = msg.Width
	}
	return s, nil
}

func (s StatusBar) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}

	var hints []string
	for _, a := range s.actions {
		hints = append(hints, fmt.Sprintf(
			"%s%s",
			theme.HintKey.Render("<"+a.Key+">"),
			theme.HintLabel.Render(a.Label),
		))
	}
	left := " " + strings.Join(hints, " ")

	refreshStr := fmt.Sprintf("every %s", s.refreshInterval)
	if s.paused {
		refreshStr = "paused"
	}
	right := fmt.Sprintf("%s | %d items | %s ", s.viewName, s.count, refreshStr)

	leftW := lipgloss.Width(left)
	rightW := len(right)
	gap := w - leftW - rightW
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + right
	return theme.Status.Width(w).Render(line)
}
