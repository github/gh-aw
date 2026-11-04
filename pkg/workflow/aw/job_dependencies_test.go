package aw

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthoredJobDependencies(t *testing.T) {
	graph := AuthoredJobDependencies(map[string]any{
		"prepare":   map[string]any{"needs": "activation"},
		"scan":      map[string]any{"needs": []any{"agent", "detection"}},
		"report":    map[string]any{"needs": []string{"scan"}},
		"publish":   map[string]any{"needs": "safe_outputs"},
		"loop":      map[string]any{"needs": "loop"},
		"malformed": map[string]any{"needs": []any{123}},
		"invalid":   false,
	})
	assert.True(t, graph.DependsOn("report", "agent"))
	assert.True(t, graph.DependsOn("report", "detection"))
	assert.True(t, graph.DependsOn("publish", "agent"))
	assert.False(t, graph.DependsOn("prepare", "agent"))
	assert.False(t, graph.DependsOn("loop", "agent"))
	assert.False(t, graph.DependsOn("malformed", "agent"))
	assert.False(t, graph.DependsOn("invalid", "agent"))
	assert.False(t, graph.DependsOn("missing", "agent"))
}

func TestAuthoredJobDependenciesPostAgentMaintenance(t *testing.T) {
	for _, job := range []string{
		"push_repo_memory",
		"update_cache_memory",
		"update_drive_memory",
		"push_ledger_changes",
		"push_evals_state",
	} {
		t.Run(job, func(t *testing.T) {
			graph := AuthoredJobDependencies(map[string]any{
				"publish": map[string]any{"needs": []any{job}},
				"report":  map[string]any{"needs": "publish"},
			})
			assert.True(t, graph.DependsOn("publish", "agent"))
			assert.True(t, graph.DependsOn("report", "agent"))

			authoredGraph := AuthoredJobDependencies(map[string]any{
				job:       map[string]any{"needs": "activation"},
				"publish": map[string]any{"needs": []any{job}},
			})
			assert.False(t, authoredGraph.DependsOn(job, "agent"))
			assert.False(t, authoredGraph.DependsOn("publish", "agent"))
		})
	}
}

func TestAuthoredJobDependenciesExperimentPersistence(t *testing.T) {
	graph := AuthoredJobDependencies(map[string]any{
		"publish": map[string]any{"needs": []any{"push_experiments_state"}},
	})
	assert.True(t, graph.DependsOn("publish", "activation"))
	assert.False(t, graph.DependsOn("publish", "agent"))
}
