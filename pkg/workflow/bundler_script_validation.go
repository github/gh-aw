// This file provides JavaScript script content validation for agentic workflows.
//
// # Script Content Validation
//
// This file validates JavaScript script content to ensure compatibility with runtime modes
// and adherence to platform conventions. Validation enforces proper API usage patterns
// for GitHub Script mode vs Node.js mode.
//
// # Validation Functions
//
//   - validateNoExecSync() - Ensures GitHub Script mode scripts use exec instead of execSync
//   - validateNoGitHubScriptGlobals() - Ensures Node.js scripts don't use GitHub Actions globals
//
// # Design Rationale
//
// The script content validation enforces two key constraints:
//  1. GitHub Script mode: Should not use execSync (use async exec from @actions/exec instead)
//  2. Node.js mode: Should not use GitHub Actions globals (core.*, exec.*, github.*)
//
// These rules ensure that scripts follow platform conventions:
//   - GitHub Script mode runs inline in GitHub Actions YAML with GitHub-specific globals available
//   - Node.js mode runs as standalone scripts with standard Node.js APIs only
//
// Validation happens at registration time (via panic) to catch errors during development/testing
// rather than at runtime.
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates JavaScript code content based on runtime mode
//   - It checks for API usage patterns (execSync, GitHub Actions globals)
//   - It validates script content for compatibility with execution environment
//
// For bundling functions, see bundler.go.
// For bundle safety validation, see bundler_safety_validation.go.
// For runtime mode validation, see bundler_runtime_validation.go.
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var bundlerScriptLog = logger.New("workflow:bundler_script_validation")

// validateNoExecSync checks that GitHub Script mode scripts do not use execSync
// GitHub Script mode should use exec instead for better async/await handling
// Returns an error if execSync is found, otherwise returns nil
func validateNoExecSync(scriptName string, content string, mode RuntimeMode) error {
	// Only validate GitHub Script mode
	if mode != RuntimeModeGitHubScript {
		return nil
	}

	bundlerScriptLog.Printf("Validating no execSync in GitHub Script: %s (%d bytes)", scriptName, len(content))

	// Regular expression to match execSync usage
	// Matches: execSync(...) with various patterns
	execSyncRegex := regexp.MustCompile(`\bexecSync\s*\(`)

	lines := strings.Split(content, "\n")
	var foundUsages []string

	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comment lines
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		// Check for execSync usage
		if execSyncRegex.MatchString(line) {
			foundUsages = append(foundUsages, fmt.Sprintf("line %d: %s", lineNum+1, strings.TrimSpace(line)))
		}
	}

	if len(foundUsages) > 0 {
		bundlerScriptLog.Printf("Validation failed: found %d execSync usage(s) in %s", len(foundUsages), scriptName)
		return fmt.Errorf("GitHub Script mode script '%s' contains %d execSync usage(s):\n  %s\n\nGitHub Script mode should use exec instead of execSync for better async/await handling",
			scriptName, len(foundUsages), strings.Join(foundUsages, "\n  "))
	}

	bundlerScriptLog.Printf("Validation successful: no execSync usage found in %s", scriptName)
	return nil
}

// validateNoGitHubScriptGlobals checks that Node.js mode scripts do not use GitHub Actions globals
// Node.js scripts should not rely on actions/github-script globals like core.*, exec.*, or github.*
// Returns an error if GitHub Actions globals are found, otherwise returns nil
func validateNoGitHubScriptGlobals(scriptName string, content string, mode RuntimeMode) error {
	// Only validate Node.js mode
	if mode != RuntimeModeNodeJS {
		return nil
	}

	bundlerScriptLog.Printf("Validating no GitHub Actions globals in Node.js script: %s (%d bytes)", scriptName, len(content))

	// Regular expressions to match GitHub Actions globals
	// Matches: core.method, exec.method, github.property
	coreGlobalRegex := regexp.MustCompile(`\bcore\.\w+`)
	execGlobalRegex := regexp.MustCompile(`\bexec\.\w+`)
	githubGlobalRegex := regexp.MustCompile(`\bgithub\.\w+`)

	lines := strings.Split(content, "\n")
	var foundUsages []string

	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comment lines and type references
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		if strings.Contains(trimmed, "/// <reference") {
			continue
		}

		// Check for core.* usage
		if coreGlobalRegex.MatchString(line) {
			foundUsages = append(foundUsages, fmt.Sprintf("line %d: core.* usage: %s", lineNum+1, strings.TrimSpace(line)))
		}

		// Check for exec.* usage
		if execGlobalRegex.MatchString(line) {
			foundUsages = append(foundUsages, fmt.Sprintf("line %d: exec.* usage: %s", lineNum+1, strings.TrimSpace(line)))
		}

		// Check for github.* usage
		if githubGlobalRegex.MatchString(line) {
			foundUsages = append(foundUsages, fmt.Sprintf("line %d: github.* usage: %s", lineNum+1, strings.TrimSpace(line)))
		}
	}

	if len(foundUsages) > 0 {
		bundlerScriptLog.Printf("Validation failed: found %d GitHub Actions global usage(s) in %s", len(foundUsages), scriptName)
		return fmt.Errorf("node.js mode script '%s' contains %d GitHub Actions global usage(s):\n  %s\n\nNode.js scripts should not use GitHub Actions globals (core.*, exec.*, github.*)",
			scriptName, len(foundUsages), strings.Join(foundUsages, "\n  "))
	}

	bundlerScriptLog.Printf("Validation successful: no GitHub Actions globals found in %s", scriptName)
	return nil
}
