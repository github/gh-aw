package console

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/github/gh-aw/pkg/styles"
)

//go:embed assets/logo.txt
var bannerLogo string

// BannerStyle defines the style for the ASCII banner
// Uses GitHub's purple color theme
var BannerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(styles.ColorPurple)

// FormatBanner returns the ASCII logo formatted with purple GitHub color theme.
// It applies the purple color styling when running in a terminal (TTY).
func FormatBanner() string {
	logo := strings.TrimRight(bannerLogo, "\n")
	return applyStyle(BannerStyle, logo)
}

// PrintBanner prints the ASCII logo to stderr with purple GitHub color theme.
// This is used by the --banner flag to display the logo at the start of command execution.
func PrintBanner() {
	fmt.Fprintln(os.Stderr, FormatBanner())
	fmt.Fprintln(os.Stderr)
}
