//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngineCatalog_IDs verifies that IDs() returns all engine IDs in sorted order.
func TestEngineCatalog_IDs(t *testing.T) {
	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	ids := catalog.IDs()
	require.NotEmpty(t, ids, "IDs() should return a non-empty list")

	// Verify all built-in engines are present
	expectedIDs := []string{"agy", "claude", "codex", "copilot", "gemini", "pi"}
	assert.Equal(t, expectedIDs, ids, "IDs() should return all built-in engines in sorted order")

	// Verify the list is sorted
	sorted := make([]string, len(ids))
	copy(sorted, ids)
	sort.Strings(sorted)
	assert.Equal(t, sorted, ids, "IDs() should return IDs in sorted order")
}

// TestEngineCatalog_All verifies that All() returns all definitions in sorted ID order.
func TestEngineCatalog_All(t *testing.T) {
	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	defs := catalog.All()
	require.NotEmpty(t, defs, "All() should return a non-empty list")
	assert.Len(t, defs, len(catalog.IDs()), "All() should have same length as IDs()")

	ids := catalog.IDs()
	for i, def := range defs {
		assert.Equal(t, ids[i], def.ID, "All()[%d].ID should match IDs()[%d]", i, i)
		assert.NotEmpty(t, def.DisplayName, "All()[%d].DisplayName should not be empty", i)
	}
}

// engineSchemaOneOfVariants parses the main workflow schema and returns the
// type identifiers of each variant in engine_config.oneOf for structural assertions.
func engineSchemaOneOfVariants(t *testing.T) []map[string]any {
	t.Helper()

	schemaBytes, err := os.ReadFile("../parser/schemas/main_workflow_schema.json")
	require.NoError(t, err, "should be able to read main_workflow_schema.json")

	var schema map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &schema), "schema should be valid JSON")

	defs, ok := schema["$defs"].(map[string]any)
	require.True(t, ok, "schema should have $defs")

	engineConfig, ok := defs["engine_config"].(map[string]any)
	require.True(t, ok, "$defs should have engine_config")

	oneOf, ok := engineConfig["oneOf"].([]any)
	require.True(t, ok, "engine_config should have oneOf")

	variants := make([]map[string]any, 0, len(oneOf))
	for _, v := range oneOf {
		if m, ok := v.(map[string]any); ok {
			variants = append(variants, m)
		}
	}
	return variants
}

// TestEngineCatalog_BuiltInsPresent verifies that built-in engines are always
// registered in the catalog with stable IDs.
func TestEngineCatalog_BuiltInsPresent(t *testing.T) {
	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	expected := []string{"claude", "codex", "copilot", "gemini", "pi"}
	catalogIDs := catalog.IDs()
	for _, id := range expected {
		assert.Contains(t, catalogIDs, id,
			"built-in engine %q must always be present in the catalog", id)
	}
}

// TestEngineCatalogMatchesSchema asserts that the engine_config schema has the expected
// structure: a plain-string variant (for built-ins and named catalog entries), an
// object-with-id variant, and an inline-definition variant (object-with-runtime).
// A failure here means the schema structure has changed unexpectedly.
func TestEngineCatalogMatchesSchema(t *testing.T) {
	variants := engineSchemaOneOfVariants(t)

	require.Len(t, variants, 6, "engine_config oneOf should have exactly 6 variants: string, object-with-id, object-with-runtime, engine-definition, mcp-only, model-only")

	// Variant 0: plain string (no enum — allows built-ins and custom named catalog entries)
	assert.Equal(t, "string", variants[0]["type"],
		"first variant should be type string")
	assert.Nil(t, variants[0]["enum"],
		"string variant must NOT have an enum so that named catalog entries are allowed")

	// Variant 1: object with 'id' field for extended engine configuration
	assert.Equal(t, "object", variants[1]["type"],
		"second variant should be type object (extended config with id)")
	props1, ok := variants[1]["properties"].(map[string]any)
	require.True(t, ok, "second variant should have properties")
	assert.Contains(t, props1, "id",
		"second variant should have an 'id' property")
	assert.Contains(t, props1, "auth",
		"second variant should have an 'auth' property")
	idProp, ok := props1["id"].(map[string]any)
	require.True(t, ok, "id property should be a map")
	assert.Nil(t, idProp["enum"],
		"id property must NOT have an enum so that named catalog entries are allowed")

	// Variant 2: object with 'runtime' sub-object for inline definitions
	assert.Equal(t, "object", variants[2]["type"],
		"third variant should be type object (inline definition with runtime)")
	props2, ok := variants[2]["properties"].(map[string]any)
	require.True(t, ok, "third variant should have properties")
	assert.Contains(t, props2, "runtime",
		"third variant should have a 'runtime' property for inline engine definitions")
	assert.Contains(t, props2, "provider",
		"third variant should have a 'provider' property for inline engine definitions")

	// Variant 3: engine definition form used in builtin engine shared workflow files
	assert.Equal(t, "object", variants[3]["type"],
		"fourth variant should be type object (engine definition)")
	props3, ok := variants[3]["properties"].(map[string]any)
	require.True(t, ok, "fourth variant should have properties")
	assert.Contains(t, props3, "id", "engine definition variant should have an 'id' property")
	assert.Contains(t, props3, "display-name", "engine definition variant should have a 'display-name' property")
	assert.Contains(t, props3, "provider", "engine definition variant should have a 'provider' property")
	assert.Contains(t, props3, "experimental", "engine definition variant should have an 'experimental' property")
	assert.Contains(t, props3, "auth", "engine definition variant should have an 'auth' property")
	assert.Contains(t, props3, "behaviors", "engine definition variant should have a 'behaviors' property")

	// Variant 4: mcp-only form for shared workflow imports (no engine id required)
	assert.Equal(t, "object", variants[4]["type"],
		"fifth variant should be type object (mcp-only for shared workflows)")
	props4, ok := variants[4]["properties"].(map[string]any)
	require.True(t, ok, "fifth variant should have properties")
	assert.Contains(t, props4, "mcp",
		"mcp-only variant should have an 'mcp' property")
	assert.NotContains(t, props4, "id",
		"mcp-only variant must NOT have an 'id' property")

	// Variant 5: model-only form — expresses a model preference without specifying an engine id
	assert.Equal(t, "object", variants[5]["type"],
		"sixth variant should be type object (model-only preference)")
	props5, ok := variants[5]["properties"].(map[string]any)
	require.True(t, ok, "sixth variant should have properties")
	assert.Contains(t, props5, "model",
		"model-only variant should have a 'model' property")
	assert.NotContains(t, props5, "id",
		"model-only variant must NOT have an 'id' property")
	assert.NotContains(t, props5, "runtime",
		"model-only variant must NOT have a 'runtime' property")
}
