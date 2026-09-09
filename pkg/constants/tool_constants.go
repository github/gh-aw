package constants

const (
	LinearMCPReadOnlyURL      = "https://mcp.linear.app/mcp/readonly"
	LinearMCPDefaultTokenExpr = "${{ secrets.LINEAR_API_KEY }}"
	LinearTeamIDExpr          = "${{ vars.LINEAR_TEAM_ID }}"
	LinearProjectIDExpr       = "${{ vars.LINEAR_PROJECT_ID }}"
	JiraBaseURLExpr           = "https://pelidehalleux.atlassian.net"
	JiraUserEmailExpr         = "${{ secrets.JIRA_USER_EMAIL }}"
	JiraAPITokenExpr          = "${{ secrets.JIRA_API_TOKEN }}"
)

// AllowedExpressions contains the GitHub Actions expressions that can be used in workflow markdown content
// see https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#github-context
var AllowedExpressions = []string{
	"github.event.after",
	"github.event.before",
	"github.event.check_run.id",
	"github.event.check_suite.id",
	"github.event.comment.id",
	"github.event.deployment.id",
	"github.event.deployment_status.id",
	"github.event.deployment_status.state", // enum-like: "error", "failure", "success", "pending", "inactive", "in_progress", "queued", "waiting"
	"github.event.head_commit.id",
	"github.event.installation.id",
	"github.event.issue.number",
	"github.event.discussion.number",
	"github.event.pull_request.number",
	"github.event.milestone.number",
	"github.event.check_run.number",
	"github.event.check_suite.number",
	"github.event.workflow_job.run_id",
	"github.event.workflow_run.number",
	"github.event.label.id",
	"github.event.milestone.id",
	"github.event.organization.id",
	"github.event.page.id",
	"github.event.project.id",
	"github.event.project_card.id",
	"github.event.project_column.id",
	"github.event.release.assets[0].id",
	"github.event.release.id",
	"github.event.release.tag_name",
	"github.event.repository.id",
	"github.event.repository.default_branch",
	"github.event.review.id",
	"github.event.review_comment.id",
	"github.event.sender.id",
	"github.event.workflow_run.id",
	"github.event.workflow_run.conclusion",
	"github.event.workflow_run.html_url",
	"github.event.workflow_run.head_sha",
	"github.event.workflow_run.run_number",
	"github.event.workflow_run.event",
	"github.event.workflow_run.status",
	"github.event.issue.state",
	"github.event.issue.title",
	"github.event.pull_request.state",
	"github.event.pull_request.title",
	"github.event.discussion.title",
	"github.event.discussion.category.name",
	"github.event.release.name",
	"github.event.workflow_job.id",
	"github.event.deployment.environment",
	"github.event.pull_request.head.sha",
	"github.event.pull_request.base.sha",
	"github.actor",
	"github.event_name",
	"github.job",
	"github.owner",
	"github.repository",
	"github.repository_owner",
	"github.run_id",
	"github.run_number",
	"github.server_url",
	"github.workflow",
	"github.workspace",
} // needs., steps. already allowed

// AllowedExpressionsSet is a pre-built set for O(1) membership checks.
// Use this instead of slices.Contains(AllowedExpressions, expr) for performance.
var AllowedExpressionsSet = func() map[string]struct{} {
	s := make(map[string]struct{}, len(AllowedExpressions))
	for _, e := range AllowedExpressions {
		s[e] = struct{}{}
	}
	return s
}()

// DangerousPropertyNames contains JavaScript built-in property names that are blocked
// in GitHub Actions expressions to prevent prototype pollution and traversal attacks.
// This list matches the DANGEROUS_PROPS list in actions/setup/js/runtime_import.cjs
// See PR #14826 for context on these security measures.
var DangerousPropertyNames = []string{
	"constructor",
	"__proto__",
	"prototype",
	"__defineGetter__",
	"__defineSetter__",
	"__lookupGetter__",
	"__lookupSetter__",
	"hasOwnProperty",
	"isPrototypeOf",
	"propertyIsEnumerable",
	"toString",
	"valueOf",
	"toLocaleString",
}

// DangerousPropertyNamesSet is a pre-built set for O(1) membership checks.
// Use this instead of iterating DangerousPropertyNames for performance.
var DangerousPropertyNamesSet = func() map[string]struct{} {
	s := make(map[string]struct{}, len(DangerousPropertyNames))
	for _, p := range DangerousPropertyNames {
		s[p] = struct{}{}
	}
	return s
}()

// DefaultReadOnlyGitHubTools defines the default read-only GitHub MCP tools.
// This list is shared by both local (Docker) and remote (hosted) modes.
// Currently, both modes use identical tool lists, but this may diverge in the future
// if different modes require different default tool sets.
var DefaultReadOnlyGitHubTools = []string{
	// actions (actions_run_trigger excluded: it is a write/trigger operation, not read-only)
	"actions_get",
	"actions_list",
	"get_job_logs",
	// code security
	"get_code_scanning_alert",
	"list_code_scanning_alerts",
	// context
	"get_me",
	// dependabot
	"get_dependabot_alert",
	"list_dependabot_alerts",
	// discussions
	"get_discussion",
	"get_discussion_comments",
	"list_discussion_categories",
	"list_discussions",
	// issues
	"issue_read",
	"list_issues",
	"search_issues",
	// notifications
	"get_notification_details",
	"list_notifications",
	// organizations
	"search_orgs",
	// labels
	"get_label",
	"list_label",
	// prs
	"get_pull_request",
	"get_pull_request_comments",
	"get_pull_request_diff",
	"get_pull_request_files",
	"get_pull_request_reviews",
	"get_pull_request_status",
	"list_pull_requests",
	"pull_request_read",
	"search_pull_requests",
	// repos
	"get_commit",
	"get_file_contents",
	"get_tag",
	"list_branches",
	"list_commits",
	"list_tags",
	"search_code",
	"search_repositories",
	// secret protection
	"get_secret_scanning_alert",
	"list_secret_scanning_alerts",
	// users
	"search_users",
	// additional unique tools (previously duplicated block extras)
	"get_latest_release",
	"get_pull_request_review_comments",
	"get_release_by_tag",
	"list_issue_types",
	"list_releases",
	"list_starred_repositories",
}

// DefaultGitHubToolsLocal defines the default read-only GitHub MCP tools for local (Docker) mode.
// Currently identical to DefaultReadOnlyGitHubTools. Kept separate for backward compatibility
// and to allow future divergence if local mode requires different defaults.
var DefaultGitHubToolsLocal = DefaultReadOnlyGitHubTools

// DefaultGitHubToolsRemote defines the default read-only GitHub MCP tools for remote (hosted) mode.
// Currently identical to DefaultReadOnlyGitHubTools. Kept separate for backward compatibility
// and to allow future divergence if remote mode requires different defaults.
var DefaultGitHubToolsRemote = DefaultReadOnlyGitHubTools

// DefaultGitHubTools is deprecated. Use DefaultGitHubToolsLocal or DefaultGitHubToolsRemote instead.
// Kept for backward compatibility and defaults to local mode tools.
var DefaultGitHubTools = DefaultGitHubToolsLocal

// DefaultBashTools defines basic bash commands that should be available by default when bash is enabled
var DefaultBashTools = []string{
	"echo",
	"printf",
	"ls",
	"pwd",
	"cat",
	"head",
	"tail",
	"grep",
	"wc",
	"sort",
	"uniq",
	"date",
	"yq",
}
