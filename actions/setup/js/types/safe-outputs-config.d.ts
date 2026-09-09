// Base interface for all safe output configurations
interface SafeOutputConfig {
  type: string;
  max?: number;
  min?: number;
  "github-token"?: string;
}

// === Specific Safe Output Configuration Interfaces ===

/**
 * Configuration for creating GitHub issues
 */
interface CreateIssueConfig extends SafeOutputConfig {
  "title-prefix"?: string;
  "body-footer"?: string;
  "deduplicate-by-title"?: boolean | number;
  labels?: string[];
  "target-repo"?: string;
  "allowed-repos"?: string[];
  footer?: boolean;
}

/**
 * Configuration for creating GitHub discussions
 */
interface CreateDiscussionConfig extends SafeOutputConfig {
  "title-prefix"?: string;
  "category-id"?: string;
  "target-repo"?: string;
  "allowed-repos"?: string[];
  footer?: boolean;
}

/**
 * Configuration for closing GitHub discussions
 */
interface CloseDiscussionConfig extends SafeOutputConfig {
  "required-labels"?: string[];
  "required-title-prefix"?: string;
  "required-category"?: string;
  target?: string;
  /** When false, any body provided by the agent is silently dropped and the discussion is closed without a comment. Default: true. */
  "allow-body"?: boolean;
}

/**
 * Configuration for closing GitHub issues
 */
interface CloseIssueConfig extends SafeOutputConfig {
  "required-labels"?: string[];
  "required-title-prefix"?: string;
  target?: string;
  /** When false, any body provided by the agent is silently dropped and the issue is closed without a comment. Default: true. */
  "allow-body"?: boolean;
}

/**
 * Configuration for closing GitHub pull requests
 */
interface ClosePullRequestConfig extends SafeOutputConfig {
  "required-labels"?: string[];
  "required-title-prefix"?: string;
  target?: string;
}

/**
 * Configuration for marking pull requests as ready for review
 */
interface MarkPullRequestAsReadyForReviewConfig extends SafeOutputConfig {
  "required-labels"?: string[];
  "required-title-prefix"?: string;
  target?: string;
}

/**
 * Configuration for approving pending workflow runs awaiting required approval
 */
interface ApproveWorkflowRunConfig extends SafeOutputConfig {
  fork?: boolean;
  comment?: boolean;
  "allowed-workflows": string[];
}

/**
 * Configuration for adding comments to issues or PRs
 */
interface AddCommentConfig extends SafeOutputConfig {
  target?: string;
  "target-repo"?: string;
}

/**
 * Configuration for creating pull requests
 */
interface CreatePullRequestConfig extends SafeOutputConfig {
  "title-prefix"?: string;
  "body-footer"?: string;
  labels?: string[];
  reviewers?: string | string[];
  "team-reviewers"?: string | string[];
  assignees?: string | string[];
  draft?: boolean;
  "if-no-changes"?: string;
  "target-repo"?: string;
  "head-repo"?: string;
  "allowed-repos"?: string[];
  "head-github-token"?: string;
  "base-branch"?: string;
  "allowed-branches"?: string[];
  footer?: boolean;
  "auto-close-issue"?: boolean | string;
}

/**
 * Configuration for creating pull request review comments
 */
interface CreatePullRequestReviewCommentConfig extends SafeOutputConfig {
  side?: string;
  target?: string;
}

/**
 * Configuration for submitting a consolidated PR review.
 * The footer field controls when AI-generated footer is added to the review body:
 * - "always" (default): Always include footer
 * - "none": Never include footer
 * - "if-body": Only include footer when review has body text
 * Boolean values are also supported: true maps to "always", false maps to "none".
 */
interface SubmitPullRequestReviewConfig extends SafeOutputConfig {
  target?: string;
  "target-repo"?: string;
  "allowed-repos"?: string[];
  "allowed-events"?: Array<"APPROVE" | "COMMENT" | "REQUEST_CHANGES">;
  footer?: boolean | "always" | "none" | "if-body";
}

/**
 * Configuration for replying to pull request review comments.
 * Inherits common fields (e.g. "github-token") from SafeOutputConfig.
 */
interface ReplyToPullRequestReviewCommentConfig extends SafeOutputConfig {
  target?: string;
  "target-repo"?: string;
  "allowed-repos"?: string[];
  footer?: boolean;
}

/**
 * Configuration for resolving pull request review threads.
 * Resolution is scoped to the triggering PR only.
 *
 * Inherits common fields (e.g. "github-token") from SafeOutputConfig.
 * The only new field explicitly supported on this interface is "max".
 */
interface ResolvePullRequestReviewThreadConfig extends SafeOutputConfig {
  // "max" is the only field added beyond those inherited from SafeOutputConfig
}

/**
 * Configuration for creating code scanning alerts
 */
interface CreateCodeScanningAlertConfig extends SafeOutputConfig {
  driver?: string;
}

/**
 * Configuration for uploading a code coverage report via actions/upload-code-coverage
 */
interface UploadCodeCoverageConfig extends SafeOutputConfig {
  "fail-on-error"?: boolean;
  "wait-for-processing-timeout"?: number;
}

/**
 * Configuration for adding code scanning autofixes
 */
interface AutofixCodeScanningAlertConfig extends SafeOutputConfig {
  // No additional configuration beyond base config
}

/**
 * Configuration for adding labels to issues or PRs
 */
interface AddLabelsConfig extends SafeOutputConfig {
  allowed?: string[];
}

/**
 * Configuration for adding reviewers to pull requests
 */
interface AddReviewerConfig extends SafeOutputConfig {
  reviewers?: string[];
  "team-reviewers"?: string[];
  target?: string;
}

/**
 * Configuration for updating issues
 */
interface UpdateIssueConfig extends SafeOutputConfig {
  status?: boolean;
  target?: string;
  title?: boolean;
  body?: boolean;
  footer?: boolean;
}

/**
 * Configuration for updating discussions
 */
interface UpdateDiscussionConfig extends SafeOutputConfig {
  target?: string;
  title?: boolean;
  body?: boolean;
  footer?: boolean;
}

/**
 * Configuration for updating pull requests
 */
interface UpdatePullRequestConfig extends SafeOutputConfig {
  target?: string;
  title?: boolean;
  body?: boolean;
}

/**
 * Configuration for pushing to pull request branches
 */
interface PushToPullRequestBranchConfig extends SafeOutputConfig {
  target?: string;
  "required-title-prefix"?: string;
  "title-prefix"?: string;
  "required-labels"?: string[];
  labels?: string[];
  "if-no-changes"?: string;
  "target-repo"?: string;
  "head-repo"?: string;
  "allowed-repos"?: string[];
  "head-github-token"?: string;
  "base-branch"?: string;
}

/**
 * Configuration for merging pull requests with policy checks.
 */
interface MergePullRequestConfig extends SafeOutputConfig {
  "required-labels"?: string[];
  "allowed-labels"?: string[];
  "allowed-branches"?: string[];
  "allowed-files"?: string[];
  "protected-files"?: string[];
}

/**
 * Configuration for uploading assets
 */
interface UploadAssetConfig extends SafeOutputConfig {
  branch?: string;
  "max-size"?: number;
  "allowed-exts"?: string[];
}

/**
 * Configuration for assigning milestones
 */
interface AssignMilestoneConfig extends SafeOutputConfig {
  allowed?: string[];
  target?: string;
  /** When true, missing milestones from the allowed list are created automatically before assignment */
  auto_create?: boolean;
}

/**
 * Configuration for setting the type of an issue
 */
interface SetIssueTypeConfig extends SafeOutputConfig {
  allowed?: string[];
  target?: string;
}

/**
 * Configuration for assigning agents to issues
 */
interface AssignToAgentConfig extends SafeOutputConfig {
  name?: string;
  model?: string;
  "custom-agent"?: string;
  "custom-instructions"?: string;
  allowed?: string[];
  target?: string;
  "target-repo"?: string;
  "pull-request-repo"?: string;
  "allowed-pull-request-repos"?: string[];
  "base-branch"?: string;
  "ignore-if-error"?: boolean;
}

/**
 * Configuration for updating releases
 */
interface UpdateReleaseConfig extends SafeOutputConfig {
  target?: string;
  footer?: boolean;
}

/**
 * Configuration for no-op output
 */
interface NoOpConfig extends SafeOutputConfig {}

/**
 * Configuration for reporting missing tools
 */
interface MissingToolConfig extends SafeOutputConfig {}

/**
 * Configuration for link-sub-issue output
 */
interface LinkSubIssueConfig extends SafeOutputConfig {
  "parent-required-labels"?: string[];
  "parent-title-prefix"?: string;
  "sub-required-labels"?: string[];
  "sub-title-prefix"?: string;
  "target-repo"?: string;
}

/**
 * Configuration for threat detection
 */
interface ThreatDetectionConfig extends SafeOutputConfig {
  enabled?: boolean;
  steps?: any[];
}

// === Safe Job Configuration Interfaces ===

/**
 * Safe job input parameter configuration
 */
interface SafeJobInput {
  description?: string;
  required?: boolean;
  default?: string;
  type?: string;
  options?: string[];
}

/**
 * Safe job configuration item
 */
interface SafeJobConfig {
  name?: string;
  "runs-on"?: any;
  if?: string;
  needs?: string[];
  steps?: any[];
  env?: Record<string, string>;
  permissions?: Record<string, string>;
  inputs?: Record<string, SafeJobInput>;
  "github-token"?: string;
  output?: string;
}

// Union type of all specific safe output configurations
type SpecificSafeOutputConfig =
  | CreateIssueConfig
  | CreateDiscussionConfig
  | UpdateDiscussionConfig
  | CloseDiscussionConfig
  | CloseIssueConfig
  | ClosePullRequestConfig
  | MarkPullRequestAsReadyForReviewConfig
  | ApproveWorkflowRunConfig
  | AddCommentConfig
  | CreatePullRequestConfig
  | CreatePullRequestReviewCommentConfig
  | SubmitPullRequestReviewConfig
  | CreateCodeScanningAlertConfig
  | UploadCodeCoverageConfig
  | AutofixCodeScanningAlertConfig
  | AddLabelsConfig
  | AddReviewerConfig
  | UpdateIssueConfig
  | UpdatePullRequestConfig
  | MergePullRequestConfig
  | PushToPullRequestBranchConfig
  | UploadAssetConfig
  | AssignMilestoneConfig
  | SetIssueTypeConfig
  | AssignToAgentConfig
  | UpdateReleaseConfig
  | NoOpConfig
  | MissingToolConfig
  | LinkSubIssueConfig
  | ReplyToPullRequestReviewCommentConfig
  | ThreatDetectionConfig
  | ResolvePullRequestReviewThreadConfig;

type SafeOutputConfigs = Record<string, SafeOutputConfig | SpecificSafeOutputConfig>;

export {
  SafeOutputConfig,
  SafeOutputConfigs,
  // Specific configuration types
  CreateIssueConfig,
  CreateDiscussionConfig,
  UpdateDiscussionConfig,
  CloseDiscussionConfig,
  CloseIssueConfig,
  ClosePullRequestConfig,
  MarkPullRequestAsReadyForReviewConfig,
  ApproveWorkflowRunConfig,
  AddCommentConfig,
  CreatePullRequestConfig,
  CreatePullRequestReviewCommentConfig,
  SubmitPullRequestReviewConfig,
  CreateCodeScanningAlertConfig,
  UploadCodeCoverageConfig,
  AutofixCodeScanningAlertConfig,
  AddLabelsConfig,
  AddReviewerConfig,
  UpdateIssueConfig,
  UpdatePullRequestConfig,
  MergePullRequestConfig,
  PushToPullRequestBranchConfig,
  UploadAssetConfig,
  AssignMilestoneConfig,
  SetIssueTypeConfig,
  AssignToAgentConfig,
  UpdateReleaseConfig,
  NoOpConfig,
  MissingToolConfig,
  LinkSubIssueConfig,
  ReplyToPullRequestReviewCommentConfig,
  ThreatDetectionConfig,
  ResolvePullRequestReviewThreadConfig,
  SpecificSafeOutputConfig,
  // Safe job configuration types
  SafeJobInput,
  SafeJobConfig,
};
