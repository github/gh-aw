package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

var generatedJobNames = map[string]string{
	string(constants.AdmissionJobName):          "Admission",
	string(constants.PreActivationJobName):      "Pre-activation",
	string(constants.ActivationJobName):         "Activation",
	string(constants.AgentJobName):              "Agent",
	string(constants.DetectionJobName):          "Detection",
	string(constants.SafeOutputsJobName):        "Safe outputs",
	string(constants.ConclusionJobName):         "Conclusion",
	string(constants.GradersJobName):            "Graders",
	string(constants.EvalsJobName):              "Evaluations",
	string(constants.UploadAssetsJobName):       "Upload assets",
	string(constants.UploadCodeScanningJobName): "Upload code scanning results",
	string(constants.UploadCodeCoverageJobName): "Upload code coverage",
	string(constants.UnlockJobName):             "Unlock",
	"push_evals_state":                          "Push evaluations state",
	"push_experiments_state":                    "Push experiments state",
	"push_repo_memory":                          "Push repository memory",
	"push_ledger_changes":                       "Push ledger changes",
	"update_cache_memory":                       "Update cache memory",
	updateDriveMemoryJobName:                    "Update drive memory",
	"check_token_telemetry":                     "Check token telemetry",
	"send_slack_message":                        "Send Slack message",
}

// annotateGeneratedJobs gives unnamed jobs their ID as a display name and explains
// job-scoped permissions after all compiler and author augmentations have run.
func (c *Compiler) annotateGeneratedJobs(data *WorkflowData) {
	if data == nil {
		return
	}
	for id, job := range c.jobManager.GetAllJobs() {
		if job.DisplayName == "" {
			job.DisplayName = id
		}
		if job.Permissions == "" {
			continue
		}
		if job.PermissionsComment == "" {
			job.PermissionsComment = fmt.Sprintf("# Permissions for the %s job (workflow permissions default to none).", id)
		}
		if id == string(constants.PreActivationJobName) && data.OnPermissions != nil {
			job.PermissionsComment = strings.Join([]string{job.PermissionsComment, "# Includes permissions declared in on.permissions."}, "\n")
		}
	}
}
