package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/evertras/bubble-table/table"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

const queriesRefreshInterval = 2 * time.Second

// queriesDataMsg carries fetched active query data.
type queriesDataMsg struct {
	queries []db.ActiveQuery
	err     error
}

// queriesTickMsg triggers the next fetch cycle.
type queriesTickMsg struct{}

// queriesStatusMsg displays a brief status message after an action.
type queriesStatusMsg struct {
	message string
}

// Queries is the active queries view.
type Queries struct {
	db          *db.DB
	table       table.Model
	width       int
	height      int
	paused      bool
	filterValue string
	userFilter  string // when set, only show queries from this user
	queries     []db.ActiveQuery
	statusMsg   string
	err         error
}

// NewQueries creates a new Queries view.
func NewQueries(database *db.DB) *Queries {
	cols := []table.Column{
		table.NewColumn("pid", "PID", 7),
		table.NewColumn("user", "USER", 10),
		table.NewFlexColumn("app", "APP", 2),
		table.NewColumn("state", "STATE", 8),
		table.NewFlexColumn("wait", "WAIT", 2),
		table.NewColumn("duration", "DURATION", 10),
	}
	t := table.New(cols).
		Focused(true).
		WithPageSize(20).
		Border(NoBorder).
		WithNoPagination().
		HeaderStyle(HeaderStyle).
		HighlightStyle(HighlightStyle).
		WithRowStyleFunc(func(input table.RowStyleFuncInput) lipgloss.Style {
			dur, _ := input.Row.Data["_dur"].(time.Duration)
			state, _ := input.Row.Data["_state"].(string)
			if state == "idle in transaction" || state == "idle in transaction (aborted)" {
				return lipgloss.NewStyle().Foreground(theme.ColorRed)
			}
			if dur >= 30*time.Second {
				return lipgloss.NewStyle().Foreground(theme.ColorRed)
			}
			if dur >= 1*time.Second {
				return lipgloss.NewStyle().Foreground(theme.ColorYellow)
			}
			return lipgloss.NewStyle()
		})
	return &Queries{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (q *Queries) SetSize(w, h int) {
	q.width = w
	q.height = h
	q.table = q.table.WithTargetWidth(w).WithPageSize(h - 2)
}

// ItemCount returns the number of queries.
func (q *Queries) ItemCount() int { return len(q.queries) }

// SetUserFilter pre-filters the view to only show queries from the given user.
func (q *Queries) SetUserFilter(user string) {
	q.userFilter = user
}

// SelectedQuery returns the active query matching the currently selected table row.
func (q *Queries) SelectedQuery() (db.ActiveQuery, bool) {
	pid, ok := q.selectedPID()
	if !ok {
		return db.ActiveQuery{}, false
	}
	for _, aq := range q.queries {
		if aq.PID == pid {
			return aq, true
		}
	}
	return db.ActiveQuery{}, false
}

// Init starts the first data fetch.
func (q *Queries) Init() tea.Cmd {
	return q.fetchData()
}

// Update handles messages.
func (q *Queries) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case queriesDataMsg:
		if msg.err != nil {
			q.err = msg.err
		} else {
			q.err = nil
			q.queries = msg.queries
			q.updateRows()
		}
		if !q.paused {
			return q, q.tick()
		}
		return q, nil

	case queriesTickMsg:
		if !q.paused {
			return q, q.fetchData()
		}
		return q, nil

	case queriesStatusMsg:
		q.statusMsg = msg.message
		return q, q.fetchData()

	case tea.KeyMsg:
		switch msg.String() {
		case "p":
			q.paused = !q.paused
			if !q.paused {
				return q, q.fetchData()
			}
			return q, nil
		case "c":
			return q, q.cancelSelected()
		case "t":
			return q, q.terminateSelected()
		}
	}

	var cmd tea.Cmd
	q.table, cmd = q.table.Update(msg)
	return q, cmd
}

// View renders the queries table.
func (q *Queries) View() string {
	if q.err != nil {
		return fmt.Sprintf("Error: %v", q.err)
	}

	return q.table.View()
}

func (q *Queries) updateRows() {
	var rows []table.Row
	for _, aq := range q.queries {
		// Hide system processes.
		if aq.User == "(system)" {
			continue
		}
		// Skip queries not matching user filter.
		if q.userFilter != "" && aq.User != q.userFilter {
			continue
		}
		pid := fmt.Sprintf("%d", aq.PID)
		wait := aq.WaitEventType
		if aq.WaitEvent != "" {
			wait += ":" + aq.WaitEvent
		}
		durStr := ""
		if aq.Duration > 0 {
			durStr = formatDuration(aq.Duration)
		}

		displayCols := []string{pid, aq.User, aq.AppName, aq.State, wait, durStr}
		if q.filterValue == "" || rowContains(displayCols, q.filterValue) {
			rows = append(rows, table.NewRow(table.RowData{
				"pid":      pid,
				"user":     aq.User,
				"app":      aq.AppName,
				"state":    aq.State,
				"wait":     wait,
				"duration": durStr,
				"_dur":     aq.Duration,
				"_state":   aq.State,
			}))
		}
	}
	q.table = q.table.WithRows(rows)
}

func (q *Queries) selectedPID() (int, bool) {
	row := q.table.HighlightedRow()
	pidVal, ok := row.Data["pid"]
	if !ok {
		return 0, false
	}
	pidStr, ok := pidVal.(string)
	if !ok || pidStr == "" {
		return 0, false
	}
	var pid int
	if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
		return 0, false
	}
	return pid, true
}

func (q *Queries) cancelSelected() tea.Cmd {
	pid, ok := q.selectedPID()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		err := q.db.CancelQuery(ctx, pid)
		if err != nil {
			return queriesStatusMsg{message: fmt.Sprintf("cancel pid %d failed: %v", pid, err)}
		}
		return queriesStatusMsg{message: fmt.Sprintf("cancelled pid %d", pid)}
	}
}

func (q *Queries) terminateSelected() tea.Cmd {
	pid, ok := q.selectedPID()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		err := q.db.TerminateBackend(ctx, pid)
		if err != nil {
			return queriesStatusMsg{message: fmt.Sprintf("terminate pid %d failed: %v", pid, err)}
		}
		return queriesStatusMsg{message: fmt.Sprintf("terminated pid %d", pid)}
	}
}

func (q *Queries) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		queries, err := q.db.GetActiveQueries(ctx)
		if err != nil {
			return queriesDataMsg{err: err}
		}
		return queriesDataMsg{queries: queries}
	}
}

func (q *Queries) tick() tea.Cmd {
	return tea.Tick(queriesRefreshInterval, func(time.Time) tea.Msg {
		return queriesTickMsg{}
	})
}
