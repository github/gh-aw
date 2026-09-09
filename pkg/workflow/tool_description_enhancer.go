package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var toolDescriptionEnhancerLog = logger.New("workflow:tool_description_enhancer")

type toolConstraintBuilder func(*SafeOutputsConfig) []string

var toolConstraintBuilders = map[string]toolConstraintBuilder{
	"ado_create_work_item": func(safeOutputs *SafeOutputsConfig) []string {
		return createWorkItemConstraints(safeOutputs.CreateWorkItems)
	},
	"ado_update_work_item": func(safeOutputs *SafeOutputsConfig) []string {
		return updateWorkItemConstraints(safeOutputs.UpdateWorkItems)
	},
	"ado_comment_on_work_item": func(safeOutputs *SafeOutputsConfig) []string {
		return commentOnWorkItemConstraints(safeOutputs.CommentOnWorkItems)
	},
	"ado_assign_work_item": func(safeOutputs *SafeOutputsConfig) []string {
		return assignWorkItemConstraints(safeOutputs.AssignWorkItems)
	},
	"ado_link_work_items": func(safeOutputs *SafeOutputsConfig) []string {
		return linkWorkItemsConstraints(safeOutputs.LinkWorkItems)
	},
	"ado_upload_workitem_attachment": func(safeOutputs *SafeOutputsConfig) []string {
		return uploadWorkItemAttachmentConstraints(safeOutputs.UploadWorkItemAttachments)
	},
	"create_issue": func(safeOutputs *SafeOutputsConfig) []string { return createIssueConstraints(safeOutputs.CreateIssues) },
	"set_issue_field": func(safeOutputs *SafeOutputsConfig) []string {
		return setIssueFieldConstraints(safeOutputs.SetIssueField)
	},
	"create_agent_session": func(safeOutputs *SafeOutputsConfig) []string {
		return createAgentSessionConstraints(safeOutputs.CreateAgentSessions)
	},
	"create_discussion": func(safeOutputs *SafeOutputsConfig) []string {
		return createDiscussionConstraints(safeOutputs.CreateDiscussions)
	},
	"close_discussion": func(safeOutputs *SafeOutputsConfig) []string {
		return closeDiscussionConstraints(safeOutputs.CloseDiscussions)
	},
	"update_discussion": func(safeOutputs *SafeOutputsConfig) []string {
		return updateDiscussionConstraints(safeOutputs.UpdateDiscussions)
	},
	"close_issue": func(safeOutputs *SafeOutputsConfig) []string { return closeIssueConstraints(safeOutputs.CloseIssues) },
	"close_pull_request": func(safeOutputs *SafeOutputsConfig) []string {
		return closePullRequestConstraints(safeOutputs.ClosePullRequests)
	},
	"mark_pull_request_as_ready_for_review": func(safeOutputs *SafeOutputsConfig) []string {
		return markPullRequestAsReadyForReviewConstraints(safeOutputs.MarkPullRequestAsReadyForReview)
	},
	"add_comment": func(safeOutputs *SafeOutputsConfig) []string { return addCommentConstraints(safeOutputs.AddComments) },
	"create_pull_request": func(safeOutputs *SafeOutputsConfig) []string {
		return createPullRequestConstraints(safeOutputs.CreatePullRequests)
	},
	"create_pull_request_review_comment": func(safeOutputs *SafeOutputsConfig) []string {
		return createPullRequestReviewCommentConstraints(safeOutputs.CreatePullRequestReviewComments)
	},
	"submit_pull_request_review": func(safeOutputs *SafeOutputsConfig) []string {
		return submitPullRequestReviewConstraints(safeOutputs.SubmitPullRequestReview)
	},
	"reply_to_pull_request_review_comment": func(safeOutputs *SafeOutputsConfig) []string {
		return replyToPullRequestReviewCommentConstraints(safeOutputs.ReplyToPullRequestReviewComment)
	},
	"dismiss_pull_request_review": func(safeOutputs *SafeOutputsConfig) []string {
		return dismissPullRequestReviewConstraints(safeOutputs.DismissPullRequestReview)
	},
	"resolve_pull_request_review_thread": func(safeOutputs *SafeOutputsConfig) []string {
		return resolvePullRequestReviewThreadConstraints(safeOutputs.ResolvePullRequestReviewThread)
	},
	"create_code_scanning_alert": func(safeOutputs *SafeOutputsConfig) []string {
		return createCodeScanningAlertConstraints(safeOutputs.CreateCodeScanningAlerts)
	},
	"create_check_run": func(safeOutputs *SafeOutputsConfig) []string {
		return createCheckRunConstraints(safeOutputs.CreateCheckRun)
	},
	"add_labels": func(safeOutputs *SafeOutputsConfig) []string { return addLabelsConstraints(safeOutputs.AddLabels) },
	"remove_labels": func(safeOutputs *SafeOutputsConfig) []string {
		return removeLabelsConstraints(safeOutputs.RemoveLabels)
	},
	"replace_label": func(safeOutputs *SafeOutputsConfig) []string {
		return replaceLabelConstraints(safeOutputs.ReplaceLabel)
	},
	"add_reviewer": func(safeOutputs *SafeOutputsConfig) []string { return addReviewerConstraints(safeOutputs.AddReviewer) },
	"update_issue": func(safeOutputs *SafeOutputsConfig) []string { return updateIssueConstraints(safeOutputs.UpdateIssues) },
	"update_pull_request": func(safeOutputs *SafeOutputsConfig) []string {
		return updatePullRequestConstraints(safeOutputs.UpdatePullRequests)
	},
	"push_to_pull_request_branch": func(safeOutputs *SafeOutputsConfig) []string {
		return pushToPullRequestBranchConstraints(safeOutputs.PushToPullRequestBranch)
	},
	"upload_asset": func(safeOutputs *SafeOutputsConfig) []string { return uploadAssetConstraints(safeOutputs.UploadAssets) },
	"update_release": func(safeOutputs *SafeOutputsConfig) []string {
		return updateReleaseConstraints(safeOutputs.UpdateRelease)
	},
	"missing_tool": func(safeOutputs *SafeOutputsConfig) []string { return missingToolConstraints(safeOutputs.MissingTool) },
	"link_sub_issue": func(safeOutputs *SafeOutputsConfig) []string {
		return linkSubIssueConstraints(safeOutputs.LinkSubIssue)
	},
	"assign_milestone": func(safeOutputs *SafeOutputsConfig) []string {
		return assignMilestoneConstraints(safeOutputs.AssignMilestone)
	},
	"assign_to_agent": func(safeOutputs *SafeOutputsConfig) []string {
		return assignToAgentConstraints(safeOutputs.AssignToAgent)
	},
	"update_project": func(safeOutputs *SafeOutputsConfig) []string {
		return updateProjectConstraints(safeOutputs.UpdateProjects)
	},
	"create_project_status_update": func(safeOutputs *SafeOutputsConfig) []string {
		return createProjectStatusUpdateConstraints(safeOutputs.CreateProjectStatusUpdates)
	},
}

// formatStringList formats a slice of strings with proper quoting for readability
// Example: ["bug", "feature request", "docs"] -> ["bug" "feature request" "docs"]
func formatStringList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = fmt.Sprintf("%q", item)
	}
	return "[" + strings.Join(quoted, " ") + "]"
}

func appendAllowedIssueFieldsConstraint(constraints *[]string, allowedFields []string) {
	if len(allowedFields) == 0 {
		return
	}
	if slices.Contains(allowedFields, "*") {
		*constraints = append(*constraints, "Any issue field is allowed.")
		return
	}
	*constraints = append(*constraints, fmt.Sprintf("Only these issue fields are allowed: %s.", formatStringList(allowedFields)))
}

func appendMaxConstraint(constraints *[]string, max *string, format string) {
	if templatableIntValue(max) > 0 {
		*constraints = append(*constraints, fmt.Sprintf(format, templatableIntValue(max)))
	}
}

// buildConstraints centralizes the nil-guard + slice setup boilerplate shared by
// every per-tool constraint builder: it returns nil when config is nil, otherwise
// it hands a fresh constraints slice to build for population.
func buildConstraints[T any](config *T, build func(config *T, constraints *[]string)) []string {
	if config == nil {
		return nil
	}
	var constraints []string
	build(config, &constraints)
	return constraints
}

// appendStringConstraint appends a formatted constraint when value is non-empty.
// format must contain a single %q/%s verb for the value; each call site stays
// self-descriptive through its format string.
func appendStringConstraint(constraints *[]string, value, format string) {
	if value != "" {
		*constraints = append(*constraints, fmt.Sprintf(format, value))
	}
}

// appendTargetConstraint appends the common "Target: <value>." constraint when target is set.
func appendTargetConstraint(constraints *[]string, target string) {
	appendStringConstraint(constraints, target, "Target: %s.")
}

// enhanceToolDescription adds configuration-specific constraints to tool descriptions
// This provides agents with context about limits and restrictions configured in the workflow
func enhanceToolDescription(toolName, baseDescription string, safeOutputs *SafeOutputsConfig) string {
	toolDescriptionEnhancerLog.Printf("Enhancing tool description: tool=%s", toolName)

	if safeOutputs == nil {
		return baseDescription
	}

	constraints := buildToolDescriptionConstraints(toolName, safeOutputs)

	if len(constraints) == 0 {
		toolDescriptionEnhancerLog.Printf("No constraints found for tool: %s", toolName)
		return baseDescription
	}

	toolDescriptionEnhancerLog.Printf("Added %d constraints to tool description: tool=%s", len(constraints), toolName)
	// Add constraints as a new paragraph at the end of the description
	return baseDescription + " CONSTRAINTS: " + strings.Join(constraints, " ")
}

func buildToolDescriptionConstraints(toolName string, safeOutputs *SafeOutputsConfig) []string {
	builder, ok := toolConstraintBuilders[toolName]
	if !ok {
		return nil
	}
	return builder(safeOutputs)
}

func createIssueConstraints(config *CreateIssuesConfig) []string {
	return buildConstraints(config, func(config *CreateIssuesConfig, constraints *[]string) {
		toolDescriptionEnhancerLog.Printf("Found create_issue config: max=%v, titlePrefix=%s", config.Max, config.TitlePrefix)

		appendMaxConstraint(constraints, config.Max, "Maximum %d issue(s) can be created.")
		appendStringConstraint(constraints, config.TitlePrefix, "Title will be prefixed with %q.")
		if len(config.Labels) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Labels %s will be automatically added.", formatStringList(config.Labels)))
		}
		if len(config.AllowedLabels) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these labels are allowed: %s.", formatStringList(config.AllowedLabels)))
		}
		appendAllowedIssueFieldsConstraint(constraints, config.AllowedFields)
		if len(config.Assignees) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Assignees %s will be automatically assigned.", formatStringList(config.Assignees)))
		}
		appendStringConstraint(constraints, config.TargetRepoSlug, "Issues will be created in repository %q.")
		if config.RequireTemporaryID {
			*constraints = append(*constraints, "temporary_id is required.")
		}
		if config.NormalizeClosingKeywords != nil && *config.NormalizeClosingKeywords {
			*constraints = append(*constraints, "Backtick-wrapped issue-closing keyword references (e.g. `Closes #1`) in the body field will be automatically normalized to plain text.")
		}
	})
}

func setIssueFieldConstraints(config *SetIssueFieldConfig) []string {
	return buildConstraints(config, func(config *SetIssueFieldConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d issue field update(s) can be made.")
		appendAllowedIssueFieldsConstraint(constraints, config.AllowedFields)
		appendStringConstraint(constraints, config.TargetRepoSlug, "Issue fields will be updated in repository %q.")
	})
}

func createAgentSessionConstraints(config *CreateAgentSessionConfig) []string {
	return buildConstraints(config, func(config *CreateAgentSessionConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d agent task(s) can be created.")
		appendStringConstraint(constraints, config.Base, "Base branch for tasks: %q.")
		appendStringConstraint(constraints, config.TargetRepoSlug, "Tasks will be created in repository %q.")
		if len(config.AllowedRepos) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Sessions can target these repositories: %v.", config.AllowedRepos))
		}
	})
}

func createDiscussionConstraints(config *CreateDiscussionsConfig) []string {
	return buildConstraints(config, func(config *CreateDiscussionsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d discussion(s) can be created.")
		appendStringConstraint(constraints, config.TitlePrefix, "Title will be prefixed with %q.")
		appendStringConstraint(constraints, config.Category, "Discussions will be created in category %q.")
		if len(config.AllowedLabels) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these labels are allowed: %s.", formatStringList(config.AllowedLabels)))
		}
		appendStringConstraint(constraints, config.TargetRepoSlug, "Discussions will be created in repository %q.")
	})
}

func closeDiscussionConstraints(config *CloseDiscussionsConfig) []string {
	return buildConstraints(config, func(config *CloseDiscussionsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d discussion(s) can be closed.")
		appendTargetConstraint(constraints, config.Target)
		appendStringConstraint(constraints, config.TargetRepoSlug, "Discussions will be closed in repository %q.")
		appendStringConstraint(constraints, config.RequiredTitlePrefix, "Only discussions with title prefix %q can be closed.")
		if config.AllowBody != nil && !*config.AllowBody {
			*constraints = append(*constraints, "Closing comments are disabled: do not include a body field.")
		}
	})
}

func updateDiscussionConstraints(config *UpdateDiscussionsConfig) []string {
	return buildConstraints(config, func(config *UpdateDiscussionsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d discussion(s) can be updated.")
		appendTargetConstraint(constraints, config.Target)
		if config.Title != nil && *config.Title {
			*constraints = append(*constraints, "Title updates are allowed.")
		}
		if config.Body != nil && *config.Body {
			*constraints = append(*constraints, "Body updates are allowed.")
		}
		if config.Labels != nil {
			if len(config.AllowedLabels) > 0 {
				*constraints = append(*constraints, fmt.Sprintf("Only these labels are allowed: %s.", formatStringList(config.AllowedLabels)))
			} else {
				*constraints = append(*constraints, "Label updates are allowed.")
			}
		}
	})
}

func closeIssueConstraints(config *CloseIssuesConfig) []string {
	return buildConstraints(config, func(config *CloseIssuesConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d issue(s) can be closed.")
		appendTargetConstraint(constraints, config.Target)
		appendStringConstraint(constraints, config.RequiredTitlePrefix, "Only issues with title prefix %q can be closed.")
		if config.AllowBody != nil && !*config.AllowBody {
			*constraints = append(*constraints, "Closing comments are disabled: do not include a body field.")
		}
	})
}

func closePullRequestConstraints(config *ClosePullRequestsConfig) []string {
	return buildConstraints(config, func(config *ClosePullRequestsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d pull request(s) can be closed.")
		appendTargetConstraint(constraints, config.Target)
		appendStringConstraint(constraints, config.TargetRepoSlug, "Pull requests will be closed in repository %q.")
		if len(config.RequiredLabels) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only PRs with labels %v can be closed.", config.RequiredLabels))
		}
		appendStringConstraint(constraints, config.RequiredTitlePrefix, "Only PRs with title prefix %q can be closed.")
	})
}

func markPullRequestAsReadyForReviewConstraints(config *MarkPullRequestAsReadyForReviewConfig) []string {
	return buildConstraints(config, func(config *MarkPullRequestAsReadyForReviewConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d pull request(s) can be marked as ready for review.")
		appendStringConstraint(constraints, config.TargetRepoSlug, "Pull requests will be marked as ready in repository %q.")
	})
}

func addCommentConstraints(config *AddCommentsConfig) []string {
	constraints := buildConstraints(config, func(config *AddCommentsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d comment(s) can be added.")
		appendTargetConstraint(constraints, config.Target)
		appendStringConstraint(constraints, config.TargetRepoSlug, "Comments will be added in repository %q.")
		if config.NormalizeClosingKeywords != nil && *config.NormalizeClosingKeywords {
			*constraints = append(*constraints, "Backtick-wrapped issue-closing keyword references (e.g. `Closes #1`) in the body field will be automatically normalized to plain text.")
		}
	})
	return append(constraints, "Supports reply_to_id for discussion threading.")
}

func createPullRequestConstraints(config *CreatePullRequestsConfig) []string {
	return buildConstraints(config, func(config *CreatePullRequestsConfig, constraints *[]string) {
		toolDescriptionEnhancerLog.Printf("Found create_pull_request config: max=%v, titlePrefix=%s, draft=%v", config.Max, config.TitlePrefix, config.Draft)

		appendMaxConstraint(constraints, config.Max, "Maximum %d pull request(s) can be created.")
		appendStringConstraint(constraints, config.BranchPrefix, "Branch name will be prefixed with %q.")
		appendStringConstraint(constraints, config.TitlePrefix, "Title will be prefixed with %q.")
		if len(config.Labels) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Labels %s will be automatically added.", formatStringList(config.Labels)))
		}
		if len(config.AllowedLabels) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these labels are allowed: %s.", formatStringList(config.AllowedLabels)))
		}
		if config.Draft != nil && *config.Draft == "true" {
			*constraints = append(*constraints, "PRs will be created as drafts.")
		}
		if len(config.Reviewers) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Reviewers %s will be assigned.", formatStringList(config.Reviewers)))
		}
		if len(config.Assignees) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Assignees %s will be assigned to the created pull request and any fallback issue.", formatStringList(config.Assignees)))
		}
		if config.RequireTemporaryID {
			*constraints = append(*constraints, "temporary_id is required.")
		}
		if config.NormalizeClosingKeywords != nil && *config.NormalizeClosingKeywords {
			*constraints = append(*constraints, "Backtick-wrapped issue-closing keyword references (e.g. `Closes #1`) in the body field will be automatically normalized to plain text.")
		}
	})
}

func createPullRequestReviewCommentConstraints(config *CreatePullRequestReviewCommentsConfig) []string {
	return buildConstraints(config, func(config *CreatePullRequestReviewCommentsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d review comment(s) can be created.")
		appendStringConstraint(constraints, config.Side, "Comments will be on the %s side of the diff.")
	})
}

func submitPullRequestReviewConstraints(config *SubmitPullRequestReviewConfig) []string {
	return buildConstraints(config, func(config *SubmitPullRequestReviewConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d review(s) can be submitted.")
		appendTargetConstraint(constraints, config.Target)
		appendStringConstraint(constraints, config.TargetRepoSlug, "Reviews will be submitted in repository %q.")
	})
}

func replyToPullRequestReviewCommentConstraints(config *ReplyToPullRequestReviewCommentConfig) []string {
	return buildConstraints(config, func(config *ReplyToPullRequestReviewCommentConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d reply/replies can be created.")
	})
}

func dismissPullRequestReviewConstraints(config *DismissPullRequestReviewConfig) []string {
	return buildConstraints(config, func(config *DismissPullRequestReviewConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d review dismissal(s) can be performed.")
		appendTargetConstraint(constraints, config.Target)
		appendStringConstraint(constraints, config.TargetRepoSlug, "Review dismissals will be performed in repository %q.")
		*constraints = append(*constraints, "justification must contain at least 20 characters.")
	})
}

func resolvePullRequestReviewThreadConstraints(config *ResolvePullRequestReviewThreadConfig) []string {
	return buildConstraints(config, func(config *ResolvePullRequestReviewThreadConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d review thread(s) can be resolved.")
	})
}

func createCodeScanningAlertConstraints(config *CreateCodeScanningAlertsConfig) []string {
	return buildConstraints(config, func(config *CreateCodeScanningAlertsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d alert(s) can be created.")
	})
}

func createCheckRunConstraints(config *CreateCheckRunConfig) []string {
	return buildConstraints(config, func(config *CreateCheckRunConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d check run(s) can be created.")
		appendStringConstraint(constraints, config.Name, "Check run name: %q.")
	})
}

func addLabelsConstraints(config *AddLabelsConfig) []string {
	return buildConstraints(config, func(config *AddLabelsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d label(s) can be added.")
		if len(config.Allowed) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these labels are allowed: %s.", formatStringList(config.Allowed)))
		}
		appendTargetConstraint(constraints, config.Target)
	})
}

func removeLabelsConstraints(config *RemoveLabelsConfig) []string {
	return buildConstraints(config, func(config *RemoveLabelsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d label(s) can be removed.")
		if len(config.Allowed) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these labels can be removed: %v.", config.Allowed))
		}
		appendTargetConstraint(constraints, config.Target)
	})
}

func replaceLabelConstraints(config *ReplaceLabelConfig) []string {
	return buildConstraints(config, func(config *ReplaceLabelConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d label replacement(s) allowed.")
		if len(config.AllowedTransitions) > 0 {
			pairs := make([]string, len(config.AllowedTransitions))
			for i, transition := range config.AllowedTransitions {
				pairs[i] = fmt.Sprintf("%q → %q", transition.From, transition.To)
			}
			*constraints = append(*constraints, fmt.Sprintf("Only these label transitions are allowed: %s.", formatStringList(pairs)))
		}
		if len(config.AllowedAdd) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these labels can be added: %s.", formatStringList(config.AllowedAdd)))
		}
		if len(config.AllowedRemove) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these labels can be removed: %s.", formatStringList(config.AllowedRemove)))
		}
		appendTargetConstraint(constraints, config.Target)
	})
}

func addReviewerConstraints(config *AddReviewerConfig) []string {
	return buildConstraints(config, func(config *AddReviewerConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d reviewer(s) can be added.")
	})
}

func updateIssueConstraints(config *UpdateIssuesConfig) []string {
	return buildConstraints(config, func(config *UpdateIssuesConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d issue(s) can be updated.")
		appendTargetConstraint(constraints, config.Target)
		titlePrefix := config.TitlePrefix
		if config.RequiredTitlePrefix != "" {
			titlePrefix = config.RequiredTitlePrefix
		}
		appendStringConstraint(constraints, titlePrefix, "The target issue title must start with %q.")
		if config.Title != nil && *config.Title {
			*constraints = append(*constraints, "Title updates are allowed.")
		}
		if config.Body != nil && *config.Body {
			*constraints = append(*constraints, "Body updates are allowed.")
		}
		if config.Status != nil && *config.Status {
			*constraints = append(*constraints, "Status updates (open/closed) are allowed.")
		}
	})
}

func updatePullRequestConstraints(config *UpdatePullRequestsConfig) []string {
	return buildConstraints(config, func(config *UpdatePullRequestsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d pull request(s) can be updated.")
		appendTargetConstraint(constraints, config.Target)
		if len(config.RequiredLabels) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only PRs with labels %v can be updated.", config.RequiredLabels))
		}
		appendStringConstraint(constraints, config.RequiredTitlePrefix, "Only PRs with title prefix %q can be updated.")
	})
}

func pushToPullRequestBranchConstraints(config *PushToPullRequestBranchConfig) []string {
	return buildConstraints(config, func(config *PushToPullRequestBranchConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d push(es) can be made.")
		appendStringConstraint(constraints, config.TitlePrefix, "The target pull request title must start with %q.")
	})
}

func uploadAssetConstraints(config *UploadAssetsConfig) []string {
	return buildConstraints(config, func(config *UploadAssetsConfig, constraints *[]string) {
		toolDescriptionEnhancerLog.Printf("Found upload_asset config: max=%v, maxSizeKB=%d, allowedExts=%v", config.Max, config.MaxSizeKB, config.AllowedExts)

		appendMaxConstraint(constraints, config.Max, "Maximum %d asset(s) can be uploaded.")
		if config.MaxSizeKB > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Maximum file size: %dKB.", config.MaxSizeKB))
		}
		if len(config.AllowedExts) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Allowed file extensions: %v.", config.AllowedExts))
		}
	})
}

func updateReleaseConstraints(config *UpdateReleaseConfig) []string {
	return buildConstraints(config, func(config *UpdateReleaseConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d release(s) can be updated.")
	})
}

func missingToolConstraints(config *MissingToolConfig) []string {
	return buildConstraints(config, func(config *MissingToolConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d missing tool report(s) can be created.")
	})
}

func linkSubIssueConstraints(config *LinkSubIssueConfig) []string {
	return buildConstraints(config, func(config *LinkSubIssueConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d sub-issue link(s) can be created.")
		appendStringConstraint(constraints, config.ParentTitlePrefix, "The parent issue title must start with %q.")
		appendStringConstraint(constraints, config.SubTitlePrefix, "The sub-issue title must start with %q.")
		appendStringConstraint(constraints, config.TargetRepoSlug, "Sub-issues will be linked in repository %q.")
		if len(config.AllowedRepos) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Sub-issue linking can target these repositories: %v.", config.AllowedRepos))
		}
	})
}

func assignMilestoneConstraints(config *AssignMilestoneConfig) []string {
	return buildConstraints(config, func(config *AssignMilestoneConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d milestone assignment(s) can be made.")
		appendStringConstraint(constraints, config.TargetRepoSlug, "Milestones will be assigned in repository %q.")
	})
}

func assignToAgentConstraints(config *AssignToAgentConfig) []string {
	return buildConstraints(config, func(config *AssignToAgentConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d issue(s) can be assigned to agent.")
		appendStringConstraint(constraints, config.BaseBranch, "Pull requests will target the %q branch.")
		appendStringConstraint(constraints, config.TargetRepoSlug, "Issues will be assigned to agent in repository %q.")
		if len(config.AllowedRepos) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Agent assignment can target these repositories: %v.", config.AllowedRepos))
		}
	})
}

func updateProjectConstraints(config *UpdateProjectConfig) []string {
	return buildConstraints(config, func(config *UpdateProjectConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d project operation(s) can be performed.")
		appendStringConstraint(constraints, config.Project, "Default project URL: %q.")
	})
}

func createProjectStatusUpdateConstraints(config *CreateProjectStatusUpdateConfig) []string {
	return buildConstraints(config, func(config *CreateProjectStatusUpdateConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d status update(s) can be created.")
		appendStringConstraint(constraints, config.Project, "Default project URL: %q.")
	})
}
