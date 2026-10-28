// Package cachepolicy defines the trigger boundary for compiler-managed cache writes.
package cachepolicy

import "slices"

// WriteEvents returns GitHub's default-branch cache-write allowlist, plus
// pull_request, whose caches are isolated to the pull request's merge ref.
// https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#cache-access-for-low-trust-workflow-triggers
func WriteEvents() []string {
	return []string{
		"push", "workflow_dispatch", "repository_dispatch", "delete",
		"registry_package", "page_build", "schedule", "pull_request",
	}
}

// AllowsWrite fails closed for unknown events. Reusable workflows use the
// original caller's runtime event, not workflow_call.
func AllowsWrite(event string) bool {
	return slices.Contains(WriteEvents(), event)
}

// ReadOnlyEvents lists known events outside the write allowlist for authored
// trigger diagnostics. workflow_call requires review of the caller's policy.
func ReadOnlyEvents() []string {
	return []string{
		"branch_protection_rule", "check_run", "check_suite", "create",
		"deployment", "deployment_status", "discussion", "discussion_comment",
		"fork", "gollum", "issue_comment", "issues", "label", "merge_group",
		"milestone", "project", "project_card", "project_column", "public",
		"pull_request_review", "pull_request_review_comment", "pull_request_target",
		"release", "status", "watch", "workflow_call", "workflow_run",
	}
}
