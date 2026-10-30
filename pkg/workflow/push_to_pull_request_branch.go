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
	GithubTokenForExtraEmptyCommit string           `yaml:"github-token-for-extra-empty-commit,omitempty"` // Token used to push an empty commit to trigger CI events. Use a PAT, "app" for GitHub App auth, or "none" to disable the extra empty commit.
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
	ExcludeLockYml                 bool             `yaml:"exclude-lock-yml,omitempty"`                    // Exclude compiled .github/workflows/*.lock.yml files from branch writes.
	MaxPatchSize                   int              `yaml:"max-patch-size,omitempty"`                      // Maximum allowed patch size in KB for push-to-pull-request-branch only. Overrides safe-outputs.max-patch-size when set.
	PatchFormat                    string           `yaml:"patch-format,omitempty"`                        // Transport format for packaging changes: "bundle" (default, uses git bundle and preserves merge topology/per-commit metadata) or "am" (uses git format-patch).
	FallbackAsPullRequest          *bool            `yaml:"fallback-as-pull-request,omitempty"`            // When true (default), creates a fallback pull request if direct push fails due to diverged/non-fast-forward branch. When false, fallback is disabled and pull-requests: write is not requested.
	SignedCommits                  *bool            `yaml:"signed-commits,omitempty"`                      // When false, skips GitHub GraphQL signed commits and pushes the local git history directly. Default is true.
	AllowFullHistoryFetch          bool             `yaml:"allow-full-history-fetch,omitempty"`            // Explicit opt-in to unbounded bundle prerequisite downloads (default: false).
	AllowMergedBaseBaseline        bool             `yaml:"allow-merged-base-baseline,omitempty"`          // Enables bounded discovery of older merged base ancestors for unsigned bundle size validation.
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
	configData, exists := outputMap["push-to-pull-request-branch"]
	if !exists {
		return nil
	}
	pushToPullRequestBranchLog.Print("Parsing push-to-pull-request-branch configuration")
	pushToBranchConfig := &PushToPullRequestBranchConfig{IfNoChanges: "warn"}
	if configData == nil {
		return pushToBranchConfig
	}
	configMap, ok := configData.(map[string]any)
	if !ok {
		return pushToBranchConfig
	}

	c.parsePushToPullRequestBranchConstraints(configMap, pushToBranchConfig)
	c.parsePushToPullRequestBranchRepository(configMap, pushToBranchConfig)
	c.parsePushToPullRequestBranchFilePolicies(configMap, pushToBranchConfig)
	c.parsePushToPullRequestBranchTransport(configMap, pushToBranchConfig)
	c.parsePushToPullRequestBranchOptions(configMap, pushToBranchConfig)
	c.parseBaseSafeOutputConfig(configMap, &pushToBranchConfig.BaseSafeOutputConfig, 0)
	return pushToBranchConfig
}

func (c *Compiler) parsePushToPullRequestBranchConstraints(configMap map[string]any, config *PushToPullRequestBranchConfig) {
	if targetStr := extractTargetFromMap(configMap, pushToPullRequestBranchLog); targetStr != "" {
		config.Target = targetStr
	}
	if ifNoChanges, exists := configMap["if-no-changes"]; exists {
		if value, ok := ifNoChanges.(string); ok {
			switch value {
			case "warn", "error", "ignore":
				config.IfNoChanges = value
			default:
				if c.verbose {
					fmt.Fprintf(os.Stderr, "Warning: invalid if-no-changes value '%s', using default 'warn'\n", value)
				}
			}
		}
	}
	if value, ok := configMap["ignore-missing-branch-failure"].(bool); ok {
		config.IgnoreMissingBranchFailure = value
	}
	config.TitlePrefix = extractStringFromMap(configMap, "required-title-prefix", pushToPullRequestBranchLog)
	if config.TitlePrefix == "" {
		config.TitlePrefix = extractStringFromMap(configMap, "title-prefix", pushToPullRequestBranchLog)
	}
	config.RequiredLabels = ParseStringArrayOrExprFromConfig(configMap, "required-labels", pushToPullRequestBranchLog)
	if len(config.RequiredLabels) == 0 {
		config.RequiredLabels = ParseStringArrayOrExprFromConfig(configMap, "labels", pushToPullRequestBranchLog)
	}
}

func (c *Compiler) parsePushToPullRequestBranchRepository(configMap map[string]any, config *PushToPullRequestBranchConfig) {
	if value, ok := configMap["commit-title-suffix"].(string); ok {
		config.CommitTitleSuffix = value
	}
	if value, ok := configMap["github-token-for-extra-empty-commit"].(string); ok {
		config.GithubTokenForExtraEmptyCommit = value
		pushToPullRequestBranchLog.Printf("Extra empty commit token configured")
	}
	config.TargetRepoSlug = extractStringFromMap(configMap, "target-repo", pushToPullRequestBranchLog)
	config.HeadRepoSlug = extractStringFromMap(configMap, "head-repo", pushToPullRequestBranchLog)
	config.HeadGitHubToken = extractStringFromMap(configMap, "head-github-token", pushToPullRequestBranchLog)
	if headAppData, exists := configMap["head-github-app"]; exists {
		if headAppMap, ok := headAppData.(map[string]any); ok {
			pushToPullRequestBranchLog.Print("Parsed head-github-app from config")
			config.HeadGitHubApp = parseAppConfig(headAppMap)
		}
	}
	config.BaseBranch = extractStringFromMap(configMap, "base-branch", pushToPullRequestBranchLog)
	config.AllowedRepos = ParseStringArrayOrExprFromConfig(configMap, "allowed-repos", pushToPullRequestBranchLog)
}

func (c *Compiler) parsePushToPullRequestBranchFilePolicies(configMap map[string]any, config *PushToPullRequestBranchConfig) {
	config.ProtectedFilesExclude = preprocessProtectedFilesField(configMap, pushToPullRequestBranchLog)
	manifestFilesEnums := []string{"blocked", "allowed", "fallback-to-issue", "request_review", "request-review"}
	validateStringEnumField(configMap, "protected-files", manifestFilesEnums, pushToPullRequestBranchLog)
	if value, ok := configMap["protected-files"].(string); ok {
		normalised := normaliseProtectedFilesPolicy(value)
		config.ManifestFilesPolicy = &normalised
	}
	config.AllowedFiles = ParseStringArrayFromConfig(configMap, "allowed-files", pushToPullRequestBranchLog)
	config.ExcludedFiles = ParseStringArrayFromConfig(configMap, "excluded-files", pushToPullRequestBranchLog)
	if value, ok := configMap["exclude-lock-yml"].(bool); ok {
		config.ExcludeLockYml = value
	}
}

func (c *Compiler) parsePushToPullRequestBranchTransport(configMap map[string]any, config *PushToPullRequestBranchConfig) {
	if maxPatchSize, exists := configMap["max-patch-size"]; exists {
		if value, ok := typeutil.ParseIntValue(maxPatchSize); ok && value > 0 {
			config.MaxPatchSize = value
		}
	}
	patchFormatEnums := []string{"am", "bundle"}
	validateStringEnumField(configMap, "patch-format", patchFormatEnums, pushToPullRequestBranchLog)
	if value, ok := configMap["patch-format"].(string); ok {
		config.PatchFormat = value
	}
}

func (c *Compiler) parsePushToPullRequestBranchOptions(configMap map[string]any, config *PushToPullRequestBranchConfig) {
	if value, ok := configMap["fallback-as-pull-request"].(bool); ok {
		config.FallbackAsPullRequest = &value
	}
	if value, ok := configMap["signed-commits"].(bool); ok {
		config.SignedCommits = &value
	}
	if value, ok := configMap["allow-full-history-fetch"].(bool); ok {
		config.AllowFullHistoryFetch = value
	}
	if value, ok := configMap["allow-merged-base-baseline"].(bool); ok {
		config.AllowMergedBaseBaseline = value
	}
	if value, ok := configMap["allow-workflows"].(bool); ok {
		config.AllowWorkflows = value
	}
	if value, ok := configMap["check-branch-protection"].(bool); ok {
		config.CheckBranchProtection = &value
	}
}
