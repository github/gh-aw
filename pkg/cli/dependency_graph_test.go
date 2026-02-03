//go:build !integration

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
)

func TestDependencyGraph_IsTopLevelWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	graph := NewDependencyGraph(workflowsDir)

	tests := []struct {
		name         string
		path         string
		wantTopLevel bool
	}{
		{
			name:         "top-level workflow",
			path:         filepath.Join(workflowsDir, "main.md"),
			wantTopLevel: true,
		},
		{
			name:         "shared workflow in subdirectory",
			path:         filepath.Join(workflowsDir, "shared", "helper.md"),
			wantTopLevel: false,
		},
		{
			name:         "nested shared workflow",
			path:         filepath.Join(workflowsDir, "shared", "mcp", "tool.md"),
			wantTopLevel: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := graph.isTopLevelWorkflow(tt.path)
			if got != tt.wantTopLevel {
				t.Errorf("isTopLevelWorkflow() = %v, want %v", got, tt.wantTopLevel)
			}
		})
	}
}

func TestDependencyGraph_BuildGraphAndGetAffectedWorkflows(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a shared workflow
	sharedWorkflow := filepath.Join(sharedDir, "helper.md")
	sharedContent := `---
description: Helper workflow
---
# Helper`
	if err := os.WriteFile(sharedWorkflow, []byte(sharedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a top-level workflow that imports the shared workflow
	topWorkflow1 := filepath.Join(workflowsDir, "main.md")
	topContent1 := `---
description: Main workflow
imports:
  - shared/helper.md
---
# Main`
	if err := os.WriteFile(topWorkflow1, []byte(topContent1), 0644); err != nil {
		t.Fatal(err)
	}

	// Create another top-level workflow that also imports the shared workflow
	topWorkflow2 := filepath.Join(workflowsDir, "secondary.md")
	topContent2 := `---
description: Secondary workflow
imports:
  - shared/helper.md
---
# Secondary`
	if err := os.WriteFile(topWorkflow2, []byte(topContent2), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a top-level workflow without imports
	topWorkflow3 := filepath.Join(workflowsDir, "standalone.md")
	topContent3 := `---
description: Standalone workflow
---
# Standalone`
	if err := os.WriteFile(topWorkflow3, []byte(topContent3), 0644); err != nil {
		t.Fatal(err)
	}

	// Build dependency graph
	graph := NewDependencyGraph(workflowsDir)
	compiler := workflow.NewCompiler()
	if err := graph.BuildGraph(compiler); err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}

	// Test 1: Modifying the shared workflow should affect both top-level workflows that import it
	t.Run("shared workflow modification affects importers", func(t *testing.T) {
		affected := graph.GetAffectedWorkflows(sharedWorkflow)

		// Should return both main.md and secondary.md
		expectedCount := 2
		if len(affected) != expectedCount {
			t.Errorf("GetAffectedWorkflows() returned %d workflows, want %d", len(affected), expectedCount)
		}

		// Check that both importers are in the list
		affectedMap := make(map[string]bool)
		for _, w := range affected {
			affectedMap[w] = true
		}

		if !affectedMap[topWorkflow1] {
			t.Errorf("GetAffectedWorkflows() should include %s", topWorkflow1)
		}
		if !affectedMap[topWorkflow2] {
			t.Errorf("GetAffectedWorkflows() should include %s", topWorkflow2)
		}
	})

	// Test 2: Modifying a top-level workflow should only affect itself
	t.Run("top-level workflow modification affects only itself", func(t *testing.T) {
		affected := graph.GetAffectedWorkflows(topWorkflow1)

		if len(affected) != 1 {
			t.Errorf("GetAffectedWorkflows() returned %d workflows, want 1", len(affected))
		}

		if len(affected) > 0 && affected[0] != topWorkflow1 {
			t.Errorf("GetAffectedWorkflows() = %v, want [%s]", affected, topWorkflow1)
		}
	})

	// Test 3: Modifying a standalone workflow should only affect itself
	t.Run("standalone workflow modification affects only itself", func(t *testing.T) {
		affected := graph.GetAffectedWorkflows(topWorkflow3)

		if len(affected) != 1 {
			t.Errorf("GetAffectedWorkflows() returned %d workflows, want 1", len(affected))
		}

		if len(affected) > 0 && affected[0] != topWorkflow3 {
			t.Errorf("GetAffectedWorkflows() = %v, want [%s]", affected, topWorkflow3)
		}
	})
}

func TestDependencyGraph_UpdateAndRemoveWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a shared workflow
	sharedWorkflow := filepath.Join(sharedDir, "helper.md")
	sharedContent := `---
description: Helper workflow
---
# Helper`
	if err := os.WriteFile(sharedWorkflow, []byte(sharedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a top-level workflow that imports the shared workflow
	topWorkflow := filepath.Join(workflowsDir, "main.md")
	topContent := `---
description: Main workflow
imports:
  - shared/helper.md
---
# Main`
	if err := os.WriteFile(topWorkflow, []byte(topContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Build dependency graph
	graph := NewDependencyGraph(workflowsDir)
	compiler := workflow.NewCompiler()
	if err := graph.BuildGraph(compiler); err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}

	// Test: Update workflow to remove import
	t.Run("update workflow removes old dependencies", func(t *testing.T) {
		// Update the workflow to remove the import
		newContent := `---
description: Main workflow
---
# Main (no imports)`
		if err := os.WriteFile(topWorkflow, []byte(newContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Update the workflow in the graph
		if err := graph.UpdateWorkflow(topWorkflow, compiler); err != nil {
			t.Fatalf("UpdateWorkflow() error = %v", err)
		}

		// Now modifying the shared workflow should not affect the top-level workflow
		affected := graph.GetAffectedWorkflows(sharedWorkflow)
		if len(affected) != 0 {
			t.Errorf("After update, GetAffectedWorkflows() returned %d workflows, want 0", len(affected))
		}
	})

	// Test: Remove workflow
	t.Run("remove workflow cleans up dependencies", func(t *testing.T) {
		// Remove the workflow
		graph.RemoveWorkflow(topWorkflow)

		// Check that the workflow is no longer in the graph
		if _, exists := graph.nodes[topWorkflow]; exists {
			t.Error("RemoveWorkflow() did not remove the node from the graph")
		}

		// Check that reverse imports are cleaned up
		if importers, exists := graph.reverseImports[sharedWorkflow]; exists && len(importers) > 0 {
			t.Errorf("RemoveWorkflow() did not clean up reverse imports, still has %d importers", len(importers))
		}
	})
}

func TestDependencyGraph_NestedImports(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a base shared workflow (leaf)
	baseWorkflow := filepath.Join(sharedDir, "base.md")
	baseContent := `---
description: Base workflow
---
# Base`
	if err := os.WriteFile(baseWorkflow, []byte(baseContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an intermediate shared workflow that imports the base
	intermediateWorkflow := filepath.Join(sharedDir, "intermediate.md")
	intermediateContent := `---
description: Intermediate workflow
imports:
  - base.md
---
# Intermediate`
	if err := os.WriteFile(intermediateWorkflow, []byte(intermediateContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a top-level workflow that imports the intermediate workflow
	topWorkflow := filepath.Join(workflowsDir, "main.md")
	topContent := `---
description: Main workflow
imports:
  - shared/intermediate.md
---
# Main`
	if err := os.WriteFile(topWorkflow, []byte(topContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Build dependency graph
	graph := NewDependencyGraph(workflowsDir)
	compiler := workflow.NewCompiler()
	if err := graph.BuildGraph(compiler); err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}

	// Test: Modifying the base workflow should transitively affect the top-level workflow
	t.Run("nested import modification affects top-level workflow", func(t *testing.T) {
		affected := graph.GetAffectedWorkflows(baseWorkflow)

		// Should find the top-level workflow through the intermediate workflow
		if len(affected) != 1 {
			t.Errorf("GetAffectedWorkflows() returned %d workflows, want 1", len(affected))
		}

		if len(affected) > 0 && affected[0] != topWorkflow {
			t.Errorf("GetAffectedWorkflows() = %v, want [%s]", affected, topWorkflow)
		}
	})
}

func TestDependencyGraph_MultipleTopLevelImporters(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	mcpDir := filepath.Join(sharedDir, "mcp")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a deeply nested shared workflow
	deepShared := filepath.Join(mcpDir, "tool.md")
	deepContent := `---
description: MCP Tool
---
# Tool`
	if err := os.WriteFile(deepShared, []byte(deepContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create multiple top-level workflows that import the deep shared workflow
	workflows := make([]string, 3)
	for i := 0; i < 3; i++ {
		workflows[i] = filepath.Join(workflowsDir, fmt.Sprintf("workflow%d.md", i))
		content := fmt.Sprintf(`---
description: Workflow %d
imports:
  - shared/mcp/tool.md
---
# Workflow %d`, i, i)
		if err := os.WriteFile(workflows[i], []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Build dependency graph
	graph := NewDependencyGraph(workflowsDir)
	compiler := workflow.NewCompiler()
	if err := graph.BuildGraph(compiler); err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}

	// Test: Modifying the deep shared workflow should affect all three top-level workflows
	affected := graph.GetAffectedWorkflows(deepShared)

	if len(affected) != 3 {
		t.Errorf("GetAffectedWorkflows() returned %d workflows, want 3", len(affected))
	}

	// Verify all three workflows are in the affected list
	affectedMap := make(map[string]bool)
	for _, w := range affected {
		affectedMap[w] = true
	}

	for i, wf := range workflows {
		if !affectedMap[wf] {
			t.Errorf("GetAffectedWorkflows() should include workflow%d.md", i)
		}
	}
}

func TestDependencyGraph_CircularImportDetection(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create workflow A that imports B
	workflowA := filepath.Join(sharedDir, "a.md")
	contentA := `---
description: Workflow A
imports:
  - b.md
---
# A`
	if err := os.WriteFile(workflowA, []byte(contentA), 0644); err != nil {
		t.Fatal(err)
	}

	// Create workflow B that imports A (circular dependency)
	workflowB := filepath.Join(sharedDir, "b.md")
	contentB := `---
description: Workflow B
imports:
  - a.md
---
# B`
	if err := os.WriteFile(workflowB, []byte(contentB), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a top-level workflow that imports A
	topWorkflow := filepath.Join(workflowsDir, "main.md")
	topContent := `---
description: Main workflow
imports:
  - shared/a.md
---
# Main`
	if err := os.WriteFile(topWorkflow, []byte(topContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Build dependency graph - should handle circular imports gracefully
	graph := NewDependencyGraph(workflowsDir)
	compiler := workflow.NewCompiler()
	if err := graph.BuildGraph(compiler); err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}

	// Test: Modifying workflow A should affect the top-level workflow
	affected := graph.GetAffectedWorkflows(workflowA)

	if len(affected) != 1 {
		t.Errorf("GetAffectedWorkflows() returned %d workflows, want 1", len(affected))
	}

	if len(affected) > 0 && affected[0] != topWorkflow {
		t.Errorf("GetAffectedWorkflows() = %v, want [%s]", affected, topWorkflow)
	}
}

func TestDependencyGraph_NewFileAddition(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create initial shared workflow
	sharedWorkflow := filepath.Join(sharedDir, "helper.md")
	sharedContent := `---
description: Helper workflow
---
# Helper`
	if err := os.WriteFile(sharedWorkflow, []byte(sharedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Build initial dependency graph
	graph := NewDependencyGraph(workflowsDir)
	compiler := workflow.NewCompiler()
	if err := graph.BuildGraph(compiler); err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}

	// Test: Adding a new top-level workflow file
	t.Run("new top-level workflow affects only itself", func(t *testing.T) {
		newWorkflow := filepath.Join(workflowsDir, "new.md")
		newContent := `---
description: New workflow
imports:
  - shared/helper.md
---
# New`
		if err := os.WriteFile(newWorkflow, []byte(newContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Get affected workflows for new file (not yet in graph)
		affected := graph.GetAffectedWorkflows(newWorkflow)

		// Should compile only itself
		if len(affected) != 1 {
			t.Errorf("GetAffectedWorkflows() for new file returned %d workflows, want 1", len(affected))
		}

		if len(affected) > 0 && affected[0] != newWorkflow {
			t.Errorf("GetAffectedWorkflows() = %v, want [%s]", affected, newWorkflow)
		}
	})

	// Test: Adding a new shared workflow file
	t.Run("new shared workflow returns all top-level workflows", func(t *testing.T) {
		// Create a top-level workflow first
		topWorkflow := filepath.Join(workflowsDir, "main.md")
		topContent := `---
description: Main workflow
---
# Main`
		if err := os.WriteFile(topWorkflow, []byte(topContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Update graph to include the top-level workflow
		if err := graph.addWorkflow(topWorkflow, compiler); err != nil {
			t.Fatal(err)
		}

		// Now test with a new shared workflow
		newShared := filepath.Join(sharedDir, "new-shared.md")
		newSharedContent := `---
description: New shared workflow
---
# New Shared`
		if err := os.WriteFile(newShared, []byte(newSharedContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Get affected workflows for new shared file (not yet in graph)
		affected := graph.GetAffectedWorkflows(newShared)

		// Should return all top-level workflows as we don't know dependencies yet
		if len(affected) != 1 {
			t.Errorf("GetAffectedWorkflows() for new shared file returned %d workflows, want 1 (all top-level)", len(affected))
		}
	})
}

func TestDependencyGraph_EmptyGraph(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Build dependency graph with no workflows
	graph := NewDependencyGraph(workflowsDir)
	compiler := workflow.NewCompiler()
	if err := graph.BuildGraph(compiler); err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}

	// Test: Query on empty graph
	t.Run("empty graph returns empty for any file", func(t *testing.T) {
		affected := graph.GetAffectedWorkflows("/nonexistent/file.md")
		if len(affected) != 0 {
			t.Errorf("GetAffectedWorkflows() on empty graph returned %d workflows, want 0", len(affected))
		}
	})

	// Test: Add workflow to empty graph
	t.Run("add first workflow to empty graph", func(t *testing.T) {
		firstWorkflow := filepath.Join(workflowsDir, "first.md")
		firstContent := `---
description: First workflow
---
# First`
		if err := os.WriteFile(firstWorkflow, []byte(firstContent), 0644); err != nil {
			t.Fatal(err)
		}

		if err := graph.addWorkflow(firstWorkflow, compiler); err != nil {
			t.Fatal(err)
		}

		// Should have one node now
		if len(graph.nodes) != 1 {
			t.Errorf("After adding first workflow, graph has %d nodes, want 1", len(graph.nodes))
		}

		affected := graph.GetAffectedWorkflows(firstWorkflow)
		if len(affected) != 1 {
			t.Errorf("GetAffectedWorkflows() returned %d workflows, want 1", len(affected))
		}
	})
}

func TestDependencyGraph_ComplexDependencyChain(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	mcpDir := filepath.Join(sharedDir, "mcp")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a complex dependency chain:
	// top1 -> shared/a -> shared/b -> shared/mcp/c
	// top2 -> shared/a
	// top3 -> shared/b

	// Level 3: Deepest shared workflow
	workflowC := filepath.Join(mcpDir, "c.md")
	contentC := `---
description: Workflow C
---
# C`
	if err := os.WriteFile(workflowC, []byte(contentC), 0644); err != nil {
		t.Fatal(err)
	}

	// Level 2: Shared workflow B imports C
	workflowB := filepath.Join(sharedDir, "b.md")
	contentB := `---
description: Workflow B
imports:
  - mcp/c.md
---
# B`
	if err := os.WriteFile(workflowB, []byte(contentB), 0644); err != nil {
		t.Fatal(err)
	}

	// Level 1: Shared workflow A imports B
	workflowA := filepath.Join(sharedDir, "a.md")
	contentA := `---
description: Workflow A
imports:
  - b.md
---
# A`
	if err := os.WriteFile(workflowA, []byte(contentA), 0644); err != nil {
		t.Fatal(err)
	}

	// Top-level workflows
	top1 := filepath.Join(workflowsDir, "top1.md")
	content1 := `---
description: Top 1
imports:
  - shared/a.md
---
# Top 1`
	if err := os.WriteFile(top1, []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}

	top2 := filepath.Join(workflowsDir, "top2.md")
	content2 := `---
description: Top 2
imports:
  - shared/a.md
---
# Top 2`
	if err := os.WriteFile(top2, []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}

	top3 := filepath.Join(workflowsDir, "top3.md")
	content3 := `---
description: Top 3
imports:
  - shared/b.md
---
# Top 3`
	if err := os.WriteFile(top3, []byte(content3), 0644); err != nil {
		t.Fatal(err)
	}

	// Build dependency graph
	graph := NewDependencyGraph(workflowsDir)
	compiler := workflow.NewCompiler()
	if err := graph.BuildGraph(compiler); err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}

	// Test: Modifying C should affect top1, top2, and top3
	t.Run("modifying deepest workflow affects all importers", func(t *testing.T) {
		affected := graph.GetAffectedWorkflows(workflowC)

		if len(affected) != 3 {
			t.Errorf("GetAffectedWorkflows(C) returned %d workflows, want 3", len(affected))
		}

		affectedMap := make(map[string]bool)
		for _, w := range affected {
			affectedMap[w] = true
		}

		if !affectedMap[top1] || !affectedMap[top2] || !affectedMap[top3] {
			t.Errorf("GetAffectedWorkflows(C) should include top1, top2, and top3")
		}
	})

	// Test: Modifying B should affect top1, top2, and top3
	t.Run("modifying intermediate workflow affects correct importers", func(t *testing.T) {
		affected := graph.GetAffectedWorkflows(workflowB)

		if len(affected) != 3 {
			t.Errorf("GetAffectedWorkflows(B) returned %d workflows, want 3", len(affected))
		}

		affectedMap := make(map[string]bool)
		for _, w := range affected {
			affectedMap[w] = true
		}

		if !affectedMap[top1] || !affectedMap[top2] || !affectedMap[top3] {
			t.Errorf("GetAffectedWorkflows(B) should include top1, top2, and top3")
		}
	})

	// Test: Modifying A should affect only top1 and top2
	t.Run("modifying upper workflow affects only direct importers", func(t *testing.T) {
		affected := graph.GetAffectedWorkflows(workflowA)

		if len(affected) != 2 {
			t.Errorf("GetAffectedWorkflows(A) returned %d workflows, want 2", len(affected))
		}

		affectedMap := make(map[string]bool)
		for _, w := range affected {
			affectedMap[w] = true
		}

		if !affectedMap[top1] || !affectedMap[top2] {
			t.Errorf("GetAffectedWorkflows(A) should include top1 and top2")
		}

		if affectedMap[top3] {
			t.Errorf("GetAffectedWorkflows(A) should NOT include top3")
		}
	})
}

func TestDependencyGraph_ImportsWithInputs(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a shared workflow
	sharedWorkflow := filepath.Join(sharedDir, "parameterized.md")
	sharedContent := `---
description: Parameterized workflow
---
# Parameterized`
	if err := os.WriteFile(sharedWorkflow, []byte(sharedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a top-level workflow that imports with inputs
	topWorkflow := filepath.Join(workflowsDir, "main.md")
	topContent := `---
description: Main workflow
imports:
  - path: shared/parameterized.md
    inputs:
      key: value
---
# Main`
	if err := os.WriteFile(topWorkflow, []byte(topContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Build dependency graph
	graph := NewDependencyGraph(workflowsDir)
	compiler := workflow.NewCompiler()
	if err := graph.BuildGraph(compiler); err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}

	// Test: Graph should handle imports with inputs object format
	t.Run("imports with inputs are tracked correctly", func(t *testing.T) {
		affected := graph.GetAffectedWorkflows(sharedWorkflow)

		if len(affected) != 1 {
			t.Errorf("GetAffectedWorkflows() returned %d workflows, want 1", len(affected))
		}

		if len(affected) > 0 && affected[0] != topWorkflow {
			t.Errorf("GetAffectedWorkflows() = %v, want [%s]", affected, topWorkflow)
		}
	})
}
