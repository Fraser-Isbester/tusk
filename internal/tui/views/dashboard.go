package views

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fraser-isbester/tusk/internal/db"
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

	// TPS tracking — delta between snapshots
	prevXactTotal int64
	prevTime      time.Time
	tps           int64
}

func NewDashboard(database *db.DB) *Dashboard {
	cols := []table.Column{
		{Title: "PID", Width: 6},
		{Title: "USER", Width: 10},
		{Title: "APP", Width: 18},
		{Title: "STATE", Width: 8},
		{Title: "WAIT", Width: 18},
		{Title: "DURATION", Width: 8},
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
	tableH := h - 2
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

			// Compute TPS as delta between snapshots.
			if d.stats != nil {
				now := time.Now()
				total := d.stats.XactCommit + d.stats.XactRollback
				if !d.prevTime.IsZero() {
					elapsed := now.Sub(d.prevTime).Seconds()
					if elapsed > 0 {
						d.tps = int64(float64(total-d.prevXactTotal) / elapsed)
					}
				}
				d.prevXactTotal = total
				d.prevTime = now
			}

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

	return TableBorder.Render(d.table.View())
}

func (d *Dashboard) updateRows() {
	var rows []table.Row
	for _, q := range d.queries {
		durStr := ""
		if q.Duration > 0 {
			durStr = formatDuration(q.Duration)
		}

		wait := q.WaitEventType
		if q.WaitEvent != "" {
			wait += ":" + q.WaitEvent
		}

		stateStr := q.State

		rows = append(rows, table.Row{
			fmt.Sprintf("%d", q.PID),
			q.User,
			q.AppName,
			stateStr,
			wait,
			durStr,
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
