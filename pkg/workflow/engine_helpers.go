// This file provides shared helper functions for AI engine implementations.
//
// This file contains utilities used across multiple AI engine files (copilot_engine.go,
// claude_engine.go, codex_engine.go, custom_engine.go) to generate common workflow
// steps and configurations.
//
// # Organization Rationale
//
// These helper functions are grouped here because they:
//   - Are used by 3+ engine implementations (shared utilities)
//   - Provide common patterns for agent installation and npm setup
//   - Have a clear domain focus (engine workflow generation)
//   - Are stable and change infrequently
//
// This follows the helper file conventions documented in skills/developer/SKILL.md.
//
// # Key Functions
//
// Agent Installation:
//   - GenerateAgentInstallSteps() - Generate agent installation workflow steps
//
// NPM Installation:
//   - GenerateNpmInstallStep() - Generate npm package installation step
//   - GenerateEngineDependenciesInstallStep() - Generate engine dependencies install step
//
// Configuration:
//   - GetClaudeSystemPrompt() - Get system prompt for Claude engine
//
// These functions encapsulate shared logic that would otherwise be duplicated across
// engine files, maintaining DRY principles while keeping engine-specific code separate.

package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var engineHelpersLog = logger.New("workflow:engine_helpers")

// EngineInstallConfig contains configuration for engine installation steps.
// This struct centralizes the configuration needed to generate the common
// installation steps shared by all engines (secret validation and npm installation).
type EngineInstallConfig struct {
	// Secrets is a list of secret names to validate (at least one must be set)
	Secrets []string
	// DocsURL is the documentation URL shown when secret validation fails
	DocsURL string
	// NpmPackage is the npm package name (e.g., "@github/copilot")
	NpmPackage string
	// Version is the default version of the npm package
	Version string
	// Name is the engine display name for secret validation messages (e.g., "Claude Code")
	Name string
	// CliName is the CLI name used for cache key prefix (e.g., "copilot")
	CliName string
	// InstallStepName is the display name for the npm install step (e.g., "Install Claude Code CLI")
	InstallStepName string
}

// GetBaseInstallationSteps returns the common installation steps for an engine.
// This includes secret validation and npm package installation steps that are
// shared across all engines.
//
// Parameters:
//   - config: Engine-specific configuration for installation
//   - workflowData: The workflow data containing engine configuration
//
// Returns:
//   - []GitHubActionStep: The base installation steps (secret validation + npm install)
func GetBaseInstallationSteps(config EngineInstallConfig, workflowData *WorkflowData) []GitHubActionStep {
	engineHelpersLog.Printf("Generating base installation steps for %s engine: workflow=%s", config.Name, workflowData.Name)

	var steps []GitHubActionStep

	// Add secret validation step
	secretValidation := GenerateMultiSecretValidationStep(
		config.Secrets,
		config.Name,
		config.DocsURL,
	)
	steps = append(steps, secretValidation)

	// Determine step name - use InstallStepName if provided, otherwise default to "Install <Name>"
	stepName := config.InstallStepName
	if stepName == "" {
		stepName = fmt.Sprintf("Install %s", config.Name)
	}

	// Add npm package installation steps
	npmSteps := BuildStandardNpmEngineInstallSteps(
		config.NpmPackage,
		config.Version,
		stepName,
		config.CliName,
		workflowData,
	)
	steps = append(steps, npmSteps...)

	return steps
}

// ExtractAgentIdentifier extracts the agent identifier (filename without extension) from an agent file path.
// This is used by the Copilot CLI which expects agent identifiers, not full paths.
//
// Parameters:
//   - agentFile: The relative path to the agent file (e.g., ".github/agents/test-agent.md" or ".github/agents/test-agent.agent.md")
//
// Returns:
//   - string: The agent identifier (e.g., "test-agent")
//
// Example:
//
//	identifier := ExtractAgentIdentifier(".github/agents/my-agent.md")
//	// Returns: "my-agent"
//
//	identifier := ExtractAgentIdentifier(".github/agents/my-agent.agent.md")
//	// Returns: "my-agent"
func ExtractAgentIdentifier(agentFile string) string {
	engineHelpersLog.Printf("Extracting agent identifier from: %s", agentFile)
	// Extract the base filename from the path
	lastSlash := strings.LastIndex(agentFile, "/")
	filename := agentFile
	if lastSlash >= 0 {
		filename = agentFile[lastSlash+1:]
	}

	// Remove extensions in order: .agent.md, then .md, then .agent
	// This handles all possible agent file naming conventions
	filename = strings.TrimSuffix(filename, ".agent.md")
	filename = strings.TrimSuffix(filename, ".md")
	filename = strings.TrimSuffix(filename, ".agent")

	return filename
}

// ResolveAgentFilePath returns the properly quoted agent file path with GITHUB_WORKSPACE prefix.
// This helper extracts the common pattern shared by Copilot, Codex, and Claude engines.
//
// The agent file path is relative to the repository root, so we prefix it with ${GITHUB_WORKSPACE}
// and wrap the entire expression in double quotes to handle paths with spaces while allowing
// shell variable expansion.
//
// Parameters:
//   - agentFile: The relative path to the agent file (e.g., ".github/agents/test-agent.md")
//
// Returns:
//   - string: The double-quoted path with GITHUB_WORKSPACE prefix (e.g., "${GITHUB_WORKSPACE}/.github/agents/test-agent.md")
//
// Example:
//
//	agentPath := ResolveAgentFilePath(".github/agents/my-agent.md")
//	// Returns: "${GITHUB_WORKSPACE}/.github/agents/my-agent.md"
//
// Note: The entire path is wrapped in double quotes (not just the variable) to ensure:
//  1. The shellEscapeArg function recognizes it as already-quoted and doesn't add single quotes
//  2. Shell variable expansion works (${GITHUB_WORKSPACE} gets expanded inside double quotes)
//  3. Paths with spaces are properly handled
func ResolveAgentFilePath(agentFile string) string {
	return fmt.Sprintf("\"${GITHUB_WORKSPACE}/%s\"", agentFile)
}

// BuildStandardNpmEngineInstallSteps creates standard npm installation steps for engines
// This helper extracts the common pattern shared by Copilot, Codex, and Claude engines.
//
// Parameters:
//   - packageName: The npm package name (e.g., "@github/copilot")
//   - defaultVersion: The default version constant (e.g., constants.DefaultCopilotVersion)
//   - stepName: The display name for the install step (e.g., "Install GitHub Copilot CLI")
//   - cacheKeyPrefix: The cache key prefix (e.g., "copilot")
//   - workflowData: The workflow data containing engine configuration
//
// Returns:
//   - []GitHubActionStep: The installation steps including Node.js setup
func BuildStandardNpmEngineInstallSteps(
	packageName string,
	defaultVersion string,
	stepName string,
	cacheKeyPrefix string,
	workflowData *WorkflowData,
) []GitHubActionStep {
	engineHelpersLog.Printf("Building npm engine install steps: package=%s, version=%s", packageName, defaultVersion)

	// Use version from engine config if provided, otherwise default to pinned version
	version := defaultVersion
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
		version = workflowData.EngineConfig.Version
		engineHelpersLog.Printf("Using engine config version: %s", version)
	}

	// Add npm package installation steps (includes Node.js setup)
	return GenerateNpmInstallSteps(
		packageName,
		version,
		stepName,
		cacheKeyPrefix,
		true, // Include Node.js setup
	)
}

// InjectCustomEngineSteps processes custom steps from engine config and converts them to GitHubActionSteps.
// This shared function extracts the common pattern used by Copilot, Codex, and Claude engines.
//
// Parameters:
//   - workflowData: The workflow data containing engine configuration
//   - convertStepFunc: A function that converts a step map to YAML string (engine-specific)
//
// Returns:
//   - []GitHubActionStep: Array of custom steps ready to be included in the execution pipeline
func InjectCustomEngineSteps(
	workflowData *WorkflowData,
	convertStepFunc func(map[string]any) (string, error),
) []GitHubActionStep {
	var steps []GitHubActionStep

	// Handle custom steps if they exist in engine config
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Steps) > 0 {
		engineHelpersLog.Printf("Injecting %d custom engine steps", len(workflowData.EngineConfig.Steps))
		for _, step := range workflowData.EngineConfig.Steps {
			stepYAML, err := convertStepFunc(step)
			if err != nil {
				engineHelpersLog.Printf("Failed to convert custom step: %v", err)
				// Log error but continue with other steps
				continue
			}
			steps = append(steps, GitHubActionStep{stepYAML})
		}
		engineHelpersLog.Printf("Successfully injected %d custom engine steps", len(steps))
	}

	return steps
}

// RenderCustomMCPToolConfigHandler is a function type that engines must provide to render their specific MCP config
// FormatStepWithCommandAndEnv formats a GitHub Actions step with command and environment variables.
// This shared function extracts the common pattern used by Copilot and Codex engines.
//
// Parameters:
//   - stepLines: Existing step lines to append to (e.g., name, id, comments, timeout)
//   - command: The command to execute (may contain multiple lines)
//   - env: Map of environment variables to include in the step
//
// Returns:
//   - []string: Complete step lines including run command and env section
func FormatStepWithCommandAndEnv(stepLines []string, command string, env map[string]string) []string {
	engineHelpersLog.Printf("Formatting step with command and %d environment variables", len(env))
	// Add the run section
	stepLines = append(stepLines, "        run: |")

	// Split command into lines and indent them properly
	commandLines := strings.Split(command, "\n")
	for _, line := range commandLines {
		// Don't add indentation to empty lines
		if line == "" {
			stepLines = append(stepLines, "")
		} else {
			stepLines = append(stepLines, "          "+line)
		}
	}

	// Add environment variables
	if len(env) > 0 {
		stepLines = append(stepLines, "        env:")
		// Sort environment keys for consistent output
		envKeys := make([]string, 0, len(env))
		for key := range env {
			envKeys = append(envKeys, key)
		}
		sort.Strings(envKeys)

		for _, key := range envKeys {
			value := env[key]
			stepLines = append(stepLines, fmt.Sprintf("          %s: %s", key, value))
		}
	}

	return stepLines
}

// FilterEnvForSecrets filters environment variables to only include allowed secrets
// This is a security measure to ensure that only necessary secrets are passed to the execution step
//
// Parameters:
//   - env: Map of all environment variables
//   - allowedSecrets: List of secret names that are allowed to be passed
//
// Returns:
//   - map[string]string: Filtered environment variables with only allowed secrets
func FilterEnvForSecrets(env map[string]string, allowedSecrets []string) map[string]string {
	engineHelpersLog.Printf("Filtering environment variables: total=%d, allowed_secrets=%d", len(env), len(allowedSecrets))

	// Create a set of allowed secret names for fast lookup
	allowedSet := make(map[string]bool)
	for _, secret := range allowedSecrets {
		allowedSet[secret] = true
	}

	filtered := make(map[string]string)
	secretsRemoved := 0

	for key, value := range env {
		// Check if this env var is a secret reference (starts with "${{ secrets.")
		if strings.Contains(value, "${{ secrets.") {
			// Extract the secret name from the expression
			// Format: ${{ secrets.SECRET_NAME }} or ${{ secrets.SECRET_NAME || ... }}
			secretName := ExtractSecretName(value)
			if secretName != "" && !allowedSet[secretName] {
				engineHelpersLog.Printf("Removing unauthorized secret from env: %s (secret: %s)", key, secretName)
				secretsRemoved++
				continue
			}
		}
		filtered[key] = value
	}

	engineHelpersLog.Printf("Filtered environment variables: kept=%d, removed=%d", len(filtered), secretsRemoved)
	return filtered
}

// GetHostedToolcachePathSetup returns a shell command that adds all runtime binaries
// from /opt/hostedtoolcache to PATH. This includes Node.js, Python, Go, Ruby, and other
// runtimes installed via actions/setup-* steps.
//
// The hostedtoolcache directory structure is: /opt/hostedtoolcache/<tool>/<version>/<arch>/bin
// This function generates a command that finds all bin directories and adds them to PATH.
//
// IMPORTANT: The command uses GH_AW_TOOL_BINS (computed by GetToolBinsSetup) which contains
// the specific tool paths from environment variables like GOROOT, JAVA_HOME, etc. These paths
// are computed on the RUNNER side and passed to the container as a literal value via --env,
// avoiding shell injection risks from variable expansion inside the container.
//
// This ensures that the version configured by actions/setup-* takes precedence over other
// versions that may exist in hostedtoolcache. Without this, the generic `find` command
// returns directories in alphabetical order, causing older versions (e.g., Go 1.22.12)
// to shadow newer ones (e.g., Go 1.25.6) because "1.22" < "1.25" alphabetically.
//
// This is used by all engine implementations (Copilot, Claude, Codex) to ensure consistent
// access to runtime tools inside the agent container.
//
// Returns:
//   - string: A shell command that sets up PATH with all hostedtoolcache binaries
//
// Example output:
//
//	export PATH="$GH_AW_TOOL_BINS$(find /opt/hostedtoolcache -maxdepth 4 -type d -name bin 2>/dev/null | tr '\n' ':')$PATH"
func GetHostedToolcachePathSetup() string {
	// Use GH_AW_TOOL_BINS which is computed on the runner side by GetToolBinsSetup()
	// and passed to the container via --env. This avoids shell injection risks from
	// expanding variables like GOROOT inside the container.
	//
	// GH_AW_TOOL_BINS contains paths like "/opt/hostedtoolcache/go/1.25.6/x64/bin:"
	// computed from GOROOT, JAVA_HOME, etc. on the runner where they are trusted.

	// Generic find for all other hostedtoolcache binaries (Node.js, Python, etc.)
	genericFind := `$(find /opt/hostedtoolcache -maxdepth 4 -type d -name bin 2>/dev/null | tr '\n' ':')`

	// Build the raw PATH string, then sanitize it using GetSanitizedPATHExport()
	// to remove empty elements, leading/trailing colons, and collapse multiple colons
	rawPath := fmt.Sprintf(`$GH_AW_TOOL_BINS%s$PATH`, genericFind)
	return GetSanitizedPATHExport(rawPath)
}

// GetNpmBinPathSetup returns a simple shell command that adds hostedtoolcache bin directories
// to PATH. This is specifically for npm-installed CLIs (like Claude and Codex) that need
// to find their binaries installed via `npm install -g`.
//
// Unlike GetHostedToolcachePathSetup(), this does NOT use GH_AW_TOOL_BINS because AWF's
// native chroot mode already handles tool-specific paths (GOROOT, JAVA_HOME, etc.) via
// AWF_HOST_PATH and the entrypoint.sh script. This function only adds the generic
// hostedtoolcache bin directories for npm packages.
//
// Returns:
//   - string: A shell command that exports PATH with hostedtoolcache bin directories prepended
func GetNpmBinPathSetup() string {
	// Find all bin directories in hostedtoolcache (Node.js, Python, etc.)
	// This finds paths like /opt/hostedtoolcache/node/22.13.0/x64/bin
	//
	// After the find, re-prepend GOROOT/bin if set. The find returns directories
	// alphabetically, so go/1.23.12 shadows go/1.25.0. Re-prepending GOROOT/bin
	// ensures the Go version set by actions/setup-go takes precedence.
	// AWF's entrypoint.sh exports GOROOT before the user command runs.
	return `export PATH="$(find /opt/hostedtoolcache -maxdepth 4 -type d -name bin 2>/dev/null | tr '\n' ':')$PATH"; [ -n "$GOROOT" ] && export PATH="$GOROOT/bin:$PATH" || true`
}

// GetSanitizedPATHExport returns a shell command that sets PATH to the given value
// with sanitization to remove security risks from malformed PATH entries.
//
// The sanitization removes:
//   - Leading colons (e.g., ":/usr/bin" -> "/usr/bin")
//   - Trailing colons (e.g., "/usr/bin:" -> "/usr/bin")
//   - Empty elements (e.g., "/a::/b" -> "/a:/b", multiple colons collapsed to one)
//
// Empty PATH elements are a security risk because they cause the current directory
// to be searched for executables, which could allow malicious code execution.
//
// The sanitization logic is implemented in actions/setup/sh/sanitize_path.sh and
// is sourced at runtime from /opt/gh-aw/actions/sanitize_path.sh.
//
// Parameters:
//   - rawPath: The unsanitized PATH value (may contain shell expansions like $PATH)
//
// Returns:
//   - string: A shell command that sources the sanitize script to export the sanitized PATH
//
// Example:
//
//	GetSanitizedPATHExport("$GH_AW_TOOL_BINS$PATH")
//	// Returns: source /opt/gh-aw/actions/sanitize_path.sh "$GH_AW_TOOL_BINS$PATH"
func GetSanitizedPATHExport(rawPath string) string {
	// Source the sanitize_path.sh script which handles:
	// 1. Remove leading colons
	// 2. Remove trailing colons
	// 3. Collapse multiple colons into single colons
	// 4. Export the sanitized PATH
	return fmt.Sprintf(`source /opt/gh-aw/actions/sanitize_path.sh "%s"`, rawPath)
}

// GetToolBinsSetup returns a shell command that computes the GH_AW_TOOL_BINS environment
// variable from specific tool paths (GOROOT, JAVA_HOME, etc.).
//
// This command should be run on the RUNNER side before invoking AWF, and the resulting
// GH_AW_TOOL_BINS should be passed to the container via --env. This ensures the paths
// are computed where they are trusted, avoiding shell injection risks.
//
// The computed paths are prepended to PATH (via GetHostedToolcachePathSetup) before the
// generic find results, ensuring versions set by actions/setup-* take precedence over
// alphabetically-earlier versions in hostedtoolcache.
//
// Returns:
//   - string: A shell command that sets GH_AW_TOOL_BINS
//
// Example output when GOROOT=/opt/hostedtoolcache/go/1.25.6/x64 and JAVA_HOME=/opt/hostedtoolcache/Java/17.0.0/x64:
//
//	GH_AW_TOOL_BINS="/opt/hostedtoolcache/go/1.25.6/x64/bin:/opt/hostedtoolcache/Java/17.0.0/x64/bin:"
func GetToolBinsSetup() string {
	// Build GH_AW_TOOL_BINS from specific tool paths on the runner side.
	// Each path is only added if the corresponding env var is set and non-empty.
	// This runs on the runner where the env vars are trusted values from actions/setup-*.
	//
	// Tools with /bin subdirectory:
	//   - Go: Detected via `go env GOROOT` (actions/setup-go doesn't export GOROOT)
	//   - JAVA_HOME: Java installation root (actions/setup-java)
	//   - CARGO_HOME: Cargo/Rust installation (rustup)
	//   - GEM_HOME: Ruby gems (actions/setup-ruby)
	//   - CONDA: Conda installation
	//
	// Tools where the path IS the bin directory (no /bin suffix needed):
	//   - PIPX_BIN_DIR: pipx binary directory
	//   - SWIFT_PATH: Swift binary path
	//   - DOTNET_ROOT: .NET root (binaries are in root, not /bin)
	return `GH_AW_TOOL_BINS=""; command -v go >/dev/null 2>&1 && GH_AW_TOOL_BINS="$(go env GOROOT)/bin:$GH_AW_TOOL_BINS"; [ -n "$JAVA_HOME" ] && GH_AW_TOOL_BINS="$JAVA_HOME/bin:$GH_AW_TOOL_BINS"; [ -n "$CARGO_HOME" ] && GH_AW_TOOL_BINS="$CARGO_HOME/bin:$GH_AW_TOOL_BINS"; [ -n "$GEM_HOME" ] && GH_AW_TOOL_BINS="$GEM_HOME/bin:$GH_AW_TOOL_BINS"; [ -n "$CONDA" ] && GH_AW_TOOL_BINS="$CONDA/bin:$GH_AW_TOOL_BINS"; [ -n "$PIPX_BIN_DIR" ] && GH_AW_TOOL_BINS="$PIPX_BIN_DIR:$GH_AW_TOOL_BINS"; [ -n "$SWIFT_PATH" ] && GH_AW_TOOL_BINS="$SWIFT_PATH:$GH_AW_TOOL_BINS"; [ -n "$DOTNET_ROOT" ] && GH_AW_TOOL_BINS="$DOTNET_ROOT:$GH_AW_TOOL_BINS"; export GH_AW_TOOL_BINS`
}

// GetToolBinsEnvArg returns the AWF --env argument for passing GH_AW_TOOL_BINS to the container.
// This should be used after GetToolBinsSetup() has been run to compute the value.
//
// Returns:
//   - []string: AWF arguments ["--env", "GH_AW_TOOL_BINS=$GH_AW_TOOL_BINS"]
func GetToolBinsEnvArg() []string {
	// Pre-wrap in double quotes so shellEscapeArg preserves them (allowing shell expansion)
	return []string{"--env", "\"GH_AW_TOOL_BINS=$GH_AW_TOOL_BINS\""}
}

// EngineHasValidateSecretStep checks if the engine's installation steps include the validate-secret step.
// This is used to determine whether the secret_verification_result job output should be added.
//
// The validate-secret step is only added by engines that include it in GetInstallationSteps():
//   - Copilot engine: Adds step when GetRequiredSecretNames returns non-empty
//   - Claude engine: Adds step when GetRequiredSecretNames returns non-empty
//   - Codex engine: Adds step when GetRequiredSecretNames returns non-empty
//   - Custom engine: Never adds this step (returns empty from GetInstallationSteps)
//
// Implementation Note:
// This uses simple string matching which is acceptable because:
//   - Installation steps are generated by our code, not user input
//   - The "id: validate-secret" format is controlled by GenerateMultiSecretValidationStep()
//   - GitHubActionStep is already a string slice, not structured YAML
//
// Parameters:
//   - engine: The agentic engine to check
//   - data: The workflow data (needed for GetInstallationSteps)
//
// Returns:
//   - bool: true if the engine includes the validate-secret step, false otherwise
func EngineHasValidateSecretStep(engine CodingAgentEngine, data *WorkflowData) bool {
	installSteps := engine.GetInstallationSteps(data)
	for _, step := range installSteps {
		for _, line := range step {
			// String matching is safe here because installation steps are generated by our code
			// and follow the format: "        id: validate-secret"
			if strings.Contains(line, "id: validate-secret") {
				return true
			}
		}
	}
	return false
}
