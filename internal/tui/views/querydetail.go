package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

// QueryDetail shows full details for a single active query.
// It is a read-only, static snapshot view with the full query text
// rendered in a scrollable viewport.
type QueryDetail struct {
	query    db.ActiveQuery
	viewport viewport.Model
	width    int
	height   int
}

// NewQueryDetail creates a new QueryDetail view for the given query.
func NewQueryDetail(q db.ActiveQuery) *QueryDetail {
	vp := viewport.New(80, 20)
	vp.SetContent(q.Query)

	return &QueryDetail{
		query:    q,
		viewport: vp,
	}
}

// Init satisfies tea.Model. No commands are needed for a static view.
func (qd *QueryDetail) Init() tea.Cmd {
	return nil
}

// Update handles key messages for viewport scrolling.
func (qd *QueryDetail) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	qd.viewport, cmd = qd.viewport.Update(msg)
	return qd, cmd
}

// SetSize updates the available terminal dimensions and recalculates the
// viewport size after accounting for the metadata header.
func (qd *QueryDetail) SetSize(w, h int) {
	qd.width = w
	qd.height = h

	// The header section (metadata + separator) uses a variable number of lines.
	headerLines := 10 // 8 fields + blank line + separator line
	if qd.query.Comment.App != "" {
		for _, v := range []string{qd.query.Comment.App, qd.query.Comment.Route, qd.query.Comment.Controller, qd.query.Comment.Action} {
			if v != "" {
				headerLines++
			}
		}
	}
	vpHeight := h - headerLines
	if vpHeight < 1 {
		vpHeight = 1
	}
	qd.viewport.Width = w
	qd.viewport.Height = vpHeight
	qd.viewport.SetContent(qd.query.Query)
}

// View renders the full query detail layout.
func (qd *QueryDetail) View() string {
	labelStyle := lipgloss.NewStyle().Foreground(theme.ColorDim)
	valueStyle := lipgloss.NewStyle().Foreground(theme.ColorFg)
	separatorStyle := lipgloss.NewStyle().Foreground(theme.ColorCrumbsHi)
	durationStyle := theme.DurationStyle(qd.query.Duration)

	// Format wait event
	waitEvent := qd.query.WaitEventType
	if qd.query.WaitEvent != "" {
		if waitEvent != "" {
			waitEvent += ":" + qd.query.WaitEvent
		} else {
			waitEvent = qd.query.WaitEvent
		}
	}
	if waitEvent == "" {
		waitEvent = "-"
	}

	// Build metadata rows
	const labelWidth = 14
	meta := []struct{ label, value string }{
		{"PID:", fmt.Sprintf("%d", qd.query.PID)},
		{"User:", qd.query.User},
		{"Application:", qd.query.AppName},
		{"Client:", qd.query.ClientAddr},
		{"State:", qd.query.State},
		{"Wait Event:", waitEvent},
	}

	var b strings.Builder
	for _, m := range meta {
		lbl := labelStyle.Render(fmt.Sprintf("%-*s", labelWidth, m.label))
		val := valueStyle.Render(m.value)
		b.WriteString(lbl + val + "\n")
	}

	// Duration gets its own color
	durLabel := labelStyle.Render(fmt.Sprintf("%-*s", labelWidth, "Duration:"))
	durValue := durationStyle.Render(formatDuration(qd.query.Duration))
	b.WriteString(durLabel + durValue + "\n")

	// SQLcommentor metadata (if present)
	if qd.query.Comment.App != "" {
		commentFields := []struct{ label, value string }{
			{"App:", qd.query.Comment.App},
			{"Route:", qd.query.Comment.Route},
			{"Controller:", qd.query.Comment.Controller},
			{"Action:", qd.query.Comment.Action},
		}
		for _, cf := range commentFields {
			if cf.value != "" {
				lbl := labelStyle.Render(fmt.Sprintf("%-*s", labelWidth, cf.label))
				val := valueStyle.Render(cf.value)
				b.WriteString(lbl + val + "\n")
			}
		}
	}

	// Blank line before separator
	b.WriteString("\n")

	// Separator line
	sepWidth := qd.width
	if sepWidth < 20 {
		sepWidth = 40
	}
	title := " Query "
	dashCount := sepWidth - len(title) - 3 // 3 leading dashes
	if dashCount < 3 {
		dashCount = 3
	}
	separator := strings.Repeat("\u2500", 3) + title + strings.Repeat("\u2500", dashCount)
	b.WriteString(separatorStyle.Render(separator) + "\n")

	// Scrollable query text
	b.WriteString(qd.viewport.View())

	return b.String()
}
