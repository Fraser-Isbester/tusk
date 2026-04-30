package views

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Breaches is the breach history view.
type Breaches struct {
	table      *tview.Table
	engine     *rules.Engine
	ruleFilter string
	visible    []rules.Breach
	onSelect   func(rules.Breach)
	mu         sync.Mutex
	ticker     *time.Ticker
	done       chan struct{}
}

// NewBreachesView creates a new Breaches view.
func NewBreachesView(engine *rules.Engine) *Breaches {
	v := &Breaches{
		table: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		engine: engine,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)
	return v
}

// Table returns the underlying tview.Table.
func (v *Breaches) Table() *tview.Table { return v.table }

// ItemCount returns the number of recent breaches.
func (v *Breaches) ItemCount() int {
	if v.engine == nil {
		return 0
	}
	return len(v.engine.RecentBreaches())
}

// SetOnSelect sets a callback invoked when Enter is pressed on a breach row.
func (v *Breaches) SetOnSelect(fn func(rules.Breach)) {
	v.onSelect = fn
	v.table.SetSelectedFunc(func(row, col int) {
		if b, ok := v.SelectedBreach(); ok && v.onSelect != nil {
			v.onSelect(b)
		}
	})
}

// SetRuleFilter restricts the view to breaches from a specific rule.
func (v *Breaches) SetRuleFilter(name string) {
	v.mu.Lock()
	v.ruleFilter = name
	v.mu.Unlock()
}

// SelectedBreach returns the breach at the currently selected row.
func (v *Breaches) SelectedBreach() (rules.Breach, bool) {
	row, _ := v.table.GetSelection()
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := row - 1
	if idx < 0 || idx >= len(v.visible) {
		return rules.Breach{}, false
	}
	return v.visible[idx], true
}

// SetFilter filters breaches by rule name (substring match).
func (v *Breaches) SetFilter(text string) {
	v.mu.Lock()
	v.ruleFilter = text
	v.mu.Unlock()
}

// Start begins the periodic refresh loop.
func (v *Breaches) Start(app *tview.Application) {
	v.done = make(chan struct{})
	v.ticker = time.NewTicker(2 * time.Second)
	go func() {
		app.QueueUpdateDraw(func() { v.render() })
		for {
			select {
			case <-v.ticker.C:
				app.QueueUpdateDraw(func() { v.render() })
			case <-v.done:
				return
			}
		}
	}()
}

// Stop stops the periodic refresh loop.
func (v *Breaches) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
	}
}

func (v *Breaches) render() {
	v.mu.Lock()
	defer v.mu.Unlock()

	sel, _ := v.table.GetSelection()
	v.table.Clear()

	headers := []string{"TIME", "RULE", "PID", "EXPRESSION", "ACTION", "STATUS"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if col == 3 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, col, cell)
	}

	if v.engine == nil {
		v.table.SetCell(1, 0, tview.NewTableCell("No rules engine configured").
			SetTextColor(theme.ColorDim).SetSelectable(false))
		return
	}

	allBreaches := v.engine.RecentBreaches()

	// Apply rule filter
	v.visible = v.visible[:0]
	for _, b := range allBreaches {
		if v.ruleFilter != "" && !strings.Contains(strings.ToLower(b.RuleName), strings.ToLower(v.ruleFilter)) {
			continue
		}
		v.visible = append(v.visible, b)
	}

	if len(v.visible) == 0 {
		msg := "No breaches recorded"
		if v.ruleFilter != "" {
			msg = fmt.Sprintf("No breaches for rule %q", v.ruleFilter)
		}
		v.table.SetCell(1, 0, tview.NewTableCell(msg).
			SetTextColor(theme.ColorDim).SetSelectable(false))
		return
	}
	for i, b := range v.visible {
		row := i + 1
		ts := b.Timestamp.Format("15:04:05")

		status := "dry-run"
		statusColor := theme.ColorDim
		if b.Error != "" {
			status = "error"
			statusColor = theme.ColorRed
		} else if b.Actioned {
			status = "fired"
			statusColor = theme.ColorRed
		} else if !b.Actioned {
			status = "cooldown"
			statusColor = theme.ColorYellow
		}

		// Override for completed (target PID gone)
		rowColor := theme.ColorFg
		if !b.Active {
			status = "completed"
			statusColor = theme.ColorDim
			rowColor = theme.ColorDim
		}

		expr := b.Expression
		if len(expr) > 60 {
			expr = expr[:57] + "..."
		}

		v.table.SetCell(row, 0, tview.NewTableCell(ts).SetTextColor(theme.ColorDim))
		v.table.SetCell(row, 1, tview.NewTableCell(b.RuleName).SetTextColor(rowColor))
		v.table.SetCell(row, 2, tview.NewTableCell(fmt.Sprintf("%d", b.PID)).SetTextColor(rowColor))
		v.table.SetCell(row, 3, tview.NewTableCell(expr).SetTextColor(theme.ColorDim).SetExpansion(1))
		v.table.SetCell(row, 4, tview.NewTableCell(b.Action).SetTextColor(rowColor))
		v.table.SetCell(row, 5, tview.NewTableCell(status).SetTextColor(statusColor))
	}

	if sel > 0 && sel < v.table.GetRowCount() {
		v.table.Select(sel, 0)
	}
}
