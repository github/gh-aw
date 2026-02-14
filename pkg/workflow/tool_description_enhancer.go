package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var toolDescriptionEnhancerLog = logger.New("workflow:tool_description_enhancer")

// enhanceToolDescription adds configuration-specific constraints to tool descriptions
// This provides agents with context about limits and restrictions configured in the workflow
func enhanceToolDescription(toolName, baseDescription string, safeOutputs *SafeOutputsConfig) string {
	toolDescriptionEnhancerLog.Printf("Enhancing tool description: tool=%s", toolName)

	if safeOutputs == nil {
		return baseDescription
	}

	var constraints []string

	switch toolName {
	case "create_issue":
		if config := safeOutputs.CreateIssues; config != nil {
			toolDescriptionEnhancerLog.Printf("Found create_issue config: max=%d, titlePrefix=%s", config.Max, config.TitlePrefix)
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d issue(s) can be created.", config.Max))
			}
			if config.TitlePrefix != "" {
				constraints = append(constraints, fmt.Sprintf("Title will be prefixed with %q.", config.TitlePrefix))
			}
			if len(config.Labels) > 0 {
				constraints = append(constraints, fmt.Sprintf("Labels %v will be automatically added.", config.Labels))
			}
			if len(config.AllowedLabels) > 0 {
				constraints = append(constraints, fmt.Sprintf("Only these labels are allowed: %v.", config.AllowedLabels))
			}
			if len(config.Assignees) > 0 {
				constraints = append(constraints, fmt.Sprintf("Assignees %v will be automatically assigned.", config.Assignees))
			}
			if config.TargetRepoSlug != "" {
				constraints = append(constraints, fmt.Sprintf("Issues will be created in repository %q.", config.TargetRepoSlug))
			}
		}

	case "create_agent_session":
		if config := safeOutputs.CreateAgentSessions; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d agent task(s) can be created.", config.Max))
			}
			if config.Base != "" {
				constraints = append(constraints, fmt.Sprintf("Base branch for tasks: %q.", config.Base))
			}
			if config.TargetRepoSlug != "" {
				constraints = append(constraints, fmt.Sprintf("Tasks will be created in repository %q.", config.TargetRepoSlug))
			}
		}

	case "create_discussion":
		if config := safeOutputs.CreateDiscussions; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d discussion(s) can be created.", config.Max))
			}
			if config.TitlePrefix != "" {
				constraints = append(constraints, fmt.Sprintf("Title will be prefixed with %q.", config.TitlePrefix))
			}
			if config.Category != "" {
				constraints = append(constraints, fmt.Sprintf("Discussions will be created in category %q.", config.Category))
			}
			if len(config.AllowedLabels) > 0 {
				constraints = append(constraints, fmt.Sprintf("Only these labels are allowed: %v.", config.AllowedLabels))
			}
			if config.TargetRepoSlug != "" {
				constraints = append(constraints, fmt.Sprintf("Discussions will be created in repository %q.", config.TargetRepoSlug))
			}
		}

	case "close_discussion":
		if config := safeOutputs.CloseDiscussions; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d discussion(s) can be closed.", config.Max))
			}
			if config.Target != "" {
				constraints = append(constraints, fmt.Sprintf("Target: %s.", config.Target))
			}
		}

	case "close_issue":
		if config := safeOutputs.CloseIssues; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d issue(s) can be closed.", config.Max))
			}
			if config.Target != "" {
				constraints = append(constraints, fmt.Sprintf("Target: %s.", config.Target))
			}
		}

	case "close_pull_request":
		if config := safeOutputs.ClosePullRequests; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d pull request(s) can be closed.", config.Max))
			}
			if config.Target != "" {
				constraints = append(constraints, fmt.Sprintf("Target: %s.", config.Target))
			}
			if len(config.RequiredLabels) > 0 {
				constraints = append(constraints, fmt.Sprintf("Only PRs with labels %v can be closed.", config.RequiredLabels))
			}
			if config.RequiredTitlePrefix != "" {
				constraints = append(constraints, fmt.Sprintf("Only PRs with title prefix %q can be closed.", config.RequiredTitlePrefix))
			}
		}

	case "add_comment":
		if config := safeOutputs.AddComments; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d comment(s) can be added.", config.Max))
			}
			if config.Target != "" {
				constraints = append(constraints, fmt.Sprintf("Target: %s.", config.Target))
			}
			if config.TargetRepoSlug != "" {
				constraints = append(constraints, fmt.Sprintf("Comments will be added in repository %q.", config.TargetRepoSlug))
			}
		}

	case "create_pull_request":
		if config := safeOutputs.CreatePullRequests; config != nil {
			toolDescriptionEnhancerLog.Printf("Found create_pull_request config: max=%d, titlePrefix=%s, draft=%v", config.Max, config.TitlePrefix, config.Draft)
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d pull request(s) can be created.", config.Max))
			}
			if config.TitlePrefix != "" {
				constraints = append(constraints, fmt.Sprintf("Title will be prefixed with %q.", config.TitlePrefix))
			}
			if len(config.Labels) > 0 {
				constraints = append(constraints, fmt.Sprintf("Labels %v will be automatically added.", config.Labels))
			}
			if len(config.AllowedLabels) > 0 {
				constraints = append(constraints, fmt.Sprintf("Only these labels are allowed: %v.", config.AllowedLabels))
			}
			if config.Draft != nil && *config.Draft {
				constraints = append(constraints, "PRs will be created as drafts.")
			}
			if len(config.Reviewers) > 0 {
				constraints = append(constraints, fmt.Sprintf("Reviewers %v will be assigned.", config.Reviewers))
			}
		}

	case "create_pull_request_review_comment":
		if config := safeOutputs.CreatePullRequestReviewComments; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d review comment(s) can be created.", config.Max))
			}
			if config.Side != "" {
				constraints = append(constraints, fmt.Sprintf("Comments will be on the %s side of the diff.", config.Side))
			}
		}

	case "submit_pull_request_review":
		if config := safeOutputs.SubmitPullRequestReview; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d review(s) can be submitted.", config.Max))
			}
		}

	case "resolve_pull_request_review_thread":
		if config := safeOutputs.ResolvePullRequestReviewThread; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d review thread(s) can be resolved.", config.Max))
			}
		}

	case "create_code_scanning_alert":
		if config := safeOutputs.CreateCodeScanningAlerts; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d alert(s) can be created.", config.Max))
			}
		}

	case "add_labels":
		if config := safeOutputs.AddLabels; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d label(s) can be added.", config.Max))
			}
			if len(config.Allowed) > 0 {
				constraints = append(constraints, fmt.Sprintf("Only these labels are allowed: %v.", config.Allowed))
			}
			if config.Target != "" {
				constraints = append(constraints, fmt.Sprintf("Target: %s.", config.Target))
			}
		}

	case "remove_labels":
		if config := safeOutputs.RemoveLabels; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d label(s) can be removed.", config.Max))
			}
			if len(config.Allowed) > 0 {
				constraints = append(constraints, fmt.Sprintf("Only these labels can be removed: %v.", config.Allowed))
			}
			if config.Target != "" {
				constraints = append(constraints, fmt.Sprintf("Target: %s.", config.Target))
			}
		}

	case "add_reviewer":
		if config := safeOutputs.AddReviewer; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d reviewer(s) can be added.", config.Max))
			}
		}

	case "update_issue":
		if config := safeOutputs.UpdateIssues; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d issue(s) can be updated.", config.Max))
			}
			if config.Target != "" {
				constraints = append(constraints, fmt.Sprintf("Target: %s.", config.Target))
			}
			if config.Title != nil && *config.Title {
				constraints = append(constraints, "Title updates are allowed.")
			}
			if config.Body != nil && *config.Body {
				constraints = append(constraints, "Body updates are allowed.")
			}
			if config.Status != nil && *config.Status {
				constraints = append(constraints, "Status updates (open/closed) are allowed.")
			}
		}

	case "update_pull_request":
		if config := safeOutputs.UpdatePullRequests; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d pull request(s) can be updated.", config.Max))
			}
			if config.Target != "" {
				constraints = append(constraints, fmt.Sprintf("Target: %s.", config.Target))
			}
		}

	case "push_to_pull_request_branch":
		if config := safeOutputs.PushToPullRequestBranch; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d push(es) can be made.", config.Max))
			}
		}

	case "upload_asset":
		if config := safeOutputs.UploadAssets; config != nil {
			toolDescriptionEnhancerLog.Printf("Found upload_asset config: max=%d, maxSizeKB=%d, allowedExts=%v", config.Max, config.MaxSizeKB, config.AllowedExts)
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d asset(s) can be uploaded.", config.Max))
			}
			if config.MaxSizeKB > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum file size: %dKB.", config.MaxSizeKB))
			}
			if len(config.AllowedExts) > 0 {
				constraints = append(constraints, fmt.Sprintf("Allowed file extensions: %v.", config.AllowedExts))
			}
		}

	case "update_release":
		if config := safeOutputs.UpdateRelease; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d release(s) can be updated.", config.Max))
			}
		}

	case "missing_tool":
		if config := safeOutputs.MissingTool; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d missing tool report(s) can be created.", config.Max))
			}
		}

	case "link_sub_issue":
		if config := safeOutputs.LinkSubIssue; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d sub-issue link(s) can be created.", config.Max))
			}
		}

	case "assign_milestone":
		if config := safeOutputs.AssignMilestone; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d milestone assignment(s) can be made.", config.Max))
			}
		}

	case "assign_to_agent":
		if config := safeOutputs.AssignToAgent; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d issue(s) can be assigned to agent.", config.Max))
			}
		}

	case "update_project":
		if config := safeOutputs.UpdateProjects; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d project operation(s) can be performed.", config.Max))
			}
			if config.Project != "" {
				constraints = append(constraints, fmt.Sprintf("Default project URL: %q.", config.Project))
			}
		}

	case "create_project_status_update":
		if config := safeOutputs.CreateProjectStatusUpdates; config != nil {
			if config.Max > 0 {
				constraints = append(constraints, fmt.Sprintf("Maximum %d status update(s) can be created.", config.Max))
			}
			if config.Project != "" {
				constraints = append(constraints, fmt.Sprintf("Default project URL: %q.", config.Project))
			}
		}

	case "noop":
		// noop has no configurable constraints
	}

	if len(constraints) == 0 {
		toolDescriptionEnhancerLog.Printf("No constraints found for tool: %s", toolName)
		return baseDescription
	}

	toolDescriptionEnhancerLog.Printf("Added %d constraints to tool description: tool=%s", len(constraints), toolName)
	// Add constraints as a new paragraph at the end of the description
	return baseDescription + " CONSTRAINTS: " + strings.Join(constraints, " ")
}
