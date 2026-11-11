// TypeScript definitions for GitHub Agentic Workflows Safe Outputs JSONL Types
// This file provides type definitions for JSONL output items produced by agents

// === JSONL Output Item Types ===

/**
 * Base interface for all safe output items
 */
interface BaseSafeOutputItem {
  /** The type of safe output action */
  type: string;
  /**
   * Secrecy level of the message content.
   * Indicates the confidentiality of the data included in this message
   * (e.g., "public", "internal", "private").
   */
  secrecy?: string;
  /**
   * Integrity level of the message content.
   * Indicates the trustworthiness of the data source for this message
   * (e.g., "low", "medium", "high").
   */
  integrity?: string;
}

type IssueArtifactType = "generic" | "link" | "plan" | "embedded_html";

interface CreateSecurityAdvisoryItem extends BaseSafeOutputItem {
  type: "create_security_advisory";
  summary: string;
  description: string;
  severity?: "critical" | "high" | "medium" | "low";
  cwe_ids?: string[];
  vulnerabilities: Array<{
    package: {
      ecosystem: "rubygems" | "npm" | "pip" | "maven" | "nuget" | "composer" | "go" | "rust" | "erlang" | "actions" | "pub" | "other" | "swift";
      name?: string | null;
    };
    vulnerable_version_range?: string | null;
    patched_versions?: string | null;
    vulnerable_functions?: string[] | null;
  }>;
}

interface IssueArtifactMetadata {
  /** Artifact format; defaults to "generic" when omitted. */
  type?: IssueArtifactType;
  name: string;
}

type IssueArtifactBodyInput = IssueArtifactMetadata & { body: string; path?: never };

/** MCP paths are absolute workspace or /tmp/gh-aw/agent files; sensitive publication sources are denied. */
type IssueArtifactInput = IssueArtifactBodyInput | (IssueArtifactMetadata & { path: string; body?: never });

interface IssueArtifact extends IssueArtifactMetadata {
  /** Validated artifact format with the default applied. */
  type: IssueArtifactType;
  body: string;
}

interface CreatedIssueArtifact {
  /** Per-issue artifact number returned by GitHub. */
  number: number;
  /** Artifact page URL, when returned by GitHub. */
  url: string | undefined;
}

/**
 * JSONL item for creating a GitHub issue
 */
interface CreateIssueItem extends BaseSafeOutputItem {
  type: "create_issue";
  /** Issue title */
  title: string;
  /** Issue body content */
  body: string;
  /** Optional artifacts to attach after creating the issue. */
  artifacts?: IssueArtifactBodyInput[];
  /** Optional labels to add to the issue */
  labels?: string[];
  /** Optional issue fields to set after creating the issue */
  fields?: Array<{
    /** Issue field display name */
    name: string;
    /** Field value (string for text/single-select/iteration/date, number for numeric fields) */
    value: string | number;
  }>;
  /** Optional parent issue number or temporary_id to link as sub-issue */
  parent?: number | string;
  /** Optional temporary identifier for this issue that can be referenced by other issues */
  temporary_id?: string;
}

/**
 * JSONL item for creating a GitHub discussion
 */
interface CreateDiscussionItem extends BaseSafeOutputItem {
  type: "create_discussion";
  /** Discussion title */
  title: string;
  /** Discussion body content */
  body: string;
  /** Optional category ID for the discussion */
  category_id?: number | string;
}

/**
 * JSONL item for updating a GitHub discussion
 */
interface UpdateDiscussionItem extends BaseSafeOutputItem {
  type: "update_discussion";
  /** Optional new discussion title */
  title?: string;
  /** Optional new discussion body */
  body?: string;
  /** Optional labels to set on the discussion (replaces existing labels) */
  labels?: string[];
  /** Optional discussion number for target "*" */
  discussion_number?: number | string;
}

/**
 * JSONL item for closing a GitHub discussion
 */
interface CloseDiscussionItem extends BaseSafeOutputItem {
  type: "close_discussion";
  /** Comment body to add when closing the discussion */
  body: string;
  /** Optional resolution reason */
  reason?: "RESOLVED" | "DUPLICATE" | "OUTDATED" | "ANSWERED";
  /** Optional discussion number (uses triggering discussion if not provided) */
  discussion_number?: number | string;
}

/**
 * JSONL item for closing a GitHub issue
 */
interface CloseIssueItem extends BaseSafeOutputItem, IssueIntentMetadata {
  type: "close_issue";
  /** Comment body to add when closing the issue */
  body: string;
  /** Optional issue number (uses triggering issue if not provided) */
  issue_number?: number | string;
  /**
   * Optional canonical issue reference when closing as a duplicate.
   * Accepts: bare number (123), #-prefixed number (#123),
   * owner/repo#number (github/gh-aw#123), or a full GitHub issue URL.
   * When provided together with state_reason: "duplicate", creates a native
   * GitHub duplicate relationship (marked_as_duplicate timeline event).
   */
  duplicate_of?: number | string;
}

/**
 * JSONL item for closing a GitHub pull request without merging
 */
interface ClosePullRequestItem extends BaseSafeOutputItem {
  type: "close_pull_request";
  /** Comment body to add when closing the pull request */
  body: string;
  /** Optional pull request number (uses triggering PR if not provided) */
  pull_request_number?: number | string;
}

/**
 * JSONL item for marking a draft pull request as ready for review
 */
interface MarkPullRequestAsReadyForReviewItem extends BaseSafeOutputItem {
  type: "mark_pull_request_as_ready_for_review";
  /** Comment explaining why the PR is ready for review */
  reason: string;
  /** Optional pull request number (uses triggering PR if not provided) */
  pull_request_number?: number | string;
}

/**
 * JSONL item for approving a pending workflow run from a fork pull request
 */
interface ApproveWorkflowRunItem extends BaseSafeOutputItem {
  type: "approve_workflow_run";
  /** Positive integer workflow run ID */
  run_id: number | string;
}

/**
 * JSONL item for adding a comment to an issue or PR
 */
interface AddCommentItem extends BaseSafeOutputItem {
  type: "add_comment";
  /** Comment body content */
  body: string;
}

/**
 * JSONL item for persisting memory in a managed issue/PR comment
 */
interface CommentMemoryItem extends BaseSafeOutputItem {
  type: "comment_memory";
  /** Markdown body content to persist */
  body: string;
  /** Optional memory identifier (defaults to "default") */
  memory_id?: string;
  /** Optional target issue/PR number */
  item_number?: number | string;
  /** Optional target repository */
  repo?: string;
}

/**
 * JSONL item for creating a pull request
 */
interface CreatePullRequestItem extends BaseSafeOutputItem {
  type: "create_pull_request";
  /** Pull request title */
  title: string;
  /** Pull request body content */
  body: string;
  /** Optional branch name (will be auto-generated if not provided) */
  branch?: string;
  /** Optional labels to add to the PR */
  labels?: string[];
  /** Whether to create the PR as a draft (default: true) */
  draft?: boolean;
  /** Optional temporary identifier for this pull request for cross-reference replacement */
  temporary_id?: string;
}

/**
 * JSONL item for creating a pull request review comment
 */
interface CreatePullRequestReviewCommentItem extends BaseSafeOutputItem {
  type: "create_pull_request_review_comment";
  /** File path for the review comment */
  path: string;
  /** Line number for the comment */
  line: number | string;
  /** Comment body content */
  body: string;
  /** Optional start line for multi-line comments */
  start_line?: number | string;
  /** Optional side of the diff: "LEFT" or "RIGHT" */
  side?: "LEFT" | "RIGHT";
}

/**
 * JSONL item for creating a code scanning alert
 */
interface CreateCodeScanningAlertItem extends BaseSafeOutputItem {
  type: "create_code_scanning_alert";
  /** File path where the issue was found */
  file: string;
  /** Line number where the issue was found */
  line: number | string;
  /** Severity level: "error", "warning", "info", or "note" */
  severity: "error" | "warning" | "info" | "note";
  /** Alert message describing the issue */
  message: string;
  /** Optional column number */
  column?: number | string;
  /** Optional rule ID suffix for uniqueness */
  ruleIdSuffix?: string;
}

/**
 * JSONL item for uploading a code coverage report via actions/upload-code-coverage
 */
interface UploadCodeCoverageItem extends BaseSafeOutputItem {
  type: "upload_code_coverage";
  /** Path to the coverage report file (staging-relative, absolute, or workspace-relative) */
  file: string;
  /** Linguist language name for the coverage report (e.g. "Go", "TypeScript") */
  language: string;
  /** Label identifying this coverage report (e.g. "code-coverage/unit-tests") */
  label: string;
}

/**
 * JSONL item for adding labels to an issue or PR
 */
interface AddLabelsItem extends BaseSafeOutputItem {
  type: "add_labels";
  /** Array of label names or label specs to add */
  labels: Array<string | IssueIntentLabel>;
  /** Target issue; otherwize resolved from current context */
  issue_number?: number;
}

interface IssueIntentMetadata {
  /** Optional rationale for the change */
  rationale?: string;
  /** Optional confidence level for the change */
  confidence?: "LOW" | "MEDIUM" | "HIGH";
  /** When true, route through pending suggestion review */
  suggest?: boolean;
}

interface IssueIntentLabel extends IssueIntentMetadata {
  /** Label name to apply */
  name: string;
}

/**
 * JSONL item for removing labels from an issue or PR
 */
interface RemoveLabelsItem extends BaseSafeOutputItem {
  type: "remove_labels";
  /** Array of label names or label specs to remove */
  labels: Array<string | IssueIntentLabel>;
  /** Target issue; otherwise resolved from current context */
  item_number?: number;
}

/**
 * JSONL item for adding reviewers to a pull request
 */
interface AddReviewerItem extends BaseSafeOutputItem {
  type: "add_reviewer";
  /** Array of GitHub usernames to add as reviewers */
  reviewers?: string[];
  /** Array of GitHub team slugs to add as team reviewers */
  team_reviewers?: string[];
  /** Pull request number (optional - uses triggering PR if not provided) */
  pull_request_number?: number | string;
}

/**
 * JSONL item for updating an issue
 */
interface UpdateIssueItem extends BaseSafeOutputItem {
  type: "update_issue";
  /** Optional artifacts to create or update by name on the target issue. */
  artifacts?: IssueArtifactBodyInput[];
  /** Optional new issue status */
  status?: "open" | "closed";
  /** Optional new issue title */
  title?: string;
  /** Optional new issue body */
  body?: string;
  /** Optional issue number for target "*" */
  issue_number?: number | string;
  /** Optional labels to replace on the issue */
  labels?: Array<string | IssueIntentLabel>;
}

/**
 * JSONL item for updating a pull request
 */
interface UpdatePullRequestItem extends BaseSafeOutputItem {
  type: "update_pull_request";
  /** Optional new pull request title (always replaces existing title) */
  title?: string;
  /** Optional new pull request body (behavior depends on operation) */
  body?: string;
  /** Update operation for body: 'replace' (default), 'append', 'prepend', or 'replace-island' */
  operation?: "replace" | "append" | "prepend" | "replace-island";
  /** When true, updates the pull request branch with the latest base branch changes before other updates */
  update_branch?: boolean;
  /** Optional pull request number for target "*" */
  pull_request_number?: number | string;
  /** Whether the PR should be a draft (true) or ready for review (false) */
  draft?: boolean;
}

/**
 * JSONL item for pushing to a PR branch
 */
interface PushToPrBranchItem extends BaseSafeOutputItem {
  type: "push_to_pull_request_branch";
  /** Optional commit message */
  message?: string;
  /** Optional pull request number for target "*" */
  pull_request_number?: number | string;
}

/**
 * JSONL item for reporting missing tools
 */
interface MissingToolItem extends BaseSafeOutputItem {
  type: "report_missing_tool" | "missing_tool";
  /** Optional: Name of the missing tool */
  tool?: string;
  /** Reason why the tool is needed or information about the limitation */
  reason: string;
  /** Optional alternatives or workarounds */
  alternatives?: string;
}

interface MissingDataItem extends BaseSafeOutputItem {
  type: "report_missing_data" | "missing_data";
  data_type?: string;
  reason?: string;
  context?: string;
  alternatives?: string;
}

/**
 * JSONL item for uploading an asset file
 */
interface UploadAssetItem extends BaseSafeOutputItem {
  type: "upload_asset";
  /** File path to upload */
  file_path: string;
}

/**
 * JSONL item for assigning an issue to a milestone
 */
interface AssignMilestoneItem extends BaseSafeOutputItem {
  type: "assign_milestone";
  /** Issue number to assign milestone to */
  issue_number: number | string;
  /** Milestone number to assign. Either milestone_number or milestone_title must be provided. */
  milestone_number?: number | string;
  /** Milestone title to assign (e.g., "v1.0"). Resolved to a number internally. Either milestone_number or milestone_title must be provided. */
  milestone_title?: string;
}

/**
 * JSONL item for setting the type of a GitHub issue
 */
interface SetIssueTypeItem extends BaseSafeOutputItem, IssueIntentMetadata {
  type: "set_issue_type";
  /** Issue type name to set (e.g., "Bug", "Feature"). Use empty string "" to clear the type. */
  issue_type: string;
  /** Issue number (optional - uses triggering issue if not provided) */
  issue_number?: number | string;
}

/**
 * JSONL item for setting a custom issue field value
 */
interface SetIssueFieldItem extends BaseSafeOutputItem, IssueIntentMetadata {
  type: "set_issue_field";
  /** Issue field name to set (e.g., "Priority", "Severity"). */
  field_name?: string;
  /** Optional issue field GraphQL node ID to skip name-based discovery. */
  field_node_id?: string;
  /** Field value to set. For single-select fields, provide the option name. */
  value: string;
  /** Issue number (optional - uses triggering issue if not provided) */
  issue_number?: number | string;
}

/**
 * JSONL item for assigning a GitHub Copilot coding agent to an issue or project item
 */
interface AssignToAgentItem extends BaseSafeOutputItem, IssueIntentMetadata {
  type: "assign_to_agent";
  /** Issue number to assign agent to */
  issue_number: number | string;
  /** Agent name or slug (defaults to 'copilot' if not provided) */
  agent?: string;
}

/**
 * JSONL item for updating a release
 */
interface UpdateReleaseItem extends BaseSafeOutputItem {
  type: "update_release";
  /** Tag name of the release to update (optional - inferred from context if missing) */
  tag?: string;
  /** Update operation: 'replace', 'append', or 'prepend' */
  operation: "replace" | "append" | "prepend";
  /** Content to set or append to the release body */
  body: string;
}

/**
 * JSONL item for no-op (logging only)
 */
interface NoOpItem extends BaseSafeOutputItem {
  type: "noop";
  /** Message to log for transparency */
  message: string;
}

/**
 * JSONL item for linking an issue as a sub-issue of a parent issue
 */
interface LinkSubIssueItem extends BaseSafeOutputItem {
  type: "link_sub_issue";
  /** Parent issue number to link the sub-issue to */
  parent_issue_number: number | string;
  /** Issue number to link as a sub-issue */
  sub_issue_number: number | string;
}

/**
 * JSONL item for hiding a comment
 */
interface HideCommentItem extends BaseSafeOutputItem {
  type: "hide_comment";
  /** GraphQL node ID of the comment to hide (e.g., 'IC_kwDOABCD123456') */
  comment_id: string;
  /** Optional reason for hiding the comment (default: SPAM) */
  reason?: "SPAM" | "ABUSE" | "OFF_TOPIC" | "OUTDATED" | "RESOLVED" | "LOW_QUALITY";
}

/**
 * JSONL item for replying to a pull request review comment
 */
interface ReplyToPullRequestReviewCommentItem extends BaseSafeOutputItem {
  type: "reply_to_pull_request_review_comment";
  /** The numeric ID of the review comment to reply to */
  comment_id: number | string;
  /** The reply body text in Markdown */
  body: string;
  /** Optional PR number (required when target is "*") */
  pull_request_number?: number | string;
}

/**
 * JSONL item for dismissing a pull request review
 */
interface DismissPullRequestReviewItem extends BaseSafeOutputItem {
  type: "dismiss_pull_request_review";
  /** Numeric review ID to dismiss, or "auto" to resolve latest dismissible review by current actor */
  review_id: number | string;
  /** Dismissal justification (minimum 20 characters) */
  justification: string;
  /** Optional review author login, must match workflow actor */
  author?: string;
  /** Optional PR number (required when target is "*") */
  pull_request_number?: number | string;
}

/**
 * JSONL item for creating a GitHub Project V2
 */
interface CreateProjectItem extends BaseSafeOutputItem {
  type: "create_project";
  /** Project title */
  title?: string;
  /** Owner login (organization or user) for the project */
  owner?: string;
  /** Owner type: 'org' or 'user' (default: 'org') */
  owner_type?: "org" | "user";
  /** Optional item URL to add to the project (e.g., issue URL) */
  item_url?: string;
}

/**
 * JSONL item for adding an autofix to a code scanning alert
 */
interface AutofixCodeScanningAlertItem extends BaseSafeOutputItem {
  type: "autofix_code_scanning_alert";
  /** The security alert number to create an autofix for */
  alert_number: number | string;
  /** Description of the fix being applied */
  fix_description: string;
  /** The code changes to apply as the autofix */
  fix_code: string;
}

/**
 * JSONL item for resolving a review thread on a pull request
 */
interface ResolvePullRequestReviewThreadItem extends BaseSafeOutputItem {
  type: "resolve_pull_request_review_thread";
  /** The node ID of the review thread to resolve (e.g., 'PRRT_kwDOABCD...') */
  thread_id: string;
}

/**
 * Union type of all possible safe output items
 */
type SafeOutputItem =
  | CreateSecurityAdvisoryItem
  | CreateIssueItem
  | CreateDiscussionItem
  | UpdateDiscussionItem
  | CloseDiscussionItem
  | CloseIssueItem
  | ClosePullRequestItem
  | MarkPullRequestAsReadyForReviewItem
  | ApproveWorkflowRunItem
  | AddCommentItem
  | CommentMemoryItem
  | CreatePullRequestItem
  | CreatePullRequestReviewCommentItem
  | CreateCodeScanningAlertItem
  | UploadCodeCoverageItem
  | AddLabelsItem
  | RemoveLabelsItem
  | AddReviewerItem
  | UpdateIssueItem
  | UpdatePullRequestItem
  | PushToPrBranchItem
  | MissingToolItem
  | MissingDataItem
  | UploadAssetItem
  | AssignMilestoneItem
  | SetIssueTypeItem
  | SetIssueFieldItem
  | AssignToAgentItem
  | UpdateReleaseItem
  | NoOpItem
  | LinkSubIssueItem
  | HideCommentItem
  | ReplyToPullRequestReviewCommentItem
  | DismissPullRequestReviewItem
  | CreateProjectItem
  | AutofixCodeScanningAlertItem
  | ResolvePullRequestReviewThreadItem;

/**
 * Sanitized safe output items
 */
interface SafeOutputItems {
  items: SafeOutputItem[];
}

// === Export JSONL types ===
export {
  CreateSecurityAdvisoryItem,
  // JSONL item types
  BaseSafeOutputItem,
  IssueArtifactType,
  IssueArtifactInput,
  IssueArtifactBodyInput,
  IssueArtifact,
  CreatedIssueArtifact,
  CreateIssueItem,
  CreateDiscussionItem,
  UpdateDiscussionItem,
  CloseDiscussionItem,
  CloseIssueItem,
  ClosePullRequestItem,
  MarkPullRequestAsReadyForReviewItem,
  ApproveWorkflowRunItem,
  AddCommentItem,
  CommentMemoryItem,
  CreatePullRequestItem,
  CreatePullRequestReviewCommentItem,
  CreateCodeScanningAlertItem,
  UploadCodeCoverageItem,
  AddLabelsItem,
  RemoveLabelsItem,
  AddReviewerItem,
  UpdateIssueItem,
  UpdatePullRequestItem,
  PushToPrBranchItem,
  MissingToolItem,
  MissingDataItem,
  UploadAssetItem,
  AssignMilestoneItem,
  SetIssueTypeItem,
  SetIssueFieldItem,
  AssignToAgentItem,
  UpdateReleaseItem,
  NoOpItem,
  LinkSubIssueItem,
  HideCommentItem,
  ReplyToPullRequestReviewCommentItem,
  DismissPullRequestReviewItem,
  AutofixCodeScanningAlertItem,
  ResolvePullRequestReviewThreadItem,
  SafeOutputItem,
  SafeOutputItems,
};
