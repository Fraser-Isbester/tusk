package views

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

// Roles is the roles view.
type Roles struct {
	table  *tview.Table
	db     *db.DB
	roles      []db.RoleInfo
	filterText string
	mu         sync.Mutex
	ticker *time.Ticker
	done   chan struct{}
}

// NewRolesView creates a new Roles view.
func NewRolesView(database *db.DB) *Roles {
	v := &Roles{
		table: tview.NewTable().
			SetSelectable(true, false).
			SetFixed(1, 0).
			SetSelectedStyle(theme.SelectedStyle),
		db: database,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)
	return v
}

// Table returns the underlying tview.Table.
func (v *Roles) Table() *tview.Table { return v.table }

// ItemCount returns the number of roles.
func (v *Roles) ItemCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.roles)
}

// Start begins the background refresh loop.
func (v *Roles) Start(app *tview.Application) {
	v.done = make(chan struct{})
	v.ticker = time.NewTicker(10 * time.Second)
	go func() {
		v.refresh()
		app.QueueUpdateDraw(func() { v.render() })
		for {
			select {
			case <-v.ticker.C:
				v.refresh()
				app.QueueUpdateDraw(func() { v.render() })
			case <-v.done:
				return
			}
		}
	}()
}

// Stop stops the background refresh loop.
func (v *Roles) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
		v.done = nil
	}
}

// SelectedRole returns the role name at the currently selected row.
func (v *Roles) SelectedRole() (string, bool) {
	row, _ := v.table.GetSelection()
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := row - 1
	if idx < 0 || idx >= len(v.roles) {
		return "", false
	}
	return v.roles[idx].Name, true
}

// SetFilter sets the filter text for searching across all columns.
func (v *Roles) SetFilter(text string) {
	v.mu.Lock()
	v.filterText = text
	v.mu.Unlock()
	v.render()
}

func (v *Roles) refresh() {
	ctx := context.Background()
	roles, err := v.db.GetRoles(ctx)
	if err != nil {
		return
	}
	v.mu.Lock()
	v.roles = roles
	v.mu.Unlock()
}

func (v *Roles) render() {
	v.mu.Lock()
	defer v.mu.Unlock()

	selectedRow, _ := v.table.GetSelection()

	v.table.Clear()

	headers := []string{"NAME", "SUPERUSER", "CREATE ROLE", "CREATE DB", "LOGIN", "CONN LIMIT"}
	for i, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if i == 0 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, i, cell)
	}

	row := 1
	for _, r := range v.roles {
		color := tcell.ColorWhite

		connLimit := fmt.Sprintf("%d", r.ConnLimit)
		if r.ConnLimit == -1 {
			connLimit = "unlimited"
		}

		superuser := boolIcon(r.IsSuperuser)
		createrole := boolIcon(r.CanCreateRole)
		createdb := boolIcon(r.CanCreateDB)
		login := boolIcon(r.CanLogin)

		if v.filterText != "" {
			match := false
			searchText := strings.ToLower(v.filterText)
			for _, val := range []string{r.Name, superuser, createrole, createdb, login, connLimit} {
				if strings.Contains(strings.ToLower(val), searchText) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		v.table.SetCell(row, 0, tview.NewTableCell(r.Name).SetTextColor(color).SetExpansion(1))
		v.table.SetCell(row, 1, tview.NewTableCell(superuser).SetTextColor(color))
		v.table.SetCell(row, 2, tview.NewTableCell(createrole).SetTextColor(color))
		v.table.SetCell(row, 3, tview.NewTableCell(createdb).SetTextColor(color))
		v.table.SetCell(row, 4, tview.NewTableCell(login).SetTextColor(color))
		v.table.SetCell(row, 5, tview.NewTableCell(connLimit).SetTextColor(color))
		row++
	}

	if selectedRow > 0 && selectedRow < v.table.GetRowCount() {
		v.table.Select(selectedRow, 0)
	}
}
