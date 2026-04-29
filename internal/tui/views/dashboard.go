package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

const dashboardRefreshInterval = 2 * time.Second

type dashboardDataMsg struct {
	serverInfo *db.ServerInfo
	stats      *db.DatabaseStats
	queries    []db.ActiveQuery
	conns      []db.ConnectionGroup
	err        error
}

type dashboardTickMsg struct{}

type Dashboard struct {
	db     *db.DB
	table  table.Model
	width  int
	height int
	paused bool

	serverInfo *db.ServerInfo
	stats      *db.DatabaseStats
	queries    []db.ActiveQuery
	conns      []db.ConnectionGroup
	err        error
}

func NewDashboard(database *db.DB) *Dashboard {
	cols := []table.Column{
		{Title: "PID", Width: 8},
		{Title: "USER", Width: 14},
		{Title: "DURATION", Width: 10},
		{Title: "STATE", Width: 12},
		{Title: "WAIT", Width: 16},
		{Title: "QUERY", Width: 50},
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
	)
	t.SetStyles(defaultTableStyles())

	return &Dashboard{db: database, table: t}
}

func (d *Dashboard) SetSize(w, h int) {
	d.width = w
	d.height = h
	d.table.SetWidth(w)
	// Reserve lines for the info section above the table
	tableH := h - 8
	if tableH < 3 {
		tableH = 3
	}
	d.table.SetHeight(tableH)
}

// QueryCount returns the number of active queries.
func (d *Dashboard) QueryCount() int { return len(d.queries) }

// SelectedQuery returns the active query matching the currently selected table row.
func (d *Dashboard) SelectedQuery() (db.ActiveQuery, bool) {
	row := d.table.SelectedRow()
	if row == nil {
		return db.ActiveQuery{}, false
	}
	var pid int
	if _, err := fmt.Sscanf(row[0], "%d", &pid); err != nil {
		return db.ActiveQuery{}, false
	}
	for _, q := range d.queries {
		if q.PID == pid {
			return q, true
		}
	}
	return db.ActiveQuery{}, false
}

func (d *Dashboard) Init() tea.Cmd {
	return d.fetchData()
}

func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case dashboardDataMsg:
		if msg.err != nil {
			d.err = msg.err
		} else {
			d.err = nil
			d.serverInfo = msg.serverInfo
			d.stats = msg.stats
			d.queries = msg.queries
			d.conns = msg.conns
			d.updateRows()
		}
		if !d.paused {
			return d, d.tick()
		}
		return d, nil

	case dashboardTickMsg:
		if !d.paused {
			return d, d.fetchData()
		}
		return d, nil

	case tea.KeyMsg:
		if msg.String() == "p" {
			d.paused = !d.paused
			if !d.paused {
				return d, d.fetchData()
			}
			return d, nil
		}
	}

	// Forward all other messages (including arrow keys) to the table.
	var cmd tea.Cmd
	d.table, cmd = d.table.Update(msg)
	return d, cmd
}

func (d *Dashboard) View() string {
	if d.err != nil {
		return fmt.Sprintf("  Error: %v", d.err)
	}

	label := lipgloss.NewStyle().Foreground(theme.ColorDim)
	value := lipgloss.NewStyle().Foreground(theme.ColorFg)
	green := lipgloss.NewStyle().Foreground(theme.ColorGreen)
	yellow := lipgloss.NewStyle().Foreground(theme.ColorYellow)
	red := lipgloss.NewStyle().Foreground(theme.ColorRed)

	var b strings.Builder

	// Server + connection info (compact, k9s-style key: value lines)
	if d.serverInfo != nil {
		ver := d.serverInfo.Version
		if idx := strings.Index(ver, ","); idx > 0 {
			ver = strings.TrimSpace(ver[:idx])
		}
		b.WriteString(fmt.Sprintf(" %s %s\n", label.Render("Server:"), value.Render(ver)))
		b.WriteString(fmt.Sprintf(" %s %s\n", label.Render("Uptime:"), value.Render(formatDuration(d.serverInfo.Uptime))))
	}

	if d.conns != nil {
		var active, idle, total int
		for _, c := range d.conns {
			total += c.Count
			switch c.State {
			case "active":
				active += c.Count
			case "idle":
				idle += c.Count
			}
		}
		maxConns := 0
		if d.serverInfo != nil {
			maxConns = d.serverInfo.MaxConnections
		}
		b.WriteString(fmt.Sprintf(" %s %s / %s / %s (max: %d)\n",
			label.Render("Conns:"),
			green.Render(fmt.Sprintf("%d active", active)),
			yellow.Render(fmt.Sprintf("%d idle", idle)),
			value.Render(fmt.Sprintf("%d total", total)),
			maxConns,
		))
	}

	if d.stats != nil {
		ratio := d.stats.CacheHitRatio * 100
		ratioStr := fmt.Sprintf("%.2f%%", ratio)
		var styled string
		switch {
		case ratio >= 99:
			styled = green.Render(ratioStr)
		case ratio >= 95:
			styled = yellow.Render(ratioStr)
		default:
			styled = red.Render(ratioStr)
		}
		b.WriteString(fmt.Sprintf(" %s %s   %s %s\n",
			label.Render("Cache:"),
			styled,
			label.Render("TPS:"),
			value.Render(fmt.Sprintf("%d", d.stats.XactCommit+d.stats.XactRollback)),
		))
	}

	b.WriteString("\n")

	// Active queries table
	b.WriteString(d.table.View())

	return b.String()
}

func (d *Dashboard) updateRows() {
	var rows []table.Row
	for _, q := range d.queries {
		durStr := formatDuration(q.Duration)
		switch {
		case q.Duration >= 5*time.Second:
			durStr = lipgloss.NewStyle().Foreground(theme.ColorRed).Render(durStr)
		case q.Duration >= time.Second:
			durStr = lipgloss.NewStyle().Foreground(theme.ColorYellow).Render(durStr)
		default:
			durStr = lipgloss.NewStyle().Foreground(theme.ColorGreen).Render(durStr)
		}

		wait := q.WaitEventType
		if q.WaitEvent != "" {
			wait += ":" + q.WaitEvent
		}

		queryPreview := truncate(strings.ReplaceAll(q.Query, "\n", " "), 60)

		rows = append(rows, table.Row{
			fmt.Sprintf("%d", q.PID),
			q.User,
			durStr,
			q.State,
			wait,
			queryPreview,
		})
	}
	d.table.SetRows(rows)
}

func (d *Dashboard) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		si, err := d.db.GetServerInfo(ctx)
		if err != nil {
			return dashboardDataMsg{err: err}
		}
		stats, err := d.db.GetDatabaseStats(ctx)
		if err != nil {
			return dashboardDataMsg{err: err}
		}
		queries, err := d.db.GetActiveQueries(ctx)
		if err != nil {
			return dashboardDataMsg{err: err}
		}
		conns, err := d.db.GetConnections(ctx)
		if err != nil {
			return dashboardDataMsg{err: err}
		}
		return dashboardDataMsg{
			serverInfo: si,
			stats:      stats,
			queries:    queries,
			conns:      conns,
		}
	}
}

func (d *Dashboard) tick() tea.Cmd {
	return tea.Tick(dashboardRefreshInterval, func(time.Time) tea.Msg {
		return dashboardTickMsg{}
	})
}
