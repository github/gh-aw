// This file provides model alias and fallback resolution for AWF (Agentic Workflow Firewall).
//
// # Model Alias Format
//
// A model payload is a map from alias name to an ordered list of model patterns:
//
//	{
//	  "sonnet": ["copilot/*sonnet*", "anthropic/*sonnet*"],
//	  "haiku":  ["copilot/*haiku*",  "anthropic/*haiku*"],
//	  "":       ["sonnet", "gpt-5"]  // default policy
//	}
//
// The syntax for each pattern entry is:
//   - "vendor/modelid" — exact vendor-scoped model name
//   - "vendor/model*id" — wildcard pattern (supports * as a glob wildcard)
//   - "alias" — reference to another alias in the same map (recursive resolution)
//
// AWF resolves aliases recursively.  Loops are not permitted.
//
// # Builtin Aliases
//
// gh-aw ships a set of builtin model aliases that cover the major model families.
// The alias data is defined in data/model_aliases.json (embedded at compile time).
// Frontmatter-defined aliases are merged on top of the builtins, allowing workflows
// to extend or override the defaults without replacing the entire mapping.

package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"unsafe"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/syncutil"
)

var modelAliasesLog = logger.New("workflow:model_aliases")

//go:embed data/model_aliases.json
var builtinModelAliasesJSON []byte

// builtinModelAliasesFile mirrors the JSON structure of data/model_aliases.json.
type builtinModelAliasesFile struct {
	Aliases map[string][]string `json:"aliases"`
}

var builtinModelAliasesLoader syncutil.OnceLoader[map[string][]string]

func loadBuiltinModelAliases() (map[string][]string, error) {
	return builtinModelAliasesLoader.Get(func() (map[string][]string, error) {
		var data builtinModelAliasesFile
		if err := json.Unmarshal(builtinModelAliasesJSON, &data); err != nil {
			return nil, fmt.Errorf("BUG: workflow: could not parse embedded model_aliases.json: %w (expected valid JSON matching the aliases schema; run 'make build' to rebuild with the latest data)", err)
		}
		return data.Aliases, nil
	})
}

// builtinOnlyAliasMap is the canonical map returned by MergeImportedModelAliases
// when there are no imported or frontmatter overrides.  It is set once via
// sync.Once so that pointer-equality checks in validateModelAliasMap can detect
// the common case and skip the redundant cycle-detection DFS.
var (
	builtinOnlyAliasMapOnce sync.Once
	builtinOnlyAliasMap     map[string][]string
	builtinOnlyAliasMapID   uintptr // unsafe map-header pointer, set once under builtinOnlyAliasMapOnce
)

// mapHeaderPointer extracts the pointer stored in the map value's header
// (the *runtime.hmap pointer).  Two map values backed by the same hash table
// return the same value.  A nil map returns 0.
//
// This relies on the Go runtime representation of map values as a single
// machine-word pointer.  This layout has been stable since Go 1.0 and is
// consistent across all supported Go versions (see runtime/map.go).  It is
// the same technique used internally by reflect.Value.Pointer() for maps.
func mapHeaderPointer(m map[string][]string) uintptr {
	return *(*uintptr)(unsafe.Pointer(&m))
}

// getBuiltinOnlyAliasMap returns the shared, read-only builtin alias map.
// The map must never be mutated by callers; it is shared across all parse calls.
func getBuiltinOnlyAliasMap() map[string][]string {
	builtinOnlyAliasMapOnce.Do(func() {
		data, err := loadBuiltinModelAliases()
		if err != nil {
			// Build-time invariant: model_aliases.json is embedded at compile time and
			// validated by TestBuiltinModelAliases; a real unmarshal failure here can only
			// follow a corrupted release build, never dynamic user input.
			panic(err)
		}
		builtinOnlyAliasMap = data
		builtinOnlyAliasMapID = mapHeaderPointer(data)
	})
	return builtinOnlyAliasMap
}

// isBuiltinOnlyAliasMap reports whether m is the shared read-only builtin alias map
// returned by getBuiltinOnlyAliasMap.  It uses unsafe map-header pointer extraction
// for identity comparison since Go maps cannot be compared with ==.
//
// It is safe to call this function before getBuiltinOnlyAliasMap has been invoked:
// in that case builtinOnlyAliasMapID is 0 and the function returns false, which is
// correct because no caller can hold a reference to the shared map before it exists.
func isBuiltinOnlyAliasMap(m map[string][]string) bool {
	id := builtinOnlyAliasMapID
	return id != 0 && mapHeaderPointer(m) == id
}

// BuiltinModelAliases returns the built-in model alias map that covers the main
// model families supported by gh-aw.  The returned map is a freshly allocated
// copy so callers may freely modify it.
//
// The alias data is loaded from data/model_aliases.json (embedded at compile time).
// Vendor aliases (patterns use * as a glob wildcard, prefer copilot gateway first):
//   - "sonnet"         → Anthropic Sonnet family
//   - "sonnet-6x"      → Sonnet family constrained to <=6x multiplier tiers (excludes 4.6+)
//   - "haiku"          → Anthropic Haiku family
//   - "opus"           → Anthropic Opus family
//   - "gpt-5"          → OpenAI GPT-5 family
//   - "gpt-5-mini"     → OpenAI GPT-5-mini family
//   - "gpt-5-nano"     → OpenAI GPT-5-nano family (ultra-lightweight)
//   - "gpt-5-codex"    → OpenAI GPT-5-Codex family
//   - "gpt-5-pro"      → OpenAI GPT-5 Pro high-capability tier
//   - "reasoning"      → OpenAI o1/o3/o4 reasoning model families
//   - "gemini-flash"   → Google Gemini Flash family (fast/lightweight)
//   - "gemini-flash-lite" → Google Gemini Flash-Lite subfamily (lowest-cost/latency)
//   - "gemini-pro"     → Google Gemini Pro family (full-capability)
//   - "deep-research"  → Google Gemini deep-research family (specialized research agents)
//
// Meta-aliases (reference other aliases; resolved recursively by AWF):
//   - "mini"  → haiku, gpt-5-mini, gpt-5-nano, gemini-flash-lite, copilot/raptor*mini*
//   - "large" → sonnet, gpt-5-pro, gpt-5, gemini-pro
//   - "any"   → copilot/*, anthropic/*, openai/*, google/*, gemini/*
//   - "agent" → sonnet-6x, gpt-5.4, gpt-5.3, gemini-pro, any
//
// Per-engine default aliases:
//   - "copilot" → agent, gpt-5.4, sonnet, gpt-5, any
//   - "claude"  → agent, sonnet-6x, haiku, any
//   - "codex"   → agent, gpt-5-codex, gpt-5, any
//   - "gemini"  → agent, gemini-pro, gemini-flash, any
//
// Panics on invalid embedded model_aliases.json data.
func BuiltinModelAliases() map[string][]string {
	data, err := loadBuiltinModelAliases()
	if err != nil {
		// Build-time invariant: model_aliases.json is embedded at compile time and
		// validated by TestBuiltinModelAliases; a real unmarshal failure here can only
		// follow a corrupted release build, never dynamic user input.
		panic(err)
	}
	// Return a fresh deep copy so callers may freely modify map entries and slices.
	result := make(map[string][]string, len(data))
	for alias, entries := range data {
		result[alias] = append([]string(nil), entries...)
	}
	return result
}

// MergeImportedModelAliases builds the final model alias map from three layers,
// with later layers overriding earlier ones (highest priority last):
//
//  1. Builtin aliases (lowest priority)
//  2. Imported workflow aliases — merged in import order; first import to define a
//     key wins among imports (same "first-wins among peers" semantics as features).
//  3. Main workflow frontmatter aliases (highest priority — main workflow file wins)
//
// ⚠ Return-value mutability contract:
//   - When both importedModels and frontmatterModels are nil/empty the function
//     returns the shared, read-only builtin alias map directly (no allocation).
//     Callers MUST NOT mutate the returned map; it is shared across all concurrent
//     parse calls.  Use isBuiltinOnlyAliasMap() to detect this case if needed.
//   - When either parameter is non-empty a freshly allocated, mutable copy is
//     returned and callers may freely modify it.
func MergeImportedModelAliases(importedModels []map[string][]string, frontmatterModels map[string][]string) map[string][]string {
	modelAliasesLog.Printf("Merging model aliases: %d import(s), %d frontmatter override(s)", len(importedModels), len(frontmatterModels))

	// Fast path: the vast majority of workflows have no imported or frontmatter model
	// aliases.  Avoid deep-copying the 52-entry builtin map (154 string slices) on every
	// ParseWorkflowFile call by returning the shared read-only builtin map directly.
	if len(importedModels) == 0 && len(frontmatterModels) == 0 {
		result := getBuiltinOnlyAliasMap()
		modelAliasesLog.Printf("Fast path: returning shared builtin alias map (%d entries)", len(result))
		return result
	}

	merged := BuiltinModelAliases()

	// Layer 2 — imported models (first import to define a key wins among imports).
	addedFromImports := 0
	for _, importedMap := range importedModels {
		for k, v := range importedMap {
			if _, exists := merged[k]; !exists {
				merged[k] = v
				addedFromImports++
			}
		}
	}
	if addedFromImports > 0 {
		modelAliasesLog.Printf("Added %d alias(es) from imports", addedFromImports)
	}

	// Layer 3 — main workflow frontmatter always wins.
	if len(frontmatterModels) > 0 {
		modelAliasesLog.Printf("Applying %d frontmatter alias override(s)", len(frontmatterModels))
	}
	maps.Copy(merged, frontmatterModels)

	modelAliasesLog.Printf("Final alias map has %d entries", len(merged))
	return merged
}

// reservedModelsKeys are keys under the models: frontmatter field that are not alias names.
var reservedModelsKeys = map[string]bool{
	"providers":                  true,
	"default-ai-credits-pricing": true,
	"allowed":                    true,
	"blocked":                    true,
}

// extractMainModelAliases returns the workflow-defined model aliases declared under the
// main workflow's models: field. Reserved keys (providers, default-ai-credits-pricing,
// allowed, blocked) keep their own meaning and are skipped.
func extractMainModelAliases(frontmatter map[string]any) map[string][]string {
	rawModels, ok := frontmatter["models"].(map[string]any)
	if !ok {
		return nil
	}
	aliases := make(map[string][]string)
	for key, value := range rawModels {
		if reservedModelsKeys[key] {
			continue
		}
		var entries []string
		switch v := value.(type) {
		case []string:
			entries = append(entries, v...)
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					entries = append(entries, s)
				}
			}
		}
		if len(entries) > 0 {
			aliases[key] = entries
		}
	}
	if len(aliases) == 0 {
		return nil
	}
	modelAliasesLog.Printf("Extracted %d workflow-defined model alias(es) from main frontmatter", len(aliases))
	return aliases
}

// modelResolutionRecordPath is the runner-written record of configured and resolved
// models, relative to /tmp/gh-aw (see MODEL_RESOLUTION_RECORD_PATH in resolve_model_alias.cjs).
const modelResolutionRecordPath = "agent/model-resolution.json"

// engineRunsSharedModelResolution reports whether the engine's harness runs the shared
// "resolve configured model" step (resolveEngineModel in resolve_model_alias.cjs) before
// it launches its CLI. Other engines receive the configured model unchanged.
func engineRunsSharedModelResolution(engineID string) bool {
	switch engineID {
	case string(constants.CopilotEngine), string(constants.ClaudeEngine), string(constants.CodexEngine), string(constants.PiEngine), string(constants.AgyEngine):
		return true
	}
	return false
}

// modelAliasesEnvVar carries the workflow-defined and imported aliases to the engine
// harness (see MODEL_ALIASES_ENV_VAR in resolve_model_alias.cjs). The built-in aliases
// ship with the setup action (actions/setup/js/model_aliases.json, a copy of
// data/model_aliases.json) and the harness overlays this variable on them at run time.
// It is set on every engine step that has such aliases, with or without AWF, so the
// unsandboxed path resolves workflow-defined aliases too.
const modelAliasesEnvVar = "GH_AW_MODEL_ALIASES"

// workflowModelAliasOverrides returns the entries of the merged alias map that differ
// from the built-ins: the workflow-defined and imported aliases that take effect. Merging
// the built-ins with the result reproduces the merged map exactly. Returns nil when the
// workflow defines and imports no effective aliases.
func workflowModelAliasOverrides(merged map[string][]string) map[string][]string {
	if len(merged) == 0 || isBuiltinOnlyAliasMap(merged) {
		return nil
	}
	builtins := getBuiltinOnlyAliasMap()
	var overrides map[string][]string
	for alias, entries := range merged {
		if builtin, ok := builtins[alias]; ok && slices.Equal(builtin, entries) {
			continue
		}
		if overrides == nil {
			overrides = make(map[string][]string)
		}
		overrides[alias] = entries
	}
	return overrides
}

// applyModelAliasOverridesEnv sets envVar to the workflow-defined and imported aliases as
// JSON, or leaves it unset when there are none.
func applyModelAliasOverridesEnv(env map[string]string, envVar string, data *WorkflowData) {
	if data == nil {
		return
	}
	overrides := workflowModelAliasOverrides(data.ModelMappings)
	if len(overrides) == 0 {
		return
	}
	aliases, err := json.Marshal(overrides)
	if err != nil {
		panic(fmt.Sprintf("BUG: cannot encode model aliases: %v", err))
	}
	env[envVar] = string(aliases)
}

// applyModelAliasesEnv sets modelAliasesEnvVar to the workflow-defined and imported aliases.
func applyModelAliasesEnv(env map[string]string, data *WorkflowData) {
	applyModelAliasOverridesEnv(env, modelAliasesEnvVar, data)
}
