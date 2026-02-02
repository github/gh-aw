package workflow

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var shLog = logger.New("workflow:sh")

// Prompt file paths at runtime (copied by setup action)
const (
	promptsDir                 = "/opt/gh-aw/prompts"
	prContextPromptFile        = "pr_context_prompt.md"
	tempFolderPromptFile       = "temp_folder_prompt.md"
	playwrightPromptFile       = "playwright_prompt.md"
	markdownPromptFile         = "markdown.md"
	cacheMemoryPromptFile      = "cache_memory_prompt.md"
	cacheMemoryPromptMultiFile = "cache_memory_prompt_multi.md"
)

// GitHub context prompt is kept embedded because it contains GitHub Actions expressions
// that need to be extracted at compile time. Moving this to a runtime file would require
// reading and parsing the file during compilation, which is more complex.
//
//go:embed prompts/github_context_prompt.md
var githubContextPromptText string

// WritePromptFileToYAML writes a shell command to cat a prompt file from /opt/gh-aw/prompts/
// This replaces the previous approach of embedding prompt text in the binary.
func WritePromptFileToYAML(yaml *strings.Builder, filename string, indent string) {
	shLog.Printf("Writing prompt file reference to YAML: file=%s", filename)
	promptPath := fmt.Sprintf("%s/%s", promptsDir, filename)
	yaml.WriteString(indent + fmt.Sprintf("cat \"%s\" >> \"$GH_AW_PROMPT\"\n", promptPath))
}

// WriteShellScriptToYAML writes a shell script with proper indentation to a strings.Builder
func WriteShellScriptToYAML(yaml *strings.Builder, script string, indent string) {
	scriptLines := strings.Split(script, "\n")
	for _, line := range scriptLines {
		// Skip empty lines at the beginning or end
		if strings.TrimSpace(line) != "" {
			fmt.Fprintf(yaml, "%s%s\n", indent, line)
		}
	}
}

// WritePromptTextToYAML writes static prompt text to a YAML heredoc with proper indentation.
// Use this function for prompt text that contains NO variable placeholders or expressions.
// It chunks the text into groups of lines of less than MaxPromptChunkSize characters, with a maximum of MaxPromptChunks chunks.
// Each chunk is written as a separate heredoc to avoid GitHub Actions step size limits (21KB).
//
// For prompt text with variable placeholders that need substitution, use WritePromptTextToYAMLWithPlaceholders instead.
func WritePromptTextToYAML(yaml *strings.Builder, text string, indent string) {
	shLog.Printf("Writing prompt text to YAML: text_size=%d bytes, chunks=%d", len(text), len(strings.Split(text, "\n")))
	textLines := strings.Split(text, "\n")
	chunks := chunkLines(textLines, indent, MaxPromptChunkSize, MaxPromptChunks)
	shLog.Printf("Created %d chunks for prompt text", len(chunks))

	// Write each chunk as a separate heredoc
	// For static prompt text without variables, use direct cat to file
	for _, chunk := range chunks {
		yaml.WriteString(indent + "cat << 'PROMPT_EOF' >> \"$GH_AW_PROMPT\"\n")
		for _, line := range chunk {
			fmt.Fprintf(yaml, "%s%s\n", indent, line)
		}
		yaml.WriteString(indent + "PROMPT_EOF\n")
	}
}

// WritePromptTextToYAMLWithPlaceholders writes prompt text with variable placeholders to a YAML heredoc with proper indentation.
// Use this function for prompt text containing __VAR__ placeholders that will be substituted with sed commands.
// The caller is responsible for adding the sed substitution commands after calling this function.
// It uses placeholder format (__VAR__) instead of shell variable expansion, to prevent template injection.
//
// For static prompt text without variables, use WritePromptTextToYAML instead.
func WritePromptTextToYAMLWithPlaceholders(yaml *strings.Builder, text string, indent string) {
	textLines := strings.Split(text, "\n")
	chunks := chunkLines(textLines, indent, MaxPromptChunkSize, MaxPromptChunks)

	// Write each chunk as a separate heredoc
	// Use direct cat to file (append mode) - placeholders will be substituted with sed
	for _, chunk := range chunks {
		yaml.WriteString(indent + "cat << 'PROMPT_EOF' >> \"$GH_AW_PROMPT\"\n")
		for _, line := range chunk {
			fmt.Fprintf(yaml, "%s%s\n", indent, line)
		}
		yaml.WriteString(indent + "PROMPT_EOF\n")
	}
}

// chunkLines splits lines into chunks where each chunk's total size (including indent) is less than maxSize.
// Returns at most maxChunks chunks. If content exceeds the limit, it truncates at the last chunk.
func chunkLines(lines []string, indent string, maxSize int, maxChunks int) [][]string {
	shLog.Printf("Chunking lines: total_lines=%d, max_size=%d, max_chunks=%d", len(lines), maxSize, maxChunks)
	if len(lines) == 0 {
		return [][]string{{}}
	}

	var chunks [][]string
	var currentChunk []string
	currentSize := 0

	for _, line := range lines {
		// Calculate size including indent and newline
		lineSize := len(indent) + len(line) + 1

		// If adding this line would exceed the limit, start a new chunk
		if currentSize+lineSize > maxSize && len(currentChunk) > 0 {
			// Check if we've reached the maximum number of chunks
			if len(chunks) >= maxChunks-1 {
				// We're at the last allowed chunk, so add remaining lines to current chunk
				currentChunk = append(currentChunk, line)
				currentSize += lineSize
				continue
			}

			// Start a new chunk
			shLog.Printf("Starting new chunk: previous_chunk_size=%d, chunks_so_far=%d", currentSize, len(chunks))
			chunks = append(chunks, currentChunk)
			currentChunk = []string{line}
			currentSize = lineSize
		} else {
			currentChunk = append(currentChunk, line)
			currentSize += lineSize
		}
	}

	// Add the last chunk if there's content
	if len(currentChunk) > 0 {
		chunks = append(chunks, currentChunk)
	}

	// If we still have no chunks, return an empty chunk
	if len(chunks) == 0 {
		return [][]string{{}}
	}

	shLog.Printf("Chunking complete: created %d chunks", len(chunks))
	return chunks
}
