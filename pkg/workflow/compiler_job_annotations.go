package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

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
