package tui

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
	"github.com/fraser-isbester/tusk/internal/tui/views"
)

// View is the interface all resource views implement.
type View interface {
	Table() *tview.Table
	Start(app *tview.Application)
	Stop()
	ItemCount() int
}

// App is the root TUI application.
type App struct {
	app      *tview.Application
	db       *db.DB
	profile  string
	color    string
	readonly bool

	layout *tview.Flex
	header *tview.TextView
	crumbs *tview.TextView
	status *tview.TextView
	pages  *tview.Pages
	prompt *tview.InputField

	viewMap      map[string]View
	activeView   string
	queryHistory *db.QueryHistory
	viewStack  []string
	registry   *CommandRegistry

	mu            sync.Mutex
	serverVersion string
	serverUptime  time.Duration
	connCount     int
	connMax       int
	activeConns   int
	idleConns     int
	idleTxnConns  int
	cacheHitRatio float64
	tps           int64
	prevXactTotal int64
	prevTime      time.Time

	promptActive bool
	promptMode   string
}

func NewApp(database *db.DB, profileName, profileColor string, readonly bool) *App {
	a := &App{
		app:      tview.NewApplication(),
		db:       database,
		profile:  profileName,
		color:    profileColor,
		readonly: readonly,
		viewMap:      make(map[string]View),
		registry:     NewCommandRegistry(),
		queryHistory: db.NewQueryHistory(50),
	}

	a.buildLayout()
	a.registerViews()
	a.activeView = "queries"
	a.switchView("queries")
	a.setupKeys()
	a.wireNavigation()

	return a
}

func (a *App) buildLayout() {
	a.header = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	a.header.SetBackgroundColor(tcell.ColorDefault)

	a.crumbs = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	a.crumbs.SetBackgroundColor(theme.ColorHeaderBg)

	a.status = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	a.status.SetBackgroundColor(theme.ColorHeaderBg)

	a.prompt = tview.NewInputField().
		SetLabel(": ").
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetLabelColor(theme.ColorLogo)
	a.prompt.SetBackgroundColor(tcell.ColorDefault)

	a.pages = tview.NewPages()
	a.pages.SetBackgroundColor(tcell.ColorDefault)

	a.layout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 6, 0, false).
		AddItem(a.crumbs, 1, 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.status, 1, 0, false)
	a.layout.SetBackgroundColor(tcell.ColorDefault)
}

func (a *App) registerViews() {
	qv := views.NewQueriesView(a.db)
	qv.SetQueryHistory(a.queryHistory)
	a.viewMap["queries"] = qv

	tv := views.NewTransactionsView(a.db)
	tv.SetQueryHistory(a.queryHistory)
	a.viewMap["transactions"] = tv

	a.viewMap["connections"] = views.NewConnectionsView(a.db)
	a.viewMap["tables"] = views.NewTablesView(a.db)
	a.viewMap["db"] = views.NewDatabasesView(a.db)
	a.viewMap["roles"] = views.NewRolesView(a.db)
	a.viewMap["slow"] = views.NewSlowQueriesView(a.db)
	a.viewMap["locks"] = views.NewLocksView(a.db)
	a.viewMap["indexes"] = views.NewIndexesView(a.db)

	for name, v := range a.viewMap {
		a.pages.AddPage(name, v.Table(), true, false)
	}
}

func (a *App) switchView(name string) {
	if _, ok := a.viewMap[name]; !ok {
		return
	}
	if old, ok := a.viewMap[a.activeView]; ok {
		old.Stop()
	}
	if a.activeView != name {
		a.viewStack = append(a.viewStack, a.activeView)
	}
	a.activeView = name
	a.pages.SwitchToPage(name)
	v := a.viewMap[name]
	v.Start(a.app)
	a.app.SetFocus(v.Table())
	a.updateCrumbs()
	a.updateStatus()
}

func (a *App) setupKeys() {
	a.app.SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
		if a.promptActive {
			return evt
		}

		switch evt.Key() {
		case tcell.KeyEscape:
			if len(a.viewStack) > 0 {
				oldView := a.activeView
				prev := a.viewStack[len(a.viewStack)-1]
				a.viewStack = a.viewStack[:len(a.viewStack)-1]
				if strings.HasSuffix(oldView, "-detail") || strings.HasPrefix(oldView, "queries-") {
					a.pages.RemovePage(oldView)
				}
				// Set activeView to prev so switchView doesn't re-push
				a.activeView = prev
				a.switchView(prev)
				return nil
			}
		case tcell.KeyRune:
			switch evt.Rune() {
			case ':':
				a.showPrompt("command")
				return nil
			case '/':
				a.showPrompt("filter")
				return nil
			case 'q':
				a.app.Stop()
				return nil
			}
		case tcell.KeyCtrlC:
			a.app.Stop()
			return nil
		}
		return evt
	})
}

func (a *App) showPrompt(mode string) {
	a.promptActive = true
	a.promptMode = mode
	if mode == "command" {
		a.prompt.SetLabel(": ")
	} else {
		a.prompt.SetLabel("/ ")
	}
	a.prompt.SetText("")

	a.layout.RemoveItem(a.pages)
	a.layout.RemoveItem(a.status)
	a.layout.AddItem(a.prompt, 1, 0, true)
	a.layout.AddItem(a.pages, 0, 1, false)
	a.layout.AddItem(a.status, 1, 0, false)
	a.app.SetFocus(a.prompt)

	a.prompt.SetDoneFunc(func(key tcell.Key) {
		text := a.prompt.GetText()
		a.hidePrompt()
		if key == tcell.KeyEnter && text != "" {
			if a.promptMode == "command" {
				if viewName, ok := a.registry.Match(text); ok {
					a.switchView(viewName)
				}
			}
		}
	})
}

func (a *App) hidePrompt() {
	a.promptActive = false
	a.layout.RemoveItem(a.prompt)
	if v, ok := a.viewMap[a.activeView]; ok {
		a.app.SetFocus(v.Table())
	}
}

func (a *App) updateHeader() {
	a.mu.Lock()
	defer a.mu.Unlock()

	l := "[#D78700]"
	v := "[white]"
	c := "[#00D7FF]"
	g := "[#00D700]"
	y := "[#FFD700]"
	r := "[#FF5F5F]"

	var lines []string
	lines = append(lines, fmt.Sprintf(" %sTusk[-]      %s%s[-]", c, v, a.serverVersion))
	lines = append(lines, fmt.Sprintf(" %sProfile:[-] %s%s[-]", l, v, a.profile))

	uptimeStr := "--"
	if a.serverUptime > 0 {
		uptimeStr = views.FormatDuration(a.serverUptime)
	}
	lines = append(lines, fmt.Sprintf(" %sUptime:[-]  %s%s[-]", l, v, uptimeStr))

	connParts := fmt.Sprintf("%s%d active[-]", g, a.activeConns)
	if a.idleConns > 0 {
		connParts += fmt.Sprintf(" / %s%d idle[-]", y, a.idleConns)
	}
	if a.idleTxnConns > 0 {
		connParts += fmt.Sprintf(" / %s%d idle-in-txn[-]", r, a.idleTxnConns)
	}
	other := a.connCount - a.activeConns - a.idleConns - a.idleTxnConns
	if other > 0 {
		connParts += fmt.Sprintf(" / %d other", other)
	}
	lines = append(lines, fmt.Sprintf(" %sConns:[-]   %s (max: %d)", l, connParts, a.connMax))

	cacheStr := "--"
	if a.cacheHitRatio > 0 {
		ratio := a.cacheHitRatio * 100
		cc := g
		if ratio < 95 {
			cc = r
		} else if ratio < 99 {
			cc = y
		}
		cacheStr = fmt.Sprintf("%s%.2f%%[-]", cc, ratio)
	}
	tpsStr := "--"
	if a.tps > 0 {
		tpsStr = fmt.Sprintf("%d/s", a.tps)
	}
	lines = append(lines, fmt.Sprintf(" %sCache:[-]   %s   %sTPS:[-] %s%s[-]", l, cacheStr, l, v, tpsStr))
	lines = append(lines, "")

	a.header.SetText(strings.Join(lines, "\n"))
}

func (a *App) updateCrumbs() {
	count := 0
	if v, ok := a.viewMap[a.activeView]; ok {
		count = v.ItemCount()
	}
	a.crumbs.SetText(fmt.Sprintf("[#00D7FF::b]%s(%d)[-:-:-]", a.activeView, count))
}

func (a *App) updateStatus() {
	type hint struct{ key, label string }
	hints := []hint{{":", "cmd"}, {"/", "filter"}, {"p", "pause"}}
	switch a.activeView {
	case "queries", "transactions":
		hints = append(hints, hint{"c", "cancel"}, hint{"t", "terminate"})
	case "locks":
		hints = append(hints, hint{"t", "terminate"})
	}

	var parts []string
	for _, h := range hints {
		parts = append(parts, fmt.Sprintf("[#00D7FF]<%s>[-][#808080]%s[-]", h.key, h.label))
	}

	pill := fmt.Sprintf("[#000000:#00D7FF:b] %s [-:-:-]", a.activeView)
	a.status.SetText(fmt.Sprintf(" %s          %s", pill, strings.Join(parts, " ")))
}

func (a *App) startServerInfoPoller() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		a.fetchServerInfo()
		a.app.QueueUpdateDraw(func() {
			a.updateHeader()
			a.updateCrumbs()
		})
		for range ticker.C {
			a.fetchServerInfo()
			a.app.QueueUpdateDraw(func() {
				a.updateHeader()
				a.updateCrumbs()
			})
		}
	}()
}

func (a *App) fetchServerInfo() {
	ctx := context.Background()
	info, err := a.db.GetServerInfo(ctx)
	if err != nil {
		return
	}
	stats, err := a.db.GetDatabaseStats(ctx)
	if err != nil {
		return
	}
	conns, err := a.db.GetConnections(ctx)
	if err != nil {
		return
	}

	var total, active, idle, idleTxn int
	for _, c := range conns {
		total += c.Count
		switch c.State {
		case "active":
			active += c.Count
		case "idle":
			idle += c.Count
		case "idle in transaction", "idle in transaction (aborted)":
			idleTxn += c.Count
		}
	}

	ver := info.Version
	if idx := strings.Index(ver, ","); idx > 0 {
		ver = strings.TrimSpace(ver[:idx])
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.serverVersion = ver
	a.serverUptime = info.Uptime
	a.connCount = total
	a.connMax = info.MaxConnections
	a.activeConns = active
	a.idleConns = idle
	a.idleTxnConns = idleTxn
	a.cacheHitRatio = stats.CacheHitRatio

	now := time.Now()
	xactTotal := stats.XactCommit + stats.XactRollback
	if !a.prevTime.IsZero() && xactTotal > a.prevXactTotal {
		elapsed := now.Sub(a.prevTime).Seconds()
		if elapsed > 0 {
			a.tps = int64(float64(xactTotal-a.prevXactTotal) / elapsed)
		}
	}
	a.prevXactTotal = xactTotal
	a.prevTime = now
}

func (a *App) wireNavigation() {
	// Queries: Enter -> Query Detail
	if qv, ok := a.viewMap["queries"].(*views.Queries); ok {
		qv.Table().SetSelectedFunc(func(row, col int) {
			if q, ok := qv.SelectedQuery(); ok {
				detail := views.NewQueryDetailView(q, a.db, a.queryHistory)
				a.showDetail("query-detail", detail)
			}
		})
	}

	// Connections: Enter -> filtered queries for selected user
	if cv, ok := a.viewMap["connections"].(*views.Connections); ok {
		cv.Table().SetSelectedFunc(func(row, col int) {
			if user, ok := cv.SelectedUser(); ok {
				qv := views.NewQueriesView(a.db)
				qv.SetUserFilter(user)
				a.showFilteredView("queries-"+user, qv)
			}
		})
	}

	// Roles: Enter -> filtered queries for selected role
	if rv, ok := a.viewMap["roles"].(*views.Roles); ok {
		rv.Table().SetSelectedFunc(func(row, col int) {
			if role, ok := rv.SelectedRole(); ok {
				qv := views.NewQueriesView(a.db)
				qv.SetUserFilter(role)
				a.showFilteredView("queries-"+role, qv)
			}
		})
	}

	// Transactions: Enter -> Query Detail
	if tv, ok := a.viewMap["transactions"].(*views.Transactions); ok {
		tv.Table().SetSelectedFunc(func(row, col int) {
			if t, ok := tv.SelectedTransaction(); ok {
				q := db.ActiveQuery{
					PID:      t.PID,
					User:     t.User,
					AppName:  t.AppName,
					State:    t.State,
					Duration: t.QueryDuration,
					Query:    t.Query,
				}
				detail := views.NewQueryDetailView(q, a.db, a.queryHistory)
				a.showDetail("txn-detail", detail)
			}
		})
	}

	// Locks: Enter -> Lock Detail
	if lv, ok := a.viewMap["locks"].(*views.Locks); ok {
		lv.Table().SetSelectedFunc(func(row, col int) {
			if l, ok := lv.SelectedLock(); ok {
				detail := views.NewLockDetailView(l, a.db)
				a.showDetail("lock-detail", detail)
			}
		})
	}

	// Tables: Enter -> Table Detail
	if tv, ok := a.viewMap["tables"].(*views.Tables); ok {
		tv.Table().SetSelectedFunc(func(row, col int) {
			if t, ok := tv.SelectedTable(); ok {
				detail := views.NewTableDetailView(t.Schema, t.Name, a.db, a.app)
				a.showDetail("table-detail", detail)
			}
		})
	}
}

func (a *App) showDetail(name string, detail *tview.TextView) {
	a.pages.AddPage(name, detail, true, true)
	a.viewStack = append(a.viewStack, a.activeView)
	a.activeView = name
	a.app.SetFocus(detail)
	a.updateCrumbs()
	a.updateStatus()
}

func (a *App) showFilteredView(name string, view View) {
	a.pages.AddPage(name, view.Table(), true, true)
	view.Start(a.app)
	a.viewStack = append(a.viewStack, a.activeView)
	a.activeView = name
	a.app.SetFocus(view.Table())
	a.updateCrumbs()
	a.updateStatus()
}

func (a *App) Run() error {
	a.startServerInfoPoller()
	a.app.SetRoot(a.layout, true)
	return a.app.Run()
}
