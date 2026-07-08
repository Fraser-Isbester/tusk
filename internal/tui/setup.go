package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/fraser-isbester/tusk/internal/config"
	"github.com/fraser-isbester/tusk/internal/connect"
	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

// SetupResult is a working connection produced by the first-run setup screen.
type SetupResult struct {
	DB          *db.DB
	Closer      func() error // tears down any tunnel; call after DB.Close
	ProfileName string
	Profile     config.Profile
}

// RunSetup shows a first-run connection screen and returns a validated, live
// connection once the user connects successfully. It returns ok=false if the
// user quits without connecting. reason explains why setup was launched (e.g.
// "profile \"dev\" not found") and is shown at the top.
func RunSetup(cfg *config.Config, reason string) (*SetupResult, bool) {
	app := tview.NewApplication()
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.ColorDefault)
	form.SetFieldBackgroundColor(tcell.NewRGBColor(0x1a, 0x1a, 0x1a))
	form.SetFieldTextColor(tcell.ColorWhite)
	form.SetLabelColor(theme.ColorLabel)
	form.SetButtonBackgroundColor(theme.ColorBorder)
	form.SetButtonTextColor(tcell.ColorWhite)
	form.SetBorder(true).SetBorderColor(theme.ColorBorder)
	form.SetTitle(" Connect to PostgreSQL ").SetTitleColor(theme.ColorLogo)

	status := tview.NewTextView().SetDynamicColors(true)
	status.SetBackgroundColor(tcell.ColorDefault)
	if reason != "" {
		status.SetText(fmt.Sprintf("  [#D78700]%s — set up a connection to continue[-]", reason))
	} else {
		status.SetText("  [#808080]Enter connection details, then Connect[-]")
	}

	methods := []string{"direct", "kube-port-forward", "exec"}

	// Field values, kept in closure vars so the form can be rebuilt per method
	// without losing input. Default the profile name to the one that was
	// expected but unusable, so saving fixes the dangling default_profile.
	name := cfg.DefaultProfile
	if name == "" {
		name = "default"
	}
	method := "direct"
	url := ""
	kctx, kns, ktarget, kremote := "", "", "", "5432"
	command := ""
	user, pass, dbname, sslmode := "", "", "", "disable"
	save := true

	var result *SetupResult
	var rebuild func()

	setStatus := func(color, msg string) { status.SetText(fmt.Sprintf("  [%s]%s[-]", color, msg)) }

	connectFn := func() {
		profile := config.Profile{}
		switch method {
		case "direct":
			if strings.TrimSpace(url) == "" {
				setStatus("#FF5F5F", "Connection URL is required")
				return
			}
			profile.URL = url
		case "kube-port-forward":
			if strings.TrimSpace(ktarget) == "" {
				setStatus("#FF5F5F", "Target is required (e.g. svc/postgres)")
				return
			}
			profile.User, profile.Password, profile.Database, profile.SSLMode = user, pass, dbname, sslmode
			remote, _ := strconv.Atoi(strings.TrimSpace(kremote))
			profile.Connect = &config.ConnectConfig{
				Via: "kube-port-forward", Context: kctx, Namespace: kns, Target: ktarget, RemotePort: remote,
			}
		case "exec":
			if strings.TrimSpace(command) == "" {
				setStatus("#FF5F5F", "Command is required")
				return
			}
			profile.User, profile.Password, profile.Database, profile.SSLMode = user, pass, dbname, sslmode
			profile.Connect = &config.ConnectConfig{Via: "exec", Command: strings.Fields(command)}
		}

		setStatus("#808080", "Connecting…")
		app.ForceDraw()

		dsn, closer, err := connect.Open(context.Background(), profile)
		if err != nil {
			setStatus("#FF5F5F", err.Error())
			return
		}
		database, err := db.New(dsn)
		if err != nil {
			_ = closer()
			setStatus("#FF5F5F", err.Error())
			return
		}
		pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := database.Pool().Ping(pingCtx); err != nil {
			database.Close()
			_ = closer()
			setStatus("#FF5F5F", fmt.Sprintf("connection failed: %s", err.Error()))
			return
		}

		profileName := strings.TrimSpace(name)
		if profileName == "" {
			profileName = "default"
		}
		if save {
			if err := config.SaveProfile(profileName, profile); err != nil {
				setStatus("#FF5F5F", fmt.Sprintf("connected, but saving profile failed: %s", err.Error()))
				// Keep the connection — persistence is best-effort.
			} else {
				if cfg.Profiles == nil {
					cfg.Profiles = map[string]config.Profile{}
				}
				cfg.Profiles[profileName] = profile
				if cfg.DefaultProfile == "" {
					cfg.DefaultProfile = profileName
				}
			}
		}

		result = &SetupResult{DB: database, Closer: closer, ProfileName: profileName, Profile: profile}
		app.Stop()
	}

	rebuild = func() {
		form.Clear(true)
		form.AddInputField("Profile name", name, 30, nil, func(t string) { name = t })
		form.AddDropDown("Connect via", methods, indexOf(methods, method), func(opt string, _ int) {
			if opt != method {
				method = opt
				rebuild()
			}
		})
		switch method {
		case "direct":
			form.AddInputField("Connection URL", url, 0, nil, func(t string) { url = t })
		case "kube-port-forward":
			form.AddInputField("Context", kctx, 0, nil, func(t string) { kctx = t })
			form.AddInputField("Namespace", kns, 0, nil, func(t string) { kns = t })
			form.AddInputField("Target (svc/pod/…)", ktarget, 0, nil, func(t string) { ktarget = t })
			form.AddInputField("Remote port", kremote, 10, nil, func(t string) { kremote = t })
			addCredFields(form, &user, &pass, &dbname, &sslmode)
		case "exec":
			form.AddInputField("Command ({local_port})", command, 0, nil, func(t string) { command = t })
			addCredFields(form, &user, &pass, &dbname, &sslmode)
		}
		form.AddCheckbox("Save profile", save, func(c bool) { save = c })
		form.AddButton("Connect", connectFn)
		form.AddButton("Quit", func() { app.Stop() })
		app.SetFocus(form)
	}
	rebuild()

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(form, 0, 1, true).
				AddItem(status, 1, 0, false), 74, 0, true).
			AddItem(nil, 0, 1, false), 24, 0, true).
		AddItem(nil, 0, 1, false)
	layout.SetBackgroundColor(tcell.ColorDefault)

	app.SetInputCapture(func(evt *tcell.EventKey) *tcell.EventKey {
		if evt.Key() == tcell.KeyEscape {
			app.Stop()
			return nil
		}
		return evt
	})

	if err := app.SetRoot(layout, true).EnableMouse(true).Run(); err != nil {
		return nil, false
	}
	return result, result != nil
}

// addCredFields adds the shared credential/database fields used by tunnel methods.
func addCredFields(form *tview.Form, user, pass, dbname, sslmode *string) {
	form.AddInputField("DB user", *user, 30, nil, func(t string) { *user = t })
	form.AddPasswordField("DB password", *pass, 30, '*', func(t string) { *pass = t })
	form.AddInputField("Database", *dbname, 30, nil, func(t string) { *dbname = t })
	form.AddInputField("SSL mode", *sslmode, 20, nil, func(t string) { *sslmode = t })
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return 0
}
