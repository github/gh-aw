// This file contains MCP (Model Context Protocol) validation functions.
// This file consolidates validation logic for MCP server configurations.
// Binary path utilities (GetBinaryPath, logAndValidateBinaryPath) live in mcp_helpers.go.

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var mcpValidationLog = logger.New("cli:mcp_validation")

// validateServerSecrets checks if required environment variables/secrets are available
func validateServerSecrets(config parser.RegistryMCPServerConfig, verbose bool, useActionsSecrets bool) error {
	mcpValidationLog.Printf("Validating server secrets: server=%s, type=%s, useActionsSecrets=%v", config.Name, config.Type, useActionsSecrets)

	// Extract secrets from the config
	requiredSecrets := extractSecretsFromConfig(config)

	// Special case: Check for GH_AW_GITHUB_TOKEN when GitHub tool is in remote mode
	if config.Name == "github" && config.Type == "http" {
		mcpValidationLog.Print("GitHub remote mode detected, checking for GH_AW_GITHUB_TOKEN")
		// GitHub remote mode requires GH_AW_GITHUB_TOKEN secret
		// Check if a custom token is already specified in the env
		hasCustomToken := false
		for _, value := range config.Env {
			if strings.Contains(value, "secrets.") && !strings.Contains(value, "GH_AW_GITHUB_TOKEN") {
				// Custom token specified, no need to check GH_AW_GITHUB_TOKEN
				hasCustomToken = true
				break
			}
		}

		if !hasCustomToken {
			// Add GH_AW_GITHUB_TOKEN to required secrets if not already present
			alreadyPresent := false
			for _, secret := range requiredSecrets {
				if secret.Name == "GH_AW_GITHUB_TOKEN" {
					alreadyPresent = true
					break
				}
			}
			if !alreadyPresent {
				requiredSecrets = append(requiredSecrets, SecretInfo{
					Name:   "GH_AW_GITHUB_TOKEN",
					EnvKey: "GITHUB_TOKEN",
				})
			}
		}
	}

	if len(requiredSecrets) == 0 {
		mcpValidationLog.Printf("No required secrets found, validating %d environment variables", len(config.Env))
		// No secrets required, proceed with normal env var validation
		for key, value := range config.Env {
			// Check if value contains variable references
			if strings.Contains(value, "${") {
				// Extract variable name (simplified parsing)
				if strings.Contains(value, "secrets.") {
					// This should have been caught by extractSecretsFromConfig
					continue
				}
				if strings.Contains(value, "GH_TOKEN") || strings.Contains(value, "GITHUB_TOKEN") || strings.Contains(value, "GITHUB_PERSONAL_ACCESS_TOKEN") {
					if token, err := parser.GetGitHubToken(); err != nil {
						return errors.New("GitHub token not found in environment (set GH_TOKEN or GITHUB_TOKEN)")
					} else {
						config.Env[key] = token
					}
				}
				// Handle our placeholder for GitHub token requirement
				if strings.Contains(value, "GITHUB_TOKEN_REQUIRED") {
					if token, err := parser.GetGitHubToken(); err != nil {
						return fmt.Errorf("GitHub token required but not available: %w", err)
					} else {
						config.Env[key] = token
					}
				}
			} else {
				// For direct environment variable values (not containing ${}),
				// check if they represent actual token values
				if value == "" {
					return fmt.Errorf("environment variable '%s' has empty value", key)
				}
				// If value contains "GITHUB_TOKEN_REQUIRED", treat it as needing validation
				if strings.Contains(value, "GITHUB_TOKEN_REQUIRED") {
					if token, err := parser.GetGitHubToken(); err != nil {
						return fmt.Errorf("GitHub token required but not available: %w", err)
					} else {
						config.Env[key] = token
					}
				} else {
					// Automatically try to get GitHub token for GitHub-related environment variables
					if key == "GITHUB_PERSONAL_ACCESS_TOKEN" || key == "GITHUB_TOKEN" || key == "GH_TOKEN" {
						if actualValue := os.Getenv(key); actualValue == "" { //nolint:osgetenvlibrary
							// Try to automatically get the GitHub token
							if token, err := parser.GetGitHubToken(); err == nil {
								config.Env[key] = token
							} else {
								return fmt.Errorf("GitHub token required for '%s' but not available: %w", key, err)
							}
						}
					} else {
						// For backward compatibility: check if environment variable with this name exists
						// This preserves the original behavior for existing tests
						if actualValue := os.Getenv(key); actualValue == "" { //nolint:osgetenvlibrary
							return fmt.Errorf("environment variable '%s' not set", key)
						}
					}
				}
			}
		}
		return nil
	}

	// Check availability of required secrets
	mcpValidationLog.Printf("Checking availability of %d required secrets", len(requiredSecrets))
	secretsStatus := checkSecretsAvailability(requiredSecrets, useActionsSecrets)

	// Separate secrets by availability
	var availableSecrets []SecretInfo
	var missingSecrets []SecretInfo

	for _, secret := range secretsStatus {
		if secret.Available {
			availableSecrets = append(availableSecrets, secret)
		} else {
			missingSecrets = append(missingSecrets, secret)
		}
	}
	for _, secret := range availableSecrets {
		if secret.Value == "" {
			continue
		}
		for key, value := range config.Headers {
			expression := "${{ secrets." + secret.Name + " }}"
			config.Headers[key] = strings.ReplaceAll(value, expression, secret.Value)
		}
	}

	// Display information about secrets
	if verbose {
		if len(availableSecrets) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d available secret(s):", len(availableSecrets))))
			for _, secret := range availableSecrets {
				source := "environment"
				if secret.Source == "actions" {
					source = "GitHub Actions"
				}
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("  ✓ %s (from %s)", secret.Name, source)))
			}
		}
	}

	// Warn about missing secrets
	if len(missingSecrets) > 0 {
		mcpValidationLog.Printf("Found %d missing secrets", len(missingSecrets))
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("⚠️  %d required secret(s) not found:", len(missingSecrets))))
		for _, secret := range missingSecrets {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("  ✗ "+secret.Name))
		}
	}

	mcpValidationLog.Printf("Secret validation completed: available=%d, missing=%d", len(availableSecrets), len(missingSecrets))
	return nil
}

// validateMCPServerConfiguration validates that the CLI is properly configured
// by running the status command as a test.
// Diagnostics are emitted through the debug logger only.
func validateMCPServerConfiguration(ctx context.Context, cmdPath string, env []string) error {
	mcpValidationLog.Printf("Validating MCP server configuration: cmdPath=%s", cmdPath)

	// Determine, log, and validate the binary path only if --cmd flag is not provided
	// When --cmd is provided, the user explicitly specified the binary path to use
	if cmdPath == "" {
		// Attempt to detect the binary path and assign it to cmdPath
		// This ensures the validation uses the actual binary path instead of falling back to "gh aw"
		detectedPath, err := logAndValidateBinaryPath()
		if err == nil && detectedPath != "" {
			cmdPath = detectedPath
		}
	}

	// Try to run the status command to verify CLI is working
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if cmdPath != "" {
		mcpValidationLog.Printf("Using custom command path: %s", cmdPath)
		// Use custom command path
		cmd = exec.CommandContext(ctx, cmdPath, "status")
	} else {
		mcpValidationLog.Print("Using default gh aw command with proper token handling")
		// Use default gh aw command with proper token handling
		cmd = workflow.ExecGHContext(ctx, "aw", "status")
	}
	if env != nil {
		cmd.Env = append([]string(nil), env...)
	}
	output, err := runMCPSubprocessCombinedOutput(ctx, cmd)

	if err != nil {
		// Check for common error cases
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			mcpValidationLog.Print("Status command timed out")
			return errors.New("status command timed out - this may indicate a configuration issue")
		}

		mcpValidationLog.Printf("Status command failed: %v", err)

		// If the command failed, provide helpful error message
		if cmdPath != "" {
			return fmt.Errorf("failed to run status command with custom command '%s': %w\nOutput: %s\n\nPlease ensure:\n  - The command path is correct and executable\n  - You are in a git repository with .github/workflows directory", cmdPath, err, string(output))
		}
		return fmt.Errorf("failed to run status command: %w\nOutput: %s\n\nPlease ensure:\n  - gh CLI is installed and in PATH\n  - gh aw extension is installed (run: gh extension install github/gh-aw)\n  - You are in a git repository with .github/workflows directory", err, string(output))
	}

	// Status command succeeded - configuration is valid
	mcpValidationLog.Print("MCP server configuration validated successfully")
	return nil
}

// validateMCPWorkflowName validates that a workflow name exists in the repository.
// Returns nil if the workflow exists, or an error with suggestions if not.
// Empty workflow names are considered valid (means "all workflows").
//
// Note: Unlike ValidateWorkflowName in validators.go (which enforces strict format
// rules and rejects empty names), this MCP-specific function accepts empty names
// because in the MCP context an empty workflow name is a valid wildcard meaning
// "apply to all workflows". It also performs existence checks rather than format
// checks, delegating to workflow.ResolveWorkflowName and the live workflow list.
func validateMCPWorkflowName(workflowName string) error {
	// Empty workflow name means "all workflows" - this is valid in the MCP context
	if workflowName == "" {
		return nil
	}

	mcpLog.Printf("Validating workflow name: %s", workflowName)

	// Try to resolve as workflow ID first
	resolvedName, err := workflow.ResolveWorkflowName(workflowName)
	if err == nil {
		mcpLog.Printf("Workflow name resolved successfully: %s -> %s", workflowName, resolvedName)
		return nil
	}

	// Check if it's a valid GitHub Actions workflow name
	agenticWorkflowNames, nameErr := getAgenticWorkflowNames(false)
	if nameErr == nil && slices.Contains(agenticWorkflowNames, workflowName) {
		mcpLog.Printf("Workflow name is valid GitHub Actions workflow name: %s", workflowName)
		return nil
	}

	// Workflow not found - build error with suggestions
	mcpLog.Printf("Workflow name not found: %s", workflowName)

	suggestions := []string{
		"Use the 'status' tool to see all available workflows",
		"Check for typos in the workflow name",
		"Use the workflow ID (e.g., 'test-claude') or GitHub Actions workflow name (e.g., 'Test Claude')",
	}

	// Add fuzzy match suggestions
	similarNames := suggestWorkflowNames(workflowName)
	if len(similarNames) > 0 {
		suggestions = append([]string{fmt.Sprintf("Did you mean: %s?", strings.Join(similarNames, ", "))}, suggestions...)
	}

	return fmt.Errorf("workflow '%s' not found. %s", workflowName, strings.Join(suggestions, " "))
}
