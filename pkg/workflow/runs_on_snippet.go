package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var runsOnSnippetLog = logger.New("workflow:runs_on_snippet")

// RunsOnValue is a JSON-deserializable type for runs_on/runs-on fields that
// accept either a single runner label string or an array of runner label
// strings (e.g. aw.json's maintenance.runs_on and safe-outputs.jobs runs-on).
// When unmarshalled, a plain string is normalised to a single-element slice so
// the rest of the code works with a uniform []string type.
type RunsOnValue []string

// UnmarshalJSON implements json.Unmarshaler, accepting either a JSON string or
// a JSON array of strings for the runs_on field.
func (r *RunsOnValue) UnmarshalJSON(data []byte) error {
	// Try plain string first (runs_on: "ubuntu-latest")
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*r = RunsOnValue{s}
		return nil
	}

	// Try array of strings (runs_on: ["self-hosted", "linux"])
	var ss []string
	if err := json.Unmarshal(data, &ss); err != nil {
		return fmt.Errorf("runs_on value is not recognized: %w. Expected a string or array of strings, for example: runs_on: \"ubuntu-latest\"", err)
	}
	*r = RunsOnValue(ss)
	return nil
}

// FormatRunsOn serialises a RunsOnValue to a YAML-compatible string that can
// be inlined directly after "runs-on: " in a generated workflow.
//
//   - empty / nil  → defaultRunsOn is returned
//   - single label → the label string (e.g. "ubuntu-latest")
//   - multiple labels → JSON-encoded flow sequence, e.g. ["self-hosted","linux"]
//
// For multi-label values json.Marshal is used so that any characters that are
// special in YAML or JSON (quotes, backslashes, …) are properly escaped.
// The schema already forbids newlines and control characters, providing a
// defence-in-depth against YAML injection.
func FormatRunsOn(runsOn RunsOnValue, defaultRunsOn string) string {
	if len(runsOn) == 0 {
		return defaultRunsOn
	}
	if len(runsOn) == 1 {
		if runsOn[0] == "" {
			return defaultRunsOn
		}
		return runsOn[0]
	}
	// Multiple labels: use json.Marshal to produce a properly-escaped YAML
	// flow sequence.  A JSON array is valid YAML flow sequence notation.
	encoded, err := json.Marshal([]string(runsOn))
	if err != nil {
		// []string marshalling never fails; fall back to the default just in case.
		return defaultRunsOn
	}
	return string(encoded)
}

func runsOnMarshalOptions() []yaml.EncodeOption {
	opts := append([]yaml.EncodeOption{}, DefaultMarshalOptions...)
	return append(opts, yaml.IndentSequence(true))
}

// renderRunsOnSnippet serializes a runs-on value into a "runs-on: ..." YAML snippet.
// Returns empty string for empty/unset values.
func renderRunsOnSnippet(value any) string {
	if isEmptyRunsOnValue(value) {
		return ""
	}

	var yamlBytes []byte
	var err error
	if valueMap, ok := value.(map[string]any); ok {
		orderedValue := OrderMapFields(valueMap, []string{})
		yamlBytes, err = marshalYAMLWithOptions(yaml.MapSlice{{Key: "runs-on", Value: orderedValue}}, runsOnMarshalOptions()...)
	} else {
		yamlBytes, err = marshalYAMLWithOptions(map[string]any{"runs-on": value}, runsOnMarshalOptions()...)
	}
	if err != nil {
		runsOnSnippetLog.Printf("Failed to marshal runs-on snippet: %v", err)
		return ""
	}

	return strings.TrimSuffix(string(yamlBytes), "\n")
}

func normalizeRunsOnSnippet(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	snippet := value
	if !strings.HasPrefix(snippet, "runs-on:") {
		snippet = "runs-on: " + snippet
	}

	var snippetMap map[string]any
	if err := yaml.Unmarshal([]byte(snippet), &snippetMap); err == nil {
		if runsOnValue, ok := snippetMap["runs-on"]; ok {
			if rendered := renderRunsOnSnippet(runsOnValue); rendered != "" {
				return rendered
			}
		}
	} else {
		runsOnSnippetLog.Printf("Could not parse runs-on snippet as YAML map, using raw form: %v", err)
	}
	return ensureRunsOnContinuationIndent(snippet)
}

func ensureRunsOnContinuationIndent(snippet string) string {
	lines := strings.Split(snippet, "\n")
	if len(lines) <= 1 {
		return snippet
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(lines[i], " ") {
			continue
		}
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}
