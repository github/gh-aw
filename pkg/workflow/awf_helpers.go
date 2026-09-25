// This file keeps shared AWF (Agentic Workflow Firewall) scaffolding used by
// the focused AWF helper modules in this package.
//
// Command assembly, environment filtering, ARC/DinD handling, and feature gates
// live in awf_command_builder.go, awf_env.go, awf_arc_dind.go, and
// awf_feature_flags.go respectively.

package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var awfHelpersLog = logger.New("workflow:awf_helpers")

const (
	awfDockerHostVarName       = "GH_AW_DOCKER_HOST"
	awfToolCacheMountVarName   = "GH_AW_TOOL_CACHE_MOUNT"
	awfMaxAICreditsVarName     = "GH_AW_MAX_AI_CREDITS"
	awfConfigRuntimePathExpr   = "${RUNNER_TEMP}/gh-aw/awf-config.json"
	awfModelsJSONPathExpr      = "/tmp/gh-aw/models.json"
	awfArcDindRootPathExpr     = "${RUNNER_TEMP}/gh-aw"
	awfArcDindHomePathExpr     = "${RUNNER_TEMP}/gh-aw/home"
	awfArcDindProxyLogsDirExpr = "${RUNNER_TEMP}/gh-aw/sandbox/firewall/logs"
	awfArcDindAuditDirExpr     = "${RUNNER_TEMP}/gh-aw/sandbox/firewall/audit"
	// Bash regex used in [[ ... =~ ... ]] to detect TCP Docker hosts (ARC/DinD).
	// Any tcp:// DOCKER_HOST indicates the Docker daemon runs on a separate filesystem,
	// requiring --docker-host so AWF connects to the correct daemon.
	// This covers localhost, pod IPs, K8s service names (e.g., tcp://dind:2375), and
	// any other TCP Docker daemon configuration.
	awfArcDindDockerHostRegex = `^tcp://`

	// awfArcDindChrootBinariesSourcePath is the runner-side directory that AWF overlays
	// at /usr/local/bin inside chroot mode for ARC/DinD split-filesystem runners.
	// This is the gh-aw staging directory that holds pre-downloaded binaries (e.g., copilot).
	awfArcDindChrootBinariesSourcePath = awfArcDindRootPathExpr

	// awfArcDindChrootIdentityHome is the home directory path exported inside chroot mode
	// for ARC/DinD runners. A dedicated directory under ${RUNNER_TEMP}/gh-aw is used so that the
	// runner user has a consistent home that exists on the daemon-visible filesystem.
	awfArcDindChrootIdentityHome = awfArcDindHomePathExpr

	// awfShellcheckDirective suppresses shellcheck warnings only on the generated AWF
	// invocation line:
	//   - SC1003 is expected because generated GitHub expression literals can include
	//     single quotes (for example ports['<port>']) and must survive unchanged.
	//   - SC2016 is expected because ${RUNNER_TEMP} and similar runtime variables appear
	//     inside the single-quoted bash -c '...' argument intentionally — they are expanded
	//     by the outer runner shell before AWF receives them, not by the inner bash -c.
	//   - SC2086 is expected because compiler-owned AWF argument fragments are emitted
	//     as intentional expandable shell snippets (for example ${GH_AW_TOOL_CACHE_MOUNT:+...}
	//     and ${GH_AW_DOCKER_HOST:+...}).
	//
	// User-controlled values remain quoted via shellEscapeArg/shellJoinArgs.
	awfShellcheckDirective = "# shellcheck disable=SC1003,SC2016,SC2086"
)

// AWFCommandConfig contains configuration for building AWF commands.
// This struct centralizes all the parameters needed to construct an AWF-wrapped command.
type AWFCommandConfig struct {
	// EngineName is the configured engine ID (e.g., "copilot", "claude", "codex")
	EngineName string

	// EngineRuntimeID is the resolved runtime engine ID when EngineName is an alias
	// or behavior-defined engine. When empty, EngineName is used.
	EngineRuntimeID string

	// EngineCommand is the command to execute inside AWF
	EngineCommand string

	// LogFile is the path to the log file
	LogFile string

	// WorkflowData contains all workflow configuration
	WorkflowData *WorkflowData

	// UsesTTY indicates if the engine requires a TTY (e.g., Claude)
	UsesTTY bool

	// AllowedDomains is the comma-separated list of allowed domains
	AllowedDomains string

	// PathSetup is optional shell commands to run before the engine command
	// (e.g., npm PATH setup)
	PathSetup string

	// ExcludeEnvVarNames is the list of environment variable names to exclude from
	// the agent container's visible environment via --exclude-env. These are the env
	// var keys whose step-env values contain secret references (${{ secrets.* }}).
	// Computed from the engine's GetRequiredSecretNames() so that every secret-bearing
	// variable is excluded — the agent can never read raw token values via `env`/`printenv`.
	// Requires AWF v0.25.3+ for --exclude-env support.
	ExcludeEnvVarNames []string

	// ResolveMaxAICreditsFromEnv switches maxAiCredits runtime resolution from an inline
	// GitHub Actions expression in run: to the GH_AW_MAX_AI_CREDITS step env variable.
	// When true and max-ai-credits is unset, BuildAWFCommand emits:
	//   GH_AW_MAX_AI_CREDITS="${GH_AW_MAX_AI_CREDITS:-<default>}"
	// instead of embedding ${{ vars.* }} directly in run:.
	ResolveMaxAICreditsFromEnv bool

	// RetryStartupFailures retries AWF startup/configuration failures before the
	// engine harness has started. This is used by engine harnesses so failures
	// outside the harness still consume the bounded startup retry budget.
	RetryStartupFailures bool
}

func shouldUseWorkflowCallNetworkAllowedInput(data *WorkflowData) bool {
	result := data != nil &&
		data.NetworkPermissions != nil &&
		data.NetworkPermissions.AllowedInput &&
		hasWorkflowCallTrigger(data.On)
	awfHelpersLog.Printf("shouldUseWorkflowCallNetworkAllowedInput: result=%v", result)
	return result
}

func buildModelsJSONPathExportScript(isArcDind bool) string {
	modelsJSONPathExpr := awfModelsJSONPathExpr
	if isArcDind {
		modelsJSONPathExpr = awfArcDindRootPathExpr + "/models.json"
	}
	awfHelpersLog.Printf("buildModelsJSONPathExportScript: isArcDind=%v, path=%s", isArcDind, modelsJSONPathExpr)
	return fmt.Sprintf(`export GH_AW_MODELS_JSON_PATH="%s"`, modelsJSONPathExpr)
}

func buildWorkflowCallNetworkAllowedUpdateScript() (string, error) {
	ecosystemDomains := getLoadedEcosystemDomains()
	awfHelpersLog.Printf("buildWorkflowCallNetworkAllowedUpdateScript: ecosystems=%d, compoundEcosystems=%d", len(ecosystemDomains), len(compoundEcosystems))
	ecosystemMap := make(map[string][]string, typeutil.SafeAllocationCapacity(len(ecosystemDomains), len(compoundEcosystems)))
	for ecosystem := range ecosystemDomains {
		ecosystemMap[ecosystem] = getEcosystemDomains(ecosystem)
	}
	for ecosystem := range compoundEcosystems {
		ecosystemMap[ecosystem] = getEcosystemDomains(ecosystem)
	}

	ecosystemJSON, err := json.Marshal(ecosystemMap)
	if err != nil {
		awfHelpersLog.Printf("buildWorkflowCallNetworkAllowedUpdateScript: failed to marshal ecosystem map: %v", err)
		return "", fmt.Errorf("marshal network allowed ecosystem map: %w", err)
	}

	// Pass the ecosystem map JSON via an env var and invoke the JavaScript
	// implementation deployed by actions/setup to ${RUNNER_TEMP}/gh-aw/actions/.
	// Using node avoids any Python dependency and eliminates quote-injection risk:
	// shellEscapeArg safely single-quotes and escapes the JSON payload.
	return fmt.Sprintf(`GH_AW_ECOSYSTEM_MAP_JSON=%s node "${RUNNER_TEMP}/gh-aw/actions/update_network_allowed.cjs"`,
		shellEscapeArg(string(ecosystemJSON))), nil
}
