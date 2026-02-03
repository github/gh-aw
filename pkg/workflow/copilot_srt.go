package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotSRTLog = logger.New("workflow:copilot_srt")

// GenerateCopilotInstallerSteps creates GitHub Actions steps to install the Copilot CLI using the official installer.
func GenerateCopilotInstallerSteps(version, stepName string) []GitHubActionStep {
	// If no version is specified, use the default version from constants
	// This prevents the installer from defaulting to "latest"
	if version == "" {
		version = string(constants.DefaultCopilotVersion)
		copilotSRTLog.Printf("No version specified, using default: %s", version)
	}

	copilotSRTLog.Printf("Generating Copilot installer steps using install_copilot_cli.sh: version=%s", version)

	// Use the install_copilot_cli.sh script from actions/setup/sh
	// This script includes retry logic for robustness against transient network failures
	stepLines := []string{
		fmt.Sprintf("      - name: %s", stepName),
		fmt.Sprintf("        run: /opt/gh-aw/actions/install_copilot_cli.sh %s", version),
	}

	return []GitHubActionStep{GitHubActionStep(stepLines)}
}

// generateSRTSystemDepsStep creates a GitHub Actions step to install SRT system dependencies.
func generateSRTSystemDepsStep() GitHubActionStep {
	stepLines := []string{
		"      - name: Install Sandbox Runtime System Dependencies",
		"        run: |",
		"          echo \"Installing system dependencies for Sandbox Runtime\"",
		"          sudo apt-get update",
		"          sudo apt-get install -y ripgrep bubblewrap socat",
		"          echo \"System dependencies installed successfully\"",
		"          echo \"Verifying installations:\"",
		"          rg --version",
		"          bwrap --version",
		"          socat -V",
	}

	return GitHubActionStep(stepLines)
}

// generateSRTSystemConfigStep creates a GitHub Actions step to configure system for SRT.
func generateSRTSystemConfigStep() GitHubActionStep {
	stepLines := []string{
		"      - name: Configure System for Sandbox Runtime",
		"        run: |",
		"          echo \"Disabling AppArmor namespace restrictions for bubblewrap\"",
		"          sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0",
		"          echo \"System configuration applied successfully\"",
	}

	return GitHubActionStep(stepLines)
}

// generateSRTInstallationStep creates a GitHub Actions step to install Sandbox Runtime.
func generateSRTInstallationStep() GitHubActionStep {
	srtVersion := string(constants.DefaultSandboxRuntimeVersion)
	stepLines := []string{
		"      - name: Install Sandbox Runtime",
		"        run: |",
		fmt.Sprintf("          echo \"Installing @anthropic-ai/sandbox-runtime@%s locally\"", srtVersion),
		fmt.Sprintf("          npm install @anthropic-ai/sandbox-runtime@%s", srtVersion),
		"          echo \"Sandbox Runtime installed successfully\"",
	}

	return GitHubActionStep(stepLines)
}

// generateSRTWrapperScript creates a shell script that wraps the copilot command with SRT.
func generateSRTWrapperScript(copilotCommand, srtConfigJSON, logFile, logsFolder string) string {
	// Escape quotes and special characters in the config JSON for shell
	escapedConfigJSON := strings.ReplaceAll(srtConfigJSON, "'", "'\\''")

	// Escape the copilot command for JavaScript string literal (not shell)
	// Must escape backslashes first, then single quotes
	escapedCopilotCommand := strings.ReplaceAll(copilotCommand, "\\", "\\\\")
	escapedCopilotCommand = strings.ReplaceAll(escapedCopilotCommand, "'", "\\'")

	script := fmt.Sprintf(`set -o pipefail

# Pre-create required directories for Sandbox Runtime
mkdir -p /home/runner/.copilot
mkdir -p /tmp/claude

# Create .srt-settings.json
cat > .srt-settings.json << 'SRT_CONFIG_EOF'
%s
SRT_CONFIG_EOF

# Create Node.js wrapper script for SRT
cat > ./.srt-wrapper.js << 'SRT_WRAPPER_EOF'
const { SandboxManager } = require('@anthropic-ai/sandbox-runtime');
const { spawn } = require('child_process');
const { readFileSync } = require('fs');

async function main() {
  try {
    // Load the sandbox configuration from .srt-settings.json
    const configData = readFileSync('.srt-settings.json', 'utf-8');
    const config = JSON.parse(configData);

    // Initialize the sandbox (starts proxy servers, etc.)
    await SandboxManager.initialize(config);

    // Collect required environment variables for the sandboxed process
    // These need to be explicitly passed because bwrap doesn't inherit env vars
    // NOTE: Do NOT include GITHUB_TOKEN or GH_TOKEN here - they conflict with
    // COPILOT_GITHUB_TOKEN. The Copilot CLI checks these tokens and if it finds
    // the GitHub Actions default GITHUB_TOKEN (which is not valid for Copilot auth),
    // it will fail with "No authentication information found" even when
    // COPILOT_GITHUB_TOKEN is correctly set.
    const requiredEnvVars = [
      'COPILOT_GITHUB_TOKEN',
      'COPILOT_AGENT_RUNNER_TYPE',
      'XDG_CONFIG_HOME',
      'GITHUB_STEP_SUMMARY',
      'GITHUB_HEAD_REF',
      'GITHUB_REF_NAME',
      'GITHUB_WORKSPACE',
      'GH_AW_PROMPT',
      'GH_AW_MCP_CONFIG',
      'GITHUB_MCP_SERVER_TOKEN',
      'GH_AW_SAFE_OUTPUTS',
      'GH_AW_STARTUP_TIMEOUT',
      'GH_AW_TOOL_TIMEOUT',
      'GH_AW_MAX_TURNS',
    ];

    // Build environment variable export statements for the command
    // Use 'export' with semicolon to ensure variables propagate through nested bash invocations
    const envPrefix = requiredEnvVars
      .filter(key => process.env[key] !== undefined)
      .map(key => {
        const value = process.env[key].replace(/'/g, "'\\''"); // Escape single quotes for shell
        return "export " + key + "='" + value + "';";
      })
      .join(' ');

    // The command to run
    const baseCommand = '%s';

    // Prepend environment variables to the command
    const command = envPrefix ? envPrefix + ' ' + baseCommand : baseCommand;

    // Wrap the command with sandbox restrictions
    const sandboxedCommand = await SandboxManager.wrapWithSandbox(command);

    // Execute the sandboxed command
    const child = spawn(sandboxedCommand, {
      shell: true,
      stdio: 'inherit',
      env: process.env
    });

    // Handle exit
    child.on('exit', async (code) => {
      // Cleanup when done
      await SandboxManager.reset();
      process.exit(code || 0);
    });

    // Handle errors
    child.on('error', async (err) => {
      console.error('Error executing command:', err);
      await SandboxManager.reset();
      process.exit(1);
    });
  } catch (err) {
    console.error('Fatal error:', err);
    try {
      await SandboxManager.reset();
    } catch (cleanupErr) {
      console.error('Error during cleanup:', cleanupErr);
    }
    process.exit(1);
  }
}

main();
SRT_WRAPPER_EOF

# Run the Node.js wrapper script
node ./.srt-wrapper.js 2>&1 | tee %s

# Move preserved Copilot logs to expected location
COPILOT_LOGS_DIR="$(find /tmp -maxdepth 1 -type d -name 'copilot-logs-*' -printf '%%T@ %%p\n' 2>/dev/null | sort -rn | head -1 | cut -d' ' -f2)"
if [ -n "$COPILOT_LOGS_DIR" ] && [ -d "$COPILOT_LOGS_DIR" ]; then
  echo "Moving Copilot logs from $COPILOT_LOGS_DIR to %s"
  mkdir -p %s
  mv "$COPILOT_LOGS_DIR"/* %s || true
  rmdir "$COPILOT_LOGS_DIR" || true
fi`, escapedConfigJSON, escapedCopilotCommand, shellEscapeArg(logFile), shellEscapeArg(logsFolder), shellEscapeArg(logsFolder), shellEscapeArg(logsFolder))

	return script
}

// generateSquidLogsUploadStep creates a GitHub Actions step to upload Squid logs as artifact.
func generateSquidLogsUploadStep(workflowName string) GitHubActionStep {
	sanitizedName := strings.ToLower(SanitizeWorkflowName(workflowName))
	artifactName := fmt.Sprintf("firewall-logs-%s", sanitizedName)
	// Firewall logs are now at a known location in the sandbox folder structure
	firewallLogsDir := "/tmp/gh-aw/sandbox/firewall/logs/"

	stepLines := []string{
		"      - name: Upload Firewall Logs",
		"        if: always()",
		"        continue-on-error: true",
		fmt.Sprintf("        uses: %s", GetActionPin("actions/upload-artifact")),
		"        with:",
		fmt.Sprintf("          name: %s", artifactName),
		fmt.Sprintf("          path: %s", firewallLogsDir),
		"          if-no-files-found: ignore",
	}

	return GitHubActionStep(stepLines)
}

// generateFirewallLogParsingStep creates a GitHub Actions step to parse firewall logs and create step summary.
func generateFirewallLogParsingStep(workflowName string) GitHubActionStep {
	// Firewall logs are at a known location in the sandbox folder structure
	firewallLogsDir := "/tmp/gh-aw/sandbox/firewall/logs"

	stepLines := []string{
		"      - name: Print firewall logs",
		"        if: always()",
		"        continue-on-error: true",
		"        env:",
		fmt.Sprintf("          AWF_LOGS_DIR: %s", firewallLogsDir),
		"        run: |",
		"          # Fix permissions on firewall logs so they can be uploaded as artifacts",
		"          # AWF runs with sudo, creating files owned by root",
		fmt.Sprintf("          sudo chmod -R a+r %s 2>/dev/null || true", firewallLogsDir),
		"          awf logs summary | tee -a \"$GITHUB_STEP_SUMMARY\"",
	}

	return GitHubActionStep(stepLines)
}
