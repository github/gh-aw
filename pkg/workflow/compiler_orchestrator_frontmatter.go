package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var orchestratorFrontmatterLog = logger.New("workflow:compiler_orchestrator_frontmatter")

// frontmatterParseResult holds the results of parsing and validating frontmatter
type frontmatterParseResult struct {
	cleanPath                string
	content                  []byte
	frontmatterResult        *parser.FrontmatterResult
	frontmatterForValidation map[string]any
	markdownDir              string
	isSharedWorkflow         bool
	// isRedirectOnly is true when the file has a redirect field but no 'on' trigger.
	// Such files are redirect-only placeholders that point to a workflow's new location.
	isRedirectOnly bool
	// redirectTarget holds the redirect destination (workflow spec or URL) for informational messages.
	redirectTarget string
}

type frontmatterReadError struct {
	message string
}

func (e frontmatterReadError) Error() string {
	return e.message
}

func (c *Compiler) validateEngineBeforeSchema(
	cleanPath string,
	content []byte,
	result *parser.FrontmatterResult,
	frontmatterForValidation map[string]any,
) error {
	engineValue, ok := frontmatterForValidation["engine"].(string)
	// Keep the empty-string default-engine behavior, but let whitespace-only values
	// fall through to getAgenticEngine so they surface as invalid engine typos.
	if !ok || engineValue == "" {
		return nil
	}

	if _, err := c.getAgenticEngine(engineValue); err != nil {
		line := result.FieldLines["engine"]
		if line == 0 {
			line = findFrontmatterFieldLine(result.FrontmatterLines, result.FrontmatterStart, "engine")
		}
		if line == 0 {
			line = 1
		}

		return formatCompilerErrorWithContext(
			cleanPath,
			line,
			// Point to the field key for invalid string engine names so the location
			// stays stable even when the specific invalid value changes.
			1,
			"error",
			err.Error(),
			err,
			readSourceContextLines(content, line),
		)
	}

	return nil
}

// parseFrontmatterSection reads the workflow file and parses its frontmatter.
// It returns a frontmatterParseResult containing the parsed data and validation information.
// If the workflow is detected as a shared workflow (no 'on' field), isSharedWorkflow is set to true.
// If the workflow is detected as a redirect-only file (has redirect but no 'on' field),
// isRedirectOnly is set to true with the redirect target in redirectTarget.
func (c *Compiler) parseFrontmatterSection(markdownPath string) (*frontmatterParseResult, error) {
	orchestratorFrontmatterLog.Printf("Starting frontmatter parsing: %s", markdownPath)
	workflowLog.Printf("Reading file: %s", markdownPath)

	cleanPath, content, contentString, result, err := c.readAndParseFrontmatter(markdownPath)
	if err != nil {
		return nil, err
	}

	// Treat comment-only frontmatter as present, but keep whitespace-only
	// and missing blocks rejected as "no frontmatter found".
	if !hasMeaningfulFrontmatter(result) {
		orchestratorFrontmatterLog.Print("No frontmatter found in file")
		return nil, errors.New("no frontmatter found")
	}

	// Preprocess schedule fields to convert human-friendly format to cron expressions
	if err := c.preprocessScheduleFields(result.Frontmatter, cleanPath, contentString); err != nil {
		orchestratorFrontmatterLog.Printf("Schedule preprocessing failed: %v", err)
		return nil, err
	}

	// Create a copy of frontmatter without internal markers for schema validation
	// Keep the original frontmatter with markers for YAML generation
	frontmatterForValidation := c.copyFrontmatterWithoutInternalMarkers(result.Frontmatter)

	// Check if user accidentally used "triggers:" instead of the correct "on:" keyword
	if _, hasTriggers := frontmatterForValidation["triggers"]; hasTriggers {
		return nil, fmt.Errorf("%s: invalid frontmatter key 'triggers:' — use 'on:' to define workflow triggers", cleanPath)
	}

	if sharedResult, handled, err := c.parseSharedOrRedirectWorkflow(cleanPath, content, result, frontmatterForValidation); err != nil {
		return nil, err
	} else if handled {
		return sharedResult, nil
	}

	if err := c.validateMainWorkflowFrontmatter(cleanPath, content, result, frontmatterForValidation); err != nil {
		return nil, err
	}

	workflowLog.Printf("Frontmatter: %d chars, Markdown: %d chars", len(result.Frontmatter), len(result.Markdown))

	return createFrontmatterParseResult(cleanPath, content, result, frontmatterForValidation), nil
}

func (c *Compiler) readAndParseFrontmatter(markdownPath string) (string, []byte, string, *parser.FrontmatterResult, error) {
	// Clean the path to prevent path traversal issues (gosec G304)
	// filepath.Clean removes ".." and other problematic path elements
	cleanPath := filepath.Clean(markdownPath)

	// Read the file
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		orchestratorFrontmatterLog.Printf("Failed to read file: %s, error: %v", cleanPath, err)
		// Keep the user-facing message while avoiding exposure of os.PathError internals.
		return "", nil, "", nil, fmt.Errorf("failed to read file: %w", frontmatterReadError{message: err.Error()})
	}
	contentString := string(content)

	workflowLog.Printf("File size: %d bytes", len(content))

	// Parse frontmatter and markdown
	orchestratorFrontmatterLog.Printf("Parsing frontmatter from file: %s", cleanPath)
	result, err := parser.ExtractFrontmatterFromContent(contentString)
	if err != nil {
		orchestratorFrontmatterLog.Printf("Frontmatter extraction failed: %v", err)
		// Use FrontmatterStart from result if available, otherwise default to line 2 (after opening ---)
		frontmatterStart := 2
		if result != nil && result.FrontmatterStart > 0 {
			frontmatterStart = result.FrontmatterStart
		}
		return "", nil, "", nil, c.createFrontmatterError(cleanPath, contentString, err, frontmatterStart)
	}

	return cleanPath, content, contentString, result, nil
}

func (c *Compiler) parseSharedOrRedirectWorkflow(
	cleanPath string,
	content []byte,
	result *parser.FrontmatterResult,
	frontmatterForValidation map[string]any,
) (*frontmatterParseResult, bool, error) {
	// Check if "on" field is missing or contains only import-safe shared fields -
	// if so, treat as a shared/imported workflow.
	onValue, hasOnField := frontmatterForValidation["on"]
	if hasOnField && !parser.IsImportSafeSharedWorkflowOn(onValue) {
		return nil, false, nil
	}

	// Check if this is a redirect-only placeholder (has a redirect field but no 'on' trigger).
	// Redirect-only files are distinct from regular shared workflows: they are placeholders
	// that point to a workflow's new canonical location and are not intended to be imported.
	// They occur when `gh aw add` downloads a workflow that has been moved but the redirect
	// was not resolved to the full content during download.
	if !hasOnField {
		if redirectVal, hasRedirect := frontmatterForValidation["redirect"]; hasRedirect {
			if redirectStr, ok := redirectVal.(string); ok {
				if redirectTarget := strings.TrimSpace(redirectStr); redirectTarget != "" {
					detectionLog.Printf("Redirect-only workflow detected: redirect=%s", redirectTarget)
					return createFrontmatterParseResult(cleanPath, content, result, frontmatterForValidation, withRedirectOnly(redirectTarget)), true, nil
				}
			}
		}
	}

	detectionLog.Printf("No 'on' field detected - treating as shared agentic workflow")

	// Validate as an included/shared workflow (uses main_workflow_schema with forbidden field checks)
	if err := parser.ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatterForValidation, cleanPath); err != nil {
		orchestratorFrontmatterLog.Printf("Shared workflow validation failed: %v", err)
		return nil, true, err
	}

	return createFrontmatterParseResult(cleanPath, content, result, frontmatterForValidation, withSharedWorkflow()), true, nil
}

func (c *Compiler) validateMainWorkflowFrontmatter(
	cleanPath string,
	content []byte,
	result *parser.FrontmatterResult,
	frontmatterForValidation map[string]any,
) error {
	// For main workflows (with 'on' field), markdown content is required
	if result.Markdown == "" {
		orchestratorFrontmatterLog.Print("No markdown content found for main workflow")
		return errors.New("no markdown content found")
	}

	if err := c.validateEngineBeforeSchema(cleanPath, content, result, frontmatterForValidation); err != nil {
		orchestratorFrontmatterLog.Printf("String engine pre-validation failed: %v", err)
		return err
	}

	if err := c.validateMainWorkflowSchemaAndEventFilters(cleanPath, frontmatterForValidation); err != nil {
		return err
	}
	if err := c.validateMainWorkflowMarkdownConstraints(result.Markdown); err != nil {
		return err
	}

	c.emitMainWorkflowWarnings(cleanPath, result.Markdown)
	return nil
}

func (c *Compiler) validateMainWorkflowSchemaAndEventFilters(cleanPath string, frontmatterForValidation map[string]any) error {
	// Validate main workflow frontmatter contains only expected entries
	orchestratorFrontmatterLog.Printf("Validating main workflow frontmatter schema")
	if err := parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatterForValidation, cleanPath); err != nil {
		orchestratorFrontmatterLog.Printf("Main workflow frontmatter validation failed: %v", err)
		return err
	}
	if err := validateFrontmatterSkills(frontmatterForValidation); err != nil {
		orchestratorFrontmatterLog.Printf("Skills frontmatter validation failed: %v", err)
		return err
	}
	if err := validateFrontmatterPlugins(frontmatterForValidation); err != nil {
		orchestratorFrontmatterLog.Printf("Plugins frontmatter validation failed: %v", err)
		return err
	}

	// Validate event filter mutual exclusivity (branches/branches-ignore, paths/paths-ignore)
	if err := ValidateEventFilters(frontmatterForValidation); err != nil {
		orchestratorFrontmatterLog.Printf("Event filter validation failed: %v", err)
		return err
	}

	// Validate that push triggers are scoped to specific branches or tags to prevent fan-out.
	// In strict mode this is an error; in non-strict mode it is downgraded to a warning.
	if err := ValidatePushBranchScope(frontmatterForValidation); err != nil {
		if c.effectiveStrictMode(frontmatterForValidation) {
			orchestratorFrontmatterLog.Printf("Push branch/tag scope validation failed: %v", err)
			return err
		}
		orchestratorFrontmatterLog.Printf("Push branch/tag scope warning (non-strict mode): %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(err.Error()))
		c.IncrementWarningCount()
	}

	// Validate event type names in the 'on:' section for potential typos
	if err := ValidateEventTypes(frontmatterForValidation); err != nil {
		orchestratorFrontmatterLog.Printf("Event type validation failed: %v", err)
		return err
	}

	// Validate glob pattern syntax in event filters (branches, tags, paths, etc.)
	if err := ValidateGlobPatterns(frontmatterForValidation); err != nil {
		orchestratorFrontmatterLog.Printf("Glob pattern validation failed: %v", err)
		return err
	}

	// Validate that the runs-on field does not specify unsupported runner types (e.g. macOS)
	if err := validateRunsOn(frontmatterForValidation, cleanPath); err != nil {
		orchestratorFrontmatterLog.Printf("runs-on validation failed: %v", err)
		return err
	}

	return nil
}

func (c *Compiler) validateMainWorkflowMarkdownConstraints(markdown string) error {
	// Validate that @include/@import directives are not used inside template regions
	if err := validateNoIncludesInTemplateRegions(markdown); err != nil {
		orchestratorFrontmatterLog.Printf("Template region validation failed: %v", err)
		return fmt.Errorf("template region validation failed: %w", err)
	}

	// Validate that pre-expanded __GH_AW_EXPERIMENTS_*__ placeholders are not used in template conditions
	if err := validateNoPreExpandedExperimentPlaceholders(markdown); err != nil {
		orchestratorFrontmatterLog.Printf("Pre-expanded experiment placeholder validation failed: %v", err)
		return fmt.Errorf("template condition validation failed: %w", err)
	}

	return nil
}

func (c *Compiler) emitMainWorkflowWarnings(cleanPath, markdown string) {
	// Warn when experiment comparison expressions use double-quoted string literals.
	// GitHub Actions expression syntax only supports single-quoted string literals, so
	// the compiler converts double quotes to single quotes automatically — but authors
	// should fix the source to use single quotes to keep it consistent with the output.
	for _, w := range detectDoubleQuotedExperimentComparisons(markdown) {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(cleanPath, "warning", w))
		c.IncrementWarningCount()
	}

	// Warn when template separators are embedded in the middle of a line.
	// Keeping separators on their own lines improves compatibility with the
	// template renderer and avoids brittle inline condition blocks.
	for _, w := range detectMidlineTemplateSeparators(markdown) {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(cleanPath, "warning", w))
		c.IncrementWarningCount()
	}
}

type frontmatterParseResultOption func(*frontmatterParseResult)

func withSharedWorkflow() frontmatterParseResultOption {
	return func(result *frontmatterParseResult) {
		result.isSharedWorkflow = true
	}
}

func withRedirectOnly(target string) frontmatterParseResultOption {
	return func(result *frontmatterParseResult) {
		result.isRedirectOnly = true
		result.redirectTarget = target
	}
}

func createFrontmatterParseResult(
	cleanPath string,
	content []byte,
	result *parser.FrontmatterResult,
	frontmatterForValidation map[string]any,
	options ...frontmatterParseResultOption,
) *frontmatterParseResult {
	parseResult := &frontmatterParseResult{
		cleanPath:                cleanPath,
		content:                  content,
		frontmatterResult:        result,
		frontmatterForValidation: frontmatterForValidation,
		markdownDir:              filepath.Dir(cleanPath),
	}

	for _, option := range options {
		option(parseResult)
	}

	return parseResult
}

// copyFrontmatterWithoutInternalMarkers creates a copy of frontmatter without internal marker fields.
// This is used for schema validation while preserving markers in the original for YAML generation.
// As an optimization, it checks whether any internal markers are present before allocating a copy.
// If no markers exist (the common case for most workflows), the original map is returned as-is.
func (c *Compiler) copyFrontmatterWithoutInternalMarkers(frontmatter map[string]any) map[string]any {
	// Fast path: check if any internal markers are present before allocating a copy.
	// Markers may appear in on.issues, on.pull_request, on.discussion, and on.issue_comment sub-maps.
	hasMarkers := false
	if onValue, hasOn := frontmatter["on"]; hasOn {
		if onMap, ok := onValue.(map[string]any); ok {
			for _, eventKey := range []string{"issues", "pull_request", "discussion", "issue_comment"} {
				if sectionValue, exists := onMap[eventKey]; exists {
					if sectionMap, ok := sectionValue.(map[string]any); ok {
						if _, hasMarker := sectionMap["__gh_aw_native_label_filter__"]; hasMarker {
							hasMarkers = true
							break
						}
					}
				}
			}
		}
	}

	// If no markers found, return the original map directly (no copy needed).
	if !hasMarkers {
		return frontmatter
	}

	// Markers exist: build a copy without them.
	copy := make(map[string]any, len(frontmatter))
	for k, v := range frontmatter {
		if k == "on" {
			// Special handling for "on" field - need to deep copy and remove markers
			if onMap, ok := v.(map[string]any); ok {
				onCopy := make(map[string]any, len(onMap))
				for onKey, onValue := range onMap {
					if onKey == "issues" || onKey == "pull_request" || onKey == "discussion" || onKey == "issue_comment" {
						// Deep copy the section and remove marker
						if sectionMap, ok := onValue.(map[string]any); ok {
							sectionCopy := make(map[string]any, len(sectionMap))
							for sectionKey, sectionValue := range sectionMap {
								if sectionKey != "__gh_aw_native_label_filter__" {
									sectionCopy[sectionKey] = sectionValue
								}
							}
							onCopy[onKey] = sectionCopy
						} else {
							onCopy[onKey] = onValue
						}
					} else {
						onCopy[onKey] = onValue
					}
				}
				copy[k] = onCopy
			} else {
				copy[k] = v
			}
		} else {
			copy[k] = v
		}
	}
	return copy
}
