package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var actionsBuildLog = logger.New("cli:actions_build")

// ActionsBuildCommand builds all custom GitHub Actions by bundling JavaScript dependencies
func ActionsBuildCommand() error {
	actionsDir := "actions"

	actionsBuildLog.Print("Starting actions build")

	// Get list of action directories
	actionDirs, err := getActionDirectories(actionsDir)
	if err != nil {
		return err
	}

	if len(actionDirs) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No action directories found in actions/"))
		return nil
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Building all actions..."))

	// Build each action
	for _, actionName := range actionDirs {
		if err := buildAction(actionsDir, actionName); err != nil {
			return fmt.Errorf("failed to build action %s: %w", actionName, err)
		}
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✨ All actions built successfully (%d actions)", len(actionDirs))))
	return nil
}

// ActionsValidateCommand validates all action.yml files
func ActionsValidateCommand() error {
	actionsDir := "actions"

	actionsBuildLog.Print("Starting actions validation")

	// Get list of action directories
	actionDirs, err := getActionDirectories(actionsDir)
	if err != nil {
		return err
	}

	if len(actionDirs) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No action directories found in actions/"))
		return nil
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("✅ Validating all actions"))

	allValid := true
	for _, actionName := range actionDirs {
		actionPath := filepath.Join(actionsDir, actionName)
		if err := validateActionYml(actionPath); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("✗ %s/action.yml: %s", actionName, err.Error())))
			allValid = false
		} else {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  ✓ %s/action.yml is valid", actionName)))
		}
	}

	if !allValid {
		return fmt.Errorf("validation failed for one or more actions")
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✨ All actions valid"))
	return nil
}

// ActionsCleanCommand removes generated index.js files from all actions
func ActionsCleanCommand() error {
	actionsDir := "actions"

	actionsBuildLog.Print("Starting actions cleanup")

	// Get list of action directories
	actionDirs, err := getActionDirectories(actionsDir)
	if err != nil {
		return err
	}

	if len(actionDirs) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No action directories found in actions/"))
		return nil
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("🧹 Cleaning generated action files"))

	cleanedCount := 0
	for _, actionName := range actionDirs {
		// Clean index.js for actions that use it (except setup)
		if actionName != "setup" {
			indexPath := filepath.Join(actionsDir, actionName, "index.js")
			if _, err := os.Stat(indexPath); err == nil {
				if err := os.Remove(indexPath); err != nil {
					return fmt.Errorf("failed to remove %s: %w", indexPath, err)
				}
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  ✓ Removed %s/index.js", actionName)))
				cleanedCount++
			}
		}

		// For setup action, both js/ and sh/ directories are source of truth (NOT generated)
		// Do not clean them
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✨ Cleanup complete (%d files removed)", cleanedCount)))
	return nil
}

// getActionDirectories returns a sorted list of action directory names
func getActionDirectories(actionsDir string) ([]string, error) {
	if _, err := os.Stat(actionsDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("actions/ directory does not exist")
	}

	entries, err := os.ReadDir(actionsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read actions directory: %w", err)
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}

	sort.Strings(dirs)
	return dirs, nil
}

// validateActionYml validates that an action.yml file exists and contains required fields.
//
// This validation function is co-located with the actions build command because:
//   - It's specific to GitHub Actions custom action structure
//   - It's only called during the actions build process
//   - It validates action metadata before bundling JavaScript
//
// The function validates:
//   - action.yml file exists in the action directory
//   - Required fields are present (name, description, runs)
//   - Basic action metadata structure is valid
//
// This follows the principle that domain-specific validation belongs in domain files.
func validateActionYml(actionPath string) error {
	ymlPath := filepath.Join(actionPath, "action.yml")

	if _, err := os.Stat(ymlPath); os.IsNotExist(err) {
		return fmt.Errorf("action.yml not found")
	}

	content, err := os.ReadFile(ymlPath)
	if err != nil {
		return fmt.Errorf("failed to read action.yml: %w", err)
	}

	contentStr := string(content)

	// Check required fields
	requiredFields := []string{"name:", "description:", "runs:"}
	for _, field := range requiredFields {
		if !strings.Contains(contentStr, field) {
			return fmt.Errorf("missing required field '%s'", strings.TrimSuffix(field, ":"))
		}
	}

	// Check that it's either a node20 or composite action
	isNode20 := strings.Contains(contentStr, "using: 'node20'") || strings.Contains(contentStr, "using: \"node20\"")
	isComposite := strings.Contains(contentStr, "using: 'composite'") || strings.Contains(contentStr, "using: \"composite\"")

	if !isNode20 && !isComposite {
		return fmt.Errorf("action must use either 'node20' or 'composite' runtime")
	}

	return nil
}

// buildAction builds a single action by bundling its dependencies
func buildAction(actionsDir, actionName string) error {
	actionsBuildLog.Printf("Building action: %s", actionName)

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("\n📦 Building action: %s", actionName)))

	actionPath := filepath.Join(actionsDir, actionName)

	// Validate action.yml
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  ✓ Validating action.yml"))
	if err := validateActionYml(actionPath); err != nil {
		return err
	}

	// Special handling for setup: build shell script with embedded files
	if actionName == "setup" {
		return buildSetupAction(actionsDir, actionName)
	}

	// Check if this is a composite action (doesn't need JavaScript bundling)
	isComposite, err := isCompositeAction(actionPath)
	if err != nil {
		return fmt.Errorf("failed to check action type: %w", err)
	}

	if isComposite {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  ✓ Composite action - no JavaScript bundling needed"))
		return nil
	}

	srcPath := filepath.Join(actionPath, "src", "index.js")
	outputPath := filepath.Join(actionPath, "index.js")

	// Check if source file exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("source file not found: %s", srcPath)
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("  ✓ Reading source file"))
	sourceContent, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}

	// Get dependencies for this action
	dependencies := getActionDependencies(actionName)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  ✓ Found %d dependencies", len(dependencies))))

	// Get all JavaScript sources
	sources := workflow.GetJavaScriptSources()

	// Read dependency files
	files := make(map[string]string)
	for _, dep := range dependencies {
		if content, ok := sources[dep]; ok {
			files[dep] = content
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("    - %s", dep)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("    ⚠ Warning: Could not find %s", dep)))
		}
	}

	// Generate FILES object with embedded content
	filesJSON, err := json.MarshalIndent(files, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal files: %w", err)
	}

	// Indent the JSON for proper embedding
	indentedJSON := strings.ReplaceAll(string(filesJSON), "\n", "\n  ")
	indentedJSON = "  " + strings.TrimPrefix(indentedJSON, " ")

	// Replace the FILES placeholder in source
	// Match: const FILES = { ... };
	filesRegex := regexp.MustCompile(`(?s)const FILES = \{[^}]*\};`)
	outputContent := filesRegex.ReplaceAllString(string(sourceContent), fmt.Sprintf("const FILES = %s;", strings.TrimSpace(indentedJSON)))

	// Write output file with restrictive permissions (0600 for security)
	if err := os.WriteFile(outputPath, []byte(outputContent), 0600); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  ✓ Built %s", outputPath)))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  ✓ Embedded %d files", len(files))))

	return nil
}

// isCompositeAction checks if an action uses the 'composite' runtime
func isCompositeAction(actionPath string) (bool, error) {
	ymlPath := filepath.Join(actionPath, "action.yml")
	content, err := os.ReadFile(ymlPath)
	if err != nil {
		return false, fmt.Errorf("failed to read action.yml: %w", err)
	}

	contentStr := string(content)
	return strings.Contains(contentStr, "using: 'composite'") || strings.Contains(contentStr, "using: \"composite\""), nil
}

// buildSetupAction builds the setup action by checking that source files exist.
// Note: Both JavaScript and shell scripts are source of truth in actions/setup/js/ and actions/setup/sh/
// These files are manually edited and committed to git. They are NOT synced to pkg/workflow/
// At runtime, setup.sh copies these files to /tmp/gh-aw/actions for workflow execution.
func buildSetupAction(actionsDir, actionName string) error {
	actionPath := filepath.Join(actionsDir, actionName)
	jsDir := filepath.Join(actionPath, "js")
	shDir := filepath.Join(actionPath, "sh")

	// JavaScript files in actions/setup/js/ are the source of truth
	if _, err := os.Stat(jsDir); err == nil {
		// Count JavaScript files
		entries, err := os.ReadDir(jsDir)
		if err == nil {
			jsCount := 0
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cjs") {
					jsCount++
				}
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  ✓ JavaScript files in js/ (source of truth): %d", jsCount)))
		}
	}

	// Shell scripts in actions/setup/sh/ are the source of truth
	if _, err := os.Stat(shDir); err == nil {
		// Count shell scripts
		entries, err := os.ReadDir(shDir)
		if err == nil {
			shCount := 0
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sh") {
					shCount++
				}
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  ✓ Shell scripts in sh/ (source of truth): %d", shCount)))
		}
	}

	return nil
}

// getActionDependencies returns the list of JavaScript dependencies for an action
// This mapping defines which files from pkg/workflow/js/ are needed for each action
func getActionDependencies(actionName string) []string {
	// For setup, use the dynamic script discovery
	// This ensures all .cjs files are included automatically
	if actionName == "setup" {
		return workflow.GetAllScriptFilenames()
	}

	return []string{}
}
