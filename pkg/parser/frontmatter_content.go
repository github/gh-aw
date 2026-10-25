package parser

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// levelPattern matches heading markers (H1-H3) at the start of a line with flexible spacing
var levelPattern = regexp.MustCompile(`^(#{1,3})[\s\t]+`)

// FrontmatterResult holds parsed frontmatter and markdown content
type FrontmatterResult struct {
	Frontmatter map[string]any
	Markdown    string
	// Additional fields for error context
	FrontmatterLines []string       // Original frontmatter lines for error context
	FrontmatterStart int            // Line number where frontmatter starts (1-based)
	FieldLines       map[string]int // Absolute line numbers (1-based) of top-level frontmatter keys in the file
}

// ExtractFrontmatterFromContent parses YAML frontmatter from markdown content string
func ExtractFrontmatterFromContent(content string) (*FrontmatterResult, error) {
	parserLog.Printf("Extracting frontmatter from content: size=%d bytes", len(content))
	firstNewline, firstLine := splitFirstLine(content)
	if !isFrontmatterDelimiterLine(firstLine) {
		parserLog.Print("No frontmatter delimiter found, returning content as markdown")
		return noFrontmatterResult(content), nil
	}

	searchStart := computeFrontmatterSearchStart(content, firstNewline)
	frontmatterEndStart, markdownStart, err := findFrontmatterDelimiters(content, searchStart)
	if err != nil {
		return nil, err
	}

	frontmatterYAML := content[searchStart:frontmatterEndStart]
	frontmatterLines, fieldLines := extractFrontmatterMetadata(frontmatterYAML, frontmatterStartLine)
	frontmatter, err := parseFrontmatterYAML(frontmatterYAML)
	if err != nil {
		return nil, err
	}
	markdown := extractMarkdownAfterFrontmatter(content, markdownStart)

	parserLog.Printf("Successfully extracted frontmatter: fields=%d, markdown_size=%d bytes", len(frontmatter), len(markdown))
	return &FrontmatterResult{
		Frontmatter:      frontmatter,
		Markdown:         strings.TrimSpace(markdown),
		FrontmatterLines: frontmatterLines,
		FrontmatterStart: frontmatterStartLine,
		FieldLines:       fieldLines,
	}, nil
}

const frontmatterStartLine = 2 // Line 2 is where frontmatter content starts (after opening ---)

func splitFirstLine(content string) (int, string) {
	firstNewline := strings.IndexByte(content, '\n')
	if firstNewline < 0 {
		return firstNewline, content
	}
	return firstNewline, content[:firstNewline]
}

func noFrontmatterResult(content string) *FrontmatterResult {
	return &FrontmatterResult{
		Frontmatter:      make(map[string]any),
		Markdown:         content,
		FrontmatterLines: []string{},
		FrontmatterStart: 0,
	}
}

func computeFrontmatterSearchStart(content string, firstNewline int) int {
	if firstNewline >= 0 {
		return firstNewline + 1
	}
	return len(content)
}

func findFrontmatterDelimiters(content string, searchStart int) (int, int, error) {
	frontmatterEndStart := -1
	markdownStart := len(content)
	for cursor := searchStart; cursor <= len(content); {
		lineStart, lineEnd, nextCursor := frontmatterLineBounds(content, cursor)
		if isFrontmatterDelimiterLine(content[lineStart:lineEnd]) {
			frontmatterEndStart = lineStart
			markdownStart = nextCursor
			break
		}
		if nextCursor > len(content) {
			break
		}
		cursor = nextCursor
	}
	if frontmatterEndStart == -1 {
		return 0, 0, errors.New("frontmatter not properly closed")
	}
	return frontmatterEndStart, markdownStart, nil
}

func frontmatterLineBounds(content string, cursor int) (int, int, int) {
	lineStart := cursor
	lineEnd := len(content)
	nextCursor := len(content) + 1
	if cursor < len(content) {
		if relNewline := strings.IndexByte(content[cursor:], '\n'); relNewline >= 0 {
			lineEnd = cursor + relNewline
			nextCursor = lineEnd + 1
		}
	}
	return lineStart, lineEnd, nextCursor
}

func extractFrontmatterMetadata(frontmatterYAML string, frontmatterStart int) ([]string, map[string]int) {
	if frontmatterYAML == "" {
		return []string{}, map[string]int{}
	}

	lines := make([]string, 0, strings.Count(frontmatterYAML, "\n")+1)
	fieldLines := make(map[string]int)
	relLine := 0

	for line := range strings.SplitSeq(frontmatterYAML, "\n") {
		relLine++
		lines = append(lines, line)

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if line != "" && line[0] != ' ' && line[0] != '\t' {
			colonIdx := strings.IndexByte(trimmed, ':')
			if colonIdx > 0 {
				key := strings.TrimSpace(trimmed[:colonIdx])
				if key != "" && !strings.ContainsAny(key, " \t{}[]\"'") {
					if _, alreadySeen := fieldLines[key]; !alreadySeen {
						fieldLines[key] = relLine + frontmatterStart - 1
					}
				}
			}
		}
	}

	if strings.HasSuffix(frontmatterYAML, "\n") {
		lines = lines[:len(lines)-1]
	}

	return lines, fieldLines
}

func parseFrontmatterYAML(frontmatterYAML string) (map[string]any, error) {
	frontmatterYAML = strings.ReplaceAll(frontmatterYAML, "\u00A0", " ")
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(frontmatterYAML), &frontmatter); err != nil {
		formattedErr := FormatYAMLError(err, 2, frontmatterYAML)
		return nil, &FormattedParserError{formatted: "failed to parse frontmatter:\n" + formattedErr, cause: err}
	}
	if frontmatter == nil {
		frontmatter = make(map[string]any)
	}
	return frontmatter, nil
}

func extractMarkdownAfterFrontmatter(content string, markdownStart int) string {
	if markdownStart <= len(content) {
		return content[markdownStart:]
	}
	return ""
}

// isFrontmatterDelimiterLine returns true when a line consists of "---" with optional surrounding whitespace.
func isFrontmatterDelimiterLine(line string) bool {
	// Fast path for common delimiters.
	if line == "---" || line == "---\r" {
		return true
	}

	// Fast path for ASCII-trimmable whitespace.
	start, end := 0, len(line)
	for start < end {
		switch line[start] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			start++
		default:
			goto leftTrimmed
		}
	}
leftTrimmed:
	if start >= end {
		return false
	}
	for end > start {
		switch line[end-1] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			end--
		default:
			goto rightTrimmed
		}
	}
rightTrimmed:
	if end-start == 3 && line[start] == '-' && line[start+1] == '-' && line[start+2] == '-' {
		return true
	}

	// Fallback keeps previous behavior for uncommon Unicode whitespace.
	return strings.TrimSpace(line) == "---"
}

// ExtractFrontmatterFromBuiltinFile is a caching wrapper around ExtractFrontmatterFromContent
// for builtin virtual files. Because builtin files are registered once at startup and never
// change, the parsed FrontmatterResult is identical across calls. This function caches the
// first parse result in builtinFrontmatterCache and returns the cached (shared) value on
// subsequent calls, avoiding repeated YAML parsing for frequently imported engine definition
// files.
//
// IMPORTANT: The returned *FrontmatterResult is a shared, read-only reference.
// Callers MUST NOT mutate the result or any of its fields (Frontmatter map, slices, etc.).
// Use ExtractFrontmatterFromContent directly when you need a mutable copy.
//
// path must start with BuiltinPathPrefix ("@builtin:"); an error is returned otherwise.
func ExtractFrontmatterFromBuiltinFile(path string, content []byte) (*FrontmatterResult, error) {
	if !strings.HasPrefix(path, BuiltinPathPrefix) {
		return nil, fmt.Errorf("ExtractFrontmatterFromBuiltinFile: path %q does not start with %q", path, BuiltinPathPrefix)
	}
	if cached, ok := GetBuiltinFrontmatterCache(path); ok {
		return cached, nil
	}
	result, err := ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return nil, err
	}
	// SetBuiltinFrontmatterCache uses LoadOrStore so concurrent races are safe.
	return SetBuiltinFrontmatterCache(path, result), nil
}

// ExtractMarkdownSection extracts a specific section from markdown content
// Supports H1-H3 headers and proper nesting (matches bash implementation)
func ExtractMarkdownSection(content, sectionName string) (string, error) {
	parserLog.Printf("Extracting markdown section: section=%s, content_size=%d bytes", sectionName, len(content))
	scanner := bufio.NewScanner(strings.NewReader(content))
	var sectionContent bytes.Buffer
	inSection := false
	var sectionLevel int

	// Create regex pattern to match headers at any level (H1-H3) with flexible spacing
	//nolint:regexpdynamicpattern // The section name is quoted, and compilation errors are returned to the caller.
	headerPattern, err := regexp.Compile(`^(#{1,3})[\s\t]+` + regexp.QuoteMeta(sectionName) + `[\s\t]*$`)
	if err != nil {
		return "", fmt.Errorf("failed to compile markdown section pattern: %w", err)
	}

	for scanner.Scan() {
		line := scanner.Text()

		// Check if this line matches our target section
		if matches := headerPattern.FindStringSubmatch(line); matches != nil {
			inSection = true
			sectionLevel = len(matches[1]) // Number of # characters
			sectionContent.WriteString(line + "\n")
			continue
		}

		// If we're in the section, check if we've hit another header at same or higher level
		if inSection {
			if levelMatches := levelPattern.FindStringSubmatch(line); levelMatches != nil {
				currentLevel := len(levelMatches[1])
				// Stop if we encounter same or higher level header
				if currentLevel <= sectionLevel {
					break
				}
			}
			sectionContent.WriteString(line + "\n")
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to scan markdown section %q: %w", sectionName, err)
	}
	if !inSection {
		parserLog.Printf("Section not found: %s", sectionName)
		return "", fmt.Errorf("section '%s' not found", sectionName)
	}

	extractedContent := strings.TrimSpace(sectionContent.String())
	parserLog.Printf("Successfully extracted section: size=%d bytes", len(extractedContent))
	return extractedContent, nil
}

// ExtractMarkdownContent extracts only the markdown content (excluding frontmatter)
// This matches the bash extract_markdown function
func ExtractMarkdownContent(content string) (string, error) {
	result, err := ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", err
	}

	return result.Markdown, nil
}

// findH1WorkflowName scans at most the first 64 lines of markdownBody for an H1 header
// and returns the trimmed title. Returns "" if no H1 is found within those lines.
func findH1WorkflowName(markdownBody string) string {
	const maxLines = 64
	lineCount := 0
	for line := range strings.Lines(markdownBody) {
		lineCount++
		if lineCount > maxLines {
			break
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
	}
	return ""
}

// ExtractWorkflowNameFromMarkdownBody extracts the workflow name from an already-extracted
// markdown body (i.e. the content after the frontmatter has been stripped). This is more
// efficient than ExtractWorkflowNameFromMarkdown or ExtractWorkflowNameFromContent because it
// avoids the redundant file-read and YAML-parse that those functions perform when the caller
// already holds the parsed FrontmatterResult.
func ExtractWorkflowNameFromMarkdownBody(markdownBody string, virtualPath string) (string, error) {
	parserLog.Printf("Extracting workflow name from markdown body: virtualPath=%s, size=%d bytes", virtualPath, len(markdownBody))

	if name := findH1WorkflowName(markdownBody); name != "" {
		parserLog.Printf("Found workflow name from H1 header: %s", name)
		return name, nil
	}

	defaultName := generateDefaultWorkflowName(virtualPath)
	parserLog.Printf("No H1 header found, using default name: %s", defaultName)
	return defaultName, nil
}

// ExtractWorkflowNameFromContent extracts the workflow name from markdown content string.
// This is the in-memory equivalent of ExtractWorkflowNameFromMarkdown, used by Wasm builds
// where filesystem access is unavailable.
func ExtractWorkflowNameFromContent(content string, virtualPath string) (string, error) {
	parserLog.Printf("Extracting workflow name from content: virtualPath=%s, size=%d bytes", virtualPath, len(content))

	markdownContent, err := ExtractMarkdownContent(content)
	if err != nil {
		return "", err
	}

	if name := findH1WorkflowName(markdownContent); name != "" {
		parserLog.Printf("Found workflow name from H1 header: %s", name)
		return name, nil
	}

	defaultName := generateDefaultWorkflowName(virtualPath)
	parserLog.Printf("No H1 header found, using default name: %s", defaultName)
	return defaultName, nil
}

// generateDefaultWorkflowName creates a default workflow name from filename
// This matches the bash implementation's fallback behavior
func generateDefaultWorkflowName(filePath string) string {
	// Get base filename without extension
	baseName := filepath.Base(filePath)
	baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))

	// Convert hyphens to spaces
	baseName = strings.ReplaceAll(baseName, "-", " ")

	// Capitalize first letter of each word
	words := strings.Fields(baseName)
	for i, word := range words {
		if word != "" {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}

	return strings.Join(words, " ")
}
