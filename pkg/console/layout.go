// Package console provides layout composition helpers for creating styled CLI output with Lipgloss.
//
// # Layout Composition Helpers
//
// The layout package provides reusable helper functions for common Lipgloss layout patterns.
// These helpers automatically respect TTY detection and provide both styled (TTY) and plain text
// (non-TTY) output modes.
//
// # Usage Example
//
// Here's a complete example showing how to compose a styled CLI output:
//
//	import (
//		"fmt"
//		"os"
//		"github.com/github/gh-aw/pkg/console"
//		"github.com/github/gh-aw/pkg/styles"
//	)
//
//	// Create layout elements
//	title := console.LayoutTitleBox("Trial Execution Plan", 60)
//	info1 := console.LayoutInfoSection("Workflow", "test-workflow")
//	info2 := console.LayoutInfoSection("Status", "Ready")
//	warning := console.LayoutEmphasisBox("⚠️ WARNING: Large workflow file", styles.ColorWarning)
//
//	// Compose sections vertically with spacing
//	output := console.LayoutJoinVertical(title, "", info1, info2, "", warning)
//	fmt.Fprintln(os.Stderr, output)
//
// # TTY Detection
//
// All layout helpers automatically detect whether output is going to a terminal (TTY) or being
// piped/redirected. In TTY mode, they use Lipgloss styling with colors and borders. In non-TTY
// mode, they output plain text suitable for parsing or logging.
//
// # Available Helpers
//
//   - LayoutTitleBox: Centered title with double border
//   - LayoutInfoSection: Info section with left border emphasis
//   - LayoutEmphasisBox: Thick-bordered box with custom color
//   - LayoutJoinVertical: Composes sections with automatic spacing
//
// # Comparison with Existing Functions
//
// These helpers complement the existing RenderTitleBox, RenderInfoSection, and
// RenderComposedSections functions in console.go. The key differences:
//
//   - Layout helpers return strings instead of []string for simpler composition
//   - LayoutInfoSection takes separate label and value parameters
//   - LayoutEmphasisBox provides custom color support with thick borders
//   - Layout helpers are designed for inline composition and chaining
package console

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/tty"
)

// LayoutTitleBox renders a title with a double border box as a single string.
// In TTY mode, uses Lipgloss styled box centered with the Info color scheme.
// In non-TTY mode, renders plain text with separator lines.
// This is a simpler alternative to RenderTitleBox that returns a string instead of []string.
//
// Example:
//
//	title := console.LayoutTitleBox("Trial Execution Plan", 60)
//	fmt.Fprintln(os.Stderr, title)
func LayoutTitleBox(title string, width int) string {
	if tty.IsStderrTerminal() {
		// TTY mode: Use Lipgloss styled box
		box := lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.ColorInfo).
			Border(lipgloss.DoubleBorder(), true, false).
			Padding(0, 2).
			Width(width).
			Align(lipgloss.Center).
			Render(title)
		return box
	}

	// Non-TTY mode: Plain text with separators
	separator := strings.Repeat("=", width)
	return separator + "\n  " + title + "\n" + separator
}

// LayoutInfoSection renders an info section with left border emphasis as a single string.
// In TTY mode, uses Lipgloss styled section with left border and padding.
// In non-TTY mode, adds manual indentation.
// This is a simpler alternative to RenderInfoSection that returns a string and takes label/value.
//
// Example:
//
//	info := console.LayoutInfoSection("Workflow", "test-workflow")
//	fmt.Fprintln(os.Stderr, info)
func LayoutInfoSection(label, value string) string {
	content := label + ": " + value

	if tty.IsStderrTerminal() {
		// TTY mode: Use Lipgloss styled section with left border and padding
		section := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(styles.ColorInfo).
			PaddingLeft(2).
			Render(content)
		return section
	}

	// Non-TTY mode: Add manual indentation
	return "  " + content
}

// LayoutEmphasisBox renders content in a rounded-bordered box with custom color.
// In TTY mode, uses Lipgloss styled box with rounded border for a polished appearance.
// In non-TTY mode, renders content with surrounding marker lines.
//
// Example:
//
//	warning := console.LayoutEmphasisBox("⚠️ WARNING: Large workflow", styles.ColorWarning)
//	fmt.Fprintln(os.Stderr, warning)
func LayoutEmphasisBox(content string, color lipgloss.AdaptiveColor) string {
	if tty.IsStderrTerminal() {
		// TTY mode: Use Lipgloss styled box with rounded border for a softer appearance
		box := lipgloss.NewStyle().
			Bold(true).
			Foreground(color).
			Border(styles.RoundedBorder).
			BorderForeground(color).
			Padding(0, 2).
			Render(content)
		return box
	}

	// Non-TTY mode: Content with marker lines
	marker := strings.Repeat("!", len(content)+4)
	return marker + "\n  " + content + "\n" + marker
}

// LayoutJoinVertical composes sections vertically with automatic spacing.
// In TTY mode, uses lipgloss.JoinVertical for proper composition.
// In non-TTY mode, joins sections with newlines.
//
// Example:
//
//	title := console.LayoutTitleBox("Plan", 60)
//	info := console.LayoutInfoSection("Status", "Ready")
//	output := console.LayoutJoinVertical(title, info)
//	fmt.Fprintln(os.Stderr, output)
func LayoutJoinVertical(sections ...string) string {
	if tty.IsStderrTerminal() {
		// TTY mode: Use Lipgloss to compose sections vertically
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Non-TTY mode: Join with newlines
	return strings.Join(sections, "\n")
}
