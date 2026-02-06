// This file contains MCP (Model Context Protocol) validation functions.
// This file consolidates validation logic for MCP server configurations.

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var mcpValidationLog = logger.New("cli:mcp_validation")

// GetBinaryPath returns the path to the currently running gh-aw binary.
// This is used by the MCP server to determine where the gh-aw binary is located
// when launching itself with different arguments.
//
// Returns the absolute path to the binary, or an error if the path cannot be determined.
func GetBinaryPath() (string, error) {
	// Get the path to the currently running executable
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve any symlinks to get the actual binary path
	// This is important because gh extensions are typically symlinked
	// Note: EvalSymlinks already returns an absolute path
	resolvedPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		// If we can't resolve symlinks, use the original path
		mcpValidationLog.Printf("Warning: failed to resolve symlinks for %s: %v", exePath, err)
		return exePath, nil
	}

	return resolvedPath, nil
}

// logAndValidateBinaryPath determines the binary path, logs it, and validates it exists.
// Returns the detected binary path and an error if the path cannot be determined or if the file doesn't exist.
// This is a helper used by both runMCPServer and validateMCPServerConfiguration.
func logAndValidateBinaryPath() (string, error) {
	binaryPath, err := GetBinaryPath()
	if err != nil {
		mcpValidationLog.Printf("Warning: failed to get binary path: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: failed to get binary path: %v", err)))
		return "", err
	}

	// Check if the binary file exists
	if _, err := os.Stat(binaryPath); err != nil {
		if os.IsNotExist(err) {
			mcpValidationLog.Printf("ERROR: binary file does not exist at path: %s", binaryPath)
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("ERROR: binary file does not exist at path: %s", binaryPath)))
			return "", fmt.Errorf("binary file does not exist at path: %s", binaryPath)
		}
		mcpValidationLog.Printf("Warning: failed to stat binary file at %s: %v", binaryPath, err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: failed to stat binary file at %s: %v", binaryPath, err)))
		return "", err
	}

	// Log the binary path for debugging
	mcpValidationLog.Printf("gh-aw binary path: %s", binaryPath)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("gh-aw binary path: %s", binaryPath)))
	return binaryPath, nil
}

// validateServerSecrets checks if required environment variables/secrets are available
func validateServerSecrets(config parser.MCPServerConfig, verbose bool, useActionsSecrets bool) error {
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
						return fmt.Errorf("GitHub token not found in environment (set GH_TOKEN or GITHUB_TOKEN)")
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
						if actualValue := os.Getenv(key); actualValue == "" {
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
						if actualValue := os.Getenv(key); actualValue == "" {
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
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("  ✗ %s", secret.Name)))
		}
	}

	mcpValidationLog.Printf("Secret validation completed: available=%d, missing=%d", len(availableSecrets), len(missingSecrets))
	return nil
}

// validateMCPServerConfiguration validates that the CLI is properly configured
// by running the status command as a test
func validateMCPServerConfiguration(cmdPath string) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Check for common error cases
		if ctx.Err() == context.DeadlineExceeded {
			mcpValidationLog.Print("Status command timed out")
			errMsg := "status command timed out - this may indicate a configuration issue"
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
			return fmt.Errorf("status command timed out - this may indicate a configuration issue")
		}

		mcpValidationLog.Printf("Status command failed: %v", err)

		// If the command failed, provide helpful error message
		if cmdPath != "" {
			errMsg := fmt.Sprintf("failed to run status command with custom command '%s': %v\nOutput: %s\n\nPlease ensure:\n  - The command path is correct and executable\n  - You are in a git repository with .github/workflows directory", cmdPath, err, string(output))
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
			return fmt.Errorf("failed to run status command with custom command '%s': %w\nOutput: %s\n\nPlease ensure:\n  - The command path is correct and executable\n  - You are in a git repository with .github/workflows directory", cmdPath, err, string(output))
		}
		errMsg := fmt.Sprintf("failed to run status command: %v\nOutput: %s\n\nPlease ensure:\n  - gh CLI is installed and in PATH\n  - gh aw extension is installed (run: gh extension install github/gh-aw)\n  - You are in a git repository with .github/workflows directory", err, string(output))
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return fmt.Errorf("failed to run status command: %w\nOutput: %s\n\nPlease ensure:\n  - gh CLI is installed and in PATH\n  - gh aw extension is installed (run: gh extension install github/gh-aw)\n  - You are in a git repository with .github/workflows directory", err, string(output))
	}

	// Status command succeeded - configuration is valid
	mcpValidationLog.Print("MCP server configuration validated successfully")
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✅ Configuration validated successfully"))
	return nil
}
