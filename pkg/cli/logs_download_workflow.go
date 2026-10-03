package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"
)

const workflowRunLogsMarker = ".complete"
const workflowRunLogsMarkerVersion = "coordinator-step-logs/v1\n"

func workflowRunLogsComplete(runDir string) bool {
	data, err := os.ReadFile(filepath.Join(runDir, "workflow-logs", workflowRunLogsMarker))
	return err == nil && string(data) == workflowRunLogsMarkerVersion
}

// Publish extracted logs only after every archive entry and the completion
// marker are written successfully. Failed downloads preserve existing logs.
func storeWorkflowRunLogsArchive(data []byte, outputDir string, verbose bool) error {
	if err := os.MkdirAll(outputDir, constants.DirPermSensitive); err != nil {
		return fmt.Errorf("failed to create workflow logs output directory: %w", err)
	}
	stagingDir, err := os.MkdirTemp(outputDir, ".workflow-logs-")
	if err != nil {
		return fmt.Errorf("failed to stage workflow logs: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	archivePath := filepath.Join(stagingDir, "archive.zip")
	if err := os.WriteFile(archivePath, data, constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write workflow logs archive: %w", err)
	}
	logsDir := filepath.Join(stagingDir, "logs")
	if err := os.Mkdir(logsDir, constants.DirPermSensitive); err != nil {
		return fmt.Errorf("failed to create workflow logs staging directory: %w", err)
	}
	if err := unzipFile(archivePath, logsDir, verbose); err != nil {
		return fmt.Errorf("failed to unzip workflow logs: %w", err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, workflowRunLogsMarker), []byte(workflowRunLogsMarkerVersion), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to mark workflow logs complete: %w", err)
	}
	dest := filepath.Join(outputDir, "workflow-logs")
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("failed to replace workflow logs: %w", err)
	}
	if err := os.Rename(logsDir, dest); err != nil {
		return fmt.Errorf("failed to publish workflow logs: %w", err)
	}
	return nil
}
