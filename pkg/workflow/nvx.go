package workflow

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const (
	defaultNVXSignerWorkflow = "github/gh-aw-firewall/.github/workflows/release.yml"
	defaultNVXMountPolicy    = "workspace-only"
	defaultNVXContainerDir   = "/workspace"
)

var trustedNVXSignerWorkflows = map[string]struct{}{
	defaultNVXSignerWorkflow: {},
	"github/gh-aw-firewall/.github/workflows/nvx-phase-3b-live-kvm.yml":     {},
	"github/gh-aw-firewall/.github/workflows/smoke-nvx-copilot.lock.yml":    {},
	"github/gh-aw-firewall/.github/workflows/smoke-nvx-build-test.lock.yml": {},
}

func validateNVXRuntimeConfig(workflowData *WorkflowData, agentConfig *AgentSandboxConfig) error {
	if agentConfig == nil {
		return nil
	}
	if agentConfig.Runtime != AgentRuntimeNVX {
		if agentConfig.NVX != nil {
			return nvxValidationError("sandbox.agent.nvx", "", "NVX options require sandbox.agent.runtime: nvx", "Set the runtime to nvx or remove sandbox.agent.nvx.")
		}
		return nil
	}

	nvx := agentConfig.NVX
	if nvx == nil {
		return nvxValidationError("sandbox.agent.nvx", "", "NVX requires explicit runtime configuration", "Set preview, network-isolation, api-proxy, layer-path, openvmm-path, kernel-path, initramfs-path, artifact-manifest-path, and artifact-manifest-bundle-path.")
	}
	if err := validateNVXSecurityOptions(nvx); err != nil {
		return err
	}
	if err := validateNVXPathInputs(nvx); err != nil {
		return err
	}
	if err := validateNVXResourceOptions(nvx); err != nil {
		return err
	}
	return validateNVXCompatibility(workflowData, agentConfig)
}

func validateNVXSecurityOptions(nvx *AgentNVXConfig) error {
	for _, required := range []struct {
		path    string
		value   bool
		message string
	}{
		{"sandbox.agent.nvx.preview", nvx.PreviewEnabled, "NVX requires explicit preview opt-in"},
		{"sandbox.agent.nvx.network-isolation", nvx.NetworkIsolation, "NVX requires strict network isolation"},
		{"sandbox.agent.nvx.api-proxy", nvx.APIProxy, "NVX requires API proxy credential isolation"},
	} {
		if !required.value {
			return nvxValidationError(required.path, "false", required.message, "Set the field to true; NVX never disables these protections.")
		}
	}
	return nil
}

func validateNVXPathInputs(nvx *AgentNVXConfig) error {
	for _, required := range []struct {
		path  string
		value string
	}{
		{"sandbox.agent.nvx.layer-path", nvx.LayerPath},
		{"sandbox.agent.nvx.openvmm-path", nvx.OpenVMMPath},
		{"sandbox.agent.nvx.kernel-path", nvx.KernelPath},
		{"sandbox.agent.nvx.initramfs-path", nvx.InitramfsPath},
		{"sandbox.agent.nvx.artifact-manifest-path", nvx.ArtifactManifestPath},
		{"sandbox.agent.nvx.artifact-manifest-bundle-path", nvx.ArtifactManifestBundlePath},
	} {
		if strings.TrimSpace(required.value) == "" {
			return nvxValidationError(required.path, "", "NVX requires this trusted input", "Provide an absolute path or a GitHub Actions expression resolving to an absolute path.")
		}
		if strings.ContainsAny(required.value, "\r\n") ||
			(!path.IsAbs(required.value) && !isNVXPathExpression(required.value)) {
			return nvxValidationError(required.path, required.value, "NVX paths must be absolute or expression-valued", "Provide an absolute path or a GitHub Actions expression such as ${{ runner.temp }}.")
		}
	}
	return nil
}

func validateNVXResourceOptions(nvx *AgentNVXConfig) error {
	if nvx.MountPolicy != "" && nvx.MountPolicy != "workspace-only" && nvx.MountPolicy != "workspace-and-tool-cache" {
		return nvxValidationError("sandbox.agent.nvx.mount-policy", nvx.MountPolicy, "unsupported NVX mount policy", "Choose workspace-only or workspace-and-tool-cache.")
	}
	if nvx.SignerWorkflow != "" {
		if _, ok := trustedNVXSignerWorkflows[nvx.SignerWorkflow]; !ok {
			return nvxValidationError("sandbox.agent.nvx.signer-workflow", nvx.SignerWorkflow, "NVX artifact signer workflow is not trusted", "Use the AWF release workflow or one of the documented NVX validation/smoke workflows.")
		}
	}
	if nvx.MemoryMiB < 0 || nvx.MemoryMaxBytes < 0 || nvx.PidsMax < 0 || nvx.ScratchBytes < 0 {
		return nvxValidationError("sandbox.agent.nvx", "", "NVX resource limits must be positive when specified", "Use positive integers or omit the optional values to use AWF defaults.")
	}
	if nvx.MemoryMiB > 0 && int64(nvx.MemoryMiB) > (1<<63-1)/(1024*1024) {
		return nvxValidationError("sandbox.agent.nvx.memory-mib", strconv.Itoa(nvx.MemoryMiB), "NVX memory value is too large", "Use a smaller positive MiB value.")
	}
	memoryMiB := nvx.MemoryMiB
	if memoryMiB == 0 {
		memoryMiB = constants.DefaultNVXMemoryMiB
	}
	memoryMaxBytes := nvx.MemoryMaxBytes
	if memoryMaxBytes == 0 {
		memoryMaxBytes = constants.DefaultNVXMemoryMaxBytes
	}
	if memoryMaxBytes < int64(memoryMiB)*1024*1024 {
		return nvxValidationError("sandbox.agent.nvx.memory-max-bytes", strconv.FormatInt(memoryMaxBytes, 10), "NVX cgroup memory ceiling must be at least memory-mib × 1048576", "Increase memory-max-bytes or reduce memory-mib.")
	}
	if nvx.ContainerWorkDir != "" {
		if !path.IsAbs(nvx.ContainerWorkDir) {
			return nvxValidationError("sandbox.agent.nvx.container-workdir", nvx.ContainerWorkDir, "NVX container working directory must be absolute", "Use /workspace or a descendant, such as /workspace/build.")
		}
		cleaned := path.Clean(nvx.ContainerWorkDir)
		if cleaned != "/workspace" && !strings.HasPrefix(cleaned, "/workspace/") {
			return nvxValidationError("sandbox.agent.nvx.container-workdir", nvx.ContainerWorkDir, "NVX container working directory must stay inside /workspace", "Use /workspace or a descendant, such as /workspace/build.")
		}
	}
	return nil
}

func validateNVXCompatibility(workflowData *WorkflowData, agentConfig *AgentSandboxConfig) error {
	if !versionAtLeast(getAWFImageTag(getFirewallConfig(workflowData)), string(constants.DefaultFirewallVersion), string(constants.AWFNVXMinVersion)) ||
		strings.EqualFold(getAWFImageTag(getFirewallConfig(workflowData)), "latest") {
		return nvxValidationError("sandbox.agent.runtime", string(AgentRuntimeNVX), fmt.Sprintf("NVX requires a versioned AWF release %s or newer", constants.AWFNVXMinVersion), "Set firewall.version or sandbox.agent.version to a concrete AWF release that supports NVX.")
	}
	if isArcDindTopology(workflowData) {
		return nvxValidationError("sandbox.agent.runtime", string(AgentRuntimeNVX), "NVX is incompatible with runner.topology: arc-dind", "Use a Linux x86_64 KVM runner with a shared filesystem and Docker daemon.")
	}
	if workflowData != nil && workflowData.Container != "" {
		return nvxValidationError("container", workflowData.Container, "NVX cannot run inside a containerized workflow job", "Run the agent job directly on a Linux x86_64 KVM runner.")
	}
	if ResolveEngineID(workflowData) == "claude" {
		return nvxValidationError("sandbox.agent.runtime", string(AgentRuntimeNVX), "NVX does not support TTY or interactive agent execution", "Select a non-interactive engine or a different sandbox runtime.")
	}
	if agentConfig.Command != "" || len(agentConfig.Args) > 0 || len(agentConfig.Env) > 0 {
		return nvxValidationError("sandbox.agent", "", "NVX does not support custom AWF command, arguments, or environment", "Remove sandbox.agent.command, sandbox.agent.args, and sandbox.agent.env so the compiler can enforce NVX security flags and credential isolation.")
	}
	if len(agentConfig.Mounts) > 0 {
		return nvxValidationError("sandbox.agent.mounts", "", "NVX does not support additional host volume mounts", "Use the workspace export or the workspace-and-tool-cache mount policy.")
	}
	if agentConfig.Memory != "" {
		return nvxValidationError("sandbox.agent.memory", agentConfig.Memory, "Docker memory limits do not apply to NVX", "Configure memory-mib and memory-max-bytes under sandbox.agent.nvx.")
	}
	if agentConfig.Config != nil && agentConfig.Config.EnableWeakerNestedSandbox {
		return nvxValidationError("sandbox.agent.config.enable-weaker-nested-sandbox", "true", "NVX does not support Docker-in-Docker or weaker nested sandbox options", "Remove enable-weaker-nested-sandbox or select a Docker runtime.")
	}
	if len(agentConfig.AllowHostPorts) > 0 || (workflowData != nil && workflowData.ServicePortExpressions != "") {
		return nvxValidationError("sandbox.agent.runtime", string(AgentRuntimeNVX), "NVX does not support host or host-service access", "Remove allow-host-ports and service port mappings.")
	}
	if workflowData != nil && len(workflowData.Enclaves) > 0 {
		return nvxValidationError("sandbox.agent.runtime", string(AgentRuntimeNVX), "NVX primary-agent execution is incompatible with enclaves", "Remove the enclaves configuration or select another runtime.")
	}
	if isCliProxyNeeded(workflowData) {
		return nvxValidationError("sandbox.agent.runtime", string(AgentRuntimeNVX), "NVX does not support DIFC proxy features", "Remove tools.github.mode: gh-proxy and integrity-reactions, or select another runtime.")
	}
	if firewallConfig := getFirewallConfig(workflowData); firewallConfig != nil && len(firewallConfig.Args) > 0 {
		return nvxValidationError("network.firewall.args", "", "NVX does not allow custom AWF arguments", "Remove raw AWF arguments; NVX security flags are generated by the compiler.")
	}
	return nil
}

func isNVXPathExpression(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "${{") {
		return false
	}
	end := strings.Index(value, "}}")
	return end > len("${{") && strings.TrimSpace(value[len("${{"):end]) != ""
}

func nvxValidationError(field, value, message, suggestion string) error {
	return NewValidationError(field, value, message, suggestion)
}

func effectiveNVXConfig(nvx *AgentNVXConfig) *AgentNVXConfig {
	if nvx == nil {
		return nil
	}
	result := *nvx
	if result.MountPolicy == "" {
		result.MountPolicy = defaultNVXMountPolicy
	}
	if result.SignerWorkflow == "" {
		result.SignerWorkflow = defaultNVXSignerWorkflow
	}
	if result.MemoryMiB == 0 {
		result.MemoryMiB = constants.DefaultNVXMemoryMiB
	}
	if result.MemoryMaxBytes == 0 {
		result.MemoryMaxBytes = constants.DefaultNVXMemoryMaxBytes
	}
	if result.PidsMax == 0 {
		result.PidsMax = constants.DefaultNVXPidsMax
	}
	if result.ContainerWorkDir == "" {
		result.ContainerWorkDir = defaultNVXContainerDir
	}
	return &result
}

func isNVXConfigComplete(nvx *AgentNVXConfig) bool {
	return nvx != nil &&
		nvx.PreviewEnabled &&
		nvx.NetworkIsolation &&
		nvx.APIProxy &&
		strings.TrimSpace(nvx.LayerPath) != "" &&
		strings.TrimSpace(nvx.OpenVMMPath) != "" &&
		strings.TrimSpace(nvx.KernelPath) != "" &&
		strings.TrimSpace(nvx.InitramfsPath) != "" &&
		strings.TrimSpace(nvx.ArtifactManifestPath) != "" &&
		strings.TrimSpace(nvx.ArtifactManifestBundlePath) != ""
}

func buildAWFNVXConfig(nvx *AgentNVXConfig) *AWFNVXConfig {
	effective := effectiveNVXConfig(nvx)
	if effective == nil {
		return nil
	}
	return &AWFNVXConfig{
		PreviewEnabled:             effective.PreviewEnabled,
		MountPolicy:                effective.MountPolicy,
		LayerPath:                  "${GH_AW_NVX_LAYER}",
		ArtifactManifestPath:       "${GH_AW_NVX_ARTIFACT_MANIFEST}",
		ArtifactManifestBundlePath: "${GH_AW_NVX_ARTIFACT_MANIFEST_BUNDLE}",
		SignerWorkflow:             effective.SignerWorkflow,
		OpenVMMPath:                "${GH_AW_NVX_OPENVMM}",
		KernelPath:                 "${GH_AW_NVX_KERNEL}",
		InitramfsPath:              "${GH_AW_NVX_INITRAMFS}",
		MemoryMiB:                  effective.MemoryMiB,
		MemoryMaxBytes:             effective.MemoryMaxBytes,
		PidsMax:                    effective.PidsMax,
		ScratchBytes:               effective.ScratchBytes,
	}
}

func generateNVXSetupStep(nvx *AgentNVXConfig, awfVersion string) GitHubActionStep {
	effective := effectiveNVXConfig(nvx)
	if effective == nil {
		return nil
	}
	env := map[string]string{
		"GH_AW_AWF_VERSION":                         awfVersion,
		"GH_AW_NVX_LAYER_SOURCE":                    effective.LayerPath,
		"GH_AW_NVX_OPENVMM_SOURCE":                  effective.OpenVMMPath,
		"GH_AW_NVX_KERNEL_SOURCE":                   effective.KernelPath,
		"GH_AW_NVX_INITRAMFS_SOURCE":                effective.InitramfsPath,
		"GH_AW_NVX_ARTIFACT_MANIFEST_SOURCE":        effective.ArtifactManifestPath,
		"GH_AW_NVX_ARTIFACT_MANIFEST_BUNDLE_SOURCE": effective.ArtifactManifestBundlePath,
		"GH_AW_NVX_SIGNER_WORKFLOW":                 effective.SignerWorkflow,
		"GH_AW_NVX_MOUNT_POLICY":                    effective.MountPolicy,
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	step := GitHubActionStep{
		"      - name: Verify and stage trusted NVX artifacts",
		"        env:",
	}
	for _, key := range keys {
		step = append(step, fmt.Sprintf("          %s: %s", key, strconv.Quote(env[key])))
	}
	step = append(step, `        run: bash "${RUNNER_TEMP}/gh-aw/actions/nvx_host_preflight.sh"`)
	return step
}

func generateNVXRuntimeSetupSteps(workflowData *WorkflowData) []GitHubActionStep {
	if !isNVXRuntime(workflowData) {
		return nil
	}
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil {
		return nil
	}
	return []GitHubActionStep{generateNVXSetupStep(agentConfig.NVX, getAWFVersionForSetup(workflowData))}
}
