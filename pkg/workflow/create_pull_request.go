package workflow

import (
	"errors"

	"github.com/github/gh-aw/pkg/logger"
)

var createPRLog = logger.New("workflow:create_pull_request")

var createPRStringOrArrayFields = []string{"reviewers", "team-reviewers", "assignees"}
var createPRExpressionArrayFields = []string{"labels", "allowed-repos", "allowed-base-branches", "allowed-branches"}

// getFallbackAsIssue returns the effective fallback-as-issue setting (defaults to true).
func getFallbackAsIssue(config *CreatePullRequestsConfig) bool {
	if config == nil || config.FallbackAsIssue == nil {
		return true // Default
	}
	return *config.FallbackAsIssue
}

// isCloseOlderPullRequestsEnabled returns true when close-older-pull-requests is
// configured and not explicitly set to false ("false" or "0"). Any other non-empty
// value, including GitHub Actions expressions like "${{ ... }}", is treated as enabled.
// Used for compile-time permission calculation.
func isCloseOlderPullRequestsEnabled(config *CreatePullRequestsConfig) bool {
	if config == nil || config.Enabled == nil {
		return false
	}
	v := *config.Enabled
	return v != "" && v != "false" && v != "0"
}

// isStackedPullRequestsEnabled returns the effective `stacked` setting (defaults to true).
// Instances without stacked pull request support (e.g. GitHub Enterprise Server) turn the feature
// off with `stacked: false`.
func isStackedPullRequestsEnabled(config *CreatePullRequestsConfig) bool {
	if config == nil || config.Stacked == nil {
		return true
	}
	return *config.Stacked
}

func validateSteeringIssue(data *WorkflowData) error {
	if data == nil || data.SafeOutputs == nil || !data.SafeOutputs.Steer {
		return nil
	}

	if isTemplatableStagedExpression(data.SafeOutputs.Staged) {
		return errors.New("safe-outputs.steer cannot be combined with an expression-valued staged option")
	}
	if isSteeringIssueStaged(data) {
		return nil
	}
	if data.SafeOutputs.FailureIssueRepo != "" {
		return errors.New("safe-outputs.steer cannot be combined with safe-outputs.failure-issue-repo because the steering issue is reused for failure reporting")
	}
	return nil
}

func validateSteeringIssuePermissions(data *WorkflowData, permissions *Permissions) error {
	if !isSteeringIssueEnabled(data) {
		return nil
	}
	if permissions == nil {
		return errors.New("safe-outputs.steer requires issues: read, which is required to read steering issue comments")
	}
	level, ok := permissions.Get(PermissionIssues)
	if !ok || (level != PermissionRead && level != PermissionWrite) {
		return errors.New("safe-outputs.steer requires issues: read, which is required to read steering issue comments")
	}
	return nil
}

// isTemplatableStagedExpression reports whether a staged value is a GitHub Actions
// expression that can only be resolved at runtime.
func isTemplatableStagedExpression(value *TemplatableBool) bool {
	return value != nil && isExpression(value.String())
}

// CreatePullRequestsConfig holds configuration for creating GitHub pull requests from agent output
type CreatePullRequestsConfig struct {
	BaseSafeOutputConfig           `yaml:",inline"`
	SafeOutputAllowedLabelsConfig  `yaml:",inline"`
	BranchPrefix                   string           `yaml:"branch-prefix,omitempty"` // Optional prefix for the pull request branch name (e.g. "signed/"). Applied before the agent-specified or auto-generated branch name.
	TitlePrefix                    string           `yaml:"title-prefix,omitempty"`
	RequireTemporaryID             bool             `yaml:"require-temporary-id,omitempty"` // When true, create_pull_request tool calls must include temporary_id.
	Labels                         []string         `yaml:"labels,omitempty"`
	Reviewers                      []string         `yaml:"reviewers,omitempty"`                           // List of users/bots to assign as reviewers to the pull request. Accepts a static list or a single GitHub Actions expression.
	TeamReviewers                  []string         `yaml:"team-reviewers,omitempty"`                      // List of team slugs to assign as team reviewers to the pull request. Accepts a static list or a single GitHub Actions expression.
	Assignees                      []string         `yaml:"assignees,omitempty"`                           // List of users to assign to the created pull request and any fallback issue. Accepts a static list or a single GitHub Actions expression.
	FallbackLabels                 []string         `yaml:"fallback-labels,omitempty"`                     // List of labels to apply to fallback issues created when PR creation cannot proceed. If omitted, fallback issues reuse PR labels.
	Draft                          *string          `yaml:"draft,omitempty"`                               // Pointer to distinguish between unset (nil), literal bool, and expression values
	IfNoChanges                    string           `yaml:"if-no-changes,omitempty"`                       // Behavior when no changes to push: "warn" (default), "error", or "ignore"
	AllowEmpty                     *string          `yaml:"allow-empty,omitempty"`                         // Allow creating PR without patch file or with empty patch (useful for preparing feature branches)
	TargetRepoSlug                 string           `yaml:"target-repo,omitempty"`                         // Target repository in format "owner/repo" for cross-repository pull requests
	HeadRepoSlug                   string           `yaml:"head-repo,omitempty"`                           // Head repository in format "owner/repo" for fork-backed pull requests; defaults to target-repo when unset
	HeadGitHubToken                string           `yaml:"head-github-token,omitempty"`                   // GitHub token used for branch writes to the head repository when it differs from the target repo
	HeadGitHubApp                  *GitHubAppConfig `yaml:"-"`                                             // GitHub App used to mint the head token for fork branch writes; parsed manually to support app-id alias
	AllowedRepos                   []string         `yaml:"allowed-repos,omitempty"`                       // List of additional repositories that pull requests can be created in (additionally to the target-repo)
	AllowedBaseBranches            []string         `yaml:"allowed-base-branches,omitempty"`               // List of allowed base branch globs (e.g. "release/*"). Enables agent-provided `base` override when configured.
	AllowedBranches                []string         `yaml:"allowed-branches,omitempty"`                    // List of allowed source branch globs (e.g. "feature/*"). Branch in create_pull_request payload must match when configured.
	Stacked                        *bool            `yaml:"stacked,omitempty"`                             // When false, rejects any pull request whose base branch is not the default base branch. Defaults to true; set to false on GitHub Enterprise Server instances without stacked pull request support.
	MaxPatchSize                   int              `yaml:"max-patch-size,omitempty"`                      // Maximum allowed patch size in KB for create-pull-request only. Overrides safe-outputs.max-patch-size when set.
	MaxPatchFiles                  int              `yaml:"max-patch-files,omitempty"`                     // Maximum allowed unique files in create-pull-request patch only. Overrides safe-outputs.max-patch-files when set.
	Expires                        int              `yaml:"expires,omitempty"`                             // Hours until the pull request expires and should be automatically closed (only for same-repo PRs)
	AutoMerge                      *string          `yaml:"auto-merge,omitempty"`                          // Enable auto-merge for the pull request; accepts true/false or merge method strings squash|merge|rebase
	BaseBranch                     string           `yaml:"base-branch,omitempty"`                         // Base branch for the pull request (defaults to github.ref_name if not specified)
	FallbackAsIssue                *bool            `yaml:"fallback-as-issue,omitempty"`                   // When true (default), creates an issue if PR creation fails. When false, no fallback occurs and issues: write permission is not requested.
	AutoCloseIssue                 *string          `yaml:"auto-close-issue,omitempty"`                    // Auto-add "Fixes #N" closing keyword when triggered from an issue (default: true). Set to false to prevent auto-closing the triggering issue on PR merge. Accepts a boolean or a GitHub Actions expression.
	GithubTokenForExtraEmptyCommit string           `yaml:"github-token-for-extra-empty-commit,omitempty"` // Token used to push an empty commit to trigger CI events. Use a PAT or "app" for GitHub App auth.
	ManifestFilesPolicy            *string          `yaml:"protected-files,omitempty"`                     // Controls protected-file protection: "request_review" (default) creates a PR and submits a REQUEST_CHANGES review, "blocked" hard-blocks, "allowed" permits all changes, and "fallback-to-issue" creates a review issue instead of a PR.
	ProtectedFilesExclude          []string         `yaml:"-"`                                             // Files/prefixes to exclude from the default protected list (from object-form protected-files.exclude). Not sourced from YAML directly; populated during pre-processing.
	AllowedFiles                   []string         `yaml:"allowed-files,omitempty"`                       // Strict allowlist of glob patterns for files eligible for create. Checked independently of protected-files; both checks must pass.
	ExcludedFiles                  []string         `yaml:"excluded-files,omitempty"`                      // List of glob patterns for files to exclude from the patch using git :(exclude) pathspecs. Matching files are stripped by git at generation time and will not appear in the commit or be subject to allowed-files or protected-files checks.
	PreserveBranchName             bool             `yaml:"preserve-branch-name,omitempty"`                // When true, skips the random salt suffix on agent-specified branch names. Invalid characters are still replaced for security; casing is always preserved. Useful when CI enforces branch naming conventions (e.g. Jira keys in uppercase).
	RecreateRef                    bool             `yaml:"recreate-ref,omitempty"`                        // When true (and preserve-branch-name is true), allows the handler to force-delete an existing remote branch ref and recreate it from the agent's local HEAD. When false (default), an existing remote branch causes a fallback to issue (or push_failed). Useful for long-lived reusable branches whose previous PR was merged.
	PatchFormat                    string           `yaml:"patch-format,omitempty"`                        // Transport format for packaging changes: "bundle" (default, uses git bundle and preserves merge topology/per-commit metadata) or "am" (uses git format-patch).
	SignedCommits                  *bool            `yaml:"signed-commits,omitempty"`                      // When false, skips GitHub GraphQL signed commits and pushes the local git history directly. Default is true.
	AllowWorkflows                 bool             `yaml:"allow-workflows,omitempty"`                     // When true, adds workflows: write to the GitHub App token. Requires safe-outputs.github-app to be configured.
	CloseOlderConfig               `yaml:",inline"` // Shared close-older settings; Enabled is sourced from close-older-pull-requests.
}

// parseCreatePullRequestsConfig handles only create-pull-request (singular) configuration.
//
//nolint:funlen // Large config parser keeps all create-pull-request validation in one place for clarity.
func (c *Compiler) parseCreatePullRequestsConfig(outputMap map[string]any) *CreatePullRequestsConfig {
	// Check for singular form only
	if _, exists := outputMap["create-pull-request"]; !exists {
		createPRLog.Print("No create-pull-request configuration found")
		return nil
	}

	var protectedFilesExclude []string
	config := parseCreateEntityConfig(
		outputMap,
		"create-pull-request",
		CreateParseOptions{
			BoolFields:    []string{"draft", "allow-empty", "footer", "auto-close-issue", "close-older-pull-requests"},
			IntFields:     []string{"max"},
			HandleExpires: true,
		},
		createPRLog,
		func(err error) *CreatePullRequestsConfig {
			createPRLog.Printf("Failed to unmarshal config: %v", err)
			// For backward compatibility, handle nil/empty config
			return &CreatePullRequestsConfig{}
		},
		func(configData map[string]any) bool {
			coerceStringOrArrayFields(configData, createPRStringOrArrayFields, createPRLog)

			// Pre-process protected-files: supports string enum OR object form {policy, exclude}.
			// Object form is preprocessed to extract the policy (stored back as string) and
			// the exclude list (stored in a local variable and assigned to the config after unmarshaling).
			protectedFilesExclude = preprocessProtectedFilesField(configData, createPRLog)

			// Validate protected-files string enum after object-form preprocessing.
			validateStringEnumField(configData, "protected-files", []string{"blocked", "allowed", "fallback-to-issue", "request_review", "request-review"}, createPRLog)
			if value, ok := configData["protected-files"].(string); ok {
				configData["protected-files"] = normaliseProtectedFilesPolicy(value)
			}

			// Pre-process patch-format: valid values are "bundle" (default) and "am".
			validateStringEnumField(configData, "patch-format", []string{"am", "bundle"}, createPRLog)

			if val, exists := configData["auto-merge"]; exists {
				switch v := val.(type) {
				case bool:
					if v {
						configData["auto-merge"] = "true"
					} else {
						configData["auto-merge"] = "false"
					}
				case string:
					if !isExpression(v) && v != "true" && v != "false" && v != "squash" && v != "merge" && v != "rebase" {
						createPRLog.Printf("Invalid auto-merge value %q, ignoring", v)
						delete(configData, "auto-merge")
					}
				default:
					createPRLog.Printf("Invalid auto-merge value type %T, ignoring", val)
					delete(configData, "auto-merge")
				}
			}

			// Pre-process list fields that also accept a GitHub Actions expression string.
			// An expression is wrapped in a single-element []string so the []string struct field
			// can receive it after YAML unmarshaling; the handler config builder later re-emits it
			// as a JSON string for runtime evaluation.
			for _, field := range createPRExpressionArrayFields {
				if err := preprocessStringArrayFieldAsTemplatable(configData, field, createPRLog); err != nil {
					createPRLog.Printf("Invalid %s value: %v", field, err)
					return false
				}
			}

			return true
		},
		func(configData map[string]any, config *CreatePullRequestsConfig, expiresDisabled bool) {
			config.Enabled = closeOlderEnabledFromConfigData(configData, "close-older-pull-requests")

			if expiresDisabled {
				createPRLog.Print("Pull request expiration disabled")
			}

			// Log expires if configured
			if config.Expires > 0 {
				createPRLog.Printf("Pull request expiration configured: %d hours", config.Expires)
			}

			// Apply the exclude list extracted from the object-form protected-files field.
			config.ProtectedFilesExclude = protectedFilesExclude

			// Set default max if not explicitly configured (default is 1)
			if config.Max == nil {
				config.Max = defaultIntStr(1)
				createPRLog.Print("Using default max count: 1")
			} else {
				createPRLog.Printf("Pull request max count configured: %s", *config.Max)
			}

			// Parse head-github-app manually so that the app-id alias is honoured
			// (YAML unmarshal would silently ignore app-id since GitHubAppConfig only
			// declares the canonical client-id tag).
			if headAppData, exists := configData["head-github-app"]; exists {
				if headAppMap, ok := headAppData.(map[string]any); ok {
					createPRLog.Print("Parsed head-github-app from config")
					config.HeadGitHubApp = parseAppConfig(headAppMap)
				}
			}
		},
	)
	if config == nil {
		return nil
	}

	return config
}
