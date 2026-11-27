package workflow

import (
	"fmt"
	"sync"

	"github.com/github/gh-aw/pkg/jsonutil"
	"github.com/goccy/go-yaml"
)

func isEngineDefinitionForm(def *EngineDefinition) bool {
	if def == nil {
		return false
	}
	// Treat richer metadata-only objects as shared engine definitions. Plain engine
	// config objects ("id", "model", "env", etc.) should continue down the normal
	// EngineConfig path instead of being registered as catalog entries.
	if def.DisplayName != "" || def.RuntimeID != "" || def.Experimental || def.GHSkillAgentName != "" || def.Behaviors != nil || def.DetectionEngine != "" || len(def.Auth) > 0 {
		return true
	}
	if def.Provider.Name != "" || def.Provider.Auth != nil || def.Provider.Request != nil {
		return true
	}
	return def.Models.Default != "" || len(def.Models.Supported) > 0 || len(def.Options) > 0
}

// engineDefinitionBuiltinKeys is the set of JSON strings corresponding to
// built-in engine definitions. It is populated once at startup by
// loadBuiltinEngineDefinitions (via registerBuiltinEngineDefinitionJSON) and
// never modified afterward. Only JSON keys present in this set are eligible for
// caching in engineDefinitionCache, which prevents unbounded growth in long-lived
// compile --watch sessions where each edit to a custom engine definition would
// otherwise create a new cache entry.
var engineDefinitionBuiltinKeys sync.Map // map[string]struct{}

// registerBuiltinEngineDefinitionJSON marks jsonKey as a known built-in engine
// JSON string so that parseEngineDefinitionFromJSON will cache it.
func registerBuiltinEngineDefinitionJSON(jsonKey string) {
	engineDefinitionBuiltinKeys.Store(jsonKey, struct{}{})
}

// engineDefinitionCache caches parsed EngineDefinition values for built-in
// engine JSON strings. Built-in engine files are injected as imports on every
// CompileWorkflow call and their JSON representation is always identical, so
// caching avoids the repeated JSON→any→YAML→struct round-trip that accounted
// for ~24% of BenchmarkCompileMCPWorkflow wall-clock time.
//
// Only keys present in engineDefinitionBuiltinKeys are stored to bound the cache
// to the fixed set of built-in engines. Deep copies are returned on every lookup
// so callers cannot corrupt the cached state through mutations to pointers, slices,
// or maps within the returned definition.
var engineDefinitionCache sync.Map // map[string]EngineDefinition

func parseEngineDefinitionFromJSON(engineJSON string) (*EngineDefinition, error) {
	if engineJSON == "" {
		return nil, nil
	}
	// Fast path: return a deep copy of the cached definition when available.
	if cached, ok := engineDefinitionCache.Load(engineJSON); ok {
		if def, ok := cached.(EngineDefinition); ok {
			behaviorDefinedEngineLog.Printf("Engine definition cache hit: id=%s", def.ID)
			defCopy := deepCopyEngineDefinition(def)
			return &defCopy, nil
		}
		// Type assertion failure indicates cache corruption or a concurrent Store with
		// an unexpected type. Log and fall through to re-parse so the caller still works.
		behaviorDefinedEngineLog.Printf("engineDefinitionCache: unexpected value type %T for key (len=%d); re-parsing", cached, len(engineJSON))
		engineDefinitionCache.Delete(engineJSON)
	}
	var engineData any
	if err := jsonutil.UnmarshalString(engineJSON, &engineData); err != nil {
		return nil, fmt.Errorf("engine JSON is not recognized, expected a valid JSON object describing the engine: %w", err)
	}
	dataMap, ok := engineData.(map[string]any)
	if !ok {
		return nil, nil
	}
	// EngineDefinition.Auth expects a []AuthBinding sequence. If the auth field is
	// an EngineAuthConfig mapping (e.g. Anthropic/Azure WIF-style auth), strip it before
	// unmarshaling to avoid "mapping was used where sequence is expected". The
	// mapping-style auth is handled separately by extractEngineConfigFromJSON via
	// applyEngineAuthField.
	if isEngineAuthConfigMapping(dataMap["auth"]) {
		delete(dataMap, "auth")
	}
	yamlBytes, err := yaml.Marshal(dataMap)
	if err != nil {
		return nil, fmt.Errorf("engine JSON could not be converted to YAML, expected values that marshal cleanly (e.g. no unsupported types): %w", err)
	}
	var def EngineDefinition
	if err := yaml.Unmarshal(yamlBytes, &def); err != nil {
		return nil, fmt.Errorf("engine definition is not recognized, expected fields matching the EngineDefinition schema: %w", err)
	}
	if def.RuntimeID == "" {
		def.RuntimeID = def.ID
	}
	// Cache only built-in engine definitions (keys pre-seeded by loadBuiltinEngineDefinitions)
	// to prevent unbounded memory growth. Store a deep copy so that any mutations the
	// caller makes to the returned definition cannot corrupt the cached entry.
	if _, isBuiltin := engineDefinitionBuiltinKeys.Load(engineJSON); isBuiltin {
		behaviorDefinedEngineLog.Printf("Caching built-in engine definition: id=%s", def.ID)
		cacheCopy := deepCopyEngineDefinition(def)
		engineDefinitionCache.Store(engineJSON, cacheCopy)
	}
	return &def, nil
}

func isEngineAuthConfigMapping(auth any) bool {
	authMap, ok := auth.(map[string]any)
	if !ok {
		return false
	}
	authType, ok := authMap["type"].(string)
	return ok && authType == "github-oidc"
}

func (c *Compiler) registerNamedEngineDefinitionFromJSON(engineJSON string) error {
	def, err := parseEngineDefinitionFromJSON(engineJSON)
	if err != nil || !isEngineDefinitionForm(def) {
		return err
	}
	behaviorDefinedEngineLog.Printf("Registering named engine definition: id=%s behaviorDefined=%t", def.ID, def.Behaviors != nil)
	if def.Behaviors != nil {
		engine, buildErr := NewBehaviorDefinedEngine(def)
		if buildErr != nil {
			return buildErr
		}
		if regErr := c.engineRegistry.Register(engine); regErr != nil {
			return regErr
		}
		def.RuntimeID = engine.GetID()
	}
	c.engineCatalog.Register(def)
	return nil
}
