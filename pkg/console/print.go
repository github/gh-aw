package console

import (
	"fmt"
	"io"
	"os"

	"github.com/github/gh-aw/pkg/logger"
)

var printLog = logger.New("console:print")

// stderr is the writer used by all Print* helpers. Tests may replace it with a
// bytes.Buffer to capture output without touching OS file descriptors.
// Tests must not call t.Parallel() as this variable is not concurrency-safe.
var stderr io.Writer = os.Stderr

// PrintSuccessMessage formats and prints a success message to stderr.
func PrintSuccessMessage(message string) {
	if _, err := fmt.Fprintln(stderr, FormatSuccessMessageStderr(message)); err != nil {
		printLog.Printf("Failed to write success message: %v", err)
	}
}

// PrintInfoMessage formats and prints an info message to stderr.
func PrintInfoMessage(message string) {
	if _, err := fmt.Fprintln(stderr, FormatInfoMessageStderr(message)); err != nil {
		printLog.Printf("Failed to write info message: %v", err)
	}
}

// PrintWarningMessage formats and prints a warning message to stderr.
func PrintWarningMessage(message string) {
	if _, err := fmt.Fprintln(stderr, FormatWarningMessageStderr(message)); err != nil {
		printLog.Printf("Failed to write warning message: %v", err)
	}
}

// PrintErrorMessage formats and prints a simple error message to stderr.
func PrintErrorMessage(message string) {
	if _, err := fmt.Fprintln(stderr, FormatErrorMessage(message)); err != nil {
		printLog.Printf("Failed to write error message: %v", err)
	}
}

// PrintCommandMessage formats and prints a command message to stderr.
func PrintCommandMessage(command string) {
	if _, err := fmt.Fprintln(stderr, FormatCommandMessageStderr(command)); err != nil {
		printLog.Printf("Failed to write command message: %v", err)
	}
}

// PrintSectionHeader formats and prints a section header to stderr.
func PrintSectionHeader(header string) {
	if _, err := fmt.Fprintln(stderr, FormatSectionHeaderStderr(header)); err != nil {
		printLog.Printf("Failed to write section header: %v", err)
	}
}
