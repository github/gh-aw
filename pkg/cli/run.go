package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var cancelLog = logger.New("cli:cancel")

func cancelWorkflowRuns(workflowID int64) error {
	cancelLog.Printf("Cancelling workflow runs for workflow ID: %d", workflowID)

	// Start spinner for network operation
	spinner := console.NewSpinner("Cancelling workflow runs...")
	spinner.Start()

	// Get running workflow runs
	cmd := workflow.ExecGH("run", "list", "--workflow", strconv.FormatInt(workflowID, 10), "--status", "in_progress", "--json", "databaseId")
	output, err := cmd.Output()
	if err != nil {
		cancelLog.Printf("Failed to list workflow runs: %v", err)
		spinner.Stop()
		return err
	}

	var runs []struct {
		DatabaseID int64 `json:"databaseId"`
	}
	if err := json.Unmarshal(output, &runs); err != nil {
		cancelLog.Printf("Failed to parse workflow runs JSON: %v", err)
		spinner.Stop()
		return err
	}

	cancelLog.Printf("Found %d in-progress workflow runs to cancel", len(runs))

	// Cancel each running workflow
	totalRuns := len(runs)
	for i, run := range runs {
		cancelLog.Printf("Cancelling workflow run: %d", run.DatabaseID)
		cancelCmd := workflow.ExecGH("run", "cancel", strconv.FormatInt(run.DatabaseID, 10))
		_ = cancelCmd.Run() // Ignore errors for individual cancellations
		// Update spinner with progress after cancellation completes
		spinner.UpdateMessage(fmt.Sprintf("Cancelling workflow runs... (%d/%d completed)", i+1, totalRuns))
	}

	if len(runs) > 0 {
		spinner.StopWithMessage(fmt.Sprintf("✓ Cancelled %d workflow runs", len(runs)))
	} else {
		spinner.StopWithMessage("✓ No in-progress workflow runs to cancel")
	}
	cancelLog.Print("Workflow run cancellation completed")
	return nil
}

// cancelWorkflowRunsByLockFile cancels in-progress runs for a workflow identified by its lock file name
func cancelWorkflowRunsByLockFile(lockFileName string) error {
	cancelLog.Printf("Cancelling workflow runs for lock file: %s", lockFileName)

	// Start spinner for network operation
	spinner := console.NewSpinner("Cancelling workflow runs...")
	spinner.Start()

	// Get running workflow runs by lock file name
	cmd := workflow.ExecGH("run", "list", "--workflow", lockFileName, "--status", "in_progress", "--json", "databaseId")
	output, err := cmd.Output()
	if err != nil {
		cancelLog.Printf("Failed to list workflow runs by lock file: %v", err)
		spinner.Stop()
		return err
	}

	var runs []struct {
		DatabaseID int64 `json:"databaseId"`
	}
	if err := json.Unmarshal(output, &runs); err != nil {
		cancelLog.Printf("Failed to parse workflow runs JSON: %v", err)
		spinner.Stop()
		return err
	}

	cancelLog.Printf("Found %d in-progress workflow runs to cancel", len(runs))

	// Cancel each running workflow
	totalRuns := len(runs)
	for i, run := range runs {
		cancelLog.Printf("Cancelling workflow run: %d", run.DatabaseID)
		cancelCmd := workflow.ExecGH("run", "cancel", strconv.FormatInt(run.DatabaseID, 10))
		_ = cancelCmd.Run() // Ignore errors for individual cancellations
		// Update spinner with progress after cancellation completes
		spinner.UpdateMessage(fmt.Sprintf("Cancelling workflow runs... (%d/%d completed)", i+1, totalRuns))
	}

	if len(runs) > 0 {
		spinner.StopWithMessage(fmt.Sprintf("✓ Cancelled %d workflow runs", len(runs)))
	} else {
		spinner.StopWithMessage("✓ No in-progress workflow runs to cancel")
	}
	cancelLog.Print("Workflow run cancellation completed")
	return nil
}
