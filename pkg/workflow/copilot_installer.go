package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotInstallerLog = logger.New("workflow:copilot_installer")

func generateCopilotInstallerSteps(version, stepName string, rootless bool, compiledVersion, copilotMinVersion, guestLibc, checksumsSHA256 string) []GitHubActionStep {
	preferToolcache := false
	if checksumsSHA256 == "" && (version == "" || strings.TrimPrefix(version, "v") == string(constants.DefaultCopilotVersion)) {
		if version == "" {
			version = string(constants.DefaultCopilotVersion)
		}
		checksumsSHA256 = constants.DefaultCopilotChecksumsSHA256
		preferToolcache = true
	}
	copilotInstallerLog.Printf("Generating Copilot installer steps using install_copilot_cli.sh: version=%q, rootless=%v, compiledVersion=%q, copilotMinVersion=%q", version, rootless, compiledVersion, copilotMinVersion)

	installerFlags := ""
	if rootless {
		installerFlags = " --rootless"
	}
	if checksumsSHA256 != "" {
		installerFlags += " --checksums-sha256 " + checksumsSHA256
	}
	if preferToolcache {
		installerFlags += " --prefer-toolcache"
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
			`        run: bash "${RUNNER_TEMP}/gh-aw/actions/install_copilot_cli.sh" "${ENGINE_VERSION}"` + installerFlags,
			"        env:",
			"          GH_HOST: github.com",
			"          ENGINE_VERSION: " + version,
		}
		stepLines = appendCopilotInstallerVersionEnv(stepLines, compiledVersion, copilotMinVersion, guestLibc)
		return []GitHubActionStep{GitHubActionStep(stepLines)}
	}

	stepLines := []string{
		"      - name: " + stepName,
		"        run: bash \"${RUNNER_TEMP}/gh-aw/actions/install_copilot_cli.sh\" " + version + installerFlags,
		"        env:",
		"          GH_HOST: github.com",
	}
	stepLines = appendCopilotInstallerVersionEnv(stepLines, "", copilotMinVersion, guestLibc)

	return []GitHubActionStep{GitHubActionStep(stepLines)}
}

func appendCopilotInstallerVersionEnv(stepLines []string, compiledVersion, copilotMinVersion, guestLibc string) []string {
	if compiledVersion != "" {
		stepLines = append(stepLines, "          GH_AW_COMPILED_VERSION: "+compiledVersion)
	}
	if copilotMinVersion != "" {
		stepLines = append(stepLines, "          GH_AW_COPILOT_MIN_VERSION: "+copilotMinVersion)
	}
	if guestLibc != "" {
		stepLines = append(stepLines, "          GH_AW_COPILOT_GUEST_LIBC: "+guestLibc)
	}
	return stepLines
}
