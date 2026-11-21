package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateImportInputType_String covers valid and invalid string inputs.
func TestValidateImportInputType_String(t *testing.T) {
	err := validateImportInputType("name", "hello", "string", nil, "owner/repo/import.md")
	require.NoError(t, err)

	err = validateImportInputType("name", 42, "string", nil, "owner/repo/import.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a string")

	bounded := map[string]any{"maxLength": 150}
	require.NoError(t, validateImportInputType("name", strings.Repeat("é", 150), "string", bounded, "import.md"))
	err = validateImportInputType("name", strings.Repeat("é", 151), "string", bounded, "import.md")
	require.ErrorContains(t, err, "at most 150 Unicode characters")
	require.Error(t, validateImportInputType("name", "a", "string", map[string]any{"maxLength": -1}, "import.md"))
}

// TestValidateImportInputType_NumberAllTypes covers all numeric Go types accepted
// from YAML parsers, plus a rejection case.
func TestValidateImportInputType_NumberAllTypes(t *testing.T) {
	validValues := []any{
		int(1), int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
		float32(1.5), float64(1.5),
	}
	for _, v := range validValues {
		err := validateImportInputType("count", v, "number", nil, "owner/repo/import.md")
		require.NoErrorf(t, err, "expected %T to be accepted as number", v)
	}

	err := validateImportInputType("count", "not-a-number", "number", nil, "owner/repo/import.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a number")

	bounded := map[string]any{"minimum": 1, "maximum": 5}
	require.NoError(t, validateImportInputType("count", 1.5, "number", bounded, "owner/repo/import.md"))
	err = validateImportInputType("count", 6, "number", bounded, "owner/repo/import.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be at most 5")
}

func TestValidateImportInputType_IntegerBounds(t *testing.T) {
	paramDef := map[string]any{"minimum": 1, "maximum": 5}
	for _, value := range []any{1, int64(3), float64(5)} {
		require.NoError(t, validateImportInputType("count", value, "integer", paramDef, "owner/repo/import.md"))
	}

	for _, test := range []struct {
		value any
		error string
	}{
		{0, "must be at least 1"},
		{6, "must be at most 5"},
		{2.5, "must be an integer"},
		{"3", "must be an integer"},
	} {
		err := validateImportInputType("count", test.value, "integer", paramDef, "owner/repo/import.md")
		require.Error(t, err)
		assert.Contains(t, err.Error(), test.error)
	}
}

// TestValidateImportInputType_Boolean covers valid and invalid boolean inputs.
func TestValidateImportInputType_Boolean(t *testing.T) {
	err := validateImportInputType("flag", true, "boolean", nil, "owner/repo/import.md")
	require.NoError(t, err)

	err = validateImportInputType("flag", "true", "boolean", nil, "owner/repo/import.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a boolean")
}

// TestValidateImportInputType_Choice covers the choice type: non-string rejection,
// no options declared, matching option, and non-matching option.
func TestValidateImportInputType_Choice(t *testing.T) {
	// Non-string value is rejected.
	err := validateImportInputType("level", 5, "choice", nil, "owner/repo/import.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a string for choice type")

	// No "options" key declared: falls through without error.
	err = validateImportInputType("level", "low", "choice", map[string]any{}, "owner/repo/import.md")
	require.NoError(t, err)

	// Options declared but not a []any: falls through the type switch (options ignored) -> no match -> error.
	err = validateImportInputType("level", "low", "choice", map[string]any{"options": "not-a-list"}, "owner/repo/import.md")
	require.NoError(t, err)

	// Matching option.
	paramDef := map[string]any{"options": []any{"low", "medium", "high"}}
	err = validateImportInputType("level", "medium", "choice", paramDef, "owner/repo/import.md")
	require.NoError(t, err)

	// Non-matching option.
	err = validateImportInputType("level", "extreme", "choice", paramDef, "owner/repo/import.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not in the allowed options")

	// Options list containing a non-string entry is skipped without panicking.
	paramDefMixed := map[string]any{"options": []any{42, "low"}}
	err = validateImportInputType("level", "low", "choice", paramDefMixed, "owner/repo/import.md")
	require.NoError(t, err)
}

// TestValidateImportInputType_Array covers non-array rejection, no items schema,
// items schema without a type, matching item types, and recursive rejection.
func TestValidateImportInputType_Array(t *testing.T) {
	// Non-array value is rejected.
	err := validateImportInputType("tags", "not-an-array", "array", nil, "owner/repo/import.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an array")

	// No "items" schema declared: any array is accepted.
	err = validateImportInputType("tags", []any{"a", "b"}, "array", map[string]any{}, "owner/repo/import.md")
	require.NoError(t, err)

	// Items schema present but not a map: no item type -> accepted.
	err = validateImportInputType("tags", []any{"a"}, "array", map[string]any{"items": "not-a-map"}, "owner/repo/import.md")
	require.NoError(t, err)

	// Items schema with empty type: accepted without recursing.
	err = validateImportInputType("tags", []any{"a"}, "array", map[string]any{"items": map[string]any{}}, "owner/repo/import.md")
	require.NoError(t, err)

	// Items schema with matching string type: all items validate successfully.
	arrParamDef := map[string]any{"items": map[string]any{"type": "string"}}
	err = validateImportInputType("tags", []any{"a", "b", "c"}, "array", arrParamDef, "owner/repo/import.md")
	require.NoError(t, err)

	// Items schema with string type but an invalid item: recursive call surfaces the error
	// with an indexed item name.
	err = validateImportInputType("tags", []any{"a", 5}, "array", arrParamDef, "owner/repo/import.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tags[1]")
	assert.Contains(t, err.Error(), "must be a string")
}

// TestValidateImportInputType_Object delegates to validateObjectInput; verify the
// delegation occurs and both success and failure paths propagate correctly.
func TestValidateImportInputType_Object(t *testing.T) {
	// A nil paramDef / no "properties" key: validateObjectInput should not error for
	// a map value regardless of properties (delegation smoke test).
	err := validateImportInputType("config", map[string]any{"a": 1}, "object", map[string]any{}, "owner/repo/import.md")
	require.NoError(t, err)

	// Non-map value for an object type should produce an error via delegation.
	err = validateImportInputType("config", "not-an-object", "object", map[string]any{}, "owner/repo/import.md")
	require.Error(t, err)
}

// TestValidateImportInputType_UnknownDeclaredType verifies an unrecognized declared
// type falls through the switch without validation and returns nil.
func TestValidateImportInputType_UnknownDeclaredType(t *testing.T) {
	err := validateImportInputType("field", "anything", "unknown-type", nil, "owner/repo/import.md")
	require.NoError(t, err)
}
