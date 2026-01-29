//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestASCIILogoAlignment verifies that the ASCII logo in generated YAML
// preserves leading spaces on the first line for proper alignment
func TestASCIILogoAlignment(t *testing.T) {
	compiler := NewCompiler()

	// Create a minimal workflow for testing
	data := &WorkflowData{
		Name:        "test-workflow",
		On:          "push",
		Permissions: "contents: read",
	}

	// Generate YAML
	yaml, err := compiler.generateYAML(data, "test.md")
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Split into lines and find the logo section
	lines := strings.Split(yaml, "\n")

	// Find the start of the ASCII logo (first line with underscores)
	logoStartIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "___") {
			logoStartIdx = i
			break
		}
	}

	if logoStartIdx == -1 {
		t.Fatal("ASCII logo not found in generated YAML")
	}

	// The first line of the ASCII art should have 3 leading spaces after the "# "
	firstLine := lines[logoStartIdx]

	// Expected format: "#    ___..." (# followed by 4 spaces then underscores)
	// This is "# " (2 chars) + 3 leading spaces from logo.txt + "___"
	if !strings.HasPrefix(firstLine, "#    ___") {
		t.Errorf("First line of ASCII logo has incorrect alignment.\nExpected: '#    ___...'\nGot:      '%s'", firstLine)
	}

	// Verify the second line has 2 leading spaces (# + 3 spaces total after #)
	if logoStartIdx+1 < len(lines) {
		secondLine := lines[logoStartIdx+1]
		if !strings.HasPrefix(secondLine, "#   / _ \\") {
			t.Errorf("Second line of ASCII logo has incorrect alignment.\nExpected: '#   / _ \\'...\nGot:      '%s'", secondLine)
		}
	}

	// Verify the third line has 1 leading space (# + 2 spaces total after #)
	if logoStartIdx+2 < len(lines) {
		thirdLine := lines[logoStartIdx+2]
		if !strings.HasPrefix(thirdLine, "#  | |_| |") {
			t.Errorf("Third line of ASCII logo has incorrect alignment.\nExpected: '#  | |_| |'...\nGot:      '%s'", thirdLine)
		}
	}
}

// TestLogoTrimming verifies that TrimRight is used instead of TrimSpace
// to preserve per-line leading spaces
func TestLogoTrimming(t *testing.T) {
	testLogo := `   ___
  / _ \
 | |_| |
`

	// Test TrimSpace behavior (what we DON'T want)
	linesWithTrimSpace := strings.Split(strings.TrimSpace(testLogo), "\n")
	firstWithTrimSpace := linesWithTrimSpace[0]

	// TrimSpace removes ALL leading spaces
	if strings.HasPrefix(firstWithTrimSpace, "   ") {
		t.Error("TrimSpace should remove leading spaces (this test validates the problem)")
	}

	// Test TrimRight behavior (what we DO want)
	linesWithTrimRight := strings.Split(strings.TrimRight(testLogo, "\n"), "\n")
	firstWithTrimRight := linesWithTrimRight[0]

	// TrimRight preserves leading spaces
	if !strings.HasPrefix(firstWithTrimRight, "   ___") {
		t.Errorf("TrimRight should preserve leading spaces.\nExpected: '   ___'\nGot:      '%s'", firstWithTrimRight)
	}
}
