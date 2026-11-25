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

func TestParseEngineDefinitionFromJSON_AuthMappingAndInvalidSchema(t *testing.T) {
	def, err := parseEngineDefinitionFromJSON(`{"id":"test-engine","auth":{"type":"github-oidc"}}`)
	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Empty(t, def.Auth)

	_, err = parseEngineDefinitionFromJSON(`{"id":"test-engine","auth":{"type":"other"}}`)
	assert.Error(t, err)
}

func TestEngineDefinitionForm(t *testing.T) {
	tests := []struct {
		name string
		def  *EngineDefinition
		want bool
	}{
		{"nil", nil, false},
		{"id only", &EngineDefinition{ID: "test"}, false},
		{"metadata", &EngineDefinition{DisplayName: "Test"}, true},
		{"provider", &EngineDefinition{Provider: ProviderSelection{Name: "test"}}, true},
		{"model", &EngineDefinition{Models: ModelSelection{Default: "test"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isEngineDefinitionForm(tt.def))
		})
	}
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

	const engineJSON = `{"id":"test-engine","options":{"key":"value"}}`

	registerBuiltinEngineDefinitionJSON(engineJSON)

	first, err := parseEngineDefinitionFromJSON(engineJSON)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.NotNil(t, first.Options)

	// Mutate the Options map on the returned definition.
	first.Options["injected"] = "evil"

	second, err := parseEngineDefinitionFromJSON(engineJSON)
	require.NoError(t, err)
	require.NotNil(t, second)
	_, poisoned := second.Options["injected"]
	assert.False(t, poisoned, "Options mutation leaked into cached state via shallow map copy")
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

func TestDeepCopyEngineDefinition_NestedFieldsAreIndependent(t *testing.T) {
	cooldown := true
	src := EngineDefinition{
		Options: map[string]any{"nested": []any{map[string]any{"key": "value"}}},
		Provider: ProviderSelection{
			Auth:    &AuthDefinition{Secret: "API_KEY"},
			Request: &RequestShape{Query: map[string]string{"q": "value"}, BodyInject: map[string]string{"body": "value"}},
		},
		Behaviors: &EngineBehaviorDefinition{
			Manifest:     &EngineManifestDefinition{Files: []string{"file"}, PathPrefixes: []string{"prefix"}},
			Network:      &EngineNetworkDefinition{Defaults: []string{"domain"}, ProviderDomains: map[string]string{"provider": "domain"}},
			Installation: &EngineInstallationDefinition{Requirements: []EnginePythonRequirement{{Name: "package"}}},
			ConfigFile:   &EngineConfigFileDefinition{Path: "config"},
			MCP:          &EngineMCPDefinition{ConfigPath: "mcp"},
			Durable: &EngineDurableDefinition{
				Files:        []string{"durable"},
				Installation: []EngineInstallationDefinition{{Cooldown: &cooldown}},
				Execution:    &EngineExecutionDefinition{Args: []string{"arg"}, Env: map[string]string{"key": "value"}},
			},
		},
	}
	dst := deepCopyEngineDefinition(src)
	assert.Equal(t, src, dst)

	dst.Options["nested"].([]any)[0].(map[string]any)["key"] = "changed"
	dst.Provider.Auth.Secret = "changed"
	dst.Provider.Request.Query["q"] = "changed"
	dst.Provider.Request.BodyInject["body"] = "changed"
	dst.Behaviors.Manifest.Files[0] = "changed"
	dst.Behaviors.Manifest.PathPrefixes[0] = "changed"
	dst.Behaviors.Network.Defaults[0] = "changed"
	dst.Behaviors.Network.ProviderDomains["provider"] = "changed"
	dst.Behaviors.Installation.Requirements[0].Name = "changed"
	dst.Behaviors.ConfigFile.Path = "changed"
	dst.Behaviors.MCP.ConfigPath = "changed"
	dst.Behaviors.Durable.Files[0] = "changed"
	*dst.Behaviors.Durable.Installation[0].Cooldown = false
	dst.Behaviors.Durable.Execution.Args[0] = "changed"
	dst.Behaviors.Durable.Execution.Env["key"] = "changed"

	assert.Equal(t, "value", src.Options["nested"].([]any)[0].(map[string]any)["key"])
	assert.Equal(t, "API_KEY", src.Provider.Auth.Secret)
	assert.Equal(t, "value", src.Provider.Request.Query["q"])
	assert.Equal(t, "value", src.Provider.Request.BodyInject["body"])
	assert.Equal(t, []string{"file"}, src.Behaviors.Manifest.Files)
	assert.Equal(t, []string{"prefix"}, src.Behaviors.Manifest.PathPrefixes)
	assert.Equal(t, []string{"domain"}, src.Behaviors.Network.Defaults)
	assert.Equal(t, "domain", src.Behaviors.Network.ProviderDomains["provider"])
	assert.Equal(t, "package", src.Behaviors.Installation.Requirements[0].Name)
	assert.Equal(t, "config", src.Behaviors.ConfigFile.Path)
	assert.Equal(t, "mcp", src.Behaviors.MCP.ConfigPath)
	assert.Equal(t, []string{"durable"}, src.Behaviors.Durable.Files)
	assert.True(t, *src.Behaviors.Durable.Installation[0].Cooldown)
	assert.Equal(t, []string{"arg"}, src.Behaviors.Durable.Execution.Args)
	assert.Equal(t, "value", src.Behaviors.Durable.Execution.Env["key"])
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
