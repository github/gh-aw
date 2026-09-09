package workflow

import (
	"os"
	"path/filepath"
)

// DisableDefaultActionFailureExpiryMarkersIfUnenforced disables implicit
// action-failure expiry markers in lockFiles when no maintenance workflow can
// be relied on to consume them. lockFiles must be the actual emitted lock
// file paths for the compiled workflows (not reconstructed from workflowDir),
// so that targets outside the default workflow directory are patched
// correctly. repoConfig may be nil.
//
// An explicit maintenance.action_failure_issue_expires in aw.json always
// opts a full directory compile into maintenance workflow generation (see
// IsActionFailureIssueExpiresExplicit), so this function leaves markers
// untouched whenever that is set, regardless of maintenance workflow state.
func DisableDefaultActionFailureExpiryMarkersIfUnenforced(lockFiles []string, workflowDir string, repoConfig *RepoConfig) {
	if repoConfig.IsActionFailureIssueExpiresExplicit() {
		return
	}
	if isActionFailureExpiryEnforced(workflowDir, repoConfig) {
		return
	}
	patchActionFailureExpiryMarkersInFiles(lockFiles)
}

// isActionFailureExpiryEnforced reports whether a maintenance workflow exists
// and is configured to run the close-expired-entities job that consumes the
// action-failure expiry marker. repoConfig may be nil.
func isActionFailureExpiryEnforced(workflowDir string, repoConfig *RepoConfig) bool {
	if repoConfig != nil && repoConfig.MaintenanceDisabled {
		return false
	}
	if _, err := os.Stat(filepath.Join(workflowDir, "agentics-maintenance.yml")); os.IsNotExist(err) {
		return false
	}
	if repoConfig != nil && repoConfig.Maintenance != nil && repoConfig.Maintenance.IsJobDisabled("close-expired-entities") {
		return false
	}
	return true
}
