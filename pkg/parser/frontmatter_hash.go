package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/jsonutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/typeutil"
)

var frontmatterHashLog = logger.New("parser:frontmatter_hash")

// templateExpressionRegex matches ${{ ... }} expressions (pre-compiled for performance)
var templateExpressionRegex = regexp.MustCompile(`\$\{\{(.*?)\}\}`)

// parseBoolFromFrontmatter extracts a boolean value from a frontmatter map.
// Returns false if the key is absent, the map is nil, or the value is not a bool.
func parseBoolFromFrontmatter(m map[string]any, key string) bool {
	return typeutil.ParseBool(m, key)
}

// FileReader is a function type that reads file content
// This abstraction allows for different file reading strategies (disk, GitHub API, in-memory, etc.)
type FileReader func(filePath string) ([]byte, error)

// DefaultFileReader reads files from disk using os.ReadFile
var DefaultFileReader FileReader = os.ReadFile

const maxFrontmatterHashInputBytes = 1 << 20

// marshalJSONWithoutHTMLEscape marshals a value to JSON without HTML escaping
// This matches JavaScript's JSON.stringify behavior
func marshalJSONWithoutHTMLEscape(v any) (string, error) {
	return jsonutil.MarshalCompactNoHTMLEscape(v)
}

// marshalSorted recursively marshals data with sorted keys
func marshalSorted(data any) string {
	switch v := data.(type) {
	case map[string]any:
		return marshalSortedMap(v)

	case []any:
		return marshalSortedSlice(v)

	case string, int, int64, float64, bool, nil:
		return marshalSortedValue(v, "primitive value")

	default:
		return marshalSortedValue(v, fmt.Sprintf("value of type %T", v))
	}
}

func marshalSortedMap(v map[string]any) string {
	if len(v) == 0 {
		return "{}"
	}

	keys := sliceutil.SortedKeys(v)

	var result strings.Builder
	result.WriteString("{")
	for i, key := range keys {
		if i > 0 {
			result.WriteString(",")
		}
		keyJSON, err := marshalJSONWithoutHTMLEscape(key)
		if err != nil {
			frontmatterHashLog.Printf("Warning: failed to marshal key %s: %v", key, err)
			continue
		}
		result.WriteString(keyJSON)
		result.WriteString(":")
		result.WriteString(marshalSorted(v[key]))
	}
	result.WriteString("}")
	return result.String()
}

func marshalSortedSlice(v []any) string {
	if len(v) == 0 {
		return "[]"
	}

	var result strings.Builder
	result.WriteString("[")
	for i, elem := range v {
		if i > 0 {
			result.WriteString(",")
		}
		result.WriteString(marshalSorted(elem))
	}
	result.WriteString("]")
	return result.String()
}

func marshalSortedValue(v any, valueDescription string) string {
	jsonStr, err := marshalJSONWithoutHTMLEscape(v)
	if err != nil {
		frontmatterHashLog.Printf("Warning: failed to marshal %s: %v", valueDescription, err)
		return "null"
	}
	return jsonStr
}

// ComputeFrontmatterHashFromParsedContent computes the frontmatter hash from already-parsed
// workflow data, avoiding a redundant file read when content has already been loaded.
// frontmatterText is the raw text between the --- delimiters (e.g. WorkflowData.FrontmatterYAML).
// markdownBody is the raw markdown body before include expansion (e.g. WorkflowData.RawMarkdown).
// parsedFrontmatter is used to detect the inlined-imports flag.
// baseDir is the directory containing the workflow file, used for resolving imports.
func ComputeFrontmatterHashFromParsedContent(frontmatterText, markdownBody string, parsedFrontmatter map[string]any, baseDir string, cache *ImportCache, fileReader FileReader) (string, error) {
	frontmatterHashLog.Printf("Computing hash from parsed content (baseDir=%s)", baseDir)

	inlinedImports := parseBoolFromFrontmatter(parsedFrontmatter, "inlined-imports")

	var relevantExpressions []string
	var fullBody string
	if inlinedImports {
		fullBody = normalizeFrontmatterText(markdownBody)
	} else {
		relevantExpressions = extractRelevantTemplateExpressions(markdownBody)
		relevantExpressions = mergeSortedUniqueStrings(relevantExpressions, collectRuntimeImportTemplateExpressions(frontmatterText, markdownBody, baseDir, fileReader))
	}

	return computeFrontmatterHashTextBasedWithReader(frontmatterText, fullBody, baseDir, cache, relevantExpressions, fileReader)
}

// ComputeFrontmatterHashFromFile computes the frontmatter hash for a workflow file
// using text-based approach (no YAML parsing) to match JavaScript implementation
func ComputeFrontmatterHashFromFile(filePath string, cache *ImportCache) (string, error) {
	return ComputeFrontmatterHashFromFileWithReader(filePath, cache, DefaultFileReader)
}

// ComputeFrontmatterHashFromFileWithParsedFrontmatter computes the frontmatter hash using
// a pre-parsed frontmatter map. The parsedFrontmatter must not be nil; callers are responsible
// for parsing the frontmatter before calling this function.
func ComputeFrontmatterHashFromFileWithParsedFrontmatter(filePath string, parsedFrontmatter map[string]any, cache *ImportCache, fileReader FileReader) (string, error) {
	frontmatterHashLog.Printf("Computing hash for file: %s", filePath)

	// Read file content using the provided file reader
	content, err := fileReader(filePath)
	if err != nil {
		return "", fmt.Errorf("could not read file %q; ensure the path exists and is readable, then retry: %w", filePath, err)
	}

	return computeFrontmatterHashFromContent(string(content), parsedFrontmatter, filePath, cache, fileReader)
}

// ComputeFrontmatterHashFromFileWithReader computes the frontmatter hash for a workflow file
// using a custom file reader function (e.g., for GitHub API, in-memory file system, etc.)
// It parses the frontmatter once from the file content, then delegates to the core logic.
func ComputeFrontmatterHashFromFileWithReader(filePath string, cache *ImportCache, fileReader FileReader) (string, error) {
	frontmatterHashLog.Printf("Computing hash for file: %s", filePath)

	// Read file content using the provided file reader
	content, err := fileReader(filePath)
	if err != nil {
		return "", fmt.Errorf("could not read file %q; ensure the path exists and is readable, then retry: %w", filePath, err)
	}

	// Parse frontmatter once from content; treat inlined-imports as false if parsing fails
	var parsedFrontmatter map[string]any
	if parsed, parseErr := ExtractFrontmatterFromContent(string(content)); parseErr == nil {
		parsedFrontmatter = parsed.Frontmatter
	}

	return computeFrontmatterHashFromContent(string(content), parsedFrontmatter, filePath, cache, fileReader)
}

// computeFrontmatterHashFromContent is the shared core that computes the hash given the
// already-read file content and pre-parsed frontmatter map (may be nil).
func computeFrontmatterHashFromContent(content string, parsedFrontmatter map[string]any, filePath string, cache *ImportCache, fileReader FileReader) (string, error) {
	frontmatterHashLog.Printf("Computing hash from content: filePath=%s, content_size=%d bytes", filePath, len(content))

	// Extract frontmatter and markdown as text (no YAML parsing)
	frontmatterText, markdown, err := extractFrontmatterAndBodyText(content)
	if err != nil {
		return "", fmt.Errorf("could not extract frontmatter from %q; ensure the workflow frontmatter has a closing delimiter, then retry: %w", filePath, err)
	}

	// Get base directory for resolving imports
	baseDir := filepath.Dir(filePath)

	// Detect inlined-imports from the pre-parsed frontmatter map.
	// If nil (parsing failed or not provided), inlined-imports is treated as false.
	inlinedImports := parseBoolFromFrontmatter(parsedFrontmatter, "inlined-imports")
	frontmatterHashLog.Printf("Hash strategy: inlined_imports=%v, markdown_size=%d bytes", inlinedImports, len(markdown))

	// When inlined-imports is enabled, the entire markdown body is compiled into the lock
	// file, so any change to the body must invalidate the hash. Include the full body text.
	// Otherwise, only extract the relevant template expressions (env./vars. references).
	var relevantExpressions []string
	var fullBody string
	if inlinedImports {
		fullBody = normalizeFrontmatterText(markdown)
	} else {
		relevantExpressions = extractRelevantTemplateExpressions(markdown)
		relevantExpressions = mergeSortedUniqueStrings(relevantExpressions, collectRuntimeImportTemplateExpressions(frontmatterText, markdown, baseDir, fileReader))
	}

	// Compute hash using text-based approach with custom file reader
	return computeFrontmatterHashTextBasedWithReader(frontmatterText, fullBody, baseDir, cache, relevantExpressions, fileReader)
}

// extractRelevantTemplateExpressions extracts template expressions from markdown
// that reference env. or vars. contexts
func extractRelevantTemplateExpressions(markdown string) []string {
	frontmatterHashLog.Printf("Extracting relevant template expressions from markdown: size=%d bytes", len(markdown))
	var expressions []string
	seen := make(map[string]struct {
	})

	// Regex to match ${{ ... }} expressions
	matches := templateExpressionRegex.FindAllStringSubmatch(markdown, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		content := strings.TrimSpace(match[1])

		// Check if expression references env. or vars.
		if strings.Contains(content, "env.") || strings.Contains(content, "vars.") {
			// Store the full expression including ${{ }}
			expr := match[0]
			// Deduplicate expressions
			if !setutil.Contains(seen, expr) {
				expressions = append(expressions, expr)
				seen[expr] = struct {
				}{}
			}
		}
	}

	// Sort for deterministic output
	sort.Strings(expressions)
	frontmatterHashLog.Printf("Found %d relevant template expression(s) referencing env./vars.", len(expressions))
	return expressions
}

func extractAllTemplateExpressions(markdown string) []string {
	var expressions []string
	seen := make(map[string]struct {
	})
	matches := templateExpressionRegex.FindAllStringSubmatch(markdown, -1)
	for _, match := range matches {
		if len(match) < 2 || strings.TrimSpace(match[1]) == "" {
			continue
		}
		expr := match[0]
		if !setutil.Contains(seen, expr) {
			expressions = append(expressions, expr)
			seen[expr] = struct {
			}{}
		}
	}
	sort.Strings(expressions)
	return expressions
}

func mergeSortedUniqueStrings(groups ...[]string) []string {
	seen := make(map[string]struct {
	})
	var merged []string
	for _, group := range groups {
		for _, item := range group {
			if item == "" || setutil.Contains(seen, item) {
				continue
			}
			merged = append(merged, item)
			seen[item] = struct {
			}{}
		}
	}
	sort.Strings(merged)
	return merged
}

// extractFrontmatterAndBodyText extracts frontmatter as raw text without parsing YAML
// Returns: frontmatterText, markdownBody, error
func extractFrontmatterAndBodyText(content string) (string, string, error) {
	// Normalize CRLF to LF so that files with Windows line-endings produce the
	// same frontmatter text (and therefore the same hash) as equivalent LF files.
	content = strings.ReplaceAll(content, "\r\n", "\n")

	lines := strings.Split(content, "\n")

	// Check if content starts with frontmatter delimiter
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		// No frontmatter
		return "", content, nil
	}

	// Find end of frontmatter
	endIndex := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIndex = i
			break
		}
	}

	if endIndex == -1 {
		return "", "", errors.New("frontmatter not properly closed")
	}

	// Extract frontmatter text (lines between --- delimiters)
	frontmatterText := strings.Join(lines[1:endIndex], "\n")

	// Extract markdown body (everything after closing ---)
	var markdown string
	if endIndex+1 < len(lines) {
		markdown = strings.Join(lines[endIndex+1:], "\n")
	}

	return frontmatterText, markdown, nil
}

// normalizeFrontmatterText normalizes frontmatter text for consistent hashing
// Removes leading/trailing whitespace and normalizes line endings
func normalizeFrontmatterText(text string) string {
	// Normalize Windows line endings to Unix
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	// Trim leading and trailing whitespace
	return strings.TrimSpace(normalized)
}

func validateFrontmatterHashInputSize(normalizedFrontmatterText string, normalizedImportedFrontmatterTexts []string) error {
	totalBytes := len(normalizedFrontmatterText)
	for _, text := range normalizedImportedFrontmatterTexts {
		totalBytes += len(text)
	}

	if totalBytes > maxFrontmatterHashInputBytes {
		return fmt.Errorf("frontmatter hash input exceeds %d bytes after normalization", maxFrontmatterHashInputBytes)
	}

	return nil
}

// extractImportsFromText extracts import paths from frontmatter text using simple text parsing.
// For the array form, extracts all top-level array items under "imports:".
// For the object form, extracts array items under "imports.aw:" only
// (the "apm-packages" subfield contains package names, not import paths).
func extractImportsFromText(frontmatterText string) []string {
	var imports []string
	lines := strings.Split(frontmatterText, "\n")

	state := importExtractionState{}

	for i := range lines {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if state.beginImportsBlock(line, trimmed) {
			continue
		}

		if !state.inImports {
			continue
		}

		lineIndent := indentationOf(line)
		if state.exitsImportsBlock(trimmed, lineIndent) {
			break
		}
		if state.handleSubfield(trimmed, lineIndent) {
			continue
		}

		if item, ok := state.extractImportItem(trimmed, lineIndent); ok {
			imports = append(imports, item)
		}
	}

	return imports
}

type importExtractionState struct {
	inImports    bool
	baseIndent   int
	inAwSubfield bool
	awIndent     int
	isObjectForm bool
}

func (s *importExtractionState) beginImportsBlock(line, trimmed string) bool {
	if !strings.HasPrefix(trimmed, "imports:") {
		return false
	}
	s.inImports = true
	s.inAwSubfield = false
	s.isObjectForm = false
	s.baseIndent = indentationOf(line)
	return true
}

func (s *importExtractionState) exitsImportsBlock(trimmed string, lineIndent int) bool {
	return lineIndent <= s.baseIndent && trimmed != "" && !strings.HasPrefix(trimmed, "#")
}

func (s *importExtractionState) handleSubfield(trimmed string, lineIndent int) bool {
	if lineIndent == s.baseIndent+2 && strings.HasPrefix(trimmed, "aw:") {
		s.isObjectForm = true
		s.inAwSubfield = true
		s.awIndent = lineIndent
		return true
	}
	if s.isObjectForm && lineIndent == s.baseIndent+2 && strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "-") {
		s.inAwSubfield = false
		return true
	}
	return false
}

func (s *importExtractionState) extractImportItem(trimmed string, lineIndent int) (string, bool) {
	if !strings.HasPrefix(trimmed, "-") {
		return "", false
	}
	if s.isObjectForm && (!s.inAwSubfield || lineIndent <= s.awIndent) {
		return "", false
	}

	item := strings.TrimSpace(trimmed[1:])
	// Extract path from object-form imports (uses: <path> / path: <path>).
	// These are valid import forms; extract the path value so that changes to
	// imported files are reflected in the hash.
	if after, ok := strings.CutPrefix(item, "uses:"); ok {
		item = strings.TrimSpace(after)
	} else if after, ok := strings.CutPrefix(item, "path:"); ok {
		item = strings.TrimSpace(after)
	}
	item = strings.Trim(item, `"'`)
	if item == "" {
		return "", false
	}
	return item, true
}

func indentationOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// processImportsTextBased processes imports from frontmatter using text-based parsing
// Returns: importedFiles (list of import paths), importedFrontmatterTexts (list of frontmatter texts)
func processImportsTextBased(frontmatterText, baseDir string, visited map[string]struct {
}, fileReader FileReader) ([]string, []string, error) {
	var importedFiles []string
	var importedFrontmatterTexts []string

	// Extract imports from frontmatter text
	imports := extractImportsFromText(frontmatterText)

	if len(imports) == 0 {
		return importedFiles, importedFrontmatterTexts, nil
	}

	frontmatterHashLog.Printf("Processing %d import(s) text-based from baseDir=%s", len(imports), baseDir)

	// Sort imports for deterministic processing
	sort.Strings(imports)

	for _, importPath := range imports {
		// Resolve import path relative to base directory
		fullPath := filepath.Join(baseDir, importPath)

		// Skip if already visited (cycle detection)
		if setutil.Contains(visited, fullPath) {
			frontmatterHashLog.Printf("Skipping already-visited import (cycle detection): %s", fullPath)
			continue
		}
		visited[fullPath] = struct {
		}{}

		// Read imported file using the provided file reader
		content, err := fileReader(fullPath)
		if err != nil {
			// Skip missing imports silently (matches JavaScript behavior)
			continue
		}

		// Extract frontmatter text from imported file
		importFrontmatterText, _, err := extractFrontmatterAndBodyText(string(content))
		if err != nil {
			// Skip files with invalid frontmatter
			continue
		}

		// Add to imported files and texts
		importedFiles = append(importedFiles, importPath)
		importedFrontmatterTexts = append(importedFrontmatterTexts, importFrontmatterText)

		// Recursively process imports in the imported file
		importBaseDir := filepath.Dir(fullPath)
		nestedFiles, nestedTexts, err := processImportsTextBased(importFrontmatterText, importBaseDir, visited, fileReader)
		if err != nil {
			// Continue processing other imports even if one fails
			continue
		}

		// Add nested imports
		importedFiles = append(importedFiles, nestedFiles...)
		importedFrontmatterTexts = append(importedFrontmatterTexts, nestedTexts...)
	}

	frontmatterHashLog.Printf("Processed imports: found %d imported file(s) from baseDir=%s", len(importedFiles), baseDir)
	return importedFiles, importedFrontmatterTexts, nil
}

// collectImportedBodies processes imports from frontmatter and returns the body texts of all
// transitively imported files. Used to include imported file bodies in the body hash.
func collectImportedBodies(frontmatterText, baseDir string, visited map[string]struct {
}, fileReader FileReader) ([]string, error) {
	var importedBodyTexts []string

	imports := extractImportsFromText(frontmatterText)
	if len(imports) == 0 {
		return importedBodyTexts, nil
	}

	sort.Strings(imports)

	for _, importPath := range imports {
		fullPath := filepath.Join(baseDir, importPath)

		if setutil.Contains(visited, fullPath) {
			continue
		}
		visited[fullPath] = struct {
		}{}

		content, err := fileReader(fullPath)
		if err != nil {
			continue
		}

		importFrontmatterText, importBody, err := extractFrontmatterAndBodyText(string(content))
		if err != nil {
			continue
		}

		importedBodyTexts = append(importedBodyTexts, importBody)

		importBaseDir := filepath.Dir(fullPath)
		nestedBodies, err := collectImportedBodies(importFrontmatterText, importBaseDir, visited, fileReader)
		if err != nil {
			continue
		}
		importedBodyTexts = append(importedBodyTexts, nestedBodies...)
	}

	return importedBodyTexts, nil
}

type runtimeImportReference struct {
	path      string
	startLine int
	endLine   int
}

var (
	runtimeImportReferenceRe = regexp.MustCompile(`\{\{#(?:runtime-import|import)\??(?:[ \t]+|[ \t]*:[ \t]*)([^{}]+?)\}\}`)
	runtimeImportLineRangeRe = regexp.MustCompile(`^(.+?):(\d+)-(\d+)$`)
)

func collectRuntimeImportTemplateExpressions(frontmatterText, markdownBody, baseDir string, fileReader FileReader) []string {
	seen := map[string]struct{}{}
	expressions := extractRuntimeImportTemplateExpressionsFromMarkdown(markdownBody, baseDir, seen, fileReader)

	importedBodies, err := collectImportedBodies(frontmatterText, baseDir, map[string]struct{}{}, fileReader)
	if err != nil {
		return expressions
	}
	for _, body := range importedBodies {
		expressions = mergeSortedUniqueStrings(expressions, extractAllTemplateExpressions(body))
		expressions = mergeSortedUniqueStrings(expressions, extractRuntimeImportTemplateExpressionsFromMarkdown(body, baseDir, seen, fileReader))
	}

	return expressions
}

func extractRuntimeImportTemplateExpressionsFromMarkdown(markdownBody, baseDir string, seen map[string]struct{}, fileReader FileReader) []string {
	refs := extractRuntimeImportReferences(markdownBody)
	if len(refs) == 0 {
		return nil
	}

	var expressions []string
	for _, ref := range refs {
		body, ok := readRuntimeImportBodyForHash(ref, baseDir, seen, fileReader)
		if !ok {
			continue
		}
		expressions = mergeSortedUniqueStrings(expressions, extractAllTemplateExpressions(body))
		expressions = mergeSortedUniqueStrings(expressions, extractRuntimeImportTemplateExpressionsFromMarkdown(body, baseDir, seen, fileReader))
	}
	return expressions
}

func extractRuntimeImportReferences(markdownBody string) []runtimeImportReference {
	if markdownBody == "" {
		return nil
	}
	var refs []runtimeImportReference
	seen := make(map[string]struct {
	})
	matches := runtimeImportReferenceRe.FindAllStringSubmatch(markdownBody, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		target := strings.TrimSpace(match[1])
		if target == "" {
			continue
		}
		ref := runtimeImportReference{path: target}
		if rangeMatch := runtimeImportLineRangeRe.FindStringSubmatch(target); len(rangeMatch) == 4 {
			ref.path = strings.TrimSpace(rangeMatch[1])
			fmt.Sscanf(rangeMatch[2], "%d", &ref.startLine)
			fmt.Sscanf(rangeMatch[3], "%d", &ref.endLine)
		}
		if strings.HasPrefix(ref.path, "http://") || strings.HasPrefix(ref.path, "https://") {
			continue
		}
		key := fmt.Sprintf("%s:%d-%d", filepath.ToSlash(ref.path), ref.startLine, ref.endLine)
		if setutil.Contains(seen, key) {
			continue
		}
		refs = append(refs, ref)
		seen[key] = struct {
		}{}
	}
	return refs
}

func readRuntimeImportBodyForHash(ref runtimeImportReference, baseDir string, seen map[string]struct{}, fileReader FileReader) (string, bool) {
	for _, candidate := range runtimeImportHashCandidatePaths(ref.path, baseDir) {
		key := fmt.Sprintf("%s:%d-%d", candidate, ref.startLine, ref.endLine)
		if setutil.Contains(seen, key) {
			return "", false
		}
		if runtimeImportHashLocalPathExists(candidate) && !runtimeImportHashRealPathAllowed(candidate, baseDir) {
			continue
		}
		rawContent, err := fileReader(candidate)
		if err != nil {
			continue
		}
		seen[key] = struct {
		}{}
		content := string(rawContent)
		if ref.startLine > 0 || ref.endLine > 0 {
			content = applyRuntimeImportLineRangeForHash(content, ref.startLine, ref.endLine)
		}
		body, extractErr := ExtractMarkdownContent(content)
		if extractErr == nil {
			content = body
		}
		return content, true
	}
	return "", false
}

func runtimeImportHashLocalPathExists(candidate string) bool {
	_, err := os.Stat(candidate)
	return err == nil
}

func runtimeImportHashRealPathAllowed(candidate, baseDir string) bool {
	workspaceRoot := runtimeImportHashWorkspaceRoot(baseDir)
	for _, base := range []string{
		filepath.Join(workspaceRoot, strings.TrimSuffix(constants.GithubDir, "/")),
		filepath.Join(workspaceRoot, ".agents"),
	} {
		if realPathWithinBaseForHash(candidate, base) {
			return true
		}
	}
	return false
}

func realPathWithinBaseForHash(pathToCheck, baseDir string) bool {
	realBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return false
	}
	realPath, err := filepath.EvalSymlinks(pathToCheck)
	if err != nil {
		return false
	}
	relativePath, err := filepath.Rel(realBase, realPath)
	return err == nil && relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) && !filepath.IsAbs(relativePath)
}

func applyRuntimeImportLineRangeForHash(content string, startLine, endLine int) string {
	lines := strings.Split(content, "\n")
	start := startLine
	if start <= 0 {
		start = 1
	}
	end := endLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > end || start > len(lines) {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}

func runtimeImportHashCandidatePaths(importPath, baseDir string) []string {
	normalized := filepath.ToSlash(strings.TrimSpace(importPath))
	if strings.HasPrefix(normalized, "/") {
		normalized = strings.TrimLeft(normalized, "/")
		if !strings.HasPrefix(normalized, constants.GithubDir) && !strings.HasPrefix(normalized, ".agents/") {
			return nil
		}
	}
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "" || strings.HasPrefix(normalized, "../") || strings.Contains(normalized, "/../") {
		return nil
	}

	workspaceRoot := runtimeImportHashWorkspaceRoot(baseDir)
	if strings.HasPrefix(normalized, ".agents/") {
		return []string{filepath.Join(workspaceRoot, filepath.FromSlash(normalized))}
	}
	if strings.HasPrefix(normalized, constants.GithubDir) {
		return []string{filepath.Join(workspaceRoot, filepath.FromSlash(normalized))}
	}

	candidates := []string{
		filepath.Join(workspaceRoot, strings.TrimSuffix(constants.GithubDir, "/"), filepath.FromSlash(normalized)),
		filepath.Join(workspaceRoot, strings.TrimSuffix(constants.GithubDir, "/"), "workflows", filepath.FromSlash(normalized)),
		filepath.Join(baseDir, filepath.FromSlash(normalized)),
	}
	deduped := candidates[:0]
	seen := make(map[string]struct {
	})
	for _, candidate := range candidates {
		clean := filepath.Clean(candidate)
		if !setutil.Contains(seen, clean) {
			deduped = append(deduped, clean)
			seen[clean] = struct {
			}{}
		}
	}
	return deduped
}

func runtimeImportHashWorkspaceRoot(baseDir string) string {
	normalized := filepath.ToSlash(baseDir)
	if before, _, ok := strings.Cut(normalized, "/.github/"); ok {
		return filepath.FromSlash(before)
	}
	if strings.HasSuffix(normalized, "/.github") {
		return filepath.Dir(baseDir)
	}
	return baseDir
}

// ComputeBodyHashFromParsedContent computes a SHA-256 hash of the markdown body (after frontmatter)
// including the bodies of all transitively imported files. This hash covers changes to the prompt
// body that are not captured by the frontmatter hash.
// markdownBody is the raw markdown body (after the frontmatter --- delimiter).
// frontmatterText is the raw frontmatter text, used to resolve imports.
func ComputeBodyHashFromParsedContent(markdownBody, frontmatterText, baseDir string, fileReader FileReader) (string, error) {
	frontmatterHashLog.Printf("Computing body hash from parsed content (baseDir=%s)", baseDir)

	normalizedBody := normalizeFrontmatterText(markdownBody)

	visited := make(map[string]struct {
	})
	importedBodies, err := collectImportedBodies(frontmatterText, baseDir, visited, fileReader)
	if err != nil {
		return "", fmt.Errorf("could not process imports for body hash; ensure imported workflow files exist and are readable, then retry: %w", err)
	}

	allParts := []string{normalizedBody}

	if len(importedBodies) > 0 {
		normalizedBodies := make([]string, len(importedBodies))
		for i, b := range importedBodies {
			normalizedBodies[i] = normalizeFrontmatterText(b)
		}
		sort.Strings(normalizedBodies)
		allParts = append(allParts, normalizedBodies...)
	}

	combined := strings.Join(allParts, "\n---\n")
	frontmatterHashLog.Printf("Body hash combined length: %d bytes", len(combined))

	hash := sha256.Sum256([]byte(combined))
	hashHex := hex.EncodeToString(hash[:])
	frontmatterHashLog.Printf("Computed body hash: %s", hashHex)
	return hashHex, nil
}

// ComputeBodyHashFromFile computes the body hash for a workflow file from disk.
func ComputeBodyHashFromFile(filePath string) (string, error) {
	content, err := DefaultFileReader(filePath)
	if err != nil {
		return "", fmt.Errorf("could not read file %q; ensure the path exists and is readable, then retry: %w", filePath, err)
	}

	frontmatterText, markdownBody, err := extractFrontmatterAndBodyText(string(content))
	if err != nil {
		return "", fmt.Errorf("could not extract frontmatter from %q; ensure the workflow frontmatter has a closing delimiter, then retry: %w", filePath, err)
	}

	baseDir := filepath.Dir(filePath)
	return ComputeBodyHashFromParsedContent(markdownBody, frontmatterText, baseDir, DefaultFileReader)
}

// computeFrontmatterHashTextBasedWithReader computes the hash using text-based approach with custom file reader.
// When markdown is non-empty, it is included as the full body text in the canonical data (used for
// inlined-imports mode where the entire body is compiled into the lock file).
func computeFrontmatterHashTextBasedWithReader(frontmatterText, markdown, baseDir string, cache *ImportCache, expressions []string, fileReader FileReader) (string, error) {
	frontmatterHashLog.Print("Computing frontmatter hash using text-based approach")

	// Process imports using text-based parsing with custom file reader
	visited := make(map[string]struct {
	})
	importedFiles, importedFrontmatterTexts, err := processImportsTextBased(frontmatterText, baseDir, visited, fileReader)
	if err != nil {
		return "", fmt.Errorf("could not process imports; ensure imported workflow files exist and are readable, then retry: %w", err)
	}

	// Build canonical representation from text
	canonical := make(map[string]any)

	normalizedFrontmatterText := normalizeFrontmatterText(frontmatterText)
	normalizedImportedTexts := make([]string, len(importedFrontmatterTexts))
	for i, text := range importedFrontmatterTexts {
		normalizedImportedTexts[i] = normalizeFrontmatterText(text)
	}

	if err := validateFrontmatterHashInputSize(normalizedFrontmatterText, normalizedImportedTexts); err != nil {
		return "", err
	}

	// Add the main frontmatter text as-is (trimmed and normalized)
	canonical["frontmatter-text"] = normalizedFrontmatterText

	// Add sorted imported files list
	if len(importedFiles) > 0 {
		sort.Strings(importedFiles)
		canonical["imports"] = importedFiles
	}

	// Add sorted imported frontmatter texts (concatenated with delimiter)
	if len(normalizedImportedTexts) > 0 {
		sort.Strings(normalizedImportedTexts)
		canonical["imported-frontmatters"] = strings.Join(normalizedImportedTexts, "\n---\n")
	}

	// When inlined-imports is enabled, include the full markdown body so any content
	// change invalidates the hash. Otherwise, include only relevant template expressions.
	if markdown != "" {
		canonical["body-text"] = markdown
	} else if len(expressions) > 0 {
		canonical["template-expressions"] = expressions
	}

	// Serialize to canonical JSON
	canonicalJSON := marshalSorted(canonical)

	frontmatterHashLog.Printf("Canonical JSON length: %d bytes", len(canonicalJSON))

	// Compute SHA-256 hash
	hash := sha256.Sum256([]byte(canonicalJSON))
	hashHex := hex.EncodeToString(hash[:])

	frontmatterHashLog.Printf("Computed hash: %s", hashHex)
	return hashHex, nil
}
