//go:build !js && !wasm

package console

import (
	"errors"
	"fmt"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/tty"
)

var listLog = logger.New("console:list")

// ShowInteractiveList displays an interactive list using huh.Select with arrow key navigation.
// Returns the selected item's value, or an error if cancelled or failed.
//
// Use this for standalone pickers outside a form context; prefer huh.Select directly
// when building a multi-field form with WithTheme/WithAccessible applied to the whole form.
func ShowInteractiveList(title string, items []ListItem) (string, error) {
	listLog.Printf("Showing interactive list: title=%s, items=%d", title, len(items))

	if len(items) == 0 {
		return "", errors.New("no items to display")
	}

	if !tty.IsStdinTerminal() {
		listLog.Print("Non-TTY stdin detected, cannot read list selection")
		return "", errors.New("interactive input not available (stdin is not a TTY)")
	}

	// The text fallback reads from stdin, while the Huh form also requires a
	// terminal on stderr for rendering.
	if !tty.IsStderrTerminal() {
		listLog.Print("Non-TTY detected, falling back to text list")
		return showTextList(title, items)
	}

	// Build huh options, combining title and description into the option label
	opts := make([]huh.Option[string], len(items))
	for i, item := range items {
		label := item.title
		if item.description != "" {
			label = fmt.Sprintf("%s – %s", item.title, item.description)
		}
		opts[i] = huh.NewOption(label, item.value)
	}

	var selected string
	form := NewSelectForm(
		huh.NewSelect[string]().
			Title(title).
			Options(opts...).
			Value(&selected),
	)

	if err := form.Run(); err != nil {
		listLog.Printf("Error running list form: %v", err)
		return "", fmt.Errorf("failed to run interactive list: %w", err)
	}

	listLog.Printf("Selected item: %s", selected)
	return selected, nil
}

// showTextList displays a non-interactive numbered list for non-TTY environments
func showTextList(title string, items []ListItem) (string, error) {
	listLog.Printf("Showing text list: title=%s, items=%d", title, len(items))
	out := stderrWriter()

	fmt.Fprintf(out, "\n%s\n\n", title)
	for i, item := range items {
		fmt.Fprintf(out, "  %d) %s\n", i+1, item.title)
		if item.description != "" {
			fmt.Fprintf(out, "     %s\n", item.description)
		}
	}
	fmt.Fprintf(out, "\nSelect (1-%d): ", len(items))

	var choice int
	_, err := fmt.Scanf("%d", &choice)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if choice < 1 || choice > len(items) {
		return "", fmt.Errorf("selection out of range (must be 1-%d)", len(items))
	}

	selectedItem := items[choice-1]
	listLog.Printf("Selected item from text list: %s", selectedItem.value)
	return selectedItem.value, nil
}
