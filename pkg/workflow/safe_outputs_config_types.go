package workflow

import "github.com/github/gh-aw/pkg/logger"

var safeOutputsConfigLog = logger.New("workflow:safe_outputs_config")

// Safe-outputs configuration is split across focused files:
//   - safe_outputs_config_types.go: shared types and module logger
//   - safe_outputs_config_extraction.go: frontmatter-to-config extraction
//   - safe_outputs_config_global.go: shared global field parsing helpers
//   - safe_outputs_config_base.go: common per-handler base parsing helpers
//   - safe_outputs_config_runtime.go: runtime handler-manager serialization
//
// Keeping these boundaries explicit makes the large safe-outputs surface easier
// to navigate without changing any of the public workflow configuration API.

// ========================================
// Safe Output Configuration Types
// ========================================

// BaseSafeOutputConfig holds common configuration fields for all safe output types
type BaseSafeOutputConfig struct {
	Max                      *string          `yaml:"max,omitempty"`                        // Maximum number of items to create (supports integer or GitHub Actions expression)
	GitHubToken              string           `yaml:"github-token,omitempty"`               // GitHub token for this specific output type
	GitHubApp                *GitHubAppConfig `yaml:"github-app,omitempty"`                 // GitHub App credentials for minting a per-handler installation access token
	Staged                   *TemplatableBool `yaml:"staged,omitempty"`                     // Templatable preview-only mode for this specific output type
	Footer                   *string          `yaml:"footer,omitempty"`                     // Controls AI-generated footer behavior. Values vary by handler; false omits the visible footer but keeps XML markers.
	BodyFooter               string           `yaml:"body-footer,omitempty"`                // Deterministic template appended after agent-generated body content by supported handlers.
	IssueIntent              *bool            `yaml:"issue-intent,omitempty"`               // When true, enable issue-intent rationale/confidence guidance and schema requirements for this output type.
	NormalizeClosingKeywords *bool            `yaml:"normalize-closing-keywords,omitempty"` // When true for this output type, strip backticks from recognized issue-closing keywords in body fields.
	// Samples carries deterministic replay samples for the hidden
	// `gh aw compile --use-samples` flag. Each entry is the JSON object
	// passed to the corresponding MCP tool's `tools/call` arguments.
	// Sample-only sidecar fields (for example `patch` for
	// create_pull_request) are stripped before the call and used by the
	// replay driver.
	Samples []map[string]any `yaml:"samples,omitempty"`
}

// SafeOutputsConfig holds configuration for automatic output routes
type SafeOutputsConfig struct {
	Steer                                  bool                                   `yaml:"steer,omitempty"` // Experimental. Create an issue and steer the agent from issue comments.
	CreateIssues                           *CreateIssuesConfig                    `yaml:"create-issue,omitempty"`
	CreateWorkItems                        *CreateWorkItemConfig                  `yaml:"ado-create-work-item,omitempty"`
	UpdateWorkItems                        *UpdateWorkItemConfig                  `yaml:"ado-update-work-item,omitempty"`
	CommentOnWorkItems                     *CommentOnWorkItemConfig               `yaml:"ado-comment-on-work-item,omitempty"`
	AssignWorkItems                        *AssignWorkItemConfig                  `yaml:"ado-assign-work-item,omitempty"`
	LinkWorkItems                          *LinkWorkItemsConfig                   `yaml:"ado-link-work-items,omitempty"`
	UploadWorkItemAttachments              *UploadWorkItemAttachmentConfig        `yaml:"ado-upload-workitem-attachment,omitempty"`
	LinearCreateIssue                      *LinearCreateIssueConfig               `yaml:"linear-create-issue,omitempty"`
	LinearAddComment                       *LinearTargetConfig                    `yaml:"linear-add-comment,omitempty"`
	LinearUpdateIssue                      *LinearUpdateIssueConfig               `yaml:"linear-update-issue,omitempty"`
	CreateDiscussions                      *CreateDiscussionsConfig               `yaml:"create-discussion,omitempty"`
	UpdateDiscussions                      *UpdateDiscussionsConfig               `yaml:"update-discussion,omitempty"`
	CloseDiscussions                       *CloseDiscussionsConfig                `yaml:"close-discussion,omitempty"`
	CloseIssues                            *CloseIssuesConfig                     `yaml:"close-issue,omitempty"`
	ClosePullRequests                      *ClosePullRequestsConfig               `yaml:"close-pull-request,omitempty"`
	MarkPullRequestAsReadyForReview        *MarkPullRequestAsReadyForReviewConfig `yaml:"mark-pull-request-as-ready-for-review,omitempty"`
	ApproveWorkflowRun                     *ApproveWorkflowRunConfig              `yaml:"approve-workflow-run,omitempty"`        // Approve a pending workflow run awaiting required approval
	DismissPullRequestReview               *DismissPullRequestReviewConfig        `yaml:"dismiss-pull-request-review,omitempty"` // Dismiss a pull request review authored by the workflow actor
	AddComments                            *AddCommentsConfig                     `yaml:"add-comment,omitempty"`
	CreatePullRequests                     *CreatePullRequestsConfig              `yaml:"create-pull-request,omitempty"`
	CreatePullRequestReviewComments        *CreatePullRequestReviewCommentsConfig `yaml:"create-pull-request-review-comment,omitempty"`
	SubmitPullRequestReview                *SubmitPullRequestReviewConfig         `yaml:"submit-pull-request-review,omitempty"`           // Submit a PR review with status (APPROVE, REQUEST_CHANGES, COMMENT)
	ReplyToPullRequestReviewComment        *ReplyToPullRequestReviewCommentConfig `yaml:"reply-to-pull-request-review-comment,omitempty"` // Reply to existing review comments on PRs
	ResolvePullRequestReviewThread         *ResolvePullRequestReviewThreadConfig  `yaml:"resolve-pull-request-review-thread,omitempty"`   // Resolve a review thread on a pull request
	CreateCodeScanningAlerts               *CreateCodeScanningAlertsConfig        `yaml:"create-code-scanning-alert,omitempty"`
	AutofixCodeScanningAlert               *AutofixCodeScanningAlertConfig        `yaml:"autofix-code-scanning-alert,omitempty"`
	CreateCheckRun                         *CreateCheckRunConfig                  `yaml:"create-check-run,omitempty"` // Create GitHub Check Runs to report agent analysis results
	JiraCreateIssue                        *JiraSafeOutputConfig                  `yaml:"jira-create-issue,omitempty"`
	JiraUpdateIssue                        *JiraSafeOutputConfig                  `yaml:"jira-update-issue,omitempty"`
	JiraAddComment                         *JiraSafeOutputConfig                  `yaml:"jira-add-comment,omitempty"`
	JiraAddLabel                           *JiraSafeOutputConfig                  `yaml:"jira-add-label,omitempty"`
	AddLabels                              *AddLabelsConfig                       `yaml:"add-labels,omitempty"`
	RemoveLabels                           *RemoveLabelsConfig                    `yaml:"remove-labels,omitempty"`
	ReplaceLabel                           *ReplaceLabelConfig                    `yaml:"replace-label,omitempty"` // Replace one label with another in a single atomic operation
	AddReviewer                            *AddReviewerConfig                     `yaml:"add-reviewer,omitempty"`
	AssignMilestone                        *AssignMilestoneConfig                 `yaml:"assign-milestone,omitempty"`
	AssignToAgent                          *AssignToAgentConfig                   `yaml:"assign-to-agent,omitempty"`
	AssignToUser                           *AssignToUserConfig                    `yaml:"assign-to-user,omitempty"`     // Assign users to issues
	UnassignFromUser                       *UnassignFromUserConfig                `yaml:"unassign-from-user,omitempty"` // Remove assignees from issues
	UpdateIssues                           *UpdateIssuesConfig                    `yaml:"update-issue,omitempty"`
	UpdatePullRequests                     *UpdatePullRequestsConfig              `yaml:"update-pull-request,omitempty"` // Update GitHub pull request title/body
	MergePullRequest                       *MergePullRequestConfig                `yaml:"merge-pull-request,omitempty"`  // Merge pull requests under constrained policy checks
	PushToPullRequestBranch                *PushToPullRequestBranchConfig         `yaml:"push-to-pull-request-branch,omitempty"`
	UploadAssets                           *UploadAssetsConfig                    `yaml:"upload-asset,omitempty"`
	UploadArtifact                         *UploadArtifactConfig                  `yaml:"upload-artifact,omitempty"`              // Upload files as run-scoped GitHub Actions artifacts
	UploadCodeCoverage                     *UploadCodeCoverageConfig              `yaml:"upload-code-coverage,omitempty"`         // Upload a code coverage report via actions/upload-code-coverage
	UpdateRelease                          *UpdateReleaseConfig                   `yaml:"update-release,omitempty"`               // Update GitHub release descriptions
	CreateAgentSessions                    *CreateAgentSessionConfig              `yaml:"create-agent-session,omitempty"`         // Create GitHub Copilot coding agent sessions
	UpdateProjects                         *UpdateProjectConfig                   `yaml:"update-project,omitempty"`               // Smart project board management (create/add/update)
	CreateProjects                         *CreateProjectsConfig                  `yaml:"create-project,omitempty"`               // Create GitHub Projects V2
	CreateProjectStatusUpdates             *CreateProjectStatusUpdateConfig       `yaml:"create-project-status-update,omitempty"` // Create GitHub project status updates
	LinkSubIssue                           *LinkSubIssueConfig                    `yaml:"link-sub-issue,omitempty"`               // Link issues as sub-issues
	HideComment                            *HideCommentConfig                     `yaml:"hide-comment,omitempty"`                 // Hide comments
	SetIssueType                           *SetIssueTypeConfig                    `yaml:"set-issue-type,omitempty"`               // Set the type of an issue (empty string clears the type)
	SetIssueField                          *SetIssueFieldConfig                   `yaml:"set-issue-field,omitempty"`              // Set a single issue field value by name/value
	DispatchWorkflow                       *DispatchWorkflowConfig                `yaml:"dispatch-workflow,omitempty"`            // Dispatch workflow_dispatch events to other workflows
	DispatchRepository                     *DispatchRepositoryConfig              `yaml:"dispatch-repository,omitempty"`          // Dispatch repository_dispatch events to external repositories; the underscore alias remains supported via parseDispatchRepositoryConfig.
	CallWorkflow                           *CallWorkflowConfig                    `yaml:"call-workflow,omitempty"`                // Call reusable workflows via workflow_call fan-out
	MissingTool                            *MissingToolConfig                     `yaml:"missing-tool,omitempty"`                 // Optional for reporting missing functionality
	MissingData                            *MissingDataConfig                     `yaml:"missing-data,omitempty"`                 // Optional for reporting missing data required to achieve goals
	NoOp                                   *NoOpConfig                            `yaml:"noop,omitempty"`                         // No-op output for logging only (always available as fallback)
	ReportIncomplete                       *ReportIncompleteConfig                `yaml:"report-incomplete,omitempty"`            // Signal that the task could not be completed due to a tool or infrastructure failure
	ThreatDetection                        *ThreatDetectionConfig                 `yaml:"threat-detection,omitempty"`             // Threat detection configuration
	Jobs                                   map[string]*SafeJobConfig              `yaml:"jobs,omitempty"`                         // Safe-jobs configuration (moved from top-level)
	Scripts                                map[string]*SafeScriptConfig           `yaml:"scripts,omitempty"`                      // Custom inline handlers that run in the safe-output handler loop
	GitHubApp                              *GitHubAppConfig                       `yaml:"github-app,omitempty"`                   // GitHub App credentials for token minting
	URLs                                   SafeOutputsURLsPolicy                  `yaml:"urls,omitempty"`                         // URL sanitization policy: SafeOutputsURLsPolicyAllowedOnly (default) or SafeOutputsURLsPolicyAllowedOrCodeRegion
	Data                                   any                                    `yaml:"data,omitempty"`                         // Structured data mode for body-based safe outputs: false/omitted (disabled), true (allow any object), object (inline schema), or GitHub Actions expression string
	DataEnabled                            bool                                   `yaml:"-"`                                      // Internal flag controlling whether `data` is allowed for body-based safe outputs
	NormalizedDataSchema                   map[string]any                         `yaml:"-"`                                      // Internal normalized schema derived from inline `data` object schemas
	DataSchemaExpression                   string                                 `yaml:"-"`                                      // Internal runtime GitHub Actions expression used to provide `data` schema dynamically
	AllowedDomains                         []string                               `yaml:"allowed-domains,omitempty"`              // Allowed domains for URL redaction, unioned with network.allowed; supports ecosystem identifiers
	AllowGitHubReferences                  []string                               `yaml:"allowed-github-references,omitempty"`    // Allowed repositories for GitHub references (e.g., ["repo", "org/repo2"])
	Staged                                 *TemplatableBool                       `yaml:"staged,omitempty"`                       // Templatable preview-only mode for all safe outputs
	Env                                    map[string]string                      `yaml:"env,omitempty"`                          // Environment variables to pass to safe output jobs
	GitHubToken                            string                                 `yaml:"github-token,omitempty"`                 // GitHub token for safe output jobs
	LinearToken                            string                                 `yaml:"linear-token,omitempty"`                 // Linear API key for Linear safe output handlers
	MaximumPatchSize                       int                                    `yaml:"max-patch-size,omitempty"`               // Maximum allowed patch size in KB (defaults to 4096)
	MaximumPatchFiles                      int                                    `yaml:"max-patch-files,omitempty"`              // Maximum allowed unique files per create-pull-request patch (defaults to 100)
	RunsOn                                 string                                 `yaml:"runs-on,omitempty"`                      // Runner configuration for safe-outputs jobs
	Messages                               *SafeOutputMessagesConfig              `yaml:"messages,omitempty"`                     // Custom message templates for footer and notifications
	Mentions                               *MentionsConfig                        `yaml:"mentions,omitempty"`                     // Configuration for @mention filtering in safe outputs
	Footer                                 *bool                                  `yaml:"footer,omitempty"`                       // Global footer control - when false, omits visible footer from all safe outputs (XML markers still included)
	GroupReports                           bool                                   `yaml:"group-reports,omitempty"`                // If true, create parent "Failed runs" issue for agent failures (default: false)
	ReportFailureAsIssue                   *TemplatableBool                       `yaml:"report-failure-as-issue,omitempty"`      // Controls failure issue creation: bool or templatable expression string. Default: true. Category arrays are parsed into ReportFailureAsIssueCategories/ExcludedCategories.
	ReportFailureAsIssueCategories         []string                               `yaml:"-"`                                      // Parsed failure categories for report-failure-as-issue (internal use only, included categories)
	ReportFailureAsIssueExcludedCategories []string                               `yaml:"-"`                                      // Parsed excluded failure categories for report-failure-as-issue (internal use only, categories starting with "!")
	ReportFailedJobs                       *bool                                  `yaml:"report-failed-jobs,omitempty"`           // Controls whether to report failed non-builtin jobs as issues (default: true). Set to false to disable.
	FailureIssueRepo                       string                                 `yaml:"failure-issue-repo,omitempty"`           // Repository to create failure issues in (format: "owner/repo"), defaults to current repo
	MaxBotMentions                         *string                                `yaml:"max-bot-mentions,omitempty"`             // Maximum bot trigger references (e.g. 'fixes #123') allowed before filtering. Default: 10. Supports integer or GitHub Actions expression.
	Steps                                  []any                                  `yaml:"steps,omitempty"`                        // User-provided steps injected after setup/checkout and before safe-output code
	IDToken                                *string                                `yaml:"id-token,omitempty"`                     // Override id-token permission: "write" to force-add, "none" to disable auto-detection
	ConcurrencyGroup                       string                                 `yaml:"concurrency-group,omitempty"`            // Concurrency group for the safe-outputs job (cancel-in-progress is always false)
	Needs                                  []string                               `yaml:"needs,omitempty"`                        // Additional custom workflow jobs that safe_outputs should depend on
	Environment                            string                                 `yaml:"environment,omitempty"`                  // Override the GitHub deployment environment for the safe-outputs job (defaults to the top-level environment: field)
	Actions                                map[string]*SafeOutputActionConfig     `yaml:"actions,omitempty"`                      // Custom GitHub Actions mounted as safe output tools (resolved at compile time)
	TimeoutMinutes                         int                                    `yaml:"timeout-minutes,omitempty"`              // Timeout for the safe_outputs job in minutes. Defaults to 45.
	AutoInjectedCreateIssue                bool                                   `yaml:"-"`                                      // Internal: true when create-issues was automatically injected by the compiler (not user-configured)
}

// SafeOutputMessagesConfig holds custom message templates for safe-output footer and notification messages
type SafeOutputMessagesConfig struct {
	Footer                         string `yaml:"footer,omitempty" json:"footer,omitempty"`                                                    // Custom footer message template
	FooterInstall                  string `yaml:"footer-install,omitempty" json:"footerInstall,omitempty"`                                     // Custom installation instructions template
	FooterWorkflowRecompile        string `yaml:"footer-workflow-recompile,omitempty" json:"footerWorkflowRecompile,omitempty"`                // Custom footer template for workflow recompile issues
	FooterWorkflowRecompileComment string `yaml:"footer-workflow-recompile-comment,omitempty" json:"footerWorkflowRecompileComment,omitempty"` // Custom footer template for comments on workflow recompile issues
	StagedTitle                    string `yaml:"staged-title,omitempty" json:"stagedTitle,omitempty"`                                         // Custom styled mode title template
	StagedDescription              string `yaml:"staged-description,omitempty" json:"stagedDescription,omitempty"`                             // Custom staged mode description template
	AppendOnlyComments             bool   `yaml:"append-only-comments,omitempty" json:"appendOnlyComments,omitempty"`                          // If true, post run status as new comments instead of updating the activation comment
	ActivationComments             string `yaml:"activation-comments,omitempty" json:"activationComments,omitempty"`                           // If "false", disable all activation/fallback comments entirely. Supports templatable boolean values (literal "true"/"false" or GitHub Actions expressions). Empty/unset preserves default enabled behavior.
	RunStarted                     string `yaml:"run-started,omitempty" json:"runStarted,omitempty"`                                           // Custom workflow activation message template
	RunSuccess                     string `yaml:"run-success,omitempty" json:"runSuccess,omitempty"`                                           // Custom workflow success message template
	RunFailure                     string `yaml:"run-failure,omitempty" json:"runFailure,omitempty"`                                           // Custom workflow failure message template
	DetectionFailure               string `yaml:"detection-failure,omitempty" json:"detectionFailure,omitempty"`                               // Custom detection job failure message template
	PullRequestCreated             string `yaml:"pull-request-created,omitempty" json:"pullRequestCreated,omitempty"`                          // Custom message template for pull request creation link. Placeholders: {item_number}, {item_url}
	IssueCreated                   string `yaml:"issue-created,omitempty" json:"issueCreated,omitempty"`                                       // Custom message template for issue creation link. Placeholders: {item_number}, {item_url}
	CommitPushed                   string `yaml:"commit-pushed,omitempty" json:"commitPushed,omitempty"`                                       // Custom message template for commit push link. Placeholders: {commit_sha}, {short_sha}, {commit_url}
	AgentFailureIssue              string `yaml:"agent-failure-issue,omitempty" json:"agentFailureIssue,omitempty"`                            // Custom footer template for agent failure tracking issues
	AgentFailureComment            string `yaml:"agent-failure-comment,omitempty" json:"agentFailureComment,omitempty"`                        // Custom footer template for comments on agent failure tracking issues
	BodyHeader                     string `yaml:"body-header,omitempty" json:"bodyHeader,omitempty"`                                           // Custom header text prepended to every message body (issues, comments, PRs, discussions). Placeholders: {workflow_name}, {run_url}
	DisclosureHeader               string `yaml:"disclosure-header,omitempty" json:"disclosureHeader,omitempty"`                               // AI authorship disclosure header prepended to every message body. Set to "true" for built-in default text, or provide a custom template string. Placeholders: {workflow_name}, {run_url}
}

// MentionsConfig holds configuration for @mention filtering in safe outputs
type MentionsConfig struct {
	// Enabled can be:
	//   true: mentions always allowed (error in strict mode)
	//   false: mentions always escaped
	//   nil: use default behavior with team members and context
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// AllowedCollaborators determines if repository collaborators can be mentioned (default: true)
	AllowedCollaborators *bool `yaml:"allowed-collaborators,omitempty" json:"allowedCollaborators,omitempty"`

	// AllowContext determines if mentions from event context are allowed (default: true)
	AllowContext *bool `yaml:"allow-context,omitempty" json:"allowContext,omitempty"`

	// Allowed is a list of user/bot names always allowed (bots not allowed by default)
	Allowed []string `yaml:"allowed,omitempty" json:"allowed,omitempty"`

	// AllowedTeams is a list of team slugs whose members are always allowed to be mentioned.
	// Accepts "team-slug" (resolved against the current org) or "org/team-slug" format.
	// Requires the workflow token to have read:org scope (a fine-grained PAT, classic PAT with
	// read:org, or a GitHub App with the Members:Read permission). The default GITHUB_TOKEN
	// does not include read:org and will produce a 403/404 warning; team members will be skipped
	// but the workflow will not fail.
	AllowedTeams []string `yaml:"allowed-teams,omitempty" json:"allowedTeams,omitempty"`

	// Max is the maximum number of mentions per message (default: 50). Supports integer or GitHub Actions expression.
	Max *string `yaml:"max,omitempty" json:"max,omitempty"`
}

// SecretMaskingConfig holds configuration for secret redaction behavior
type SecretMaskingConfig struct {
	Steps []map[string]any `yaml:"steps,omitempty"` // Additional secret redaction steps to inject after built-in redaction
}
