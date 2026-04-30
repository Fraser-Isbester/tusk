package views

import (
	"fmt"
	"time"

	"github.com/fraser-isbester/tusk/internal/rules"
	"github.com/fraser-isbester/tusk/internal/tui/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/google/cel-go/cel"
	"github.com/rivo/tview"
)

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

	// Add form fields
	form.AddInputField("Name", nameVal, 40, nil, nil)
	var whenExpr string // track separately to avoid lookup before field exists
	whenExpr = whenVal
	form.AddDropDown("Resource", resources, resourceIdx, func(option string, index int) {
		currentResource = option
		validateCEL(option, whenExpr)
	})
	form.AddInputField("When", whenVal, 60, nil, func(text string) {
		whenExpr = text
		validateCEL(currentResource, text)
	})
	form.AddDropDown("Action", actions, actionIdx, nil)
	form.AddInputField("Cooldown", cooldownVal, 20, nil, nil)
	form.AddCheckbox("Enabled", enabledVal, nil)
	form.AddCheckbox("Dry Run", dryRunVal, nil)

	// Initial validation
	validateCEL(currentResource, whenVal)

	// Save button
	form.AddButton("Save", func() {
		nameField := form.GetFormItemByLabel("Name").(*tview.InputField)
		name := nameField.GetText()
		if name == "" {
			status.SetText("  [#FF5F5F]Name is required[-]")
			return
		}

		whenField := form.GetFormItemByLabel("When").(*tview.InputField)
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

		cooldownField := form.GetFormItemByLabel("Cooldown").(*tview.InputField)
		cooldown := cooldownField.GetText()
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
