//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestWritePromptTextToYAML_SmallText(t *testing.T) {
	var yaml strings.Builder
	text := "This is a small text\nWith a few lines\nThat doesn't need chunking"
	indent := "          "

	WritePromptTextToYAML(&yaml, text, indent)

	result := yaml.String()

	// Should have exactly one heredoc block
	if strings.Count(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`) != 1 {
		t.Errorf("Expected 1 heredoc block for small text, got %d", strings.Count(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`))
	}

	// Should contain all original lines
	if !strings.Contains(result, "This is a small text") {
		t.Error("Expected to find original text in output")
	}
	if !strings.Contains(result, "With a few lines") {
		t.Error("Expected to find original text in output")
	}
	if !strings.Contains(result, "That doesn't need chunking") {
		t.Error("Expected to find original text in output")
	}

	// Should have proper EOF markers
	if strings.Count(result, indent+"PROMPT_EOF") != 1 {
		t.Errorf("Expected 1 EOF marker, got %d", strings.Count(result, indent+"PROMPT_EOF"))
	}
}

func TestWritePromptTextToYAML_LargeText(t *testing.T) {
	var yaml strings.Builder
	// Create text that exceeds 20000 characters
	longLine := strings.Repeat("This is a very long line of content that will be repeated many times to exceed the character limit. ", 10)
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = longLine
	}
	text := strings.Join(lines, "\n")
	indent := "          "

	// Calculate expected size
	totalSize := 0
	for _, line := range lines {
		totalSize += len(indent) + len(line) + 1
	}

	// This should create multiple chunks since each line is ~1000 chars and we have 50 lines
	WritePromptTextToYAML(&yaml, text, indent)

	result := yaml.String()

	// Should have multiple heredoc blocks
	heredocCount := strings.Count(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`)
	if heredocCount < 2 {
		t.Errorf("Expected at least 2 heredoc blocks for large text (total size ~%d bytes), got %d", totalSize, heredocCount)
	}

	// Should not exceed 5 chunks (max limit)
	if heredocCount > 5 {
		t.Errorf("Expected at most 5 heredoc blocks, got %d", heredocCount)
	}

	// Should have matching EOF markers
	eofCount := strings.Count(result, indent+"PROMPT_EOF")
	if eofCount != heredocCount {
		t.Errorf("Expected %d EOF markers to match %d heredoc blocks, got %d", heredocCount, heredocCount, eofCount)
	}

	// Should contain original content (or at least the beginning if truncated)
	firstLine := strings.Split(text, "\n")[0]
	if !strings.Contains(result, firstLine[:50]) {
		t.Error("Expected to find beginning of original text in output")
	}
}

func TestWritePromptTextToYAML_ExactChunkBoundary(t *testing.T) {
	var yaml strings.Builder
	indent := "          "

	// Create text that's exactly at the 20000 character boundary
	// Each line: indent (10) + line (100) + newline (1) = 111 bytes
	// 180 lines = 19,980 bytes (just under 20000)
	line := strings.Repeat("x", 100)
	lines := make([]string, 180)
	for i := range lines {
		lines[i] = line
	}
	text := strings.Join(lines, "\n")

	WritePromptTextToYAML(&yaml, text, indent)

	result := yaml.String()

	// Should have exactly 1 heredoc block since we're just under the limit
	heredocCount := strings.Count(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`)
	if heredocCount != 1 {
		t.Errorf("Expected 1 heredoc block for text just under limit, got %d", heredocCount)
	}
}

func TestWritePromptTextToYAML_MaxChunksLimit(t *testing.T) {
	var yaml strings.Builder
	indent := "          "

	// Create text that would need more than 5 chunks (if we allowed it)
	// Each line: indent (10) + line (1000) + newline (1) = 1011 bytes
	// 600 lines = ~606,600 bytes
	// At 20000 bytes per chunk, this would need ~31 chunks, but we limit to 5
	line := strings.Repeat("y", 1000)
	lines := make([]string, 600)
	for i := range lines {
		lines[i] = line
	}
	text := strings.Join(lines, "\n")

	WritePromptTextToYAML(&yaml, text, indent)

	result := yaml.String()

	// Should have exactly 5 heredoc blocks (the maximum)
	heredocCount := strings.Count(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`)
	if heredocCount != 5 {
		t.Errorf("Expected exactly 5 heredoc blocks (max limit), got %d", heredocCount)
	}

	// Should have matching EOF markers
	eofCount := strings.Count(result, indent+"PROMPT_EOF")
	if eofCount != 5 {
		t.Errorf("Expected 5 EOF markers, got %d", eofCount)
	}
}

func TestWritePromptTextToYAML_EmptyText(t *testing.T) {
	var yaml strings.Builder
	text := ""
	indent := "          "

	WritePromptTextToYAML(&yaml, text, indent)

	result := yaml.String()

	// Should have at least one heredoc block (even for empty text)
	if strings.Count(result, `cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`) < 1 {
		t.Error("Expected at least 1 heredoc block even for empty text")
	}

	// Should have matching EOF markers
	if strings.Count(result, indent+"PROMPT_EOF") < 1 {
		t.Error("Expected at least 1 EOF marker")
	}
}

func TestChunkLines_SmallInput(t *testing.T) {
	lines := []string{"line1", "line2", "line3"}
	indent := "          "
	maxSize := 20000
	maxChunks := 5

	chunks := chunkLines(lines, indent, maxSize, maxChunks)

	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk for small input, got %d", len(chunks))
	}

	if len(chunks[0]) != 3 {
		t.Errorf("Expected chunk to contain 3 lines, got %d", len(chunks[0]))
	}
}

func TestChunkLines_ExceedsSize(t *testing.T) {
	// Create lines that will exceed maxSize
	line := strings.Repeat("x", 1000)
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = line
	}

	indent := "          "
	maxSize := 20000
	maxChunks := 5

	chunks := chunkLines(lines, indent, maxSize, maxChunks)

	// Should have multiple chunks
	if len(chunks) < 2 {
		t.Errorf("Expected at least 2 chunks, got %d", len(chunks))
	}

	// Verify each chunk (except possibly the last) stays within size limit
	for i, chunk := range chunks {
		size := 0
		for _, line := range chunk {
			size += len(indent) + len(line) + 1
		}

		// Last chunk might exceed if we hit maxChunks limit
		if i < len(chunks)-1 && size > maxSize {
			t.Errorf("Chunk %d exceeds size limit: %d > %d", i, size, maxSize)
		}
	}

	// Verify total lines are preserved
	totalLines := 0
	for _, chunk := range chunks {
		totalLines += len(chunk)
	}
	if totalLines != len(lines) {
		t.Errorf("Expected %d total lines, got %d", len(lines), totalLines)
	}
}

func TestChunkLines_MaxChunksEnforced(t *testing.T) {
	// Create many lines that would need more than maxChunks
	line := strings.Repeat("x", 1000)
	lines := make([]string, 600)
	for i := range lines {
		lines[i] = line
	}

	indent := "          "
	maxSize := 20000
	maxChunks := 5

	chunks := chunkLines(lines, indent, maxSize, maxChunks)

	// Should have exactly maxChunks
	if len(chunks) != maxChunks {
		t.Errorf("Expected exactly %d chunks (max limit), got %d", maxChunks, len(chunks))
	}

	// Verify all lines are included (even if last chunk is large)
	totalLines := 0
	for _, chunk := range chunks {
		totalLines += len(chunk)
	}
	if totalLines != len(lines) {
		t.Errorf("Expected %d total lines, got %d", len(lines), totalLines)
	}
}

func TestChunkLines_EmptyInput(t *testing.T) {
	lines := []string{}
	indent := "          "
	maxSize := 20000
	maxChunks := 5

	chunks := chunkLines(lines, indent, maxSize, maxChunks)

	// Should return at least one empty chunk
	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk for empty input, got %d", len(chunks))
	}

	if len(chunks[0]) != 0 {
		t.Errorf("Expected empty chunk, got %d lines", len(chunks[0]))
	}
}

func TestChunkLines_SingleLineExceedsLimit(t *testing.T) {
	// Single line that exceeds maxSize
	line := strings.Repeat("x", 25000)
	lines := []string{line}

	indent := "          "
	maxSize := 20000
	maxChunks := 5

	chunks := chunkLines(lines, indent, maxSize, maxChunks)

	// Should still have one chunk with that single line
	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk, got %d", len(chunks))
	}

	if len(chunks[0]) != 1 {
		t.Errorf("Expected 1 line in chunk, got %d", len(chunks[0]))
	}
}
