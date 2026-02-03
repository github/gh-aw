//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

func TestLogsCommandFlags(t *testing.T) {
	// Test that the logs command has the expected flags including the new engine flag
	cmd := NewLogsCommand()

	// Check that all expected flags are present
	expectedFlags := []string{"count", "start-date", "end-date", "output", "engine", "ref", "before-run-id", "after-run-id"}

	for _, flagName := range expectedFlags {
		flag := cmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Errorf("Expected flag '%s' not found in logs command", flagName)
		}
	}

	// Test ref flag specifically
	refFlag := cmd.Flags().Lookup("ref")
	if refFlag == nil {
		t.Fatal("Ref flag not found")
	}

	if refFlag.Usage != "Filter runs by branch or tag name (e.g., main, v1.0.0)" {
		t.Errorf("Unexpected ref flag usage text: %s", refFlag.Usage)
	}

	if refFlag.DefValue != "" {
		t.Errorf("Expected ref flag default value to be empty, got: %s", refFlag.DefValue)
	}

	// Test before-run-id flag
	beforeRunIDFlag := cmd.Flags().Lookup("before-run-id")
	if beforeRunIDFlag == nil {
		t.Fatal("Before-run-id flag not found")
	}

	if beforeRunIDFlag.Usage != "Filter runs with database ID before this value (exclusive)" {
		t.Errorf("Unexpected before-run-id flag usage text: %s", beforeRunIDFlag.Usage)
	}

	// Test after-run-id flag
	afterRunIDFlag := cmd.Flags().Lookup("after-run-id")
	if afterRunIDFlag == nil {
		t.Fatal("After-run-id flag not found")
	}

	if afterRunIDFlag.Usage != "Filter runs with database ID after this value (exclusive)" {
		t.Errorf("Unexpected after-run-id flag usage text: %s", afterRunIDFlag.Usage)
	}

	// Test engine flag specifically
	engineFlag := cmd.Flags().Lookup("engine")
	if engineFlag == nil {
		t.Fatal("Engine flag not found")
	}

	if engineFlag.Usage != "Filter logs by AI engine (claude, codex, copilot, custom)" {
		t.Errorf("Unexpected engine flag usage text: %s", engineFlag.Usage)
	}

	if engineFlag.DefValue != "" {
		t.Errorf("Expected engine flag default value to be empty, got: %s", engineFlag.DefValue)
	}

	// Test that engine flag has the -e shorthand for consistency with other commands
	if engineFlag.Shorthand != "e" {
		t.Errorf("Expected engine flag shorthand to be 'e', got: %s", engineFlag.Shorthand)
	}
}

func TestRunIDFilteringLogic(t *testing.T) {
	// Test the run ID filtering logic in isolation
	testRuns := []WorkflowRun{
		{DatabaseID: 1000, WorkflowName: "Test Workflow"},
		{DatabaseID: 1500, WorkflowName: "Test Workflow"},
		{DatabaseID: 2000, WorkflowName: "Test Workflow"},
		{DatabaseID: 2500, WorkflowName: "Test Workflow"},
		{DatabaseID: 3000, WorkflowName: "Test Workflow"},
	}

	// Test before-run-id filter (exclusive)
	var filteredRuns []WorkflowRun
	beforeRunID := int64(2000)
	for _, run := range testRuns {
		if beforeRunID > 0 && run.DatabaseID >= beforeRunID {
			continue
		}
		filteredRuns = append(filteredRuns, run)
	}

	if len(filteredRuns) != 2 {
		t.Errorf("Expected 2 runs before ID 2000 (exclusive), got %d", len(filteredRuns))
	}
	if filteredRuns[0].DatabaseID != 1000 || filteredRuns[1].DatabaseID != 1500 {
		t.Errorf("Expected runs 1000 and 1500, got %d and %d", filteredRuns[0].DatabaseID, filteredRuns[1].DatabaseID)
	}

	// Test after-run-id filter (exclusive)
	filteredRuns = nil
	afterRunID := int64(2000)
	for _, run := range testRuns {
		if afterRunID > 0 && run.DatabaseID <= afterRunID {
			continue
		}
		filteredRuns = append(filteredRuns, run)
	}

	if len(filteredRuns) != 2 {
		t.Errorf("Expected 2 runs after ID 2000 (exclusive), got %d", len(filteredRuns))
	}
	if filteredRuns[0].DatabaseID != 2500 || filteredRuns[1].DatabaseID != 3000 {
		t.Errorf("Expected runs 2500 and 3000, got %d and %d", filteredRuns[0].DatabaseID, filteredRuns[1].DatabaseID)
	}

	// Test range filter (both before and after)
	filteredRuns = nil
	beforeRunID = int64(2500)
	afterRunID = int64(1000)
	for _, run := range testRuns {
		// Apply before-run-id filter (exclusive)
		if beforeRunID > 0 && run.DatabaseID >= beforeRunID {
			continue
		}
		// Apply after-run-id filter (exclusive)
		if afterRunID > 0 && run.DatabaseID <= afterRunID {
			continue
		}
		filteredRuns = append(filteredRuns, run)
	}

	if len(filteredRuns) != 2 {
		t.Errorf("Expected 2 runs in range (1000, 2500), got %d", len(filteredRuns))
	}
	if filteredRuns[0].DatabaseID != 1500 || filteredRuns[1].DatabaseID != 2000 {
		t.Errorf("Expected runs 1500 and 2000, got %d and %d", filteredRuns[0].DatabaseID, filteredRuns[1].DatabaseID)
	}
}

func TestRefFilteringWithGitHubCLI(t *testing.T) {
	// Test that ref filtering is properly added to GitHub CLI args
	// This is a unit test for the args construction, not a network test

	// Simulate args construction for ref filtering
	args := []string{"run", "list", "--json", "databaseId,number,url,status,conclusion,workflowName,createdAt,startedAt,updatedAt,event,headBranch,headSha,displayTitle"}

	ref := "feature-branch"
	if ref != "" {
		args = append(args, "--branch", ref)
	}

	// Verify that the ref filter was added correctly (uses --branch flag under the hood)
	found := false
	for i, arg := range args {
		if arg == "--branch" && i+1 < len(args) && args[i+1] == "feature-branch" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected ref filter '--branch feature-branch' not found in args: %v", args)
	}
}

func TestFindAgentLogFile(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir := testutil.TempDir(t, "test-*")

	// Test 1: Copilot engine with agent_output directory
	t.Run("Copilot engine uses agent_output", func(t *testing.T) {
		copilotEngine := workflow.NewCopilotEngine()

		// Create agent_output directory with a log file
		agentOutputDir := filepath.Join(tmpDir, "copilot_test", "agent_output")
		err := os.MkdirAll(agentOutputDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create agent_output directory: %v", err)
		}

		logFile := filepath.Join(agentOutputDir, "debug.log")
		err = os.WriteFile(logFile, []byte("test log content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create log file: %v", err)
		}

		// Create agent-stdio.log as well (should be ignored for Copilot)
		stdioLog := filepath.Join(tmpDir, "copilot_test", "agent-stdio.log")
		err = os.WriteFile(stdioLog, []byte("stdio content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create agent-stdio.log: %v", err)
		}

		// Test findAgentLogFile
		found, ok := findAgentLogFile(filepath.Join(tmpDir, "copilot_test"), copilotEngine)
		if !ok {
			t.Errorf("Expected to find agent log file for Copilot engine")
		}

		// Should find the file in agent_output directory
		if !strings.Contains(found, "agent_output") {
			t.Errorf("Expected to find file in agent_output directory, got: %s", found)
		}
	})

	// Test Copilot engine with flattened agent_outputs artifact
	// After flattening, session logs are at sandbox/agent/logs/ in the root
	t.Run("copilot_engine_flattened_location", func(t *testing.T) {
		copilotDir := filepath.Join(tmpDir, "copilot_flattened_test")
		err := os.MkdirAll(copilotDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}

		// Create flattened session logs directory (after flattenAgentOutputsArtifact)
		sessionLogsDir := filepath.Join(copilotDir, "sandbox", "agent", "logs")
		err = os.MkdirAll(sessionLogsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create flattened session logs directory: %v", err)
		}

		// Create a test session log file
		sessionLog := filepath.Join(sessionLogsDir, "session-test-123.log")
		err = os.WriteFile(sessionLog, []byte("test session log content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create session log file: %v", err)
		}

		copilotEngine := workflow.NewCopilotEngine()

		// Test findAgentLogFile - should find the session log in flattened location
		found, ok := findAgentLogFile(copilotDir, copilotEngine)
		if !ok {
			t.Errorf("Expected to find agent log file for Copilot engine in flattened location")
		}

		// Should find the session log file
		if !strings.HasSuffix(found, "session-test-123.log") {
			t.Errorf("Expected to find session-test-123.log, but found %s", found)
		}

		// Verify the path is correct
		expectedPath := filepath.Join(copilotDir, "sandbox", "agent", "logs", "session-test-123.log")
		if found != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, found)
		}
	})

	// Test Copilot engine with session log directly in run directory (recursive search)
	// This handles cases where artifact structure differs from expected
	t.Run("copilot_engine_recursive_search", func(t *testing.T) {
		copilotDir := filepath.Join(tmpDir, "copilot_recursive_test")
		err := os.MkdirAll(copilotDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}

		// Create session log directly in the run directory
		// This simulates the case where the artifact was flattened differently
		sessionLog := filepath.Join(copilotDir, "session-direct-456.log")
		err = os.WriteFile(sessionLog, []byte("test session log content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create session log file: %v", err)
		}

		copilotEngine := workflow.NewCopilotEngine()

		// Test findAgentLogFile - should find via recursive search
		found, ok := findAgentLogFile(copilotDir, copilotEngine)
		if !ok {
			t.Errorf("Expected to find agent log file via recursive search")
		}

		// Should find the session log file
		if !strings.HasSuffix(found, "session-direct-456.log") {
			t.Errorf("Expected to find session-direct-456.log, but found %s", found)
		}

		// Verify the path is correct
		if found != sessionLog {
			t.Errorf("Expected path %s, got %s", sessionLog, found)
		}
	})

	// Test Copilot engine with process log (new naming convention)
	// Copilot changed from session-*.log to process-*.log
	t.Run("copilot_engine_process_log", func(t *testing.T) {
		copilotDir := filepath.Join(tmpDir, "copilot_process_test")
		err := os.MkdirAll(copilotDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}

		// Create process log directly in the run directory
		// This simulates the new naming convention for Copilot logs
		processLog := filepath.Join(copilotDir, "process-12345.log")
		err = os.WriteFile(processLog, []byte("test process log content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create process log file: %v", err)
		}

		copilotEngine := workflow.NewCopilotEngine()

		// Test findAgentLogFile - should find via recursive search
		found, ok := findAgentLogFile(copilotDir, copilotEngine)
		if !ok {
			t.Errorf("Expected to find agent log file via recursive search")
		}

		// Should find the process log file
		if !strings.HasSuffix(found, "process-12345.log") {
			t.Errorf("Expected to find process-12345.log, but found %s", found)
		}

		// Verify the path is correct
		if found != processLog {
			t.Errorf("Expected path %s, got %s", processLog, found)
		}
	})

	// Test Copilot engine with process log in nested directory
	t.Run("copilot_engine_process_log_nested", func(t *testing.T) {
		copilotDir := filepath.Join(tmpDir, "copilot_process_nested_test")
		err := os.MkdirAll(copilotDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}

		// Create nested directory structure
		processLogsDir := filepath.Join(copilotDir, "sandbox", "agent", "logs")
		err = os.MkdirAll(processLogsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create process logs directory: %v", err)
		}

		// Create a test process log file
		processLog := filepath.Join(processLogsDir, "process-test-789.log")
		err = os.WriteFile(processLog, []byte("test process log content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create process log file: %v", err)
		}

		copilotEngine := workflow.NewCopilotEngine()

		// Test findAgentLogFile - should find the process log in nested location
		found, ok := findAgentLogFile(copilotDir, copilotEngine)
		if !ok {
			t.Errorf("Expected to find agent log file for Copilot engine in nested location")
		}

		// Should find the process log file
		if !strings.HasSuffix(found, "process-test-789.log") {
			t.Errorf("Expected to find process-test-789.log, but found %s", found)
		}

		// Verify the path is correct
		expectedPath := filepath.Join(copilotDir, "sandbox", "agent", "logs", "process-test-789.log")
		if found != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, found)
		}
	})

	// Test 2: Claude engine with agent-stdio.log
	t.Run("Claude engine uses agent-stdio.log", func(t *testing.T) {
		claudeEngine := workflow.NewClaudeEngine()

		// Create only agent-stdio.log
		claudeDir := filepath.Join(tmpDir, "claude_test")
		err := os.MkdirAll(claudeDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create claude test directory: %v", err)
		}

		stdioLog := filepath.Join(claudeDir, "agent-stdio.log")
		err = os.WriteFile(stdioLog, []byte("stdio content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create agent-stdio.log: %v", err)
		}

		// Test findAgentLogFile
		found, ok := findAgentLogFile(claudeDir, claudeEngine)
		if !ok {
			t.Errorf("Expected to find agent log file for Claude engine")
		}

		// Should find agent-stdio.log
		if !strings.Contains(found, "agent-stdio.log") {
			t.Errorf("Expected to find agent-stdio.log, got: %s", found)
		}
	})

	// Test 3: No logs found
	t.Run("No logs found returns false", func(t *testing.T) {
		emptyDir := filepath.Join(tmpDir, "empty_test")
		err := os.MkdirAll(emptyDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create empty test directory: %v", err)
		}

		claudeEngine := workflow.NewClaudeEngine()
		_, ok := findAgentLogFile(emptyDir, claudeEngine)
		if ok {
			t.Errorf("Expected to not find agent log file in empty directory")
		}
	})

	// Test 4: Codex engine with agent-stdio.log
	t.Run("Codex engine uses agent-stdio.log", func(t *testing.T) {
		codexEngine := workflow.NewCodexEngine()

		// Create only agent-stdio.log
		codexDir := filepath.Join(tmpDir, "codex_test")
		err := os.MkdirAll(codexDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create codex test directory: %v", err)
		}

		stdioLog := filepath.Join(codexDir, "agent-stdio.log")
		err = os.WriteFile(stdioLog, []byte("stdio content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create agent-stdio.log: %v", err)
		}

		// Test findAgentLogFile
		found, ok := findAgentLogFile(codexDir, codexEngine)
		if !ok {
			t.Errorf("Expected to find agent log file for Codex engine")
		}

		// Should find agent-stdio.log
		if !strings.Contains(found, "agent-stdio.log") {
			t.Errorf("Expected to find agent-stdio.log, got: %s", found)
		}
	})
}
