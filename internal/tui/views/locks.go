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

const locksRefreshInterval = 2 * time.Second

// locksDataMsg carries fetched lock data.
type locksDataMsg struct {
	locks []db.LockInfo
	err   error
}

// locksTickMsg triggers the next fetch cycle.
type locksTickMsg struct{}

// locksStatusMsg displays a brief status message after an action.
type locksStatusMsg struct {
	message string
}

// Locks is the lock contention view.
type Locks struct {
	db     *db.DB
	table  table.Model
	width  int
	height int
	paused bool
	data   []db.LockInfo
	err    error
}

// NewLocks creates a new Locks view.
func NewLocks(database *db.DB) *Locks {
	cols := []table.Column{
		table.NewColumn("blocked", "BLOCKED", 8),
		table.NewColumn("blocker", "BLOCKER", 8),
		table.NewColumn("type", "TYPE", 10),
		table.NewColumn("mode", "MODE", 12),
		table.NewColumn("wait", "WAIT", 8),
		table.NewFlexColumn("blocker_app", "BLOCKER APP", 1),
	}
	t := table.New(cols).
		Focused(true).
		WithPageSize(20).
		Border(NoBorder).
		WithNoPagination().
		HeaderStyle(HeaderStyle).
		HighlightStyle(HighlightStyle)
	return &Locks{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Locks) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table = v.table.WithTargetWidth(w).WithPageSize(h - 2)
}

// ItemCount returns the number of lock entries.
func (v *Locks) ItemCount() int { return len(v.data) }

// Init starts the first data fetch.
func (v *Locks) Init() tea.Cmd {
	return v.fetchData()
}

// Update handles messages.
func (v *Locks) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case locksDataMsg:
		if msg.err != nil {
			v.err = msg.err
		} else {
			v.err = nil
			v.data = msg.locks
			v.updateRows()
		}
		if !v.paused {
			return v, v.tick()
		}
		return v, nil

	case locksTickMsg:
		if !v.paused {
			return v, v.fetchData()
		}
		return v, nil

	case locksStatusMsg:
		return v, v.fetchData()

	case tea.KeyMsg:
		switch msg.String() {
		case "p":
			v.paused = !v.paused
			if !v.paused {
				return v, v.fetchData()
			}
			return v, nil
		case "t":
			return v, v.terminateBlocker()
		}
	}

	var cmd tea.Cmd
	v.table, cmd = v.table.Update(msg)
	return v, cmd
}

// View renders the locks table.
func (v *Locks) View() string {
	if v.err != nil {
		return fmt.Sprintf("Error: %v", v.err)
	}
	return v.table.View()
}

func (v *Locks) updateRows() {
	var rows []table.Row
	for _, l := range v.data {
		rows = append(rows, table.NewRow(table.RowData{
			"blocked":       fmt.Sprintf("%d", l.BlockedPID),
			"blocker":       fmt.Sprintf("%d", l.BlockingPID),
			"type":          l.LockType,
			"mode":          l.Mode,
			"wait":          formatDuration(l.WaitDuration),
			"blocker_app":   l.BlockingApp,
			"_blocking_pid": l.BlockingPID,
		}))
	}
	v.table = v.table.WithRows(rows).WithRowStyleFunc(func(input table.RowStyleFuncInput) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(theme.ColorYellow)
	})
}

func (v *Locks) selectedBlockingPID() (int, bool) {
	row := v.table.HighlightedRow()
	pid, ok := row.Data["_blocking_pid"].(int)
	if !ok {
		return 0, false
	}
	return pid, true
}

func (v *Locks) terminateBlocker() tea.Cmd {
	pid, ok := v.selectedBlockingPID()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		err := v.db.TerminateBackend(ctx, pid)
		if err != nil {
			return locksStatusMsg{message: fmt.Sprintf("terminate pid %d failed: %v", pid, err)}
		}
		return locksStatusMsg{message: fmt.Sprintf("terminated pid %d", pid)}
	}
}

func (v *Locks) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		locks, err := v.db.GetLocks(ctx)
		if err != nil {
			return locksDataMsg{err: err}
		}
		return locksDataMsg{locks: locks}
	}
}

func (v *Locks) tick() tea.Cmd {
	return tea.Tick(locksRefreshInterval, func(time.Time) tea.Msg {
		return locksTickMsg{}
	})
}
