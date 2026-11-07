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

func TestIsPreAgentCustomJob(t *testing.T) {
	for _, tt := range []struct {
		name  string
		job   string
		graph JobDependencyGraph
		want  bool
	}{
		{"direct", "metadata", JobDependencyGraph{"agent": {"metadata"}, "metadata": {}}, true},
		{"transitive", "metadata", JobDependencyGraph{"agent": {"activation"}, "activation": {"metadata"}, "metadata": {}}, true},
		{"parallel", "metadata", JobDependencyGraph{"agent": {}, "metadata": {}}, false},
		{"missing", "metadata", JobDependencyGraph{"agent": {}}, false},
		{"agent", "agent", JobDependencyGraph{"agent": {}}, false},
		{"compiler-owned pre-agent", "activation", JobDependencyGraph{"agent": {"activation"}, "activation": {}}, false},
		{"cycle through agent", "metadata", JobDependencyGraph{"agent": {"metadata"}, "metadata": {"agent"}}, false},
		{"detection dependency", "metadata", JobDependencyGraph{"agent": {"metadata"}, "metadata": {"detection"}}, false},
		{"transitive detection", "metadata", JobDependencyGraph{"agent": {"metadata"}, "metadata": {"relay"}, "relay": {"detection"}}, false},
		{"mixed transitive taint", "metadata", JobDependencyGraph{"agent": {"metadata", "clean"}, "metadata": {"clean", "relay"}, "relay": {"safe_outputs"}, "safe_outputs": {"agent"}, "clean": {}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.graph.IsPreAgentCustomJob(tt.job))
		})
	}
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
