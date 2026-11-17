// Package lifecycle defines provider-independent safe-output lifecycle rules.
package lifecycle

import "slices"

const RuleVersion = "gh-aw-adoption-v1"

type Rule struct {
	EffectClass        string   `json:"effect_class"`
	FallbackToDelivery bool     `json:"fallback_to_delivery"`
	ValueEligible      bool     `json:"value_eligible"`
	SurvivalRequired   bool     `json:"survival_required"`
	ParentResource     string   `json:"parent_resource,omitempty"`
	HumanDecision      []string `json:"human_decision,omitempty"`
	Rejection          []string `json:"rejection,omitempty"`
	Reversal           []string `json:"reversal,omitempty"`
}

var rules = func() map[string]Rule {
	result := make(map[string]Rule)
	add := func(class string, fallback, eligible bool, names ...string) {
		for _, name := range names {
			result[name] = Rule{
				EffectClass: class, FallbackToDelivery: fallback,
				ValueEligible: eligible, SurvivalRequired: eligible,
			}
		}
	}
	add("proposal", false, true,
		"create_issue", "create_pull_request", "create_discussion",
		"create_security_advisory", "create_code_scanning_alert")
	add("publication", true, true,
		"add_comment", "add_security_advisory_comment", "create_check_run",
		"create_project", "create_project_status_update", "create_pull_request_review_comment",
		"reply_to_pull_request_review_comment", "submit_pull_request_review",
		"update_release", "upload_artifact", "upload_asset")
	add("mutation", true, true,
		"add_labels", "add_reviewer", "assign_milestone", "assign_to_user",
		"autofix_code_scanning_alert", "close_discussion", "close_issue", "close_pull_request",
		"dismiss_pull_request_review", "hide_comment", "link_sub_issue",
		"mark_pull_request_as_ready_for_review", "merge_pull_request", "push_to_pull_request_branch",
		"remove_labels", "replace_label", "resolve_pull_request_review_thread", "set_issue_field",
		"set_issue_type", "unassign_from_user", "update_discussion", "update_issue",
		"update_project", "update_pull_request")
	add("delegation", true, true,
		"approve_workflow_run", "assign_to_agent", "call_workflow", "create_agent_session",
		"dispatch_repository", "dispatch_workflow")
	add("infrastructure", false, false,
		"comment_memory", "ledger_append", "ledger_request_compaction", "noop", "push_repo_memory",
		"report_incomplete", "report_missing_data", "report_missing_tool", "upload_code_coverage")
	setRuleSignals(result)
	return result
}()

func setRuleSignals(result map[string]Rule) {
	set := func(names []string, parent string, human, rejection, reversal []string) {
		for _, name := range names {
			rule := result[name]
			rule.ParentResource, rule.HumanDecision = parent, human
			rule.Rejection, rule.Reversal = rejection, reversal
			result[name] = rule
		}
	}
	set([]string{"create_issue"}, "issue",
		[]string{"ISS_CLOSED_OK", "PR_MERGED_CLOSER"}, []string{"ISS_CLOSED_REJ", "ISS_DELETED"},
		[]string{"ISS_REOPENED", "ISS_DELETED"})
	set([]string{"create_pull_request"}, "pull_request",
		[]string{"PR_MERGED", "PR_AUTOMERGE_ENABLED", "PR_REVIEW_APPROVED"}, []string{"PR_CLOSED_REJ"},
		[]string{"PR_REVERT", "PR_REOPENED", "PR_HEAD_REWRITE"})
	set([]string{"create_discussion"}, "discussion",
		[]string{"DISC_ANSWERED", "DISC_CLOSED_RESOLVED"}, []string{"DISC_CLOSED_DUPLICATE", "DISC_CLOSED_OUTDATED", "DISC_DELETED"},
		[]string{"DISC_UNANSWERED", "DISC_DELETED"})
	set([]string{"create_security_advisory", "add_security_advisory_comment"}, "security_advisory",
		[]string{"ADV_PUBLISHED"}, []string{"ADV_CLOSED"}, []string{"ADV_WITHDRAWN", "ADV_CLOSED"})
	set([]string{"create_code_scanning_alert"}, "code_scanning_alert",
		[]string{"CS_FIXED"}, []string{"CS_DISMISSED"}, []string{"CS_REOPENED"})
	set([]string{"add_labels"}, "issue_or_pull_request",
		[]string{"SUGGESTION_APPROVED_ADD_LABEL"}, nil, []string{"LABEL_REMOVED"})
	set([]string{"assign_to_user"}, "issue_or_pull_request",
		[]string{"SUGGESTION_APPROVED_ADD_ASSIGNEE"}, nil, []string{"ASSIGNEE_REMOVED"})
	set([]string{"close_issue"}, "issue", []string{"SUGGESTION_APPROVED_CLOSE"},
		[]string{"SUGGESTION_DISMISSED"}, []string{"ISS_REOPENED"})
	set([]string{"set_issue_type"}, "issue", []string{"SUGGESTION_APPROVED_SET_TYPE"},
		[]string{"SUGGESTION_DISMISSED"}, []string{"ISSUE_TYPE_CHANGED"})
	set([]string{"set_issue_field"}, "issue", []string{"SUGGESTION_APPROVED_SET_FIELD"},
		[]string{"SUGGESTION_DISMISSED"}, []string{"ISSUE_FIELD_CHANGED"})
	set([]string{"add_comment"}, "issue_pull_request_or_discussion",
		[]string{"COMMENT_PINNED", "HUMAN_REPLY", "HUMAN_REACTION"}, nil, []string{"COMMENT_DELETED", "COMMENT_MINIMIZED"})
	set([]string{"create_pull_request_review_comment"}, "pull_request",
		[]string{"PR_SUGGESTION_APPLIED", "PR_THREAD_RESOLVED"}, nil,
		[]string{"PR_THREAD_UNRESOLVED", "PR_REVIEW_DISMISSED", "COMMENT_DELETED"})
	set([]string{"add_reviewer", "create_check_run", "dismiss_pull_request_review",
		"mark_pull_request_as_ready_for_review", "push_to_pull_request_branch",
		"reply_to_pull_request_review_comment", "resolve_pull_request_review_thread",
		"submit_pull_request_review", "update_pull_request"}, "pull_request",
		[]string{"PR_MERGED"}, []string{"PR_CLOSED_REJ"}, []string{"PR_REVERT", "PR_HEAD_REWRITE"})
	set([]string{"merge_pull_request"}, "pull_request", []string{"PR_REVIEW_APPROVED"},
		[]string{"PR_REVIEW_CHANGES_REQUESTED"}, []string{"PR_REVERT"})
	set([]string{"assign_to_agent", "create_agent_session"}, "agent_task",
		[]string{"AGENT_PR_MERGED"}, []string{"AGENT_PR_CLOSED_REJ"}, []string{"AGENT_TASK_FAILED", "AGENT_PR_REVERTED"})
	set([]string{"approve_workflow_run", "call_workflow", "dispatch_workflow"}, "workflow_run",
		nil, nil, []string{"RUN_FAILED", "RUN_CANCELLED", "RUN_TIMED_OUT"})
	set([]string{"dispatch_repository"}, "repository_dispatch",
		nil, nil, []string{"DOWNSTREAM_FAILED", "DOWNSTREAM_UNCORRELATED"})
	set([]string{"update_release"}, "release", nil, nil, []string{"RELEASE_EDITED", "RELEASE_DELETED", "RELEASE_UNPUBLISHED"})
	set([]string{"upload_artifact"}, "artifact", nil, nil, []string{"ARTIFACT_EXPIRED", "ARTIFACT_DELETED"})
	set([]string{"upload_asset"}, "asset_branch", nil, nil, []string{"FILE_REMOVED", "BRANCH_DELETED"})
	set([]string{"create_project"}, "project", nil, nil, []string{"PROJECT_CLOSED", "PROJECT_DELETED"})
	set([]string{"create_project_status_update"}, "project", nil, nil, []string{"PROJECT_STATUS_EDITED", "PROJECT_STATUS_DELETED"})
	set([]string{"update_project"}, "project", nil, nil, []string{"PROJECT_EDITED", "PROJECT_ITEM_REMOVED", "PROJECT_DELETED"})
}

func RuleFor(outputType string) (Rule, bool) {
	rule, ok := rules[outputType]
	rule.HumanDecision = slices.Clone(rule.HumanDecision)
	rule.Rejection = slices.Clone(rule.Rejection)
	rule.Reversal = slices.Clone(rule.Reversal)
	return rule, ok
}
