package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var generateActionMetadataLog = logger.New("cli:generate_action_metadata")

// jsdocDescriptionPattern matches a JSDoc block comment description line
var jsdocDescriptionPattern = regexp.MustCompile(`/\*\*\s*\n\s*\*\s*([^\n]+)`)

// coreGetInputPattern matches core.getInput('name') or core.getInput("name") calls
var coreGetInputPattern = regexp.MustCompile(`core\.getInput\(['"]([^'"]+)['"]\)`)

// coreSetOutputPattern matches core.setOutput('name', ...) or core.setOutput("name", ...) calls
var coreSetOutputPattern = regexp.MustCompile(`core\.setOutput\(['"]([^'"]+)['"]`)

// requireCJSPattern matches require('./filename.cjs') or require("./filename.cjs") calls
var requireCJSPattern = regexp.MustCompile(`require\(['"]\.\/([^'"]+\.cjs)['"]\)`)

// ActionMetadata represents metadata extracted from a JavaScript file
type ActionMetadata struct {
	Name         string
	Description  string
	Filename     string // e.g., "noop.cjs"
	ActionName   string // e.g., "noop"
	Inputs       []ActionInput
	Outputs      []ActionOutput
	Dependencies []string
}

// ActionInput represents an input parameter
type ActionInput struct {
	Name        string
	Description string
	Required    bool
	Default     string
}

// ActionOutput represents an output parameter
type ActionOutput struct {
	Name        string
	Description string
}

// GenerateActionMetadataCommand generates action.yml and README.md files for JavaScript modules
func GenerateActionMetadataCommand() error {
	jsDir := "pkg/workflow/js"
	actionsDir := "actions"

	generateActionMetadataLog.Print("Starting action metadata generation")

	// Select target JavaScript files (simple ones for proof of concept)
	targetFiles := []string{
		"noop.cjs",
		"minimize_comment.cjs",
		"close_issue.cjs",
		"close_pull_request.cjs",
		"close_discussion.cjs",
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("🔍 Generating actions for %d JavaScript modules...", len(targetFiles))))

	generatedCount := 0
	generateActionMetadataLog.Printf("Processing %d target JavaScript files in %s", len(targetFiles), jsDir)
	for _, filename := range targetFiles {
		jsPath := filepath.Join(jsDir, filename)
		generateActionMetadataLog.Printf("Processing file: %s", filename)

		// Read file content directly from filesystem
		contentBytes, err := os.ReadFile(jsPath)
		if err != nil {
			generateActionMetadataLog.Printf("Skipping %s: failed to read file: %v", filename, err)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("⚠ Skipping %s: %s", filename, err)))
			continue
		}
		content := string(contentBytes)

		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("\n📦 Processing: "+filename))

		// Extract metadata
		metadata, err := extractActionMetadata(filename, content)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("✗ Failed to extract metadata from %s: %s", filename, err)))
			continue
		}

		// Create action directory
		actionDir := filepath.Join(actionsDir, metadata.ActionName)
		if err := os.MkdirAll(actionDir, constants.DirPermPublic); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", actionDir, err)
		}

		// Create src directory
		srcDir := filepath.Join(actionDir, "src")
		if err := os.MkdirAll(srcDir, constants.DirPermPublic); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", srcDir, err)
		}

		// Generate action.yml
		if err := generateActionYml(actionDir, metadata); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage("✗ Failed to generate action.yml: "+err.Error()))
			continue
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  ✓ Generated action.yml"))

		// Generate README.md
		if err := generateReadme(actionDir, metadata); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage("✗ Failed to generate README.md: "+err.Error()))
			continue
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  ✓ Generated README.md"))

		// Copy source file with owner-only read/write permissions (0600) for security best practices
		srcPath := filepath.Join(srcDir, "index.js")
		if err := os.WriteFile(srcPath, []byte(content), constants.FilePermSensitive); err != nil {
			return fmt.Errorf("failed to write source file: %w", err)
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  ✓ Copied source to src/index.js"))

		generatedCount++
	}

	generateActionMetadataLog.Printf("Action metadata generation complete: generated=%d, total=%d", generatedCount, len(targetFiles))
	if generatedCount == 0 {
		return errors.New("no actions were generated")
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("\n✨ Successfully generated %d action(s)", generatedCount)))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("\nNext steps:"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  1. Review the generated action.yml and README.md files"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  2. Update dependency mapping in pkg/cli/actions_build_command.go"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  3. Run 'make actions-build' to build the actions"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  4. Test the actions in a workflow"))

	return nil
}

// extractActionMetadata extracts metadata from a JavaScript file
func extractActionMetadata(filename, content string) (*ActionMetadata, error) {
	generateActionMetadataLog.Printf("Extracting metadata from %s", filename)

	// Extract action name from filename (e.g., "noop.cjs" -> "noop")
	actionName := strings.TrimSuffix(filename, ".cjs")

	// Extract description from JSDoc comment
	description := extractDescription(content)
	if description == "" {
		description = fmt.Sprintf("Process %s safe output", actionName)
	}

	// Generate human-readable name from action name
	name := generateHumanReadableName(actionName)

	// Extract inputs
	inputs := extractInputs(content)

	// Extract outputs
	outputs := extractOutputs(content)

	// Extract dependencies
	dependencies := extractDependencies(content)

	metadata := &ActionMetadata{
		Name:         name,
		Description:  description,
		Filename:     filename,
		ActionName:   actionName,
		Inputs:       inputs,
		Outputs:      outputs,
		Dependencies: dependencies,
	}

	generateActionMetadataLog.Printf("Extracted metadata: %d inputs, %d outputs, %d dependencies",
		len(inputs), len(outputs), len(dependencies))

	return metadata, nil
}

// extractDescription extracts description from JSDoc comment
func extractDescription(content string) string {
	// Look for JSDoc block comment at the start of main() or file
	matches := jsdocDescriptionPattern.FindStringSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// generateHumanReadableName converts action name to human-readable format
func generateHumanReadableName(actionName string) string {
	// Replace underscores with spaces and capitalize words
	words := strings.Split(actionName, "_")
	for i, word := range words {
		if word != "" {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// extractInputs extracts input parameters from core.getInput() calls
func extractInputs(content string) []ActionInput {
	var inputs []ActionInput
	seen := make(map[string]struct {
	})

	// Match core.getInput('name') or core.getInput("name")
	matches := coreGetInputPattern.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) > 1 {
			inputName := match[1]
			if !setutil.Contains(seen, inputName) {
				inputs = append(inputs, ActionInput{
					Name:        inputName,
					Description: "Input parameter: " + inputName,
					Required:    false,
					Default:     "",
				})
				seen[inputName] = struct {
				}{}
			}
		}
	}

	// Sort inputs by name for consistency
	slices.SortFunc(inputs, func(a, b ActionInput) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	return inputs
}

// extractOutputs extracts output parameters from core.setOutput() calls
func extractOutputs(content string) []ActionOutput {
	var outputs []ActionOutput
	seen := make(map[string]struct {
	})

	// Match core.setOutput('name', ...) or core.setOutput("name", ...)
	matches := coreSetOutputPattern.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) > 1 {
			outputName := match[1]
			if !setutil.Contains(seen, outputName) {
				outputs = append(outputs, ActionOutput{
					Name:        outputName,
					Description: "Output parameter: " + outputName,
				})
				seen[outputName] = struct {
				}{}
			}
		}
	}

	// Sort outputs by name for consistency
	slices.SortFunc(outputs, func(a, b ActionOutput) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	return outputs
}

// extractDependencies extracts require() dependencies
func extractDependencies(content string) []string {
	var deps []string
	seen := make(map[string]struct {
	})

	// Match require('./filename.cjs') or require("./filename.cjs")
	matches := requireCJSPattern.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) > 1 {
			dep := match[1]
			if !setutil.Contains(seen, dep) {
				deps = append(deps, dep)
				seen[dep] = struct {
				}{}
			}
		}
	}

	// Sort dependencies for consistency
	sort.Strings(deps)

	return deps
}

// generateActionYml generates an action.yml file
func generateActionYml(actionDir string, metadata *ActionMetadata) error {
	generateActionMetadataLog.Printf("Generating action.yml: action=%s, inputs=%d, outputs=%d", metadata.ActionName, len(metadata.Inputs), len(metadata.Outputs))
	var content strings.Builder

	fmt.Fprintf(&content, "name: '%s'\n", metadata.Name)
	fmt.Fprintf(&content, "description: '%s'\n", metadata.Description)
	content.WriteString("author: 'GitHub Next'\n\n")

	// Add inputs
	if len(metadata.Inputs) > 0 {
		content.WriteString("inputs:\n")
		for _, input := range metadata.Inputs {
			fmt.Fprintf(&content, "  %s:\n", input.Name)
			fmt.Fprintf(&content, "    description: '%s'\n", input.Description)
			fmt.Fprintf(&content, "    required: %t\n", input.Required)
			if input.Default != "" {
				fmt.Fprintf(&content, "    default: '%s'\n", input.Default)
			}
		}
		content.WriteString("\n")
	}

	// Add outputs
	if len(metadata.Outputs) > 0 {
		content.WriteString("outputs:\n")
		for _, output := range metadata.Outputs {
			fmt.Fprintf(&content, "  %s:\n", output.Name)
			fmt.Fprintf(&content, "    description: '%s'\n", output.Description)
		}
		content.WriteString("\n")
	}

	// Add runs section
	content.WriteString("runs:\n")
	content.WriteString("  using: 'node24'\n")
	content.WriteString("  main: 'index.js'\n\n")

	// Add branding
	content.WriteString("branding:\n")
	content.WriteString("  icon: 'package'\n")
	content.WriteString("  color: 'blue'\n")

	// Write to file with owner-only read/write permissions (0600) for security best practices
	ymlPath := filepath.Join(actionDir, "action.yml")
	if err := os.WriteFile(ymlPath, []byte(content.String()), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write action.yml: %w", err)
	}

	return nil
}

// generateReadme generates a README.md file
func generateReadme(actionDir string, metadata *ActionMetadata) error {
	generateActionMetadataLog.Printf("Generating README.md: action=%s, deps=%d", metadata.ActionName, len(metadata.Dependencies))
	var content strings.Builder

	fmt.Fprintf(&content, "# %s\n\n", metadata.Name)
	fmt.Fprintf(&content, "%s\n\n", metadata.Description)

	content.WriteString("## Overview\n\n")
	fmt.Fprintf(&content, "This action is generated from `pkg/workflow/js/%s` and provides functionality ", metadata.Filename)
	content.WriteString("for GitHub Agentic Workflows.\n\n")

	// Usage section
	content.WriteString("## Usage\n\n")
	content.WriteString("```yaml\n")
	fmt.Fprintf(&content, "- uses: ./actions/%s\n", metadata.ActionName)
	if len(metadata.Inputs) > 0 {
		content.WriteString("  with:\n")
		for _, input := range metadata.Inputs {
			fmt.Fprintf(&content, "    %s: 'value'  # %s\n", input.Name, input.Description)
		}
	}
	content.WriteString("```\n\n")

	// Inputs section
	if len(metadata.Inputs) > 0 {
		content.WriteString("## Inputs\n\n")
		for _, input := range metadata.Inputs {
			fmt.Fprintf(&content, "### `%s`\n\n", input.Name)
			fmt.Fprintf(&content, "**Description**: %s\n\n", input.Description)
			fmt.Fprintf(&content, "**Required**: %t\n\n", input.Required)
			if input.Default != "" {
				fmt.Fprintf(&content, "**Default**: `%s`\n\n", input.Default)
			}
		}
	}

	// Outputs section
	if len(metadata.Outputs) > 0 {
		content.WriteString("## Outputs\n\n")
		for _, output := range metadata.Outputs {
			fmt.Fprintf(&content, "### `%s`\n\n", output.Name)
			fmt.Fprintf(&content, "**Description**: %s\n\n", output.Description)
		}
	}

	// Dependencies section
	if len(metadata.Dependencies) > 0 {
		content.WriteString("## Dependencies\n\n")
		content.WriteString("This action depends on the following JavaScript modules:\n\n")
		for _, dep := range metadata.Dependencies {
			fmt.Fprintf(&content, "- `%s`\n", dep)
		}
		content.WriteString("\n")
	}

	// Development section
	content.WriteString("## Development\n\n")
	content.WriteString("### Building\n\n")
	content.WriteString("To build this action, you need to:\n\n")
	fmt.Fprintf(&content, "1. Update the dependency mapping in `pkg/cli/actions_build_command.go` for `%s`\n", metadata.ActionName)
	content.WriteString("2. Run `make actions-build` to bundle the JavaScript dependencies\n")
	content.WriteString("3. The bundled `index.js` will be generated and committed\n\n")

	content.WriteString("### Testing\n\n")
	content.WriteString("Test this action by creating a workflow:\n\n")
	content.WriteString("```yaml\n")
	content.WriteString("jobs:\n")
	content.WriteString("  test:\n")
	content.WriteString("    runs-on: ubuntu-latest\n")
	content.WriteString("    steps:\n")
	fmt.Fprintf(&content, "      - uses: ./actions/%s\n", metadata.ActionName)
	content.WriteString("```\n\n")

	// License
	content.WriteString("## License\n\n")
	content.WriteString("MIT\n")

	// Write to file with owner-only read/write permissions (0600) for security best practices
	readmePath := filepath.Join(actionDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(content.String()), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write README.md: %w", err)
	}

	return nil
}
