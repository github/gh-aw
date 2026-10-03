//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetEngineDefinitionCacheForTest clears the engine definition cache and
// built-in key set between tests to ensure hermetic behaviour.
func resetEngineDefinitionCacheForTest() {
	engineDefinitionCache.Range(func(k, _ any) bool {
		engineDefinitionCache.Delete(k)
		return true
	})
	engineDefinitionBuiltinKeys.Range(func(k, _ any) bool {
		engineDefinitionBuiltinKeys.Delete(k)
		return true
	})
}

func TestParseEngineDefinitionFromJSON_EmptyJSON(t *testing.T) {
	def, err := parseEngineDefinitionFromJSON("")
	assert.Nil(t, def)
	assert.NoError(t, err)
}

func TestParseEngineDefinitionFromJSON_InvalidJSON(t *testing.T) {
	_, err := parseEngineDefinitionFromJSON("{bad json")
	assert.Error(t, err)
}

func TestParseEngineDefinitionFromJSON_NonObjectJSON(t *testing.T) {
	// A JSON string (not an object) should return nil without error.
	def, err := parseEngineDefinitionFromJSON(`"copilot"`)
	assert.Nil(t, def)
	assert.NoError(t, err)
}

func TestParseEngineDefinitionFromJSON_ValidObject(t *testing.T) {
	const engineJSON = `{"id":"test-engine","display-name":"Test Engine"}`

	def, err := parseEngineDefinitionFromJSON(engineJSON)
	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Equal(t, "test-engine", def.ID)
	assert.Equal(t, "Test Engine", def.DisplayName)
	// RuntimeID defaults to ID when omitted.
	assert.Equal(t, "test-engine", def.RuntimeID)
}

func TestParseEngineDefinitionFromJSON_CacheHitReturnsCopy(t *testing.T) {
	resetEngineDefinitionCacheForTest()
	defer resetEngineDefinitionCacheForTest()

	const engineJSON = `{"id":"test-engine","display-name":"Test Engine"}`

	// Pre-seed the built-in key so the engine gets cached.
	registerBuiltinEngineDefinitionJSON(engineJSON)

	first, err := parseEngineDefinitionFromJSON(engineJSON)
	require.NoError(t, err)
	require.NotNil(t, first)

	// Mutate a scalar field on the returned pointer.
	first.RuntimeID = "mutated"

	// A second call must return a fresh independent copy.
	second, err := parseEngineDefinitionFromJSON(engineJSON)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.NotEqual(t, "mutated", second.RuntimeID,
		"cache hit returned the same object; scalar mutation leaked into cached state")
}

func TestParseEngineDefinitionFromJSON_CacheHitReturnsDeepCopy(t *testing.T) {
	resetEngineDefinitionCacheForTest()
	defer resetEngineDefinitionCacheForTest()

	const engineJSON = `{"id":"test-engine","options":{"key":"value"},"env":{"OPENAI_BASE_URL":"https://gateway.example"}}`

	registerBuiltinEngineDefinitionJSON(engineJSON)

	first, err := parseEngineDefinitionFromJSON(engineJSON)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.NotNil(t, first.Options)

	// Mutate the Options map on the returned definition.
	first.Options["injected"] = "evil"
	first.Env["OPENAI_BASE_URL"] = "https://mutated.example"

	second, err := parseEngineDefinitionFromJSON(engineJSON)
	require.NoError(t, err)
	require.NotNil(t, second)
	_, poisoned := second.Options["injected"]
	assert.False(t, poisoned, "Options mutation leaked into cached state via shallow map copy")
	assert.Equal(t, "https://gateway.example", second.Env["OPENAI_BASE_URL"], "Env mutation leaked into cached state")
}

func TestParseEngineDefinitionFromJSON_NonBuiltinNotCached(t *testing.T) {
	resetEngineDefinitionCacheForTest()
	defer resetEngineDefinitionCacheForTest()

	const engineJSON = `{"id":"custom-engine"}`
	// engineJSON is NOT registered as a built-in key.

	_, err := parseEngineDefinitionFromJSON(engineJSON)
	require.NoError(t, err)

	// The cache must remain empty since this is not a known built-in.
	found := false
	engineDefinitionCache.Range(func(_, _ any) bool {
		found = true
		return false
	})
	assert.False(t, found, "non-builtin engine JSON was incorrectly added to the cache")
}

func TestDeepCopyEngineDefinition_SlicesAreIndependent(t *testing.T) {
	src := EngineDefinition{
		Models: ModelSelection{Supported: []string{"gpt-4", "gpt-3.5"}},
		Auth:   []AuthBinding{{Role: "api", Secret: "MY_SECRET"}},
	}
	dst := deepCopyEngineDefinition(src)

	// Mutating src slices must not affect dst.
	src.Models.Supported[0] = "changed"
	src.Auth[0].Secret = "CHANGED"

	assert.Equal(t, "gpt-4", dst.Models.Supported[0])
	assert.Equal(t, "MY_SECRET", dst.Auth[0].Secret)
}

func TestDeepCopyEngineDefinition_BehaviorsAreIndependent(t *testing.T) {
	src := EngineDefinition{
		Behaviors: &EngineBehaviorDefinition{
			SupportedEnvVarKeys: []string{"API_KEY"},
			Execution: &EngineExecutionDefinition{
				Args: []string{"--model", "gpt-4"},
				Env:  map[string]string{"FOO": "bar"},
			},
		},
	}
	dst := deepCopyEngineDefinition(src)

	// Mutate src's nested reference types.
	src.Behaviors.SupportedEnvVarKeys[0] = "CHANGED"
	src.Behaviors.Execution.Args[0] = "CHANGED"
	src.Behaviors.Execution.Env["FOO"] = "CHANGED"

	require.NotNil(t, dst.Behaviors)
	assert.Equal(t, "API_KEY", dst.Behaviors.SupportedEnvVarKeys[0])
	require.NotNil(t, dst.Behaviors.Execution)
	assert.Equal(t, "--model", dst.Behaviors.Execution.Args[0])
	assert.Equal(t, "bar", dst.Behaviors.Execution.Env["FOO"])
}

func TestDeepCopyAny_NestedMapAndSlice(t *testing.T) {
	src := map[string]any{
		"nested": map[string]any{"key": "value"},
		"list":   []any{"a", "b"},
	}
	dst := deepCopyAny(src).(map[string]any)

	// Mutate src; dst must be unaffected.
	src["nested"].(map[string]any)["key"] = "mutated"
	src["list"].([]any)[0] = "mutated"

	assert.Equal(t, "value", dst["nested"].(map[string]any)["key"])
	assert.Equal(t, "a", dst["list"].([]any)[0])
}
