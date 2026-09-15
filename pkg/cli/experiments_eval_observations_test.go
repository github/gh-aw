//go:build !integration

package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveEvalMetricReferences(t *testing.T) {
	t.Parallel()
	evals := &workflow.EvalsConfig{Questions: []workflow.EvalDefinition{
		{ID: "quality", Question: "Is the output high quality?"},
	}}

	t.Run("normalizes canonical and dotted forms", func(t *testing.T) {
		refs, err := resolveEvalMetricReferences(map[string]*workflow.ExperimentConfig{
			"canonical": {Metric: "eval:quality"},
			"dotted":    {Metric: "evals.quality.value"},
		}, evals)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"canonical": "quality", "dotted": "quality"}, refs)
	})

	t.Run("rejects missing eval", func(t *testing.T) {
		_, err := resolveEvalMetricReferences(map[string]*workflow.ExperimentConfig{
			"test": {Metric: "eval:missing"},
		}, evals)
		require.EqualError(t, err, `experiments.test.metric: references unknown eval "missing"`)
	})

	t.Run("rejects reference when no evals declared", func(t *testing.T) {
		_, err := resolveEvalMetricReferences(map[string]*workflow.ExperimentConfig{
			"test": {Metric: "eval:quality"},
		}, nil)
		require.EqualError(t, err, `experiments.test.metric: references eval "quality" but no evals are declared`)
	})

	t.Run("rejects malformed reference", func(t *testing.T) {
		_, err := resolveEvalMetricReferences(map[string]*workflow.ExperimentConfig{
			"test": {Metric: "eval:"},
		}, evals)
		require.EqualError(t, err, "experiments.test.metric: expected eval reference format eval:<question_id> or evals.<question_id>")
	})

	t.Run("ignores non-eval metrics", func(t *testing.T) {
		refs, err := resolveEvalMetricReferences(map[string]*workflow.ExperimentConfig{
			"test": {Metric: "aic"},
		}, evals)
		require.NoError(t, err)
		assert.Empty(t, refs)
	})
}

func TestBuildEvalMetricObservationSetsJoinsAssignments(t *testing.T) {
	t.Parallel()
	experiments := []ExperimentVariantStats{{
		Name:     "prompt",
		Variants: map[string]int{"control": 3, "candidate": 3},
		Total:    6,
	}}
	runs := []ExperimentRunRecord{
		{RunID: "1", Assignments: map[string]string{"prompt": "control"}},
		{RunID: "2", Assignments: map[string]string{"prompt": "candidate"}},
		{RunID: "3", Assignments: map[string]string{"prompt": "candidate"}},
		{RunID: "2", Assignments: map[string]string{"prompt": "candidate"}},
		{RunID: "4", Assignments: map[string]string{"prompt": "control"}},
	}
	evalRecords := []evalResultRecord{
		{ID: "quality", Answer: "YES", RunID: "1"},
		{ID: "quality", Answer: "NO", RunID: "2"},
		{ID: "quality", Answer: "MAYBE", RunID: "4"},
	}

	sets := buildEvalMetricObservationSets(
		experiments,
		runs,
		map[string]string{"prompt": "quality"},
		evalRecords,
	)
	set := sets["prompt"]
	require.NotNil(t, set)
	require.Len(t, set.ByVariant["control"], 1)
	assert.Equal(t, "1", set.ByVariant["control"][0].RunID)
	assert.InDelta(t, 1.0, set.ByVariant["control"][0].Value, 0.0001)
	assert.True(t, set.ByVariant["control"][0].Binary)
	require.Len(t, set.ByVariant["candidate"], 1)
	assert.Equal(t, "2", set.ByVariant["candidate"][0].RunID)
	assert.InDelta(t, 0.0, set.ByVariant["candidate"][0].Value, 0.0001)

	controlExclusions := exclusionsByReason(set.Exclusions["control"])
	assert.Equal(t, 1, controlExclusions[exclusionInvalidValue].Count)
	assert.Equal(t, []string{"4"}, controlExclusions[exclusionInvalidValue].RunIDs)

	candidateExclusions := exclusionsByReason(set.Exclusions["candidate"])
	assert.Equal(t, 1, candidateExclusions[exclusionEvalRecordUnavailable].Count)
	assert.Equal(t, []string{"3"}, candidateExclusions[exclusionEvalRecordUnavailable].RunIDs)
	assert.Equal(t, 1, candidateExclusions[exclusionDuplicateAssignment].Count)
	assert.Equal(t, []string{"2"}, candidateExclusions[exclusionDuplicateAssignment].RunIDs)
}

func TestBuildEvalMetricObservationSetsMissingRecord(t *testing.T) {
	t.Parallel()
	experiments := []ExperimentVariantStats{{
		Name:     "prompt",
		Variants: map[string]int{"control": 1},
		Total:    1,
	}}
	runs := []ExperimentRunRecord{
		{RunID: "1", Assignments: map[string]string{"prompt": "control"}},
	}

	sets := buildEvalMetricObservationSets(
		experiments,
		runs,
		map[string]string{"prompt": "quality"},
		nil,
	)
	set := sets["prompt"]
	require.NotNil(t, set)
	assert.Empty(t, set.ByVariant["control"])
	controlExclusions := exclusionsByReason(set.Exclusions["control"])
	assert.Equal(t, 1, controlExclusions[exclusionEvalRecordUnavailable].Count)
}

func TestParseEvalAnswerValue(t *testing.T) {
	t.Parallel()
	value, ok := parseEvalAnswerValue("yes")
	assert.True(t, ok)
	assert.InDelta(t, 1.0, value, 0.0001)

	value, ok = parseEvalAnswerValue(" No ")
	assert.True(t, ok)
	assert.InDelta(t, 0.0, value, 0.0001)

	_, ok = parseEvalAnswerValue("UNKNOWN")
	assert.False(t, ok)
}
