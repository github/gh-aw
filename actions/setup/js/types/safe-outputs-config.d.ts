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
}

/**
 * Configuration for closing GitHub issues
 */
interface CloseIssueConfig extends SafeOutputConfig {
  "required-labels"?: string[];
  "required-title-prefix"?: string;
  target?: string;
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
  labels?: string[];
  draft?: boolean;
  "if-no-changes"?: string;
  footer?: boolean;
}

/**
 * Configuration for creating pull request review comments
 */
interface CreatePullRequestReviewCommentConfig extends SafeOutputConfig {
  side?: string;
  target?: string;
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
  "title-prefix"?: string;
  labels?: string[];
  "if-no-changes"?: string;
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
}

/**
 * Configuration for assigning agents to issues
 */
interface AssignToAgentConfig extends SafeOutputConfig {
  "default-agent"?: string;
  target?: string;
  "target-repo"?: string;
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
  | AddCommentConfig
  | CreatePullRequestConfig
  | CreatePullRequestReviewCommentConfig
  | CreateCodeScanningAlertConfig
  | AutofixCodeScanningAlertConfig
  | AddLabelsConfig
  | AddReviewerConfig
  | UpdateIssueConfig
  | UpdatePullRequestConfig
  | PushToPullRequestBranchConfig
  | UploadAssetConfig
  | AssignMilestoneConfig
  | AssignToAgentConfig
  | UpdateReleaseConfig
  | NoOpConfig
  | MissingToolConfig
  | LinkSubIssueConfig
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
  AddCommentConfig,
  CreatePullRequestConfig,
  CreatePullRequestReviewCommentConfig,
  CreateCodeScanningAlertConfig,
  AutofixCodeScanningAlertConfig,
  AddLabelsConfig,
  AddReviewerConfig,
  UpdateIssueConfig,
  UpdatePullRequestConfig,
  PushToPullRequestBranchConfig,
  UploadAssetConfig,
  AssignMilestoneConfig,
  AssignToAgentConfig,
  UpdateReleaseConfig,
  NoOpConfig,
  MissingToolConfig,
  LinkSubIssueConfig,
  ThreatDetectionConfig,
  ResolvePullRequestReviewThreadConfig,
  SpecificSafeOutputConfig,
  // Safe job configuration types
  SafeJobInput,
  SafeJobConfig,
};
