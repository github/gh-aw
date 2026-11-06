package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

// SampleEntry is the per-call payload consumed by apply_samples.cjs.
// Each entry corresponds to a single MCP `tools/call` invocation.
type SampleEntry struct {
	// Tool is the snake_case MCP tool name (e.g. "create_pull_request").
	Tool string `json:"tool"`
	// Arguments are passed verbatim as the MCP `tools/call` arguments.
	// Sample sidecar fields (e.g. `patch`) have already been stripped.
	Arguments map[string]any `json:"arguments"`
	// Sidecars carries fields stripped from Arguments that need out-of-band
	// pre-staging by the driver (e.g. `patch` for create_pull_request).
	Sidecars map[string]any `json:"sidecars,omitempty"`
}

type sampleRuntimeBinding struct {
	Entry int      `json:"entry"`
	Path  []string `json:"path"`
	Env   string   `json:"env"`
}

// Keep runtime values outside serialized replay control data. Bindings point
// only to authored string values, never tool names or object keys.
func collectSampleRuntimeBindings(entries []SampleEntry) ([]sampleRuntimeBinding, map[string]string) {
	var bindings []sampleRuntimeBinding
	values := make(map[string]string)
	var visit func(int, []string, any)
	visit = func(entry int, path []string, value any) {
		switch v := value.(type) {
		case string:
			if sampleRuntimeExpressionPattern.MatchString(v) {
				name := fmt.Sprintf("GH_AW_SAMPLE_VALUE_%d", len(bindings))
				bindings = append(bindings, sampleRuntimeBinding{Entry: entry, Path: slices.Clone(path), Env: name})
				values[name] = v
			}
		case map[string]any:
			for _, key := range sliceutil.SortedKeys(v) {
				visit(entry, append(path, key), v[key])
			}
		case []any:
			for i, item := range v {
				visit(entry, append(path, strconv.Itoa(i)), item)
			}
		}
	}
	for i, entry := range entries {
		visit(i, []string{"arguments"}, entry.Arguments)
		visit(i, []string{"sidecars"}, entry.Sidecars)
	}
	return bindings, values
}

// samplesFeatureName is the frontmatter feature flag that opts a single
// workflow into deterministic samples replay without passing the hidden
// `gh aw compile --use-samples` flag. It exists so that a workflow which is
// designed around `samples:` (e.g. the repository's own smoke tests) keeps a
// stable compiled lock file under a plain `gh aw compile`.
const samplesFeatureName = "samples"

// samplesFeatureEnabled reports whether the workflow frontmatter declares
// `features: { samples: true }`.
func samplesFeatureEnabled(frontmatter map[string]any) bool {
	if frontmatter == nil {
		return false
	}
	features, ok := frontmatter["features"].(map[string]any)
	if !ok {
		return false
	}
	return samplesFeatureEnabledInMap(features)
}

// samplesFeatureEnabledInMap reports whether an already-extracted `features`
// map (as opposed to a raw frontmatter document) enables samples replay.
func samplesFeatureEnabledInMap(features map[string]any) bool {
	if features == nil {
		return false
	}
	enabled, ok := features[samplesFeatureName].(bool)
	return ok && enabled
}

// samplesEnabled reports whether samples replay is active for the workflow
// being compiled, either through the hidden `--use-samples` CLI flag or
// through the per-workflow `features.samples: true` opt-in declared directly
// in the main frontmatter. It does not see `features.samples: true` declared
// only in an imported workflow; use samplesEnabledFromImports for that, once
// imported features have been merged into WorkflowData.Features.
func (c *Compiler) samplesEnabled(frontmatter map[string]any) bool {
	return c.useSamples || samplesFeatureEnabled(frontmatter)
}

// samplesEnabledFromImports reports whether samples replay is active once
// imported `features` maps have been folded into the main frontmatter's
// features. It mirrors samplesEnabled, but also honours `features.samples:
// true` declared only in an imported shared workflow, which the merge in
// mergeImportedWorkflowConfiguration performs after the main frontmatter is
// first extracted.
func (c *Compiler) samplesEnabledFromImports(frontmatter map[string]any, importedFeatures []map[string]any) bool {
	if c.samplesEnabled(frontmatter) {
		return true
	}
	return slices.ContainsFunc(importedFeatures, samplesFeatureEnabledInMap)
}

// runtimeVisibleFeatures returns a copy of features with internal
// compiler-only flags (currently just `samples`) removed. It is used
// whenever WorkflowData.Features is serialized into runtime-visible
// metadata (e.g. GH_AW_INFO_FEATURES), so that `features.samples: true`
// stays a compiler knob rather than becoming part of the observable
// runtime API.
func runtimeVisibleFeatures(features map[string]any) map[string]any {
	if len(features) == 0 {
		return nil
	}
	if _, ok := features[samplesFeatureName]; !ok {
		return features
	}
	result := make(map[string]any, len(features)-1)
	for k, v := range features {
		if k == samplesFeatureName {
			continue
		}
		result[k] = v
	}
	return result
}

// collectSampleEntries walks the safe-outputs config and flattens every
// configured `samples` entry into the order they will be sent to the MCP
// server. Iteration order is deterministic (sorted by struct field name) so
// that compiled YAML is stable across runs.
func collectSampleEntries(config *SafeOutputsConfig) []SampleEntry {
	if config == nil {
		return nil
	}

	fieldNames := getSortedSafeOutputFieldNames()

	var entries []SampleEntry
	for _, fieldName := range fieldNames {
		toolName := safeOutputFieldMapping[fieldName]
		base := extractBaseSafeOutputConfig(config, fieldName)
		if base == nil || len(base.Samples) == 0 {
			continue
		}
		sidecarKeys := sampleSidecarFields[toolName]
		for _, sample := range base.Samples {
			if dynamicEntry, ok := buildDynamicWorkflowSampleEntry(toolName, sample); ok {
				entries = append(entries, dynamicEntry)
				continue
			}

			args := make(map[string]any, len(sample))
			var sidecars map[string]any
			for k, v := range sample {
				if _, isSidecar := sidecarKeys[k]; isSidecar {
					if sidecars == nil {
						sidecars = make(map[string]any)
					}
					sidecars[k] = v
					continue
				}
				args[k] = v
			}
			entries = append(entries, SampleEntry{
				Tool:      toolName,
				Arguments: args,
				Sidecars:  sidecars,
			})
		}
	}
	for _, name := range sliceutil.SortedKeys(config.Scripts) {
		script := config.Scripts[name]
		if script == nil || script.connectorOptions == nil {
			continue
		}
		for _, sample := range script.connectorOptions.Samples {
			entries = append(entries, SampleEntry{
				Tool:      stringutil.NormalizeSafeOutputIdentifier(name),
				Arguments: maps.Clone(sample),
			})
		}
	}
	return entries
}

func buildDynamicWorkflowSampleEntry(toolName string, sample map[string]any) (SampleEntry, bool) {
	if toolName != "dispatch_workflow" && toolName != "call_workflow" {
		return SampleEntry{}, false
	}

	workflowName, _ := sample["workflow_name"].(string)
	workflowName = strings.TrimSpace(workflowName)
	if workflowName == "" {
		return SampleEntry{}, false
	}

	args := map[string]any{}
	if rawInputs, ok := sample["inputs"]; ok {
		if inputs, ok := rawInputs.(map[string]any); ok {
			maps.Copy(args, inputs)
		}
	}
	if len(args) == 0 {
		for k, v := range sample {
			if k == "workflow_name" || k == "inputs" {
				continue
			}
			args[k] = v
		}
	}
	if toolName == "dispatch_workflow" {
		if ref, ok := sample["ref"]; ok {
			args["ref"] = ref
		}
	}

	return SampleEntry{
		Tool:      stringutil.NormalizeSafeOutputIdentifier(workflowName),
		Arguments: args,
	}, true
}

// collectSampleRepoTokens walks the workflow's checkout configs and returns
// a map of "owner/repo" -> token expression so that apply_samples.cjs can
// pick the right token when calling the GitHub REST API to resolve a PR head
// ref for a cross-repo sample.
//
// The keys are repository slugs as written in the workflow frontmatter; the
// value is the GitHub Actions expression that resolves to the token at
// runtime — for plain `github-token: ${{ secrets.X }}` checkouts this is the
// expression itself, and for `github-app:` checkouts it is the
// `${{ steps.checkout-app-token-N.outputs.token }}` reference for the same
// app-token step that the agent job's checkout uses. Entries whose checkout
// declares no auth are omitted; the driver falls back to GITHUB_TOKEN for
// those.
func collectSampleRepoTokens(configs []*CheckoutConfig) map[string]string {
	if len(configs) == 0 {
		return nil
	}
	cm := NewCheckoutManager(configs)
	tokens := make(map[string]string)
	for i, entry := range cm.ordered {
		repo := entry.key.repository
		if repo == "" {
			// The repo slug is unknown at compile time; emit a GitHub Actions
			// expression. The Actions runner expands ${{ github.repository }} to
			// "owner/repo" before apply_samples.cjs reads GH_AW_REPO_TOKENS, so
			// the runtime key matches the slug the driver looks up.
			repo = "${{ github.repository }}"
		}
		var token string
		switch {
		case entry.githubApp != nil:
			//nolint:gosec // G101: GitHub Actions expression template, not a hardcoded credential
			token = fmt.Sprintf("${{ steps.checkout-app-token-%d.outputs.token }}", i)
		case entry.token != "":
			token = entry.token
		default:
			continue
		}
		// First-seen wins so the per-repo entry from the user's frontmatter
		// takes precedence over later imported configs (CheckoutManager
		// already enforces this for merged entries; this guards against
		// distinct entries that share a repo but differ in path).
		if _, exists := tokens[repo]; !exists {
			tokens[repo] = token
		}
	}
	if len(tokens) == 0 {
		return nil
	}
	return tokens
}

// marshalRepoTokens returns a compact JSON object encoding of m, or nil for
// empty/nil maps so callers can skip emission. encoding/json sorts string-
// keyed map entries lexicographically, so the output is byte-stable across
// runs without an explicit sort step.
func marshalRepoTokens(m map[string]string) []byte {
	if len(m) == 0 {
		return nil
	}
	out, _ := json.Marshal(m) //nolint:jsonmarshalignoredeerror // marshaling a string map cannot fail
	return out
}

// writeSampleReplayInputEnvVars forwards GH_AW_INPUT_* vars referenced by the
// safe-outputs config so the replay's safe_outputs_config.cjs can resolve
// ${GH_AW_INPUT_…} placeholders (e.g. push-to-pull-request-branch's
// `target: ${{ inputs.pull_request_number }}`), the same way the agentic
// execution step does via applySafeOutputEnvToMap.
func writeSampleReplayInputEnvVars(yaml *strings.Builder, inputEnvVars map[string]string) {
	if len(inputEnvVars) == 0 {
		return
	}
	names := make([]string, 0, len(inputEnvVars))
	for name := range inputEnvVars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(yaml, "          %s: %s\n", name, inputEnvVars[name])
	}
}

func writeSampleReplayPayloadEnv(yaml *strings.Builder, entries []SampleEntry, payload []byte) {
	bindings, sampleValues := collectSampleRuntimeBindings(entries)
	// Only the driver decodes JSON escapes; Actions cannot expand control data.
	staticPayload := strings.ReplaceAll(string(payload), "$", `\u0024`)
	yaml.WriteString("          GH_AW_SAMPLES: |\n")
	for line := range strings.SplitSeq(staticPayload, "\n") {
		yaml.WriteString("            " + line + "\n")
	}
	if len(bindings) == 0 {
		return
	}
	bindingJSON, _ := json.Marshal(bindings) //nolint:jsonmarshalignoredeerror // bindings contain only integers and strings.
	yaml.WriteString("          GH_AW_SAMPLE_BINDINGS: |\n            " + strings.ReplaceAll(string(bindingJSON), "$", `\u0024`) + "\n")
	for _, binding := range bindings {
		writeYAMLEnv(yaml, "          ", binding.Env, sampleValues[binding.Env])
	}
}

// generateSamplesReplayStep emits the YAML that replaces the agentic
// `Execute coding agent` step when the hidden `gh aw compile --use-samples`
// flag is used. It spawns the safe-outputs MCP server over stdio and feeds it
// a `tools/call` for every collected sample, after pre-staging branches/patches
// for samples that carry them.
func (c *Compiler) generateSamplesReplayStep(yaml *strings.Builder, data *WorkflowData, logFile string) {
	entries := collectSampleEntries(data.SafeOutputs)
	compilerYamlLog.Printf("Generating samples replay step: entries=%d", len(entries))

	// Normalize a nil slice to an empty slice so json.Marshal emits "[]" not "null".
	// The driver rejects anything that isn't a JSON array; emitting "null" here
	// would crash the replay step with `GH_AW_SAMPLES must be a JSON array` for
	// workflows that opt into --use-samples but configure no samples (or whose
	// configured samples all live on disabled handlers).
	if entries == nil {
		entries = []SampleEntry{}
	}

	// Serialize entries to JSON for the driver. Always emit valid JSON even when
	// empty so the driver can produce a clear `no samples configured` message
	// rather than crashing on an empty env var.
	payload, err := json.Marshal(entries)
	if err != nil {
		// Should never happen for map[string]any payloads; fall back to empty
		// array so the workflow still compiles and the driver reports cleanly.
		compilerYamlLog.Printf("Warning: failed to marshal samples entries: %v", err)
		payload = []byte("[]")
	}
	// Build the per-repo token map so apply_samples.cjs can reach repos that
	// require a non-default token (cross-repo `checkout:` entries with their
	// own `github-token:` or `github-app:`).
	repoTokens := collectSampleRepoTokens(data.CheckoutConfigs)
	repoTokensPayload := marshalRepoTokens(repoTokens)

	yaml.WriteString("      - name: Replay safe-outputs samples (deterministic)\n")
	yaml.WriteString("        id: agentic_execution\n")
	yaml.WriteString("        env:\n")
	writeSampleReplayPayloadEnv(yaml, entries, payload)
	fmt.Fprintf(yaml, "          GH_AW_AGENT_STDIO_LOG: %s\n", logFile)
	yaml.WriteString("          GH_AW_SAFE_OUTPUTS_CONFIG_PATH: ${{ runner.temp }}/gh-aw/safeoutputs/config.json\n")
	yaml.WriteString("          GH_AW_SAFE_OUTPUTS: ${{ runner.temp }}/gh-aw/safeoutputs/outputs.jsonl\n")
	writeSampleReplayInputEnvVars(yaml, data.SafeOutputsInputEnvVars)
	// GITHUB_TOKEN is the fallback used by apply_samples.cjs when resolving a
	// pull-request head ref via the REST API for issue_comment / slash_command
	// events. For cross-repo samples whose target repository has its own
	// `checkout:` entry with `github-token:` or `github-app:`, the driver
	// prefers the matching token from GH_AW_REPO_TOKENS below.
	yaml.WriteString("          GITHUB_TOKEN: ${{ github.token }}\n")
	if repoTokensPayload != nil {
		yaml.WriteString("          GH_AW_REPO_TOKENS: |\n")
		for line := range strings.SplitSeq(string(repoTokensPayload), "\n") {
			fmt.Fprintf(yaml, "            %s\n", line)
		}
	}
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          set -euo pipefail\n")
	yaml.WriteString("          mkdir -p \"$(dirname \"$GH_AW_AGENT_STDIO_LOG\")\"\n")
	yaml.WriteString("          node \"${RUNNER_TEMP}/gh-aw/actions/apply_samples.cjs\"\n")
}

// safeOutputsMissingSamples returns the frontmatter keys of enabled,
// non-builtin safe outputs that declare no `samples:` entries. Under samples
// replay these handlers are never exercised: the agent is replaced by the
// deterministic driver, so a handler without samples silently produces no
// output at all. The keys are returned in a deterministic (sorted) order.
func safeOutputsMissingSamples(config *SafeOutputsConfig) []string {
	if config == nil {
		return nil
	}
	var keys []string
	for _, handler := range safeOutputHandlers {
		if handler.Builtin || handler.Key == "" || handler.StructField == "" || handler.ToolName == "" {
			continue
		}
		base := extractBaseSafeOutputConfig(config, handler.StructField)
		if base == nil || len(base.Samples) > 0 {
			continue
		}
		keys = append(keys, toolDisplayKey(handler.Key))
	}
	for name, script := range config.Scripts {
		if script != nil && script.connectorOptions != nil && len(script.connectorOptions.Samples) == 0 {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}
