// This file provides Copilot engine installation logic.
//
// This file contains functions for generating GitHub Actions steps to install
// the GitHub Copilot CLI and related sandbox infrastructure (AWF or SRT).
//
// Installation includes:
//  1. Secret validation (COPILOT_GITHUB_TOKEN) — runs in the activation job
//  2. Sandbox installation (SRT or AWF, if needed)
//  3. Copilot CLI installation
//
// The installation strategy differs based on sandbox mode:
//   - Standard mode: Global installation using official installer script
//   - SRT mode: Local npm installation for offline compatibility
//   - AWF mode: Global installation + AWF binary

package workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotInstallLog = logger.New("workflow:copilot_engine_installation")

type copilotSDKInstallSpec struct {
	runtimeID string
	stepName  string
	command   string
	// runLines, if non-empty, generates a "run: |" multiline step instead of "run: <command>".
	runLines []string
}

const workspaceCommandPrefix = `cd "${GITHUB_WORKSPACE}" && `
const copilotSDKPythonTargetDir = `${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk/python`
const copilotSDKWebFetchDependency = "undici@6.28.0"

// inlineMavenVersion is the pinned Maven version used to bootstrap Maven for inline Java drivers
// on runners that don't have it pre-installed (e.g. self-hosted). GitHub-hosted runners already
// have Maven, so the bootstrap is a no-op there. The binary is fetched from repo.maven.apache.org
// which is already in the Java ecosystem firewall allowlist.
const inlineMavenVersion = "3.9.9"

// getWorkspaceCommandPrefixFor returns the shell cd prefix for engine command generation.
// When engine.cwd is configured it returns a prefix that changes to ${GH_AW_ENGINE_CWD}
// (set as an env var by applyEngineCwdEnv). When engine.cwd is not configured it falls
// back to the default workspace prefix.
func getWorkspaceCommandPrefixFor(config *EngineConfig) string {
	if config != nil && config.Cwd != "" {
		return `cd "${GH_AW_ENGINE_CWD}" && `
	}
	return workspaceCommandPrefix
}

// GetSecretValidationStep returns the secret validation step for the Copilot engine.
// Returns an empty step if:
//   - permissions.copilot-requests is set to write (uses GitHub Actions token instead), or
//   - COPILOT_PROVIDER_BASE_URL, COPILOT_PROVIDER_API_KEY, or COPILOT_PROVIDER_BEARER_TOKEN is set to a non-empty value in engine.env
//     (BYOK mode — the external provider handles authentication, so COPILOT_GITHUB_TOKEN
//     is not required for model routing).
func (e *CopilotEngine) GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep {
	provider := e.ResolveLLMProvider(workflowData)
	return BuildEngineSecretValidationStep(workflowData, EngineSecretValidationConfig{
		SecretNames: llmProviderSecretNames(provider),
		EngineName:  "GitHub Copilot CLI",
		DocsURL:     llmProviderDocsURL(provider),
		Skip: func(workflowData *WorkflowData) bool {
			if provider == LLMProviderGitHub && hasCopilotRequestsWritePermission(workflowData) {
				copilotInstallLog.Print("Skipping secret validation step: permissions.copilot-requests=write enabled, using GitHub Actions token")
				return true
			}
			if engineEnvHasNonEmptyValue(workflowData, constants.CopilotProviderBaseURL) ||
				engineEnvHasNonEmptyValue(workflowData, constants.CopilotProviderAPIKey) ||
				engineEnvHasNonEmptyValue(workflowData, constants.CopilotProviderBearerToken) {
				copilotInstallLog.Print("Skipping COPILOT_GITHUB_TOKEN validation: BYOK provider credentials are configured")
				return true
			}
			return false
		},
	})
}

// GetSecretFailureMessage returns a Copilot-specific guidance message shown in the agentic
// failure issue when the COPILOT_GITHUB_TOKEN secret validation step fails. The message
// explains the permissions: copilot-requests: write alternative that avoids the need for a
// personal access token when an organization Copilot subscription is available.
//
// When the workflow uses a non-GitHub provider (engine.model-provider: openai or anthropic),
// the copilot-requests: write permission does not apply and an empty string is returned instead.
func (e *CopilotEngine) GetSecretFailureMessage(workflowData *WorkflowData) string {
	if e.ResolveLLMProvider(workflowData) != LLMProviderGitHub {
		return ""
	}
	return "**Alternative**: If your organization has a Copilot subscription, you can avoid the need for a personal access token by adding a top-level `permissions` block to your workflow file. " +
		"This enables Copilot inference through the org using the built-in GitHub Actions token.\n" +
		"\n```yaml\npermissions:\n  copilot-requests: write\n```\n" +
		"\nSee: https://github.github.com/gh-aw/reference/engines/#github-copilot-default"
}

// GetInstallationSteps generates the complete installation workflow for Copilot CLI.
// This includes Node.js setup, sandbox installation (SRT or AWF), and Copilot CLI installation.
// Secret validation is handled separately in the activation job via GetSecretValidationStep.
// The generated steps include Copilot CLI installation and sandbox installation
// (AWF, if needed).
//
// If a custom command is specified in the engine configuration, this function skips
// standard Copilot CLI installation. When firewall is enabled, it still returns AWF
// runtime installation steps required for harness execution.
func (e *CopilotEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	copilotInstallLog.Printf("Generating installation steps for Copilot engine: workflow=%s", workflowData.Name)
	inlineDriverWriteStep := buildInlineCopilotSDKDriverWriteStep(workflowData)
	sdkInstallStep := buildCopilotSDKInstallStep(workflowData)

	// Skip standard Copilot CLI installation if custom command is specified.
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		return getCustomCopilotCommandInstallationSteps(workflowData, inlineDriverWriteStep, sdkInstallStep)
	}

	// Version selection follows a three-level priority:
	//   1. engine.version if explicitly set in the workflow — pass it as a positional arg.
	//   2. compat.json toolcache lookup at runtime — enabled when no explicit version is set;
	//      the script uses GH_AW_COMPILED_VERSION (injected by compiledVersion below) to
	//      select the right compat window and pick the best cached binary.
	//   3. Baked-in DEFAULT_COPILOT_VERSION in the script — final fallback.
	//
	// EngineConfig.Version is intentionally left unset when no explicit engine.version is given.
	// Downstream compile-time lookups (OTel, GH_AW_INFO_VERSION, copilotSupportsNoAskUser, …)
	// already fall back to DefaultCopilotVersion via getVersionForSetup / getInstallationVersion,
	// so no normalization mutation is needed here.
	copilotVersion := "" // empty means "let the script decide via compat/default" (priorities 2 & 3)
	if workflowData.EngineConfig != nil {
		if workflowData.EngineConfig.Version != "" {
			copilotVersion = workflowData.EngineConfig.Version
			copilotInstallLog.Printf("Using engine.version for Copilot CLI installation: %s", copilotVersion)
		} else {
			copilotInstallLog.Printf("No engine.version specified; script will resolve via compat.json or baked-in default")
		}
	}

	// Use the installer script for global installation
	copilotInstallLog.Print("Using new installer script for Copilot installation")
	// On ARC/DinD runners, sudo may not be available (allowPrivilegeEscalation: false).
	// Pass --rootless so the script installs to ~/.local/bin without sudo.
	// The "Copy Copilot CLI to daemon-visible path" step in nodejs.go then copies from
	// the rootless location to ${RUNNER_TEMP}/gh-aw/bin/copilot where AWF expects it.
	rootless := isArcDindTopology(workflowData)
	compiledVersion := workflowData.CompiledVersion
	npmSteps := GenerateCopilotInstallerSteps(copilotVersion, "Install GitHub Copilot CLI", rootless, compiledVersion)
	if len(inlineDriverWriteStep) > 0 {
		npmSteps = append(npmSteps, inlineDriverWriteStep)
	}
	if len(sdkInstallStep) > 0 {
		npmSteps = append(npmSteps, sdkInstallStep)
	}
	steps := BuildNpmEngineInstallStepsWithAWF(npmSteps, workflowData)

	return appendCopilotLSPInstallSteps(steps, workflowData)
}

func getCustomCopilotCommandInstallationSteps(workflowData *WorkflowData, inlineDriverWriteStep, sdkInstallStep GitHubActionStep) []GitHubActionStep {
	var steps []GitHubActionStep
	if len(inlineDriverWriteStep) > 0 {
		steps = append(steps, inlineDriverWriteStep)
	}
	if len(sdkInstallStep) > 0 {
		steps = append(steps, sdkInstallStep)
	}

	if isFirewallEnabled(workflowData) {
		copilotInstallLog.Printf("Skipping Copilot CLI installation: custom command specified (%s); keeping AWF runtime installation because firewall is enabled", workflowData.EngineConfig.Command)
		return appendCopilotLSPInstallSteps(buildNpmEngineInstallStepsWithAWF(steps, workflowData, false), workflowData)
	}
	if len(sdkInstallStep) > 0 {
		copilotInstallLog.Printf("Skipping Copilot CLI installation: custom command specified (%s); keeping Copilot SDK install step", workflowData.EngineConfig.Command)
	} else {
		copilotInstallLog.Printf("Skipping installation steps: custom command specified (%s)", workflowData.EngineConfig.Command)
	}
	return appendCopilotLSPInstallSteps(steps, workflowData)
}

func appendCopilotLSPInstallSteps(steps []GitHubActionStep, workflowData *WorkflowData) []GitHubActionStep {
	if workflowData == nil {
		return steps
	}
	manager := NewLSPManager(workflowData.LSP)
	lspSteps := manager.GenerateInstallSteps(workflowData)
	if len(lspSteps) == 0 {
		return steps
	}
	copilotInstallLog.Printf("Adding %d LSP dependency installation step(s)", len(lspSteps))
	return append(steps, lspSteps...)
}

func buildCopilotSDKInstallStep(workflowData *WorkflowData) GitHubActionStep {
	if workflowData == nil || workflowData.EngineConfig == nil || !workflowData.EngineConfig.CopilotSDK {
		return GitHubActionStep{}
	}
	if inlineRuntimeID := copilotSDKInlineDriverRuntimeID(workflowData); inlineRuntimeID != "" {
		spec := getInlineCopilotSDKInstallSpec(inlineRuntimeID)
		copilotInstallLog.Printf("copilot-sdk enabled with inline driver; runtime=%s; install command=%s", spec.runtimeID, spec.command)
		return specToInstallStep(spec)
	}
	// When a custom SDK driver is configured without a custom engine command, use the driver's
	// file extension to determine which language SDK to install. This ensures the correct SDK
	// package manager command is generated (e.g., pip for .py drivers, ruby/gem for .rb drivers).
	command := workflowData.EngineConfig.Command
	if command == "" && workflowData.EngineConfig.Driver != "" {
		command = sdkDriverInstallCommand(workflowData.EngineConfig.Driver)
	}
	spec := getCopilotSDKInstallSpec(command)
	copilotInstallLog.Printf("copilot-sdk enabled; runtime=%s; install command=%s", spec.runtimeID, spec.command)
	return specToInstallStep(spec)
}

// specToInstallStep converts a copilotSDKInstallSpec into a GitHubActionStep.
// When the spec has runLines set it emits a "run: |" multi-line block; otherwise
// it emits a single "run: <command>" line.
func specToInstallStep(spec copilotSDKInstallSpec) GitHubActionStep {
	if len(spec.runLines) > 0 {
		step := GitHubActionStep{
			"      - name: " + spec.stepName,
			"        run: |",
		}
		for _, line := range spec.runLines {
			step = append(step, "          "+line)
		}
		return step
	}
	return GitHubActionStep{
		"      - name: " + spec.stepName,
		"        run: " + spec.command,
	}
}

// sdkDriverInstallCommand returns a synthetic command string for the given driver filename
// that can be passed to getCopilotSDKInstallSpec/detectRuntimeFromCopilotCommand to select
// the correct SDK package manager. Python and Ruby extensions need special handling;
// JS, TypeScript, and arbitrary commands (no extension) fall back to the Node.js default.
// TypeScript uses Node.js native support (Node 24+) so no extra toolchain install is needed.
func sdkDriverInstallCommand(driverName string) string {
	ext := strings.ToLower(filepath.Ext(driverName))
	switch ext {
	case ".py":
		return "python3 " + driverName
	case ".rb":
		return "ruby " + driverName
	default:
		// .js/.cjs/.mjs, .ts/.mts, and no-extension (arbitrary commands) default to Node.js.
		return ""
	}
}

func getCopilotSDKInstallSpec(command string) copilotSDKInstallSpec {
	runtimeID := detectRuntimeFromCopilotCommand(command)
	version := string(constants.DefaultCopilotSDKVersion)

	spec := copilotSDKInstallSpec{
		runtimeID: runtimeID,
		stepName:  "Install GitHub Copilot SDK (Node.js)",
		command:   workspaceCommandPrefix + "npm install --ignore-scripts --no-save @github/copilot-sdk@" + version + " " + copilotSDKWebFetchDependency,
	}

	switch runtimeID {
	case "python":
		spec.stepName = "Install GitHub Copilot SDK (Python)"
		spec.command = workspaceCommandPrefix + fmt.Sprintf(
			`mkdir -p "%[1]s" && python3 -m pip install --disable-pip-version-check --target "%[1]s" github-copilot-sdk==%[2]s`,
			copilotSDKPythonTargetDir,
			version,
		)
	case "typescript":
		spec.stepName = "Install GitHub Copilot SDK (TypeScript)"
		spec.command = workspaceCommandPrefix + "npm install --ignore-scripts --no-save @github/copilot-sdk@" + version + " " + copilotSDKWebFetchDependency + " ts-node typescript"
	case "go":
		spec.stepName = "Install GitHub Copilot SDK (Go)"
		spec.command = workspaceCommandPrefix + "go get github.com/github/copilot-sdk/go@v" + version
	case "rust":
		spec.stepName = "Install GitHub Copilot SDK (Rust)"
		spec.command = workspaceCommandPrefix + "cargo add github-copilot-sdk@" + version
	case "dotnet":
		spec.stepName = "Install GitHub Copilot SDK (.NET)"
		spec.command = workspaceCommandPrefix + "dotnet add package GitHub.Copilot.SDK --version " + version
	case "java":
		spec.stepName = "Install GitHub Copilot SDK (Java)"
		spec.command = workspaceCommandPrefix + "mvn -q org.apache.maven.plugins:maven-dependency-plugin:3.8.1:get -Dartifact=com.github:copilot-sdk-java:" + version
	}

	return spec
}

func getInlineCopilotSDKInstallSpec(runtimeID string) copilotSDKInstallSpec {
	version := string(constants.DefaultCopilotSDKVersion)

	spec := copilotSDKInstallSpec{
		runtimeID: runtimeID,
		stepName:  "Install GitHub Copilot SDK (Node.js)",
		command:   workspaceCommandPrefix + "npm install --ignore-scripts --no-save @github/copilot-sdk@" + version + " " + copilotSDKWebFetchDependency,
	}

	switch runtimeID {
	case "python":
		spec.stepName = "Install GitHub Copilot SDK (Python)"
		spec.command = workspaceCommandPrefix + fmt.Sprintf(
			`mkdir -p "%[1]s" && python3 -m pip install --disable-pip-version-check --target "%[1]s" github-copilot-sdk==%[2]s`,
			copilotSDKPythonTargetDir,
			version,
		)
	case "go":
		spec.stepName = "Install GitHub Copilot SDK (Go)"
		// Fetch the SDK and compile the driver to a binary in one step.
		// Using a pre-compiled binary eliminates per-invocation `go run` recompilation
		// and removes the Go toolchain requirement from the agent's runtime path.
		goSrcFile := inlineCopilotSDKDriverGoPath[strings.LastIndex(inlineCopilotSDKDriverGoPath, "/")+1:]
		goBinFile := inlineCopilotSDKDriverGoBinPath[strings.LastIndex(inlineCopilotSDKDriverGoBinPath, "/")+1:]
		spec.command = fmt.Sprintf(
			`mkdir -p "${GITHUB_WORKSPACE}/%[1]s" && cd "${GITHUB_WORKSPACE}/%[1]s" && go get github.com/github/copilot-sdk/go@v%[2]s && go build -o "%[4]s" "./%[3]s"`,
			inlineCopilotSDKDriverDir,
			version,
			goSrcFile,
			goBinFile,
		)
	case "java":
		spec.stepName = "Install GitHub Copilot SDK (Java)"
		classpathFile := inlineCopilotSDKDriverJavaClassPath[strings.LastIndex(inlineCopilotSDKDriverJavaClassPath, "/")+1:]
		spec.runLines = []string{
			`# Bootstrap Maven if not already available (e.g. self-hosted runners).`,
			`# GitHub-hosted runners have Maven pre-installed; this is a no-op there.`,
			`if ! command -v mvn >/dev/null 2>&1; then`,
			`  MAVEN_HOME="${RUNNER_TEMP:-/tmp}/apache-maven-` + inlineMavenVersion + `"`,
			`  if [ ! -d "${MAVEN_HOME}" ]; then`,
			`    curl -fsSL "https://repo.maven.apache.org/maven2/org/apache/maven/apache-maven/` + inlineMavenVersion + `/apache-maven-` + inlineMavenVersion + `-bin.tar.gz" \`,
			`      | tar -xzf - -C "${RUNNER_TEMP:-/tmp}"`,
			`  fi`,
			`  export PATH="${MAVEN_HOME}/bin:${PATH}"`,
			`fi`,
			fmt.Sprintf(`cd "${GITHUB_WORKSPACE}/%s" && mvn -q dependency:build-classpath -Dmdep.outputFile="%s"`,
				inlineCopilotSDKDriverDir,
				classpathFile,
			),
		}
	}

	return spec
}

func detectRuntimeFromCopilotCommand(command string) string {
	token := firstCommandToken(command)
	if token == "" {
		return "node"
	}

	runtime, found := commandToRuntime[token]
	if found && runtime.ID != "" {
		return runtime.ID
	}

	switch token {
	case "ts-node":
		return "typescript"
	case "cargo", "rustc":
		return "rust"
	case "mvnw":
		return "java"
	}
	return "node"
}

func firstCommandToken(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	token := normalizeCommandToken(fields[0])
	if token != "env" {
		return token
	}
	// Shell-form commands sometimes start with `env` wrappers:
	//   env FOO=bar python app.py
	// Skip env assignments/flags and return the first executable token.
	for _, field := range fields[1:] {
		if strings.Contains(field, "=") || strings.HasPrefix(field, "-") {
			continue
		}
		return normalizeCommandToken(field)
	}
	return ""
}

func normalizeCommandToken(token string) string {
	trimmed := strings.Trim(token, `"'`)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(filepath.Base(trimmed))
}

// generateAWFInstallationStep creates a GitHub Actions step to install the AWF binary
// with SHA256 checksum verification to protect against supply chain attacks.
//
// The installation logic is implemented in a separate shell script (install_awf_binary.sh)
// which downloads the binary directly from GitHub releases, verifies its checksum against
// the official checksums.txt file, and installs it. This approach:
// - Eliminates trust in the installer script itself
// - Provides full transparency of the installation process
// - Protects against tampered or compromised installer scripts
// - Verifies the binary integrity before execution
//
// If a custom command is specified in the agent config, the installation is skipped
// as the custom command replaces the AWF binary.
func generateAWFInstallationStep(version string, agentConfig *AgentSandboxConfig) GitHubActionStep {
	// If custom command is specified, skip installation (command replaces binary)
	if agentConfig != nil && agentConfig.Command != "" {
		copilotInstallLog.Print("Skipping AWF binary installation (custom command specified)")
		// Return empty step - custom command will be used in execution
		return GitHubActionStep([]string{})
	}

	// Use default version for logging when not specified
	if version == "" {
		version = string(constants.DefaultFirewallVersion)
	}

	installCmd := "bash \"${RUNNER_TEMP}/gh-aw/actions/install_awf_binary.sh\" " + version
	// Rootless runtime profiles run AWF as the runner user: pass --rootless so the
	// install script installs into $HOME/.local/{bin,lib/awf} (always writable, even on
	// standard GitHub-hosted runners where /usr/local is root-owned) and exports
	// $GITHUB_PATH so the bare awf invocation in later steps resolves correctly.
	//
	// Exceptions: the docker-sudo-iptables and cloud-hypervisor profiles use privileged
	// AWF invocations, so the binary must be installed to /usr/local/bin to be on
	// sudo's secure_path.
	if agentConfig != nil && !agentConfig.Disabled && resolveSandboxRuntimeProfile(agentConfig).Rootless {
		installCmd += " --rootless"
	}

	stepLines := []string{
		"      - name: Install AWF binary",
		"        run: " + installCmd,
	}

	return GitHubActionStep(stepLines)
}

// generateDockerComposeInstallStep creates a step that installs the Docker Compose
// CLI plugin. ARC/DinD runners may not have Docker Compose pre-installed, but AWF
// requires it to orchestrate the squid-proxy, agent, and api-proxy containers.
func generateDockerComposeInstallStep() GitHubActionStep {
	return GitHubActionStep([]string{
		"      - name: Install Docker Compose plugin",
		"        run: |",
		`          export DOCKER_CONFIG="${DOCKER_CONFIG:-$HOME/.docker}"`,
		`          mkdir -p "$DOCKER_CONFIG/cli-plugins"`,
		`          arch="$(uname -m)"`,
		`          case "$arch" in x86_64|amd64) arch="x86_64" ;; aarch64|arm64) arch="aarch64" ;; *) echo "Unsupported architecture for docker compose plugin: $arch" >&2; exit 1 ;; esac`,
		`          curl -fsSL "https://github.com/docker/compose/releases/download/v2.36.2/docker-compose-linux-$arch" -o "$DOCKER_CONFIG/cli-plugins/docker-compose"`,
		`          chmod +x "$DOCKER_CONFIG/cli-plugins/docker-compose"`,
		`          docker compose version`,
	})
}

// generateGVisorInstallStep creates a GitHub Actions step that downloads, installs,
// and verifies the gVisor (runsc) container runtime. This step must run BEFORE the
// AWF invocation step so that Docker can start the agent container under runsc.
//
// Key implementation notes:
//   - Pins to constants.DefaultGVisorVersion rather than "latest" for reproducible,
//     verifiable installs. Each binary is verified against its official SHA-512 file
//     before being installed with root privileges, matching the pattern used by the
//     adjacent AWF installer.
//   - Uses uname -m directly (x86_64, aarch64) — gVisor download URLs use raw arch names.
//   - Restarts Docker with `systemctl restart` (NOT reload): Docker's SIGHUP reload does
//     not call setHostGatewayIP(), which breaks --add-host host.docker.internal:host-gateway.
//   - Downloads both runsc and containerd-shim-runsc-v1; the shim is required for Docker's
//     containerd integration.
//   - Script source: actions/setup/sh/sudo_gvisor_install.sh (requires sudo).
func generateGVisorInstallStep() GitHubActionStep {
	version := constants.DefaultGVisorVersion
	return GitHubActionStep([]string{
		"      - name: Install gVisor (runsc)",
		"        # runner-guard:ignore RGS-012 -- pinned release, SHA-512 verified artifacts, download-only step (no outbound secret transmission).",
		`        run: bash "${RUNNER_TEMP}/gh-aw/actions/sudo_gvisor_install.sh" ` + version,
	})
}
