package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/fraser-isbester/tusk/internal/config"
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
	config   *config.Config

	layout    *tview.Flex
	header    *tview.TextView
	tabBar    *tview.TextView
	status    *tview.TextView
	pages     *tview.Pages
	viewOrder []string

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

	connUser string

	promptActive bool
	promptMode   string
	tuskColorIdx int // cycles for tusk color animation
	filterActive bool
	filterText   string
}

func NewApp(database *db.DB, cfg *config.Config, profileName, profileColor, connUser string, readonly bool, engine *rules.Engine) *App {
	a := &App{
		app:      tview.NewApplication(),
		db:       database,
		profile:  profileName,
		color:    profileColor,
		readonly: readonly,
		config:   cfg,
		connUser: connUser,
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

	a.viewOrder = []string{"queries", "transactions", "sessions", "tables", "locks", "indexes", "rules", "violations"}

	a.tabBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	a.tabBar.SetBackgroundColor(theme.ColorHeaderBg)

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
		AddItem(a.tabBar, 1, 0, false).
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
	if a.engine != nil {
		tv.SetEngine(a.engine)
	}
	a.viewMap["transactions"] = tv

	a.viewMap["sessions"] = views.NewConnectionsView(a.db)
	a.viewMap["tables"] = views.NewTablesView(a.db)
	a.viewMap["locks"] = views.NewLocksView(a.db)
	a.viewMap["indexes"] = views.NewIndexesView(a.db)
	a.viewMap["rules"] = views.NewRulesView(a.engine)
	a.viewMap["violations"] = views.NewViolationsView(a.engine)

	for name, v := range a.viewMap {
		a.pages.AddPage(name, v.Table(), true, false)
	}
}

func (a *App) switchView(name string) {
	if _, ok := a.viewMap[name]; !ok {
		return
	}
	// Clear any active filter when switching views
	if a.filterText != "" {
		a.hideFilter()
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
	a.updateTabBar()
	a.updateStatus()
}

func (a *App) setupKeys() {
	a.app.SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
		// When filter input has focus, handle Esc/Enter
		if a.filterActive {
			switch evt.Key() {
			case tcell.KeyEscape:
				a.hideFilter()
				return nil
			case tcell.KeyEnter:
				// Keep filter box visible, just move focus back to table
				a.filterActive = false
				if v, ok := a.viewMap[a.activeView]; ok {
					a.app.SetFocus(v.Table())
				}
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
			case 'h':
				a.showHelp()
				return nil
			case 'q':
				a.app.Stop()
				return nil
			}
		case tcell.KeyLeft:
			a.cycleTab(-1)
			return nil
		case tcell.KeyRight:
			a.cycleTab(1)
			return nil
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
		a.updateTabBar()
	})

	// Insert filterBox between crumbs and pages
	a.layout.RemoveItem(a.pages)
	a.layout.RemoveItem(a.status)
	a.layout.AddItem(a.filterBox, 3, 0, true)
	a.layout.AddItem(a.pages, 0, 1, false)
	a.layout.AddItem(a.status, 1, 0, false)
	a.app.SetFocus(a.filterInput)
}

func (a *App) hideFilter() {
	a.filterActive = false
	a.filterText = ""
	a.filterInput.SetText("")
	a.layout.RemoveItem(a.filterBox)

	if v, ok := a.viewMap[a.activeView]; ok {
		v.SetFilter("")
		a.app.SetFocus(v.Table())
	}
	a.updateTabBar()
}

// applyFilter shows the filter box pre-populated with text and applies it to the active view.
func (a *App) applyFilter(text string) {
	a.filterText = text
	a.filterInput.SetText(text)

	a.filterInput.SetChangedFunc(func(t string) {
		a.filterText = t
		if v, ok := a.viewMap[a.activeView]; ok {
			v.SetFilter(t)
		}
		a.updateTabBar()
	})

	if v, ok := a.viewMap[a.activeView]; ok {
		v.SetFilter(text)
	}

	// Show filter box if not already visible
	a.layout.RemoveItem(a.filterBox)
	a.layout.RemoveItem(a.pages)
	a.layout.RemoveItem(a.status)
	a.layout.AddItem(a.filterBox, 3, 0, false)
	a.layout.AddItem(a.pages, 0, 1, true)
	a.layout.AddItem(a.status, 1, 0, false)

	// Don't focus the filter input — keep focus on the table
	a.filterActive = false
	a.updateTabBar()
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
		fmt.Sprintf(" %sUser:[-]    %s%s[-]", l, v, a.connUser),
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

func (a *App) cycleTab(dir int) {
	// Find current index in viewOrder
	current := -1
	for i, name := range a.viewOrder {
		if name == a.activeView || strings.HasPrefix(a.activeView, name+"-") {
			current = i
			break
		}
	}
	if current < 0 {
		return
	}
	next := (current + dir + len(a.viewOrder)) % len(a.viewOrder)
	// Skip views that don't exist in the viewMap
	for i := 0; i < len(a.viewOrder); i++ {
		name := a.viewOrder[next]
		if _, ok := a.viewMap[name]; ok {
			a.switchView(name)
			return
		}
		next = (next + dir + len(a.viewOrder)) % len(a.viewOrder)
	}
}

// parentView returns the logical parent view name for the active view.
// Detail pages like "query-detail" map back to "queries", "txn-detail" to "transactions", etc.
func (a *App) parentView() string {
	av := a.activeView
	if strings.HasPrefix(av, "query-detail") || strings.HasPrefix(av, "violation-query") {
		return "queries"
	}
	if strings.HasPrefix(av, "txn-detail") || strings.HasPrefix(av, "violation-txn") {
		return "transactions"
	}
	if strings.HasPrefix(av, "lock-detail") || strings.HasPrefix(av, "violation-lock") {
		return "locks"
	}
	if strings.HasPrefix(av, "table-detail") {
		return "tables"
	}
	if strings.HasPrefix(av, "rule-form") || strings.HasPrefix(av, "delete-confirm") {
		return "rules"
	}
	return av
}

func (a *App) updateTabBar() {
	parent := a.parentView()
	inDetail := parent != a.activeView

	var parts []string
	for _, name := range a.viewOrder {
		if _, ok := a.viewMap[name]; !ok {
			continue
		}
		label := name
		if name == parent {
			count := 0
			if v, ok := a.viewMap[name]; ok {
				count = v.ItemCount()
			}
			if inDetail {
				// Lime green for detail pages
				label = fmt.Sprintf("[#000000:#00D700:b] %s(%d) [-:-:-]", name, count)
			} else {
				// Cyan for normal active view
				label = fmt.Sprintf("[#000000:#00D7FF:b] %s(%d) [-:-:-]", name, count)
			}
		} else {
			label = fmt.Sprintf("[#D78700] %s [-]", name)
		}
		parts = append(parts, label)
	}
	text := strings.Join(parts, "[#D78700]│[-]")
	a.tabBar.SetText(text)
}

func (a *App) updateStatus() {
	type hint struct{ key, label string }
	hints := []hint{{":", "cmd"}, {"/", "filter"}, {"h", "help"}, {"p", "pause"}}
	switch a.activeView {
	case "queries", "transactions":
		hints = append(hints, hint{"c", "cancel"}, hint{"t", "terminate"})
	case "locks":
		hints = append(hints, hint{"t", "terminate"})
	case "rules":
		hints = append(hints, hint{"n", "new"}, hint{"e", "edit"}, hint{"d", "delete"})
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
			a.updateTabBar()
		})
		for range ticker.C {
			a.fetchServerInfo()
			a.evaluateRules()
			a.app.QueueUpdateDraw(func() {
				a.updateHeader()
				a.updateTabBar()
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
				detail := views.NewQueryDetailView(q, a.db, nil, a.app, a.engine, a.showDetail)
				a.showDetail("query-detail", detail)
			}
		})
	}

	// Connections: Enter -> switch to queries view with user filter
	if cv, ok := a.viewMap["sessions"].(*views.Connections); ok {
		cv.Table().SetSelectedFunc(func(row, col int) {
			if user, ok := cv.SelectedUser(); ok {
				a.switchView("queries")
				a.applyFilter(user)
			}
		})
	}

	// Transactions: Enter -> Transaction Detail
	if tv, ok := a.viewMap["transactions"].(*views.Transactions); ok {
		tv.Table().SetSelectedFunc(func(row, col int) {
			if t, ok := tv.SelectedTransaction(); ok {
				detail := views.NewTransactionDetailView(t, a.db, a.queryHistory, a.app, a.engine, a.showDetail)
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

	// Rules: Enter -> violations, n -> new, e -> edit, d -> delete
	if rv, ok := a.viewMap["rules"].(*views.Rules); ok {
		rv.Table().SetSelectedFunc(func(row, col int) {
			if name, ok := rv.SelectedRule(); ok {
				a.switchView("violations")
				a.applyFilter(name)
			}
		})
		rv.Table().SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
			if evt.Key() != tcell.KeyRune {
				return evt
			}
			switch evt.Rune() {
			case 'n':
				a.showRuleForm(nil)
				return nil
			case 'e':
				if name, ok := rv.SelectedRule(); ok && a.config != nil {
					if profile, err := a.config.ResolveProfile(a.profile); err == nil {
						for _, r := range profile.Rules {
							if r.Name == name {
								a.showRuleForm(&r)
								break
							}
						}
					}
				}
				return nil
			case 'd':
				if name, ok := rv.SelectedRule(); ok {
					a.showDeleteConfirm(name)
				}
				return nil
			}
			return evt
		})
	}

	// Violations: Enter -> Resource detail for the violated PID
	if vv, ok := a.viewMap["violations"].(*views.Violations); ok {
		vv.SetOnSelect(func(v rules.Violation) { a.showViolationDetail(v) })
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

func (a *App) showHelp() {
	help := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	help.SetBackgroundColor(tcell.ColorDefault)
	help.SetBorder(true).
		SetBorderColor(theme.ColorBorder).
		SetTitle(" Help ").
		SetTitleColor(theme.ColorLogo)

	k := "[#00D7FF]"
	d := "[#D0D0D0]"
	h := "[#D78700]"
	r := "[-]"

	lines := []string{
		"",
		h + "  Navigation" + r,
		fmt.Sprintf("    %s←/→%s  %sCycle tabs%s", k, r, d, r),
		fmt.Sprintf("    %sEnter%s  %sOpen detail view%s", k, r, d, r),
		fmt.Sprintf("    %sEsc%s    %sBack / close%s", k, r, d, r),
		"",
		h + "  Detail Views" + r,
		fmt.Sprintf("    %sTab%s        %sNext pane%s", k, r, d, r),
		fmt.Sprintf("    %sShift+Tab%s  %sPrevious pane%s", k, r, d, r),
		"",
		h + "  Commands" + r,
		fmt.Sprintf("    %s:%s      %sCommand prompt (type a view name)%s", k, r, d, r),
		fmt.Sprintf("    %s/%s      %sFilter rows%s", k, r, d, r),
		fmt.Sprintf("    %sq%s      %sQuit%s", k, r, d, r),
		"",
		h + "  Query / Transaction Views" + r,
		fmt.Sprintf("    %sc%s      %sCancel query (pg_cancel_backend)%s", k, r, d, r),
		fmt.Sprintf("    %st%s      %sTerminate backend (pg_terminate_backend)%s", k, r, d, r),
		"",
		h + "  Rules View" + r,
		fmt.Sprintf("    %sn%s      %sNew rule%s", k, r, d, r),
		fmt.Sprintf("    %se%s      %sEdit selected rule%s", k, r, d, r),
		fmt.Sprintf("    %sd%s      %sDelete selected rule%s", k, r, d, r),
		"",
		h + "  Sorting (in table views)" + r,
		fmt.Sprintf("    %sShift+Column Key%s  %sSort by column (see column headers)%s", k, r, d, r),
		"",
		fmt.Sprintf("    %sPress Esc or h to close%s", "[#808080]", r),
		"",
	}
	help.SetText(strings.Join(lines, "\n"))

	// Center the help panel
	modal := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(help, 60, 0, true).
			AddItem(nil, 0, 1, false),
			len(lines)+2, 0, true).
		AddItem(nil, 0, 1, false)
	modal.SetBackgroundColor(tcell.ColorDefault)

	a.showDetail("help-detail", modal)
}

func (a *App) popDetail(name string) {
	a.pages.RemovePage(name)
	if len(a.viewStack) > 0 {
		prev := a.viewStack[len(a.viewStack)-1]
		a.viewStack = a.viewStack[:len(a.viewStack)-1]
		a.activeView = prev
		a.switchView(prev)
	}
}

func (a *App) showRuleForm(existing *rules.RuleConfig) {
	form := views.NewRuleForm(a.app, a.readonly, existing, func(cfg rules.RuleConfig) {
		a.saveRule(cfg, existing)
		a.popDetail("rule-form-detail")
	}, func() {
		a.popDetail("rule-form-detail")
	})
	a.showDetail("rule-form-detail", form)
}

func (a *App) saveRule(cfg rules.RuleConfig, existing *rules.RuleConfig) {
	if a.config == nil {
		return
	}
	profile, err := a.config.ResolveProfile(a.profile)
	if err != nil {
		return
	}

	if existing != nil {
		for i, r := range profile.Rules {
			if r.Name == existing.Name {
				profile.Rules[i] = cfg
				break
			}
		}
	} else {
		profile.Rules = append(profile.Rules, cfg)
	}

	a.config.Profiles[a.profile] = profile
	_ = a.config.Save()
	a.recompileAndSwap()
}

func (a *App) deleteRule(name string) {
	if a.config == nil {
		return
	}
	profile, err := a.config.ResolveProfile(a.profile)
	if err != nil {
		return
	}

	for i, r := range profile.Rules {
		if r.Name == name {
			profile.Rules = append(profile.Rules[:i], profile.Rules[i+1:]...)
			break
		}
	}

	a.config.Profiles[a.profile] = profile
	_ = a.config.Save()
	a.recompileAndSwap()
}

func (a *App) recompileAndSwap() {
	if a.config == nil {
		return
	}
	profile, err := a.config.ResolveProfile(a.profile)
	if err != nil {
		return
	}
	compiled, err := rules.BuildRules(profile.Rules, a.readonly)
	if err != nil {
		return
	}

	if a.engine == nil {
		a.engine = rules.NewEngine(compiled, a.db, 5*time.Minute, 1000)
		if qv, ok := a.viewMap["queries"].(*views.Queries); ok {
			qv.SetEngine(a.engine)
		}
		if tv, ok := a.viewMap["transactions"].(*views.Transactions); ok {
			tv.SetEngine(a.engine)
		}
		if rv, ok := a.viewMap["rules"].(*views.Rules); ok {
			rv.SetEngine(a.engine)
		}
		if vv, ok := a.viewMap["violations"].(*views.Violations); ok {
			vv.SetEngine(a.engine)
		}
	} else {
		a.engine.UpdateRules(compiled)
	}
}

func (a *App) showDeleteConfirm(name string) {
	confirm := tview.NewModal().
		SetText(fmt.Sprintf("Delete rule '%s'?", name)).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			a.pages.RemovePage("delete-confirm-detail")
			if len(a.viewStack) > 0 {
				prev := a.viewStack[len(a.viewStack)-1]
				a.viewStack = a.viewStack[:len(a.viewStack)-1]
				a.activeView = prev
			}
			if buttonLabel == "Delete" {
				a.deleteRule(name)
			}
			a.switchView("rules")
		})
	confirm.SetBackgroundColor(tcell.ColorDefault)
	a.showDetail("delete-confirm-detail", confirm)
}

func (a *App) showDetail(name string, detail tview.Primitive) {
	a.pages.AddPage(name, detail, true, true)
	a.viewStack = append(a.viewStack, a.activeView)
	a.activeView = name
	a.app.SetFocus(detail)
	a.updateTabBar()
	a.updateStatus()
}

func (a *App) showViolationDetail(v rules.Violation) {
	ctx := context.Background()
	switch v.ResourceType {
	case rules.ResourceQuery:
		if queries, err := a.db.GetActiveQueries(ctx); err == nil {
			for _, q := range queries {
				if q.PID == v.PID {
					detail := views.NewQueryDetailView(q, a.db, nil, a.app, a.engine, a.showDetail)
					a.showDetail("violation-query-detail", detail)
					return
				}
			}
		}
		if v.QuerySnap != nil {
			q := *v.QuerySnap
			q.State = "completed"
			detail := views.NewQueryDetailView(q, a.db, nil, a.app, a.engine, a.showDetail)
			a.showDetail("violation-query-detail", detail)
		}
	case rules.ResourceTransaction:
		if txns, err := a.db.GetTransactions(ctx); err == nil {
			for _, t := range txns {
				if t.PID == v.PID {
					detail := views.NewTransactionDetailView(t, a.db, a.queryHistory, a.app, a.engine, a.showDetail)
					a.showDetail("violation-txn-detail", detail)
					return
				}
			}
		}
		if v.TransactionSnap != nil {
			t := *v.TransactionSnap
			t.State = "completed"
			detail := views.NewTransactionDetailView(t, a.db, a.queryHistory, a.app, a.engine, a.showDetail)
			a.showDetail("violation-txn-detail", detail)
		}
	case rules.ResourceLock:
		if locks, err := a.db.GetLocks(ctx); err == nil {
			for _, l := range locks {
				if l.BlockedPID == v.PID {
					detail := views.NewLockDetailView(l, a.db)
					a.showDetail("violation-lock-detail", detail)
					return
				}
			}
		}
		if v.LockSnap != nil {
			detail := views.NewLockDetailView(*v.LockSnap, a.db)
			a.showDetail("violation-lock-detail", detail)
		}
	}
}

func (a *App) Run() error {
	a.startServerInfoPoller()
	a.app.SetRoot(a.layout, true)
	return a.app.Run()
}
