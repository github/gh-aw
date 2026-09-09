// Package workflow - stateless predicates and utility helpers for threat detection.
package workflow

import (
	"encoding/json"
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var threatLog = logger.New("workflow:threat_detection")

// detectionStepCondition is the if condition applied to inline detection steps.
// Detection steps only run when the detection guard determines there's output to analyze.
const detectionStepCondition = "always() && steps.detection_guard.outputs.run_detection == 'true'"

// stepEnvIndent is the indentation prefix used for env var lines in rendered step YAML.
const stepEnvIndent = "        "

// resolvedThreatDetectionDir returns the threat-detection working directory used by
// host-side steps in the detection job (prepare, mount, upload, conclude). On ARC/DinD
// topology, the AWF chroot's engine command has every /tmp/gh-aw reference rewritten to
// ${RUNNER_TEMP}/gh-aw (see rewriteArcDindPath), because the chroot filesystem lives on
// a separate Docker-in-Docker daemon that only shares the ${RUNNER_TEMP}/gh-aw volume
// with the runner host. Host-side steps that read/write files the threat-detect binary
// itself writes from inside that chroot must therefore agree on the same rewritten path;
// otherwise a read-write mount or a later read looks at a directory the container never
// touched. Non-ARC/DinD topologies keep the original /tmp/gh-aw path since AWF mounts and
// rewrites are not involved there.
func resolvedThreatDetectionDir(data *WorkflowData) string {
	if isArcDindTopology(data) {
		return rewriteArcDindPath(constants.ThreatDetectionDir)
	}
	return constants.ThreatDetectionDir
}

// resolvedThreatDetectionResultPath is the topology-aware counterpart of
// constants.ThreatDetectionResultPath; see resolvedThreatDetectionDir for rationale.
func resolvedThreatDetectionResultPath(data *WorkflowData) string {
	if isArcDindTopology(data) {
		return rewriteArcDindPath(constants.ThreatDetectionResultPath)
	}
	return constants.ThreatDetectionResultPath
}

// rewriteArcDindPathExpr is the YAML-expression counterpart of rewriteArcDindPath. Steps
// that read `run:` shell scripts can rely on ${RUNNER_TEMP} shell expansion, but YAML
// `with:` blocks (e.g. actions/upload-artifact's `path:` list) are not executed in a
// shell — only GitHub Actions' own ${{ }} expressions are expanded there. This substitutes
// the double-brace ${{ runner.temp }} form instead so paths embedded in `with:` blocks
// resolve correctly.
func rewriteArcDindPathExpr(path string) string {
	return strings.ReplaceAll(path, constants.TmpGhAwDir, constants.GhAwRootDir)
}

// resolvedThreatDetectionResultPathExpr is the YAML with:-block counterpart of
// resolvedThreatDetectionResultPath (see rewriteArcDindPathExpr).
func resolvedThreatDetectionResultPathExpr(data *WorkflowData) string {
	if isArcDindTopology(data) {
		return rewriteArcDindPathExpr(constants.ThreatDetectionResultPath)
	}
	return constants.ThreatDetectionResultPath
}

// IsDetectionJobEnabled reports whether a detection job should be created for
// the given safe-outputs configuration. This is the single source of truth
// used by all codepaths that decide whether to create, depend on, or reference
// the detection job.
func IsDetectionJobEnabled(so *SafeOutputsConfig) bool {
	return so != nil && so.ThreatDetection != nil && so.ThreatDetection.HasRunnableDetection()
}

// IsConditionalDetection reports whether the safe-outputs configuration uses an expression
// to control threat detection at runtime. When true, the detection job is always compiled
// but may be skipped at runtime; downstream jobs must handle the skipped result.
func IsConditionalDetection(so *SafeOutputsConfig) bool {
	return so != nil && so.ThreatDetection != nil && so.ThreatDetection.IsConditional()
}

// isThreatDetectionExplicitlyDisabledInConfigs checks whether any of the provided
// safe-outputs config JSON strings has threat-detection explicitly set to disabled.
// Supports both the boolean form (threat-detection: false) and the object form
// (threat-detection: { enabled: false }), mirroring parseThreatDetectionConfig.
// This is used to determine whether the default detection should be applied when
// safe-outputs comes from imports/includes (i.e. no safe-outputs: section in the
// main workflow frontmatter).
func isThreatDetectionExplicitlyDisabledInConfigs(configs []string) bool {
	for _, configJSON := range configs {
		if configJSON == "" || configJSON == "{}" {
			continue
		}
		var config map[string]any
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			continue
		}
		if tdVal, exists := config["threat-detection"]; exists {
			// Boolean form: threat-detection: false
			if tdBool, ok := tdVal.(bool); ok && !tdBool {
				threatLog.Print("Threat detection explicitly disabled (boolean form) in safe-outputs config")
				return true
			}
			// Object form: threat-detection: { enabled: false }
			if tdMap, ok := tdVal.(map[string]any); ok {
				if enabled, exists := tdMap["enabled"]; exists {
					if enabledBool, ok := enabled.(bool); ok && !enabledBool {
						threatLog.Print("Threat detection explicitly disabled (object form) in safe-outputs config")
						return true
					}
				}
			}
		}
	}
	return false
}

func getThreatDetectionAdditionalAllowedDomains(data *WorkflowData) []string {
	if data == nil || data.NetworkPermissions == nil {
		return []string{}
	}

	// Evaluate the effective merged detection environment (main + detection-specific
	// overrides) so that a custom base URL configured only in
	// safe-outputs.threat-detection.engine.env also triggers domain propagation.
	var detectionSpecificEnv map[string]string
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && data.SafeOutputs.ThreatDetection.EngineConfig != nil {
		detectionSpecificEnv = data.SafeOutputs.ThreatDetection.EngineConfig.Env
	}
	effectiveEnv := mergeThreatDetectionEngineEnv(data, detectionSpecificEnv)

	hasCustomTarget := effectiveEnv["OPENAI_BASE_URL"] != "" ||
		effectiveEnv["ANTHROPIC_BASE_URL"] != "" ||
		effectiveEnv[constants.CopilotProviderBaseURL] != ""
	if !hasCustomTarget {
		return []string{}
	}

	additional := make([]string, 0, len(data.NetworkPermissions.Allowed))
	seen := make(map[string]struct{})
	for _, entry := range data.NetworkPermissions.Allowed {
		if entry == "" || strings.Contains(entry, "${{") {
			continue
		}
		if len(getEcosystemDomains(entry)) > 0 {
			continue
		}
		if _, exists := seen[entry]; exists {
			continue
		}
		seen[entry] = struct{}{}
		additional = append(additional, entry)
	}

	threatLog.Printf("Computed %d additional allowed domains for threat detection", len(additional))
	return additional
}

// mergeThreatDetectionEngineEnv composes detection engine env vars from the main
// engine env and detection-specific overrides.
//
// Detection values take precedence when keys overlap. When detectionEnv is empty,
// it still returns a copy of the main env map to avoid aliasing/mutation of the
// parent WorkflowData.EngineConfig.Env by downstream detection-specific updates.
func mergeThreatDetectionEngineEnv(data *WorkflowData, detectionEnv map[string]string) map[string]string {
	if data == nil || data.EngineConfig == nil || len(data.EngineConfig.Env) == 0 {
		return detectionEnv
	}
	if len(detectionEnv) == 0 {
		// Return a copy (not the original map) so subsequent detection-specific
		// env merges cannot mutate the main engine's env map by aliasing.
		return maps.Clone(data.EngineConfig.Env)
	}

	merged := make(map[string]string, len(data.EngineConfig.Env)+len(detectionEnv))
	maps.Copy(merged, data.EngineConfig.Env)
	maps.Copy(merged, detectionEnv)
	return merged
}

// buildExternalDetectorWorkflowData creates the base WorkflowData for an external
// detector step. It calls buildThreatDetectionWorkflowData and then applies the
// detection engine config, env, and APITarget inheritance that is shared by both
// the install step and the execution step. Callers add step-specific overrides
// (such as network permissions or mounts) on top of the returned value.
func buildExternalDetectorWorkflowData(data *WorkflowData, engineID string) *WorkflowData {
	d := buildThreatDetectionWorkflowData(data, engineID)
	d.Tools = map[string]any{
		"bash": []any{"*"},
	}
	d.EngineConfig = resolveExternalDetectorEngineConfig(data, engineID)
	// threat-detect currently resolves the selected engine by its standard
	// executable name and has no custom-command option. Keep the standard
	// Copilot CLI installed for external detection rather than propagating a
	// command that threat-detect cannot consume.
	if engineID == "copilot" && d.EngineConfig.Command != "" {
		threatLog.Printf("Ignoring custom Copilot command for external detector; threat-detect requires the standard Copilot CLI")
		d.EngineConfig.Command = ""
	}
	if engineID == "codex" && NewCodexEngine().ResolveLLMProvider(d) != LLMProviderGitHub {
		d.EngineConfig.LLMProvider = LLMProviderOpenAI
	}
	d.EngineConfig.Env = mergeThreatDetectionEngineEnv(data, d.EngineConfig.Env)
	if d.EngineConfig.APITarget == "" && data.EngineConfig != nil {
		d.EngineConfig.APITarget = data.EngineConfig.APITarget
	}
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && data.SafeOutputs.ThreatDetection.MaxAICredits != 0 {
		d.EngineConfig.MaxAICredits = data.SafeOutputs.ThreatDetection.MaxAICredits
	}
	if d.EngineConfig.HarnessMaxRetries == "" {
		d.EngineConfig.HarnessMaxRetries = "0"
	}
	return d
}

// resolveExternalDetectorEngineConfig determines the EngineConfig used to install and
// execute the engine on the external detector path. Precedence:
//  1. An explicit safe-outputs.threat-detection.engine override — cloned with its ID
//     normalized to the resolved detection engine ID (handles cases like the pi->copilot
//     detection normalization where the override's declared ID differs from the engine
//     actually used).
//  2. No override configured and the resolved detection engine matches the main
//     engine — inherit Version/Config/Args/HarnessScript/Driver from the main engine
//     config. This mirrors the inline detection path (buildDetectionEngineExecutionStep)
//     and ensures behavior-defined engines (e.g. a pinned npm package version declared
//     via a shared engine definition's default Version) install the same version in the
//     detection job as in the main agent job, instead of silently falling back to the
//     package's "latest" version.
//  3. Otherwise, a minimal config containing only the resolved engine ID.
func resolveExternalDetectorEngineConfig(data *WorkflowData, engineID string) *EngineConfig {
	hasThreatDetectionEngineOverride := data.SafeOutputs != nil &&
		data.SafeOutputs.ThreatDetection != nil &&
		data.SafeOutputs.ThreatDetection.EngineConfig != nil
	if hasThreatDetectionEngineOverride {
		return cloneThreatDetectionEngineConfig(engineID, data.SafeOutputs.ThreatDetection.EngineConfig)
	}
	if data.EngineConfig != nil && (data.EngineConfig.ID == "" || data.EngineConfig.ID == engineID) {
		return &EngineConfig{
			ID:                       engineID,
			Version:                  data.EngineConfig.Version,
			Command:                  data.EngineConfig.Command,
			LLMProvider:              data.EngineConfig.LLMProvider,
			Config:                   data.EngineConfig.Config,
			Args:                     data.EngineConfig.Args,
			HarnessScript:            data.EngineConfig.HarnessScript,
			Driver:                   data.EngineConfig.Driver,
			HarnessMaxRetries:        data.EngineConfig.HarnessMaxRetries,
			HarnessInitialDelayMs:    data.EngineConfig.HarnessInitialDelayMs,
			HarnessBackoffMultiplier: data.EngineConfig.HarnessBackoffMultiplier,
			HarnessMaxDelayMs:        data.EngineConfig.HarnessMaxDelayMs,
			HarnessWatchdogTimeoutMs: data.EngineConfig.HarnessWatchdogTimeoutMs,
		}
	}
	return &EngineConfig{ID: engineID}
}

// cloneThreatDetectionEngineConfig returns a shallow copy of source with engine ID
// normalized to the provided detection engineID. If source is nil, it returns a
// minimal config containing only the ID.
func cloneThreatDetectionEngineConfig(engineID string, source *EngineConfig) *EngineConfig {
	if source == nil {
		return &EngineConfig{ID: engineID}
	}
	cloned := *source
	cloned.ID = engineID
	return &cloned
}

// engineCoreSecretVarNames returns the secret-backed env var names for the given engine ID
// that must be excluded from the AWF container via --exclude-env. These are the credentials
// that AWF's API proxy intercepts, so the container itself does not need them.
func engineCoreSecretVarNames(engineID string) []string {
	switch engineID {
	case "copilot":
		return []string{"COPILOT_GITHUB_TOKEN"}
	case "claude":
		return []string{"ANTHROPIC_API_KEY"}
	case "codex":
		return []string{"OPENAI_API_KEY", "CODEX_API_KEY", "COPILOT_GITHUB_TOKEN"}
	case "gemini":
		return []string{"GEMINI_API_KEY"}
	default:
		return []string{}
	}
}

// inheritedDetectionModel returns the main workflow model when the detection engine can
// interpret it. Workflows using a custom engine run detection on a built-in engine that
// does not understand the custom engine's model IDs, so their model is not inherited and
// the detection engine's own default model is used instead.
func inheritedDetectionModel(data *WorkflowData) string {
	if data == nil {
		return ""
	}
	engineID := ResolveEngineID(data)
	if engineID == "" || engineID == "pi" || isThreatDetectionCapableEngineID(engineID) {
		return data.Model
	}
	return ""
}
