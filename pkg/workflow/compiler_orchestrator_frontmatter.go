package workflow

import (
	"fmt"
	"os"
	"path/filepath"

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
}

// parseFrontmatterSection reads the workflow file and parses its frontmatter.
// It returns a frontmatterParseResult containing the parsed data and validation information.
// If the workflow is detected as a shared workflow (no 'on' field), isSharedWorkflow is set to true.
func (c *Compiler) parseFrontmatterSection(markdownPath string) (*frontmatterParseResult, error) {
	orchestratorFrontmatterLog.Printf("Starting frontmatter parsing: %s", markdownPath)
	log.Printf("Reading file: %s", markdownPath)

	// Clean the path to prevent path traversal issues (gosec G304)
	// filepath.Clean removes ".." and other problematic path elements
	cleanPath := filepath.Clean(markdownPath)

	// Read the file
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		orchestratorFrontmatterLog.Printf("Failed to read file: %s, error: %v", cleanPath, err)
		// Don't wrap os.PathError - format it instead to avoid exposing internals
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	log.Printf("File size: %d bytes", len(content))

	// Parse frontmatter and markdown
	orchestratorFrontmatterLog.Printf("Parsing frontmatter from file: %s", cleanPath)
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		orchestratorFrontmatterLog.Printf("Frontmatter extraction failed: %v", err)
		// Use FrontmatterStart from result if available, otherwise default to line 2 (after opening ---)
		frontmatterStart := 2
		if result != nil && result.FrontmatterStart > 0 {
			frontmatterStart = result.FrontmatterStart
		}
		return nil, c.createFrontmatterError(cleanPath, string(content), err, frontmatterStart)
	}

	if len(result.Frontmatter) == 0 {
		orchestratorFrontmatterLog.Print("No frontmatter found in file")
		return nil, fmt.Errorf("no frontmatter found")
	}

	// Preprocess schedule fields to convert human-friendly format to cron expressions
	if err := c.preprocessScheduleFields(result.Frontmatter, cleanPath, string(content)); err != nil {
		orchestratorFrontmatterLog.Printf("Schedule preprocessing failed: %v", err)
		return nil, err
	}

	// Create a copy of frontmatter without internal markers for schema validation
	// Keep the original frontmatter with markers for YAML generation
	frontmatterForValidation := c.copyFrontmatterWithoutInternalMarkers(result.Frontmatter)

	// Check if "on" field is missing - if so, treat as a shared/imported workflow
	_, hasOnField := frontmatterForValidation["on"]
	if !hasOnField {
		detectionLog.Printf("No 'on' field detected - treating as shared agentic workflow")

		// Validate as an included/shared workflow (uses main_workflow_schema with forbidden field checks)
		if err := parser.ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatterForValidation, cleanPath); err != nil {
			orchestratorFrontmatterLog.Printf("Shared workflow validation failed: %v", err)
			return nil, err
		}

		return &frontmatterParseResult{
			cleanPath:                cleanPath,
			content:                  content,
			frontmatterResult:        result,
			frontmatterForValidation: frontmatterForValidation,
			markdownDir:              filepath.Dir(cleanPath),
			isSharedWorkflow:         true,
		}, nil
	}

	// For main workflows (with 'on' field), markdown content is required
	if result.Markdown == "" {
		orchestratorFrontmatterLog.Print("No markdown content found for main workflow")
		return nil, fmt.Errorf("no markdown content found")
	}

	// Validate main workflow frontmatter contains only expected entries
	orchestratorFrontmatterLog.Printf("Validating main workflow frontmatter schema")
	if err := parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatterForValidation, cleanPath); err != nil {
		orchestratorFrontmatterLog.Printf("Main workflow frontmatter validation failed: %v", err)
		return nil, err
	}

	// Validate event filter mutual exclusivity (branches/branches-ignore, paths/paths-ignore)
	if err := ValidateEventFilters(frontmatterForValidation); err != nil {
		orchestratorFrontmatterLog.Printf("Event filter validation failed: %v", err)
		return nil, err
	}

	// Validate that @include/@import directives are not used inside template regions
	if err := validateNoIncludesInTemplateRegions(result.Markdown); err != nil {
		orchestratorFrontmatterLog.Printf("Template region validation failed: %v", err)
		return nil, fmt.Errorf("template region validation failed: %w", err)
	}

	log.Printf("Frontmatter: %d chars, Markdown: %d chars", len(result.Frontmatter), len(result.Markdown))

	return &frontmatterParseResult{
		cleanPath:                cleanPath,
		content:                  content,
		frontmatterResult:        result,
		frontmatterForValidation: frontmatterForValidation,
		markdownDir:              filepath.Dir(cleanPath),
		isSharedWorkflow:         false,
	}, nil
}

// copyFrontmatterWithoutInternalMarkers creates a deep copy of frontmatter without internal marker fields
// This is used for schema validation while preserving markers in the original for YAML generation
func (c *Compiler) copyFrontmatterWithoutInternalMarkers(frontmatter map[string]any) map[string]any {
	// Create a shallow copy of the top level
	copy := make(map[string]any)
	for k, v := range frontmatter {
		if k == "on" {
			// Special handling for "on" field - need to deep copy and remove markers
			if onMap, ok := v.(map[string]any); ok {
				onCopy := make(map[string]any)
				for onKey, onValue := range onMap {
					if onKey == "issues" || onKey == "pull_request" || onKey == "discussion" {
						// Deep copy the section and remove marker
						if sectionMap, ok := onValue.(map[string]any); ok {
							sectionCopy := make(map[string]any)
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
