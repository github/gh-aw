package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
)

// showUpdateSummary displays a summary of workflow updates using console helpers
func showUpdateSummary(successfulUpdates []string, failedUpdates []updateFailure) {
	fmt.Fprintln(os.Stderr, "")

	// Show successful updates
	if len(successfulUpdates) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully updated and compiled %d workflow(s):", len(successfulUpdates))))
		for _, name := range successfulUpdates {
			fmt.Fprintln(os.Stderr, console.FormatListItem(name))
		}
		fmt.Fprintln(os.Stderr, "")
	}

	// Show failed updates
	if len(failedUpdates) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Failed to update %d workflow(s):", len(failedUpdates))))
		for _, failure := range failedUpdates {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", failure.Name, failure.Error)
		}
		fmt.Fprintln(os.Stderr, "")
	}
}
