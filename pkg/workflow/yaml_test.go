//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	yamlv3 "go.yaml.in/yaml/v3"
)

func TestExtractTopLevelYAMLSectionPreservesNullInScripts(t *testing.T) {
	const script = `cat <<'EOF'
{
  "a": null,
  "b": null
}
EOF`
	const frontmatterYAML = `on:
  workflow_dispatch: null
steps:
  - name: Write JSON with null values
    run: |-
      cat <<'EOF'
      {
        "a": null,
        "b": null
      }
      EOF
`
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(frontmatterYAML), &frontmatter); err != nil {
		t.Fatal(err)
	}
	compiler := NewCompiler()
	frontmatter["post-steps"] = frontmatter["steps"]
	for _, key := range []string{"steps", "post-steps"} {
		t.Run(key, func(t *testing.T) {
			section := compiler.extractTopLevelYAMLSection(frontmatter, key)
			var decoded map[string][]struct{ Run string }
			if err := yamlv3.Unmarshal([]byte(section), &decoded); err != nil {
				t.Fatalf("invalid YAML: %v\n%s", err, section)
			}
			if steps := decoded[key]; len(steps) != 1 || steps[0].Run != script {
				t.Errorf("expected unchanged script %q, got:\n%s", script, section)
			}
		})
	}
	if got := compiler.extractTopLevelYAMLSection(frontmatter, "on"); !strings.HasSuffix(got, "\n  workflow_dispatch:") {
		t.Errorf("expected an empty workflow_dispatch mapping value, got:\n%s", got)
	}
}

func TestExtractTopLevelYAMLSectionNullValues(t *testing.T) {
	const input = `steps:
  - env:
      EMPTY: null
    with:
      optional: null
      list:
        - null
        - value
`
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(input), &frontmatter); err != nil {
		t.Fatal(err)
	}
	section := NewCompiler().extractTopLevelYAMLSection(frontmatter, "steps")
	for _, want := range []string{"EMPTY:", "optional:", "- null"} {
		found := false
		for line := range strings.SplitSeq(section, "\n") {
			if strings.TrimSpace(line) == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in:\n%s", want, section)
		}
	}
	var decoded struct {
		Steps []struct {
			Env  map[string]any
			With map[string]any
		}
	}
	if err := yamlv3.Unmarshal([]byte(section), &decoded); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, section)
	}
	if len(decoded.Steps) != 1 {
		t.Fatalf("expected one step, got %d", len(decoded.Steps))
	}
	step := decoded.Steps[0]
	if value, ok := step.Env["EMPTY"]; !ok || value != nil {
		t.Errorf("env null lost: %#v", step.Env)
	}
	if value, ok := step.With["optional"]; !ok || value != nil {
		t.Errorf("with null lost: %#v", step.With)
	}
	list, ok := step.With["list"].([]any)
	if !ok || len(list) != 2 || list[0] != nil || list[1] != "value" {
		t.Errorf("sequence null lost: %#v", step.With["list"])
	}
}

func TestMarshalYAMLWithImplicitMappingNullsFlowStyle(t *testing.T) {
	input := map[string]any{"key": nil, "list": []any{nil}}
	encoded, err := marshalYAMLWithImplicitMappingNulls(input, yaml.Flow(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "key: null") || !strings.Contains(string(encoded), "[null]") {
		t.Errorf("expected explicit flow-style nulls, got %s", encoded)
	}
	var decoded map[string]any
	if err := yamlv3.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("invalid flow-style YAML: %v\n%s", err, encoded)
	}
	if value, ok := decoded["key"]; !ok || value != nil {
		t.Errorf("flow mapping null lost: %#v", decoded)
	}
}

func TestUnquoteYAMLKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		key      string
		expected string
	}{
		{
			name: "unquote 'on' at start of line",
			input: `"on":
  issues:
    types:
    - opened`,
			key: "on",
			expected: `on:
  issues:
    types:
    - opened`,
		},
		{
			name: "unquote 'on' with indentation",
			input: `  "on":
    issues:
      types:
      - opened`,
			key: "on",
			expected: `  on:
    issues:
      types:
      - opened`,
		},
		{
			name:     "do not unquote 'on' in middle of line",
			input:    `key: "on":value`,
			key:      "on",
			expected: `key: "on":value`,
		},
		{
			name:     "do not unquote 'on' in string value",
			input:    `description: "This is about on: something"`,
			key:      "on",
			expected: `description: "This is about on: something"`,
		},
		{
			name: "unquote multiple occurrences at start of lines",
			input: `"on":
  issues:
    types:
    - opened
"on":
  push:
    branches:
    - main`,
			key: "on",
			expected: `on:
  issues:
    types:
    - opened
on:
  push:
    branches:
    - main`,
		},
		{
			name: "unquote other keys",
			input: `"if":
  github.actor == 'bot'`,
			key: "if",
			expected: `if:
  github.actor == 'bot'`,
		},
		{
			name: "handle key with special regex characters",
			input: `"key.with.dots":
  value: test`,
			key: "key.with.dots",
			expected: `key.with.dots:
  value: test`,
		},
		{
			name: "no change when key is not quoted",
			input: `on:
  issues:
    types:
    - opened`,
			key: "on",
			expected: `on:
  issues:
    types:
    - opened`,
		},
		{
			name: "unquote with tabs",
			input: `		"on":
		  issues:`,
			key: "on",
			expected: `		on:
		  issues:`,
		},
		{
			name:     "empty string",
			input:    "",
			key:      "on",
			expected: "",
		},
		{
			name:     "only newlines",
			input:    "\n\n\n",
			key:      "on",
			expected: "\n\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UnquoteYAMLKey(tt.input, tt.key)
			if result != tt.expected {
				t.Errorf("UnquoteYAMLKey() failed\nInput:\n%s\n\nExpected:\n%s\n\nGot:\n%s",
					tt.input, tt.expected, result)
			}
		})
	}
}

func TestUnquoteYAMLTopLevelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		key      string
		expected string
	}{
		{
			name: "unquotes top-level on key",
			input: `"on":
  workflow_dispatch:`,
			key: "on",
			expected: `on:
  workflow_dispatch:`,
		},
		{
			name: "does not unquote nested key",
			input: `parent:
  "on":
    value: true`,
			key: "on",
			expected: `parent:
  "on":
    value: true`,
		},
		{
			name: "no change when top-level key already unquoted",
			input: `on:
  push:`,
			key: "on",
			expected: `on:
  push:`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UnquoteYAMLTopLevelKey(tt.input, tt.key)
			if result != tt.expected {
				t.Errorf("UnquoteYAMLTopLevelKey() failed\nInput:\n%s\n\nExpected:\n%s\n\nGot:\n%s",
					tt.input, tt.expected, result)
			}
		})
	}
}

func TestMarshalWithFieldOrder(t *testing.T) {
	tests := []struct {
		name           string
		data           map[string]any
		priorityFields []string
		expectedOrder  []string
	}{
		{
			name: "on section with events in priority order",
			data: map[string]any{
				"workflow_dispatch": map[string]any{},
				"push": map[string]any{
					"branches": []string{"main"},
				},
				"issues": map[string]any{
					"types": []string{"opened"},
				},
			},
			priorityFields: []string{"push", "pull_request", "issues", "workflow_dispatch"},
			expectedOrder:  []string{"push", "issues", "workflow_dispatch"},
		},
		{
			name: "permissions with mixed order",
			data: map[string]any{
				"pull-requests": "write",
				"contents":      "read",
				"issues":        "write",
			},
			priorityFields: []string{"actions", "contents", "issues", "pull-requests"},
			expectedOrder:  []string{"contents", "issues", "pull-requests"},
		},
		{
			name: "alphabetical fallback for non-priority fields",
			data: map[string]any{
				"zebra": "value",
				"alpha": "value",
				"beta":  "value",
			},
			priorityFields: []string{},
			expectedOrder:  []string{"alpha", "beta", "zebra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlBytes, err := MarshalWithFieldOrder(tt.data, tt.priorityFields)
			if err != nil {
				t.Errorf("MarshalWithFieldOrder() error = %v", err)
				return
			}

			yamlStr := string(yamlBytes)
			t.Logf("Generated YAML:\n%s", yamlStr)

			// Parse the YAML to extract the field order
			var parsed yaml.MapSlice
			if err := yaml.Unmarshal(yamlBytes, &parsed); err != nil {
				t.Errorf("Failed to parse generated YAML: %v", err)
				return
			}

			// Extract the field names in order
			var actualOrder []string
			for _, item := range parsed {
				if key, ok := item.Key.(string); ok {
					actualOrder = append(actualOrder, key)
				}
			}

			// Verify the order matches expected
			if len(actualOrder) != len(tt.expectedOrder) {
				t.Errorf("Expected %d fields, got %d", len(tt.expectedOrder), len(actualOrder))
			}

			for i, expected := range tt.expectedOrder {
				if i >= len(actualOrder) || actualOrder[i] != expected {
					t.Errorf("Field %d: expected %q, got %q. Full order: %v", i, expected, actualOrder[i], actualOrder)
				}
			}
		})
	}
}

func TestMarshalWithFieldOrder_OrdersNestedEnvWithSecretsRecursively(t *testing.T) {
	data := map[string]any{
		"env": map[string]string{
			"ZETA_WORKFLOW":  "zeta",
			"ALPHA_WORKFLOW": "alpha",
		},
		"secrets": map[string]string{
			"ZETA_SECRET":  "${{ secrets.ZETA_SECRET }}",
			"ALPHA_SECRET": "${{ secrets.ALPHA_SECRET }}",
		},
		"steps": []any{
			map[string]any{
				"name": "Deterministic action",
				"uses": "actions/checkout@v4",
				"with": map[string]any{
					"zeta-input": "zeta",
					"alpha-input": map[string]any{
						"zeta-child":  "zeta",
						"alpha-child": "alpha",
					},
				},
				"env": map[string]string{
					"ZETA_STEP":  "zeta",
					"ALPHA_STEP": "alpha",
				},
			},
		},
	}

	yamlBytes, err := MarshalWithFieldOrder(data, []string{"env", "secrets", "steps"})
	if err != nil {
		t.Fatalf("MarshalWithFieldOrder() error = %v", err)
	}

	yamlStr := string(yamlBytes)
	assertOrder := func(before, after string) {
		t.Helper()
		beforeIndex := strings.Index(yamlStr, before)
		afterIndex := strings.Index(yamlStr, after)
		if beforeIndex == -1 || afterIndex == -1 {
			t.Fatalf("expected both %q and %q in YAML:\n%s", before, after, yamlStr)
		}
		if beforeIndex >= afterIndex {
			t.Fatalf("expected %q before %q in YAML:\n%s", before, after, yamlStr)
		}
	}

	assertOrder("  ALPHA_WORKFLOW:", "  ZETA_WORKFLOW:")
	assertOrder("  ALPHA_SECRET:", "  ZETA_SECRET:")
	assertOrder("    alpha-input:", "    zeta-input:")
	assertOrder("      alpha-child:", "      zeta-child:")
	assertOrder("    ALPHA_STEP:", "    ZETA_STEP:")
}

func TestExtractTopLevelYAMLSectionExcludesOnNeeds(t *testing.T) {
	compiler := NewCompiler()

	frontmatter := map[string]any{
		"on": map[string]any{
			"needs":             []any{"custom_job"},
			"workflow_dispatch": nil,
		},
	}

	result := compiler.extractTopLevelYAMLSection(frontmatter, "on")

	if strings.Contains(result, "needs:") {
		t.Errorf("on.needs must not appear in compiled on: section, but found it in:\n%s", result)
	}
	if !strings.Contains(result, "workflow_dispatch") {
		t.Errorf("workflow_dispatch should be present in the compiled on: section:\n%s", result)
	}
	// Verify the original frontmatter["on"] map is not mutated
	onMap, ok := frontmatter["on"].(map[string]any)
	if !ok {
		t.Fatal("frontmatter[\"on\"] should remain a map[string]any")
	}
	if _, hasNeeds := onMap["needs"]; !hasNeeds {
		t.Error("extractTopLevelYAMLSection must not mutate the original frontmatter[\"on\"] map")
	}
}

func TestExtractTopLevelYAMLSectionWithOrdering(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name          string
		frontmatter   map[string]any
		key           string
		expectedOrder []string
	}{
		{
			name: "on section orders events alphabetically",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": map[string]any{},
					"push": map[string]any{
						"branches": []string{"main"},
					},
					"issues": map[string]any{
						"types": []string{"opened"},
					},
				},
			},
			key:           "on",
			expectedOrder: []string{"issues", "push", "workflow_dispatch"},
		},
		{
			name: "permissions section orders alphabetically",
			frontmatter: map[string]any{
				"permissions": map[string]any{
					"pull-requests": "write",
					"contents":      "read",
					"issues":        "write",
				},
			},
			key:           "permissions",
			expectedOrder: []string{"contents", "issues", "pull-requests"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.extractTopLevelYAMLSection(tt.frontmatter, tt.key)
			if result == "" {
				t.Error("Expected non-empty result")
				return
			}

			t.Logf("Generated YAML:\n%s", result)

			// Parse the YAML to verify field order
			var parsed map[string]yaml.MapSlice
			if err := yaml.Unmarshal([]byte(result), &parsed); err != nil {
				t.Errorf("Failed to parse generated YAML: %v", err)
				return
			}

			section, ok := parsed[tt.key]
			if !ok {
				t.Errorf("Expected section %q not found in parsed YAML", tt.key)
				return
			}

			// Extract field names in order
			var actualOrder []string
			for _, item := range section {
				if key, ok := item.Key.(string); ok {
					actualOrder = append(actualOrder, key)
				}
			}

			// Verify order
			if len(actualOrder) != len(tt.expectedOrder) {
				t.Errorf("Expected %d fields, got %d", len(tt.expectedOrder), len(actualOrder))
			}

			for i, expected := range tt.expectedOrder {
				if i >= len(actualOrder) || actualOrder[i] != expected {
					t.Errorf("Field %d: expected %q, got %q. Full order: %v", i, expected, actualOrder[i], actualOrder)
				}
			}

			// Also check that the YAML string has the fields in the right order
			lines := strings.Split(result, "\n")
			fieldLineIndices := make(map[string]int)
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				for _, field := range tt.expectedOrder {
					if strings.HasPrefix(trimmed, field+":") {
						fieldLineIndices[field] = i
					}
				}
			}

			// Verify that the line indices are in ascending order for the expected fields
			for i := 1; i < len(tt.expectedOrder); i++ {
				prev := tt.expectedOrder[i-1]
				curr := tt.expectedOrder[i]
				prevIdx, prevOk := fieldLineIndices[prev]
				currIdx, currOk := fieldLineIndices[curr]
				if prevOk && currOk && prevIdx >= currIdx {
					t.Errorf("Field %q (line %d) should come before %q (line %d)", prev, prevIdx, curr, currIdx)
				}
			}
		})
	}
}

// BenchmarkUnquoteYAMLKey measures single-pass regex replacement performance.
// This benchmark guards against regressions where the implementation calls
// FindStringSubmatch inside a ReplaceAllStringFunc callback (double regex pass).
func BenchmarkUnquoteYAMLKey(b *testing.B) {
	// A realistic workflow YAML with the "on" key quoted by the marshaler
	yamlStr := `"on":
  push:
    branches:
    - main
  pull_request:
    types:
    - opened
    - synchronize
  issues:
    types:
    - opened
  workflow_dispatch:
jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
    - name: Run
      run: echo hello
`
	b.ReportAllocs()
	for b.Loop() {
		_ = UnquoteYAMLKey(yamlStr, "on")
	}
}

func TestFormatYAMLValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		// string cases
		{name: "string: true keyword quoted", value: "true", expected: "'true'"},
		{name: "string: false keyword quoted", value: "false", expected: "'false'"},
		{name: "string: null keyword quoted", value: "null", expected: "'null'"},
		{name: "string: numeric string quoted", value: "42", expected: "'42'"},
		{name: "string: float string quoted", value: "3.14", expected: "'3.14'"},
		{name: "string: plain string quoted", value: "hello", expected: "'hello'"},
		{name: "string: empty string quoted", value: "", expected: "''"},
		// bool cases
		{name: "bool: true", value: true, expected: "true"},
		{name: "bool: false", value: false, expected: "false"},
		// integer cases
		{name: "int", value: int(42), expected: "42"},
		{name: "int8", value: int8(8), expected: "8"},
		{name: "int16", value: int16(16), expected: "16"},
		{name: "int32", value: int32(32), expected: "32"},
		{name: "int64", value: int64(64), expected: "64"},
		{name: "int: zero", value: int(0), expected: "0"},
		{name: "int: negative", value: int(-1), expected: "-1"},
		// unsigned integer cases
		{name: "uint", value: uint(10), expected: "10"},
		{name: "uint8", value: uint8(8), expected: "8"},
		{name: "uint16", value: uint16(16), expected: "16"},
		{name: "uint32", value: uint32(32), expected: "32"},
		{name: "uint64", value: uint64(64), expected: "64"},
		// float cases
		{name: "float32", value: float32(1.5), expected: "1.5"},
		{name: "float64", value: float64(2.5), expected: "2.5"},
		// default fallback
		{name: "nil: quoted", value: nil, expected: "'<nil>'"},
		{name: "struct: quoted", value: struct{ A int }{A: 1}, expected: "'{1}'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatYAMLValue(tt.value)
			if result != tt.expected {
				t.Errorf("formatYAMLValue(%v) = %q, want %q", tt.value, result, tt.expected)
			}
		})
	}
}

func TestPrepareNestedMapValueForYAML(t *testing.T) {
	t.Run("map[string]any under with field is ordered", func(t *testing.T) {
		value := map[string]any{"b": 1, "a": 2}
		result := prepareNestedMapValueForYAML("with", value)
		ms, ok := result.(yaml.MapSlice)
		if !ok {
			t.Fatalf("expected yaml.MapSlice, got %T", result)
		}
		if len(ms) != 2 || ms[0].Key != "a" || ms[1].Key != "b" {
			t.Errorf("expected sorted keys a,b; got %+v", ms)
		}
	})

	t.Run("map[string]any under env field is ordered", func(t *testing.T) {
		value := map[string]any{"z": 1, "a": 2}
		result := prepareNestedMapValueForYAML("env", value)
		ms, ok := result.(yaml.MapSlice)
		if !ok {
			t.Fatalf("expected yaml.MapSlice, got %T", result)
		}
		if len(ms) != 2 || ms[0].Key != "a" {
			t.Errorf("expected sorted keys; got %+v", ms)
		}
	})

	t.Run("map[string]any under secrets field is ordered", func(t *testing.T) {
		value := map[string]any{"token": "x"}
		result := prepareNestedMapValueForYAML("secrets", value)
		if _, ok := result.(yaml.MapSlice); !ok {
			t.Fatalf("expected yaml.MapSlice, got %T", result)
		}
	})

	t.Run("map[string]any under other field is recursively copied not ordered", func(t *testing.T) {
		value := map[string]any{"nested": map[string]any{"x": 1}}
		result := prepareNestedMapValueForYAML("other", value)
		copied, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", result)
		}
		if _, ok := copied["nested"]; !ok {
			t.Errorf("expected nested key to be preserved")
		}
		value["added"] = 2
		if _, ok := copied["added"]; ok {
			t.Errorf("expected top-level map copy to be independent of original")
		}
		value["nested"].(map[string]any)["x"] = 2
		nestedCopied, ok := copied["nested"].(map[string]any)
		if !ok {
			t.Fatalf("expected nested map[string]any, got %T", copied["nested"])
		}
		if nestedCopied["x"] != 1 {
			t.Errorf("expected nested map copy to be independent of original; got x=%v", nestedCopied["x"])
		}
	})

	t.Run("map[string]string under with field is ordered", func(t *testing.T) {
		value := map[string]string{"b": "1", "a": "2"}
		result := prepareNestedMapValueForYAML("with", value)
		if _, ok := result.(yaml.MapSlice); !ok {
			t.Fatalf("expected yaml.MapSlice, got %T", result)
		}
	})

	t.Run("map[string]string under other field returned unchanged", func(t *testing.T) {
		value := map[string]string{"b": "1", "a": "2"}
		result := prepareNestedMapValueForYAML("other", value)
		m, ok := result.(map[string]string)
		if !ok {
			t.Fatalf("expected map[string]string, got %T", result)
		}
		if m["a"] != "2" || m["b"] != "1" {
			t.Errorf("expected value unchanged; got %+v", m)
		}
	})

	t.Run("[]any recursively processes elements", func(t *testing.T) {
		value := []any{
			map[string]any{"b": 1, "a": 2},
			"plain",
		}
		result := prepareNestedMapValueForYAML("other", value)
		arr, ok := result.([]any)
		if !ok {
			t.Fatalf("expected []any, got %T", result)
		}
		if len(arr) != 2 {
			t.Fatalf("expected 2 elements, got %d", len(arr))
		}
		if arr[1] != "plain" {
			t.Errorf("expected second element unchanged; got %v", arr[1])
		}
		if _, ok := arr[0].(map[string]any); !ok {
			t.Errorf("expected nested map to remain a map[string]any (fieldName empty means no special ordering); got %T", arr[0])
		}
	})

	t.Run("yaml.MapSlice with string keys recurses using key as fieldName", func(t *testing.T) {
		value := yaml.MapSlice{
			{Key: "with", Value: map[string]any{"b": 1, "a": 2}},
			{Key: "plain", Value: "value"},
		}
		result := prepareNestedMapValueForYAML("other", value)
		ms, ok := result.(yaml.MapSlice)
		if !ok {
			t.Fatalf("expected yaml.MapSlice, got %T", result)
		}
		if len(ms) != 2 {
			t.Fatalf("expected 2 items, got %d", len(ms))
		}
		if ms[0].Key != "with" {
			t.Errorf("expected first key 'with', got %v", ms[0].Key)
		}
		if _, ok := ms[0].Value.(yaml.MapSlice); !ok {
			t.Errorf("expected nested 'with' value to become ordered yaml.MapSlice, got %T", ms[0].Value)
		}
		if ms[1].Value != "value" {
			t.Errorf("expected plain value unchanged; got %v", ms[1].Value)
		}
	})

	t.Run("yaml.MapSlice with non-string key falls back to empty fieldName", func(t *testing.T) {
		value := yaml.MapSlice{
			{Key: 42, Value: "x"},
		}
		result := prepareNestedMapValueForYAML("other", value)
		ms, ok := result.(yaml.MapSlice)
		if !ok {
			t.Fatalf("expected yaml.MapSlice, got %T", result)
		}
		if len(ms) != 1 || ms[0].Key != 42 {
			t.Errorf("expected non-string key preserved; got %+v", ms)
		}
		if ms[0].Value != "x" {
			t.Errorf("expected value unchanged; got %v", ms[0].Value)
		}
	})

	t.Run("default case returns value unchanged for scalar types", func(t *testing.T) {
		for _, v := range []any{42, "hello", true, 3.14, nil} {
			result := prepareNestedMapValueForYAML("field", v)
			if result != v {
				t.Errorf("expected %v unchanged, got %v", v, result)
			}
		}
	})
}
