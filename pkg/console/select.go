package console

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/github/gh-aw/pkg/tty"
)

// SelectOption represents a selectable option with a label and value
type SelectOption struct {
	Label string
	Value string
}

// PromptSelect shows an interactive single-select menu
// Returns the selected value or an error
func PromptSelect(title, description string, options []SelectOption) (string, error) {
	// Validate inputs first
	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}

	// Check if stdin is a TTY - if not, we can't show interactive forms
	if !tty.IsStderrTerminal() {
		return "", fmt.Errorf("interactive selection not available (not a TTY)")
	}

	var selected string

	// Convert options to huh.Option format
	huhOptions := make([]huh.Option[string], len(options))
	for i, opt := range options {
		huhOptions[i] = huh.NewOption(opt.Label, opt.Value)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Description(description).
				Options(huhOptions...).
				Value(&selected),
		),
	).WithAccessible(IsAccessibleMode())

	if err := form.Run(); err != nil {
		return "", err
	}

	return selected, nil
}

// PromptMultiSelect shows an interactive multi-select menu
// Returns the selected values or an error
func PromptMultiSelect(title, description string, options []SelectOption, limit int) ([]string, error) {
	// Validate inputs first
	if len(options) == 0 {
		return nil, fmt.Errorf("no options provided")
	}

	// Check if stdin is a TTY - if not, we can't show interactive forms
	if !tty.IsStderrTerminal() {
		return nil, fmt.Errorf("interactive selection not available (not a TTY)")
	}

	var selected []string

	// Convert options to huh.Option format
	huhOptions := make([]huh.Option[string], len(options))
	for i, opt := range options {
		huhOptions[i] = huh.NewOption(opt.Label, opt.Value)
	}

	multiSelect := huh.NewMultiSelect[string]().
		Title(title).
		Description(description).
		Options(huhOptions...).
		Value(&selected)

	// Set limit if specified (0 means no limit)
	if limit > 0 {
		multiSelect.Limit(limit)
	}

	form := huh.NewForm(
		huh.NewGroup(multiSelect),
	).WithAccessible(IsAccessibleMode())

	if err := form.Run(); err != nil {
		return nil, err
	}

	return selected, nil
}
