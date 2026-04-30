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
	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
	"github.com/fraser-isbester/tusk/internal/tui/views"
)

// View is the interface all resource views implement.
type View interface {
	Table() *tview.Table
	Start(app *tview.Application)
	Stop()
	ItemCount() int
	SetFilter(text string)
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

	cmdPrompt   *tview.InputField
	cmdBox      *tview.Flex
	filterInput *tview.InputField
	filterBox   *tview.Flex

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

	engine       *rules.Engine

	promptActive bool
	promptMode   string
	tuskColorIdx int // cycles for tusk color animation
	filterActive bool
	filterText   string
}

func NewApp(database *db.DB, profileName, profileColor string, readonly bool, engine *rules.Engine) *App {
	a := &App{
		app:      tview.NewApplication(),
		db:       database,
		profile:  profileName,
		color:    profileColor,
		readonly: readonly,
		engine:   engine,
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

	a.cmdPrompt = tview.NewInputField().
		SetLabel(": ").
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetLabelColor(theme.ColorLogo)
	a.cmdPrompt.SetBackgroundColor(tcell.ColorDefault)

	a.cmdBox = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.cmdPrompt, 1, 0, true)
	a.cmdBox.SetBorder(true).
		SetBorderColor(theme.ColorBorder).
		SetBackgroundColor(tcell.ColorDefault)

	a.filterInput = tview.NewInputField().
		SetLabel("/ ").
		SetLabelColor(theme.ColorLogo).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(tcell.ColorWhite)
	a.filterInput.SetBackgroundColor(tcell.ColorDefault)

	a.filterBox = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.filterInput, 1, 0, true)
	a.filterBox.SetBorder(true).
		SetBorderColor(theme.ColorBorder).
		SetBackgroundColor(tcell.ColorDefault)

	a.pages = tview.NewPages()
	a.pages.SetBackgroundColor(tcell.ColorDefault)

	a.layout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 8, 0, false).
		AddItem(a.crumbs, 1, 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.status, 1, 0, false)
	a.layout.SetBackgroundColor(tcell.ColorDefault)
}

func (a *App) registerViews() {
	qv := views.NewQueriesView(a.db)
	qv.SetQueryHistory(a.queryHistory)
	if a.engine != nil {
		qv.SetEngine(a.engine)
	}
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
	a.viewMap["rules"] = views.NewRulesView(a.engine)
	a.viewMap["breaches"] = views.NewBreachesView(a.engine)

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
		// When filter input is active, handle Esc/Enter specially
		if a.filterActive {
			switch evt.Key() {
			case tcell.KeyEscape:
				a.hideFilter(true)
				return nil
			case tcell.KeyEnter:
				a.hideFilter(false)
				return nil
			}
			return evt
		}

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
				a.showFilter()
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
	a.cmdPrompt.SetText("")

	a.layout.RemoveItem(a.pages)
	a.layout.RemoveItem(a.status)
	a.layout.AddItem(a.cmdBox, 3, 0, true)
	a.layout.AddItem(a.pages, 0, 1, false)
	a.layout.AddItem(a.status, 1, 0, false)
	a.app.SetFocus(a.cmdPrompt)

	a.cmdPrompt.SetDoneFunc(func(key tcell.Key) {
		text := a.cmdPrompt.GetText()
		a.hidePrompt()
		if key == tcell.KeyEnter && text != "" {
			if viewName, ok := a.registry.Match(text); ok {
				a.switchView(viewName)
			}
		}
	})
}

func (a *App) hidePrompt() {
	a.promptActive = false
	a.layout.RemoveItem(a.cmdBox)
	if v, ok := a.viewMap[a.activeView]; ok {
		a.app.SetFocus(v.Table())
	}
}

func (a *App) showFilter() {
	a.filterActive = true
	a.filterInput.SetText("")
	a.filterText = ""

	a.filterInput.SetChangedFunc(func(text string) {
		a.filterText = text
		if v, ok := a.viewMap[a.activeView]; ok {
			v.SetFilter(text)
		}
		a.updateCrumbs()
	})

	// Insert filterBox between crumbs and pages
	a.layout.RemoveItem(a.pages)
	a.layout.RemoveItem(a.status)
	a.layout.AddItem(a.filterBox, 3, 0, true)
	a.layout.AddItem(a.pages, 0, 1, false)
	a.layout.AddItem(a.status, 1, 0, false)
	a.app.SetFocus(a.filterInput)
}

func (a *App) hideFilter(clear bool) {
	a.filterActive = false
	a.layout.RemoveItem(a.filterBox)

	if clear {
		a.filterText = ""
		a.filterInput.SetText("")
		if v, ok := a.viewMap[a.activeView]; ok {
			v.SetFilter("")
		}
	}

	a.updateCrumbs()
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

	// TUSK in big open letters (figlet "big" style)
	o := "[#D78700]"
	logo := []string{
		o + tview.Escape("  _______  _    _   _____  _  __") + "[-]",
		o + tview.Escape(" |__   __|| |  | | / ____|| |/ /") + "[-]",
		o + tview.Escape("    | |   | |  | || (___  | ' / ") + "[-]",
		o + tview.Escape("    | |   | |  | | \\___ \\ |  <  ") + "[-]",
		o + tview.Escape("    | |   | |__| | ____) || . \\ ") + "[-]",
		o + tview.Escape("    |_|    \\____/ |_____/ |_|\\_\\") + "[-]",
	}

	uptimeStr := "--"
	if a.serverUptime > 0 {
		uptimeStr = views.FormatDuration(a.serverUptime)
	}

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

	infoLines := []string{
		"",
		fmt.Sprintf(" %sTusk[-]      %s%s[-]", c, v, a.serverVersion),
		fmt.Sprintf(" %sProfile:[-] %s%s[-]", l, v, a.profile),
		fmt.Sprintf(" %sUptime:[-]  %s%s[-]", l, v, uptimeStr),
		fmt.Sprintf(" %sConns:[-]   %s (max: %d)", l, connParts, a.connMax),
		fmt.Sprintf(" %sCache:[-]   %s   %sTPS:[-] %s%s[-]", l, cacheStr, l, v, tpsStr),
		"",
		"",
	}

	// Get terminal width for right-justification
	_, _, headerWidth, _ := a.header.GetInnerRect()
	if headerWidth <= 0 {
		headerWidth = 120
	}
	logoWidth := 33 // visual width of the big letters

	var lines []string
	for i := 0; i < len(infoLines) || i < len(logo); i++ {
		left := ""
		if i < len(infoLines) {
			left = infoLines[i]
		}
		if i < len(logo) {
			leftVisual := tview.TaggedStringWidth(left)
			pad := headerWidth - leftVisual - logoWidth
			if pad < 2 {
				pad = 2
			}
			lines = append(lines, left+strings.Repeat(" ", pad)+logo[i])
		} else {
			lines = append(lines, left)
		}
	}

	a.header.SetText(strings.Join(lines, "\n"))
}

func (a *App) updateCrumbs() {
	count := 0
	if v, ok := a.viewMap[a.activeView]; ok {
		count = v.ItemCount()
	}
	text := fmt.Sprintf("[#00D7FF::b]%s(%d)[-:-:-]", a.activeView, count)
	if a.filterText != "" {
		text += fmt.Sprintf(" [#808080]filter: %s[-]", a.filterText)
	}
	a.crumbs.SetText(text)
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
		a.evaluateRules()
		a.app.QueueUpdateDraw(func() {
			a.updateHeader()
			a.updateCrumbs()
		})
		for range ticker.C {
			a.fetchServerInfo()
			a.evaluateRules()
			a.app.QueueUpdateDraw(func() {
				a.updateHeader()
				a.updateCrumbs()
			})
		}
	}()
}

func (a *App) evaluateRules() {
	if a.engine == nil {
		return
	}
	ctx := context.Background()
	queries, _ := a.db.GetActiveQueries(ctx)
	txns, _ := a.db.GetTransactions(ctx)
	locks, _ := a.db.GetLocks(ctx)
	a.engine.Evaluate(ctx, rules.Snapshot{
		Queries:      queries,
		Transactions: txns,
		Locks:        locks,
	})
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
	// Queries: Enter -> Query Detail (no transaction history — that's for :txn)
	if qv, ok := a.viewMap["queries"].(*views.Queries); ok {
		qv.Table().SetSelectedFunc(func(row, col int) {
			if q, ok := qv.SelectedQuery(); ok {
				detail := views.NewQueryDetailView(q, a.db, nil, a.app)
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
				q := db.Query{
					ResourceBase: db.ResourceBase{
						PID:   t.PID,
						User:  t.User,
						App:   t.App,
						State: t.State,
					},
					Duration:  t.QueryDuration,
					QueryText: t.QueryText,
				}
				detail := views.NewQueryDetailView(q, a.db, a.queryHistory, a.app)
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

	// Rules: Enter -> Breaches filtered by selected rule
	if rv, ok := a.viewMap["rules"].(*views.Rules); ok {
		rv.Table().SetSelectedFunc(func(row, col int) {
			if name, ok := rv.SelectedRule(); ok {
				bv := views.NewBreachesView(a.engine)
				bv.SetRuleFilter(name)
				bv.SetOnSelect(func(b rules.Breach) { a.showBreachDetail(b) })
				a.showFilteredView("breaches-"+name, bv)
			}
		})
	}

	// Breaches: Enter -> Resource detail for the breached PID
	if bv, ok := a.viewMap["breaches"].(*views.Breaches); ok {
		bv.SetOnSelect(func(b rules.Breach) { a.showBreachDetail(b) })
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

func (a *App) showBreachDetail(b rules.Breach) {
	ctx := context.Background()
	switch b.ResourceType {
	case rules.ResourceQuery:
		// Try live data first, fall back to snapshot
		if queries, err := a.db.GetActiveQueries(ctx); err == nil {
			for _, q := range queries {
				if q.PID == b.PID {
					detail := views.NewQueryDetailView(q, a.db, nil, a.app)
					a.showDetail("breach-query-detail", detail)
					return
				}
			}
		}
		if b.QuerySnap != nil {
			q := *b.QuerySnap
			if q.State != "completed" {
				q.State = "completed"
			}
			detail := views.NewQueryDetailView(q, a.db, nil, a.app)
			a.showDetail("breach-query-detail", detail)
		}
	case rules.ResourceTransaction:
		if txns, err := a.db.GetTransactions(ctx); err == nil {
			for _, t := range txns {
				if t.PID == b.PID {
					q := db.Query{
						ResourceBase: db.ResourceBase{
							PID: t.PID, User: t.User, App: t.App, State: t.State,
						},
						Duration: t.QueryDuration, QueryText: t.QueryText,
					}
					detail := views.NewQueryDetailView(q, a.db, a.queryHistory, a.app)
					a.showDetail("breach-txn-detail", detail)
					return
				}
			}
		}
		if b.TransactionSnap != nil {
			t := *b.TransactionSnap
			q := db.Query{
				ResourceBase: db.ResourceBase{
					PID: t.PID, User: t.User, App: t.App, State: "completed",
				},
				Duration: t.QueryDuration, QueryText: t.QueryText,
			}
			detail := views.NewQueryDetailView(q, a.db, a.queryHistory, a.app)
			a.showDetail("breach-txn-detail", detail)
		}
	case rules.ResourceLock:
		if locks, err := a.db.GetLocks(ctx); err == nil {
			for _, l := range locks {
				if l.BlockedPID == b.PID {
					detail := views.NewLockDetailView(l, a.db)
					a.showDetail("breach-lock-detail", detail)
					return
				}
			}
		}
		if b.LockSnap != nil {
			detail := views.NewLockDetailView(*b.LockSnap, a.db)
			a.showDetail("breach-lock-detail", detail)
		}
	}
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
