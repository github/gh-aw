//go:build !js && !wasm

package console

import (
	_ "embed"
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/styles"
)

var bannerLog = logger.New("console:banner")

//go:embed assets/logo.txt
var bannerLogo string

// bannerStyle defines the style for the ASCII banner
// Uses GitHub's purple color theme
var bannerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(styles.ColorPurple)

// FormatBanner returns the ASCII logo formatted with purple GitHub color theme.
// It applies the purple color styling when running in a terminal (TTY).
func FormatBanner() string {
	logo := strings.TrimRight(bannerLogo, "\n")
	return applyStyle(bannerStyle, logo)
}

// PrintBanner prints the ASCII logo to stderr with purple GitHub color theme.
// This is used by the --banner flag to display the logo at the start of command execution.
func PrintBanner() {
	out := stderrWriter()
	if _, err := fmt.Fprintln(out, FormatBanner()); err != nil {
		bannerLog.Printf("Failed to write banner: %v", err)
	}
	if _, err := fmt.Fprintln(out); err != nil {
		bannerLog.Printf("Failed to write banner spacing: %v", err)
	}
}
