package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/google/cel-go/cel"
	"github.com/rivo/tview"

	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
)

// celVars returns the variable names available for a given resource type.
func celVars(resource string) []string {
	switch resource {
	case "query":
		return []string{
			"pid", "user", "app", "database", "client_addr", "state",
			"duration", "wait_event_type", "wait_event", "query",
			"route", "controller", "action_name", "framework",
			"blocked_by", "query_id",
		}
	case "transaction":
		return []string{
			"pid", "user", "app", "database", "state",
			"xact_duration", "query_duration", "query", "lock_count",
		}
	case "lock":
		return []string{
			"blocked_pid", "blocking_pid",
			"blocked_user", "blocking_user",
			"blocked_app", "blocking_app",
			"lock_type", "mode", "wait_duration",
		}
	}
	return nil
}

// celBuiltins are common CEL functions and keywords useful in rule expressions.
var celBuiltins = []string{
	"duration(", "true", "false",
	"contains(", "startsWith(", "endsWith(", "matches(",
	"size(", "int(", "string(",
}

// lastToken extracts the token currently being typed (after operators/whitespace).
func lastToken(text string) string {
	// Find the start of the current token
	for i := len(text) - 1; i >= 0; i-- {
		ch := text[i]
		if ch == ' ' || ch == '(' || ch == ')' || ch == '&' || ch == '|' ||
			ch == '!' || ch == '=' || ch == '<' || ch == '>' || ch == '\'' {
			return text[i+1:]
		}
	}
	return text
}

// NewRuleForm creates a rule builder form. If existing is non-nil, fields are pre-populated for editing.
func NewRuleForm(app *tview.Application, readonly bool, existing *rules.RuleConfig, onSave func(rules.RuleConfig), onCancel func()) tview.Primitive {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.ColorDefault)
	form.SetFieldBackgroundColor(tcell.NewRGBColor(0x1a, 0x1a, 0x1a))
	form.SetFieldTextColor(tcell.ColorWhite)
	form.SetLabelColor(theme.ColorLabel)
	form.SetButtonBackgroundColor(theme.ColorBorder)
	form.SetButtonTextColor(tcell.ColorWhite)
	form.SetBorder(true)
	form.SetBorderColor(theme.ColorBorder)
	form.SetTitleColor(theme.ColorLogo)

	title := " New Rule "
	if existing != nil {
		title = " Edit Rule "
	}
	form.SetTitle(title)

	// Status line for CEL validation feedback
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	status.SetBackgroundColor(tcell.ColorDefault)

	// Default values
	nameVal := ""
	resourceIdx := 0
	whenVal := ""
	actionIdx := 0
	cooldownVal := ""
	enabledVal := true
	dryRunVal := false

	if existing != nil {
		nameVal = existing.Name
		whenVal = existing.When
		cooldownVal = existing.Cooldown
		if existing.Enabled != nil {
			enabledVal = *existing.Enabled
		}
		dryRunVal = existing.DryRun
		switch existing.Resource {
		case "transaction":
			resourceIdx = 1
		case "lock":
			resourceIdx = 2
		}
		switch existing.Action {
		case "cancel":
			actionIdx = 1
		case "terminate":
			actionIdx = 2
		}
	}

	if readonly {
		dryRunVal = true
	}

	resources := []string{"query", "transaction", "lock"}
	actions := []string{"log", "cancel", "terminate"}

	currentResource := resources[resourceIdx]

	validateCEL := func(resource, expression string) {
		if expression == "" {
			status.SetText("  [#808080]Enter a CEL expression[-]")
			return
		}
		rt := rules.ResourceType(resource)
		env, err := rules.EnvForResource(rt)
		if err != nil {
			status.SetText(fmt.Sprintf("  [#FF5F5F]%s[-]", err.Error()))
			return
		}
		ast, issues := env.Compile(expression)
		if issues != nil && issues.Err() != nil {
			status.SetText(fmt.Sprintf("  [#FF5F5F]%s[-]", issues.Err().Error()))
			return
		}
		if ast.OutputType() != cel.BoolType {
			status.SetText(fmt.Sprintf("  [#FF5F5F]must return bool, got %s[-]", ast.OutputType()))
			return
		}
		status.SetText("  [#00D700]Expression valid[-]")
	}

	// Build autocomplete function for the When field
	autocomplete := func(text string) []string {
		tok := lastToken(text)
		if tok == "" {
			return nil
		}
		prefix := strings.ToLower(tok)
		var matches []string
		for _, v := range celVars(currentResource) {
			if strings.HasPrefix(v, prefix) && v != tok {
				matches = append(matches, v)
			}
		}
		for _, b := range celBuiltins {
			if strings.HasPrefix(b, prefix) && b != tok {
				matches = append(matches, b)
			}
		}
		if len(matches) == 0 {
			return nil
		}
		// Build full-text completions by replacing the current token
		base := text[:len(text)-len(tok)]
		entries := make([]string, len(matches))
		for i, m := range matches {
			entries[i] = base + m
		}
		return entries
	}

	// Add form fields
	form.AddInputField("Name", nameVal, 0, nil, nil)
	var whenExpr string // track separately to avoid lookup before field exists
	whenExpr = whenVal
	form.AddDropDown("Resource", resources, resourceIdx, func(option string, index int) {
		currentResource = option
		validateCEL(option, whenExpr)
	})
	whenField := tview.NewInputField().
		SetLabel("When").
		SetText(whenVal).
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.NewRGBColor(0x1a, 0x1a, 0x1a)).
		SetFieldTextColor(tcell.ColorWhite).
		SetLabelColor(theme.ColorLabel)
	whenField.SetBackgroundColor(tcell.ColorDefault)
	whenField.SetChangedFunc(func(text string) {
		whenExpr = text
		validateCEL(currentResource, text)
	})
	whenField.SetAutocompleteFunc(autocomplete)
	whenField.SetAutocompletedFunc(func(text string, index, source int) bool {
		whenExpr = text
		whenField.SetText(text)
		validateCEL(currentResource, text)
		return source == tview.AutocompletedNavigate
	})
	form.AddFormItem(whenField)
	form.AddDropDown("Action", actions, actionIdx, nil)
	form.AddInputField("Cooldown", cooldownVal, 20, nil, nil)
	form.AddCheckbox("Enabled", enabledVal, nil)
	form.AddCheckbox("Dry Run", dryRunVal, nil)

	// Initial validation
	validateCEL(currentResource, whenVal)

	// Save button
	form.AddButton("Save", func() {
		nameItem := form.GetFormItemByLabel("Name").(*tview.InputField)
		name := nameItem.GetText()
		if name == "" {
			status.SetText("  [#FF5F5F]Name is required[-]")
			return
		}

		when := whenField.GetText()
		if when == "" {
			status.SetText("  [#FF5F5F]Expression is required[-]")
			return
		}

		// Validate CEL compiles
		rt := rules.ResourceType(currentResource)
		env, err := rules.EnvForResource(rt)
		if err != nil {
			status.SetText(fmt.Sprintf("  [#FF5F5F]%s[-]", err.Error()))
			return
		}
		ast, issues := env.Compile(when)
		if issues != nil && issues.Err() != nil {
			status.SetText(fmt.Sprintf("  [#FF5F5F]%s[-]", issues.Err().Error()))
			return
		}
		if ast.OutputType() != cel.BoolType {
			status.SetText(fmt.Sprintf("  [#FF5F5F]must return bool, got %s[-]", ast.OutputType()))
			return
		}

		cooldownItem := form.GetFormItemByLabel("Cooldown").(*tview.InputField)
		cooldown := cooldownItem.GetText()
		if cooldown != "" {
			if _, err := time.ParseDuration(cooldown); err != nil {
				status.SetText(fmt.Sprintf("  [#FF5F5F]Invalid cooldown: %s[-]", err.Error()))
				return
			}
		}

		_, action := form.GetFormItemByLabel("Action").(*tview.DropDown).GetCurrentOption()

		enabledCheckbox := form.GetFormItemByLabel("Enabled").(*tview.Checkbox)
		enabled := enabledCheckbox.IsChecked()

		dryRunCheckbox := form.GetFormItemByLabel("Dry Run").(*tview.Checkbox)
		dryRun := dryRunCheckbox.IsChecked()
		if readonly {
			dryRun = true
		}

		cfg := rules.RuleConfig{
			Name:     name,
			Enabled:  &enabled,
			DryRun:   dryRun,
			Resource: currentResource,
			When:     when,
			Action:   action,
			Cooldown: cooldown,
		}
		onSave(cfg)
	})

	// Cancel button
	form.AddButton("Cancel", func() {
		onCancel()
	})

	// Wrap form + status in a vertical flex
	inner := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(status, 1, 0, false)
	inner.SetBackgroundColor(tcell.ColorDefault)

	// Center the modal
	modal := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(inner, 70, 0, true).
			AddItem(nil, 0, 1, false),
			22, 0, true).
		AddItem(nil, 0, 1, false)
	modal.SetBackgroundColor(tcell.ColorDefault)

	return modal
}
