package views

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fraser-isbester/tusk/internal/db"
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
		{Title: "BLOCKED", Width: 8},
		{Title: "BLOCKER", Width: 8},
		{Title: "TYPE", Width: 10},
		{Title: "MODE", Width: 14},
		{Title: "WAIT", Width: 8},
		{Title: "BLOCKER APP", Width: 14},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
	)
	t.SetStyles(defaultTableStyles())
	return &Locks{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Locks) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table.SetWidth(w)
	v.table.SetHeight(h - 2)
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
	return TableBorder.Render(v.table.View())
}

func (v *Locks) updateRows() {
	var rows []table.Row
	for _, l := range v.data {
		row := table.Row{
			fmt.Sprintf("%d", l.BlockedPID),
			fmt.Sprintf("%d", l.BlockingPID),
			l.LockType,
			l.Mode,
			formatDuration(l.WaitDuration),
			l.BlockingApp,
		}
		rows = append(rows, row)
	}
	v.table.SetRows(rows)
}

func (v *Locks) selectedBlockingPID() (int, bool) {
	row := v.table.SelectedRow()
	if row == nil {
		return 0, false
	}
	var pid int
	if _, err := fmt.Sscanf(row[1], "%d", &pid); err != nil {
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
