package views

import (
	"fmt"
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Rules is the configured rules view.
type Rules struct {
	table  *tview.Table
	engine *rules.Engine
	mu     sync.Mutex
	ticker *time.Ticker
	done   chan struct{}
}

// NewRulesView creates a new Rules view.
func NewRulesView(engine *rules.Engine) *Rules {
	v := &Rules{
		table: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle),
		engine: engine,
	}
	v.table.SetBackgroundColor(tcell.ColorDefault)
	v.table.SetBorder(true).SetBorderColor(theme.ColorBorder).SetBorderPadding(0, 0, 1, 1)
	return v
}

// Table returns the underlying tview.Table.
func (v *Rules) Table() *tview.Table { return v.table }

// ItemCount returns the number of configured rules.
func (v *Rules) ItemCount() int {
	if v.engine == nil {
		return 0
	}
	return len(v.engine.Rules())
}

// SelectedRule returns the rule name at the currently selected row.
func (v *Rules) SelectedRule() (string, bool) {
	row, _ := v.table.GetSelection()
	if v.engine == nil {
		return "", false
	}
	configuredRules := v.engine.Rules()
	idx := row - 1
	if idx < 0 || idx >= len(configuredRules) {
		return "", false
	}
	return configuredRules[idx].Name, true
}

// SetFilter is a no-op for the rules view.
func (v *Rules) SetFilter(_ string) {}

// Start begins the periodic refresh loop.
func (v *Rules) Start(app *tview.Application) {
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
func (v *Rules) Stop() {
	if v.ticker != nil {
		v.ticker.Stop()
	}
	if v.done != nil {
		close(v.done)
		v.done = nil
	}
}

// SetEngine updates the rules engine reference.
func (v *Rules) SetEngine(e *rules.Engine) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.engine = e
}

func (v *Rules) render() {
	v.mu.Lock()
	defer v.mu.Unlock()

	sel, _ := v.table.GetSelection()
	v.table.Clear()

	headers := []string{"NAME", "RESOURCE", "EXPRESSION", "ACTION", "COOLDOWN", "STATUS", "BREACHES"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if col == 2 {
			cell.SetExpansion(1)
		}
		v.table.SetCell(0, col, cell)
	}

	if v.engine == nil {
		v.table.SetCell(1, 0, tview.NewTableCell("No rules engine configured").
			SetTextColor(theme.ColorDim).SetSelectable(false))
		return
	}

	configuredRules := v.engine.Rules()
	if len(configuredRules) == 0 {
		v.table.SetCell(1, 0, tview.NewTableCell("No rules configured — add rules to your profile in ~/.config/tusk/config.yaml").
			SetTextColor(theme.ColorDim).SetSelectable(false))
		return
	}

	violatedPIDs := v.engine.ViolatedPIDs()
	recentViolations := v.engine.RecentViolations()

	for i, r := range configuredRules {
		row := i + 1

		violationCount := 0
		for _, viol := range recentViolations {
			if viol.RuleName == r.Name {
				violationCount++
			}
		}

		activePIDs := 0
		for _, viol := range violatedPIDs {
			if viol.RuleName == r.Name {
				activePIDs++
			}
		}

		expr := r.Expression
		if len(expr) > 80 {
			expr = expr[:77] + "..."
		}

		cooldown := "--"
		if r.Cooldown > 0 {
			cooldown = r.Cooldown.String()
		}

		status := "active"
		statusColor := theme.ColorGreen
		if r.DryRun {
			status = "dry-run"
			statusColor = theme.ColorYellow
		}
		if !r.Enabled {
			status = "disabled"
			statusColor = theme.ColorDim
		}

		violStr := fmt.Sprintf("%d", violationCount)
		violColor := theme.ColorFg
		if activePIDs > 0 {
			violStr = fmt.Sprintf("%d (%d active)", violationCount, activePIDs)
			violColor = theme.ColorRed
		}

		rowColor := theme.ColorFg
		if !r.Enabled {
			rowColor = theme.ColorDim
		}

		v.table.SetCell(row, 0, tview.NewTableCell(r.Name).SetTextColor(rowColor))
		v.table.SetCell(row, 1, tview.NewTableCell(string(r.Resource)).SetTextColor(rowColor))
		v.table.SetCell(row, 2, tview.NewTableCell(expr).SetTextColor(theme.ColorDim).SetExpansion(1))
		v.table.SetCell(row, 3, tview.NewTableCell(r.Action.Name()).SetTextColor(rowColor))
		v.table.SetCell(row, 4, tview.NewTableCell(cooldown).SetTextColor(rowColor))
		v.table.SetCell(row, 5, tview.NewTableCell(status).SetTextColor(statusColor))
		v.table.SetCell(row, 6, tview.NewTableCell(violStr).SetTextColor(violColor))
	}

	if sel > 0 && sel < v.table.GetRowCount() {
		v.table.Select(sel, 0)
	}
}
