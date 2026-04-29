package views

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fraser-isbester/tusk/internal/db"
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
		{Title: "PID", Width: 6},
		{Title: "USER", Width: 10},
		{Title: "APP", Width: 18},
		{Title: "STATE", Width: 8},
		{Title: "TXN AGE", Width: 10},
		{Title: "Q AGE", Width: 10},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
	)
	t.SetStyles(defaultTableStyles())
	return &Transactions{
		db:    database,
		table: t,
	}
}

// SetSize updates the terminal dimensions.
func (v *Transactions) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.table.SetWidth(w)
	v.table.SetHeight(h - 2)
}

// ItemCount returns the number of transactions.
func (v *Transactions) ItemCount() int { return len(v.data) }

// SelectedQuery returns an ActiveQuery built from the selected transaction row.
func (v *Transactions) SelectedQuery() (db.ActiveQuery, bool) {
	pid, ok := v.selectedPID()
	if !ok {
		return db.ActiveQuery{}, false
	}
	for _, txn := range v.data {
		if txn.PID == pid {
			return db.ActiveQuery{
				PID:     txn.PID,
				User:    txn.User,
				AppName: txn.AppName,
				State:   txn.State,
				Query:   txn.Query,
			}, true
		}
	}
	return db.ActiveQuery{}, false
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
	return TableBorder.Render(v.table.View())
}

func (v *Transactions) updateRows() {
	var rows []table.Row
	for _, txn := range v.data {
		if txn.User == "(system)" {
			continue
		}
		row := table.Row{
			fmt.Sprintf("%d", txn.PID),
			txn.User,
			txn.AppName,
			txn.State,
			formatDuration(txn.XactDuration),
			formatDuration(txn.QueryDuration),
		}
		rows = append(rows, row)
	}
	v.table.SetRows(rows)
}

func (v *Transactions) selectedPID() (int, bool) {
	row := v.table.SelectedRow()
	if row == nil {
		return 0, false
	}
	var pid int
	if _, err := fmt.Sscanf(row[0], "%d", &pid); err != nil {
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
