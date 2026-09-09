package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var nodejsLog = logger.New("workflow:nodejs")

const npmDefaultCooldownDays = 3

// NPMInstallOptions configures generated npm installation steps.
type NPMInstallOptions struct {
	IncludeNodeSetup  bool
	IsGlobal          bool
	RunInstallScripts bool
	CooldownEnabled   bool
}

// GenerateNodeJsSetupStep creates a GitHub Actions step for setting up Node.js
// Returns a step that installs Node.js using the default version from constants.DefaultNodeVersion
// Caching is disabled by default to prevent cache poisoning vulnerabilities in release workflows
func GenerateNodeJsSetupStep() GitHubActionStep {
	return GitHubActionStep{
		"      - name: Setup Node.js",
		"        uses: " + getActionPin("actions/setup-node"),
		"        with:",
		fmt.Sprintf("          node-version: '%s'", constants.DefaultNodeVersion),
		"          package-manager-cache: false",
	}
}

// installStepsContainNodeSetup reports whether any of the provided steps is already
// a "Setup Node.js" step. Uses the same extractStepName matcher as
// JobManager.ValidateDuplicateSteps so the guard cannot drift from what the
// validator would flag as a duplicate.
func installStepsContainNodeSetup(steps []GitHubActionStep) bool {
	for _, step := range steps {
		if extractStepName(strings.Join(step, "\n")) == "Setup Node.js" {
			return true
		}
	}
	return false
}

// By default, --ignore-scripts is added to the install command to prevent pre/post install
// scripts from executing (supply chain security). Pass runInstallScripts=true to allow scripts.
// By default, a 3-day npm dependency cooldown is enabled via NPM_CONFIG_MIN_RELEASE_AGE.
// Pass cooldownEnabled=false to disable it.
// Parameters:
//   - packageName: The npm package name (e.g., "@anthropic-ai/claude-code")
//   - version: The package version to install
//   - stepName: The name to display for the install step (e.g., "Install Claude Code CLI")
//   - cacheKeyPrefix: The prefix for the cache key (unused, kept for API compatibility)
//   - options.IncludeNodeSetup: If true, includes Node.js setup step before npm install
//   - options.RunInstallScripts: If true, allow pre/post install scripts (omits --ignore-scripts)
//   - options.CooldownEnabled: If true, apply a default 3-day npm release-age cooldown
//
// Returns steps for installing the npm package (optionally with Node.js setup)
func GenerateNpmInstallSteps(packageName, version, stepName, cacheKeyPrefix string, options NPMInstallOptions) []GitHubActionStep {
	scopeOptions := options
	scopeOptions.IsGlobal = true
	return GenerateNpmInstallStepsWithScope(packageName, version, stepName, cacheKeyPrefix, scopeOptions)
}

// BuildStandardNpmEngineInstallStepsNoCooldown creates standard npm installation
// steps for engines while forcing the default npm release-age cooldown off.
func BuildStandardNpmEngineInstallStepsNoCooldown(
	packageName string,
	defaultVersion string,
	stepName string,
	cacheKeyPrefix string,
	workflowData *WorkflowData,
) []GitHubActionStep {
	return buildStandardNpmEngineInstallSteps(packageName, defaultVersion, stepName, cacheKeyPrefix, workflowData, false)
}

func buildStandardNpmEngineInstallSteps(
	packageName string,
	defaultVersion string,
	stepName string,
	cacheKeyPrefix string,
	workflowData *WorkflowData,
	cooldownEnabled bool,
) []GitHubActionStep {
	nodejsLog.Printf("Building npm engine install steps: package=%s, version=%s", packageName, defaultVersion)

	// Use version from engine config if provided, otherwise default to pinned version
	version := defaultVersion
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
		version = workflowData.EngineConfig.Version
		nodejsLog.Printf("Using engine config version: %s", version)
	}

	// Add npm package installation steps (includes Node.js setup)
	// Always pass false for runInstallScripts: engine CLI installs must never run
	// pre/post install scripts regardless of the workflow's run-install-scripts setting.
	// This is a supply chain security requirement for the engine binary itself.
	return GenerateNpmInstallSteps(
		packageName,
		version,
		stepName,
		cacheKeyPrefix,
		NPMInstallOptions{
			IncludeNodeSetup:  true,
			RunInstallScripts: false,
			CooldownEnabled:   cooldownEnabled,
		},
	)
}

// BuildNpmEngineInstallStepsWithAWF injects an AWF installation step between the Node.js
// setup step and the CLI install steps when the firewall is enabled. This eliminates the
// duplicated AWF-injection pattern shared by Claude, Gemini, and Copilot engines.
//
// The expected layout of npmSteps is:
//   - npmSteps[0]  – Node.js setup step
//   - npmSteps[1:] – CLI installation step(s)
//
// Parameters:
//   - npmSteps: Pre-computed npm installation steps (from BuildStandardNpmEngineInstallSteps
//     or GenerateCopilotInstallerSteps)
//   - workflowData: The workflow data (used to determine firewall configuration)
//
// Returns:
//   - []GitHubActionStep: Steps in order: Node.js setup, AWF (if enabled), CLI install
func BuildNpmEngineInstallStepsWithAWF(npmSteps []GitHubActionStep, workflowData *WorkflowData) []GitHubActionStep {
	return buildNpmEngineInstallStepsWithAWF(npmSteps, workflowData, true)
}

func buildNpmEngineInstallStepsWithAWF(npmSteps []GitHubActionStep, workflowData *WorkflowData, stageCopilotCLI bool) []GitHubActionStep {
	var steps []GitHubActionStep

	if len(npmSteps) > 0 {
		steps = append(steps, npmSteps[0]) // Node.js setup step
	}

	steps = appendAWFInstallationSteps(steps, workflowData)

	if len(npmSteps) > 1 {
		steps = append(steps, npmSteps[1:]...) // CLI installation and subsequent steps
	}

	// Copy Copilot CLI to daemon-visible path for ARC/DinD.
	// With --rootless, the binary is at ~/.local/bin/copilot; otherwise /usr/local/bin/copilot.
	// On ARC/DinD, the AWF command references ${RUNNER_TEMP}/gh-aw/bin/copilot which is
	// daemon-visible, so we copy from wherever the install script placed it.
	if stageCopilotCLI && isFirewallEnabled(workflowData) && isArcDindTopology(workflowData) {
		copyStep := GitHubActionStep([]string{
			"      - name: Copy Copilot CLI to daemon-visible path",
			"        run: |",
			"          mkdir -p \"${RUNNER_TEMP}/gh-aw/bin\"",
			`          COPILOT_SRC="$(command -v copilot)"`,
			"          cp \"$COPILOT_SRC\" \"${RUNNER_TEMP}/gh-aw/bin/copilot\"",
			"          chmod +x \"${RUNNER_TEMP}/gh-aw/bin/copilot\"",
		})
		steps = append(steps, copyStep)
	}

	return steps
}

func appendAWFInstallationSteps(steps []GitHubActionStep, workflowData *WorkflowData) []GitHubActionStep {
	if !isFirewallEnabled(workflowData) {
		return steps
	}

	firewallConfig := getFirewallConfig(workflowData)
	agentConfig := getAgentConfig(workflowData)
	awfVersion := ""
	if firewallConfig != nil {
		awfVersion = firewallConfig.Version
	}

	// gVisor must be installed and registered BEFORE AWF starts the agent container.
	if isGVisorRuntime(workflowData) && isRuntimeInstallEnabled(workflowData) {
		steps = append(steps, generateGVisorInstallStep())
	}

	// docker-sbx must be installed, authenticated, and smoke-tested BEFORE AWF
	// starts so the microVM runtime is ready when AWF launches the agent.
	if isDockerSbxRuntime(workflowData) && isRuntimeInstallEnabled(workflowData) {
		steps = append(steps, generateDockerSbxKVMCheckStep())
		steps = append(steps, generateDockerSbxSecretsCheckStep())
		steps = append(steps, generateDockerSbxInstallStep())
		steps = append(steps, generateDockerSbxAuthAndDaemonStep())
		steps = append(steps, generateDockerSbxPreFlightStep())
	}
	if isCloudHypervisorRuntime(workflowData) {
		steps = append(steps, generateCloudHypervisorKVMAccessStep())
		steps = append(steps, generateCloudHypervisorHostPreflightStep())
		steps = append(steps, generateCloudHypervisorBundleSetupStep(getAWFVersionForSetup(workflowData)))
	}

	if awfInstall := generateAWFInstallationStep(awfVersion, agentConfig); len(awfInstall) > 0 {
		steps = append(steps, awfInstall)
	}

	// Install Docker Compose plugin for ARC/DinD runners where it may not be pre-installed.
	if isArcDindTopology(workflowData) {
		steps = append(steps, generateDockerComposeInstallStep())
	}
	return steps
}

// GetNpmBinPathSetup returns a simple shell command that adds hostedtoolcache bin directories
// to PATH. This is specifically for npm-installed CLIs (like Claude, Codex, and the Copilot
// driver) that need to find their binaries installed via `npm install -g` or via
// `actions/setup-node`.
//
// Unlike GetHostedToolcachePathSetup(), this does NOT use GH_AW_TOOL_BINS because AWF's
// native chroot mode already handles tool-specific paths (GOROOT, JAVA_HOME, etc.) via
// AWF_HOST_PATH and the entrypoint.sh script. This function only adds the generic
// hostedtoolcache bin directories for npm packages.
//
// RUNNER_TOOL_CACHE is required because the Actions runner populates it from the
// runner tool_cache context. The generated command does not guess fallback paths.
//
// Returns:
//   - string: A shell command that exports PATH with hostedtoolcache bin directories appended
func GetNpmBinPathSetup() string {
	// Find all bin directories in hostedtoolcache (Node.js, Python, etc.)
	// This finds paths like /opt/hostedtoolcache/node/22.13.0/x64/bin
	// or whatever path the runner exposes through RUNNER_TOOL_CACHE.
	//
	// After the find, re-prepend GOROOT/bin if set. The find returns directories
	// alphabetically, so go/1.23.12 shadows go/1.25.0. Re-prepending GOROOT/bin
	// ensures the Go version set by actions/setup-go takes precedence.
	// AWF's entrypoint.sh exports GOROOT before the user command runs.
	//
	// Re-prepend ERLANG_HOME/bin if set. erlef/setup-beam installs OTP to
	// ${RUNNER_TEMP}/.setup-beam/otp/ via core.addPath(), which is not under
	// RUNNER_TOOL_CACHE and therefore missed by the find above. ERLANG_HOME is
	// captured by the elixir runtime capture step and exported to GITHUB_ENV,
	// making it available inside the AWF container. Without this, mix commands
	// fail because Elixir tries to exec erl which is not found via the find.
	return `: "${RUNNER_TOOL_CACHE:?RUNNER_TOOL_CACHE must be set}"; GH_AW_TOOL_CACHE="$RUNNER_TOOL_CACHE"; GH_AW_TOOL_BINS="$(find "$GH_AW_TOOL_CACHE" -maxdepth 5 -type d -name bin 2>/dev/null | tr '\n' ':')"; GH_AW_TOOL_BINS="${GH_AW_TOOL_BINS%:}"; export PATH="$PATH${GH_AW_TOOL_BINS:+:}$GH_AW_TOOL_BINS"; [ -n "$GOROOT" ] && export PATH="$GOROOT/bin:$PATH" || true; [ -n "$ERLANG_HOME" ] && export PATH="$ERLANG_HOME/bin:$PATH" || true`
}

// GenerateDockerSbxNpmCLIInstallStep installs an npm CLI into a runner path that is
// visible inside microVM runtimes (docker-sbx/cloud-hypervisor), then creates a
// stable bin/ symlink from
// ${RUNNER_TEMP}/gh-aw/engine-cli/bin/<command> to the package's node_modules/.bin entry.
func GenerateDockerSbxNpmCLIInstallStep(packageName, version, stepName, commandName string, runInstallScripts bool, cooldownEnabled bool) GitHubActionStep {
	ignoreScriptsFlag := "--ignore-scripts "
	if runInstallScripts {
		ignoreScriptsFlag = ""
	}

	var installStep GitHubActionStep
	if ExpressionPattern.MatchString(version) {
		installStep = GitHubActionStep{
			"      - name: " + stepName,
			"        run: |",
			"          mkdir -p \"${RUNNER_TEMP}/gh-aw/engine-cli/bin\"",
			fmt.Sprintf(`          npm install %s--prefix "${RUNNER_TEMP}/gh-aw/engine-cli" %s@"${ENGINE_VERSION}"`, ignoreScriptsFlag, packageName),
			fmt.Sprintf(`          ln -sf "../node_modules/.bin/%s" "${RUNNER_TEMP}/gh-aw/engine-cli/bin/%s"`, commandName, commandName),
			"        env:",
			"          ENGINE_VERSION: " + version,
		}
		if cooldownEnabled {
			installStep = append(installStep, fmt.Sprintf("          NPM_CONFIG_MIN_RELEASE_AGE: '%d'", npmDefaultCooldownDays))
		}
		return installStep
	}

	installStep = GitHubActionStep{
		"      - name: " + stepName,
		"        run: |",
		"          mkdir -p \"${RUNNER_TEMP}/gh-aw/engine-cli/bin\"",
		fmt.Sprintf(`          npm install %s--prefix "${RUNNER_TEMP}/gh-aw/engine-cli" %s@%s`, ignoreScriptsFlag, packageName, version),
		fmt.Sprintf(`          ln -sf "../node_modules/.bin/%s" "${RUNNER_TEMP}/gh-aw/engine-cli/bin/%s"`, commandName, commandName),
	}
	if cooldownEnabled {
		installStep = append(installStep,
			"        env:",
			fmt.Sprintf("          NPM_CONFIG_MIN_RELEASE_AGE: '%d'", npmDefaultCooldownDays),
		)
	}
	return installStep
}

// GetDockerSbxNpmCLIPathSetup returns the PATH export needed for npm CLIs that were
// staged into ${RUNNER_TEMP}/gh-aw/engine-cli/bin for microVM runs.
func GetDockerSbxNpmCLIPathSetup(workflowData *WorkflowData) string {
	if !isDockerSbxRuntime(workflowData) && !isCloudHypervisorRuntime(workflowData) {
		return ""
	}
	return `export PATH="${RUNNER_TEMP}/gh-aw/engine-cli/bin:$PATH"`
}

// GenerateNpmInstallStepsWithScope generates npm installation steps with control over global vs local installation.
// By default, --ignore-scripts is added to the install command to prevent pre/post install
// scripts from executing (supply chain security). Pass options.RunInstallScripts=true to allow scripts.
func GenerateNpmInstallStepsWithScope(packageName, version, stepName, cacheKeyPrefix string, options NPMInstallOptions) []GitHubActionStep {
	nodejsLog.Printf("Generating npm install steps: package=%s, version=%s, includeNodeSetup=%v, isGlobal=%v, runInstallScripts=%v", packageName, version, options.IncludeNodeSetup, options.IsGlobal, options.RunInstallScripts)

	var steps []GitHubActionStep

	// Add Node.js setup if requested
	if options.IncludeNodeSetup {
		nodejsLog.Print("Including Node.js setup step")
		steps = append(steps, GenerateNodeJsSetupStep())
	}

	// Add npm install step
	globalFlag := ""
	if options.IsGlobal {
		globalFlag = "-g "
	}

	// Add --ignore-scripts by default to prevent pre/post install scripts (supply chain security).
	// runInstallScripts=true disables this protection (emits a warning at compile time).
	ignoreScriptsFlag := "--ignore-scripts "
	if options.RunInstallScripts {
		ignoreScriptsFlag = ""
	}

	var installStep GitHubActionStep
	if ExpressionPattern.MatchString(version) {
		// Version is a GitHub Actions expression (e.g. ${{ inputs.engine-version }}).
		// Pass it via an env var instead of direct shell interpolation to prevent injection:
		// if the expression evaluates to a malicious string, it would otherwise be
		// substituted verbatim into the shell command before the shell parses it.
		nodejsLog.Printf("Version contains GitHub Actions expression, using env var for injection safety: %s", version)
		installCmd := fmt.Sprintf(`npm install %s%s%s@"${ENGINE_VERSION}"`, ignoreScriptsFlag, globalFlag, packageName)
		installStep = GitHubActionStep{
			"      - name: " + stepName,
			"        run: " + installCmd,
			"        env:",
			"          ENGINE_VERSION: " + version,
		}
		if options.CooldownEnabled {
			installStep = append(installStep, fmt.Sprintf("          NPM_CONFIG_MIN_RELEASE_AGE: '%d'", npmDefaultCooldownDays))
		}
	} else {
		installCmd := fmt.Sprintf("npm install %s%s%s@%s", ignoreScriptsFlag, globalFlag, packageName, version)
		installStep = GitHubActionStep{
			"      - name: " + stepName,
			"        run: " + installCmd,
		}
		if options.CooldownEnabled {
			installStep = append(installStep,
				"        env:",
				fmt.Sprintf("          NPM_CONFIG_MIN_RELEASE_AGE: '%d'", npmDefaultCooldownDays),
			)
		}
	}
	steps = append(steps, installStep)

	return steps
}

// resolveRuntimeCooldown returns whether runtime-associated dependency installs should apply
// the default release-age cooldown. Defaults to true; runtimes.<id>.cooldown: false disables it.
func resolveRuntimeCooldown(workflowData *WorkflowData, runtimeID string) bool {
	if workflowData == nil {
		return true
	}

	if workflowData.ParsedFrontmatter != nil && workflowData.ParsedFrontmatter.RuntimesTyped != nil {
		var runtimeConfig *RuntimeConfig
		switch runtimeID {
		case "node":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Node
		case "python":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Python
		case "go":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Go
		case "uv":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.UV
		case "bun":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Bun
		case "deno":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Deno
		case "dotnet":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Dotnet
		case "elixir":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Elixir
		case "gh-aw":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.GhAw
		case "haskell":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Haskell
		case "java":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Java
		case "ruby":
			runtimeConfig = workflowData.ParsedFrontmatter.RuntimesTyped.Ruby
		}
		if runtimeConfig != nil && runtimeConfig.Cooldown != nil {
			return *runtimeConfig.Cooldown
		}
	}

	runtimeAny, ok := workflowData.Runtimes[runtimeID]
	if !ok {
		return true
	}
	runtimeMap, ok := runtimeAny.(map[string]any)
	if !ok {
		return true
	}
	cooldown, ok := runtimeMap["cooldown"].(bool)
	if !ok {
		return true
	}
	return cooldown
}
