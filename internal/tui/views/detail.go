package views

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func durationColor(d time.Duration) string {
	switch {
	case d >= 30*time.Second:
		return "#FF5F5F"
	case d >= time.Second:
		return "#FFD700"
	default:
		return "#00D700"
	}
}

func separator(title string) string {
	line := strings.Repeat("\u2500", 30)
	return fmt.Sprintf("[#808080]\u2500\u2500 %s %s[-]\n", title, line)
}

func kvLine(label, value string) string {
	return fmt.Sprintf("[#D78700]%-16s[-] [white]%s[-]\n", label+":", value)
}

func kvLineColored(label, value, color string) string {
	return fmt.Sprintf("[#D78700]%-16s[-] [%s]%s[-]\n", label+":", color, value)
}

var (
	sqlKeywordRe = regexp.MustCompile(`(?i)\b(SELECT|FROM|WHERE|JOIN|ON|AND|OR|INSERT|UPDATE|DELETE|SET|INTO|VALUES|GROUP\s+BY|ORDER\s+BY|LIMIT|BEGIN|COMMIT|ROLLBACK|CREATE|ALTER|DROP|AS|LEFT|RIGHT|INNER|OUTER|HAVING|DISTINCT|UNION|CASE|WHEN|THEN|ELSE|END|NOT|IN|EXISTS|IS|NULL|TRUE|FALSE|LIKE|BETWEEN|WITH|RECURSIVE|RETURNING)\b`)
	sqlStringRe  = regexp.MustCompile(`'[^']*'`)
	sqlNumberRe  = regexp.MustCompile(`\b\d+\b`)
	sqlCommentRe = regexp.MustCompile(`(--[^\n]*|/\*.*?\*/)`)
)

func highlightSQL(sql string) string {
	type region struct{ start, end int }
	var regions []region
	overlaps := func(s, e int) bool {
		for _, r := range regions {
			if s < r.end && e > r.start {
				return true
			}
		}
		return false
	}
	type replacement struct {
		start, end int
		text       string
	}
	var repls []replacement
	for _, m := range sqlCommentRe.FindAllStringIndex(sql, -1) {
		if !overlaps(m[0], m[1]) {
			regions = append(regions, region{m[0], m[1]})
			repls = append(repls, replacement{m[0], m[1], "[#585858]" + sql[m[0]:m[1]] + "[-]"})
		}
	}
	for _, m := range sqlStringRe.FindAllStringIndex(sql, -1) {
		if !overlaps(m[0], m[1]) {
			regions = append(regions, region{m[0], m[1]})
			repls = append(repls, replacement{m[0], m[1], "[#00D700]" + sql[m[0]:m[1]] + "[-]"})
		}
	}
	for _, m := range sqlKeywordRe.FindAllStringIndex(sql, -1) {
		if !overlaps(m[0], m[1]) {
			regions = append(regions, region{m[0], m[1]})
			repls = append(repls, replacement{m[0], m[1], "[#5F87FF]" + sql[m[0]:m[1]] + "[-]"})
		}
	}
	for _, m := range sqlNumberRe.FindAllStringIndex(sql, -1) {
		if !overlaps(m[0], m[1]) {
			regions = append(regions, region{m[0], m[1]})
			repls = append(repls, replacement{m[0], m[1], "[#FFD700]" + sql[m[0]:m[1]] + "[-]"})
		}
	}
	for i := 0; i < len(repls); i++ {
		for j := i + 1; j < len(repls); j++ {
			if repls[j].start > repls[i].start {
				repls[i], repls[j] = repls[j], repls[i]
			}
		}
	}
	result := sql
	for _, r := range repls {
		result = result[:r.start] + r.text + result[r.end:]
	}
	return result
}

func mergeComments(query string) db.SQLComment {
	var merged db.SQLComment
	for _, stmt := range strings.Split(query, ";") {
		c := db.ParseSQLComment(strings.TrimSpace(stmt))
		if c.App != "" {
			merged.App = c.App
		}
		if c.Route != "" {
			merged.Route = c.Route
		}
		if c.Controller != "" {
			merged.Controller = c.Controller
		}
		if c.Action != "" {
			merged.Action = c.Action
		}
		if c.Framework != "" {
			merged.Framework = c.Framework
		}
	}
	return merged
}

// formatSQL adds line breaks before major SQL clauses for readable display.
var sqlClauseRe = regexp.MustCompile(`(?i)\b(SELECT|FROM|WHERE|JOIN|LEFT JOIN|RIGHT JOIN|INNER JOIN|OUTER JOIN|CROSS JOIN|ON|AND|OR|ORDER BY|GROUP BY|HAVING|LIMIT|OFFSET|VALUES|SET|INTO|RETURNING|UNION|EXCEPT|INTERSECT|BEGIN|COMMIT|ROLLBACK)\b`)

func formatSQL(sql string) string {
	// Protect string literals from being split
	type span struct{ start, end int }
	var literals []span
	for _, m := range sqlStringRe.FindAllStringIndex(sql, -1) {
		literals = append(literals, span{m[0], m[1]})
	}
	inLiteral := func(pos int) bool {
		for _, l := range literals {
			if pos >= l.start && pos < l.end {
				return true
			}
		}
		return false
	}

	matches := sqlClauseRe.FindAllStringIndex(sql, -1)
	if len(matches) == 0 {
		return sql
	}

	var b strings.Builder
	last := 0
	for i, m := range matches {
		if inLiteral(m[0]) {
			continue
		}
		// Don't break before the very first keyword
		if i == 0 && m[0] == 0 {
			continue
		}
		// Write everything before this keyword
		b.WriteString(strings.TrimRight(sql[last:m[0]], " \t"))
		keyword := strings.ToUpper(strings.TrimSpace(sql[m[0]:m[1]]))
		// Indent sub-clauses
		switch keyword {
		case "AND", "OR", "ON":
			b.WriteString("\n  " + keyword)
		default:
			b.WriteString("\n" + keyword)
		}
		last = m[1]
	}
	b.WriteString(sql[last:])
	return b.String()
}

func newTextPane(title string) *tview.TextView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	tv.SetBackgroundColor(tcell.ColorDefault)
	tv.SetBorder(true).SetBorderColor(theme.ColorBorderActive).SetTitle(" " + title + " ").SetTitleColor(theme.ColorLogo)
	return tv
}

type kv struct{ k, v string }

func renderTwoColumns(b *strings.Builder, left, right []kv) {
	rows := len(left)
	if len(right) > rows {
		rows = len(right)
	}
	for i := 0; i < rows; i++ {
		leftStr := ""
		if i < len(left) {
			leftStr = fmt.Sprintf("[#D78700]%-16s[-] %s", left[i].k+":", left[i].v)
		}
		rightStr := ""
		if i < len(right) {
			rightStr = fmt.Sprintf("[#00D7FF]%-14s[-] [white]%s[-]", right[i].k+":", right[i].v)
		}
		if rightStr != "" {
			leftVisual := tview.TaggedStringWidth(leftStr)
			pad := 45 - leftVisual
			if pad < 2 {
				pad = 2
			}
			b.WriteString(leftStr + strings.Repeat(" ", pad) + rightStr + "\n")
		} else {
			b.WriteString(leftStr + "\n")
		}
	}
}

// Navigator is a callback for pushing detail pages onto the app's view stack.
type Navigator func(name string, detail tview.Primitive)

// borderedPrimitive is any tview primitive that supports border coloring.
type borderedPrimitive interface {
	tview.Primitive
	SetBorderColor(tcell.Color) *tview.Box
	SetInputCapture(func(*tcell.EventKey) *tcell.EventKey) *tview.Box
}

// setupPaneNav adds Tab/Shift-Tab navigation between focusable panes.
// extraKeys is an optional handler for additional keys (c/t/q/Esc) — called
// if the event isn't consumed by tab navigation.
func setupPaneNav(layout *tview.Flex, panes []borderedPrimitive, app *tview.Application, extraKeys func(*tcell.EventKey) *tcell.EventKey) {
	idx := new(int)
	updateFocus := func() {
		for i, p := range panes {
			if i == *idx {
				p.SetBorderColor(theme.ColorLogo)
			} else {
				p.SetBorderColor(theme.ColorBorderActive)
			}
		}
		app.SetFocus(panes[*idx])
	}

	layout.SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
		switch evt.Key() {
		case tcell.KeyTab:
			*idx = (*idx + 1) % len(panes)
			updateFocus()
			return nil
		case tcell.KeyBacktab:
			*idx = (*idx - 1 + len(panes)) % len(panes)
			updateFocus()
			return nil
		}
		if extraKeys != nil {
			return extraKeys(evt)
		}
		return evt
	})

	updateFocus()
}

// ---------------------------------------------------------------------------
// Query Detail
// ---------------------------------------------------------------------------

// NewQueryDetailView creates a split-pane detail view for a query.
func NewQueryDetailView(q db.Query, dbConn *db.DB, history *db.QueryHistory, app *tview.Application, engine *rules.Engine, nav Navigator) *tview.Flex {
	info := newTextPane("Info")
	query := newTextPane("Query")
	activityTable := newTablePane("Activity")

	middle := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(query, 0, 2, false).
		AddItem(activityTable, 0, 1, false)
	middle.SetBackgroundColor(tcell.ColorDefault)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(info, 10, 0, false).
		AddItem(middle, 0, 1, true)
	layout.SetBackgroundColor(tcell.ColorDefault)

	var statusMsg string
	var actItems map[int]activityItem

	renderAll := func(q db.Query) {
		renderQueryInfo(info, q, statusMsg)
		query.SetText(highlightSQL(formatSQL(q.QueryText)))
		actItems = renderActivityTable(activityTable, q.PID, dbConn, engine)
	}

	activityTable.SetSelectedFunc(func(row, col int) {
		handleActivitySelect(row, actItems, q.PID, activityTable, dbConn, app, engine, nav)
	})

	renderAll(q)

	if q.State == "completed" {
		setupPaneNav(layout, []borderedPrimitive{info, query, activityTable}, app, nil)
		return layout
	}

	pid := q.PID
	backendStart := q.BackendStart
	done := make(chan struct{})
	var closeOnce sync.Once
	stopRefresh := func() { closeOnce.Do(func() { close(done) }) }

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				queries, err := dbConn.GetActiveQueries(context.Background())
				if err != nil {
					continue
				}
				found := false
				for _, updated := range queries {
					if updated.PID == pid && updated.BackendStart.Equal(backendStart) {
						app.QueueUpdateDraw(func() { renderAll(updated) })
						found = true
						break
					}
				}
				if !found {
					gone := q
					gone.State = "ended"
					app.QueueUpdateDraw(func() { renderAll(gone) })
					stopRefresh()
					return
				}
			case <-done:
				return
			}
		}
	}()

	setupPaneNav(layout, []borderedPrimitive{info, query, activityTable}, app, func(evt *tcell.EventKey) *tcell.EventKey {
		switch evt.Rune() {
		case 'c':
			go func() {
				err := dbConn.CancelQuery(context.Background(), pid)
				app.QueueUpdateDraw(func() {
					if err != nil {
						statusMsg = fmt.Sprintf("[#FF5F5F]Error cancelling PID %d: %s[-]", pid, err.Error())
					} else {
						statusMsg = fmt.Sprintf("[#00D700]Cancelled PID %d[-]", pid)
					}
					renderAll(q)
				})
			}()
			return nil
		case 't':
			go func() {
				err := dbConn.TerminateBackend(context.Background(), pid)
				app.QueueUpdateDraw(func() {
					if err != nil {
						statusMsg = fmt.Sprintf("[#FF5F5F]Error terminating PID %d: %s[-]", pid, err.Error())
					} else {
						statusMsg = fmt.Sprintf("[#00D700]Terminated PID %d[-]", pid)
					}
					renderAll(q)
				})
			}()
			return nil
		case 'q':
			stopRefresh()
		}
		if evt.Key() == tcell.KeyEscape {
			stopRefresh()
		}
		return evt
	})

	return layout
}

func renderQueryInfo(tv *tview.TextView, q db.Query, statusMsg string) {
	var b strings.Builder
	if statusMsg != "" {
		b.WriteString(statusMsg + "\n")
	}

	var left []kv
	left = append(left, kv{"PID", fmt.Sprintf("%d", q.PID)})
	left = append(left, kv{"User", q.User})
	left = append(left, kv{"Application", q.App})
	if q.Database != "" {
		left = append(left, kv{"Database", q.Database})
	}
	if q.ClientAddr != "" {
		left = append(left, kv{"Client", q.ClientAddr})
	}
	left = append(left, kv{"State", q.State})
	if q.WaitEventType != "" || q.WaitEvent != "" {
		w := q.WaitEventType
		if q.WaitEvent != "" {
			w += ":" + q.WaitEvent
		}
		left = append(left, kv{"Wait Event", w})
	}
	left = append(left, kv{"Duration", fmt.Sprintf("[%s]%s[-]", durationColor(q.Duration), formatDuration(q.Duration))})
	left = append(left, kv{"Statements", fmt.Sprintf("%d", countStatements(q.QueryText))})
	if q.QueryID != 0 {
		qhash := fmt.Sprintf("%x", uint64(q.QueryID))
		if len(qhash) > 8 {
			qhash = qhash[:8]
		}
		left = append(left, kv{"QHASH", qhash})
	}
	if q.BlockedBy > 0 {
		left = append(left, kv{"Blocked By", fmt.Sprintf("PID %d", q.BlockedBy)})
	}

	// Right column: SQLcommenter tags
	comment := mergeComments(q.QueryText)
	if q.Comment.App != "" && comment.App == "" {
		comment.App = q.Comment.App
	}
	if q.Comment.Route != "" && comment.Route == "" {
		comment.Route = q.Comment.Route
	}
	if q.Comment.Controller != "" && comment.Controller == "" {
		comment.Controller = q.Comment.Controller
	}
	if q.Comment.Action != "" && comment.Action == "" {
		comment.Action = q.Comment.Action
	}
	if q.Comment.Framework != "" && comment.Framework == "" {
		comment.Framework = q.Comment.Framework
	}

	var right []kv
	if comment.App != "" {
		right = append(right, kv{"app", comment.App})
	}
	if comment.Route != "" {
		right = append(right, kv{"route", comment.Route})
	}
	if comment.Controller != "" {
		right = append(right, kv{"controller", comment.Controller})
	}
	if comment.Action != "" {
		right = append(right, kv{"action", comment.Action})
	}
	if comment.Framework != "" {
		right = append(right, kv{"framework", comment.Framework})
	}

	renderTwoColumns(&b, left, right)
	tv.SetText(b.String())
}

// ---------------------------------------------------------------------------
// Transaction Detail
// ---------------------------------------------------------------------------

// newTablePane creates a bordered, selectable table pane for detail views.
func newTablePane(title string) *tview.Table {
	t := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(theme.SelectedStyle)
	t.SetBackgroundColor(tcell.ColorDefault)
	t.SetBorder(true).SetBorderColor(theme.ColorBorderActive).SetTitle(" " + title + " ").SetTitleColor(theme.ColorLogo)
	return t
}

// NewTransactionDetailView creates a split-pane detail view for a transaction.
// The main pane shows all queries executed in this transaction (from history).
func NewTransactionDetailView(t db.Transaction, dbConn *db.DB, history *db.QueryHistory, app *tview.Application, engine *rules.Engine, nav Navigator) *tview.Flex {
	info := newTextPane("Info")
	queriesTable := newTablePane("Queries")
	activityTable := newTablePane("Activity")

	middle := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(queriesTable, 0, 2, false).
		AddItem(activityTable, 0, 1, false)
	middle.SetBackgroundColor(tcell.ColorDefault)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(info, 10, 0, false).
		AddItem(middle, 0, 1, true)
	layout.SetBackgroundColor(tcell.ColorDefault)

	var histEntries []db.QueryHistoryEntry
	var actItems map[int]activityItem

	renderAll := func(t db.Transaction) {
		renderTxnInfo(info, t)
		histEntries = renderTxnQueriesTable(queriesTable, t, history)
		actItems = renderActivityTable(activityTable, t.PID, dbConn, engine)
	}

	renderAll(t)

	activityTable.SetSelectedFunc(func(row, col int) {
		handleActivitySelect(row, actItems, t.PID, activityTable, dbConn, app, engine, nav)
	})

	// Enter on a query row → push a query detail page
	queriesTable.SetSelectedFunc(func(row, col int) {
		idx := row - 1
		if idx < 0 || idx >= len(histEntries) || nav == nil {
			return
		}
		e := histEntries[idx]
		q := db.Query{
			ResourceBase: db.ResourceBase{
				PID: t.PID, User: t.User, App: t.App, State: e.State, Database: t.Database,
			},
			QueryText: e.Query,
		}
		detail := NewQueryDetailView(q, dbConn, nil, app, engine, nav)
		nav("query-detail", detail)
	})

	pid := t.PID
	xactStart := t.XactStart
	done := make(chan struct{})
	var closeOnce sync.Once
	stopRefresh := func() { closeOnce.Do(func() { close(done) }) }

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				txns, err := dbConn.GetTransactions(context.Background())
				if err != nil {
					continue
				}
				found := false
				for _, updated := range txns {
					if updated.PID == pid && updated.XactStart.Equal(xactStart) {
						app.QueueUpdateDraw(func() { renderAll(updated) })
						found = true
						break
					}
				}
				if !found {
					// PID is gone — mark as terminated
					gone := t
					gone.State = "ended"
					app.QueueUpdateDraw(func() { renderAll(gone) })
					stopRefresh()
					return
				}
			case <-done:
				return
			}
		}
	}()

	setupPaneNav(layout, []borderedPrimitive{info, queriesTable, activityTable}, app, func(evt *tcell.EventKey) *tcell.EventKey {
		switch evt.Rune() {
		case 't':
			go func() {
				dbConn.TerminateBackend(context.Background(), pid)
			}()
			return nil
		case 'q':
			stopRefresh()
		}
		if evt.Key() == tcell.KeyEscape {
			stopRefresh()
		}
		return evt
	})

	return layout
}

func renderTxnInfo(tv *tview.TextView, t db.Transaction) {
	var b strings.Builder
	var left []kv
	left = append(left, kv{"PID", fmt.Sprintf("%d", t.PID)})
	left = append(left, kv{"User", t.User})
	left = append(left, kv{"Application", t.App})
	if t.Database != "" {
		left = append(left, kv{"Database", t.Database})
	}
	left = append(left, kv{"State", t.State})
	left = append(left, kv{"Txn Age", fmt.Sprintf("[%s]%s[-]", durationColor(t.XactDuration), formatDuration(t.XactDuration))})
	left = append(left, kv{"Last Query Age", fmt.Sprintf("[%s]%s[-]", durationColor(t.QueryDuration), formatDuration(t.QueryDuration))})
	left = append(left, kv{"Lock Count", fmt.Sprintf("%d", t.LockCount)})
	renderTwoColumns(&b, left, nil)
	tv.SetText(b.String())
}

// renderTxnQueriesTable populates a table with transaction query history.
// Returns the history entries for use by selection handlers.
func renderTxnQueriesTable(table *tview.Table, t db.Transaction, history *db.QueryHistory) []db.QueryHistoryEntry {
	sel, _ := table.GetSelection()
	table.Clear()

	headers := []string{"TIME", "QUERY"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(theme.ColorTableHeader).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if col == 1 {
			cell.SetExpansion(1)
		}
		table.SetCell(0, col, cell)
	}

	if history == nil {
		table.SetTitle(" Current Query ")
		table.SetCell(1, 0, tview.NewTableCell("").SetTextColor(theme.ColorDim))
		table.SetCell(1, 1, tview.NewTableCell(truncate(t.QueryText, 120)).SetTextColor(theme.ColorFg).SetExpansion(1))
		return nil
	}

	entries := history.Get(t.PID, t.XactStart)
	if len(entries) == 0 {
		table.SetTitle(" Current Query ")
		table.SetCell(1, 0, tview.NewTableCell("").SetTextColor(theme.ColorDim))
		table.SetCell(1, 1, tview.NewTableCell(truncate(t.QueryText, 120)).SetTextColor(theme.ColorFg).SetExpansion(1))
		return nil
	}

	table.SetTitle(fmt.Sprintf(" Queries (%d) ", len(entries)))

	for i, e := range entries {
		row := i + 1
		ts := e.Timestamp.Format("15:04:05.000")
		color := theme.ColorDim
		if i == len(entries)-1 {
			color = theme.ColorLogo
		}
		queryPreview := e.Query
		if len(queryPreview) > 120 {
			queryPreview = queryPreview[:117] + "..."
		}
		table.SetCell(row, 0, tview.NewTableCell(ts).SetTextColor(theme.ColorDim))
		table.SetCell(row, 1, tview.NewTableCell(queryPreview).SetTextColor(color).SetExpansion(1))
	}

	if sel > 0 && sel < table.GetRowCount() {
		table.Select(sel, 0)
	}

	return entries
}

// ---------------------------------------------------------------------------
// Activity Pane (shared by query and transaction detail)
// ---------------------------------------------------------------------------

type activityKind int

const (
	actLockPID   activityKind = iota // navigate to this PID
	actViolation                     // manually fire the action
	actEvent                         // informational, no action
)

type activityItem struct {
	kind     activityKind
	pid      int    // for actLockPID: the target PID to navigate to
	ruleName string // for actViolation: rule name to manually fire
	targetPID int   // for actViolation: PID to act on
}

// renderActivityTable populates the activity table and returns a row→item map.
// The map key is the table row number, the value is the item data.
func renderActivityTable(table *tview.Table, pid int, dbConn *db.DB, engine *rules.Engine) map[int]activityItem {
	sel, _ := table.GetSelection()
	table.Clear()
	rowMap := make(map[int]activityItem)
	row := 0

	// Lock context
	locks, err := dbConn.GetLocks(context.Background())
	if err == nil {
		var blockedBy []db.Lock
		var blocking []db.Lock
		for _, l := range locks {
			if l.BlockedPID == pid {
				blockedBy = append(blockedBy, l)
			}
			if l.BlockingPID == pid {
				blocking = append(blocking, l)
			}
		}
		if len(blockedBy) > 0 || len(blocking) > 0 {
			table.SetCell(row, 0, tview.NewTableCell("LOCKS").
				SetTextColor(theme.ColorTableHeader).SetAttributes(tcell.AttrBold).SetSelectable(false))
			table.SetCell(row, 1, tview.NewTableCell("").SetSelectable(false))
			row++

			for _, l := range blockedBy {
				q := l.BlockingQuery
				if len(q) > 50 {
					q = q[:47] + "..."
				}
				table.SetCell(row, 0, tview.NewTableCell(
					fmt.Sprintf("→ blocked by PID %d (%s)", l.BlockingPID, l.BlockingApp)).
					SetTextColor(theme.ColorRed))
				table.SetCell(row, 1, tview.NewTableCell(q).SetTextColor(theme.ColorDim).SetExpansion(1))
				rowMap[row] = activityItem{kind: actLockPID, pid: l.BlockingPID}
				row++
			}
			for _, l := range blocking {
				table.SetCell(row, 0, tview.NewTableCell(
					fmt.Sprintf("→ blocking PID %d (%s)", l.BlockedPID, l.BlockedApp)).
					SetTextColor(theme.ColorYellow))
				table.SetCell(row, 1, tview.NewTableCell("").SetExpansion(1))
				rowMap[row] = activityItem{kind: actLockPID, pid: l.BlockedPID}
				row++
			}

			table.SetCell(row, 0, tview.NewTableCell("").SetSelectable(false))
			row++
		}
	}

	// Violations
	if engine != nil {
		violations := engine.RecentViolations()
		var matching []rules.Violation
		for _, v := range violations {
			if v.PID == pid {
				matching = append(matching, v)
			}
		}
		if len(matching) > 0 {
			table.SetCell(row, 0, tview.NewTableCell("VIOLATIONS").
				SetTextColor(theme.ColorTableHeader).SetAttributes(tcell.AttrBold).SetSelectable(false))
			table.SetCell(row, 1, tview.NewTableCell("").SetSelectable(false))
			row++

			for _, v := range matching {
				hint := ""
				if v.DryRun {
					hint = "[#00D700] ⏎ fire action[-]"
				}
				table.SetCell(row, 0, tview.NewTableCell("→ "+v.RuleName).
					SetTextColor(theme.ColorLabel).SetAttributes(tcell.AttrBold))
				table.SetCell(row, 1, tview.NewTableCell(hint).SetExpansion(1))

				rowMap[row] = activityItem{
					kind:      actViolation,
					ruleName:  v.RuleName,
					targetPID: pid,
				}
				row++

				for _, evt := range v.Events {
					ts := evt.Time.Format("15:04:05.000")
					color := theme.ColorDim
					switch evt.Kind {
					case rules.EventDetected:
						color = theme.ColorYellow
					case rules.EventAction:
						color = theme.ColorLabel
					case rules.EventSent:
						color = theme.ColorRed
					case rules.EventError:
						color = theme.ColorRed
					}
					table.SetCell(row, 0, tview.NewTableCell("  "+ts).SetTextColor(theme.ColorDim).SetSelectable(false))
					table.SetCell(row, 1, tview.NewTableCell(evt.Message).SetTextColor(color).SetExpansion(1).SetSelectable(false))
					row++
				}
			}
		}
	}

	if row == 0 {
		table.SetCell(0, 0, tview.NewTableCell("No activity").
			SetTextColor(theme.ColorDim).SetSelectable(false))
	}

	// Restore selection
	if sel > 0 && sel < table.GetRowCount() {
		table.Select(sel, 0)
	}

	return rowMap
}

// handleActivitySelect processes Enter on an activity table row.
func handleActivitySelect(row int, items map[int]activityItem, pid int, table *tview.Table, dbConn *db.DB, app *tview.Application, engine *rules.Engine, nav Navigator) {
	item, ok := items[row]
	if !ok {
		return
	}

	switch item.kind {
	case actLockPID:
		if nav == nil {
			return
		}
		queries, err := dbConn.GetActiveQueries(context.Background())
		if err != nil {
			return
		}
		for _, q := range queries {
			if q.PID == item.pid {
				detail := NewQueryDetailView(q, dbConn, nil, app, engine, nav)
				nav("query-detail", detail)
				return
			}
		}
	case actViolation:
		if engine == nil {
			return
		}
		go func() {
			result := engine.ManualAction(item.ruleName, item.targetPID)
			// Re-render the activity table to show the new events
			app.QueueUpdateDraw(func() {
				renderActivityTable(table, pid, dbConn, engine)
			})
			_ = result
		}()
	}
}

// ---------------------------------------------------------------------------
// Lock Detail
// ---------------------------------------------------------------------------

// NewLockDetailView creates a split-pane detail view for a lock.
func NewLockDetailView(l db.Lock, dbConn *db.DB) *tview.Flex {
	meta := newTextPane("Lock Info")
	blocked := newTextPane(fmt.Sprintf("Blocked (PID %d)", l.BlockedPID))
	blocker := newTextPane(fmt.Sprintf("Blocker (PID %d)", l.BlockingPID))

	var mb strings.Builder
	mb.WriteString(kvLine("Lock Type", l.LockType))
	mb.WriteString(kvLine("Mode", l.Mode))
	mb.WriteString(kvLineColored("Wait Time", formatDuration(l.WaitDuration), durationColor(l.WaitDuration)))
	mb.WriteString(kvLine("Blocked PID", fmt.Sprintf("%d", l.BlockedPID)))
	mb.WriteString(kvLine("Blocker PID", fmt.Sprintf("%d", l.BlockingPID)))
	meta.SetText(mb.String())

	var bl strings.Builder
	bl.WriteString(kvLine("User", l.BlockedUser))
	bl.WriteString(kvLine("App", l.BlockedApp))
	bl.WriteString("\n" + highlightSQL(formatSQL(l.BlockedQuery)) + "\n")
	blocked.SetText(bl.String())

	var bk strings.Builder
	bk.WriteString(kvLine("User", l.BlockingUser))
	bk.WriteString(kvLine("App", l.BlockingApp))
	bk.WriteString("\n" + highlightSQL(formatSQL(l.BlockingQuery)) + "\n")
	blocker.SetText(bk.String())

	queries := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(blocked, 0, 1, false).
		AddItem(blocker, 0, 1, false)
	queries.SetBackgroundColor(tcell.ColorDefault)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(meta, 7, 0, false).
		AddItem(queries, 0, 1, true)
	layout.SetBackgroundColor(tcell.ColorDefault)

	layout.SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
		if evt.Rune() == 't' {
			go func() {
				dbConn.TerminateBackend(context.Background(), l.BlockingPID)
			}()
			return nil
		}
		return evt
	})

	return layout
}

// ---------------------------------------------------------------------------
// Table Detail
// ---------------------------------------------------------------------------

// NewTableDetailView creates a detail view for a table, loading data asynchronously.
func NewTableDetailView(schema, name string, dbConn *db.DB, app *tview.Application) *tview.Flex {
	meta := newTextPane("Table Info")
	columns := newTextPane("Columns")
	indexes := newTextPane("Indexes")

	meta.SetText(fmt.Sprintf("[#808080]Loading %s.%s...[-]", schema, name))

	colsAndIdx := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(columns, 0, 2, false).
		AddItem(indexes, 0, 1, false)
	colsAndIdx.SetBackgroundColor(tcell.ColorDefault)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(meta, 12, 0, false).
		AddItem(colsAndIdx, 0, 1, true)
	layout.SetBackgroundColor(tcell.ColorDefault)

	go func() {
		detail, err := dbConn.GetTableDetail(context.Background(), schema, name)
		if err != nil {
			app.QueueUpdateDraw(func() {
				meta.SetText(fmt.Sprintf("[#FF5F5F]Error: %s[-]", err.Error()))
			})
			return
		}
		app.QueueUpdateDraw(func() {
			renderTableMeta(meta, detail)
			renderTableColumns(columns, detail)
			renderTableIndexes(indexes, detail)
		})
	}()

	return layout
}

func renderTableMeta(tv *tview.TextView, d *db.TableDetail) {
	var b strings.Builder
	b.WriteString(kvLine("Table", fmt.Sprintf("%s.%s", d.Schema, d.Name)))
	b.WriteString(kvLine("Size", formatSize(d.TotalSize)))
	b.WriteString(kvLine("Rows", fmt.Sprintf("%d", d.LiveTuples)))
	var deadPct float64
	total := d.LiveTuples + d.DeadTuples
	if total > 0 {
		deadPct = float64(d.DeadTuples) / float64(total) * 100
	}
	b.WriteString(kvLine("Dead Tuples", fmt.Sprintf("%d (%.1f%%)", d.DeadTuples, deadPct)))
	b.WriteString(kvLine("Seq Scans", fmt.Sprintf("%d", d.SeqScan)))
	b.WriteString(kvLine("Idx Scans", fmt.Sprintf("%d", d.IdxScan)))
	b.WriteString(kvLine("Last Vacuum", timeAgo(d.LastVacuum)))
	b.WriteString(kvLine("Last AutoVac", timeAgo(d.LastAutoVacuum)))
	b.WriteString(kvLine("Last Analyze", timeAgo(d.LastAnalyze)))
	tv.SetText(b.String())
}

func renderTableColumns(tv *tview.TextView, d *db.TableDetail) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[#D78700]%-20s %-16s %-8s %-6s %s[-]\n", "NAME", "TYPE", "NULL", "PK", "DEFAULT"))
	for _, c := range d.Columns {
		nullable := "NO"
		if c.Nullable {
			nullable = "YES"
		}
		pk := ""
		if c.IsPrimary {
			pk = "\u2713"
		}
		defVal := c.Default
		if len(defVal) > 30 {
			defVal = defVal[:27] + "..."
		}
		b.WriteString(fmt.Sprintf("[white]%-20s %-16s %-8s %-6s %s[-]\n", c.Name, c.DataType, nullable, pk, defVal))
	}
	tv.SetText(b.String())
}

func renderTableIndexes(tv *tview.TextView, d *db.TableDetail) {
	if len(d.Indexes) == 0 {
		tv.SetText("[#808080]No indexes[-]")
		return
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[#D78700]%-30s %10s %10s[-]\n", "NAME", "SCANS", "SIZE"))
	for _, idx := range d.Indexes {
		b.WriteString(fmt.Sprintf("[white]%-30s %10d %10s[-]\n", idx.IndexName, idx.Scans, formatSize(idx.Size)))
	}
	tv.SetText(b.String())
}
