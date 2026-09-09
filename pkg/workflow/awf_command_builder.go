// This file contains AWF command and argument assembly helpers.

package workflow

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

// BuildAWFCommand builds a complete AWF command with all arguments.
// This consolidates the AWF command building logic that was duplicated across
// Copilot, Claude, and Codex engines.
//
// Parameters:
//   - config: AWF command configuration
//
// Returns:
//   - string: Complete AWF command with arguments and wrapped engine command
func BuildAWFCommand(config AWFCommandConfig) string {
	awfHelpersLog.Printf("Building AWF command for engine: %s", config.EngineName)
	isArcDind := isArcDindTopology(config.WorkflowData)
	awfCommand := GetAWFCommandPrefix(config.WorkflowData)
	awfArgs := BuildAWFArgs(config)
	firewallConfig := getFirewallConfig(config.WorkflowData)
	isCloudHypervisor := isCloudHypervisorRuntime(config.WorkflowData)
	arcDindPrefixProbe, arcDindDockerHostProbe, arcDindDockerHostRef := buildArcDindDockerHostSettings(config, firewallConfig)
	toolCacheMountProbe, toolCacheMountRef := buildToolCacheMountSettings(isCloudHypervisor)
	var expandableArgs string
	expandableArgs, arcDindDockerHostProbe = buildExpandableAWFArgs(config, isCloudHypervisor, isArcDind, arcDindDockerHostProbe)
	var configFileSetup string
	awfConfigJSON, err := BuildAWFConfigJSON(config)
	if err != nil {
		awfHelpersLog.Printf("Warning: failed to build AWF config JSON: %v", err)
	} else {
		configFileSetup = buildAWFConfigFileSetup(config, awfConfigJSON)
		expandableArgs = fmt.Sprintf("--config %q ", awfConfigRuntimePathExpr) + expandableArgs
		awfHelpersLog.Print("Using AWF config file (--config flag)")
	}
	modelsJSONPathExport := buildModelsJSONPathExportScript(isArcDind)
	engineCommand := rewriteEngineCommandForRuntime(config.EngineCommand, isArcDind)
	shellWrappedCommand := WrapCommandInShell(engineCommand)
	preCreateLog := fmt.Sprintf("(umask 177 && touch %s)", shellEscapeArg(config.LogFile))
	writeAgentCLIStartMs := "printf '%s' \"$(date +%s%3N)\" > " + shellEscapeArg(AgentCLIStartMsPath)
	command := buildAWFCommandScript(buildAWFCommandScriptInput{
		writeAgentCLIStartMs:   writeAgentCLIStartMs,
		pathSetup:              config.PathSetup,
		preCreateLog:           preCreateLog,
		configFileSetup:        configFileSetup,
		modelsJSONPathExport:   modelsJSONPathExport,
		arcDindDockerHostProbe: arcDindDockerHostProbe,
		arcDindPrefixProbe:     arcDindPrefixProbe,
		toolCacheMountProbe:    toolCacheMountProbe,
		awfCommand:             awfCommand,
		expandableArgs:         expandableArgs,
		toolCacheMountRef:      toolCacheMountRef,
		arcDindDockerHostRef:   arcDindDockerHostRef,
		awfArgs:                awfArgs,
		shellWrappedCommand:    shellWrappedCommand,
		logFile:                config.LogFile,
		engineName:             config.EngineName,
		retryStartupFailures:   config.RetryStartupFailures || usesBuiltInEngineHarness(config.EngineName, config.EngineCommand),
	})

	awfHelpersLog.Print("Successfully built AWF command")
	return command
}

func usesBuiltInEngineHarness(engineName, engineCommand string) bool {
	switch engineName {
	case "claude":
		return strings.Contains(engineCommand, "claude_harness.cjs")
	case "codex":
		return strings.Contains(engineCommand, "codex_harness.cjs")
	case "copilot":
		return strings.Contains(engineCommand, "copilot_harness.cjs")
	case "gemini", "pi":
		return strings.Contains(engineCommand, "shell_harness.cjs")
	default:
		return false
	}
}

func buildExpandableAWFArgs(config AWFCommandConfig, isCloudHypervisor, isArcDind bool, arcDindDockerHostProbe string) (string, string) {
	ghAwDir := constants.GhAwRootDirShell
	expandableArgs := `--container-workdir "${GITHUB_WORKSPACE}"`
	if !isCloudHypervisor {
		expandableArgs += fmt.Sprintf(` --mount "%s:%s:ro" --mount "%s:/host%s:ro"`, ghAwDir, ghAwDir, ghAwDir, ghAwDir)
	}
	expandableArgs, arcDindDockerHostProbe = appendArcDindMountSettings(expandableArgs, arcDindDockerHostProbe, isArcDind)
	if !isCloudHypervisor && config.WorkflowData != nil && usesSafeOutputsArtifactStaging(config.WorkflowData.SafeOutputs) {
		stagingDir := SafeOutputsUploadArtifactsDir
		expandableArgs += fmt.Sprintf(` --mount "%s:%s:rw"`, stagingDir, stagingDir)
		awfHelpersLog.Print("Added read-write mount for upload_artifact staging directory")
	}
	return appendExpandableServiceAndHypervisorArgs(config, isCloudHypervisor, expandableArgs), arcDindDockerHostProbe
}

func appendExpandableServiceAndHypervisorArgs(config AWFCommandConfig, isCloudHypervisor bool, expandableArgs string) string {
	if config.WorkflowData != nil && config.WorkflowData.ServicePortExpressions != "" && isLegacySecurityRuntime(config.WorkflowData) {
		expandableArgs += fmt.Sprintf(` --allow-host-service-ports "%s"`, config.WorkflowData.ServicePortExpressions)
		awfHelpersLog.Printf("Added --allow-host-service-ports with %s", config.WorkflowData.ServicePortExpressions)
	} else if config.WorkflowData != nil && config.WorkflowData.ServicePortExpressions != "" {
		awfHelpersLog.Printf("Skipping --allow-host-service-ports: requires sandbox.agent.runtime: %s", AgentRuntimeDockerSudoIptables)
	}
	if isCloudHypervisor {
		expandableArgs += ` --cloud-hypervisor-binary "${GH_AW_CLOUD_HYPERVISOR_BINARY}"` +
			` --cloud-hypervisor-kernel "${GH_AW_CLOUD_HYPERVISOR_KERNEL}"` +
			` --cloud-hypervisor-rootfs "${GH_AW_CLOUD_HYPERVISOR_ROOTFS}"` +
			` --cloud-hypervisor-supervisor "${GH_AW_CLOUD_HYPERVISOR_SUPERVISOR}"` +
			` --cloud-hypervisor-artifact-manifest "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST}"` +
			` --cloud-hypervisor-artifact-manifest-bundle "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST_BUNDLE}"` +
			` --cloud-hypervisor-artifact-release-tag "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_RELEASE_TAG}"`
	}
	return expandableArgs
}

func rewriteEngineCommandForRuntime(engineCommand string, isArcDind bool) string {
	if !isArcDind {
		return engineCommand
	}
	return rewriteArcDindEngineCommand(engineCommand)
}

func buildArcDindDockerHostSettings(config AWFCommandConfig, firewallConfig *FirewallConfig) (string, string, string) {
	arcDindPrefixProbe := ""
	arcDindDockerHostProbe := fmt.Sprintf(`%s=""
if [[ "${DOCKER_HOST:-}" =~ %s ]]; then
  %s="${DOCKER_HOST}"
fi`,
		awfDockerHostVarName,
		awfArcDindDockerHostRegex,
		awfDockerHostVarName,
	)
	arcDindDockerHostRef := fmt.Sprintf("${%s:+--docker-host \"$%s\"}", awfDockerHostVarName, awfDockerHostVarName)
	if awfSupportsDockerHostPathPrefix(firewallConfig) {
		chrootPatchBody := ""
		if awfSupportsChrootConfig(firewallConfig) {
			if config.WorkflowData != nil && config.WorkflowData.IsDetectionRun {
				chrootPatchBody = "\n" + buildArcDindChrootConfigPatchBodyBash()
			} else {
				chrootPatchBody = "\n" + buildArcDindChrootConfigPatchBody()
			}
		}
		if chrootPatchBody != "" {
			arcDindPrefixProbe = fmt.Sprintf(`if [[ "${DOCKER_HOST:-}" =~ %s ]]; then%s
fi`,
				awfArcDindDockerHostRegex,
				chrootPatchBody)
		}
	}
	return arcDindPrefixProbe, arcDindDockerHostProbe, arcDindDockerHostRef
}

func buildToolCacheMountSettings(isCloudHypervisor bool) (string, string) {
	toolCacheMountProbe := fmt.Sprintf(`%s=""
GH_AW_TOOL_CACHE="${RUNNER_TOOL_CACHE:?RUNNER_TOOL_CACHE must be set}"
if [ -d "$GH_AW_TOOL_CACHE" ]; then
  if [[ "$GH_AW_TOOL_CACHE" != /opt/* ]]; then
    %s="$GH_AW_TOOL_CACHE:$GH_AW_TOOL_CACHE:ro"
  fi
fi`,
		awfToolCacheMountVarName,
		awfToolCacheMountVarName,
	)
	toolCacheMountRef := fmt.Sprintf("${%s:+--mount \"$%s\"}", awfToolCacheMountVarName, awfToolCacheMountVarName)
	if isCloudHypervisor {
		return "", ""
	}
	return toolCacheMountProbe, toolCacheMountRef
}

func appendArcDindMountSettings(expandableArgs, arcDindDockerHostProbe string, isArcDind bool) (string, string) {
	if !isArcDind {
		return expandableArgs, arcDindDockerHostProbe
	}
	expandableArgs += fmt.Sprintf(
		` --mount "%s:%s:rw" --mount "%s:%s:rw"`,
		awfArcDindHomePathExpr, awfArcDindHomePathExpr,
		awfArcDindRootPathExpr+"/sandbox/agent", awfArcDindRootPathExpr+"/sandbox/agent",
	)
	expandableArgs += ` --mount "${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw"`
	arcDindDockerHostProbe += fmt.Sprintf("\nmkdir -p \"%s\" \"%s\"",
		awfArcDindHomePathExpr,
		awfArcDindRootPathExpr+"/sandbox/agent",
	)
	arcDindDockerHostProbe += fmt.Sprintf("\nif [ -d /tmp/gh-aw/aw-prompts ]; then cp -a /tmp/gh-aw/aw-prompts \"%s/aw-prompts\"; fi",
		awfArcDindRootPathExpr,
	)
	return expandableArgs, arcDindDockerHostProbe
}

func buildAWFConfigFileSetup(config AWFCommandConfig, awfConfigJSON string) string {
	maxAICreditsExportLine, updatedAWFConfigJSON := buildMaxAICreditsExport(config, awfConfigJSON)
	printfArg := buildAWFConfigPrintfArg(updatedAWFConfigJSON, maxAICreditsExportLine != "")
	configFileSetup := buildConfigFilePrintfLine(printfArg)
	if maxAICreditsExportLine != "" {
		configFileSetup = maxAICreditsExportLine + "\n" + configFileSetup
	}
	if shouldUseWorkflowCallNetworkAllowedInput(config.WorkflowData) {
		updateScript, updateErr := buildWorkflowCallNetworkAllowedUpdateScript()
		if updateErr != nil {
			awfHelpersLog.Printf("Warning: failed to build workflow_call network_allowed updater: %v", updateErr)
		} else {
			configFileSetup += "\n" + updateScript
		}
	}
	if mkdirScript := buildCloudHypervisorFilesystemMkdirScript(config.WorkflowData); mkdirScript != "" {
		configFileSetup = mkdirScript + "\n" + configFileSetup
	}
	return configFileSetup + fmt.Sprintf("\ncp %q %s", awfConfigRuntimePathExpr, constants.AWFConfigFilePath)
}

// buildCloudHypervisorFilesystemMkdirScript returns a shell command that creates, on the
// host, every directory backing a filesystem.allowWrite entry emitted for the Cloud
// Hypervisor runtime. The AWF planner requires every allowWrite path to already exist on
// the host before AWF starts and fails closed otherwise (e.g. ".awf-home" is not
// auto-created by the Cloud Hypervisor backend the way it is for other runtimes). Returns
// an empty string when the Cloud Hypervisor filesystem.allowWrite section is not being
// emitted, or when there is nothing to create.
func buildCloudHypervisorFilesystemMkdirScript(workflowData *WorkflowData) string {
	if !awfEmitsFilesystemAllowWrite(workflowData, getFirewallConfig(workflowData)) {
		return ""
	}
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || agentConfig.Config == nil || agentConfig.Config.Filesystem == nil {
		return ""
	}

	seen := make(map[string]struct{})
	var targets []string
	for _, guestPath := range agentConfig.Config.Filesystem.AllowWrite {
		hostPath, needsMkdir := cloudHypervisorAllowWriteHostMkdirTarget(guestPath)
		if !needsMkdir {
			continue
		}
		if _, ok := seen[hostPath]; ok {
			continue
		}
		seen[hostPath] = struct{}{}
		targets = append(targets, hostPath)
	}
	if len(targets) == 0 {
		return ""
	}
	sort.Strings(targets)
	quoted := make([]string, len(targets))
	for i, target := range targets {
		quoted[i] = shellEscapeArgWithVarsPreserved(target, "GITHUB_WORKSPACE")
	}
	return "mkdir -p " + strings.Join(quoted, " ")
}

// cloudHypervisorAllowWriteHostMkdirTarget maps a filesystem.allowWrite guest path to the
// host-side directory that must exist before AWF starts under the Cloud Hypervisor
// runtime. The workspace root itself always exists (it is checked out by actions/checkout)
// and does not need to be created.
func cloudHypervisorAllowWriteHostMkdirTarget(guestPath string) (hostPath string, needsMkdir bool) {
	if guestPath == cloudHypervisorWorkspaceWritePath {
		return "", false
	}
	if rel, ok := strings.CutPrefix(guestPath, cloudHypervisorWorkspaceWritePath+"/"); ok {
		return "${GITHUB_WORKSPACE}/" + rel, true
	}
	// Paths outside /workspace (e.g. /tmp/gh-aw/agent) use the same path on the host and
	// in the guest under Cloud Hypervisor's virtiofs exports.
	return guestPath, true
}

func buildMaxAICreditsExport(config AWFCommandConfig, awfConfigJSON string) (string, string) {
	if config.WorkflowData != nil && config.WorkflowData.EngineConfig != nil && config.WorkflowData.EngineConfig.MaxAICredits != 0 {
		return "", awfConfigJSON
	}
	defaultMaxAICredits := strconv.FormatInt(constants.DefaultMaxAICredits, 10)
	if config.WorkflowData != nil {
		switch {
		case config.WorkflowData.IsEvalsRun:
			defaultMaxAICredits = strconv.FormatInt(constants.DefaultDetectionMaxAICredits, 10)
		case config.WorkflowData.IsDetectionRun:
			defaultMaxAICredits = strconv.FormatInt(constants.DefaultDetectionMaxAICredits, 10)
		}
	}
	awfConfigJSON = injectMaxAICreditsExpression(awfConfigJSON, fmt.Sprintf("${%s}", awfMaxAICreditsVarName))
	awfHelpersLog.Printf("Injected maxAiCredits local var reference into AWF config JSON")
	return fmt.Sprintf(`%s="${%s:-%s}"
if [[ ! "$%s" =~ ^[0-9]+$ ]]; then
  %s="%s"
fi`, awfMaxAICreditsVarName, awfMaxAICreditsVarName, defaultMaxAICredits, awfMaxAICreditsVarName, awfMaxAICreditsVarName, defaultMaxAICredits), awfConfigJSON
}

func buildAWFConfigPrintfArg(awfConfigJSON string, hasMaxAICreditsExport bool) string {
	preservedVars := make([]string, 0, 2)
	if hasMaxAICreditsExport {
		preservedVars = append(preservedVars, awfMaxAICreditsVarName)
	}
	if strings.Contains(awfConfigJSON, awfArcDindRootPathExpr) {
		preservedVars = append(preservedVars, "RUNNER_TEMP")
	}
	if len(preservedVars) > 0 {
		return shellEscapeArgWithVarsPreserved(awfConfigJSON, preservedVars...)
	}
	return shellEscapeArg(awfConfigJSON)
}

func buildConfigFilePrintfLine(printfArg string) string {
	printfLine := "printf '%%s\\n' %s > %q"
	if strings.HasPrefix(printfArg, "'") {
		printfLine = "# shellcheck disable=SC2016\nprintf '%%s\\n' %s > %q"
	}
	return fmt.Sprintf(printfLine, printfArg, awfConfigRuntimePathExpr)
}

type buildAWFCommandScriptInput struct {
	writeAgentCLIStartMs   string
	pathSetup              string
	preCreateLog           string
	configFileSetup        string
	modelsJSONPathExport   string
	arcDindDockerHostProbe string
	arcDindPrefixProbe     string
	toolCacheMountProbe    string
	awfCommand             string
	expandableArgs         string
	toolCacheMountRef      string
	arcDindDockerHostRef   string
	awfArgs                []string
	shellWrappedCommand    string
	logFile                string
	engineName             string
	retryStartupFailures   bool
}

func buildAWFCommandScript(input buildAWFCommandScriptInput) string {
	lines := []string{
		"set -o pipefail",
		input.writeAgentCLIStartMs,
	}
	if input.pathSetup != "" {
		lines = append(lines, input.pathSetup)
	}
	lines = append(lines, input.preCreateLog)
	if input.configFileSetup != "" {
		lines = append(lines, input.configFileSetup)
	}
	lines = append(
		lines,
		input.modelsJSONPathExport,
		input.arcDindDockerHostProbe,
		input.arcDindPrefixProbe,
		input.toolCacheMountProbe,
		awfShellcheckDirective,
		buildAWFInvocationCommand(input),
	)
	return strings.Join(lines, "\n")
}

func buildAWFInvocationCommand(input buildAWFCommandScriptInput) string {
	engineName := input.engineName
	if engineName == "" {
		engineName = "agent"
	}
	safeEngineName := safeAWFTempFileNamePart(engineName)
	harnessMarker := shellEscapeArg(fmt.Sprintf("[%s-harness]", engineName))
	command := fmt.Sprintf(`%s %s %s %s %s \
  -- %s`,
		input.awfCommand,
		input.expandableArgs,
		input.toolCacheMountRef,
		input.arcDindDockerHostRef,
		shellJoinArgs(input.awfArgs),
		input.shellWrappedCommand,
	)
	if !input.retryStartupFailures {
		return fmt.Sprintf("%s 2>&1 | tee -a %s", command, shellEscapeArg(input.logFile))
	}

	return fmt.Sprintf(`GH_AW_AWF_ENGINE_NAME=%s \
GH_AW_AWF_HARNESS_MARKER=%s \
GH_AW_AWF_LOG_FILE=%s \
GH_AW_AWF_ATTEMPT_LOG_NAME=%s \
bash "${RUNNER_TEMP}/gh-aw/actions/run_awf_with_startup_retries.sh" -- \
  %s`,
		shellEscapeArg(engineName),
		harnessMarker,
		shellEscapeArg(input.logFile),
		shellEscapeArg(safeEngineName),
		command,
	)
}

func safeAWFTempFileNamePart(value string) string {
	if value == "" {
		return "agent"
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, value)
}

// BuildAWFArgs constructs common AWF arguments from configuration.
// This extracts the shared AWF argument building logic from engine implementations.
//
// The following flags are expressed in the generated JSON config file written by
// BuildAWFCommand and are therefore not emitted here:
//   - --allow-domains / --block-domains   → network.allowDomains / network.blockDomains
//   - --image-tag                         → container.imageTag
//   - --openai-api-target                 → apiProxy.targets.openai.host
//   - --anthropic-api-target              → apiProxy.targets.anthropic.host
//   - --copilot-api-target                → apiProxy.targets.copilot.host
//   - --gemini-api-target                 → apiProxy.targets.gemini.host
//
// Note: --enable-api-proxy is deprecated in AWF v0.27.32+ (API proxy is always on).
// The apiProxy.enabled field is still emitted in the config file for backward compat.
//
// Parameters:
//   - config: AWF command configuration
//
// Returns:
//   - []string: List of AWF arguments (safe args only; expandable-var args like
//     --container-workdir and --mount are handled by BuildAWFCommand)
func BuildAWFArgs(config AWFCommandConfig) []string {
	awfHelpersLog.Printf("Building AWF args for engine: %s", config.EngineName)
	firewallConfig := getFirewallConfig(config.WorkflowData)
	agentConfig := getAgentConfig(config.WorkflowData)
	awfArgs := appendTTYAndContainerRuntimeArgs(config, firewallConfig)
	awfArgs = appendEnvAndMountArgs(config, firewallConfig, agentConfig, awfArgs)
	awfArgs = appendLogAndLegacySecurityArgs(config, firewallConfig, agentConfig, awfArgs)
	awfArgs = append(awfArgs, "--skip-pull")
	awfHelpersLog.Print("Using --skip-pull since images are pre-downloaded")
	awfArgs = appendCliProxyArgs(config, firewallConfig, awfArgs)
	awfArgs = appendAPIBasePathArgs(config, awfArgs)
	awfArgs = appendCustomAWFArgs(firewallConfig, agentConfig, awfArgs)
	awfHelpersLog.Printf("Built %d AWF arguments", len(awfArgs))
	return awfArgs
}

func appendTTYAndContainerRuntimeArgs(config AWFCommandConfig, firewallConfig *FirewallConfig) []string {
	var awfArgs []string
	if config.UsesTTY && !isDockerSbxRuntime(config.WorkflowData) && !isCloudHypervisorRuntime(config.WorkflowData) {
		awfArgs = append(awfArgs, "--tty")
	}
	if isDockerSbxRuntime(config.WorkflowData) && awfSupportsContainerRuntime(firewallConfig) {
		awfArgs = append(awfArgs, "--container-runtime", "sbx")
		awfHelpersLog.Print("Added --container-runtime sbx for docker-sbx microVM runtime")
	} else if isDockerSbxRuntime(config.WorkflowData) {
		awfHelpersLog.Printf("Skipping --container-runtime sbx: AWF version %q is older than required minimum %s", getAWFImageTag(firewallConfig), constants.AWFContainerRuntimeMinVersion)
	}
	if isCloudHypervisorRuntime(config.WorkflowData) && awfSupportsCloudHypervisor(firewallConfig) {
		awfArgs = append(awfArgs, "--container-runtime", "cloud-hypervisor", "--cloud-hypervisor-preview", "--cloud-hypervisor-vcpus", strconv.Itoa(constants.DefaultCloudHypervisorVCPUs), "--cloud-hypervisor-memory-mib", strconv.Itoa(constants.DefaultCloudHypervisorMemoryMiB))
		awfHelpersLog.Print("Added cloud-hypervisor runtime arguments")
	} else if isCloudHypervisorRuntime(config.WorkflowData) {
		awfHelpersLog.Printf("Skipping cloud-hypervisor runtime flags: AWF version %q is older than required minimum %s", getAWFImageTag(firewallConfig), constants.AWFCloudHypervisorMinVersion)
	}
	return awfArgs
}

func appendEnvAndMountArgs(config AWFCommandConfig, firewallConfig *FirewallConfig, agentConfig *AgentSandboxConfig, awfArgs []string) []string {
	awfArgs = append(awfArgs, "--env-all")
	if awfSupportsExcludeEnv(firewallConfig) {
		sortedExclude := make([]string, len(config.ExcludeEnvVarNames))
		copy(sortedExclude, config.ExcludeEnvVarNames)
		sort.Strings(sortedExclude)
		for _, excludedVar := range sortedExclude {
			awfArgs = append(awfArgs, "--exclude-env", excludedVar)
		}
	} else {
		awfHelpersLog.Printf("Skipping --exclude-env: AWF version %q is older than minimum %s", getAWFImageTag(firewallConfig), constants.AWFExcludeEnvMinVersion)
	}
	if !isCloudHypervisorRuntime(config.WorkflowData) {
		awfArgs = append(awfArgs, "--mount", constants.DefaultTmpGhAwMount)
	}
	return appendCustomMountArgs(config.WorkflowData, agentConfig, awfArgs)
}

func appendCustomMountArgs(workflowData *WorkflowData, agentConfig *AgentSandboxConfig, awfArgs []string) []string {
	if isCloudHypervisorRuntime(workflowData) || agentConfig == nil || len(agentConfig.Mounts) == 0 {
		return awfArgs
	}
	sortedMounts := make([]string, len(agentConfig.Mounts))
	copy(sortedMounts, agentConfig.Mounts)
	sort.Strings(sortedMounts)
	for _, mount := range sortedMounts {
		awfArgs = append(awfArgs, "--mount", mount)
	}
	awfHelpersLog.Printf("Added %d custom mounts from agent config", len(sortedMounts))
	return awfArgs
}

func appendLogAndLegacySecurityArgs(config AWFCommandConfig, firewallConfig *FirewallConfig, agentConfig *AgentSandboxConfig, awfArgs []string) []string {
	awfLogLevel := string(constants.AWFDefaultLogLevel)
	if firewallConfig != nil && firewallConfig.LogLevel != "" {
		awfLogLevel = firewallConfig.LogLevel
	}
	awfArgs = append(awfArgs, "--log-level", awfLogLevel)
	if isFeatureEnabled(constants.AwfDiagnosticLogsFeatureFlag, config.WorkflowData) {
		awfArgs = append(awfArgs, "--diagnostic-logs")
		awfHelpersLog.Print("Added --diagnostic-logs because awf-diagnostic-logs feature flag is enabled")
	}
	return appendLegacySecurityArgs(config.WorkflowData, firewallConfig, agentConfig, awfArgs)
}

func appendLegacySecurityArgs(workflowData *WorkflowData, firewallConfig *FirewallConfig, agentConfig *AgentSandboxConfig, awfArgs []string) []string {
	if !isLegacySecurityRuntime(workflowData) {
		awfHelpersLog.Print("Strict security: skipping host-access flag (default)")
		return awfArgs
	}
	if awfSupportsLegacySecurity(firewallConfig) {
		awfArgs = append(awfArgs, "--legacy-security")
		awfHelpersLog.Printf("Added --legacy-security (sandbox.agent.runtime: %s)", AgentRuntimeDockerSudoIptables)
	} else {
		awfHelpersLog.Printf("Skipping --legacy-security: AWF version %q is older than minimum %s (legacy mode is the default for older versions)", getAWFImageTag(firewallConfig), constants.AWFLegacySecurityMinVersion)
	}
	awfArgs = append(awfArgs, "--enable-host-access")
	awfHelpersLog.Printf("Added --enable-host-access for the %s runtime profile", AgentRuntimeDockerSudoIptables)
	return appendLegacyHostPortsArgs(workflowData, firewallConfig, agentConfig, awfArgs)
}

func appendLegacyHostPortsArgs(workflowData *WorkflowData, firewallConfig *FirewallConfig, agentConfig *AgentSandboxConfig, awfArgs []string) []string {
	hostPorts := collectAllowedHostPorts(workflowData, agentConfig)
	if len(hostPorts) == 0 {
		return awfArgs
	}
	if awfSupportsAllowHostPorts(firewallConfig) {
		hostPortsValue := joinPorts(hostPorts)
		awfArgs = append(awfArgs, "--allow-host-ports", hostPortsValue)
		awfHelpersLog.Printf("Added --allow-host-ports %s", hostPortsValue)
		return awfArgs
	}
	warning := fmt.Sprintf("sandbox host ports require AWF %s or newer; skipping --allow-host-ports for AWF version %q", constants.AWFAllowHostPortsMinVersion, getAWFImageTag(firewallConfig))
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warning))
	awfHelpersLog.Printf("Warning: %s", warning)
	return awfArgs
}

func appendCliProxyArgs(config AWFCommandConfig, firewallConfig *FirewallConfig, awfArgs []string) []string {
	if !isGitHubCLIModeEnabled(config.WorkflowData) {
		return awfArgs
	}
	if !awfSupportsCliProxy(firewallConfig) {
		awfHelpersLog.Printf("Skipping CLI proxy flags: AWF version %q is older than minimum %s", getAWFImageTag(firewallConfig), constants.AWFCliProxyMinVersion)
		return awfArgs
	}
	difcProxyHost := "host.docker.internal:18443"
	if isAWFNetworkIsolationEnabled(config.WorkflowData) {
		difcProxyHost = "awmg-cli-proxy:18443"
	}
	awfArgs = append(awfArgs, "--difc-proxy-host", difcProxyHost, "--difc-proxy-ca-cert", constants.TmpDIFCProxyTLSCACert)
	awfHelpersLog.Print("Added --difc-proxy-host and --difc-proxy-ca-cert for CLI proxy sidecar")
	return awfArgs
}

func appendAPIBasePathArgs(config AWFCommandConfig, awfArgs []string) []string {
	openaiBasePath := extractAPIBasePath(config.WorkflowData, "OPENAI_BASE_URL")
	if openaiBasePath != "" {
		awfArgs = append(awfArgs, "--openai-api-base-path", openaiBasePath)
		awfHelpersLog.Printf("Added --openai-api-base-path=%s", openaiBasePath)
	}
	anthropicBasePath := extractAPIBasePath(config.WorkflowData, "ANTHROPIC_BASE_URL")
	if anthropicBasePath != "" {
		awfArgs = append(awfArgs, "--anthropic-api-base-path", anthropicBasePath)
		awfHelpersLog.Printf("Added --anthropic-api-base-path=%s", anthropicBasePath)
	}
	geminiBasePath := extractAPIBasePath(config.WorkflowData, "GEMINI_API_BASE_URL")
	if geminiBasePath != "" {
		awfArgs = append(awfArgs, "--gemini-api-base-path", geminiBasePath)
		awfHelpersLog.Printf("Added --gemini-api-base-path=%s", geminiBasePath)
	}
	return awfArgs
}

func appendCustomAWFArgs(firewallConfig *FirewallConfig, agentConfig *AgentSandboxConfig, awfArgs []string) []string {
	awfArgs = append(awfArgs, getSSLBumpArgs(firewallConfig)...)
	if firewallConfig != nil && len(firewallConfig.Args) > 0 {
		awfArgs = append(awfArgs, firewallConfig.Args...)
	}
	if agentConfig != nil && len(agentConfig.Args) > 0 {
		awfArgs = append(awfArgs, agentConfig.Args...)
		awfHelpersLog.Printf("Added %d custom args from agent config", len(agentConfig.Args))
	}
	if agentConfig != nil && agentConfig.Memory != "" {
		awfArgs = append(awfArgs, "--memory-limit", agentConfig.Memory)
		awfHelpersLog.Printf("Set AWF memory limit to %s", agentConfig.Memory)
	}
	return awfArgs
}

// collectAllowedHostPorts merges the default host-access ports (80, 443, and the
// MCP gateway port) with any explicit sandbox.agent.allow-host-ports values.
//
// This is only called for the docker-sudo-iptables runtime profile:
// --allow-host-ports requires --enable-host-access, which that profile alone enables. GitHub Actions services: ports
// are intentionally NOT derived here — AWF's --allow-host-service-ports flag
// (see ExtractServicePortExpressions) is the correct mechanism for reaching
// services, since it resolves the actual (possibly dynamically assigned) host
// port at runtime via ${{ job.services['<id>'].ports['<port>'] }} expressions
// rather than relying on a static port number.
func collectAllowedHostPorts(workflowData *WorkflowData, agentConfig *AgentSandboxConfig) []int {
	ports := map[int]struct{}{
		80:  {},
		443: {},
	}
	ports[getMCPGatewayPort(workflowData)] = struct{}{}
	if agentConfig != nil {
		for _, port := range agentConfig.AllowHostPorts {
			if port < minPort || port > maxPort {
				continue
			}
			// Defense-in-depth: dangerous ports must never reach --allow-host-ports,
			// even if validateAllowHostPorts was bypassed or its call order changes.
			if _, dangerous := awfDangerousHostPorts[port]; dangerous {
				continue
			}
			ports[port] = struct{}{}
		}
	}
	result := make([]int, 0, len(ports))
	for port := range ports {
		result = append(result, port)
	}
	sort.Ints(result)
	return result
}

func getMCPGatewayPort(workflowData *WorkflowData) int {
	if workflowData != nil && workflowData.SandboxConfig != nil &&
		workflowData.SandboxConfig.MCP != nil && workflowData.SandboxConfig.MCP.Port > 0 {
		return workflowData.SandboxConfig.MCP.Port
	}
	return int(DefaultMCPGatewayPort)
}

func joinPorts(ports []int) string {
	parts := make([]string, len(ports))
	for i, port := range ports {
		parts[i] = strconv.Itoa(port)
	}
	return strings.Join(parts, ",")
}

// GetAWFCommandPrefix determines the AWF command to use (custom or standard).
// This extracts the common pattern for determining AWF command from agent config.
//
// Parameters:
//   - workflowData: The workflow data containing agent configuration
//
// Returns:
//   - string: The AWF command to use (e.g., "sudo --preserve-env awf",
//     "sudo -E /usr/bin/env PATH=\"$PATH\" /usr/local/bin/awf", "awf", or custom command)
func GetAWFCommandPrefix(workflowData *WorkflowData) string {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil && agentConfig.Command != "" {
		awfHelpersLog.Printf("Using custom AWF command: %s", agentConfig.Command)
		return agentConfig.Command
	}

	// The runtime profile decides whether AWF runs rootless or with host privileges.
	profile := resolveSandboxRuntimeProfile(agentConfig)
	awfHelpersLog.Printf("Using AWF command %q for runtime profile %s", profile.AWFCommand, profile.Runtime)
	return profile.AWFCommand
}

// WrapCommandInShell wraps an engine command in a shell invocation for AWF execution.
// This is needed because AWF requires commands to be wrapped in shell for proper execution.
//
// set +o histexpand disables bash history expansion so that agent-authored strings
// containing '!' characters (e.g. "!**") cannot be silently misinterpreted or dropped.
// History expansion is meaningless for non-interactive execution and has no other effect.
//
// Parameters:
//   - command: The engine command to wrap (may include PATH setup and other initialization)
//
// Returns:
//   - string: Shell-wrapped command suitable for AWF execution
func WrapCommandInShell(command string) string {
	awfHelpersLog.Print("Wrapping command in shell for AWF execution")

	// Escape single quotes in the command by replacing ' with '\''
	escapedCommand := strings.ReplaceAll(command, "'", "'\\''")

	// Wrap in shell invocation.
	// set +o histexpand is first to prevent bash from expanding !-patterns in any
	// double-quoted strings that appear in the engine command or its arguments.
	return fmt.Sprintf("/bin/bash -c 'set +o histexpand; %s'", escapedCommand)
}
