package workflow

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var pushToPullRequestBranchLog = logger.New("workflow:push_to_pull_request_branch")

// PushToPullRequestBranchConfig holds configuration for pushing changes to a specific branch from agent output
type PushToPullRequestBranchConfig struct {
	BaseSafeOutputConfig           `yaml:",inline"`
	Target                         string           `yaml:"target,omitempty"`                              // Target for push-to-pull-request-branch: like add-comment but for pull requests
	TitlePrefix                    string           `yaml:"title-prefix,omitempty"`                        // Required title prefix for pull request validation
	RequiredLabels                 []string         `yaml:"required-labels,omitempty"`                     // Required labels for pull request validation
	Labels                         []string         `yaml:"labels,omitempty"`                              // Deprecated alias for required-labels
	IfNoChanges                    string           `yaml:"if-no-changes,omitempty"`                       // Behavior when no changes to push: "warn", "error", or "ignore" (default: "warn")
	IgnoreMissingBranchFailure     bool             `yaml:"ignore-missing-branch-failure,omitempty"`       // When true, missing/deleted target branches are treated as skipped instead of hard failures.
	CommitTitleSuffix              string           `yaml:"commit-title-suffix,omitempty"`                 // Optional suffix to append to generated commit titles
	GithubTokenForExtraEmptyCommit string           `yaml:"github-token-for-extra-empty-commit,omitempty"` // Token used to push an empty commit to trigger CI events. Use a PAT or "app" for GitHub App auth.
	TargetRepoSlug                 string           `yaml:"target-repo,omitempty"`                         // Target repository in format "owner/repo" for cross-repository push to pull request branch
	HeadRepoSlug                   string           `yaml:"head-repo,omitempty"`                           // Head repository in format "owner/repo" for allowed fork-backed pull request updates
	HeadGitHubToken                string           `yaml:"head-github-token,omitempty"`                   // GitHub token used for branch writes to the head repository when it differs from the target repo
	HeadGitHubApp                  *GitHubAppConfig `yaml:"-"`                                             // GitHub App used to mint the head token for fork branch writes; parsed manually to support app-id alias
	BaseBranch                     string           `yaml:"base-branch,omitempty"`                         // Base branch of the target repository for incremental patch computation. When unset, the runtime resolves it from the local checkout or the repo's default branch.
	AllowedRepos                   []string         `yaml:"allowed-repos,omitempty"`                       // List of additional repositories in format "owner/repo" that push to pull request branch can target
	ManifestFilesPolicy            *string          `yaml:"protected-files,omitempty"`                     // Controls protected-file protection: "blocked" (default) hard-blocks, "allowed" permits all changes, "fallback-to-issue" creates a review issue instead of pushing.
	ProtectedFilesExclude          []string         `yaml:"-"`                                             // Files/prefixes to exclude from the default protected list (from object-form protected-files.exclude). Not sourced from YAML directly; populated during parsing.
	AllowedFiles                   []string         `yaml:"allowed-files,omitempty"`                       // Strict allowlist of glob patterns for files eligible for push. Checked independently of protected-files; both checks must pass.
	ExcludedFiles                  []string         `yaml:"excluded-files,omitempty"`                      // List of glob patterns for files to exclude from the patch using git :(exclude) pathspecs. Matching files are stripped by git at generation time and will not appear in the commit or be subject to allowed-files or protected-files checks.
	MaxPatchSize                   int              `yaml:"max-patch-size,omitempty"`                      // Maximum allowed patch size in KB for push-to-pull-request-branch only. Overrides safe-outputs.max-patch-size when set.
	PatchFormat                    string           `yaml:"patch-format,omitempty"`                        // Transport format for packaging changes: "bundle" (default, uses git bundle and preserves merge topology/per-commit metadata) or "am" (uses git format-patch).
	FallbackAsPullRequest          *bool            `yaml:"fallback-as-pull-request,omitempty"`            // When true (default), creates a fallback pull request if direct push fails due to diverged/non-fast-forward branch. When false, fallback is disabled and pull-requests: write is not requested.
	SignedCommits                  *bool            `yaml:"signed-commits,omitempty"`                      // When false, skips GitHub GraphQL signed commits and pushes the local git history directly. Default is true.
	AllowWorkflows                 bool             `yaml:"allow-workflows,omitempty"`                     // When true, adds workflows: write to the GitHub App token. Requires safe-outputs.github-app to be configured.
	CheckBranchProtection          *bool            `yaml:"check-branch-protection,omitempty"`             // When false, skips the branch protection API pre-flight check. Default is true (check enabled). Set to false to avoid needing administration: read permission.
}

// buildCheckoutRepository generates a checkout step with optional target repository and custom token
// Parameters:
//   - steps: existing steps to append to
//   - c: compiler instance for trialMode checks
//   - targetRepoSlug: optional target repository (e.g., "org/repo") for cross-repo operations
//     If empty, checks out the source repository (github.repository)
//     If set, checks out the specified target repository
//   - customToken: optional custom GitHub token for authentication
//     If empty, uses default GH_AW_GITHUB_TOKEN || GITHUB_TOKEN fallback
func buildCheckoutRepository(steps []string, c *Compiler, targetRepoSlug string, customToken string) []string {
	pushToPullRequestBranchLog.Printf("Building checkout repository step: targetRepo=%s, trialMode=%t", targetRepoSlug, c.trialMode)

	steps = append(steps, "      - name: Checkout repository\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getActionPin("actions/checkout")))
	steps = append(steps, "        with:\n")

	// Determine which repository to check out
	// Priority: targetRepoSlug > trialLogicalRepoSlug > default (source repo)
	effectiveTargetRepo := targetRepoSlug
	if c.trialMode && c.trialLogicalRepoSlug != "" {
		effectiveTargetRepo = c.trialLogicalRepoSlug
		pushToPullRequestBranchLog.Printf("Trial mode: using logical repo slug: %s", effectiveTargetRepo)
	}

	// Set repository parameter if we're checking out a different repo
	if effectiveTargetRepo != "" {
		pushToPullRequestBranchLog.Printf("Checking out non-default repository: %s", effectiveTargetRepo)
		steps = append(steps, fmt.Sprintf("          repository: %s\n", effectiveTargetRepo))
	}

	steps = append(steps, "          persist-credentials: false\n")
	steps = append(steps, "          fetch-depth: 0\n")

	// Add token for trial mode or when checking out a different repository
	if c.trialMode || targetRepoSlug != "" {
		// Use custom token if provided, otherwise use default fallback
		token := customToken
		if token == "" {
			token = "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
		}
		pushToPullRequestBranchLog.Printf("Adding authentication token to checkout step (customToken=%t)", customToken != "")
		steps = append(steps, fmt.Sprintf("          token: %s\n", token))
	}

	return steps
}

// parsePushToPullRequestBranchConfig handles push-to-pull-request-branch configuration
func (c *Compiler) parsePushToPullRequestBranchConfig(outputMap map[string]any) *PushToPullRequestBranchConfig {
	if configData, exists := outputMap["push-to-pull-request-branch"]; exists {
		pushToPullRequestBranchLog.Print("Parsing push-to-pull-request-branch configuration")
		pushToBranchConfig := &PushToPullRequestBranchConfig{
			IfNoChanges: "warn", // Default behavior: warn when no changes
		}

		// Handle the case where configData is nil (push-to-pull-request-branch: with no value)
		if configData == nil {
			return pushToBranchConfig
		}

		if configMap, ok := configData.(map[string]any); ok {
			// Parse target (optional, similar to add-comment)
			if target, exists := configMap["target"]; exists {
				if targetStr, ok := target.(string); ok {
					pushToBranchConfig.Target = targetStr
				}
			}

			// Parse if-no-changes (optional, defaults to "warn")
			if ifNoChanges, exists := configMap["if-no-changes"]; exists {
				if ifNoChangesStr, ok := ifNoChanges.(string); ok {
					// Validate the value
					switch ifNoChangesStr {
					case "warn", "error", "ignore":
						pushToBranchConfig.IfNoChanges = ifNoChangesStr
					default:
						// Invalid value, use default and log warning
						if c.verbose {
							fmt.Fprintf(os.Stderr, "Warning: invalid if-no-changes value '%s', using default 'warn'\n", ifNoChangesStr)
						}
						pushToBranchConfig.IfNoChanges = "warn"
					}
				}
			}

			// Parse ignore-missing-branch-failure (optional, defaults to false)
			if ignoreMissingBranchFailure, exists := configMap["ignore-missing-branch-failure"]; exists {
				if ignoreMissingBranchFailureBool, ok := ignoreMissingBranchFailure.(bool); ok {
					pushToBranchConfig.IgnoreMissingBranchFailure = ignoreMissingBranchFailureBool
				}
			}

			// Parse required-title-prefix (preferred) with fallback to deprecated title-prefix alias.
			pushToBranchConfig.TitlePrefix = extractStringFromMap(configMap, "required-title-prefix", pushToPullRequestBranchLog)
			if pushToBranchConfig.TitlePrefix == "" {
				pushToBranchConfig.TitlePrefix = extractStringFromMap(configMap, "title-prefix", pushToPullRequestBranchLog)
			}

			// Parse required-labels (preferred) with fallback to deprecated labels.
			pushToBranchConfig.RequiredLabels = ParseStringArrayOrExprFromConfig(configMap, "required-labels", pushToPullRequestBranchLog)
			if len(pushToBranchConfig.RequiredLabels) == 0 {
				pushToBranchConfig.RequiredLabels = ParseStringArrayOrExprFromConfig(configMap, "labels", pushToPullRequestBranchLog)
			}

			// Parse commit-title-suffix (optional)
			if commitTitleSuffix, exists := configMap["commit-title-suffix"]; exists {
				if commitTitleSuffixStr, ok := commitTitleSuffix.(string); ok {
					pushToBranchConfig.CommitTitleSuffix = commitTitleSuffixStr
				}
			}

			// Parse github-token-for-extra-empty-commit (optional) - token for pushing empty commit to trigger CI
			if emptyCommitToken, exists := configMap["github-token-for-extra-empty-commit"]; exists {
				if emptyCommitTokenStr, ok := emptyCommitToken.(string); ok {
					pushToBranchConfig.GithubTokenForExtraEmptyCommit = emptyCommitTokenStr
					pushToPullRequestBranchLog.Printf("Extra empty commit token configured")
				}
			}

			// Parse target-repo for cross-repository push
			pushToBranchConfig.TargetRepoSlug = extractStringFromMap(configMap, "target-repo", pushToPullRequestBranchLog)
			pushToBranchConfig.HeadRepoSlug = extractStringFromMap(configMap, "head-repo", pushToPullRequestBranchLog)
			pushToBranchConfig.HeadGitHubToken = extractStringFromMap(configMap, "head-github-token", pushToPullRequestBranchLog)

			// Parse head-github-app manually so that the app-id alias is honoured
			// (YAML unmarshal would silently ignore app-id since GitHubAppConfig only
			// declares the canonical client-id tag).
			if headAppData, exists := configMap["head-github-app"]; exists {
				if headAppMap, ok := headAppData.(map[string]any); ok {
					pushToPullRequestBranchLog.Print("Parsed head-github-app from config")
					pushToBranchConfig.HeadGitHubApp = parseAppConfig(headAppMap)
				}
			}

			// Parse base-branch for explicit override of the target repo's base branch.
			// When unset, the safe-outputs MCP server resolves it at runtime from the local
			// checkout metadata (origin/HEAD) or falls back to the repo's default branch.
			pushToBranchConfig.BaseBranch = extractStringFromMap(configMap, "base-branch", pushToPullRequestBranchLog)

			// Parse allowed-repos for cross-repository push (expression-aware)
			pushToBranchConfig.AllowedRepos = ParseStringArrayOrExprFromConfig(configMap, "allowed-repos", pushToPullRequestBranchLog)

			// Parse protected-files: supports string enum OR object form {policy, exclude}.
			exclude := preprocessProtectedFilesField(configMap, pushToPullRequestBranchLog)
			pushToBranchConfig.ProtectedFilesExclude = exclude
			// Validate policy string (no-op if the field was replaced by preprocessor)
			manifestFilesEnums := []string{"blocked", "allowed", "fallback-to-issue", "request_review", "request-review"}
			validateStringEnumField(configMap, "protected-files", manifestFilesEnums, pushToPullRequestBranchLog)
			if strVal, ok := configMap["protected-files"].(string); ok {
				normalised := normaliseProtectedFilesPolicy(strVal)
				pushToBranchConfig.ManifestFilesPolicy = &normalised
			}

			// Parse allowed-files: list of glob patterns forming a strict allowlist of eligible files
			pushToBranchConfig.AllowedFiles = ParseStringArrayFromConfig(configMap, "allowed-files", pushToPullRequestBranchLog)

			// Parse excluded-files: list of glob patterns for files to exclude via git :(exclude) pathspecs
			pushToBranchConfig.ExcludedFiles = ParseStringArrayFromConfig(configMap, "excluded-files", pushToPullRequestBranchLog)

			// Parse max-patch-size override (optional, must be > 0)
			if maxPatchSize, exists := configMap["max-patch-size"]; exists {
				if maxPatchSizeInt, ok := typeutil.ParseIntValue(maxPatchSize); ok && maxPatchSizeInt > 0 {
					pushToBranchConfig.MaxPatchSize = maxPatchSizeInt
				}
			}

			// Parse patch-format: valid values are "bundle" (default) and "am"
			patchFormatEnums := []string{"am", "bundle"}
			validateStringEnumField(configMap, "patch-format", patchFormatEnums, pushToPullRequestBranchLog)
			if patchFormat, exists := configMap["patch-format"]; exists {
				if patchFormatStr, ok := patchFormat.(string); ok {
					pushToBranchConfig.PatchFormat = patchFormatStr
				}
			}

			// Parse fallback-as-pull-request (optional, defaults to true)
			if fallbackAsPullRequest, exists := configMap["fallback-as-pull-request"]; exists {
				if fallbackAsPullRequestBool, ok := fallbackAsPullRequest.(bool); ok {
					pushToBranchConfig.FallbackAsPullRequest = &fallbackAsPullRequestBool
				}
			}

			// Parse signed-commits (optional, defaults to true)
			if signedCommits, exists := configMap["signed-commits"]; exists {
				if signedCommitsBool, ok := signedCommits.(bool); ok {
					pushToBranchConfig.SignedCommits = &signedCommitsBool
				}
			}

			// Parse allow-workflows: when true, adds workflows: write to the GitHub App token
			if allowWorkflows, exists := configMap["allow-workflows"]; exists {
				if allowWorkflowsBool, ok := allowWorkflows.(bool); ok {
					pushToBranchConfig.AllowWorkflows = allowWorkflowsBool
				}
			}

			// Parse check-branch-protection: when false, skips the branch protection API pre-flight check
			if checkBranchProtection, exists := configMap["check-branch-protection"]; exists {
				if checkBranchProtectionBool, ok := checkBranchProtection.(bool); ok {
					pushToBranchConfig.CheckBranchProtection = &checkBranchProtectionBool
				}
			}

			// Parse common base fields with default max of 0 (no limit)
			c.parseBaseSafeOutputConfig(configMap, &pushToBranchConfig.BaseSafeOutputConfig, 0)
		}

		return pushToBranchConfig
	}

	return nil
}
