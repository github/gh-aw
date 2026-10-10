// This file implements compile-time validation of the Model Alias Format (MAF)
// as specified in docs/src/content/docs/specs/model-alias-specification.md.
//
// # Validation Rules Implemented
//
//   - V-MAF-001: Reject model identifiers not conforming to the grammar (Section 4.1).
//   - V-MAF-002: Reject effort values not in {low, medium, high}.
//   - V-MAF-003: Reject temperature values outside [0.0, 2.0].
//   - V-MAF-004: Reject glob patterns in engine.model.
//   - V-MAF-005: Reject alias keys containing "/", "?", or "&".
//   - V-MAF-006: Reject identifiers with characters outside the allowed set;
//     error message MUST name the offending character and segment type.
//   - V-MAF-010: Detect and report circular alias references (DFS, compile time).
//   - V-MAF-011: Emit a warning for unrecognised parameter keys.
//
// # Entry Point
//
//   - validateModelAliasMap() is called from ParseWorkflowFile (compiler_orchestrator_workflow.go)
//     after ModelMappings is populated.

package workflow

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var modelAliasValidationLog = logger.New("workflow:model_alias_validation")

// builtinCycleCheckOnce caches the result of the first cycle-detection DFS over
// the pure builtin alias map.  The builtins are a compile-time constant so cycles
// can only appear once; checking them on every ParseWorkflowFile call is wasteful.
var (
	builtinCycleCheckOnce sync.Once
	builtinCycleCheckErr  error
)

// ─── Known-parameter validation ───────────────────────────────────────────────

// ValidateEffortParam validates the "effort" parameter value (V-MAF-002).
// Allowed values: low, medium, high.
func ValidateEffortParam(value string) error {
	switch value {
	case "low", "medium", "high":
		return nil
	default:
		return fmt.Errorf("model parameter 'effort': value %q is not valid; allowed values are: low, medium, high (V-MAF-002)", value)
	}
}

// ValidateTemperatureParam validates the "temperature" parameter value (V-MAF-003).
// Must be a finite decimal float in [0.0, 2.0].
func ValidateTemperatureParam(value string) error {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("model parameter 'temperature': value %q cannot be parsed as a decimal float (V-MAF-003)", value)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("model parameter 'temperature': value %q is not a finite number (V-MAF-003)", value)
	}
	if f < 0.0 || f > 2.0 {
		return fmt.Errorf("model parameter 'temperature': value %q is out of range; must be in [0.0, 2.0] (V-MAF-003)", value)
	}
	return nil
}

// ValidateKnownParams validates the known parameters in a parsed identifier.
// Unknown parameters are tolerated (V-MAF-011 emits a warning, not an error).
// Returns an error if a known parameter has an invalid value.
func ValidateKnownParams(params map[string]string) error {
	if v, ok := params[modelParamEffort]; ok {
		if err := ValidateEffortParam(v); err != nil {
			return err
		}
	}
	if v, ok := params[modelParamTemperature]; ok {
		if err := ValidateTemperatureParam(v); err != nil {
			return err
		}
	}
	return nil
}

// validateModelAliasMap is the main entry point for compile-time model-alias validation.
// It validates:
//   - The user-supplied alias map entries in frontmatterModels (V-MAF-001..006, V-MAF-011).
//   - The engine.model value (V-MAF-001, V-MAF-004, V-MAF-006).
//   - Circular references across the fully-merged alias map (V-MAF-010).
//
// frontmatterModels contains only the aliases declared in the main workflow's
// frontmatter (not builtins or imports). Cycle detection runs over the full
// mergedAliasMap so that cycles spanning multiple layers are also caught.
//
// Returns a non-nil error (causing compilation to abort) for hard violations.
// Warnings are printed to stderr via the compiler's warning counter.
func (c *Compiler) validateModelAliasMap(
	mergedAliasMap map[string][]string,
	frontmatterModels map[string][]string,
	engineModel string,
	markdownPath string,
) error {
	modelAliasValidationLog.Printf("Validating model alias map: %d merged entries, %d frontmatter entries, engine.model=%q",
		len(mergedAliasMap), len(frontmatterModels), engineModel)

	// V-MAF-004: engine.model MUST NOT contain a glob pattern ("*").
	// The check is always performed, but only on the *literal* parts of the
	// value — expression segments (${{ ... }}) are stripped first so that a
	// "*" inside an expression body (e.g. "${{ contains(inputs.m, '*') }}")
	// is not falsely flagged.  A "*" that appears *outside* an expression
	// (e.g. "${{ inputs.model }}*" or "copilot/*${{ inputs.model }}") is
	// still a glob and must be rejected.
	if engineModel != "" {
		literalText := ExpressionPattern.ReplaceAllString(engineModel, "")
		if strings.Contains(literalText, "*") {
			return formatCompilerError(markdownPath, "error",
				fmt.Sprintf("engine.model: glob patterns are not allowed in engine.model; "+
					"got %q — glob patterns may only appear in models alias list entries (V-MAF-004)", engineModel),
				nil)
		}
		// Syntax and parameter checks ($-character parsing, known params) are
		// skipped for runtime-resolved expressions — they cannot be parsed at
		// compile time.
		if !containsExpression(engineModel) {
			// V-MAF-001 + V-MAF-006: validate syntax of engine.model.
			if errs := validateModelIdentifierStrings([]string{engineModel}, "engine.model"); len(errs) > 0 {
				return formatCompilerError(markdownPath, "error", errs[0], nil)
			}
			// V-MAF-011: warn about unrecognised parameter keys in engine.model.
			c.warnUnrecognizedModelParams([]string{engineModel}, markdownPath)
		}
	}

	// Validate user-supplied frontmatter aliases only (builtins are pre-validated).
	for key, entries := range frontmatterModels {
		// V-MAF-005: alias keys MUST NOT contain "/", "?", or "&".
		if err := validateAliasKey(key, markdownPath); err != nil {
			return err
		}

		// V-MAF-001 + V-MAF-002 + V-MAF-003 + V-MAF-006: validate each entry string.
		if errs := validateModelIdentifierStrings(entries, "models."+displayKey(key)); len(errs) > 0 {
			return formatCompilerError(markdownPath, "error", errs[0], nil)
		}

		// V-MAF-011: warn about unrecognised parameter keys in each entry.
		c.warnUnrecognizedModelParams(entries, markdownPath)
	}

	// V-MAF-010: detect circular alias references across the merged map.
	if err := detectCircularModelAliases(mergedAliasMap, markdownPath); err != nil {
		return err
	}

	modelAliasValidationLog.Print("Model alias map validation passed")
	return nil
}

// ─── V-MAF-005: alias key validation ─────────────────────────────────────────

// validateAliasKey validates a single alias map key (V-MAF-005).
// The empty string key ("") is allowed (default policy).
func validateAliasKey(key, markdownPath string) error {
	if key == "" {
		return nil // empty string is the default policy — permitted
	}
	for _, forbidden := range []string{"/", "?", "&"} {
		if strings.Contains(key, forbidden) {
			return formatCompilerError(markdownPath, "error",
				fmt.Sprintf("models: alias key %q must not contain %q (V-MAF-005)", key, forbidden),
				nil)
		}
	}
	return nil
}

// ─── V-MAF-001, 002, 003, 006: identifier syntax & param validation ───────────

// validateModelIdentifierStrings validates a slice of model identifier strings.
// Returns a slice of error messages (not wrapped errors) so the caller can
// decide how to report them.
func validateModelIdentifierStrings(identifiers []string, context string) []string {
	var errs []string
	for _, id := range identifiers {
		if id == "" {
			errs = append(errs, context+": model identifier must not be empty")
			continue
		}
		// Skip GitHub Actions expressions — they are resolved at runtime.
		// This includes whole-string expressions ("${{ inputs.model }}") and
		// partial expressions ("${{ inputs.model }}?effort=high", "copilot/${{ inputs.model }}").
		if containsExpression(id) {
			continue
		}
		p, err := ParseModelIdentifier(id)
		if err != nil {
			// V-MAF-001 / V-MAF-006
			errs = append(errs, fmt.Sprintf("%s: %s", context, err))
			continue
		}
		// V-MAF-002 and V-MAF-003: validate known parameter values.
		if err := ValidateKnownParams(p.Params); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", context, err))
		}
	}
	return errs
}

// ─── V-MAF-011: unknown parameter warning ─────────────────────────────────────

// warnUnrecognizedModelParams emits a compiler warning for each unrecognised
// parameter key found in the given model identifier strings (V-MAF-011).
func (c *Compiler) warnUnrecognizedModelParams(identifiers []string, markdownPath string) {
	for _, id := range identifiers {
		if id == "" || containsExpression(id) {
			continue
		}
		p, err := ParseModelIdentifier(id)
		if err != nil {
			continue // syntax errors are reported elsewhere
		}
		for _, k := range UnrecognizedParams(p.Params) {
			msg := fmt.Sprintf("models: unrecognised parameter key %q in %q — "+
				"known parameters are: effort, temperature (V-MAF-011)", k, id)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				formatCompilerMessage(markdownPath, "warning", msg)))
			c.IncrementWarningCount()
		}
	}
}

// ─── V-MAF-010: circular alias detection ─────────────────────────────────────

// detectCircularModelAliases performs a full DFS cycle check over the merged
// alias map and returns an error naming every alias in the cycle (V-MAF-010).
//
// Algorithm (Section 8.6.1):
//
//	For each alias key, perform a depth-first traversal of its list entries.
//	Maintain a set of alias names on the current DFS path.
//	If any traversal reaches an alias key already on the current path, a cycle
//	is detected and MUST be reported as a compile-time error.
func detectCircularModelAliases(aliasMap map[string][]string, markdownPath string) error {
	modelAliasValidationLog.Printf("Checking for circular alias references in %d aliases", len(aliasMap))

	// Fast path: the builtin alias map is a compile-time constant and cycle-free by
	// construction.  Cache the result of the first DFS check so that the expensive
	// sort + 52-node traversal is skipped on subsequent ParseWorkflowFile calls that
	// use only builtins (no imports, no frontmatter overrides).
	if isBuiltinOnlyAliasMap(aliasMap) {
		builtinCycleCheckOnce.Do(func() {
			// Builtin aliases are cycle-free by construction (validated in CI).
			// Pass a sentinel path so that if a cycle is ever introduced into the
			// builtin data (a bug) the error message clearly identifies the source
			// rather than referencing a caller's workflow path.
			builtinCycleCheckErr = detectCircularModelAliasesUnoptimized(aliasMap, "<builtin-aliases>")
		})
		return builtinCycleCheckErr
	}

	return detectCircularModelAliasesUnoptimized(aliasMap, markdownPath)
}

// detectCircularModelAliasesUnoptimized is the full DFS implementation used when the
// alias map may include user-defined aliases (imports or frontmatter overrides).
func detectCircularModelAliasesUnoptimized(aliasMap map[string][]string, markdownPath string) error {
	// visited tracks keys for which all DFS descendants have been fully explored
	// (no cycle detected from that key).
	visited := map[string]struct {
	}{}

	// Iterate keys in deterministic order for reproducible error messages.
	keys := sliceutil.SortedKeys(aliasMap)

	state := &dfsState{
		aliasMap: aliasMap,
		visited:  visited,
		onPath:   make(map[string]bool, 16),
		path:     make([]string, 0, 16),
	}

	for _, key := range keys {
		if setutil.Contains(visited, key) {
			continue
		}
		clear(state.onPath)
		state.path = state.path[:0]
		if cycle := state.dfs(key); cycle != nil {
			// Format cycle chain for a clear error message.
			chain := strings.Join(append(cycle, cycle[0]), " → ")
			return formatCompilerError(markdownPath, "error",
				fmt.Sprintf("circular alias reference detected: %s\n\n"+
					"Circular alias references are prohibited. Remove or rewrite the cycle in the 'models:' "+
					"frontmatter section (V-MAF-010).", chain),
				nil)
		}
	}

	return nil
}

// dfsState holds the mutable state for a single DFS traversal.
type dfsState struct {
	aliasMap map[string][]string
	visited  map[string]struct{}
	onPath   map[string]bool
	path     []string
}

func (s *dfsState) dfs(node string) []string {
	if setutil.Contains(s.visited, node) {
		return nil
	}
	if s.onPath[node] {
		// Cycle found — return the chain from node back around.
		for i, n := range s.path {
			if n == node {
				return append([]string(nil), s.path[i:]...)
			}
		}
		return append([]string(nil), s.path...) // fallback: should not happen
	}

	s.onPath[node] = true
	s.path = append(s.path, node)

	for _, entry := range s.aliasMap[node] {
		base, _, _ := strings.Cut(entry, "?")
		if isAliasReference(base, s.aliasMap) {
			if cycle := s.dfs(base); cycle != nil {
				return cycle
			}
		}
	}

	s.path = s.path[:len(s.path)-1]
	delete(s.onPath, node)
	s.visited[node] = struct{}{}
	return nil
}

// isAliasReference reports whether base is a bare identifier that refers to
// another alias key in the alias map (as opposed to a provider-scoped name or glob).
func isAliasReference(base string, aliasMap map[string][]string) bool {
	if strings.Contains(base, "/") || strings.Contains(base, "*") {
		return false
	}
	_, exists := aliasMap[base]
	return exists
}

// ─── Utilities ────────────────────────────────────────────────────────────────

// displayKey returns a human-readable representation of an alias key for use in
// error messages. The empty-string key (default policy) is shown as `""`.
func displayKey(key string) string {
	if key == "" {
		return `""`
	}
	return key
}
