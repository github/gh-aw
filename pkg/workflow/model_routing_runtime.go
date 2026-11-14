// This file implements the AWF runtime, topology, and host compatibility
// contract for primary-agent model routing.
//
// AWF routes the primary agent only with the default runc Docker runtime and,
// from AWF v0.29.12, the cloud-hypervisor runtime. It also refuses routing with
// Docker-in-Docker, agent access to the Docker socket, and non-Linux hosts. The
// compiler applies the parts of that contract it can establish from the
// resolved workflow, so the implicit routing default and explicit-routing
// validation share one decision. AWF keeps runtime enforcement for everything
// the compiler cannot know, such as the actual host platform behind opaque
// runner selections or runtime-selected socket paths.

package workflow

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
)

// modelRoutingBackendParityURL tracks AWF model-routing support for other runtimes.
const modelRoutingBackendParityURL = "https://github.com/github/gh-aw-firewall/issues/10335"

const (
	modelRoutingDockerVarRunSocketPath = "/var/run/docker.sock"
	modelRoutingDockerRunSocketPath    = "/run/docker.sock"
)

// modelRoutingDockerSocketPaths are the host Docker socket paths AWF checks for
// agent exposure.
var modelRoutingDockerSocketPaths = []string{modelRoutingDockerVarRunSocketPath, modelRoutingDockerRunSocketPath}

// githubHostedMacOSLabelPattern matches GitHub-hosted macOS runner labels.
var githubHostedMacOSLabelPattern = regexp.MustCompile(`^macos-(latest|\d+)(-(large|xlarge|intel|arm64))?$`)

// modelRoutingRuntimeConflict describes a workflow setting that AWF primary-agent
// model routing does not support.
type modelRoutingRuntimeConflict struct {
	Field  string
	Value  string
	Reason string
	Detail string
}

// modelRoutingRuntimeCompatibility returns the first runtime, topology, mount,
// or host setting that prevents AWF from routing the primary agent, or nil when
// the compiler cannot rule routing out.
func modelRoutingRuntimeCompatibility(data *WorkflowData) *modelRoutingRuntimeConflict {
	agentConfig := getAgentConfig(data)
	if agentConfig != nil && agentConfig.Disabled {
		return nil
	}
	if conflict := modelRoutingRuntimeSupportConflict(data, agentConfig); conflict != nil {
		return conflict
	}
	if isArcDindTopology(data) {
		return &modelRoutingRuntimeConflict{
			Field:  "runner.topology",
			Value:  string(RunnerTopologyArcDind),
			Reason: "model routing is not supported with Docker-in-Docker runner topology",
			Detail: "AWF refuses model routing with Docker-in-Docker and split runner/Docker filesystems. Remove runner.topology: arc-dind.",
		}
	}
	if mount := modelRoutingDockerSocketExposingMount(data, agentConfig); mount != "" {
		return &modelRoutingRuntimeConflict{
			Field:  "sandbox.agent.mounts",
			Value:  mount,
			Reason: "model routing is not supported when the agent can access the Docker socket",
			Detail: "This mount exposes the host Docker socket, or a directory containing it, to the agent. Remove the mount, or bind the socket only at /var/run/docker.sock or /run/docker.sock, where AWF masks it.",
		}
	}
	if labels, ok := nativeNonLinuxRunsOnLabels(data.RunsOn); ok {
		return &modelRoutingRuntimeConflict{
			Field:  "runs-on",
			Value:  strings.Join(labels, ", "),
			Reason: "model routing requires a Linux AWF execution host",
			Detail: "This runs-on selects a native Windows or macOS runner, where AWF cannot route. Use a Linux runner, or the literal runs-on: windows-latest, which runs AWF in WSL.",
		}
	}
	return nil
}

// modelRoutingRuntimeSupportConflict reports a sandbox runtime, or a
// cloud-hypervisor AWF version, that does not support primary-agent routing.
func modelRoutingRuntimeSupportConflict(data *WorkflowData, agentConfig *AgentSandboxConfig) *modelRoutingRuntimeConflict {
	if agentConfig == nil {
		return nil
	}
	switch agentConfig.Runtime {
	case "", AgentRuntimeDocker, AgentRuntimeDockerSudoIptables:
		return nil
	case AgentRuntimeCloudHypervisor:
		firewallConfig := getFirewallConfig(data)
		if awfVersionAtLeast(firewallConfig, constants.AWFCloudHypervisorModelRoutingMinVersion) {
			return nil
		}
		field, version := "firewall.version", string(constants.DefaultFirewallVersion)
		if firewallConfig != nil && firewallConfig.Version != "" {
			version = firewallConfig.Version
		}
		if agentConfig.Version != "" {
			field = "sandbox.agent.version"
		}
		return &modelRoutingRuntimeConflict{
			Field:  field,
			Value:  version,
			Reason: fmt.Sprintf("model routing with sandbox.agent.runtime: cloud-hypervisor requires AWF %s or newer", constants.AWFCloudHypervisorModelRoutingMinVersion),
			Detail: fmt.Sprintf("The effective AWF version is %s. Set sandbox.agent.version or firewall.version to %s or newer.", version, constants.AWFCloudHypervisorModelRoutingMinVersion),
		}
	default:
		return &modelRoutingRuntimeConflict{
			Field:  "sandbox.agent.runtime",
			Value:  string(agentConfig.Runtime),
			Reason: fmt.Sprintf("model routing is not supported with the %q sandbox runtime", agentConfig.Runtime),
			Detail: "Select the default Docker runtime (omit sandbox.agent.runtime or set docker) or cloud-hypervisor.",
		}
	}
}

// validateModelRoutingRuntimeCompatibility rejects explicit model routing on a
// runtime, topology, mount, or host that AWF refuses to route.
func validateModelRoutingRuntimeCompatibility(data *WorkflowData) error {
	if !isModelRoutingEnabled(data) {
		return nil
	}
	conflict := modelRoutingRuntimeCompatibility(data)
	if conflict == nil {
		return nil
	}
	return NewValidationError(conflict.Field, conflict.Value, conflict.Reason,
		fmt.Sprintf("%s Primary-agent model routing supports the default Docker (runc) runtime and cloud-hypervisor with AWF %s or newer, on a Linux host without Docker-in-Docker or agent Docker socket access. "+
			"Alternatively, choose a model or set engine.model-routing: false. Support for other runtimes is tracked in %s",
			conflict.Detail, constants.AWFCloudHypervisorModelRoutingMinVersion, modelRoutingBackendParityURL))
}

// modelRoutingDockerSocketExposingMount returns the first custom agent mount the
// compiler emits that exposes the host Docker socket, following AWF's
// findDockerSocketExposingMount: a bind of the socket or of any directory
// containing it exposes the socket unless it lands at the masked
// /var/run/docker.sock or /run/docker.sock path inside the agent.
func modelRoutingDockerSocketExposingMount(data *WorkflowData, agentConfig *AgentSandboxConfig) string {
	// Custom mounts are not emitted for these runtimes; see appendCustomMountArgs.
	if agentConfig == nil || isCloudHypervisorRuntime(data) || isNVXRuntime(data) {
		return ""
	}
	for _, mount := range agentConfig.Mounts {
		source, remainder, hasTarget := strings.Cut(mount, ":")
		target, _, _ := strings.Cut(remainder, ":")
		if !hasTarget || strings.Contains(mount, "${") || !path.IsAbs(source) || !path.IsAbs(target) {
			continue
		}
		source = path.Clean(source)
		for _, socket := range modelRoutingDockerSocketPaths {
			if mountPathsOverlap(source, socket) && !isMaskedDockerSocketMount(source, target, socket) {
				return mount
			}
		}
	}
	return ""
}

// mountPathContains reports whether parent is child or one of its ancestors.
func mountPathContains(parent, child string) bool {
	return parent == child || parent == "/" || strings.HasPrefix(child, parent+"/")
}

func mountPathsOverlap(left, right string) bool {
	return mountPathContains(left, right) || mountPathContains(right, left)
}

// isMaskedDockerSocketMount reports whether a bind of source at target places
// the socket at a path AWF masks inside the agent.
func isMaskedDockerSocketMount(source, target, socket string) bool {
	if !mountPathContains(source, socket) {
		return false
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(socket, source), "/")
	hostTarget := target
	if !strings.HasPrefix(target, "/host/") {
		hostTarget = "/host" + target
	}
	exposed := path.Join(hostTarget, relative)
	return exposed == "/host/var/run/docker.sock" || exposed == "/host/run/docker.sock"
}

// nativeNonLinuxRunsOnLabels returns the runs-on labels when they provably
// select a native Windows or macOS runner. The literal runs-on: windows-latest
// runs AWF in WSL and is allowed. Runner groups, expressions, and arbitrary
// self-hosted labels are not treated as proof of an operating system.
func nativeNonLinuxRunsOnLabels(runsOn string) ([]string, bool) {
	if strings.TrimSpace(runsOn) == "" || strings.TrimSpace(strings.TrimPrefix(runsOn, "runs-on:")) == "windows-latest" {
		return nil, false
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(runsOn), &parsed); err != nil {
		return nil, false
	}
	// The object form's group name is opaque; only its labels identify an OS.
	labels := extractRunnerLabels(parsed["runs-on"])
	lower := make([]string, 0, len(labels))
	for _, label := range labels {
		if strings.Contains(label, "${{") {
			return nil, false
		}
		lower = append(lower, strings.ToLower(strings.TrimSpace(label)))
	}
	if slices.Contains(lower, "self-hosted") {
		// Only GitHub's reserved operating-system labels identify a self-hosted runner's OS.
		if slices.Contains(lower, "linux") {
			return nil, false
		}
		if slices.Contains(lower, "windows") || slices.Contains(lower, "macos") {
			return labels, true
		}
		return nil, false
	}
	for _, label := range lower {
		if (strings.HasPrefix(label, "windows-") && standardGitHubHostedRunners[label]) || githubHostedMacOSLabelPattern.MatchString(label) {
			return labels, true
		}
	}
	return nil, false
}
