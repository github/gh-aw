package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

var generatedJobNames = map[string]string{
	string(constants.PreActivationJobName):      "Pre-activation",
	string(constants.ActivationJobName):         "Activation",
	string(constants.AgentJobName):              "Agent",
	string(constants.DetectionJobName):          "Detection",
	string(constants.SafeOutputsJobName):        "Safe outputs",
	string(constants.ConclusionJobName):         "Conclusion",
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
	"check_token_telemetry":                     "Check token telemetry",
	"send_slack_message":                        "Send Slack message",
}

// annotateGeneratedJobs adds human-readable names and explains the job-scoped
// permissions after all compiler and author permission augmentations have run.
func (c *Compiler) annotateGeneratedJobs(data *WorkflowData) {
	if data == nil {
		return
	}
	sourceComments := sourceJobPermissionComments(data.FrontmatterYAML)
	for id, job := range c.jobManager.GetAllJobs() {
		if displayName, generated := generatedJobNames[id]; generated && job.DisplayName == "" {
			job.DisplayName = displayName
		}
		if job.Permissions == "" {
			continue
		}
		_, generated := generatedJobNames[id]
		if job.PermissionsComment == "" && (generated || len(sourceComments[id]) == 0) {
			job.PermissionsComment = fmt.Sprintf("# Permissions for the %s job (workflow permissions default to none).", id)
		}
		if id == string(constants.PreActivationJobName) && data.OnPermissions != nil {
			job.PermissionsComment = strings.Join([]string{job.PermissionsComment, "# Includes permissions declared in on.permissions."}, "\n")
		}
		if comments := sourceComments[id]; len(comments) > 0 {
			job.PermissionsComment = strings.TrimPrefix(strings.Join([]string{job.PermissionsComment, strings.Join(comments, "\n")}, "\n"), "\n")
		}
	}
}

// sourceJobPermissionComments keeps author comments attached to permissions
// when job permissions are parsed, merged, and rendered in canonical order.
// Comments directly before or inside on.permissions or
// jobs.<id>.permissions are copied.
func sourceJobPermissionComments(frontmatter string) map[string][]string { //nolint:largefunc // Tracks YAML nesting and attached comments in a single ordered pass.
	result := make(map[string][]string)
	section, jobName, inPermissions := "", "", false
	onIndent, jobIndent, permissionIndent := 0, 0, 0
	var precedingComments []string
	for line := range strings.SplitSeq(frontmatter, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			precedingComments = nil
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if strings.HasPrefix(trimmed, "#") {
			if inPermissions && indent > permissionIndent {
				id := jobName
				if section == "on" {
					id = string(constants.PreActivationJobName)
				}
				result[id] = append(result[id], trimmed)
			} else if (section == "on" || (section == "jobs" && jobName != "")) && (indent == permissionIndent || permissionIndent == 0 && indent > jobIndent) {
				precedingComments = append(precedingComments, trimmed)
			} else {
				precedingComments = nil
			}
			continue
		}
		if indent == 0 {
			section = strings.TrimSuffix(trimmed, ":")
			jobName, inPermissions = "", false
			onIndent, jobIndent, permissionIndent = 0, 0, 0
			precedingComments = nil
			continue
		}
		if section == "on" && onIndent == 0 {
			onIndent = indent
			permissionIndent = indent
		}
		if section == "jobs" && jobIndent == 0 {
			jobIndent = indent
		}
		if section == "jobs" && indent == jobIndent {
			jobName, inPermissions = strings.TrimSuffix(trimmed, ":"), false
			permissionIndent = 0
			precedingComments = nil
			continue
		}
		if section == "jobs" && jobName != "" && permissionIndent == 0 && indent > jobIndent {
			permissionIndent = indent
		}
		if indent == permissionIndent {
			inPermissions = (section == "on" || section == "jobs") && (trimmed == "permissions:" || strings.HasPrefix(trimmed, "permissions: #"))
			if inPermissions {
				id := jobName
				if section == "on" {
					id = string(constants.PreActivationJobName)
				}
				result[id] = append(result[id], precedingComments...)
				if _, comment, ok := strings.Cut(trimmed, "#"); ok {
					result[id] = append(result[id], "#"+comment)
				}
			}
			precedingComments = nil
			continue
		}
		precedingComments = nil
		if !inPermissions || indent <= permissionIndent {
			continue
		}
		// Permission values are simple read/write/none scalars; only copy
		// trailing comments from those lines, not '#' within YAML strings.
		if _, after, ok := strings.Cut(trimmed, ":"); ok {
			value := strings.TrimSpace(after)
			for _, level := range []string{"read", "write", "none"} {
				if comment, found := strings.CutPrefix(value, level+" #"); found {
					id := jobName
					if section == "on" {
						id = string(constants.PreActivationJobName)
					}
					result[id] = append(result[id], "#"+comment)
					break
				}
			}
		}
	}
	return result
}
