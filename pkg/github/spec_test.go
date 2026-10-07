//go:build !integration

package github_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/github"
)

// TestSpec_* tests derive from pkg/github/README.md, not from implementation
// source. Each test function maps to a documented section of the package
// specification (Public API, Types, Constants, Design Notes, Usage Examples).

// TestSpec_DefaultObjectiveMapping_Values validates that the documented default
// objective mapping values match DefaultObjectiveMapping.
func TestSpec_DefaultObjectiveMapping_Values(t *testing.T) {
	t.Parallel()
	om := github.DefaultObjectiveMapping()

	// This assertion intentionally locks the full default shape so score changes
	// require an explicit spec update in one place.
	assert.Equal(t, map[string]int{
		"critical":        100,
		"p0":              100,
		"high-priority":   50,
		"copilot-opt":     50,
		"p1":              50,
		"security-fix":    75,
		"p2":              25,
		"medium-priority": 25,
		"performance":     30,
		"p3":              10,
		"low-priority":    10,
		"documentation":   5,
	}, om.LabelToValue)
}

// TestSpec_DefaultObjectiveMapping_ExcludesUnmappedLabels validates labels that
// are intentionally not part of the default mapping.
func TestSpec_DefaultObjectiveMapping_ExcludesUnmappedLabels(t *testing.T) {
	t.Parallel()
	om := github.DefaultObjectiveMapping()
	for _, label := range []string{
		"bug", "testing", "reliability", "workflow", "engine", "mcp", "enhancement", "dependencies",
	} {
		assert.NotContains(t, om.LabelToValue, label, "label %q should not be in the default mapping", label)
	}
}

// TestSpec_Constants_MultiLabelLogic validates the documented multi-label logic
// option constants and their string values.
func TestSpec_Constants_MultiLabelLogic(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "max", github.MultiLabelLogicMax,
		"MultiLabelLogicMax should be \"max\"")
	assert.Equal(t, "sum", github.MultiLabelLogicSum,
		"MultiLabelLogicSum should be \"sum\"")
	assert.Equal(t, "first", github.MultiLabelLogicFirst,
		"MultiLabelLogicFirst should be \"first\"")
}

// TestSpec_PublicAPI_ComputeObjectiveValue validates the documented behavior of
// ComputeObjectiveValue. Per the README it "calculates the numeric value for an
// issue based on its labels; returns 0 if no labels match or if the receiver is
// nil". Multi-label combination follows MultiLabelLogic ("max" default, "sum",
// "first").
//
// Tests construct explicit mappings so they validate documented behavior rather
// than the contents of the built-in default mapping.
func TestSpec_PublicAPI_ComputeObjectiveValue(t *testing.T) {
	t.Parallel()
	// Mapping is intentionally explicit for this test and independent of defaults.
	mapping := func(logic string, priorities ...string) *github.ObjectiveMapping {
		return &github.ObjectiveMapping{
			// Explicit values used by this test: bug=60, high-priority=35.
			LabelToValue: map[string]int{
				"bug":           60,
				"high-priority": 35,
				"documentation": 5,
			},
			MultiLabelLogic: logic,
			PriorityLabels:  priorities,
		}
	}

	t.Run("max logic returns highest matching value (documented default)", func(t *testing.T) {
		t.Parallel()
		// Explicit test mapping: max of bug=60, high-priority=35 -> 60.
		got := mapping(github.MultiLabelLogicMax).ComputeObjectiveValue([]string{"bug", "high-priority"})
		assert.Equal(t, 60, got, "max logic should return the highest matching value")
	})

	t.Run("empty MultiLabelLogic defaults to max", func(t *testing.T) {
		t.Parallel()
		got := mapping("").ComputeObjectiveValue([]string{"bug", "high-priority"})
		assert.Equal(t, 60, got, "empty MultiLabelLogic should behave as \"max\"")
	})

	t.Run("sum logic adds all matching values", func(t *testing.T) {
		t.Parallel()
		got := mapping(github.MultiLabelLogicSum).ComputeObjectiveValue([]string{"bug", "high-priority"})
		assert.Equal(t, 95, got, "sum logic should add matching values (60+35)")
	})

	t.Run("first logic uses the first prioritized match", func(t *testing.T) {
		t.Parallel()
		got := mapping(github.MultiLabelLogicFirst, "high-priority", "bug").
			ComputeObjectiveValue([]string{"bug", "high-priority"})
		assert.Equal(t, 60, got,
			"first logic should use issue-label order when a label is present in PriorityLabels")
	})

	t.Run("first logic falls back to first mapped label", func(t *testing.T) {
		t.Parallel()
		got := mapping(github.MultiLabelLogicFirst, "not-present").
			ComputeObjectiveValue([]string{"high-priority", "bug"})
		assert.Equal(t, 35, got,
			"first logic should fall back to the first mapped issue label when no priority matches")
	})

	t.Run("nil receiver returns 0", func(t *testing.T) {
		t.Parallel()
		var om *github.ObjectiveMapping
		assert.Equal(t, 0, om.ComputeObjectiveValue([]string{"bug"}),
			"nil receiver should return 0")
	})

	t.Run("no matching labels returns 0", func(t *testing.T) {
		t.Parallel()
		got := mapping(github.MultiLabelLogicMax).ComputeObjectiveValue([]string{"nonexistent"})
		assert.Equal(t, 0, got, "no matching labels should return 0")
	})

	t.Run("empty issue labels returns 0", func(t *testing.T) {
		t.Parallel()
		got := mapping(github.MultiLabelLogicMax).ComputeObjectiveValue([]string{})
		assert.Equal(t, 0, got, "empty issue labels should return 0")
	})

	t.Run("label matching is case-insensitive (design note)", func(t *testing.T) {
		t.Parallel()
		got := mapping(github.MultiLabelLogicMax).ComputeObjectiveValue([]string{"  BUG  "})
		assert.Equal(t, 60, got,
			"labels should be normalized with ToLower/TrimSpace before lookup")
	})
}

// TestSpec_PublicAPI_FilterObjectiveLabels validates that FilterObjectiveLabels
// returns the subset of issue labels that have defined objective values,
// preserving original order.
func TestSpec_PublicAPI_FilterObjectiveLabels(t *testing.T) {
	t.Parallel()
	om := &github.ObjectiveMapping{
		LabelToValue: map[string]int{
			"bug":           60,
			"high-priority": 35,
		},
	}

	t.Run("returns only labels with defined values", func(t *testing.T) {
		t.Parallel()
		got := om.FilterObjectiveLabels([]string{"bug", "good first issue"})
		assert.Equal(t, []string{"bug"}, got,
			"only labels with defined objective values should be returned")
	})

	t.Run("preserves original input order", func(t *testing.T) {
		t.Parallel()
		got := om.FilterObjectiveLabels([]string{"high-priority", "unknown", "bug"})
		assert.Equal(t, []string{"high-priority", "bug"}, got,
			"returned labels should preserve their original order")
	})

	t.Run("no matching labels returns empty", func(t *testing.T) {
		t.Parallel()
		got := om.FilterObjectiveLabels([]string{"unknown"})
		assert.Empty(t, got, "no matching labels should yield an empty result")
	})
}

// TestSpec_PublicAPI_HasObjectiveLabel validates that HasObjectiveLabel
// reports whether a label has a defined objective value.
func TestSpec_PublicAPI_HasObjectiveLabel(t *testing.T) {
	t.Parallel()
	om := &github.ObjectiveMapping{
		LabelToValue: map[string]int{"bug": 60},
	}

	assert.True(t, om.HasObjectiveLabel("bug"),
		"a defined label should report as existing")
	assert.True(t, om.HasObjectiveLabel("  BUG  "),
		"label lookup should be case-insensitive and trim surrounding space")
	assert.False(t, om.HasObjectiveLabel("unknown"),
		"an undefined label should report as not existing")
}

// TestSpec_ConfigPrecedence_EnvironmentJSON validates that the documented
// environment source takes precedence and accepts raw JSON.
func TestSpec_ConfigPrecedence_EnvironmentJSON(t *testing.T) {
	t.Setenv("OBJECTIVE_MAPPING_JSON", `{"label_to_value":{"from-env":42},"multi_label_logic":"sum","priority_labels":[]}`)

	om := github.LoadObjectiveMapping()
	require.NotNil(t, om, "a valid environment mapping should load")
	assert.Equal(t, 42, om.ComputeObjectiveValue([]string{"from-env"}),
		"LoadObjectiveMapping should use the raw environment JSON mapping")
	assert.Equal(t, github.MultiLabelLogicSum, om.MultiLabelLogic,
		"LoadObjectiveMapping should preserve environment-provided logic")
}

// TestSpec_PublicAPI_GetAllLabels validates that GetAllLabels returns all
// defined labels sorted alphabetically.
func TestSpec_PublicAPI_GetAllLabels(t *testing.T) {
	t.Parallel()
	om := &github.ObjectiveMapping{
		LabelToValue: map[string]int{
			"high-priority": 35,
			"bug":           60,
			"documentation": 5,
		},
	}

	got := om.GetAllLabels()
	assert.Equal(t, []string{"bug", "documentation", "high-priority"}, got,
		"GetAllLabels should return all labels sorted alphabetically")
}

// TestSpec_PublicAPI_MarshalJSON validates that MarshalJSON implements
// json.Marshaler and produces indented JSON output.
func TestSpec_PublicAPI_MarshalJSON(t *testing.T) {
	t.Parallel()
	om := &github.ObjectiveMapping{
		LabelToValue:    map[string]int{"bug": 60},
		MultiLabelLogic: github.MultiLabelLogicMax,
	}

	data, err := om.MarshalJSON()
	require.NoError(t, err, "MarshalJSON should not error for a valid mapping")

	// Indented output (json.MarshalIndent) contains newlines.
	assert.Contains(t, string(data), "\n",
		"MarshalJSON output should be indented (contain newlines)")

	// Output must be valid JSON.
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded),
		"MarshalJSON output should be valid JSON")
}

// TestSpec_PublicAPI_String validates the documented String() format:
// "ObjectiveMapping{labels: N, logic: X, priorities: M}".
func TestSpec_PublicAPI_String(t *testing.T) {
	t.Parallel()
	om := &github.ObjectiveMapping{
		LabelToValue:    map[string]int{"bug": 60, "documentation": 5},
		MultiLabelLogic: github.MultiLabelLogicSum,
		PriorityLabels:  []string{"bug"},
	}

	got := om.String()
	assert.Equal(t, "ObjectiveMapping{labels: 2, logic: sum, priorities: 1}", got,
		"String() should follow the documented format")
}

// TestSpec_Functions_DefaultObjectiveMapping validates that
// DefaultObjectiveMapping returns the built-in default mapping. The README
// documents its String() representation as
// "ObjectiveMapping{labels: 12, logic: max, priorities: 7}".
func TestSpec_Functions_DefaultObjectiveMapping(t *testing.T) {
	t.Parallel()
	om := github.DefaultObjectiveMapping()
	require.NotNil(t, om, "DefaultObjectiveMapping should return a non-nil mapping")

	assert.Equal(t, github.MultiLabelLogicMax, om.MultiLabelLogic,
		"the default mapping uses \"max\" logic per the README")
	assert.Equal(t, "ObjectiveMapping{labels: 12, logic: max, priorities: 7}", om.String(),
		"the documented default mapping summary should match")
}

// TestSpec_Functions_LoadObjectiveMapping validates that, absent any
// environment or config-file override, LoadObjectiveMapping falls
// back to the built-in defaults (precedence step 3 in the README).
//
// This test deliberately does not set OBJECTIVE_MAPPING_JSON; in the absence of
// a repository .github/objective-mapping.json it must return the defaults.
func TestSpec_ConfigPrecedence_DefaultFallback(t *testing.T) {
	t.Setenv("OBJECTIVE_MAPPING_JSON", "")

	om := github.LoadObjectiveMapping()
	require.NotNil(t, om, "LoadObjectiveMapping should never return nil")
	assert.Equal(t, github.MultiLabelLogicMax, om.MultiLabelLogic,
		"the default fallback mapping should use \"max\" logic")
}
