package connectors

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func boundedStringArray() Input {
	return Input{
		Type: "array", Required: true, MinItems: 1, MaxItems: 2,
		Items: &Input{Type: "string", MinLength: 1, MaxLength: 4, Pattern: "^[a-z😀]+$"},
	}
}

func TestHostedMCPStringArrayArguments(t *testing.T) {
	input := boundedStringArray()
	inputs := map[string]Input{"queries": input}
	require.NoError(t, validateInputs(inputs))
	grants := map[string][]string{"queries": {"abc", "😀😀😀😀"}}
	for _, value := range []any{[]string{"abc"}, []any{"abc", "😀😀😀😀"}} {
		require.NoError(t, validateArguments(inputs, map[string]string{"queries": "queries"}, grants, map[string]any{"queries": value}))
	}
	for _, value := range []any{
		nil, "abc", []string(nil), []any{}, []any{"abc", "abc", "abc"},
		[]any{1}, []any{[]any{"abc"}}, []any{""}, []any{"abcde"},
		[]any{"😀😀😀😀😀"}, []any{"ABC"}, []any{"abc", "nope"},
	} {
		require.Error(t, validateArguments(inputs, map[string]string{"queries": "queries"}, grants, map[string]any{"queries": value}), "%#v", value)
	}
	require.Error(t, validateArguments(inputs, nil, nil, map[string]any{}))
	require.Error(t, validateArguments(inputs, nil, nil, map[string]any{"queries": []string{"abc"}, "query": "abc"}))

	property := inputSchema(inputs)["properties"].(map[string]any)["queries"].(map[string]any)
	require.Equal(t, "array", property["type"])
	require.Equal(t, 1, property["minItems"])
	require.Equal(t, 2, property["maxItems"])
	require.Equal(t, map[string]any{"type": "string", "minLength": 1, "maxLength": 4, "pattern": "^[a-z😀]+$"}, property["items"])
}

func TestHostedMCPStringArrayDeclarations(t *testing.T) {
	for name, mutate := range map[string]func(*Input){
		"missing-minimum": func(input *Input) { input.MinItems = 0 },
		"missing-maximum": func(input *Input) { input.MaxItems = 0 },
		"excessive-items": func(input *Input) { input.MaxItems = 101 },
		"inverted-bounds": func(input *Input) { input.MinItems = 3 },
		"missing-items":   func(input *Input) { input.Items = nil },
		"nested-array":    func(input *Input) { input.Items = new(boundedStringArray()) },
		"unbounded-item":  func(input *Input) { input.Items.MaxLength = 0 },
		"numeric-item":    func(input *Input) { input.Items = &Input{Type: "integer"} },
		"item-sanitizer":  func(input *Input) { input.Items.Sanitize = "body" },
		"required-item":   func(input *Input) { input.Items.Required = true },
		"numeric-bound":   func(input *Input) { input.Maximum = new(float64(1)) },
		"string-bound":    func(input *Input) { input.MaxLength = 10 },
		"array-pattern":   func(input *Input) { input.Pattern = "x" },
		"scalar-items":    func(input *Input) { input.Type, input.MaxLength = "string", 10 },
	} {
		t.Run(name, func(t *testing.T) {
			input := boundedStringArray()
			mutate(&input)
			require.Error(t, validateInputs(map[string]Input{"queries": input}))
		})
	}
	spec, err := LoadSpec("testdata/example.yaml")
	require.NoError(t, err)
	read := spec.MCP["get-issue"]
	read.Inputs["queries"] = boundedStringArray()
	spec.MCP["get-issue"] = read
	require.ErrorContains(t, spec.Validate(), "only by hosted HTTP MCP")
}

func TestHostedMCPAPIVersionURL(t *testing.T) {
	for _, query := range []string{
		"", "?api-version=2026-11-01-preview",
		"?", "?token=secret", "?api-version=", "?api-version=v1&token=secret",
		"?api-version=v1&api-version=v2", "?api-version=v1%26token",
		"?api-version=" + strings.Repeat("x", 101), "?api-version=v1#fragment",
	} {
		t.Run(query, func(t *testing.T) {
			spec := loadJiraProvider(t)
			server := spec.HTTPMCP["service-account"]
			server.URL = "https://mcp.atlassian.com/v2/mcp" + query
			spec.HTTPMCP["service-account"] = server
			err := spec.Validate()
			if query == "" || query == "?api-version=2026-11-01-preview" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
