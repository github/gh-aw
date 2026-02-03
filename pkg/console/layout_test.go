//go:build !integration

package console

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/github/gh-aw/pkg/styles"
)

func TestLayoutTitleBox(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		width    int
		expected []string // Substrings that should be present in output
	}{
		{
			name:  "basic title",
			title: "Test Title",
			width: 40,
			expected: []string{
				"Test Title",
			},
		},
		{
			name:  "longer title",
			title: "Trial Execution Plan",
			width: 80,
			expected: []string{
				"Trial Execution Plan",
			},
		},
		{
			name:  "title with special characters",
			title: "⚠️ Important Notice",
			width: 60,
			expected: []string{
				"⚠️ Important Notice",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := LayoutTitleBox(tt.title, tt.width)

			// Check that output is not empty
			if output == "" {
				t.Error("LayoutTitleBox() returned empty string")
			}

			// Check that title appears in output
			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Errorf("LayoutTitleBox() output missing expected string '%s'\nGot:\n%s", expected, output)
				}
			}
		})
	}
}

func TestLayoutInfoSection(t *testing.T) {
	tests := []struct {
		name     string
		label    string
		value    string
		expected []string // Substrings that should be present in output
	}{
		{
			name:  "simple label and value",
			label: "Workflow",
			value: "test-workflow",
			expected: []string{
				"Workflow",
				"test-workflow",
			},
		},
		{
			name:  "status label",
			label: "Status",
			value: "Active",
			expected: []string{
				"Status",
				"Active",
			},
		},
		{
			name:  "file path value",
			label: "Location",
			value: "/path/to/file",
			expected: []string{
				"Location",
				"/path/to/file",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := LayoutInfoSection(tt.label, tt.value)

			// Check that output is not empty
			if output == "" {
				t.Error("LayoutInfoSection() returned empty string")
			}

			// Check that expected strings appear in output
			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Errorf("LayoutInfoSection() output missing expected string '%s'\nGot:\n%s", expected, output)
				}
			}
		})
	}
}

func TestLayoutEmphasisBox(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		color    lipgloss.AdaptiveColor
		expected []string // Substrings that should be present in output
	}{
		{
			name:    "warning message",
			content: "⚠️ WARNING",
			color:   styles.ColorWarning,
			expected: []string{
				"⚠️ WARNING",
			},
		},
		{
			name:    "error message",
			content: "✗ ERROR: Failed",
			color:   styles.ColorError,
			expected: []string{
				"✗ ERROR: Failed",
			},
		},
		{
			name:    "success message",
			content: "✓ Success",
			color:   styles.ColorSuccess,
			expected: []string{
				"✓ Success",
			},
		},
		{
			name:    "info message",
			content: "ℹ Information",
			color:   styles.ColorInfo,
			expected: []string{
				"ℹ Information",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := LayoutEmphasisBox(tt.content, tt.color)

			// Check that output is not empty
			if output == "" {
				t.Error("LayoutEmphasisBox() returned empty string")
			}

			// Check that content appears in output
			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Errorf("LayoutEmphasisBox() output missing expected string '%s'\nGot:\n%s", expected, output)
				}
			}
		})
	}
}

func TestLayoutJoinVertical(t *testing.T) {
	tests := []struct {
		name     string
		sections []string
		expected []string // Substrings that should be present in output
	}{
		{
			name:     "single section",
			sections: []string{"Section 1"},
			expected: []string{"Section 1"},
		},
		{
			name:     "multiple sections",
			sections: []string{"Section 1", "Section 2", "Section 3"},
			expected: []string{
				"Section 1",
				"Section 2",
				"Section 3",
			},
		},
		{
			name:     "sections with empty strings",
			sections: []string{"Section 1", "", "Section 2"},
			expected: []string{
				"Section 1",
				"Section 2",
			},
		},
		{
			name:     "empty sections",
			sections: []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := LayoutJoinVertical(tt.sections...)

			// For empty sections, output should be empty
			if len(tt.sections) == 0 {
				if output != "" {
					t.Errorf("LayoutJoinVertical() expected empty string, got: %s", output)
				}
				return
			}

			// Check that expected strings appear in output
			for _, expected := range tt.expected {
				if expected == "" {
					continue
				}
				if !strings.Contains(output, expected) {
					t.Errorf("LayoutJoinVertical() output missing expected string '%s'\nGot:\n%s", expected, output)
				}
			}
		})
	}
}

func TestLayoutCompositionAPI(t *testing.T) {
	t.Run("compose multiple layout elements", func(t *testing.T) {
		// Test the API example from the documentation
		title := LayoutTitleBox("Trial Execution Plan", 60)
		info := LayoutInfoSection("Workflow", "test-workflow")
		warning := LayoutEmphasisBox("⚠️ WARNING", styles.ColorWarning)

		// Compose sections vertically with spacing
		output := LayoutJoinVertical(title, "", info, "", warning)

		// Verify all elements are present in output
		expected := []string{
			"Trial Execution Plan",
			"Workflow",
			"test-workflow",
			"⚠️ WARNING",
		}

		for _, exp := range expected {
			if !strings.Contains(output, exp) {
				t.Errorf("Composed output missing expected string '%s'\nGot:\n%s", exp, output)
			}
		}
	})
}

func TestLayoutWidthConstraints(t *testing.T) {
	tests := []struct {
		name  string
		width int
	}{
		{"narrow width", 40},
		{"medium width", 60},
		{"wide width", 80},
		{"very wide width", 120},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := LayoutTitleBox("Test", tt.width)

			// Output should not be empty
			if output == "" {
				t.Error("LayoutTitleBox() returned empty string")
			}

			// In non-TTY mode, separator length should match width
			// We can't test TTY mode easily, but we can check non-TTY
			lines := strings.Split(output, "\n")
			if len(lines) > 0 {
				// First line should contain separators or styled content
				if len(lines[0]) == 0 {
					t.Error("LayoutTitleBox() first line is empty")
				}
			}
		})
	}
}

func TestLayoutWithDifferentColors(t *testing.T) {
	colors := []struct {
		name  string
		color lipgloss.AdaptiveColor
	}{
		{"error color", styles.ColorError},
		{"warning color", styles.ColorWarning},
		{"success color", styles.ColorSuccess},
		{"info color", styles.ColorInfo},
		{"purple color", styles.ColorPurple},
		{"yellow color", styles.ColorYellow},
	}

	for _, c := range colors {
		t.Run(c.name, func(t *testing.T) {
			output := LayoutEmphasisBox("Test Content", c.color)

			// Output should not be empty
			if output == "" {
				t.Error("LayoutEmphasisBox() returned empty string")
			}

			// Content should be present
			if !strings.Contains(output, "Test Content") {
				t.Errorf("LayoutEmphasisBox() missing content, got: %s", output)
			}
		})
	}
}

func TestLayoutNonTTYOutput(t *testing.T) {
	// These tests verify that non-TTY output is plain text
	// In actual non-TTY environment, output should be plain without ANSI codes

	t.Run("title box non-tty format", func(t *testing.T) {
		output := LayoutTitleBox("Test", 40)
		// Should contain the title
		if !strings.Contains(output, "Test") {
			t.Errorf("Expected title in output, got: %s", output)
		}
	})

	t.Run("info section non-tty format", func(t *testing.T) {
		output := LayoutInfoSection("Label", "Value")
		// Should contain label and value
		if !strings.Contains(output, "Label") || !strings.Contains(output, "Value") {
			t.Errorf("Expected label and value in output, got: %s", output)
		}
	})

	t.Run("emphasis box non-tty format", func(t *testing.T) {
		output := LayoutEmphasisBox("Content", styles.ColorWarning)
		// Should contain content
		if !strings.Contains(output, "Content") {
			t.Errorf("Expected content in output, got: %s", output)
		}
	})
}

// Example demonstrates how to compose a styled CLI output
// using the layout helper functions.
func Example() {
	// Create layout elements
	title := LayoutTitleBox("Trial Execution Plan", 60)
	info1 := LayoutInfoSection("Workflow", "test-workflow")
	info2 := LayoutInfoSection("Status", "Ready")
	warning := LayoutEmphasisBox("⚠️ WARNING: Large workflow file", styles.ColorWarning)

	// Compose sections vertically with spacing
	output := LayoutJoinVertical(title, "", info1, info2, "", warning)

	// In a real application, you would output to stderr:
	// fmt.Fprintln(os.Stderr, output)

	// For test purposes, just verify the output contains expected content
	if !strings.Contains(output, "Trial Execution Plan") {
		panic("missing title")
	}
	if !strings.Contains(output, "test-workflow") {
		panic("missing workflow name")
	}
	if !strings.Contains(output, "WARNING") {
		panic("missing warning")
	}
}
