package parser

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var workflowUpdateLog = logger.New("parser:workflow_update")

// UpdateWorkflowFrontmatter updates the frontmatter of a workflow file using a callback function
func UpdateWorkflowFrontmatter(workflowPath string, updateFunc func(frontmatter map[string]any) error, verbose bool) error {
	workflowUpdateLog.Printf("Updating workflow frontmatter: path=%s", workflowPath)
	// Read the workflow file
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("failed to read workflow file: %w", err)
	}

	// Parse frontmatter using existing helper
	result, err := ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	// Ensure frontmatter map exists
	if result.Frontmatter == nil {
		result.Frontmatter = make(map[string]any)
	}

	// Apply the update function
	if err := updateFunc(result.Frontmatter); err != nil {
		return err
	}

	// Convert back to YAML
	updatedFrontmatter, err := yaml.Marshal(result.Frontmatter)
	if err != nil {
		return fmt.Errorf("failed to marshal updated frontmatter: %w", err)
	}

	// Reconstruct the file content
	updatedContent, err := reconstructWorkflowFile(string(updatedFrontmatter), result.Markdown)
	if err != nil {
		return fmt.Errorf("failed to reconstruct workflow file: %w", err)
	}

	// Write the updated content back to the file
	if err := os.WriteFile(workflowPath, []byte(updatedContent), 0644); err != nil {
		return fmt.Errorf("failed to write updated workflow file: %w", err)
	}

	workflowUpdateLog.Printf("Successfully updated workflow file: %s", workflowPath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Updated workflow file: %s", console.ToRelativePath(workflowPath))))
	}

	return nil
}

// EnsureToolsSection ensures the tools section exists in frontmatter and returns it
func EnsureToolsSection(frontmatter map[string]any) map[string]any {
	workflowUpdateLog.Print("Ensuring tools section exists in frontmatter")
	if frontmatter["tools"] == nil {
		frontmatter["tools"] = make(map[string]any)
		workflowUpdateLog.Print("Created new tools section")
	}

	tools, ok := frontmatter["tools"].(map[string]any)
	if !ok {
		// If tools exists but is not a map, replace it
		tools = make(map[string]any)
		frontmatter["tools"] = tools
	}

	return tools
}

// reconstructWorkflowFile reconstructs a complete workflow file from frontmatter YAML and markdown content
func reconstructWorkflowFile(frontmatterYAML, markdownContent string) (string, error) {
	var lines []string

	// Add opening frontmatter delimiter
	lines = append(lines, "---")

	// Add frontmatter content (trim trailing newline from YAML marshal)
	frontmatterStr := strings.TrimSuffix(frontmatterYAML, "\n")
	if frontmatterStr != "" {
		lines = append(lines, strings.Split(frontmatterStr, "\n")...)
	}

	// Add closing frontmatter delimiter
	lines = append(lines, "---")

	// Add markdown content if present
	if markdownContent != "" {
		lines = append(lines, markdownContent)
	}

	return strings.Join(lines, "\n"), nil
}

// QuoteCronExpressions ensures cron expressions in schedule sections are properly quoted.
// The YAML library may drop quotes from cron expressions like "0 14 * * 1-5" which
// causes validation errors since they start with numbers but contain spaces and special chars.
func QuoteCronExpressions(yamlContent string) string {
	workflowUpdateLog.Print("Quoting cron expressions in YAML content")
	// Pattern to match unquoted cron expressions after "cron:"
	// Matches: cron: 0 14 * * 1-5
	// Captures the cron value to be quoted
	cronPattern := regexp.MustCompile(`(?m)^(\s*-?\s*cron:\s*)([0-9][^\n"']*)$`)

	// Replace unquoted cron expressions with quoted versions
	return cronPattern.ReplaceAllStringFunc(yamlContent, func(match string) string {
		// Extract the cron prefix and value
		submatches := cronPattern.FindStringSubmatch(match)
		if len(submatches) < 3 {
			return match
		}
		prefix := submatches[1]
		cronValue := strings.TrimSpace(submatches[2])

		// Remove any trailing comments
		if idx := strings.Index(cronValue, "#"); idx != -1 {
			comment := cronValue[idx:]
			cronValue = strings.TrimSpace(cronValue[:idx])
			return prefix + `"` + cronValue + `" ` + comment
		}

		return prefix + `"` + cronValue + `"`
	})
}
