package views

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fraser-isbester/tusk/internal/db"
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

// NewQueryDetailView creates a detail view for an active query.
// If history is non-nil, shows the query history for this PID.
// The view live-refreshes every 2s by re-fetching the query from the DB.
func NewQueryDetailView(q db.ActiveQuery, dbConn *db.DB, history *db.QueryHistory, app *tview.Application) *tview.TextView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	tv.SetBackgroundColor(tcell.ColorDefault)
	tv.SetBorder(false)

	var statusMsg string

	renderWithStatus := func(query db.ActiveQuery) {
		renderQueryDetail(tv, query, history, statusMsg)
	}

	renderWithStatus(q)

	pid := q.PID
	done := make(chan struct{})
	var closeOnce sync.Once
	stopRefresh := func() { closeOnce.Do(func() { close(done) }) }

	// Live refresh goroutine.
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
						app.QueueUpdateDraw(func() {
							renderWithStatus(updated)
						})
						break
					}
				}
			case <-done:
				return
			}
		}
	}()

	tv.SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
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
					renderWithStatus(q)
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
					renderWithStatus(q)
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

	return tv
}

func renderQueryDetail(tv *tview.TextView, q db.ActiveQuery, history *db.QueryHistory, statusMsg string) {
	var b strings.Builder
	b.WriteString("\n")

	if statusMsg != "" {
		b.WriteString(statusMsg + "\n\n")
	}

	b.WriteString(kvLine("PID", fmt.Sprintf("%d", q.PID)))
	b.WriteString(kvLine("User", q.User))
	b.WriteString(kvLine("Application", q.AppName))
	if q.ClientAddr != "" {
		b.WriteString(kvLine("Client", q.ClientAddr))
	}
	b.WriteString(kvLine("State", q.State))

	if q.WaitEventType != "" || q.WaitEvent != "" {
		waitStr := q.WaitEventType
		if q.WaitEvent != "" {
			waitStr += ":" + q.WaitEvent
		}
		b.WriteString(kvLine("Wait Event", waitStr))
	}

	b.WriteString(kvLineColored("Duration", formatDuration(q.Duration), durationColor(q.Duration)))

	// SQLcommentor tags — parse from query text, merge across statements.
	comment := mergeComments(q.Query)
	// Also merge with pre-parsed comment on the ActiveQuery.
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

	// Show tags inline in the header area
	if comment.App != "" || comment.Route != "" || comment.Controller != "" || comment.Action != "" || comment.Framework != "" {
		var tags []string
		if comment.App != "" {
			tags = append(tags, fmt.Sprintf("[#00D7FF]app=[white]%s[-]", comment.App))
		}
		if comment.Route != "" {
			tags = append(tags, fmt.Sprintf("[#00D7FF]route=[white]%s[-]", comment.Route))
		}
		if comment.Controller != "" {
			tags = append(tags, fmt.Sprintf("[#00D7FF]controller=[white]%s[-]", comment.Controller))
		}
		if comment.Action != "" {
			tags = append(tags, fmt.Sprintf("[#00D7FF]action=[white]%s[-]", comment.Action))
		}
		if comment.Framework != "" {
			tags = append(tags, fmt.Sprintf("[#00D7FF]framework=[white]%s[-]", comment.Framework))
		}
		b.WriteString(fmt.Sprintf("[#D78700]%-16s[-] %s\n", "Tags:", strings.Join(tags, "  ")))
	}

	// Current query with syntax highlighting.
	b.WriteString(separator("Query"))
	b.WriteString(highlightSQL(q.Query) + "\n")

	// Query history for this PID.
	if history != nil {
		entries := history.Get(q.PID)
		if len(entries) > 1 {
			b.WriteString(separator(fmt.Sprintf("Transaction History (%d queries)", len(entries))))
			for i, e := range entries {
				ts := e.Timestamp.Format("15:04:05")
				prefix := "  "
				if i == len(entries)-1 {
					prefix = "→ " // current query
				}
				// Truncate long queries for the history list.
				queryPreview := e.Query
				if len(queryPreview) > 120 {
					queryPreview = queryPreview[:117] + "..."
				}
				b.WriteString(fmt.Sprintf("[#808080]%s%s[-] [#585858]%s[-] %s\n",
					prefix, ts, e.State, highlightSQL(queryPreview)))
			}
		}
	}

	// Footer
	b.WriteString("\n[#808080][c] cancel  [t] terminate  [Esc] back[-]\n")

	tv.SetText(b.String())
}

// NewLockDetailView creates a detail view for a lock.
func NewLockDetailView(l db.LockInfo, dbConn *db.DB) *tview.TextView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	tv.SetBackgroundColor(tcell.ColorDefault)
	tv.SetBorder(false)

	renderLockDetail(tv, l)

	tv.SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
		if evt.Rune() == 't' {
			go func() {
				dbConn.TerminateBackend(context.Background(), l.BlockingPID)
			}()
			return nil
		}
		return evt
	})

	return tv
}

func renderLockDetail(tv *tview.TextView, l db.LockInfo) {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(kvLine("Lock Type", l.LockType))
	b.WriteString(kvLine("Mode", l.Mode))
	b.WriteString(kvLineColored("Wait Time", formatDuration(l.WaitDuration), durationColor(l.WaitDuration)))

	// Blocked section
	b.WriteString(separator("Blocked"))
	b.WriteString(kvLine("PID", fmt.Sprintf("%d", l.BlockedPID)))
	b.WriteString(kvLine("User", l.BlockedUser))
	b.WriteString(kvLine("App", l.BlockedApp))
	b.WriteString(kvLine("Query", ""))
	b.WriteString(fmt.Sprintf("[#00D7FF]%s[-]\n", l.BlockedQuery))

	// Blocker section
	b.WriteString(separator("Blocker"))
	b.WriteString(kvLine("PID", fmt.Sprintf("%d", l.BlockingPID)))
	b.WriteString(kvLine("User", l.BlockingUser))
	b.WriteString(kvLine("App", l.BlockingApp))
	b.WriteString(kvLine("Query", ""))
	b.WriteString(fmt.Sprintf("[#00D7FF]%s[-]\n", l.BlockingQuery))

	// Footer
	b.WriteString("\n[#808080][t] terminate blocker  [Esc] back[-]\n")

	tv.SetText(b.String())
}

// NewTableDetailView creates a detail view for a table, loading data asynchronously.
func NewTableDetailView(schema, name string, dbConn *db.DB, app *tview.Application) *tview.TextView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	tv.SetBackgroundColor(tcell.ColorDefault)
	tv.SetBorder(false)

	tv.SetText(fmt.Sprintf("\n[#808080]Loading %s.%s...[-]", schema, name))

	go func() {
		detail, err := dbConn.GetTableDetail(context.Background(), schema, name)
		if err != nil {
			app.QueueUpdateDraw(func() {
				tv.SetText(fmt.Sprintf("\n[#FF5F5F]Error: %s[-]", err.Error()))
			})
			return
		}
		app.QueueUpdateDraw(func() {
			renderTableDetail(tv, detail)
		})
	}()

	return tv
}

func renderTableDetail(tv *tview.TextView, d *db.TableDetail) {
	var b strings.Builder
	b.WriteString("\n")
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

	// Columns section
	b.WriteString(separator("Columns"))
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

	// Indexes section
	if len(d.Indexes) > 0 {
		b.WriteString(separator("Indexes"))
		b.WriteString(fmt.Sprintf("[#D78700]%-30s %10s %10s[-]\n", "NAME", "SCANS", "SIZE"))
		for _, idx := range d.Indexes {
			b.WriteString(fmt.Sprintf("[white]%-30s %10d %10s[-]\n", idx.IndexName, idx.Scans, formatSize(idx.Size)))
		}
	}

	// Footer
	b.WriteString("\n[#808080][Esc] back[-]\n")

	tv.SetText(b.String())
}
