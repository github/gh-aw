package console

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var verboseLog = logger.New("console:verbose")

// LogVerbose outputs a verbose message to stderr only when verbose mode is enabled.
// This is a convenience helper to avoid repetitive if-verbose checks throughout the codebase.
func LogVerbose(verbose bool, message string) {
	if verbose {
		if _, err := fmt.Fprintln(stderrWriter(), FormatVerboseMessage(message)); err != nil {
			verboseLog.Printf("Failed to write verbose message: %v", err)
		}
	}
}
