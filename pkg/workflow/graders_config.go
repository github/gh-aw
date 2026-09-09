// Package workflow - Deterministic graders configuration types and parser.
package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/logger"
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
}

// BuiltinGraderIDs is the ordered list of built-in grader IDs (derived from registry).
var BuiltinGraderIDs = func() []string {
	ids := make([]string, len(BuiltinGraderRegistry))
	for i, m := range BuiltinGraderRegistry {
		ids[i] = m.ID
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
	ID               string         // grader identifier (must be unique)
	Enabled          *bool          // explicit enable/disable; nil means use default (true for built-ins)
	Name             string         // human-readable name (defaults from registry for built-ins)
	Description      string         // description of the metric
	Unit             string         // e.g. "ratio", "count", "ms", "factor"
	Direction        string         // "higher_is_better" or "lower_is_better"
	Threshold        *float64       // quality threshold (pass/fail boundary)
	Max              *float64       // theoretical maximum
	Min              *float64       // theoretical minimum
	Run              string         // operational-value evaluator script path
	Script           string         // inline JS body for trusted custom graders (built-ins leave empty)
	Config           map[string]any // arbitrary config passed to grader at runtime
	evaluatorContent string
}

// ScriptDigest returns the SHA-256 hex digest of the script, or "" if no script.
func (g *GraderDefinition) ScriptDigest() string {
	if g.Script == "" {
		return ""
	}
	h := sha256.Sum256([]byte(g.Script))
	return hex.EncodeToString(h[:])
}

// EvaluatorDigest returns the SHA-256 hex digest of the frozen operational-value evaluator.
func (g *GraderDefinition) EvaluatorDigest() string {
	if g.evaluatorContent == "" {
		return ""
	}
	h := sha256.Sum256([]byte(g.evaluatorContent))
	return hex.EncodeToString(h[:])
}

// GradersConfig holds the configuration for deterministic graders declared
// in workflow frontmatter. Graders run as an always() post-agent step in the agent job.
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

// HasCustomScripts returns true if any enabled grader has trusted custom code.
func (gc *GradersConfig) HasCustomScripts() bool {
	if gc == nil {
		return false
	}
	for _, g := range gc.Graders {
		if (g.Enabled == nil || *g.Enabled) && (g.Script != "" || g.evaluatorContent != "") {
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
	// Stable order: built-ins first in canonical order, then custom sorted
	var result []string
	builtinSet := make(map[string]struct{}, len(BuiltinGraderIDs))
	for _, bid := range BuiltinGraderIDs {
		builtinSet[bid] = struct{}{}
		if _, ok := enabledSet[bid]; ok {
			result = append(result, bid)
		}
	}
	var custom []string
	for id := range enabledSet {
		if _, ok := builtinSet[id]; !ok {
			custom = append(custom, id)
		}
	}
	sort.Strings(custom)
	result = append(result, custom...)
	return result
}

// graderIDPattern validates grader IDs: lowercase alphanumeric + hyphens, 1-64 chars.
var graderIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// parseGradersFromFrontmatter extracts and validates the graders configuration from the
// raw frontmatter map. Returns nil when the graders field is absent.
//
// Supported forms:
//
//	# Zero-config: all built-ins enabled
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
//
//	# Custom inline grader
//	graders:
//	  my-metric:
//	    script: |
//	      return { value: trace.toolCalls.length }
//	    unit: count
//	    direction: lower_is_better
func (c *Compiler) parseGradersFromFrontmatter(frontmatter map[string]any) (*GradersConfig, error) { //nolint:largefunc
	raw, exists := frontmatter["graders"]
	if !exists || raw == nil {
		return nil, nil
	}

	cfg := &GradersConfig{
		Graders: make(map[string]*GraderDefinition),
	}

	m, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("graders must be a map of grader IDs to configuration objects (or {} for all built-in defaults). Example:\ngraders:\n  tool-success-rate:\n    enabled: true")
	}

	builtinSet := make(map[string]struct{}, len(BuiltinGraderIDs))
	for _, id := range BuiltinGraderIDs {
		builtinSet[id] = struct{}{}
	}

	// If empty map {}, populate all built-ins with defaults
	if len(m) == 0 {
		for _, id := range BuiltinGraderIDs {
			meta := builtinGraderMetaByID[id]
			cfg.Graders[id] = builtinDefFromMeta(meta)
		}
		gradersConfigLog.Printf("Parsed graders config: zero-config with %d built-in graders", len(BuiltinGraderIDs))
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
			return nil, fmt.Errorf("graders has invalid id %q: must match %s. Example:\ngraders:\n  my-metric:\n    script: \"return { value: trace.toolCalls.length }\"", id, graderIDPattern.String())
		}

		def := &GraderDefinition{ID: id}

		// Apply built-in defaults if this is a built-in
		if meta, ok := builtinGraderMetaByID[id]; ok {
			def = builtinDefFromMeta(meta)
		} else if id == "operational-value" {
			def.Name = "Operational Value"
			def.Unit = "ratio"
			def.Direction = "higher_is_better"
			def.Min = new(0.0)
			def.Max = new(1.0)
		}

		_, isBuiltin := builtinSet[id]
		if entryRaw == nil {
			if id == "operational-value" {
				return nil, errors.New("graders.operational-value requires a 'run' field")
			}
			if !isBuiltin {
				return nil, fmt.Errorf("graders.%s is not a built-in grader and requires a 'script' field. Built-in graders: %s", id, strings.Join(BuiltinGraderIDs, ", "))
			}
			cfg.Graders[id] = def
			continue
		}

		entry, ok := entryRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("graders.%s must be a map or null, got %T. Example:\ngraders:\n  %s:\n    enabled: true", id, entryRaw, id)
		}

		if err := parseGraderEntryFields(def, entry, id, isBuiltin); err != nil {
			return nil, err
		}

		// The operational-value grader uses a repository evaluator; other custom graders use inline scripts.
		if id == "operational-value" && def.Run == "" && (def.Enabled == nil || *def.Enabled) {
			return nil, errors.New("graders.operational-value requires a 'run' field")
		}
		if !isBuiltin && id != "operational-value" && def.Script == "" && (def.Enabled == nil || *def.Enabled) {
			return nil, fmt.Errorf("graders.%s is not a built-in grader and requires a 'script' field. Built-in graders: %s", id, strings.Join(BuiltinGraderIDs, ", "))
		}

		cfg.Graders[id] = def
	}

	// Add missing built-ins as defaults when at least one built-in is explicitly listed
	hasAnyBuiltin := false
	for id := range cfg.Graders {
		if _, ok := builtinSet[id]; ok {
			hasAnyBuiltin = true
			break
		}
	}
	if hasAnyBuiltin {
		for _, id := range BuiltinGraderIDs {
			if _, exists := cfg.Graders[id]; !exists {
				meta := builtinGraderMetaByID[id]
				cfg.Graders[id] = builtinDefFromMeta(meta)
			}
		}
	}

	if err := validateGraders(cfg); err != nil {
		return nil, err
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
func parseGraderEntryFields(def *GraderDefinition, entry map[string]any, id string, isBuiltin bool) error { //nolint:largefunc
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

	if runRaw, ok := entry["run"]; ok {
		runPath, ok := runRaw.(string)
		if !ok {
			return fmt.Errorf("graders.%s.run must be a string, got %T", id, runRaw)
		}
		runPath = strings.TrimSpace(runPath)
		if id != "operational-value" {
			return fmt.Errorf("graders.%s.run is only supported by the operational-value grader", id)
		}
		if !IsValidOperationalValueEvaluatorRunPath(runPath) {
			return fmt.Errorf("graders.operational-value.run must be a workspace-relative or ./ local .sh file, got %q", runPath)
		}
		def.Run = runPath
	}

	if scriptRaw, ok := entry["script"]; ok {
		s, ok := scriptRaw.(string)
		if !ok {
			return fmt.Errorf("graders.%s.script must be a string, got %T", id, scriptRaw)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return fmt.Errorf("graders.%s.script must be non-empty when specified", id)
		}
		if isBuiltin {
			return fmt.Errorf("graders.%s is a built-in grader and cannot have a custom script", id)
		}
		if id == "operational-value" {
			return errors.New("graders.operational-value cannot have an inline script; use 'run'")
		}
		scriptCharCount := utf8.RuneCountInString(s)
		if scriptCharCount > 4096 {
			return fmt.Errorf("graders.%s.script exceeds maximum length of 4096 characters (%d)", id, scriptCharCount)
		}
		forbiddenPatterns := []string{"require(", "import(", "import ", "fetch(", "eval(", "process.exit", "child_process", "execSync", "spawnSync", "Function("}
		for _, p := range forbiddenPatterns {
			if strings.Contains(s, p) {
				return fmt.Errorf("graders.%s.script contains forbidden pattern %q — inline grader scripts must be pure functions without side effects", id, p)
			}
		}
		def.Script = s
	}

	return nil
}

// IsValidOperationalValueEvaluatorRunPath reports whether evaluatorPath is a
// safe shell script path. Paths may be repository-root-relative, or explicitly
// local to the workflow file when they start with "./". Empty components, ".",
// and ".." are rejected to avoid traversal.
func IsValidOperationalValueEvaluatorRunPath(evaluatorPath string) bool {
	if evaluatorPath == "" || strings.Contains(evaluatorPath, "\\") || evaluatorPath[0] == '/' {
		return false
	}
	pathForValidation := evaluatorPath
	if trimmed, ok := strings.CutPrefix(pathForValidation, "./"); ok {
		pathForValidation = trimmed
	}
	if pathForValidation == "" || pathForValidation[0] == '/' {
		return false
	}
	for part := range strings.SplitSeq(pathForValidation, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return strings.HasSuffix(pathForValidation, ".sh")
}

// parseOptionalFloat parses an optional float64 field from a map.
func parseOptionalFloat(m map[string]any, key string, graderID string, target **float64) error {
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case float64:
		*target = &n
	case int:
		f := float64(n)
		*target = &f
	default:
		return fmt.Errorf("graders.%s.%s must be a number, got %T", graderID, key, v)
	}
	return nil
}

// validateGraders checks invariants after parsing.
func validateGraders(cfg *GradersConfig) error {
	if cfg == nil {
		return nil
	}
	if grader, ok := cfg.Graders["operational-value"]; ok {
		if grader.Direction != "higher_is_better" {
			return errors.New("graders.operational-value.direction must be 'higher_is_better'")
		}
		if grader.Min == nil || *grader.Min != 0 || grader.Max == nil || *grader.Max != 1 {
			return errors.New("graders.operational-value range must be min: 0 and max: 1")
		}
		if grader.Threshold != nil && (math.IsNaN(*grader.Threshold) || math.IsInf(*grader.Threshold, 0) || *grader.Threshold < 0 || *grader.Threshold > 1) {
			return errors.New("graders.operational-value.threshold must be between 0 and 1")
		}
	}
	if !cfg.HasGraders() {
		return errors.New("graders configuration has no enabled graders. Remove the graders field to disable grading, or set enabled: true on at least one grader")
	}
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
