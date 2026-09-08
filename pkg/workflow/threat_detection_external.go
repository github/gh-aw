// Package workflow - external-detector-specific install, run, and conclude logic.
package workflow

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

func (c *Compiler) buildPrepareDetectionEngineConfigForExternalDetectorStep(data *WorkflowData) []string {
	if c.getExternalThreatDetectionEngineID(data) != "codex" {
		return nil
	}

	const emptyMCPServersJSON = `{"mcpServers":{}}`
	shellCodexConfigPath := constants.ShellMcpConfigDir + "/config.toml"
	codexHomeConfigPath := constants.TmpMcpConfigDir + "/config.toml"
	detectionData := buildExternalDetectorWorkflowData(data, "codex")
	detectionData.Model = data.Model
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && data.SafeOutputs.ThreatDetection.Model != "" {
		detectionData.Model = data.SafeOutputs.ThreatDetection.Model
	}
	// Reuse the agent job's provider endpoint resolution so detection never pins
	// Codex at a non-OpenAI api-proxy ingress (e.g. the Anthropic port 10001,
	// which rejects Codex requests with 403 "Credentials for Anthropic ... are
	// not configured"). Codex speaks the OpenAI Responses wire API, so only the
	// OpenAI (10000) or Copilot (10002) ingress can serve it.
	codexAPIBase := NewCodexEngine().getOpenAIProxyProviderBaseURL(detectionData)
	codexWSSBase := codexProxyWebsocketBaseURL(codexAPIBase)
	codexConfig := buildExternalDetectorCodexConfig(codexAPIBase, codexWSSBase)
	codexConfigDelimiter := GenerateHeredocDelimiterFromContent("CODEX_DETECTION_CONFIG", codexConfig)

	return []string{
		"      - name: Prepare Codex config for threat-detect\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		"        run: |\n",
		fmt.Sprintf("          mkdir -p %q %q %q\n", constants.ShellMcpConfigDir, constants.TmpMcpConfigDir, constants.TmpMcpConfigLogsDir),
		fmt.Sprintf("          printf '%%s\\n' %q > %q\n", emptyMCPServersJSON, constants.ShellMcpServersJsonPath),
		"          # Point Codex at the AWF OpenAI proxy and disable websocket startup.\n",
		fmt.Sprintf("          cat > %q << %s\n", shellCodexConfigPath, codexConfigDelimiter), //nolint:generatedyamlheredoc // Legacy detector config rendering remains to be migrated.
		codexConfig,
		fmt.Sprintf("          %s\n", codexConfigDelimiter),
		fmt.Sprintf("          cp %q %q\n", shellCodexConfigPath, codexHomeConfigPath),
		fmt.Sprintf("          chmod 600 %q %q\n", shellCodexConfigPath, codexHomeConfigPath),
	}
}

func buildExternalDetectorCodexConfig(apiBase, wssBase string) string {
	// The top-level model_provider selector must precede any table header
	// ([history], [model_providers.*], ...). TOML assigns bare keys to the most
	// recent table, so emitting model_provider after [history] would parse it as
	// history.model_provider, which Codex ignores (falling back to the default
	// openai provider and bypassing the AWF api-proxy sidecar).
	return strings.Join([]string{
		"          model_provider = \"" + codexOpenAIProxyProviderID + "\"",
		"",
		"          [history]",
		"          persistence = \"none\"",
		"",
		"          [model_providers." + codexOpenAIProxyProviderID + "]",
		"          name = \"" + codexOpenAIProxyProviderName + "\"",
		"          base_url = \"" + apiBase + "\"",
		"          api_base = \"" + apiBase + "\"",
		"          wss_base = \"" + wssBase + "\"",
		"          env_key = \"CODEX_API_KEY\"",
		"          wire_api = \"responses\"",
		"          requires_openai_auth = false",
		"          supports_websockets = false",
		"",
	}, "\n")
}

func codexProxyWebsocketBaseURL(apiBase string) string {
	switch {
	case strings.HasPrefix(apiBase, "https://"):
		return "wss://" + strings.TrimPrefix(apiBase, "https://")
	case strings.HasPrefix(apiBase, "http://"):
		return "ws://" + strings.TrimPrefix(apiBase, "http://")
	default:
		return apiBase
	}
}

// buildThreatDetectionWorkflowData creates the shared minimal WorkflowData used by
// detection-job helper steps so topology- and feature-dependent behavior stays in sync.
// It always initializes SandboxConfig.Agent because downstream detection helpers
// extend the agent sandbox configuration (for example, external-detector mounts).
// Callers can pass an empty engineID to inherit the detection job's default engine
// resolution from the source WorkflowData.
func buildThreatDetectionWorkflowData(data *WorkflowData, engineID string) *WorkflowData {
	if engineID == "" {
		engineID = data.AI
	}
	if engineID == "" {
		engineID = "claude"
	}

	detectionData := &WorkflowData{
		AI:                engineID,
		ActionCache:       data.ActionCache,
		Features:          data.Features,
		Jobs:              data.Jobs,
		Permissions:       data.Permissions,
		ParsedFrontmatter: data.ParsedFrontmatter,
		CachedPermissions: data.CachedPermissions,
		ModelCosts:        data.ModelCosts,
		IsDetectionRun:    true,
		RunnerConfig:      data.RunnerConfig,
		TimeoutMinutes:    "timeout-minutes: " + resolveDetectionJobTimeoutValue(data),
		CompiledVersion:   data.CompiledVersion,
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		},
	}

	if firewallConfig := getFirewallConfig(data); firewallConfig != nil {
		firewallCopy := *firewallConfig
		detectionData.NetworkPermissions = &NetworkPermissions{Firewall: &firewallCopy}
		if detectionData.SandboxConfig == nil {
			detectionData.SandboxConfig = &SandboxConfig{}
		}
		if detectionData.SandboxConfig.Agent == nil {
			detectionData.SandboxConfig.Agent = &AgentSandboxConfig{Type: SandboxTypeAWF}
		}
		detectionData.SandboxConfig.Agent.Version = firewallCopy.Version
	}

	return detectionData
}

// buildPullAWFContainersStep creates a step that pre-pulls AWF (agent workflow firewall)
// container images in the detection job. The detection engine runs inside AWF, which uses
// the firewall stack containers needed for the selected topology (squid, agent, api-proxy,
// plus build-tools on arc-dind). Pre-pulling avoids on-demand pulls at runtime. Only AWF
// images are pulled here; MCP server images are not needed for detection.
func (c *Compiler) buildPullAWFContainersStep(data *WorkflowData) []string {
	// Build a minimal WorkflowData that represents the detection engine context so
	// collectDockerImages returns only the AWF firewall images (no MCP tool images).
	detectionData := buildThreatDetectionWorkflowData(data, "")
	detectionData.Tools = map[string]any{}

	images := collectDockerImages(detectionData.Tools, detectionData, c.actionMode)
	if len(images) == 0 {
		threatLog.Print("No AWF container images to pre-pull for detection job")
		return nil
	}
	threatLog.Printf("Pre-pulling %d AWF container image(s) for detection job", len(images))

	var b strings.Builder
	generateDownloadDockerImagesStep(&b, images)
	if b.Len() == 0 {
		return nil
	}

	// Split the generated YAML into individual lines so each is a separate entry
	lines := strings.Split(b.String(), "\n")
	var steps []string
	for _, line := range lines {
		if line != "" {
			steps = append(steps, line+"\n")
		}
	}
	return steps
}

// getThreatDetectionEngineID returns the effective engine ID for the detection job.
// It mirrors threat-detection engine resolution: threat-detection.engine overrides main engine.
func (c *Compiler) getThreatDetectionEngineID(data *WorkflowData) string {
	var engineID string

	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil &&
		data.SafeOutputs.ThreatDetection.EngineConfig != nil &&
		data.SafeOutputs.ThreatDetection.EngineConfig.ID != "" {
		engineID = data.SafeOutputs.ThreatDetection.EngineConfig.ID
	} else {
		engineID = data.AI
		if engineID == "" && data.EngineConfig != nil && data.EngineConfig.ID != "" {
			engineID = data.EngineConfig.ID
		}
	}

	if engineID == "" {
		engineID = "claude"
	}

	// Threat detection currently does not support the Pi engine backend.
	// Normalize to Copilot so workflows with engine: pi still get a working detector.
	if engineID == "pi" {
		return "copilot"
	}

	return engineID
}

// getExternalThreatDetectionEngineID returns the engine used by the external
// threat-detect path. Threat-detection engine resolution is centralized in
// getThreatDetectionEngineID, including Pi -> Copilot normalization.
func (c *Compiler) getExternalThreatDetectionEngineID(data *WorkflowData) string {
	return c.getThreatDetectionEngineID(data)
}

type externalDetectorPathSetup struct {
	hostSetup     string
	commandPrefix string
}

// buildExternalDetectorPathSetup returns the host-side shell commands that run
// before AWF plus any command prefix needed inside the external detector container.
// For the installed Copilot engine, threat-detect invokes the engine binary by
// name, so the mounted ${RUNNER_TEMP}/gh-aw/bin/ directory must be prepended to
// PATH in the container command. Non-ARC topologies also need a host-side copy
// into that mounted directory; ARC/DinD already stages Copilot there during install.
func (c *Compiler) buildExternalDetectorPathSetup(data *WorkflowData, engineID string) externalDetectorPathSetup {
	if engineID == "codex" && NewCodexEngine().ResolveLLMProvider(data) == LLMProviderGitHub {
		return externalDetectorPathSetup{commandPrefix: codexBYOKAPIKeyExport() + " && "}
	}
	if engineID != "copilot" {
		return externalDetectorPathSetup{}
	}
	setup := externalDetectorPathSetup{
		commandPrefix: `export PATH="${RUNNER_TEMP}/gh-aw/bin:$PATH" && `,
	}
	if isArcDindTopology(data) {
		return setup
	}
	setup.hostSetup = copilotBinaryPathSetup
	return setup
}

// buildInstallAWFForExternalDetectorStep creates the AWF installation step required
// by the external detector execution path, which invokes `awf` directly.
func (c *Compiler) buildInstallAWFForExternalDetectorStep(data *WorkflowData) []string {
	version := string(constants.DefaultFirewallVersion)
	if firewallConfig := getFirewallConfig(data); firewallConfig != nil && firewallConfig.Version != "" {
		version = firewallConfig.Version
	}

	// Pass the detection job's own sandbox agent config so the install mode matches
	// how AWF is invoked in this job. Passing nil would install without --rootless
	// while the execution step still runs the rootless `awf` command, which breaks on
	// runners where /usr/local is not writable by the runner user.
	detectionData := buildThreatDetectionWorkflowData(data, "")
	threatLog.Printf("Building AWF installation step for external detector (version=%s)", version)
	step := generateAWFInstallationStep(version, getAgentConfig(detectionData))
	if len(step) == 0 {
		return nil
	}

	lines := make([]string, 0, len(step))
	for _, line := range step {
		lines = append(lines, line+"\n")
	}
	return lines
}

// buildInstallDetectionEngineForExternalDetectorStep installs the selected detection
// engine in the external detector path so threat-detect can invoke the engine binary.
func (c *Compiler) buildInstallDetectionEngineForExternalDetectorStep(data *WorkflowData) []string {
	engineID := c.getExternalThreatDetectionEngineID(data)
	engine, err := c.getAgenticEngine(engineID)
	if err != nil {
		threatLog.Printf("Failed to resolve detection engine %q for external detector installation: %v (compilation will continue without engine install steps; threat-detect will only succeed if the engine binary is already available at runtime)", engineID, err)
		return nil
	}

	// Build a synthetic detection WorkflowData solely to generate the engine's
	// installation steps for this separate detection job context.
	threatDetectionData := buildExternalDetectorWorkflowData(data, engineID)

	installSteps := engine.GetInstallationSteps(threatDetectionData)
	filteredInstallSteps := make([]GitHubActionStep, 0, len(installSteps)+1)
	for _, step := range installSteps {
		if isAWFBinaryInstallStep(step) {
			continue
		}
		filteredInstallSteps = append(filteredInstallSteps, step)
	}

	if engineRequiresNodeHarness(engine) && !installStepsContainNodeSetup(filteredInstallSteps) {
		threatLog.Print("Injecting Node.js setup step for external detection engine harness")
		filteredInstallSteps = append([]GitHubActionStep{GenerateNodeJsSetupStep()}, filteredInstallSteps...)
	}

	var yaml strings.Builder
	arcDind := isArcDindTopology(threatDetectionData)
	if arcDind && installStepsContainNodeSetup(filteredInstallSteps) {
		c.generateArcDindToolCacheRedirectStep(&yaml)
	}
	c.emitRuntimeSetupSteps(&yaml, filteredInstallSteps, arcDind)
	return []string{yaml.String()}
}

func isAWFBinaryInstallStep(step GitHubActionStep) bool {
	for _, line := range step {
		if strings.Contains(line, "install_awf_binary.sh") {
			return true
		}
	}
	return false
}

func appendThreatDetectionRWMount(mounts []string) []string {
	threatDetectionMount := constants.ThreatDetectionDir + ":" + constants.ThreatDetectionDir + ":rw"
	if slices.Contains(mounts, threatDetectionMount) {
		return mounts
	}
	return append(mounts, threatDetectionMount)
}

// buildExternalDetectorExecutionStep creates the AWF execution step for the external
// threat-detect binary. It runs threat-detect inside the AWF firewall sandbox with a
// read-write mount so detection_result.json can be written from inside the container
// back to the host filesystem. This replaces the inline engine execution step when
// features: gh-aw-detection: true is set.
//
//nolint:largefunc // Existing external detector assembly is intentionally kept together.
func (c *Compiler) buildExternalDetectorExecutionStep(data *WorkflowData) []string {
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil &&
		data.SafeOutputs.ThreatDetection.EngineDisabled {
		return []string{
			"      # AI engine disabled for threat detection (engine: false)\n",
		}
	}

	engineID := c.getExternalThreatDetectionEngineID(data)
	engine, err := c.getAgenticEngine(engineID)
	if err != nil {
		threatLog.Printf("Failed to resolve detection engine %q for external detector execution: %v", engineID, err)
		return []string{fmt.Sprintf("      # Failed to resolve detection engine %q: %v\n", engineID, err)}
	}

	// Build detection WorkflowData for the external detector.
	// The rw mount for ThreatDetectionDir allows the threat-detect binary to write
	// detection_result.json from inside the AWF container to the host filesystem.
	threatDetectionData := buildExternalDetectorWorkflowData(data, engineID)

	// Resolve the detection model, mirroring buildDetectionEngineExecutionStep on the
	// inline path. Without this, the engine env block falls back to
	// ${{ vars.GH_AW_MODEL_DETECTION_COPILOT || ... || 'auto' }}, and when no org var
	// is set COPILOT_MODEL is 'auto'. The AWF API proxy has no pricing for 'auto' and
	// returns HTTP 400, causing every inference attempt to fail.
	resolvedDetectionModel := data.Model
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && data.SafeOutputs.ThreatDetection.Model != "" {
		resolvedDetectionModel = data.SafeOutputs.ThreatDetection.Model
	}
	if resolvedDetectionModel == "" {
		if defaultModel := compilerenv.ResolveDefaultDetectionModel(""); defaultModel != "" {
			resolvedDetectionModel = defaultModel
		} else if defaultModel := engine.GetDefaultDetectionModel(); defaultModel != "" {
			resolvedDetectionModel = defaultModel
		}
	}
	if resolvedDetectionModel == "" {
		resolvedDetectionModel = "detection"
	}
	// Pi workflows normalise to Copilot; strip the provider prefix so the Copilot CLI
	// receives a bare model ID rather than a "pi/model-name" string.
	// Precedence mirrors the inline path: explicit threat-detection.engine.id overrides
	// the main engine config, which overrides the legacy top-level AI field.
	originalEngineID := data.AI
	if data.EngineConfig != nil && data.EngineConfig.ID != "" {
		originalEngineID = data.EngineConfig.ID
	}
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil &&
		data.SafeOutputs.ThreatDetection.EngineConfig != nil &&
		data.SafeOutputs.ThreatDetection.EngineConfig.ID != "" {
		originalEngineID = data.SafeOutputs.ThreatDetection.EngineConfig.ID
	}
	if engineID == "copilot" && originalEngineID == "pi" {
		resolvedDetectionModel = extractPiModelID(resolvedDetectionModel)
	}
	threatDetectionData.Model = resolvedDetectionModel
	// Propagate the model alias map so the detection AWF config includes
	// apiProxy.models, enabling the harness to resolve aliases (e.g. "haiku") to
	// concrete model IDs before the Copilot CLI makes inference requests.
	threatDetectionData.ModelMappings = data.ModelMappings
	// Propagate default AI credits pricing so the detection AWF config includes
	// apiProxy.defaultAiCreditsPricing when the main workflow configures it.
	threatDetectionData.DefaultAiCreditsPricing = data.DefaultAiCreditsPricing

	threatDetectionData.NetworkPermissions = &NetworkPermissions{
		Allowed: getThreatDetectionAdditionalAllowedDomains(data),
	}
	// Add a read-write mount so the threat-detect binary can write
	// detection_result.json inside the container and it becomes visible
	// on the host through the bind mount.
	threatDetectionData.SandboxConfig.Agent.Mounts = appendThreatDetectionRWMount(threatDetectionData.SandboxConfig.Agent.Mounts)

	// Compute which env vars to exclude from the AWF container. The API proxy
	// handles authentication, so the raw credentials must not reach the container.
	excludeEnvVarNames := ComputeAWFExcludeEnvVarNames(threatDetectionData, engineCoreSecretVarNames(engineID))

	// Compute allowed domains for the detection engine. The AWF firewall for the
	// detection job must permit the engine's required API endpoints. Without this,
	// engines such as Codex (which connects to api.openai.com and chatgpt.com) fail
	// with "domain not in allowlist" and the detection job exits with code 1/2.
	var allowedDomains string
	if engineID == string(constants.CodexEngine) {
		// Codex's allowed domains depend on the resolved LLM provider (e.g. GitHub-hosted
		// inference adds CopilotDefaultDomains), which GetAllowedDomainsForEngine's static
		// defaults do not account for. Compute domains the same way the main Codex
		// execution path does so GitHub-hosted detection requests are not blocked.
		allowedDomains = mergeDomainsWithNetworkToolsAndRuntimes(NewCodexEngine().defaultDomains(threatDetectionData), threatDetectionData.NetworkPermissions, data.Tools, data.Runtimes)
	} else {
		allowedDomains = GetAllowedDomainsForEngine(constants.EngineName(engineID), threatDetectionData.NetworkPermissions, data.Tools, data.Runtimes)
	}
	// Extend the allowlist with any custom API target domains when engine.api-target
	// is set (e.g. GHE or a custom OpenAI-compatible endpoint).
	if threatDetectionData.EngineConfig != nil && threatDetectionData.EngineConfig.APITarget != "" {
		allowedDomains = mergeAPITargetDomains(allowedDomains, threatDetectionData.EngineConfig.APITarget)
	}

	// Build the threat-detect command. The binary reads the prepared detection
	// artifacts directory from /tmp/gh-aw/threat-detection/ (set up by previous
	// steps) and writes the structured verdict to detection_result.json there.
	// Prepend npm PATH setup so that npm-installed engine CLIs (e.g. claude, codex)
	// can be found inside the AWF container's chroot environment. threat-detect
	// invokes the engine binary as a subprocess and relies on PATH to locate it.
	//
	// The --step-summary flag was removed from threat-detect (v0.4.5+): the binary
	// no longer writes any step-summary output (see
	// github/gh-aw-threat-detection#792), so the flag is intentionally omitted here.
	npmPathSetup := GetNpmBinPathSetup()
	threatDetectCmd := buildThreatDetectCommand(npmPathSetup, engineID, data.SafeOutputs.ThreatDetection)

	// Build the complete AWF command. BuildAWFCommand handles config file setup,
	// ARC/DinD probes, tool cache mount, and the log tee pattern.
	//
	// PathSetup stages the engine binary (e.g. copilot) to the mounted
	// ${RUNNER_TEMP}/gh-aw/bin/ directory on the host when required. The paired
	// command prefix prepends that staged bin dir to PATH in the AWF container.
	pathSetup := c.buildExternalDetectorPathSetup(threatDetectionData, engineID)
	if pathSetup.commandPrefix != "" {
		threatDetectCmd = pathSetup.commandPrefix + threatDetectCmd
	}

	awfConfig := AWFCommandConfig{
		EngineName:         engineID,
		EngineCommand:      threatDetectCmd,
		LogFile:            constants.ThreatDetectionLogPath,
		WorkflowData:       threatDetectionData,
		ExcludeEnvVarNames: excludeEnvVarNames,
		AllowedDomains:     allowedDomains,
		PathSetup:          pathSetup.hostSetup,
	}
	command := BuildAWFCommand(awfConfig)

	// Reuse the engine's own execution env block so the external detector path
	// gets the same token/model/runtime environment configuration as the agent job.
	executionSteps := engine.GetExecutionSteps(threatDetectionData, constants.ThreatDetectionLogPath)
	var envLines []string
	if len(executionSteps) > 0 {
		envLines = extractStepEnvLines(executionSteps[0])
		if len(envLines) == 0 {
			threatLog.Printf("Detection engine %q execution step did not expose env lines; external detector will run with minimal env", engineID)
		}
	} else {
		threatLog.Printf("Detection engine %q did not generate execution steps; external detector will run with minimal env", engineID)
	}

	continueOnError := true
	var continueOnErrorExpr *string
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil {
		continueOnError = data.SafeOutputs.ThreatDetection.IsContinueOnError()
		continueOnErrorExpr = data.SafeOutputs.ThreatDetection.ContinueOnErrorExpr
	}

	steps := []string{
		"      - name: Execute threat detection with AWF\n",
		"        id: detection_agentic_execution\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		"        continue-on-error: true\n",
		// Bound the step at the workflow level as well as through
		// GH_AW_TIMEOUT_MINUTES: the env var is only honoured once the binary is
		// running, so a wedge before that point (image pull, AWF startup) would
		// otherwise run until the 360 minute GitHub default.
		"        timeout-minutes: " + resolveStepTimeoutValue(threatDetectionData) + "\n",
	}
	if len(envLines) == 0 {
		steps = append(steps, "        env:\n")
	} else {
		for _, line := range envLines {
			steps = append(steps, line+"\n")
		}
	}
	// Pass context as environment variables: AWF's --env-all forwards them to
	// threat-detect without interpolating user-controlled prompt text into a command.
	steps = append(steps, c.buildThreatDetectionContextEnvVars(data, continueOnError, continueOnErrorExpr)...)
	steps = append(steps, "        run: |\n")
	for _, line := range strings.SplitAfter(command, "\n") {
		if line == "" {
			continue
		}
		prefixed := "          " + line
		if !strings.HasSuffix(prefixed, "\n") {
			prefixed += "\n"
		}
		steps = append(steps, prefixed)
	}

	return steps
}

func buildThreatDetectCommand(npmPathSetup, engineID string, config *ThreatDetectionConfig) string {
	args := []string{
		"threat-detect",
		"--engine", shellEscapeArg(engineID),
	}

	if config != nil {
		if config.EngineTimeout != nil {
			args = append(args, "--engine-timeout", shellEscapeArg(*config.EngineTimeout))
		}
		if config.MaxTurns != nil {
			args = append(args, "--max-turns", strconv.Itoa(*config.MaxTurns))
		}
		if config.Retries != nil {
			args = append(args, "--retries", strconv.Itoa(*config.Retries))
		}
	}

	args = append(args,
		"--output", shellEscapeArg(constants.ThreatDetectionResultPath),
		shellEscapeArg(constants.ThreatDetectionDir),
	)

	return fmt.Sprintf("%s && %s", npmPathSetup, strings.Join(args, " "))
}

// extractStepEnvLines copies the YAML env: block from a rendered engine execution step.
// It intentionally stops when a comment line appears because comments in step templates
// are section separators, and consuming past them may bleed into non-env content.
func extractStepEnvLines(step GitHubActionStep) []string {
	envIndex := -1
	for i, line := range step {
		if strings.TrimSpace(line) == "env:" {
			envIndex = i
			break
		}
	}
	if envIndex == -1 {
		return nil
	}

	var envLines []string
	for _, line := range step[envIndex:] {
		if line == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			break
		}
		if !strings.HasPrefix(line, stepEnvIndent) && trimmed != "env:" {
			break
		}
		envLines = append(envLines, line)
	}

	return envLines
}

// buildUploadDetectionArtifactStep creates a step that uploads the structured verdict
// file (detection_result.json) as the detection artifact. Used when
// features: gh-aw-detection: true is set; the inline path uses buildUploadDetectionLogStep.
//
// The raw engine log (detection.log) is intentionally NOT uploaded here: it can contain
// content derived from the untrusted agent transcript/output that was passed to the
// detection engine (including secrets the agent may have echoed), and persisting it to a
// downloadable workflow artifact would create a secret-exfiltration path. It stays on the
// runner's filesystem and is only ever inspected in-job (e.g. by the conclude step).
func (c *Compiler) buildUploadDetectionArtifactStep(data *WorkflowData) []string {
	detectionArtifactName := artifactPrefixExprForAgentDownstreamJob(data) + constants.DetectionArtifactName.String()
	steps := []string{
		"      - name: Upload threat detection artifact\n",
		fmt.Sprintf("        if: %s\n", detectionStepCondition),
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		"          name: " + detectionArtifactName + "\n",
		"          path: |\n",
		"            " + constants.ThreatDetectionResultPath + "\n",
	}
	// Include the detection AWF run's own firewall proxy/audit logs (token usage, squid
	// logs) so detection-phase usage surfaces in the usage artifact and counts toward the
	// AI-credits budget cap (see gh-aw#54047). These are firewall/proxy metadata, not the
	// untrusted agent transcript, so bundling them does not introduce the secret-exfiltration
	// risk that keeps detection.log off this artifact.
	if isFirewallEnabled(data) {
		steps = append(steps,
			"            "+detectionFirewallLogsDir+"/logs/\n",
			"            "+detectionFirewallLogsDir+"/audit/\n",
		)
	}
	steps = append(steps, "          if-no-files-found: ignore\n")
	return steps
}

// buildExternalDetectorConcludeStep creates the conclude step for the external
// threat-detect binary. It runs `threat-detect conclude --result-file ...` which reads
// the structured detection_result.json and sets the detection_conclusion/detection_reason/
// detection_success step outputs, preserving the same gate contract as the inline
// parse_threat_detection_results.cjs path. Outputs (not env vars) are used exclusively;
// downstream jobs consume these via needs.detection.outputs.* expressions.
// The step ID (detection_conclusion) and env vars (RUN_DETECTION, DETECTION_AGENTIC_EXECUTION_OUTCOME,
// GH_AW_DETECTION_CONTINUE_ON_ERROR) are byte-identical to the inline conclude step.
func (c *Compiler) buildExternalDetectorConcludeStep(data *WorkflowData) []string {
	// Determine continue-on-error mode (same logic as buildDetectionConclusionStep).
	continueOnError := true
	var continueOnErrorExpr *string
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil {
		continueOnError = data.SafeOutputs.ThreatDetection.IsContinueOnError()
		continueOnErrorExpr = data.SafeOutputs.ThreatDetection.ContinueOnErrorExpr
	}

	steps := []string{
		"      - name: Conclude threat detection\n",
		"        id: detection_conclusion\n",
		"        if: always()\n",
	}

	if continueOnErrorExpr != nil {
		steps = append(steps, fmt.Sprintf("        continue-on-error: %s\n", *continueOnErrorExpr))
	} else if continueOnError {
		steps = append(steps, "        continue-on-error: true\n")
	}

	var coeEnvLine string
	if continueOnErrorExpr != nil {
		coeEnvLine = fmt.Sprintf("          GH_AW_DETECTION_CONTINUE_ON_ERROR: %s\n", *continueOnErrorExpr)
	} else {
		coeEnvLine = fmt.Sprintf("          GH_AW_DETECTION_CONTINUE_ON_ERROR: %q\n", strconv.FormatBool(continueOnError))
	}

	steps = append(steps, []string{
		"        env:\n",
		"          RUN_DETECTION: ${{ steps.detection_guard.outputs.run_detection }}\n",
		"          DETECTION_AGENTIC_EXECUTION_OUTCOME: ${{ steps.detection_agentic_execution.outcome }}\n",
		coeEnvLine,
		"        run: |\n",
		fmt.Sprintf("          bash \"${RUNNER_TEMP}/gh-aw/actions/conclude_threat_detection.sh\" %s\n", shellEscapeArg(constants.ThreatDetectionResultPath)),
	}...)

	return steps
}

// buildWorkspaceCheckoutForDetectionStep creates a checkout step for the detection job.
// It runs only when the agent job produced a patch, so the detection engine can
// analyze code changes in the context of the surrounding codebase.
func (c *Compiler) buildWorkspaceCheckoutForDetectionStep(data *WorkflowData) []string {
	checkoutPin := getActionPin("actions/checkout")
	if checkoutPin == "" {
		threatLog.Print("No action pin found for actions/checkout, skipping workspace checkout step")
		return nil
	}

	steps := []string{
		"      - name: Checkout repository for patch context\n",
		fmt.Sprintf("        if: needs.%s.outputs.has_patch == 'true'\n", constants.AgentJobName),
		fmt.Sprintf("        uses: %s\n", checkoutPin),
		"        with:\n",
		"          persist-credentials: false\n",
	}

	threatLog.Print("Added conditional workspace checkout step for patch context")
	return steps
}
