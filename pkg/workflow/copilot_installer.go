package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var copilotInstallerLog = logger.New("workflow:copilot_installer")

func generateCopilotInstallerSteps(version, stepName string, rootless bool, compiledVersion, copilotMinVersion string) []GitHubActionStep {
	copilotInstallerLog.Printf("Generating Copilot installer steps using install_copilot_cli.sh: version=%q, rootless=%v, compiledVersion=%q, copilotMinVersion=%q", version, rootless, compiledVersion, copilotMinVersion)

	rootlessFlag := ""
	if rootless {
		rootlessFlag = " --rootless"
	}

	// Use the install_copilot_cli.sh script from actions/setup/sh
	// This script includes retry logic for robustness against transient network failures.
	// The script downloads the Copilot CLI using curl with hardcoded github.com URLs.
	//
	// GH_HOST is pinned to github.com at the step level to prevent any workflow-level
	// env.GH_HOST (common on GHES deployments) from leaking into this step and
	// interfering with the Copilot CLI install/auth path, which requires github.com.
	if ExpressionPattern.MatchString(version) {
		// Version is a GitHub Actions expression (e.g. ${{ inputs.engine-version }}).
		// Pass it via an env var instead of direct shell interpolation to prevent injection.
		// GH_AW_COMPILED_VERSION is also injected so the script can fall back to compat.json
		// resolution when the expression evaluates to an empty string at runtime.
		copilotInstallerLog.Printf("Version contains GitHub Actions expression, using env var for injection safety: %s", version)
		stepLines := []string{
			"      - name: " + stepName,
			`        run: bash "${RUNNER_TEMP}/gh-aw/actions/install_copilot_cli.sh" "${ENGINE_VERSION}"` + rootlessFlag,
			"        env:",
			"          GH_HOST: github.com",
			"          ENGINE_VERSION: " + version,
		}
		stepLines = appendCopilotInstallerVersionEnv(stepLines, compiledVersion, copilotMinVersion)
		return []GitHubActionStep{GitHubActionStep(stepLines)}
	}

	if version == "" {
		// No explicit engine.version — let the script resolve via compat.json (priority 2)
		// or fall back to its baked-in default (priority 3).
		// Inject GH_AW_COMPILED_VERSION so the script can look up the correct compat window.
		copilotInstallerLog.Print("No explicit version; script will resolve via compat.json or baked-in default")
		stepLines := []string{
			"      - name: " + stepName,
			`        run: bash "${RUNNER_TEMP}/gh-aw/actions/install_copilot_cli.sh"` + rootlessFlag,
			"        env:",
			"          GH_HOST: github.com",
		}
		stepLines = appendCopilotInstallerVersionEnv(stepLines, compiledVersion, copilotMinVersion)
		return []GitHubActionStep{GitHubActionStep(stepLines)}
	}

	stepLines := []string{
		"      - name: " + stepName,
		"        run: bash \"${RUNNER_TEMP}/gh-aw/actions/install_copilot_cli.sh\" " + version + rootlessFlag,
		"        env:",
		"          GH_HOST: github.com",
	}

	return []GitHubActionStep{GitHubActionStep(stepLines)}
}

func appendCopilotInstallerVersionEnv(stepLines []string, compiledVersion, copilotMinVersion string) []string {
	if compiledVersion != "" {
		stepLines = append(stepLines, "          GH_AW_COMPILED_VERSION: "+compiledVersion)
	}
	if copilotMinVersion != "" {
		stepLines = append(stepLines, "          GH_AW_COPILOT_MIN_VERSION: "+copilotMinVersion)
	}
	return stepLines
}
