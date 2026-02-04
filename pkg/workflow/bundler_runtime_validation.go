// This file provides JavaScript runtime mode validation for agentic workflows.
//
// # Runtime Mode Validation
//
// This file validates that JavaScript scripts are compatible with their target runtime mode
// and that different runtime modes are not mixed in a bundling operation. This prevents
// runtime errors from incompatible API usage.
//
// # Runtime Modes
//
// GitHub Script Mode:
//   - Used for JavaScript embedded in GitHub Actions YAML via actions/github-script
//   - No module system available (no require() or module.exports at runtime)
//   - GitHub Actions globals available (core.*, exec.*, github.*)
//
// Node.js Mode:
//   - Used for standalone Node.js scripts that run on filesystem
//   - Full CommonJS module system available
//   - Standard Node.js APIs available (child_process, fs, etc.)
//   - No GitHub Actions globals
//
// # Validation Functions
//
//   - validateNoRuntimeMixing() - Ensures all files being bundled are compatible with target mode
//   - validateRuntimeModeRecursive() - Recursively validates runtime compatibility
//   - detectRuntimeMode() - Detects the intended runtime mode of a JavaScript file
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates runtime mode compatibility
//   - It checks for mixing of incompatible scripts
//   - It detects runtime-specific APIs
//
// For bundling functions, see bundler.go.
// For bundle safety validation, see bundler_safety_validation.go.
// For script content validation, see bundler_script_validation.go.
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var bundlerRuntimeLog = logger.New("workflow:bundler_runtime_validation")

// validateNoRuntimeMixing checks that all files being bundled are compatible with the target runtime mode
// This prevents mixing nodejs-only scripts (that use child_process) with github-script scripts
// Returns an error if incompatible runtime modes are detected
// Note: This function uses fail-fast error handling because runtime mode conflicts in dependencies
// need to be resolved one at a time, and showing multiple conflicting dependency chains would be confusing
func validateNoRuntimeMixing(mainScript string, sources map[string]string, targetMode RuntimeMode) error {
	bundlerRuntimeLog.Printf("Validating runtime mode compatibility: target_mode=%s", targetMode)

	// Track which files have been checked to avoid redundant checks
	checked := make(map[string]bool)

	// Recursively validate the main script and its dependencies
	// This uses fail-fast error handling because runtime conflicts need sequential resolution
	return validateRuntimeModeRecursive(mainScript, "", sources, targetMode, checked)
}

// validateRuntimeModeRecursive recursively validates that all required files are compatible with the target runtime mode
func validateRuntimeModeRecursive(content string, currentPath string, sources map[string]string, targetMode RuntimeMode, checked map[string]bool) error {
	// Extract all local require statements
	requireRegex := regexp.MustCompile(`require\(['"](\.\.?/[^'"]+)['"]\)`)
	matches := requireRegex.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) <= 1 {
			continue
		}

		requirePath := match[1]

		// Resolve the full path
		var fullPath string
		if currentPath == "" {
			fullPath = requirePath
		} else {
			fullPath = currentPath + "/" + requirePath
		}

		// Ensure .cjs extension
		if !strings.HasSuffix(fullPath, ".cjs") && !strings.HasSuffix(fullPath, ".js") {
			fullPath += ".cjs"
		}

		// Normalize the path
		fullPath = stringutil.NormalizePath(fullPath)

		// Skip if already checked
		if checked[fullPath] {
			continue
		}
		checked[fullPath] = true

		// Get the required file content
		requiredContent, ok := sources[fullPath]
		if !ok {
			// File not found - this will be caught by other validation
			continue
		}

		// Detect the runtime mode of the required file
		detectedMode := detectRuntimeMode(requiredContent)

		// Check for incompatibility
		if detectedMode != RuntimeModeGitHubScript && targetMode != detectedMode {
			return fmt.Errorf("runtime mode conflict: script requires '%s' which is a %s script, but the main script is compiled for %s mode.\n\nNode.js scripts cannot be bundled with GitHub Script mode scripts because they use incompatible APIs (e.g., child_process, fs).\n\nTo fix this:\n- Use only GitHub Script compatible scripts (core.*, exec.*, github.*) for GitHub Script mode\n- Or change the main script to Node.js mode if it needs Node.js APIs",
				fullPath, detectedMode, targetMode)
		}

		// Recursively check the required file's dependencies
		requiredDir := ""
		if strings.Contains(fullPath, "/") {
			parts := strings.Split(fullPath, "/")
			requiredDir = strings.Join(parts[:len(parts)-1], "/")
		}

		if err := validateRuntimeModeRecursive(requiredContent, requiredDir, sources, targetMode, checked); err != nil {
			return err
		}
	}

	return nil
}

// detectRuntimeMode attempts to detect the intended runtime mode of a JavaScript file
// by analyzing its content for runtime-specific patterns.
// This is used to detect if a LOCAL file being bundled is incompatible with the target mode.
func detectRuntimeMode(content string) RuntimeMode {
	// Check for Node.js-specific APIs that are CALLED in the code
	// These indicate the script uses Node.js-only functionality
	// Note: We only check for APIs that are fundamentally incompatible with github-script,
	// specifically child_process APIs like execSync/spawnSync
	nodeOnlyPatterns := []string{
		`\bexecSync\s*\(`,  // execSync function call
		`\bspawnSync\s*\(`, // spawnSync function call
	}

	for _, pattern := range nodeOnlyPatterns {
		matched, _ := regexp.MatchString(pattern, content)
		if matched {
			bundlerRuntimeLog.Printf("Detected Node.js mode: pattern '%s' found", pattern)
			return RuntimeModeNodeJS
		}
	}

	// Check for github-script specific APIs
	// These indicate the script is intended for GitHub Script mode
	githubScriptPatterns := []string{
		`\bcore\.\w+`,   // @actions/core
		`\bgithub\.\w+`, // github context
	}

	for _, pattern := range githubScriptPatterns {
		matched, _ := regexp.MatchString(pattern, content)
		if matched {
			bundlerRuntimeLog.Printf("Detected GitHub Script mode: pattern '%s' found", pattern)
			return RuntimeModeGitHubScript
		}
	}

	// If no specific patterns found, assume it's compatible with both (utility/helper functions)
	// and return GitHub Script mode as the default/most restrictive
	bundlerRuntimeLog.Print("No runtime-specific patterns found, assuming GitHub Script compatible")
	return RuntimeModeGitHubScript
}
