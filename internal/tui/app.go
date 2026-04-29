package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/components"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
	"github.com/fraser-isbester/tusk/internal/tui/views"
)

type viewModel interface {
	tea.Model
	SetSize(w, h int)
}

type serverInfoMsg struct {
	version     string
	uptime      time.Duration
	connections int
	maxConns    int
	activeConns int
	idleConns   int
	idleTxnConns int
	cacheHitRatio float64
	xactTotal   int64
	err         error
}

type serverInfoTickMsg struct{}

type App struct {
	db       *db.DB
	profile  string
	color    string
	readonly bool

	width  int
	height int

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

	views      map[string]viewModel
	activeView string
	viewStack  []string

	filter  components.Filter
	help    components.Help
	confirm *components.ConfirmDialog

	commandInput textinput.Model
	commandMode  bool

	registry *CommandRegistry
}

func NewApp(database *db.DB, profileName, profileColor string, readonly bool) *App {
	ci := textinput.New()
	ci.Prompt = ":"
	ci.Placeholder = ""
	ci.CharLimit = 64

	a := &App{
		db:           database,
		profile:      profileName,
		color:        profileColor,
		readonly:     readonly,
		views:        make(map[string]viewModel),
		registry:     NewCommandRegistry(),
		filter:       components.NewFilter(),
		help:         components.NewHelp(),
		commandInput: ci,
	}

	a.views["queries"] = views.NewQueries(database)
	a.views["tables"] = views.NewTables(database)
	a.views["connections"] = views.NewConnections(database)
	a.views["db"] = views.NewDatabases(database)
	a.views["roles"] = views.NewRoles(database)
	a.views["slow"] = views.NewSlowQueries(database)
	a.views["transactions"] = views.NewTransactions(database)
	a.views["locks"] = views.NewLocks(database)
	a.views["indexes"] = views.NewIndexes(database)

	a.activeView = "queries"

	a.help.SetBindings([]components.HelpBinding{
		{Key: ":", Description: "command mode"},
		{Key: "/", Description: "filter"},
		{Key: "?", Description: "help"},
		{Key: "j/k", Description: "up / down"},
		{Key: "g/G", Description: "top / bottom"},
		{Key: "Esc", Description: "back"},
		{Key: "p", Description: "pause / resume"},
		{Key: "c", Description: "cancel query"},
		{Key: "t", Description: "terminate backend"},
		{Key: "q", Description: "quit"},
	})

	return a
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.views[a.activeView].Init(),
		a.fetchServerInfo(),
	)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.resizeViews()
		f, _ := a.filter.Update(msg)
		a.filter = f
		hp, _ := a.help.Update(msg)
		a.help = hp
		return a, nil

	case serverInfoMsg:
		if msg.err == nil {
			a.serverVersion = msg.version
			a.serverUptime = msg.uptime
			a.connCount = msg.connections
			a.connMax = msg.maxConns
			a.activeConns = msg.activeConns
			a.idleConns = msg.idleConns
			a.idleTxnConns = msg.idleTxnConns
			a.cacheHitRatio = msg.cacheHitRatio

			// Compute TPS
			now := time.Now()
			if !a.prevTime.IsZero() && msg.xactTotal > a.prevXactTotal {
				elapsed := now.Sub(a.prevTime).Seconds()
				if elapsed > 0 {
					a.tps = int64(float64(msg.xactTotal-a.prevXactTotal) / elapsed)
				}
			}
			a.prevXactTotal = msg.xactTotal
			a.prevTime = now
		}
		return a, a.tickServerInfo()

	case serverInfoTickMsg:
		return a, a.fetchServerInfo()

	case components.FilterMsg:
		return a, nil

	case components.FilterCancelMsg:
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)
	}

	// Forward non-key messages to active view.
	if view, ok := a.views[a.activeView]; ok {
		updated, cmd := view.Update(msg)
		a.views[a.activeView] = updated.(viewModel)
		return a, cmd
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Overlays eat all keys first.
	if a.help.Visible() {
		hp, cmd := a.help.Update(msg)
		a.help = hp
		return a, cmd
	}
	if a.confirm != nil && a.confirm.Active() {
		c, cmd := a.confirm.Update(msg)
		a.confirm = &c
		return a, cmd
	}
	if a.filter.Active() {
		f, cmd := a.filter.Update(msg)
		a.filter = f
		return a, cmd
	}

	// Command mode.
	if a.commandMode {
		switch msg.Type {
		case tea.KeyEnter:
			input := a.commandInput.Value()
			a.commandMode = false
			a.commandInput.Blur()
			a.commandInput.SetValue("")
			if viewName, ok := a.registry.Match(input); ok {
				return a, a.switchView(viewName)
			}
			return a, nil
		case tea.KeyEscape:
			a.commandMode = false
			a.commandInput.Blur()
			a.commandInput.SetValue("")
			return a, nil
		default:
			var cmd tea.Cmd
			a.commandInput, cmd = a.commandInput.Update(msg)
			return a, cmd
		}
	}

	// Global keys — only intercept specific ones, let everything else
	// (arrows, j/k, etc.) fall through to the active view.
	switch msg.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "q":
		return a, tea.Quit
	case ":":
		a.commandMode = true
		a.commandInput.SetValue("")
		a.commandInput.Focus()
		return a, textinput.Blink
	case "/":
		a.filter.Activate()
		return a, a.filter.Init()
	case "?":
		a.help.Toggle()
		return a, nil
	case "esc":
		if len(a.viewStack) > 0 {
			prev := a.viewStack[len(a.viewStack)-1]
			a.viewStack = a.viewStack[:len(a.viewStack)-1]
			return a, a.switchView(prev)
		}
		return a, nil
	case "enter":
		switch v := a.views[a.activeView].(type) {
		case *views.Queries:
			if q, ok := v.SelectedQuery(); ok {
				a.views["query-detail"] = views.NewQueryDetail(q)
				return a, a.switchView("query-detail")
			}
		case *views.Connections:
			if user, ok := v.SelectedUser(); ok {
				qv := views.NewQueries(a.db)
				qv.SetUserFilter(user)
				qv.SetSize(a.width, a.height-6)
				a.views["queries-filtered"] = qv
				return a, a.switchView("queries-filtered")
			}
		case *views.Transactions:
			if q, ok := v.SelectedQuery(); ok {
				a.views["query-detail"] = views.NewQueryDetail(q)
				return a, a.switchView("query-detail")
			}
		}
	}

	// Everything else goes to the active view (arrows, j, k, g, G, p, c, t, etc.)
	if view, ok := a.views[a.activeView]; ok {
		updated, cmd := view.Update(msg)
		a.views[a.activeView] = updated.(viewModel)
		return a, cmd
	}
	return a, nil
}

// View renders the full k9s-style layout.
func (a *App) View() string {
	if a.help.Visible() {
		return a.help.View()
	}
	if a.confirm != nil && a.confirm.Active() {
		return a.confirm.View()
	}

	w := a.width
	if w <= 0 {
		w = 80
	}
	h := a.height
	if h <= 0 {
		h = 24
	}

	// Top: info header (multi-line, like k9s context block)
	header := a.renderHeader(w)
	headerLines := strings.Count(header, "\n") + 1

	// Crumbs line
	crumbs := a.renderCrumbs(w)

	// Filter/command bar (between crumbs and view content, like k9s)
	var filterBar string
	if a.commandMode {
		filterBar = theme.Status.Width(w).Render(a.commandInput.View())
	} else if a.filter.Active() {
		filterBar = a.filter.View()
	} else {
		filterBar = ""
	}

	// Status bar always at bottom
	statusBar := a.renderStatus(w)

	// View content fills remaining space
	// chrome = header + crumbs + filter bar + status bar
	viewHeight := h - headerLines - 3 // crumbs + filter line + status
	if viewHeight < 1 {
		viewHeight = 1
	}

	var viewContent string
	if view, ok := a.views[a.activeView]; ok {
		viewContent = view.View()
	}

	// Pad/truncate to exactly fill
	viewLines := strings.Split(viewContent, "\n")
	if len(viewLines) > viewHeight {
		viewLines = viewLines[:viewHeight]
	}
	for len(viewLines) < viewHeight {
		viewLines = append(viewLines, "")
	}
	middle := strings.Join(viewLines, "\n")

	return header + "\n" + crumbs + "\n" + filterBar + "\n" + middle + "\n" + statusBar
}

// renderHeader renders k9s-style info lines at top with ASCII tusk art.
func (a *App) renderHeader(w int) string {
	lbl := lipgloss.NewStyle().Foreground(theme.ColorLabel)
	val := lipgloss.NewStyle().Foreground(theme.ColorFgBright)
	logoStyle := lipgloss.NewStyle().Foreground(theme.ColorLogo).Bold(true)
	tuskArt := lipgloss.NewStyle().Foreground(theme.ColorLogo)
	green := lipgloss.NewStyle().Foreground(theme.ColorGreen)
	yellow := lipgloss.NewStyle().Foreground(theme.ColorYellow)
	red := lipgloss.NewStyle().Foreground(theme.ColorRed)
	hk := theme.HintKey
	hl := theme.HintLabel

	art := []string{`  __,,  `, ` ( o\   `, `  \  \  `, `   )  ) `, `        `, `        `}
	hints := []string{
		hk.Render("<:>") + hl.Render(" Command"),
		hk.Render("<?>") + hl.Render(" Help"),
		hk.Render("</>") + hl.Render(" Filter"),
		hk.Render("<p>") + hl.Render(" Pause"),
		"",
		"",
	}

	// Build connection string with color-coded breakdown
	connStr := ""
	if a.connMax > 0 {
		parts := []string{green.Render(fmt.Sprintf("%d active", a.activeConns))}
		if a.idleConns > 0 {
			parts = append(parts, yellow.Render(fmt.Sprintf("%d idle", a.idleConns)))
		}
		if a.idleTxnConns > 0 {
			parts = append(parts, red.Render(fmt.Sprintf("%d idle-in-txn", a.idleTxnConns)))
		}
		other := a.connCount - a.activeConns - a.idleConns - a.idleTxnConns
		if other > 0 {
			parts = append(parts, val.Render(fmt.Sprintf("%d other", other)))
		}
		connStr = strings.Join(parts, val.Render(" / "))
	}

	// Cache hit ratio
	cacheStr := "--"
	if a.cacheHitRatio > 0 {
		ratio := a.cacheHitRatio * 100
		cacheStr = fmt.Sprintf("%.2f%%", ratio)
		switch {
		case ratio >= 99:
			cacheStr = green.Render(cacheStr)
		case ratio >= 95:
			cacheStr = yellow.Render(cacheStr)
		default:
			cacheStr = red.Render(cacheStr)
		}
	}

	// TPS
	tpsStr := "--"
	if a.tps > 0 {
		tpsStr = val.Render(fmt.Sprintf("%d/s", a.tps))
	}

	uptimeStr := "--"
	if a.serverUptime > 0 {
		uptimeStr = views.FormatDuration(a.serverUptime)
	}

	infoLines := []string{
		" " + logoStyle.Render("Tusk") + "      " + val.Render(a.serverVersion),
		fmt.Sprintf(" %s %s", lbl.Render("Profile:"), val.Render(a.profile)),
		fmt.Sprintf(" %s %s", lbl.Render("Uptime:"), val.Render(uptimeStr)),
		fmt.Sprintf(" %s %s (max: %d)", lbl.Render("Conns:"), connStr, a.connMax),
		fmt.Sprintf(" %s %s   %s %s", lbl.Render("Cache:"), cacheStr, lbl.Render("TPS:"), tpsStr),
		"",
	}

	lines := make([]string, 6)
	artWidth := 9
	for i := range 6 {
		left := infoLines[i]
		center := hints[i]
		right := tuskArt.Render(art[i])

		leftW := lipgloss.Width(left)
		centerW := lipgloss.Width(center)

		centerCol := 48
		if centerCol < leftW+2 {
			centerCol = leftW + 2
		}
		gapLeft := centerCol - leftW
		if gapLeft < 1 {
			gapLeft = 1
		}
		gapRight := w - centerCol - centerW - artWidth
		if gapRight < 1 {
			gapRight = 1
		}

		lines[i] = left + strings.Repeat(" ", gapLeft) + center + strings.Repeat(" ", gapRight) + right
	}

	return strings.Join(lines, "\n")
}

// renderCrumbs renders a centered decorative crumbs line with box-drawing chars.
func (a *App) renderCrumbs(w int) string {
	viewName := a.activeView
	count := a.viewItemCount()

	label := fmt.Sprintf(" %s(%d) ", viewName, count)
	labelStyled := theme.CrumbsHighlight.Render(fmt.Sprintf(" %s(%d) ", viewName, count))
	labelW := lipgloss.Width(label)

	dimLine := lipgloss.NewStyle().Foreground(theme.ColorDim)
	totalDash := w - labelW
	if totalDash < 2 {
		totalDash = 2
	}
	leftDash := totalDash / 2
	rightDash := totalDash - leftDash

	line := dimLine.Render(strings.Repeat("─", leftDash)) + labelStyled + dimLine.Render(strings.Repeat("─", rightDash))
	return theme.Crumbs.Width(w).Render(line)
}

// renderStatus renders the bottom k9s-style status bar with pill indicators.
func (a *App) renderStatus(w int) string {
	// Pill-style resource indicators on the left
	activePill := lipgloss.NewStyle().
		Background(theme.ColorLogo).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1).
		Render(a.activeView)

	var pills string
	if len(a.viewStack) > 0 {
		prev := a.viewStack[len(a.viewStack)-1]
		dimPill := lipgloss.NewStyle().
			Background(lipgloss.Color("#404040")).
			Foreground(theme.ColorDim).
			Padding(0, 1).
			Render(prev)
		pills = " " + dimPill + " " + activePill
	} else {
		pills = " " + activePill
	}

	// Key hints on the right
	type hint struct{ key, label string }
	hints := []hint{
		{":", "cmd"},
		{"/", "filter"},
		{"?", "help"},
		{"p", "pause"},
	}
	switch a.activeView {
	case "queries":
		hints = append(hints, hint{"c", "cancel"}, hint{"t", "terminate"})
	}

	var parts []string
	for _, h := range hints {
		parts = append(parts, theme.HintKey.Render("<"+h.key+">")+theme.HintLabel.Render(h.label))
	}
	right := strings.Join(parts, " ") + " "

	rightW := lipgloss.Width(right)
	pillsW := lipgloss.Width(pills)
	gap := w - pillsW - rightW
	if gap < 1 {
		gap = 1
	}

	line := pills + strings.Repeat(" ", gap) + right
	return theme.Status.Width(w).Render(line)
}

// viewItemCount returns the number of items in the active view's data.
func (a *App) viewItemCount() int {
	// We'll use a type assertion approach
	switch v := a.views[a.activeView].(type) {
	case *views.Queries:
		return v.ItemCount()
	case *views.Tables:
		return v.ItemCount()
	case *views.Connections:
		return v.ItemCount()
	case *views.Databases:
		return v.ItemCount()
	case *views.Roles:
		return v.ItemCount()
	case *views.SlowQueries:
		return v.ItemCount()
	case *views.Transactions:
		return v.ItemCount()
	case *views.Locks:
		return v.ItemCount()
	case *views.Indexes:
		return v.ItemCount()
	}
	return 0
}

func (a *App) switchView(name string) tea.Cmd {
	if _, ok := a.views[name]; !ok {
		return nil
	}
	if a.activeView != name {
		a.viewStack = append(a.viewStack, a.activeView)
	}
	a.activeView = name
	a.resizeViews()
	return a.views[name].Init()
}

func (a *App) resizeViews() {
	// Header is 6 lines, crumbs is 1, filter bar is 1, status is 1 = 9 chrome lines
	viewHeight := a.height - 9
	if viewHeight < 1 {
		viewHeight = 1
	}
	for _, v := range a.views {
		v.SetSize(a.width, viewHeight)
	}
}

func (a *App) fetchServerInfo() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		info, err := a.db.GetServerInfo(ctx)
		if err != nil {
			return serverInfoMsg{err: err}
		}
		stats, err := a.db.GetDatabaseStats(ctx)
		if err != nil {
			return serverInfoMsg{err: err}
		}
		conns, err := a.db.GetConnections(ctx)
		if err != nil {
			return serverInfoMsg{err: err}
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

		return serverInfoMsg{
			version:       ver,
			uptime:        info.Uptime,
			connections:   total,
			maxConns:      info.MaxConnections,
			activeConns:   active,
			idleConns:     idle,
			idleTxnConns:  idleTxn,
			cacheHitRatio: stats.CacheHitRatio,
			xactTotal:     stats.XactCommit + stats.XactRollback,
		}
	}
}

func (a *App) tickServerInfo() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return serverInfoTickMsg{}
	})
}
