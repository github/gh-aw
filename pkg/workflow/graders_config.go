// Package workflow - Deterministic graders configuration types and parser.
package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/gradershell"
)

var gradersConfigLog = logger.New("workflow:graders_config")

// BuiltinGraderMeta describes a built-in deterministic grader with its default metadata.
type BuiltinGraderMeta struct {
	ID          string
	Name        string
	Description string
	Unit        string
	Direction   string // "higher_is_better" | "lower_is_better"
	Threshold   *float64
	Max         *float64
	Min         *float64
}

// BuiltinGraderRegistry is the ordered list of all built-in grader definitions.
var BuiltinGraderRegistry = []BuiltinGraderMeta{
	{ID: "tool-success-rate", Name: "Tool Success Rate", Description: "Fraction of tool calls that succeeded", Unit: "ratio", Direction: "higher_is_better", Threshold: new(0.8), Min: new(0.0), Max: new(1.0)},
	{ID: "tool-failure-count", Name: "Tool Failure Count", Description: "Number of tool calls that failed", Unit: "count", Direction: "lower_is_better", Threshold: new(5.0)},
	{ID: "retries", Name: "Retries", Description: "Number of retry events detected in gateway logs", Unit: "count", Direction: "lower_is_better", Threshold: new(10.0)},
	{ID: "loops", Name: "Loops", Description: "Consecutive identical tool calls (same name and arguments)", Unit: "count", Direction: "lower_is_better", Threshold: new(3.0)},
	{ID: "trajectory-efficiency", Name: "Trajectory Efficiency", Description: "Ratio of unique tool names to total tool calls (higher = more diverse usage)", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "execution-step-count", Name: "Execution Step Count", Description: "Total LLM request count", Unit: "count", Direction: "lower_is_better"},
	{ID: "execution-duration", Name: "Execution Duration", Description: "Total execution duration", Unit: "ms", Direction: "lower_is_better"},
	{ID: "working-set-rebuild-factor", Name: "Working-Set Rebuild Factor", Description: "Cumulative input tokens divided by peak invocation input tokens", Unit: "factor", Direction: "lower_is_better", Min: new(1.0)},
	{ID: "context-growth", Name: "Context Growth", Description: "Ratio of total tokens to first-request tokens", Unit: "factor", Direction: "lower_is_better"},
	{ID: "artifact-production", Name: "Artifact Production", Description: "Count of outputs/artifacts produced by the agent", Unit: "count", Direction: "higher_is_better"},
	{ID: "progress-stagnation", Name: "Progress Stagnation", Description: "Repeated unchanged tool failures without observed progress", Unit: "count", Direction: "lower_is_better", Threshold: new(0.0)},
	{ID: "ineffective-retries", Name: "Ineffective Retries", Description: "Repeated failures of an unchanged scoped tool operation", Unit: "count", Direction: "lower_is_better", Threshold: new(0.0)},
	{ID: "premature-termination", Name: "Premature Termination", Description: "Session termination with explicitly recorded unmet objectives", Unit: "count", Direction: "lower_is_better", Threshold: new(0.0)},
	{ID: "completion-misrepresentation", Name: "Completion Misrepresentation", Description: "Explicit success claims contradicted by scoped execution evidence", Unit: "count", Direction: "lower_is_better", Threshold: new(0.0)},
	// Opt-in trajectory analyses use projections of the unified session and
	// report unavailable when the required evidence is absent.
	{ID: "policy-near-miss", Name: "Policy Near-Miss Rate", Description: "Fraction of successful traces that left guard or policy objectives unsatisfied", Unit: "ratio", Direction: "lower_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "skill-constraint-coverage", Name: "Skill Constraint Coverage", Description: "Fraction of configured skill constraints that were exercised and passed", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "exploration-error", Name: "Exploration Error", Description: "Unmet objectives attributable to insufficient search", Unit: "ratio", Direction: "lower_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "exploitation-error", Name: "Exploitation Error", Description: "Unmet objectives attributable to gathered evidence that was never used", Unit: "ratio", Direction: "lower_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "state-revisit-probability-rep", Name: "State Revisit Probability REP", Description: "Fraction of canonical state visits that revisit an already visited state", Unit: "ratio", Direction: "lower_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "recurrence-determinism", Name: "Recurrence Determinism (RQA DET)", Description: "RQA DET: fraction of recurrent points forming diagonal (repeated-subsequence) lines", Unit: "ratio", Direction: "lower_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "recurrence-laminarity", Name: "Recurrence Laminarity (RQA LAM)", Description: "RQA LAM: fraction of recurrent points forming vertical (stagnation) lines", Unit: "ratio", Direction: "lower_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "recurrence-trapping-time", Name: "Recurrence Trapping Time (RQA TT)", Description: "RQA TT: average length of vertical recurrence lines", Unit: "steps", Direction: "lower_is_better", Min: new(0.0)},
	{ID: "recurrence-rate", Name: "Recurrence Rate (RQA RR)", Description: "RQA RR: density of recurrent state pairs across the run", Unit: "ratio", Direction: "lower_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "event-entropy-rate", Name: "Event Entropy Rate", Description: "Normalized conditional Shannon entropy rate of the ordered event sequence", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "lempel-ziv-trajectory-complexity", Name: "Lempel-Ziv Trajectory Complexity", Description: "Normalized LZ76 complexity of the canonical event sequence", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "tool-output-consumption-rate", Name: "Tool Output Consumption Rate", Description: "Fraction of tool outputs referenced by a later action", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "end-to-end-lineage-completeness", Name: "End-to-End Lineage Completeness", Description: "Fraction of final outputs traceable to tool or observation evidence roots", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "action-provenance-coverage", Name: "Action Provenance Coverage", Description: "Fraction of consequential actions with a provenance path to tool or observation evidence", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "premature-termination-gap", Name: "Premature Termination Gap", Description: "Declared completion conditions still unsatisfied at termination", Unit: "count", Direction: "lower_is_better", Min: new(0.0)},
	{ID: "evidence-saturation-stopping-lag", Name: "Evidence Saturation Stopping Lag", Description: "Events elapsed between all objectives being satisfied and the run stopping", Unit: "count", Direction: "lower_is_better", Min: new(0.0)},
	{ID: "dependency-order-violation-rate", Name: "Dependency Order Violation Rate", Description: "Fraction of dependent objectives satisfied before their prerequisites", Unit: "ratio", Direction: "lower_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "objective-coverage", Name: "Objective Coverage", Description: "Fraction of declared objectives that were completed", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "grounding-accuracy", Name: "Grounding Accuracy", Description: "Fraction of actions that were valid in the state in which they were issued", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "tool-wise-score", Name: "Tool-Wise Score", Description: "Longest correct execution prefix against a reference trajectory, with parameter credit", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "trajectory-ndtw", Name: "Trajectory nDTW", Description: "Normalized dynamic time warping similarity to a reference state trajectory", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "code-search-recall", Name: "Code Search Recall", Description: "Fraction of reference patch files located during the run", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "code-read-precision", Name: "Code Read Precision", Description: "Fraction of distinct read or inspected files touched by the reference patch", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "code-edit-precision", Name: "Code Edit Precision", Description: "Fraction of distinct edited files touched by the reference patch", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
	{ID: "process-alignment-fitness", Name: "Process Alignment Fitness", Description: "Fraction of observed tool-sequence checks allowed by a configured process model", Unit: "ratio", Direction: "higher_is_better", Min: new(0.0), Max: new(1.0)},
}

// BuiltinGraderIDs is the ordered list of built-in grader IDs (derived from registry).
var BuiltinGraderIDs = func() []string {
	ids := make([]string, 0, len(BuiltinGraderRegistry))
	for _, m := range BuiltinGraderRegistry {
		ids = append(ids, m.ID)
	}
	return ids
}()

// builtinGraderMetaByID is a lookup map for BuiltinGraderRegistry.
var builtinGraderMetaByID = func() map[string]*BuiltinGraderMeta {
	m := make(map[string]*BuiltinGraderMeta, len(BuiltinGraderRegistry))
	for i := range BuiltinGraderRegistry {
		m[BuiltinGraderRegistry[i].ID] = &BuiltinGraderRegistry[i]
	}
	return m
}()

// GraderDefinition represents a single grader entry in the graders map.
type GraderDefinition struct {
	ID            string         // grader identifier (must be unique)
	Enabled       *bool          // explicit enable/disable; nil means use default (true for built-ins)
	Name          string         // human-readable name (defaults from registry for built-ins)
	Description   string         // description of the metric
	Unit          string         // e.g. "ratio", "count", "ms", "factor"
	Direction     string         // "higher_is_better" or "lower_is_better"
	Threshold     *float64       // quality threshold (pass/fail boundary)
	Max           *float64       // theoretical maximum
	Min           *float64       // theoretical minimum
	Config        map[string]any // arbitrary config passed to grader at runtime
	Script        string         // JavaScript executed only in the isolated graders job
	Run           string         // Repository Bash evaluator path, only for operational-value
	RunSourcePath string         // Declaring import file, for workflow-relative run paths
	ShellScript   string         // Frozen evaluator source, never sent to conclusion
}

// GradersConfig holds the configuration for deterministic graders declared
// in workflow frontmatter. Graders run on the unified session in the conclusion job.
type GradersConfig struct {
	// Graders is the map of grader ID to definition.
	Graders map[string]*GraderDefinition
}

// HasGraders returns true when the config contains at least one enabled grader.
func (gc *GradersConfig) HasGraders() bool {
	if gc == nil {
		return false
	}
	for _, g := range gc.Graders {
		if g.Enabled == nil || *g.Enabled {
			return true
		}
	}
	return false
}

// EnabledGraderIDs returns the sorted list of enabled grader IDs.
func (gc *GradersConfig) EnabledGraderIDs() []string {
	if gc == nil {
		return nil
	}
	enabledSet := make(map[string]struct{})
	for id, g := range gc.Graders {
		if g.Enabled == nil || *g.Enabled {
			enabledSet[id] = struct{}{}
		}
	}
	// Stable canonical order.
	var result []string
	for _, bid := range BuiltinGraderIDs {
		if _, ok := enabledSet[bid]; ok {
			result = append(result, bid)
		}
	}
	var custom []string
	for id := range enabledSet {
		if _, builtin := builtinGraderMetaByID[id]; !builtin {
			custom = append(custom, id)
		}
	}
	sort.Strings(custom)
	result = append(result, custom...)
	return result
}

// CustomGraderIDs returns enabled custom graders in stable order.
func (gc *GradersConfig) CustomGraderIDs() []string {
	var ids []string
	for _, id := range gc.EnabledGraderIDs() {
		if _, builtin := builtinGraderMetaByID[id]; !builtin {
			ids = append(ids, id)
		}
	}
	return ids
}

// DefaultGraderIDs contains only linear-time built-ins. Trajectory analyses that
// need reference data or potentially quadratic work must be explicitly enabled.
var DefaultGraderIDs = []string{
	"tool-success-rate", "tool-failure-count", "retries", "loops", "trajectory-efficiency",
	"execution-step-count", "execution-duration", "working-set-rebuild-factor", "context-growth", "artifact-production",
	"progress-stagnation", "ineffective-retries", "premature-termination", "completion-misrepresentation",
}

// graderIDPattern validates grader IDs: lowercase alphanumeric + hyphens, 1-64 chars.
var graderIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// parseGradersFromFrontmatter extracts and validates the graders configuration from the
// raw frontmatter map. Fast built-ins are enabled even when the field is absent.
//
// Supported forms:
//
//	# Zero-config: fast built-ins enabled
//	graders: {}
//
//	# Selective disable
//	graders:
//	  loops:
//	    enabled: false
//
//	# Built-in with threshold override
//	graders:
//	  tool-success-rate:
//	    threshold: 0.95
func (c *Compiler) parseGradersFromFrontmatter(frontmatter map[string]any) (*GradersConfig, error) { //nolint:largefunc
	raw := frontmatter["graders"]
	cfg := &GradersConfig{
		Graders: make(map[string]*GraderDefinition),
	}
	for _, id := range DefaultGraderIDs {
		cfg.Graders[id] = builtinDefFromMeta(builtinGraderMetaByID[id])
	}
	if raw == nil {
		return cfg, nil
	}

	m, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("graders must be a map of grader IDs to configuration objects (or {} for all built-in defaults). Example:\ngraders:\n  tool-success-rate:\n    enabled: true")
	}

	if len(m) == 0 {
		return cfg, nil
	}

	// Parse explicit entries
	seenNormalizedIDs := make(map[string]string, len(m))
	for id, entryRaw := range m {
		rawID := id
		id = strings.TrimSpace(id)
		if existingRawID, exists := seenNormalizedIDs[id]; exists {
			return nil, fmt.Errorf("graders has duplicate id %q after normalization. Remove whitespace variants (for example %q and %q)", id, existingRawID, rawID)
		}
		seenNormalizedIDs[id] = rawID
		if !graderIDPattern.MatchString(id) {
			return nil, fmt.Errorf("graders has invalid id %q: must match %s", id, graderIDPattern.String())
		}

		meta, isBuiltin := builtinGraderMetaByID[id]
		def := &GraderDefinition{ID: id, Name: id}
		if isBuiltin {
			def = builtinDefFromMeta(meta)
		} else if id == "operational-value" {
			def.Name = "Operational Value"
			def.Direction = "higher_is_better"
		}
		if entryRaw == nil {
			if !isBuiltin {
				if id == "operational-value" {
					return nil, errors.New("graders.operational-value requires run: a repository Bash evaluator path")
				}
				return nil, fmt.Errorf("graders.%s requires a JavaScript script over the in-memory session", id)
			}
			cfg.Graders[id] = def
			continue
		}

		entry, ok := entryRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("graders.%s must be a map or null, got %T. Example:\ngraders:\n  %s:\n    enabled: true", id, entryRaw, id)
		}

		if err := parseGraderEntryFields(def, entry, id); err != nil {
			return nil, err
		}
		if id == "operational-value" && def.Run == "" {
			return nil, errors.New("graders.operational-value requires run: a repository Bash evaluator path")
		}
		if !isBuiltin && id != "operational-value" && def.Script == "" {
			return nil, fmt.Errorf("graders.%s requires a JavaScript script over the in-memory session", id)
		}
		if !isBuiltin && def.Threshold != nil && def.Direction == "" {
			return nil, fmt.Errorf("graders.%s.direction is required when a custom grader declares a threshold", id)
		}

		cfg.Graders[id] = def
	}

	enabledCount := len(cfg.EnabledGraderIDs())
	gradersConfigLog.Printf("Parsed %d grader definitions (%d enabled)", len(cfg.Graders), enabledCount)
	return cfg, nil
}

// builtinDefFromMeta creates a GraderDefinition from built-in metadata.
func builtinDefFromMeta(meta *BuiltinGraderMeta) *GraderDefinition {
	def := &GraderDefinition{
		ID:          meta.ID,
		Name:        meta.Name,
		Description: meta.Description,
		Unit:        meta.Unit,
		Direction:   meta.Direction,
	}
	if meta.Threshold != nil {
		v := *meta.Threshold
		def.Threshold = &v
	}
	if meta.Max != nil {
		v := *meta.Max
		def.Max = &v
	}
	if meta.Min != nil {
		v := *meta.Min
		def.Min = &v
	}
	return def
}

// parseGraderEntryFields parses individual fields from a grader entry map into the definition.
func parseGraderEntryFields(def *GraderDefinition, entry map[string]any, id string) error { //nolint:largefunc
	for key := range entry {
		switch key {
		case "enabled", "name", "description", "unit", "direction", "threshold", "max", "min", "config", "script", "run":
		default:
			return fmt.Errorf("graders.%s.%s is not supported; use data-only built-in configuration or an inline JavaScript script for a custom grader", id, key)
		}
	}
	if raw, exists := entry["run"]; exists {
		if id != "operational-value" {
			return fmt.Errorf("graders.%s.run is unsupported; only graders.operational-value accepts a Bash evaluator path", id)
		}
		run, valid := raw.(string)
		if !valid || !gradershell.ValidRunPath(run) {
			return errors.New("graders.operational-value.run must be a static repository-relative .sh path without traversal or expressions")
		}
		def.Run = run
	}
	if raw, exists := entry["script"]; exists {
		if id == "operational-value" {
			return errors.New("graders.operational-value requires run instead of script; use a distinct ID for JavaScript")
		}
		if _, builtin := builtinGraderMetaByID[id]; builtin {
			return fmt.Errorf("graders.%s.script cannot override a built-in grader; choose a custom grader ID", id)
		}
		script, valid := raw.(string)
		if !valid || strings.TrimSpace(script) == "" || len(script) > 256*1024 {
			return fmt.Errorf("graders.%s.script must be nonempty JavaScript of at most 256 KiB", id)
		}
		def.Script = script
	}
	if v, ok := entry["enabled"]; ok {
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("graders.%s.enabled must be a boolean, got %T", id, v)
		}
		def.Enabled = &b
	}

	if v, ok := entry["name"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("graders.%s.name must be a string, got %T", id, v)
		}
		def.Name = s
	}
	if v, ok := entry["description"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("graders.%s.description must be a string, got %T", id, v)
		}
		def.Description = s
	}
	if v, ok := entry["unit"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("graders.%s.unit must be a string, got %T", id, v)
		}
		def.Unit = s
	}
	if v, ok := entry["direction"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("graders.%s.direction must be a string, got %T", id, v)
		}
		if s != "higher_is_better" && s != "lower_is_better" {
			return fmt.Errorf("graders.%s.direction must be 'higher_is_better' or 'lower_is_better', got %q", id, s)
		}
		def.Direction = s
	}
	if err := parseOptionalFloat(entry, "threshold", id, &def.Threshold); err != nil {
		return err
	}
	if err := parseOptionalFloat(entry, "max", id, &def.Max); err != nil {
		return err
	}
	if err := parseOptionalFloat(entry, "min", id, &def.Min); err != nil {
		return err
	}

	if v, ok := entry["config"]; ok {
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("graders.%s.config must be an object, got %T", id, v)
		}
		def.Config = m
	}

	return nil
}

// parseOptionalFloat parses an optional float64 field from a map.
func parseOptionalFloat(m map[string]any, key string, graderID string, target **float64) error {
	v, ok := m[key]
	if !ok {
		return nil
	}
	n, valid := extractFloat(v)
	if !valid {
		return fmt.Errorf("graders.%s.%s must be a number, got %T", graderID, key, v)
	}
	*target = &n
	return nil
}

func deepMergeMaps(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = make(map[string]any)
	}
	for key, value := range src {
		if current, ok := dst[key]; ok {
			currentMap, currentIsMap := current.(map[string]any)
			incomingMap, incomingIsMap := value.(map[string]any)
			if currentIsMap && incomingIsMap {
				dst[key] = deepMergeMaps(currentMap, incomingMap)
				continue
			}
		}
		dst[key] = value
	}
	return dst
}

func mergeImportedGradersFrontmatter(frontmatter map[string]any, importedGraders string) (map[string]any, error) {
	if strings.TrimSpace(importedGraders) == "" {
		return frontmatter, nil
	}

	mergedGraders := make(map[string]any)
	for line := range strings.SplitSeq(importedGraders, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var imported map[string]any
		if err := json.Unmarshal([]byte(line), &imported); err != nil {
			return nil, fmt.Errorf("imported graders configuration is not valid JSON: %w", err)
		}
		mergedGraders = deepMergeMaps(mergedGraders, imported)
	}

	if raw, ok := frontmatter["graders"].(map[string]any); ok {
		mergedGraders = deepMergeMaps(mergedGraders, raw)
	}
	if len(mergedGraders) == 0 {
		return frontmatter, nil
	}

	mergedFrontmatter := make(map[string]any, len(frontmatter)+1)
	maps.Copy(mergedFrontmatter, frontmatter)
	mergedFrontmatter["graders"] = mergedGraders
	return mergedFrontmatter, nil
}

// ParseGradersFromFrontmatter is a public standalone convenience wrapper.
func ParseGradersFromFrontmatter(frontmatter map[string]any) (*GradersConfig, error) {
	var c Compiler
	return c.parseGradersFromFrontmatter(frontmatter)
}
