//go:build !integration

package agentdrain_test

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/agentdrain"
)

// TestSpec_PublicAPI_DefaultConfig validates Public API / Functions: production
// defaults include masking rules and excluded fields and construct a usable miner.
// SPEC_AMBIGUITY: The README does not specify exact default configuration values.
func TestSpec_PublicAPI_DefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()

	assert.NotEmpty(t, cfg.MaskRules, "production defaults should include masking rules")
	assert.NotEmpty(t, cfg.ExcludeFields, "production defaults should include excluded fields")
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err, "the documented default configuration should construct a miner")
	require.NotNil(t, miner, "the default configuration should produce a usable miner")
}

// TestSpec_PublicAPI_NewMiner validates that NewMiner creates a usable miner.
func TestSpec_PublicAPI_NewMiner(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err, "NewMiner with valid config should not error")
	assert.NotNil(t, miner, "NewMiner should return non-nil miner")
}

// TestSpec_PublicAPI_Miner_TrainEvent validates that TrainEvent processes known-good events.
func TestSpec_PublicAPI_Miner_TrainEvent(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{
		Stage:  "plan",
		Fields: map[string]string{"action": "start", "step": "1"},
	}
	result, err := miner.TrainEvent(evt)
	require.NoError(t, err, "TrainEvent should not error on valid event")
	assert.NotNil(t, result, "TrainEvent should return a MatchResult")
	assert.Positive(t, result.ClusterID, "ClusterID should be positive after training")
}

// TestSpec_PublicAPI_Miner_AnalyzeEvent validates that AnalyzeEvent produces a match result and anomaly report.
func TestSpec_PublicAPI_Miner_AnalyzeEvent(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{
		Stage:  "tool_call",
		Fields: map[string]string{"tool": "bash", "status": "ok"},
	}
	result, report, err := miner.AnalyzeEvent(evt)
	require.NoError(t, err, "AnalyzeEvent should not error on valid event")
	assert.NotNil(t, result, "AnalyzeEvent should return a MatchResult")
	assert.NotNil(t, report, "AnalyzeEvent should return an AnomalyReport")
}

// TestSpec_PublicAPI_Miner_Clusters validates that Clusters and ClusterCount report trained state.
func TestSpec_PublicAPI_Miner_Clusters(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)

	assert.Empty(t, miner.Clusters(), "Clusters should be empty before training")

	evt := agentdrain.AgentEvent{Stage: "finish", Fields: map[string]string{"result": "success"}}
	_, err = miner.TrainEvent(evt)
	require.NoError(t, err)

	assert.Len(t, miner.Clusters(), 1, "Clusters should have one entry after training one unique event")
}

// TestSpec_PublicAPI_Miner_Persistence validates SaveJSON/LoadJSON round-trip preserves cluster state.
func TestSpec_PublicAPI_Miner_Persistence(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{Stage: "plan", Fields: map[string]string{"step": "evaluate"}}
	_, err = miner.TrainEvent(evt)
	require.NoError(t, err)

	data, err := miner.SaveJSON()
	require.NoError(t, err, "SaveJSON should not error")
	assert.NotEmpty(t, data, "SaveJSON should return non-empty JSON data")

	restored, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)
	err = restored.LoadJSON(data)
	require.NoError(t, err, "LoadJSON should not error with valid data")
	assert.Equal(t, miner.Clusters(), restored.Clusters(), "loading saved state should preserve cluster contents")

	replacement := agentdrain.AgentEvent{Stage: "finish", Fields: map[string]string{"result": "success"}}
	_, err = restored.TrainEvent(replacement)
	require.NoError(t, err, "the restored miner should accept further training")
	require.NoError(t, restored.LoadJSON(data), "loading a snapshot should replace existing miner state")
	assert.Equal(t, miner.Clusters(), restored.Clusters(), "loading should replace subsequent training with saved cluster state")
}

// TestSpec_PublicAPI_NewCoordinator validates that NewCoordinator creates a coordinator for given stages.
func TestSpec_PublicAPI_NewCoordinator(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	stages := []string{"plan", "tool_call", "finish"}
	coord, err := agentdrain.NewCoordinator(cfg, stages)
	require.NoError(t, err, "NewCoordinator with valid config and stages should not error")
	assert.NotNil(t, coord, "NewCoordinator should return non-nil Coordinator")
}

// TestSpec_PublicAPI_Coordinator_AllClusters validates that AllClusters returns a map keyed by stage.
func TestSpec_PublicAPI_Coordinator_AllClusters(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	stages := []string{"plan", "tool_call", "finish"}
	coord, err := agentdrain.NewCoordinator(cfg, stages)
	require.NoError(t, err)

	all := coord.AllClusters()
	assert.NotNil(t, all, "AllClusters should return non-nil map")
	for _, stage := range stages {
		_, exists := all[stage]
		assert.True(t, exists, "AllClusters should contain an entry for stage %q", stage)
	}
}

// TestSpec_PublicAPI_Coordinator_Snapshots validates SaveSnapshots/LoadSnapshots round-trip.
func TestSpec_PublicAPI_Coordinator_Snapshots(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	stages := []string{"plan", "finish"}
	coord, err := agentdrain.NewCoordinator(cfg, stages)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{Stage: "plan", Fields: map[string]string{"action": "start"}}
	_, err = coord.TrainEvent(evt)
	require.NoError(t, err)

	snapshots, err := coord.SaveSnapshots()
	require.NoError(t, err, "SaveSnapshots should not error")
	assert.NotEmpty(t, snapshots, "SaveSnapshots should return non-empty map")

	coord2, err := agentdrain.NewCoordinator(cfg, []string{"tool_call"})
	require.NoError(t, err)
	err = coord2.LoadSnapshots(snapshots)
	require.NoError(t, err, "LoadSnapshots should not error with valid snapshots")
	for _, stage := range stages {
		assert.Equal(t, coord.AllClusters()[stage], coord2.AllClusters()[stage],
			"LoadSnapshots should restore cluster contents for previously unknown stage %q", stage)
	}
}

// TestSpec_PublicAPI_Utility_Tokenize validates that Tokenize splits on whitespace boundaries.
func TestSpec_PublicAPI_Utility_Tokenize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		expected []string
	}{
		{
			name:     "splits single-space delimited tokens",
			line:     "a b c",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "handles multiple whitespace",
			line:     "foo  bar",
			expected: []string{"foo", "bar"},
		},
		{
			name:     "returns empty for empty string",
			line:     "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := agentdrain.Tokenize(tt.line)
			assert.Equal(t, tt.expected, result, "Tokenize(%q) mismatch", tt.line)
		})
	}
}

// TestSpec_PublicAPI_Utility_FlattenEvent validates that FlattenEvent excludes specified fields
// and produces deterministic (sorted) output.
func TestSpec_PublicAPI_Utility_FlattenEvent(t *testing.T) {
	t.Parallel()
	t.Run("excludes listed fields", func(t *testing.T) {
		t.Parallel()
		evt := agentdrain.AgentEvent{
			Stage: "plan",
			Fields: map[string]string{
				"session_id": "abc-123",
				"action":     "start",
			},
		}
		result := agentdrain.FlattenEvent(evt, []string{"session_id"})
		assert.NotContains(t, result, "session_id", "excluded field should not appear in flattened output")
		assert.Contains(t, result, "action", "non-excluded field should appear in flattened output")
	})

	t.Run("produces deterministic output for same input", func(t *testing.T) {
		t.Parallel()
		evt := agentdrain.AgentEvent{
			Stage: "tool_call",
			Fields: map[string]string{
				"tool":   "bash",
				"status": "ok",
				"step":   "3",
			},
		}
		first := agentdrain.FlattenEvent(evt, nil)
		second := agentdrain.FlattenEvent(evt, nil)
		assert.Equal(t, first, second, "FlattenEvent should be deterministic for same input")
	})
}

// TestSpec_PublicAPI_Utility_StageSequence validates space-separated stage extraction.
// Spec: returns "a space-separated string of the stages from a slice of events"
func TestSpec_PublicAPI_Utility_StageSequence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		events   []agentdrain.AgentEvent
		expected string
	}{
		{
			name: "returns space-separated stage names",
			events: []agentdrain.AgentEvent{
				{Stage: "plan"},
				{Stage: "tool_call"},
				{Stage: "tool_result"},
				{Stage: "finish"},
			},
			expected: "plan tool_call tool_result finish",
		},
		{
			name:     "returns empty string for empty events",
			events:   []agentdrain.AgentEvent{},
			expected: "",
		},
		{
			name:     "returns single stage for single event",
			events:   []agentdrain.AgentEvent{{Stage: "plan"}},
			expected: "plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := agentdrain.StageSequence(tt.events)
			assert.Equal(t, tt.expected, result, "StageSequence mismatch for %q", tt.name)
		})
	}
}

// TestSpec_PublicAPI_NewMasker validates that NewMasker creates a masker and Mask applies substitutions.
func TestSpec_PublicAPI_NewMasker(t *testing.T) {
	t.Parallel()
	rules := []agentdrain.MaskRule{
		{
			Name:        "number_test",
			Pattern:     `\d+`,
			Replacement: "<NUM>",
		},
	}
	masker, err := agentdrain.NewMasker(rules)
	require.NoError(t, err, "NewMasker should not error with valid rules")
	assert.NotNil(t, masker, "NewMasker should return non-nil Masker")

	result := masker.Mask("step 42 completed")
	assert.Contains(t, result, "<NUM>", "Mask should apply substitution rules")
	assert.NotContains(t, result, "42", "Mask should replace matched content")
}

// TestSpec_PublicAPI_NewAnomalyDetector validates AnomalyDetector construction.
func TestSpec_PublicAPI_NewAnomalyDetector(t *testing.T) {
	t.Parallel()
	detector, err := agentdrain.NewAnomalyDetector(0.4, 2)
	require.NoError(t, err, "NewAnomalyDetector should not error with valid thresholds")
	assert.NotNil(t, detector, "NewAnomalyDetector should return non-nil detector")
}

// TestSpec_Types_AgentEvent validates the documented AgentEvent type structure.
// Spec: Stage string, Fields map[string]string
func TestSpec_Types_AgentEvent(t *testing.T) {
	t.Parallel()
	evt := agentdrain.AgentEvent{
		Stage:  "plan",
		Fields: map[string]string{"key": "value"},
	}
	assert.Equal(t, "plan", evt.Stage)
	assert.Equal(t, "value", evt.Fields["key"])
}

// TestSpec_Types_MaskRule validates the documented MaskRule type structure.
// Spec: Name, Pattern, Replacement fields
func TestSpec_Types_MaskRule(t *testing.T) {
	t.Parallel()
	rule := agentdrain.MaskRule{
		Name:        "test-rule",
		Pattern:     `\d+`,
		Replacement: "<NUM>",
	}
	assert.Equal(t, "test-rule", rule.Name)
	assert.Equal(t, `\d+`, rule.Pattern)
	assert.Equal(t, "<NUM>", rule.Replacement)
}

// TestSpec_DesignDecision_CoordinatorRouting validates that events from different stages
// are routed to separate miners so templates do not interfere.
// Spec: "The Coordinator routes each AgentEvent to its stage-specific Miner"
func TestSpec_DesignDecision_CoordinatorRouting(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	stages := []string{"plan", "tool_call"}
	coord, err := agentdrain.NewCoordinator(cfg, stages)
	require.NoError(t, err)

	planEvt := agentdrain.AgentEvent{Stage: "plan", Fields: map[string]string{"x": "y"}}
	toolEvt := agentdrain.AgentEvent{Stage: "tool_call", Fields: map[string]string{"a": "b"}}

	_, err = coord.TrainEvent(planEvt)
	require.NoError(t, err)
	_, err = coord.TrainEvent(toolEvt)
	require.NoError(t, err)

	all := coord.AllClusters()
	assert.Len(t, all["plan"], 1, "plan stage should have one cluster")
	assert.Len(t, all["tool_call"], 1, "tool_call stage should have one cluster")
}

// TestSpec_PublicAPI_Coordinator_AnalyzeEvent validates Coordinator.AnalyzeEvent routes events
// to stage-specific miners and returns MatchResult and AnomalyReport.
func TestSpec_PublicAPI_Coordinator_AnalyzeEvent(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	stages := []string{"plan", "tool_call", "finish"}
	coord, err := agentdrain.NewCoordinator(cfg, stages)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{
		Stage:  "plan",
		Fields: map[string]string{"action": "evaluate", "step": "1"},
	}
	result, report, err := coord.AnalyzeEvent(evt)
	require.NoError(t, err, "AnalyzeEvent should not error for a registered stage")
	assert.NotNil(t, result, "AnalyzeEvent should return a MatchResult")
	assert.NotNil(t, report, "AnalyzeEvent should return an AnomalyReport")
	assert.Equal(t, "plan", result.Stage, "MatchResult.Stage should match the event stage")
}

// TestSpec_PublicAPI_Coordinator_WeightsPersistence validates SaveWeightsJSON/LoadWeightsJSON
// round-trip as documented in the specification.
func TestSpec_PublicAPI_Coordinator_WeightsPersistence(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	stages := []string{"plan", "finish"}
	coord, err := agentdrain.NewCoordinator(cfg, stages)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{Stage: "plan", Fields: map[string]string{"step": "start"}}
	_, err = coord.TrainEvent(evt)
	require.NoError(t, err)

	data, err := coord.SaveWeightsJSON()
	require.NoError(t, err, "SaveWeightsJSON should not error")
	assert.NotEmpty(t, data, "SaveWeightsJSON should return non-empty JSON")

	coord2, err := agentdrain.NewCoordinator(cfg, stages)
	require.NoError(t, err)
	err = coord2.LoadWeightsJSON(data)
	require.NoError(t, err, "LoadWeightsJSON should not error with valid data")
	assert.Equal(t, coord.AllClusters(), coord2.AllClusters(), "combined weights should preserve all stages and cluster contents")
}

// TestSpec_PublicAPI_MaskerRuleOrder validates Public API / Functions: all
// configured regex substitutions run in order.
func TestSpec_PublicAPI_MaskerRuleOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		rules    []agentdrain.MaskRule
		expected string
	}{
		{
			name: "later rule consumes the earlier replacement",
			rules: []agentdrain.MaskRule{
				{Name: "number", Pattern: `\d+`, Replacement: "<NUM>"},
				{Name: "label", Pattern: "<NUM>", Replacement: "<VALUE>"},
			},
			expected: "step <VALUE> completed",
		},
		{
			name: "reversed rules produce a different result",
			rules: []agentdrain.MaskRule{
				{Name: "label", Pattern: "<NUM>", Replacement: "<VALUE>"},
				{Name: "number", Pattern: `\d+`, Replacement: "<NUM>"},
			},
			expected: "step <NUM> completed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			masker, err := agentdrain.NewMasker(tt.rules)
			require.NoError(t, err, "documented regex substitution rules should compile")
			assert.Equal(t, tt.expected, masker.Mask("step 42 completed"), "masking should apply rules in their configured order")
		})
	}
}

// TestSpec_DesignDecision_AnomalyFlags validates Design Decisions: a new
// template is never double-counted as a low-similarity match.
func TestSpec_DesignDecision_AnomalyFlags(t *testing.T) {
	t.Parallel()
	miner, err := agentdrain.NewMiner(agentdrain.DefaultConfig())
	require.NoError(t, err, "default configuration should construct a miner")
	event := agentdrain.AgentEvent{Stage: "plan", Fields: map[string]string{"action": "start"}}
	_, report, err := miner.AnalyzeEvent(event)
	require.NoError(t, err, "analysis should train and score the documented event")
	require.NotNil(t, report, "analysis should return an anomaly report")
	assert.True(t, report.IsNewTemplate, "the first event in an empty miner should create a new template")
	assert.False(t, report.LowSimilarity, "a new template must not also be flagged as low similarity")
	clusters := miner.Clusters()
	require.Len(t, clusters, 1, "analysis should train on the same event")
	assert.Equal(t, event.Stage, clusters[0].Stage, "training should propagate the event stage into its cluster")
}

// TestSpec_PublicAPI_AnomalyDetector_Analyze validates that AnomalyDetector.Analyze
// returns an AnomalyReport with AnomalyScore in the documented [0, 1] range.
func TestSpec_PublicAPI_AnomalyDetector_Analyze(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{Stage: "plan", Fields: map[string]string{"action": "start"}}
	_, report, err := miner.AnalyzeEvent(evt)
	require.NoError(t, err)

	assert.NotNil(t, report, "AnomalyReport should not be nil")
	assert.GreaterOrEqual(t, report.AnomalyScore, 0.0, "AnomalyScore should be >= 0 (documented range [0,1])")
	assert.LessOrEqual(t, report.AnomalyScore, 1.0, "AnomalyScore should be <= 1 (documented range [0,1])")
}

// TestSpec_Types_Cluster validates the documented Cluster type structure.
// Spec: ID (unique identifier), Template (tokenized template), Size (count), Stage (pipeline stage).
func TestSpec_Types_Cluster(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{Stage: "plan", Fields: map[string]string{"step": "1"}}
	_, err = miner.TrainEvent(evt)
	require.NoError(t, err)

	clusters := miner.Clusters()
	require.Len(t, clusters, 1, "should have one cluster after training one unique event")
	c := clusters[0]
	assert.Positive(t, c.ID, "Cluster.ID should be a positive unique identifier")
	assert.NotEmpty(t, c.Template, "Cluster.Template should be a non-empty tokenized template")
	assert.Equal(t, 1, c.Size, "Cluster.Size should be 1 after one training event")
	assert.Equal(t, "plan", c.Stage, "Cluster.Stage should match the event stage")
}

// TestSpec_Types_MatchResult validates the documented MatchResult type structure.
// Spec: ClusterID, Template (space-joined), Params, Similarity in [0,1], Stage.
func TestSpec_Types_MatchResult(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{Stage: "tool_call", Fields: map[string]string{"tool": "bash"}}
	result, err := miner.TrainEvent(evt)
	require.NoError(t, err)

	assert.Positive(t, result.ClusterID, "MatchResult.ClusterID should be positive")
	assert.NotEmpty(t, result.Template, "MatchResult.Template should be a space-joined template string")
	assert.GreaterOrEqual(t, result.Similarity, 0.0, "MatchResult.Similarity should be >= 0")
	assert.LessOrEqual(t, result.Similarity, 1.0, "MatchResult.Similarity should be <= 1")
	assert.Equal(t, "tool_call", result.Stage, "MatchResult.Stage should match event stage")
}

// TestSpec_Types_AnomalyReport validates the documented AnomalyReport type structure.
// Spec: IsNewTemplate, LowSimilarity, RareCluster, AnomalyScore in [0,1], Reason.
func TestSpec_Types_AnomalyReport(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{Stage: "finish", Fields: map[string]string{"result": "success"}}
	_, report, err := miner.AnalyzeEvent(evt)
	require.NoError(t, err)

	// Verify all documented fields are accessible
	_ = report.IsNewTemplate
	_ = report.LowSimilarity
	_ = report.RareCluster
	_ = report.Reason
	assert.GreaterOrEqual(t, report.AnomalyScore, 0.0, "AnomalyReport.AnomalyScore should be in documented range [0, 1]")
	assert.LessOrEqual(t, report.AnomalyScore, 1.0, "AnomalyReport.AnomalyScore should be in documented range [0, 1]")
}

// TestSpec_PublicAPI_Coordinator_TrainEvent validates Coordinator.TrainEvent routes known-good
// events to the correct stage miner and returns a MatchResult.
// Spec: "Training phase — call for known-good events" on Coordinator
func TestSpec_PublicAPI_Coordinator_TrainEvent(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	stages := []string{"plan", "tool_call", "finish"}
	coord, err := agentdrain.NewCoordinator(cfg, stages)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{
		Stage:  "plan",
		Fields: map[string]string{"action": "start", "step": "1"},
	}
	result, err := coord.TrainEvent(evt)
	require.NoError(t, err, "Coordinator.TrainEvent should not error for valid event in registered stage")
	assert.NotNil(t, result, "Coordinator.TrainEvent should return a non-nil MatchResult")
	assert.Equal(t, "plan", result.Stage, "MatchResult.Stage should match the trained event stage")
}

// TestSpec_PublicAPI_Coordinator_LoadDefaultWeights validates that LoadDefaultWeights
// does not error on a freshly constructed coordinator.
// Spec: "Call coord.LoadDefaultWeights() to initialize the coordinator with pre-trained cluster weights"
func TestSpec_PublicAPI_Coordinator_LoadDefaultWeights(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	stages := []string{"plan", "tool_call", "finish"}
	coord, err := agentdrain.NewCoordinator(cfg, stages)
	require.NoError(t, err)

	err = coord.LoadDefaultWeights()
	require.NoError(t, err, "LoadDefaultWeights should not error (no-op when empty, loads otherwise)")
}

// TestSpec_Types_Snapshot validates the documented Snapshot/SnapshotCluster type structures.
// Spec: Snapshot{Config, Clusters []SnapshotCluster, NextID}, SnapshotCluster{ID, Template, Size, Stage}.
func TestSpec_Types_Snapshot(t *testing.T) {
	t.Parallel()
	cfg := agentdrain.DefaultConfig()
	miner, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)

	evt := agentdrain.AgentEvent{Stage: "plan", Fields: map[string]string{"step": "init"}}
	_, err = miner.TrainEvent(evt)
	require.NoError(t, err)

	data, err := miner.SaveJSON()
	require.NoError(t, err, "SaveJSON should succeed to produce snapshot bytes")
	assert.NotEmpty(t, data, "Snapshot data should be non-empty JSON bytes")

	restored, err := agentdrain.NewMiner(cfg)
	require.NoError(t, err)
	err = restored.LoadJSON(data)
	require.NoError(t, err, "LoadJSON should restore miner from snapshot bytes")
	assert.Len(t, restored.Clusters(), 1, "restored miner should have the same cluster count as the original")
}

// TestSpec_Constants_AnomalyWeights validates the documented anomaly scoring constants.
func TestSpec_Constants_AnomalyWeights(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, 1.0, agentdrain.AnomalyWeightNew, 0, "new-template anomaly weight should match the specification")
	assert.InDelta(t, 0.7, agentdrain.AnomalyWeightLow, 0, "low-similarity anomaly weight should match the specification")
	assert.InDelta(t, 0.3, agentdrain.AnomalyWeightRare, 0, "rare-cluster anomaly weight should match the specification")
	assert.InDelta(t, 2.0, agentdrain.AnomalyMaxScore, 0, "maximum anomaly score should match the specification")
}

// TestSpec_PublicAPI_FlattenEvent_Ordering validates the documented stage-first,
// alphabetically sorted key=value representation.
func TestSpec_PublicAPI_FlattenEvent_Ordering(t *testing.T) {
	t.Parallel()
	event := agentdrain.AgentEvent{
		Stage: "tool_call",
		Fields: map[string]string{
			"zeta":       "last",
			"action":     "start",
			"session_id": "excluded",
		},
	}
	assert.Equal(t, "stage=tool_call action=start zeta=last", agentdrain.FlattenEvent(event, []string{"session_id"}),
		"FlattenEvent should emit a non-empty stage first, sort keys, and omit excluded fields")
}

// TestSpec_ThreadSafety_Miner validates the documented guarantee that training
// and read-only snapshot operations are safe to use concurrently.
func TestSpec_ThreadSafety_Miner(t *testing.T) {
	t.Parallel()
	miner, err := agentdrain.NewMiner(agentdrain.DefaultConfig())
	require.NoError(t, err, "NewMiner should construct a miner for the concurrency test")

	const workers = 12
	errCh := make(chan error, workers*2)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(2)
		go func(index int) {
			defer wg.Done()
			_, trainErr := miner.TrainEvent(agentdrain.AgentEvent{
				Stage:  "plan",
				Fields: map[string]string{"worker": strconv.Itoa(index)},
			})
			errCh <- trainErr
		}(i)
		go func() {
			defer wg.Done()
			_ = miner.Clusters()
			_, saveErr := miner.SaveJSON()
			errCh <- saveErr
		}()
	}
	wg.Wait()
	close(errCh)
	for operationErr := range errCh {
		require.NoError(t, operationErr, "documented concurrent Miner operations should succeed")
	}
}

// TestSpec_ThreadSafety_Coordinator validates the documented guarantee that
// stage routing and stage-map snapshot operations are safe to use concurrently.
func TestSpec_ThreadSafety_Coordinator(t *testing.T) {
	t.Parallel()
	coordinator, err := agentdrain.NewCoordinator(agentdrain.DefaultConfig(), []string{"plan", "tool_call"})
	require.NoError(t, err, "NewCoordinator should construct a coordinator for the concurrency test")

	const workers = 12
	errCh := make(chan error, workers*2)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(2)
		go func(index int) {
			defer wg.Done()
			stage := "plan"
			if index%2 == 1 {
				stage = "tool_call"
			}
			_, trainErr := coordinator.TrainEvent(agentdrain.AgentEvent{
				Stage:  stage,
				Fields: map[string]string{"worker": strconv.Itoa(index)},
			})
			errCh <- trainErr
		}(i)
		go func() {
			defer wg.Done()
			_ = coordinator.AllClusters()
			_, saveErr := coordinator.SaveSnapshots()
			errCh <- saveErr
		}()
	}
	wg.Wait()
	close(errCh)
	for operationErr := range errCh {
		require.NoError(t, operationErr, "documented concurrent Coordinator operations should succeed")
	}
}
