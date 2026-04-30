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

// durationColor returns a tview dynamic color tag based on duration severity.
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

// separator renders a section divider line.
func separator(title string) string {
	line := strings.Repeat("\u2500", 40)
	return fmt.Sprintf("\n[#808080]\u2500\u2500 %s %s[-]\n", title, line)
}

// kvLine renders a label-value pair with consistent formatting.
func kvLine(label, value string) string {
	return fmt.Sprintf("[#D78700]%-16s[-] [white]%s[-]\n", label+":", value)
}

// kvLineColored renders a label-value pair with a custom value color.
func kvLineColored(label, value, color string) string {
	return fmt.Sprintf("[#D78700]%-16s[-] [%s]%s[-]\n", label+":", color, value)
}

// SQL syntax highlighting patterns.
var (
	sqlKeywordRe = regexp.MustCompile(`(?i)\b(SELECT|FROM|WHERE|JOIN|ON|AND|OR|INSERT|UPDATE|DELETE|SET|INTO|VALUES|GROUP\s+BY|ORDER\s+BY|LIMIT|BEGIN|COMMIT|ROLLBACK|CREATE|ALTER|DROP|AS|LEFT|RIGHT|INNER|OUTER|HAVING|DISTINCT|UNION|CASE|WHEN|THEN|ELSE|END|NOT|IN|EXISTS|IS|NULL|TRUE|FALSE|LIKE|BETWEEN|WITH|RECURSIVE|RETURNING)\b`)
	sqlStringRe  = regexp.MustCompile(`'[^']*'`)
	sqlNumberRe  = regexp.MustCompile(`\b\d+\b`)
	sqlCommentRe = regexp.MustCompile(`(--[^\n]*|/\*.*?\*/)`)
)

// highlightSQL adds tview color tags for SQL syntax highlighting.
func highlightSQL(sql string) string {
	// Track regions that are already tagged to avoid double-tagging.
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

	// We'll do replacements via an index-based approach.
	type replacement struct {
		start, end int
		text       string
	}
	var repls []replacement

	// Comments first (lowest precedence visually but should not be re-tagged).
	for _, m := range sqlCommentRe.FindAllStringIndex(sql, -1) {
		if !overlaps(m[0], m[1]) {
			regions = append(regions, region{m[0], m[1]})
			repls = append(repls, replacement{m[0], m[1], "[#585858]" + sql[m[0]:m[1]] + "[-]"})
		}
	}

	// String literals.
	for _, m := range sqlStringRe.FindAllStringIndex(sql, -1) {
		if !overlaps(m[0], m[1]) {
			regions = append(regions, region{m[0], m[1]})
			repls = append(repls, replacement{m[0], m[1], "[#00D700]" + sql[m[0]:m[1]] + "[-]"})
		}
	}

	// Keywords.
	for _, m := range sqlKeywordRe.FindAllStringIndex(sql, -1) {
		if !overlaps(m[0], m[1]) {
			regions = append(regions, region{m[0], m[1]})
			repls = append(repls, replacement{m[0], m[1], "[#5F87FF]" + sql[m[0]:m[1]] + "[-]"})
		}
	}

	// Numbers.
	for _, m := range sqlNumberRe.FindAllStringIndex(sql, -1) {
		if !overlaps(m[0], m[1]) {
			regions = append(regions, region{m[0], m[1]})
			repls = append(repls, replacement{m[0], m[1], "[#FFD700]" + sql[m[0]:m[1]] + "[-]"})
		}
	}

	// Apply replacements from end to start to preserve indices.
	// Sort by start descending.
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

// mergeComments parses sqlcommentor tags from all statements in a query,
// merging with last-write-wins semantics.
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

// queryDetailPanes holds the sub-views for the split-pane query detail.
type queryDetailPanes struct {
	meta    *tview.TextView
	query   *tview.TextView
	rules   *tview.TextView
	history *tview.TextView
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

// NewQueryDetailView creates a split-pane detail view for an active query.
func NewQueryDetailView(q db.Query, dbConn *db.DB, history *db.QueryHistory, app *tview.Application, engine *rules.Engine) *tview.Flex {
	panes := &queryDetailPanes{
		meta:  newTextPane("Info"),
		query: newTextPane("Query"),
		rules: newTextPane("Breaches"),
	}

	// Middle row: query (left) + breaches (right)
	middle := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(panes.query, 0, 2, false).
		AddItem(panes.rules, 0, 1, false)
	middle.SetBackgroundColor(tcell.ColorDefault)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(panes.meta, 8, 0, false).
		AddItem(middle, 0, 1, true)
	layout.SetBackgroundColor(tcell.ColorDefault)

	// Only add the history pane for transaction context (history != nil)
	if history != nil {
		panes.history = newTextPane("Transaction History")
		layout.AddItem(panes.history, 8, 0, false)
	}

	var statusMsg string

	renderAll := func(query db.Query) {
		renderMetaPane(panes.meta, query, history, statusMsg)
		renderQueryPane(panes.query, query)
		renderBreachesPane(panes.rules, query, engine)
		renderHistoryPane(panes.history, query, history)
	}

	renderAll(q)

	if q.State == "completed" {
		return layout
	}

	pid := q.PID
	done := make(chan struct{})
	var closeOnce sync.Once
	stopRefresh := func() { closeOnce.Do(func() { close(done) }) }

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx := context.Background()
				queries, err := dbConn.GetActiveQueries(ctx)
				if err != nil {
					continue
				}
				for _, updated := range queries {
					if updated.PID == pid {
						app.QueueUpdateDraw(func() { renderAll(updated) })
						break
					}
				}
			case <-done:
				return
			}
		}
	}()

	layout.SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
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

func renderMetaPane(tv *tview.TextView, q db.Query, history *db.QueryHistory, statusMsg string) {
	var b strings.Builder
	if statusMsg != "" {
		b.WriteString(statusMsg + "\n")
	}

	// Build left and right columns
	type kv struct{ k, v string }
	var left []kv
	left = append(left, kv{"PID", fmt.Sprintf("%d", q.PID)})
	left = append(left, kv{"User", q.User})
	left = append(left, kv{"Application", q.App})
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

	if history != nil {
		entries := history.Get(q.PID)
		if len(entries) > 1 {
			totalStmts := 0
			for _, e := range entries {
				totalStmts += countStatements(e.Query)
			}
			left = append(left, kv{"Queries (txn)", fmt.Sprintf("%d (%d stmts)", len(entries), totalStmts)})
		}
	}

	// Render two columns side by side
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

	tv.SetText(b.String())
}

func renderQueryPane(tv *tview.TextView, q db.Query) {
	tv.SetText(highlightSQL(q.QueryText))
}

func renderBreachesPane(tv *tview.TextView, q db.Query, engine *rules.Engine) {
	if engine == nil {
		tv.SetText("[#808080]No rules engine[-]")
		return
	}

	breaches := engine.RecentBreaches()
	var matching []rules.Breach
	for _, b := range breaches {
		if b.PID == q.PID {
			matching = append(matching, b)
		}
	}

	if len(matching) == 0 {
		tv.SetText("[#808080]No breaches for PID " + fmt.Sprintf("%d", q.PID) + "[-]")
		return
	}

	var b strings.Builder
	for _, br := range matching {
		status := "dry-run"
		statusColor := "#808080"
		if br.Error != "" {
			status = "error"
			statusColor = "#FF5F5F"
		} else if br.Actioned {
			status = "fired"
			statusColor = "#FF5F5F"
		} else if !br.Active {
			status = "completed"
			statusColor = "#808080"
		}

		b.WriteString(fmt.Sprintf("[#D78700]%s[-]\n", br.RuleName))
		b.WriteString(fmt.Sprintf("  [#808080]%s[-]\n", br.Expression))
		b.WriteString(fmt.Sprintf("  action: %s  status: [%s]%s[-]\n", br.Action, statusColor, status))
		b.WriteString(fmt.Sprintf("  [#808080]%s[-]\n\n", br.Timestamp.Format("15:04:05")))
	}
	tv.SetText(b.String())
}

func renderHistoryPane(tv *tview.TextView, q db.Query, history *db.QueryHistory) {
	if tv == nil || history == nil {
		return
	}
	entries := history.Get(q.PID)
	if len(entries) <= 1 {
		tv.SetText("[#808080]Single query — no transaction history[-]")
		return
	}
	tv.SetTitle(fmt.Sprintf(" Transaction History (%d queries) ", len(entries)))
	var b strings.Builder
	for i, e := range entries {
		prefix := "  "
		if i == len(entries)-1 {
			prefix = "→ "
		}
		queryPreview := e.Query
		if len(queryPreview) > 120 {
			queryPreview = queryPreview[:117] + "..."
		}
		b.WriteString(fmt.Sprintf("[#808080]%s%s[-] [#585858]%s[-] %s\n",
			prefix, e.Timestamp.Format("15:04:05"), e.State, highlightSQL(queryPreview)))
	}
	tv.SetText(b.String())
}

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
	bl.WriteString("\n" + highlightSQL(l.BlockedQuery) + "\n")
	blocked.SetText(bl.String())

	var bk strings.Builder
	bk.WriteString(kvLine("User", l.BlockingUser))
	bk.WriteString(kvLine("App", l.BlockingApp))
	bk.WriteString("\n" + highlightSQL(l.BlockingQuery) + "\n")
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
