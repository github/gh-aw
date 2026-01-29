//go:build !integration

package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayStatsTable_Empty(t *testing.T) {
	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayStatsTable([]*WorkflowStats{})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "No workflow statistics to display") {
		t.Errorf("Expected warning message for empty stats list, got: %s", output)
	}
}

func TestDisplayStatsTable_LessThan10(t *testing.T) {
	// Create test stats with 5 workflows
	statsList := []*WorkflowStats{
		{Workflow: "workflow1.lock.yml", FileSize: 5000, Jobs: 5, Steps: 10, ScriptCount: 8},
		{Workflow: "workflow2.lock.yml", FileSize: 4000, Jobs: 4, Steps: 8, ScriptCount: 6},
		{Workflow: "workflow3.lock.yml", FileSize: 3000, Jobs: 3, Steps: 6, ScriptCount: 4},
		{Workflow: "workflow4.lock.yml", FileSize: 2000, Jobs: 2, Steps: 4, ScriptCount: 2},
		{Workflow: "workflow5.lock.yml", FileSize: 1000, Jobs: 1, Steps: 2, ScriptCount: 1},
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayStatsTable(statsList)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Should show all 5 workflows
	for _, stats := range statsList {
		if !strings.Contains(output, stats.Workflow) {
			t.Errorf("Expected workflow %s to be displayed, but it wasn't found", stats.Workflow)
		}
	}

	// Should show "Total workflows: 5"
	if !strings.Contains(output, "Total workflows: 5") {
		t.Errorf("Expected 'Total workflows: 5' in output, got: %s", output)
	}

	// Should NOT show "Showing top X of Y" message
	if strings.Contains(output, "Showing top") {
		t.Errorf("Should not show truncation message for 5 workflows, got: %s", output)
	}
}

func TestDisplayStatsTable_Exactly10(t *testing.T) {
	// Create test stats with exactly 10 workflows
	statsList := make([]*WorkflowStats, 10)
	for i := 0; i < 10; i++ {
		statsList[i] = &WorkflowStats{
			Workflow:    fmt.Sprintf("workflow%d.lock.yml", i+1),
			FileSize:    int64((10 - i) * 1000), // Descending sizes
			Jobs:        10 - i,
			Steps:       (10 - i) * 2,
			ScriptCount: (10 - i) * 3,
		}
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayStatsTable(statsList)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Should show all 10 workflows
	for _, stats := range statsList {
		if !strings.Contains(output, stats.Workflow) {
			t.Errorf("Expected workflow %s to be displayed, but it wasn't found", stats.Workflow)
		}
	}

	// Should show "Total workflows: 10"
	if !strings.Contains(output, "Total workflows: 10") {
		t.Errorf("Expected 'Total workflows: 10' in output, got: %s", output)
	}

	// Should NOT show "Showing top X of Y" message
	if strings.Contains(output, "Showing top") {
		t.Errorf("Should not show truncation message for exactly 10 workflows, got: %s", output)
	}
}

func TestDisplayStatsTable_MoreThan10(t *testing.T) {
	// Create test stats with 15 workflows
	statsList := make([]*WorkflowStats, 15)
	for i := 0; i < 15; i++ {
		statsList[i] = &WorkflowStats{
			Workflow:    fmt.Sprintf("workflow%d.lock.yml", i+1),
			FileSize:    int64((15 - i) * 1000), // Descending sizes
			Jobs:        15 - i,
			Steps:       (15 - i) * 2,
			ScriptCount: (15 - i) * 3,
		}
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayStatsTable(statsList)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Should show top 10 workflows (largest sizes)
	for i := 0; i < 10; i++ {
		if !strings.Contains(output, statsList[i].Workflow) {
			t.Errorf("Expected top 10 workflow %s to be displayed, but it wasn't found", statsList[i].Workflow)
		}
	}

	// Should NOT show the last 5 workflows (smallest sizes)
	for i := 10; i < 15; i++ {
		if strings.Contains(output, statsList[i].Workflow) {
			t.Errorf("Did not expect workflow %s to be displayed in top 10, but it was found", statsList[i].Workflow)
		}
	}

	// Should show "Showing top 10 of 15 workflows"
	if !strings.Contains(output, "Showing top 10 of 15 workflows") {
		t.Errorf("Expected 'Showing top 10 of 15 workflows' in output, got: %s", output)
	}

	// Should still show "Total workflows: 15" (total count, not displayed count)
	if !strings.Contains(output, "Total workflows: 15") {
		t.Errorf("Expected 'Total workflows: 15' in output, got: %s", output)
	}
}

func TestDisplayStatsTable_SortsBySize(t *testing.T) {
	// Create test stats with unsorted sizes
	statsList := []*WorkflowStats{
		{Workflow: "small.lock.yml", FileSize: 1000, Jobs: 1, Steps: 2, ScriptCount: 1},
		{Workflow: "large.lock.yml", FileSize: 5000, Jobs: 5, Steps: 10, ScriptCount: 8},
		{Workflow: "medium.lock.yml", FileSize: 3000, Jobs: 3, Steps: 6, ScriptCount: 4},
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayStatsTable(statsList)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Find positions of workflows in output
	largePos := strings.Index(output, "large.lock.yml")
	mediumPos := strings.Index(output, "medium.lock.yml")
	smallPos := strings.Index(output, "small.lock.yml")

	// Verify they appear in descending order by size
	if largePos == -1 || mediumPos == -1 || smallPos == -1 {
		t.Fatalf("Not all workflows found in output: large=%d, medium=%d, small=%d", largePos, mediumPos, smallPos)
	}

	if largePos >= mediumPos {
		t.Errorf("Expected large workflow to appear before medium workflow")
	}

	if mediumPos >= smallPos {
		t.Errorf("Expected medium workflow to appear before small workflow")
	}
}

func TestCollectWorkflowStats(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()

	// Create a test workflow YAML file
	testYAML := `name: Test Workflow
on: [push]
jobs:
  test-job:
    runs-on: ubuntu-latest
    steps:
      - name: Step 1
        run: echo "test1"
      - name: Step 2
        run: echo "test2"
      - name: Step 3
        uses: actions/checkout@v2
  test-job-2:
    runs-on: ubuntu-latest
    steps:
      - name: Step 4
        run: echo "test3"
`
	lockFilePath := filepath.Join(tempDir, "test.lock.yml")
	err := os.WriteFile(lockFilePath, []byte(testYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Collect stats
	stats, err := collectWorkflowStats(lockFilePath)
	if err != nil {
		t.Fatalf("collectWorkflowStats failed: %v", err)
	}

	// Verify stats
	if stats.Workflow != "test.lock.yml" {
		t.Errorf("Expected workflow name 'test.lock.yml', got %s", stats.Workflow)
	}

	if stats.Jobs != 2 {
		t.Errorf("Expected 2 jobs, got %d", stats.Jobs)
	}

	if stats.Steps != 4 {
		t.Errorf("Expected 4 steps, got %d", stats.Steps)
	}

	if stats.ScriptCount != 3 {
		t.Errorf("Expected 3 scripts (run commands), got %d", stats.ScriptCount)
	}

	if stats.FileSize <= 0 {
		t.Errorf("Expected positive file size, got %d", stats.FileSize)
	}
}

func TestCollectWorkflowStats_NonExistentFile(t *testing.T) {
	stats, err := collectWorkflowStats("/nonexistent/file.lock.yml")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
	if stats != nil {
		t.Error("Expected nil stats for non-existent file")
	}
}

func TestCollectWorkflowStats_InvalidYAML(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()

	// Create an invalid YAML file
	invalidYAML := `this is not: valid: yaml: at: all:`
	lockFilePath := filepath.Join(tempDir, "invalid.lock.yml")
	err := os.WriteFile(lockFilePath, []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Attempt to collect stats
	stats, err := collectWorkflowStats(lockFilePath)
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
	if stats != nil {
		t.Error("Expected nil stats for invalid YAML")
	}
}
