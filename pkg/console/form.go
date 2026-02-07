package console

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/github/gh-aw/pkg/tty"
)

// FormField represents a generic form field configuration
type FormField struct {
	Type        string // "input", "password", "confirm", "select"
	Title       string
	Description string
	Placeholder string
	Value       any                // Pointer to the value to store the result
	Options     []SelectOption     // For select fields
	Validate    func(string) error // For input/password fields
}

// RunForm executes a multi-field form with validation
// This is a higher-level helper that creates a form with multiple fields
func RunForm(fields []FormField) error {
	// Validate inputs first before checking TTY
	if len(fields) == 0 {
		return fmt.Errorf("no form fields provided")
	}

	// Validate field configurations before checking TTY
	for _, field := range fields {
		if field.Type == "select" && len(field.Options) == 0 {
			return fmt.Errorf("select field '%s' requires options", field.Title)
		}
		if field.Type != "input" && field.Type != "password" && field.Type != "confirm" && field.Type != "select" {
			return fmt.Errorf("unknown field type: %s", field.Type)
		}
	}

	// Check if stdin is a TTY - if not, we can't show interactive forms
	if !tty.IsStderrTerminal() {
		return fmt.Errorf("interactive forms not available (not a TTY)")
	}

	// Build form fields
	var huhFields []huh.Field
	for _, field := range fields {
		switch field.Type {
		case "input":
			inputField := huh.NewInput().
				Title(field.Title).
				Description(field.Description).
				Placeholder(field.Placeholder)

			if field.Validate != nil {
				inputField.Validate(field.Validate)
			}

			// Type assert to *string
			if strPtr, ok := field.Value.(*string); ok {
				inputField.Value(strPtr)
			} else {
				return fmt.Errorf("input field '%s' requires *string value", field.Title)
			}

			huhFields = append(huhFields, inputField)

		case "password":
			passwordField := huh.NewInput().
				Title(field.Title).
				Description(field.Description).
				EchoMode(huh.EchoModePassword)

			if field.Validate != nil {
				passwordField.Validate(field.Validate)
			}

			// Type assert to *string
			if strPtr, ok := field.Value.(*string); ok {
				passwordField.Value(strPtr)
			} else {
				return fmt.Errorf("password field '%s' requires *string value", field.Title)
			}

			huhFields = append(huhFields, passwordField)

		case "confirm":
			confirmField := huh.NewConfirm().
				Title(field.Title)

			// Type assert to *bool
			if boolPtr, ok := field.Value.(*bool); ok {
				confirmField.Value(boolPtr)
			} else {
				return fmt.Errorf("confirm field '%s' requires *bool value", field.Title)
			}

			huhFields = append(huhFields, confirmField)

		case "select":
			selectField := huh.NewSelect[string]().
				Title(field.Title).
				Description(field.Description)

			// Convert options to huh.Option format
			huhOptions := make([]huh.Option[string], len(field.Options))
			for i, opt := range field.Options {
				huhOptions[i] = huh.NewOption(opt.Label, opt.Value)
			}
			selectField.Options(huhOptions...)

			// Type assert to *string
			if strPtr, ok := field.Value.(*string); ok {
				selectField.Value(strPtr)
			} else {
				return fmt.Errorf("select field '%s' requires *string value", field.Title)
			}

			huhFields = append(huhFields, selectField)

		default:
		}
	}

	// Create and run the form
	form := huh.NewForm(
		huh.NewGroup(huhFields...),
	).WithAccessible(IsAccessibleMode())

	return form.Run()
}
