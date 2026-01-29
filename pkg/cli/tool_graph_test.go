//go:build !integration

package cli

import (
	"strings"
	"testing"
)

func TestToolGraph(t *testing.T) {
	// Test basic tool graph creation and Mermaid generation
	graph := NewToolGraph()

	// Test empty graph
	mermaid := graph.GenerateMermaidGraph()
	if mermaid == "" {
		t.Error("Expected non-empty Mermaid graph for empty tool graph")
	}

	// Test with a simple sequence
	sequence := []string{"bash_ls", "github_search_issues", "bash_cat"}
	graph.AddSequence(sequence)

	if len(graph.Tools) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(graph.Tools))
	}

	if len(graph.Transitions) != 2 {
		t.Errorf("Expected 2 transitions, got %d", len(graph.Transitions))
	}

	// Test Mermaid generation with actual data
	mermaid = graph.GenerateMermaidGraph()
	if mermaid == "" {
		t.Error("Expected non-empty Mermaid graph")
	}

	// Should contain mermaid syntax
	if !strings.Contains(mermaid, "```mermaid") {
		t.Error("Expected Mermaid graph to contain mermaid code block")
	}

	if !strings.Contains(mermaid, "stateDiagram-v2") {
		t.Error("Expected Mermaid graph to use stateDiagram-v2 syntax")
	}

	// Test summary generation
	summary := graph.GetSummary()
	if summary == "" {
		t.Error("Expected non-empty summary")
	}
}

func TestToolGraphMultipleSequences(t *testing.T) {
	graph := NewToolGraph()

	// Add multiple sequences
	seq1 := []string{"bash_ls", "github_search_issues"}
	seq2 := []string{"bash_ls", "bash_cat"}
	seq3 := []string{"github_search_issues", "bash_cat"}

	graph.AddSequence(seq1)
	graph.AddSequence(seq2)
	graph.AddSequence(seq3)

	// Should have 3 unique tools
	if len(graph.Tools) != 3 {
		t.Errorf("Expected 3 unique tools, got %d", len(graph.Tools))
	}

	// Should have transitions with counts
	expectedTransitions := map[string]int{
		"bash_ls->github_search_issues":  1,
		"bash_ls->bash_cat":              1,
		"github_search_issues->bash_cat": 1,
	}

	for key, expectedCount := range expectedTransitions {
		if actualCount, exists := graph.Transitions[key]; !exists || actualCount != expectedCount {
			t.Errorf("Expected transition %s to have count %d, got %d", key, expectedCount, actualCount)
		}
	}
}

func TestToolGraphEmptySequences(t *testing.T) {
	graph := NewToolGraph()

	// Add empty sequence
	graph.AddSequence([]string{})

	// Should remain empty
	if len(graph.Tools) != 0 {
		t.Errorf("Expected 0 tools for empty sequence, got %d", len(graph.Tools))
	}

	if len(graph.Transitions) != 0 {
		t.Errorf("Expected 0 transitions for empty sequence, got %d", len(graph.Transitions))
	}
}
