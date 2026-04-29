package views

import (
	"context"
	"fmt"
	"strings"
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

// NewQueryDetailView creates a detail view for an active query.
func NewQueryDetailView(q db.ActiveQuery, dbConn *db.DB) *tview.TextView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	tv.SetBackgroundColor(tcell.ColorDefault)
	tv.SetBorder(false)

	renderQueryDetail(tv, q)

	tv.SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
		switch evt.Rune() {
		case 'c':
			go func() {
				dbConn.CancelQuery(context.Background(), q.PID)
			}()
			return nil
		case 't':
			go func() {
				dbConn.TerminateBackend(context.Background(), q.PID)
			}()
			return nil
		}
		return evt
	})

	return tv
}

func renderQueryDetail(tv *tview.TextView, q db.ActiveQuery) {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(kvLine("PID", fmt.Sprintf("%d", q.PID)))
	b.WriteString(kvLine("User", q.User))
	b.WriteString(kvLine("Application", q.AppName))
	b.WriteString(kvLine("Client", q.ClientAddr))
	b.WriteString(kvLine("State", q.State))

	if q.WaitEventType != "" || q.WaitEvent != "" {
		waitStr := q.WaitEventType
		if q.WaitEvent != "" {
			waitStr += ":" + q.WaitEvent
		}
		b.WriteString(kvLine("Wait Event", waitStr))
	}

	b.WriteString(kvLineColored("Duration", formatDuration(q.Duration), durationColor(q.Duration)))

	// SQLcommentor section
	if q.Comment.App != "" {
		b.WriteString(separator("SQLcommentor"))
		b.WriteString(kvLine("App", q.Comment.App))
		if q.Comment.Route != "" {
			b.WriteString(kvLine("Route", q.Comment.Route))
		}
	}

	// Query section
	b.WriteString(separator("Query"))
	b.WriteString(fmt.Sprintf("[#00D7FF]%s[-]\n", q.Query))

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
