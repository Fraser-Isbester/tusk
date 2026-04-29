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

const transactionsRefreshInterval = 2 * time.Second

// transactionsDataMsg carries fetched transaction data.
type transactionsDataMsg struct {
	transactions []db.Transaction
	err          error
}

// transactionsTickMsg triggers the next fetch cycle.
type transactionsTickMsg struct{}

// transactionsStatusMsg displays a brief status message after an action.
type transactionsStatusMsg struct {
	message string
}

// Transactions is the transaction monitor view.
type Transactions struct {
	db     *db.DB
	table  table.Model
	width  int
	height int
	paused bool
	data   []db.Transaction
	err    error
}

// NewTransactions creates a new Transactions view.
func NewTransactions(database *db.DB) *Transactions {
	cols := []table.Column{
		table.NewColumn("pid", "PID", 7),
		table.NewColumn("user", "USER", 10),
		table.NewFlexColumn("app", "APP", 2),
		table.NewColumn("state", "STATE", 8),
		table.NewColumn("txn_age", "TXN AGE", 10),
		table.NewColumn("q_age", "Q AGE", 10),
	}
	t := table.New(cols).
		Focused(true).
		WithPageSize(20).
		Border(NoBorder).
		WithNoPagination().
		HeaderStyle(HeaderStyle).
		HighlightStyle(HighlightStyle)
	return &Transactions{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Transactions) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table = v.table.WithTargetWidth(w).WithPageSize(h - 2)
}

// ItemCount returns the number of transactions.
func (v *Transactions) ItemCount() int { return len(v.data) }

// SelectedQuery returns an ActiveQuery built from the selected transaction row.
func (v *Transactions) SelectedQuery() (db.ActiveQuery, bool) {
	row := v.table.HighlightedRow()
	pid, ok := row.Data["_pid"].(int)
	if !ok {
		return db.ActiveQuery{}, false
	}
	query, _ := row.Data["_query"].(string)
	user, _ := row.Data["user"].(string)
	app, _ := row.Data["app"].(string)
	state, _ := row.Data["_state"].(string)
	return db.ActiveQuery{
		PID:     pid,
		User:    user,
		AppName: app,
		State:   state,
		Query:   query,
	}, true
}

// Init starts the first data fetch.
func (v *Transactions) Init() tea.Cmd {
	return v.fetchData()
}

// Update handles messages.
func (v *Transactions) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case transactionsDataMsg:
		if msg.err != nil {
			v.err = msg.err
		} else {
			v.err = nil
			v.data = msg.transactions
			v.updateRows()
		}
		if !v.paused {
			return v, v.tick()
		}
		return v, nil

	case transactionsTickMsg:
		if !v.paused {
			return v, v.fetchData()
		}
		return v, nil

	case transactionsStatusMsg:
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
			return v, v.terminateSelected()
		}
	}

	var cmd tea.Cmd
	v.table, cmd = v.table.Update(msg)
	return v, cmd
}

// View renders the transactions table.
func (v *Transactions) View() string {
	if v.err != nil {
		return fmt.Sprintf("Error: %v", v.err)
	}
	return v.table.View()
}

func (v *Transactions) updateRows() {
	var rows []table.Row
	for _, txn := range v.data {
		if txn.User == "(system)" {
			continue
		}
		rows = append(rows, table.NewRow(table.RowData{
			"pid":       fmt.Sprintf("%d", txn.PID),
			"user":      txn.User,
			"app":       txn.AppName,
			"state":     txn.State,
			"txn_age":   formatDuration(txn.XactDuration),
			"q_age":     formatDuration(txn.QueryDuration),
			"_xact_dur": txn.XactDuration,
			"_state":    txn.State,
			"_query":    txn.Query,
			"_pid":      txn.PID,
		}))
	}
	v.table = v.table.WithRows(rows).WithRowStyleFunc(func(input table.RowStyleFuncInput) lipgloss.Style {
		state, _ := input.Row.Data["_state"].(string)
		xactDur, _ := input.Row.Data["_xact_dur"].(time.Duration)
		if state == "idle in transaction" {
			return lipgloss.NewStyle().Foreground(theme.ColorRed)
		}
		if xactDur >= 60*time.Second {
			return lipgloss.NewStyle().Foreground(theme.ColorRed)
		}
		if xactDur >= 30*time.Second {
			return lipgloss.NewStyle().Foreground(theme.ColorYellow)
		}
		return lipgloss.NewStyle()
	})
}

func (v *Transactions) selectedPID() (int, bool) {
	row := v.table.HighlightedRow()
	pid, ok := row.Data["_pid"].(int)
	if !ok {
		return 0, false
	}
	return pid, true
}

func (v *Transactions) terminateSelected() tea.Cmd {
	pid, ok := v.selectedPID()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		err := v.db.TerminateBackend(ctx, pid)
		if err != nil {
			return transactionsStatusMsg{message: fmt.Sprintf("terminate pid %d failed: %v", pid, err)}
		}
		return transactionsStatusMsg{message: fmt.Sprintf("terminated pid %d", pid)}
	}
}

func (v *Transactions) fetchData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		transactions, err := v.db.GetTransactions(ctx)
		if err != nil {
			return transactionsDataMsg{err: err}
		}
		return transactionsDataMsg{transactions: transactions}
	}
}

func (v *Transactions) tick() tea.Cmd {
	return tea.Tick(transactionsRefreshInterval, func(time.Time) tea.Msg {
		return transactionsTickMsg{}
	})
}
