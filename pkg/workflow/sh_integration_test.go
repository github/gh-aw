//go:build integration

package workflow

import (
	"strings"
	"testing"
)

// TestWritePromptTextToYAML_IntegrationWithCompiler verifies that WritePromptTextToYAML
// correctly handles large prompt text that would be used in actual workflow compilation.
// This test simulates what would happen if an embedded prompt file was very large.
func TestWritePromptTextToYAML_IntegrationWithCompiler(t *testing.T) {
	// Create a realistic scenario: a very long help text or documentation
	// that might be included as prompt instructions
	section := strings.Repeat("This is an important instruction line that provides guidance to the AI agent on how to perform its task correctly. ", 10)

	// Create 200 lines to ensure we exceed 20KB
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = section
	}
	largePromptText := strings.Join(lines, "\n")

	// Calculate total size
	totalSize := len(largePromptText)
	if totalSize < 20000 {
		t.Fatalf("Test setup error: prompt text should be at least 20000 bytes, got %d", totalSize)
	}

	var yaml strings.Builder
	indent := "          " // Standard indent used in workflow generation

	// Call the function as it would be called in real compilation
	WritePromptTextToYAML(&yaml, largePromptText, indent)

	result := yaml.String()

	// Verify multiple heredoc blocks were created
	heredocCount := strings.Count(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`)
	if heredocCount < 2 {
		t.Errorf("Expected multiple heredoc blocks for large text (%d bytes), got %d", totalSize, heredocCount)
	}

	// Verify we didn't exceed 5 chunks
	if heredocCount > 5 {
		t.Errorf("Expected at most 5 heredoc blocks (max limit), got %d", heredocCount)
	}

	// Verify each heredoc is closed
	eofCount := strings.Count(result, indent+"PROMPT_EOF")
	if eofCount != heredocCount {
		t.Errorf("Expected %d EOF markers to match %d heredoc blocks, got %d", heredocCount, heredocCount, eofCount)
	}

	// Verify the content is preserved (check first and last sections)
	firstSection := section[:100]
	lastSection := section[len(section)-100:]
	if !strings.Contains(result, firstSection) {
		t.Error("Expected to find beginning of original text in output")
	}
	if !strings.Contains(result, lastSection) {
		t.Error("Expected to find end of original text in output")
	}

	// Verify the YAML structure is valid (basic check)
	if !strings.Contains(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`) {
		t.Error("Expected proper heredoc syntax in output")
	}

	t.Logf("Successfully chunked %d bytes into %d heredoc blocks", totalSize, heredocCount)

	// Verify no lines are lost - extract content from heredoc blocks and compare
	extractedLines := extractLinesFromYAML(result, indent)
	originalLines := strings.Split(largePromptText, "\n")

	if len(extractedLines) != len(originalLines) {
		t.Errorf("Line count mismatch: expected %d lines, got %d lines", len(originalLines), len(extractedLines))
	}

	// Verify content integrity by checking line-by-line
	mismatchCount := 0
	for i := 0; i < len(originalLines) && i < len(extractedLines); i++ {
		if originalLines[i] != extractedLines[i] {
			mismatchCount++
			if mismatchCount <= 3 { // Only report first 3 mismatches
				t.Errorf("Line %d mismatch:\nExpected: %q\nGot: %q", i+1, originalLines[i], extractedLines[i])
			}
		}
	}

	if mismatchCount > 0 {
		t.Errorf("Total line mismatches: %d", mismatchCount)
	}
}

// TestWritePromptTextToYAML_RealWorldSizeSimulation simulates various real-world scenarios
// to ensure chunking works correctly across different text sizes.
func TestWritePromptTextToYAML_RealWorldSizeSimulation(t *testing.T) {
	tests := []struct {
		name           string
		textSize       int // approximate size in bytes
		linesCount     int // number of lines
		expectedChunks int // expected number of chunks
		maxChunks      int // should not exceed this
	}{
		{
			name:           "small prompt (< 1KB)",
			textSize:       500,
			linesCount:     10,
			expectedChunks: 1,
			maxChunks:      1,
		},
		{
			name:           "medium prompt (~10KB)",
			textSize:       10000,
			linesCount:     100,
			expectedChunks: 1,
			maxChunks:      1,
		},
		{
			name:           "large prompt (~25KB)",
			textSize:       25000,
			linesCount:     250,
			expectedChunks: 2,
			maxChunks:      2,
		},
		{
			name:           "very large prompt (~50KB)",
			textSize:       50000,
			linesCount:     500,
			expectedChunks: 3,
			maxChunks:      3,
		},
		{
			name:           "extremely large prompt (~120KB)",
			textSize:       120000,
			linesCount:     1200,
			expectedChunks: 5,
			maxChunks:      5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create text of approximately the desired size
			// Account for newlines: total size = linesCount * (lineSize + 1) - 1 (no trailing newline)
			lineSize := (tt.textSize + 1) / tt.linesCount // Adjust for newlines
			if lineSize < 1 {
				lineSize = 1
			}
			line := strings.Repeat("x", lineSize)
			lines := make([]string, tt.linesCount)
			for i := range lines {
				lines[i] = line
			}
			text := strings.Join(lines, "\n")

			var yaml strings.Builder
			indent := "          "

			WritePromptTextToYAML(&yaml, text, indent)

			result := yaml.String()
			heredocCount := strings.Count(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`)

			if heredocCount < tt.expectedChunks {
				t.Errorf("Expected at least %d chunks for %s, got %d", tt.expectedChunks, tt.name, heredocCount)
			}

			if heredocCount > tt.maxChunks {
				t.Errorf("Expected at most %d chunks for %s, got %d", tt.maxChunks, tt.name, heredocCount)
			}

			eofCount := strings.Count(result, indent+"PROMPT_EOF")
			if eofCount != heredocCount {
				t.Errorf("EOF count (%d) doesn't match heredoc count (%d) for %s", eofCount, heredocCount, tt.name)
			}

			t.Logf("%s: %d bytes chunked into %d blocks", tt.name, len(text), heredocCount)

			// Verify no lines are lost
			extractedLines := extractLinesFromYAML(result, indent)
			originalLines := strings.Split(text, "\n")

			if len(extractedLines) != len(originalLines) {
				t.Errorf("%s: Line count mismatch - expected %d lines, got %d lines", tt.name, len(originalLines), len(extractedLines))
			}
		})
	}
}

// extractLinesFromYAML extracts the actual content lines from a YAML heredoc output
// by parsing the heredoc blocks and removing the indent
func extractLinesFromYAML(yamlOutput string, indent string) []string {
	var lines []string
	inHeredoc := false

	for _, line := range strings.Split(yamlOutput, "\n") {
		// Check if we're starting a heredoc block
		if strings.Contains(line, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`) {
			inHeredoc = true
			continue
		}

		// Check if we're ending a heredoc block
		if strings.TrimSpace(line) == "PROMPT_EOF" {
			inHeredoc = false
			continue
		}

		// If we're in a heredoc block, extract the content line
		if inHeredoc {
			// Remove the indent from the line
			if strings.HasPrefix(line, indent) {
				contentLine := strings.TrimPrefix(line, indent)
				lines = append(lines, contentLine)
			}
		}
	}

	return lines
}

// TestWritePromptTextToYAML_NoDataLoss verifies that no lines or chunks are lost
// during the chunking process, even with edge cases.
func TestWritePromptTextToYAML_NoDataLoss(t *testing.T) {
	tests := []struct {
		name       string
		lines      []string
		expectLoss bool
	}{
		{
			name:       "single line",
			lines:      []string{"Single line of text"},
			expectLoss: false,
		},
		{
			name:       "multiple short lines",
			lines:      []string{"Line 1", "Line 2", "Line 3", "Line 4", "Line 5"},
			expectLoss: false,
		},
		{
			name:       "empty lines",
			lines:      []string{"Line 1", "", "Line 3", "", "Line 5"},
			expectLoss: false,
		},
		{
			name:       "very long single line",
			lines:      []string{strings.Repeat("x", 25000)},
			expectLoss: false,
		},
		{
			name: "exactly at chunk boundary",
			lines: func() []string {
				// Create lines that total exactly 20000 bytes with indent
				line := strings.Repeat("x", 100)
				lines := make([]string, 180)
				for i := range lines {
					lines[i] = line
				}
				return lines
			}(),
			expectLoss: false,
		},
		{
			name: "large number of lines requiring max chunks",
			lines: func() []string {
				line := strings.Repeat("y", 1000)
				lines := make([]string, 600)
				for i := range lines {
					lines[i] = line
				}
				return lines
			}(),
			expectLoss: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := strings.Join(tt.lines, "\n")
			var yaml strings.Builder
			indent := "          "

			WritePromptTextToYAML(&yaml, text, indent)

			result := yaml.String()

			// Extract lines from the YAML output
			extractedLines := extractLinesFromYAML(result, indent)

			// Verify line count
			if len(extractedLines) != len(tt.lines) {
				t.Errorf("Line count mismatch: expected %d lines, got %d lines", len(tt.lines), len(extractedLines))
				t.Logf("Original lines: %d", len(tt.lines))
				t.Logf("Extracted lines: %d", len(extractedLines))
			}

			// Verify content integrity
			mismatchCount := 0
			for i := 0; i < len(tt.lines) && i < len(extractedLines); i++ {
				if tt.lines[i] != extractedLines[i] {
					mismatchCount++
					if mismatchCount <= 3 {
						t.Errorf("Line %d mismatch:\nExpected: %q\nGot: %q", i+1, tt.lines[i], extractedLines[i])
					}
				}
			}

			if mismatchCount > 0 {
				t.Errorf("Total line mismatches: %d", mismatchCount)
			}
		})
	}
}

// TestWritePromptTextToYAML_ChunkIntegrity verifies that chunks are properly formed
// and that the chunking process maintains data integrity.
func TestWritePromptTextToYAML_ChunkIntegrity(t *testing.T) {
	// Create a large text that will require multiple chunks
	line := strings.Repeat("Test line with some content. ", 50)
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = line
	}
	text := strings.Join(lines, "\n")

	var yaml strings.Builder
	indent := "          "

	WritePromptTextToYAML(&yaml, text, indent)

	result := yaml.String()

	// Count heredoc blocks
	heredocCount := strings.Count(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`)

	t.Logf("Created %d heredoc blocks for %d lines (%d bytes)", heredocCount, len(lines), len(text))

	// Verify we have multiple chunks but not exceeding max
	if heredocCount < 2 {
		t.Errorf("Expected multiple chunks for large text, got %d", heredocCount)
	}

	if heredocCount > MaxPromptChunks {
		t.Errorf("Expected at most %d chunks, got %d", MaxPromptChunks, heredocCount)
	}

	// Verify all heredocs are properly closed
	eofCount := strings.Count(result, indent+"PROMPT_EOF")
	if eofCount != heredocCount {
		t.Errorf("Heredoc closure mismatch: %d opens, %d closes", heredocCount, eofCount)
	}

	// Verify no data loss
	extractedLines := extractLinesFromYAML(result, indent)
	if len(extractedLines) != len(lines) {
		t.Errorf("Line count mismatch: expected %d, got %d", len(lines), len(extractedLines))
	}

	// Verify content integrity by checking a few random samples
	sampleIndices := []int{0, len(lines) / 4, len(lines) / 2, len(lines) * 3 / 4, len(lines) - 1}
	for _, idx := range sampleIndices {
		if idx < len(lines) && idx < len(extractedLines) {
			if lines[idx] != extractedLines[idx] {
				t.Errorf("Content mismatch at line %d:\nExpected: %q\nGot: %q", idx+1, lines[idx], extractedLines[idx])
			}
		}
	}
}
