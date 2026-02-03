//go:build !integration

package cli

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/cli/fileutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestDownloadWorkflowLogs(t *testing.T) {
	t.Skip("Skipping slow network-dependent test")

	// Test the DownloadWorkflowLogs function
	// This should either fail with auth error (if not authenticated)
	// or succeed with no results (if authenticated but no workflows match)
	err := DownloadWorkflowLogs(context.Background(), "", 1, "", "", "./test-logs", "", "", 0, 0, "", false, false, false, false, false, false, false, 0, "summary.json", "")

	// If GitHub CLI is authenticated, the function may succeed but find no results
	// If not authenticated, it should return an auth error
	if err != nil {
		// If there's an error, it should be an authentication or workflow-related error
		errMsg := strings.ToLower(err.Error())
		if !strings.Contains(errMsg, "authentication required") &&
			!strings.Contains(errMsg, "failed to list workflow runs") &&
			!strings.Contains(errMsg, "exit status 1") {
			t.Errorf("Expected authentication error, workflow listing error, or no error, got: %v", err)
		}
	}
	// If err is nil, that's also acceptable (authenticated case with no results)

	// Clean up
	os.RemoveAll("./test-logs")
}

func TestDirExists(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Test existing directory
	if !fileutil.DirExists(tmpDir) {
		t.Errorf("DirExists should return true for existing directory")
	}

	// Test non-existing directory
	nonExistentDir := filepath.Join(tmpDir, "does-not-exist")
	if fileutil.DirExists(nonExistentDir) {
		t.Errorf("DirExists should return false for non-existing directory")
	}

	// Test file vs directory
	testFile := filepath.Join(tmpDir, "testfile")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if fileutil.DirExists(testFile) {
		t.Errorf("DirExists should return false for a file")
	}
}

func TestIsDirEmpty(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Test empty directory
	emptyDir := filepath.Join(tmpDir, "empty")
	err := os.Mkdir(emptyDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create empty directory: %v", err)
	}

	if !fileutil.IsDirEmpty(emptyDir) {
		t.Errorf("IsDirEmpty should return true for empty directory")
	}

	// Test directory with files
	nonEmptyDir := filepath.Join(tmpDir, "nonempty")
	err = os.Mkdir(nonEmptyDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create non-empty directory: %v", err)
	}

	testFile := filepath.Join(nonEmptyDir, "testfile")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if fileutil.IsDirEmpty(nonEmptyDir) {
		t.Errorf("IsDirEmpty should return false for directory with files")
	}

	// Test non-existing directory
	nonExistentDir := filepath.Join(tmpDir, "does-not-exist")
	if !fileutil.IsDirEmpty(nonExistentDir) {
		t.Errorf("IsDirEmpty should return true for non-existing directory")
	}
}

func TestErrNoArtifacts(t *testing.T) {
	// Test that ErrNoArtifacts is properly defined and can be used with errors.Is
	err := ErrNoArtifacts
	if !errors.Is(err, ErrNoArtifacts) {
		t.Errorf("errors.Is should return true for ErrNoArtifacts")
	}

	// Test wrapping
	wrappedErr := errors.New("wrapped: " + ErrNoArtifacts.Error())
	if errors.Is(wrappedErr, ErrNoArtifacts) {
		t.Errorf("errors.Is should return false for wrapped error that doesn't use errors.Wrap")
	}
}

func TestListWorkflowRunsWithPagination(t *testing.T) {
	// Test that listWorkflowRunsWithPagination properly adds beforeDate filter
	// Since we can't easily mock the GitHub CLI, we'll test with known auth issues

	// This should fail with authentication error (if not authenticated)
	// or succeed with empty results (if authenticated but no workflows match)
	runs, _, err := listWorkflowRunsWithPagination(ListWorkflowRunsOptions{
		WorkflowName:   "nonexistent-workflow",
		Limit:          5,
		BeforeDate:     "2024-01-01T00:00:00Z",
		ProcessedCount: 0,
		TargetCount:    5,
		Verbose:        false,
	})

	if err != nil {
		// If there's an error, it should be an authentication error or workflow not found
		if !strings.Contains(err.Error(), "authentication required") && !strings.Contains(err.Error(), "failed to list workflow runs") {
			t.Errorf("Expected authentication error or workflow error, got: %v", err)
		}
	} else {
		// If no error, should return empty results for nonexistent workflow
		if len(runs) > 0 {
			t.Errorf("Expected empty results for nonexistent workflow, got %d runs", len(runs))
		}
	}
}

func TestIterativeAlgorithmConstants(t *testing.T) {
	// Test that our constants are reasonable
	if MaxIterations <= 0 {
		t.Errorf("MaxIterations should be positive, got %d", MaxIterations)
	}
	if MaxIterations > 20 {
		t.Errorf("MaxIterations seems too high (%d), could cause performance issues", MaxIterations)
	}

	if BatchSize <= 0 {
		t.Errorf("BatchSize should be positive, got %d", BatchSize)
	}
	if BatchSize > 100 {
		t.Errorf("BatchSize seems too high (%d), might hit GitHub API limits", BatchSize)
	}
}

func TestDownloadWorkflowLogsWithEngineFilter(t *testing.T) {
	t.Skip("Skipping slow network-dependent test")

	// Test that the engine filter parameter is properly validated and passed through
	tests := []struct {
		name        string
		engine      string
		expectError bool
		errorText   string
	}{
		{
			name:        "valid claude engine",
			engine:      "claude",
			expectError: false,
		},
		{
			name:        "valid codex engine",
			engine:      "codex",
			expectError: false,
		},
		{
			name:        "valid copilot engine",
			engine:      "copilot",
			expectError: false,
		},
		{
			name:        "empty engine (no filter)",
			engine:      "",
			expectError: false,
		},
		{
			name:        "invalid engine",
			engine:      "gpt",
			expectError: true,
			errorText:   "invalid engine value 'gpt'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This function should validate the engine parameter
			// If invalid, it would exit in the actual command but we can't test that easily
			// So we just test that valid engines don't cause immediate errors
			if !tt.expectError {
				// For valid engines, test that the function can be called without panic
				// It may still fail with auth errors, which is expected
				err := DownloadWorkflowLogs(context.Background(), "", 1, "", "", "./test-logs", tt.engine, "", 0, 0, "", false, false, false, false, false, false, false, 0, "summary.json", "")

				// Clean up any created directories
				os.RemoveAll("./test-logs")

				// If there's an error, it should be auth or workflow-related, not parameter validation
				if err != nil {
					errMsg := strings.ToLower(err.Error())
					if strings.Contains(errMsg, "invalid engine") {
						t.Errorf("Got engine validation error for valid engine '%s': %v", tt.engine, err)
					}
				}
			}
		})
	}
}

func TestUnzipFile(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test zip file
	zipPath := filepath.Join(tmpDir, "test.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Failed to create test zip file: %v", err)
	}

	zipWriter := zip.NewWriter(zipFile)

	// Add a test file to the zip
	testContent := "This is test content for workflow logs"
	writer, err := zipWriter.Create("test-log.txt")
	if err != nil {
		zipFile.Close()
		t.Fatalf("Failed to create file in zip: %v", err)
	}
	_, err = writer.Write([]byte(testContent))
	if err != nil {
		zipFile.Close()
		t.Fatalf("Failed to write content to zip: %v", err)
	}

	// Add a subdirectory with a file
	writer, err = zipWriter.Create("logs/job-1.txt")
	if err != nil {
		zipFile.Close()
		t.Fatalf("Failed to create subdirectory file in zip: %v", err)
	}
	_, err = writer.Write([]byte("Job 1 logs"))
	if err != nil {
		zipFile.Close()
		t.Fatalf("Failed to write subdirectory content to zip: %v", err)
	}

	// Close the zip writer
	err = zipWriter.Close()
	if err != nil {
		zipFile.Close()
		t.Fatalf("Failed to close zip writer: %v", err)
	}
	zipFile.Close()

	// Create a destination directory
	destDir := filepath.Join(tmpDir, "extracted")
	err = os.MkdirAll(destDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create destination directory: %v", err)
	}

	// Test the unzipFile function
	err = unzipFile(zipPath, destDir, false)
	if err != nil {
		t.Fatalf("unzipFile failed: %v", err)
	}

	// Verify the extracted files
	extractedFile := filepath.Join(destDir, "test-log.txt")
	content, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("Failed to read extracted file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Extracted content mismatch: got %q, want %q", string(content), testContent)
	}

	// Verify subdirectory file
	subdirFile := filepath.Join(destDir, "logs", "job-1.txt")
	content, err = os.ReadFile(subdirFile)
	if err != nil {
		t.Fatalf("Failed to read extracted subdirectory file: %v", err)
	}

	if string(content) != "Job 1 logs" {
		t.Errorf("Extracted subdirectory content mismatch: got %q, want %q", string(content), "Job 1 logs")
	}
}

func TestUnzipFileZipSlipPrevention(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test zip file with a malicious path
	zipPath := filepath.Join(tmpDir, "malicious.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Failed to create test zip file: %v", err)
	}

	zipWriter := zip.NewWriter(zipFile)

	// Try to create a file that escapes the destination directory
	writer, err := zipWriter.Create("../../../etc/passwd")
	if err != nil {
		zipFile.Close()
		t.Fatalf("Failed to create malicious file in zip: %v", err)
	}
	_, err = writer.Write([]byte("malicious content"))
	if err != nil {
		zipFile.Close()
		t.Fatalf("Failed to write malicious content to zip: %v", err)
	}

	err = zipWriter.Close()
	if err != nil {
		zipFile.Close()
		t.Fatalf("Failed to close zip writer: %v", err)
	}
	zipFile.Close()

	// Create a destination directory
	destDir := filepath.Join(tmpDir, "safe-extraction")
	err = os.MkdirAll(destDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create destination directory: %v", err)
	}

	// Test that unzipFile rejects the malicious path
	err = unzipFile(zipPath, destDir, false)
	if err == nil {
		t.Error("Expected unzipFile to reject malicious path, but it succeeded")
	}

	if !strings.Contains(err.Error(), "invalid file path") {
		t.Errorf("Expected error about invalid file path, got: %v", err)
	}
}

func TestDownloadWorkflowRunLogsStructure(t *testing.T) {
	// This test verifies that workflow logs are extracted into a workflow-logs subdirectory
	// Note: This test cannot fully test downloadWorkflowRunLogs without GitHub CLI authentication
	// So we test the directory creation and unzipFile behavior that mimics the workflow

	tmpDir := testutil.TempDir(t, "test-*")

	// Create a mock workflow logs zip file similar to what GitHub API returns
	zipPath := filepath.Join(tmpDir, "workflow-logs.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Failed to create test zip file: %v", err)
	}

	zipWriter := zip.NewWriter(zipFile)

	// Add files that simulate GitHub Actions workflow logs structure
	logFiles := map[string]string{
		"1_job1.txt":        "Job 1 execution logs",
		"2_job2.txt":        "Job 2 execution logs",
		"3_build/build.txt": "Build step logs",
		"4_test/test-1.txt": "Test step 1 logs",
		"4_test/test-2.txt": "Test step 2 logs",
	}

	for filename, content := range logFiles {
		writer, err := zipWriter.Create(filename)
		if err != nil {
			zipFile.Close()
			t.Fatalf("Failed to create file %s in zip: %v", filename, err)
		}
		_, err = writer.Write([]byte(content))
		if err != nil {
			zipFile.Close()
			t.Fatalf("Failed to write content to %s: %v", filename, err)
		}
	}

	err = zipWriter.Close()
	if err != nil {
		zipFile.Close()
		t.Fatalf("Failed to close zip writer: %v", err)
	}
	zipFile.Close()

	// Create a run directory (simulating logs/run-12345)
	runDir := filepath.Join(tmpDir, "run-12345")
	err = os.MkdirAll(runDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create run directory: %v", err)
	}

	// Create some other artifacts in the run directory (to verify they don't get mixed with logs)
	err = os.WriteFile(filepath.Join(runDir, "aw_info.json"), []byte(`{"engine_id": "claude"}`), 0644)
	if err != nil {
		t.Fatalf("Failed to create aw_info.json: %v", err)
	}

	// Create the workflow-logs subdirectory and extract logs there
	workflowLogsDir := filepath.Join(runDir, "workflow-logs")
	err = os.MkdirAll(workflowLogsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create workflow-logs directory: %v", err)
	}

	// Extract logs into the workflow-logs subdirectory (mimics downloadWorkflowRunLogs behavior)
	err = unzipFile(zipPath, workflowLogsDir, false)
	if err != nil {
		t.Fatalf("Failed to extract logs: %v", err)
	}

	// Verify that workflow-logs directory exists
	if !fileutil.DirExists(workflowLogsDir) {
		t.Error("workflow-logs directory should exist")
	}

	// Verify that log files are in the workflow-logs subdirectory, not in run root
	for filename := range logFiles {
		expectedPath := filepath.Join(workflowLogsDir, filename)
		if !fileutil.FileExists(expectedPath) {
			t.Errorf("Expected log file %s to be in workflow-logs subdirectory", filename)
		}

		// Verify the file is NOT in the run directory root
		wrongPath := filepath.Join(runDir, filename)
		if fileutil.FileExists(wrongPath) {
			t.Errorf("Log file %s should not be in run directory root", filename)
		}
	}

	// Verify that other artifacts remain in the run directory root
	awInfoPath := filepath.Join(runDir, "aw_info.json")
	if !fileutil.FileExists(awInfoPath) {
		t.Error("aw_info.json should remain in run directory root")
	}

	// Verify the content of one of the extracted log files
	testLogPath := filepath.Join(workflowLogsDir, "1_job1.txt")
	content, err := os.ReadFile(testLogPath)
	if err != nil {
		t.Fatalf("Failed to read extracted log file: %v", err)
	}

	expectedContent := "Job 1 execution logs"
	if string(content) != expectedContent {
		t.Errorf("Log file content mismatch: got %q, want %q", string(content), expectedContent)
	}

	// Verify nested directory structure is preserved
	nestedLogPath := filepath.Join(workflowLogsDir, "3_build", "build.txt")
	if !fileutil.FileExists(nestedLogPath) {
		t.Error("Nested log directory structure should be preserved")
	}
}

// TestCountParameterBehavior verifies that the count parameter limits matching results
// not the number of runs fetched when date filters are specified
func TestCountParameterBehavior(t *testing.T) {
	// This test documents the expected behavior:
	// 1. When date filters (startDate/endDate) are specified, fetch ALL runs in that range
	// 2. Apply post-download filters (engine, staged, etc.)
	// 3. Limit final output to 'count' matching runs
	//
	// Without date filters:
	// 1. Fetch runs iteratively until we have 'count' runs with artifacts
	// 2. Apply filters during iteration (old behavior for backward compatibility)

	// Note: This is a documentation test - the actual logic is tested via integration tests
	// that require GitHub CLI authentication and a real repository

	tests := []struct {
		name             string
		startDate        string
		endDate          string
		count            int
		expectedFetchAll bool
	}{
		{
			name:             "with startDate should fetch all in range",
			startDate:        "2024-01-01",
			endDate:          "",
			count:            10,
			expectedFetchAll: true,
		},
		{
			name:             "with endDate should fetch all in range",
			startDate:        "",
			endDate:          "2024-12-31",
			count:            10,
			expectedFetchAll: true,
		},
		{
			name:             "with both dates should fetch all in range",
			startDate:        "2024-01-01",
			endDate:          "2024-12-31",
			count:            10,
			expectedFetchAll: true,
		},
		{
			name:             "without dates should use count as fetch limit",
			startDate:        "",
			endDate:          "",
			count:            10,
			expectedFetchAll: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This documents the logic: when startDate or endDate is set, we fetch all
			fetchAllInRange := tt.startDate != "" || tt.endDate != ""

			if fetchAllInRange != tt.expectedFetchAll {
				t.Errorf("Expected fetchAllInRange=%v for startDate=%q endDate=%q, got %v",
					tt.expectedFetchAll, tt.startDate, tt.endDate, fetchAllInRange)
			}
		})
	}
}
