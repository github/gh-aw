package workflow

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/logger"
)

var pushToPullRequestBranchLog = logger.New("workflow:push_to_pull_request_branch")

// PushToPullRequestBranchConfig holds configuration for pushing changes to a specific branch from agent output
type PushToPullRequestBranchConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Target               string   `yaml:"target,omitempty"`              // Target for push-to-pull-request-branch: like add-comment but for pull requests
	TitlePrefix          string   `yaml:"title-prefix,omitempty"`        // Required title prefix for pull request validation
	Labels               []string `yaml:"labels,omitempty"`              // Required labels for pull request validation
	IfNoChanges          string   `yaml:"if-no-changes,omitempty"`       // Behavior when no changes to push: "warn", "error", or "ignore" (default: "warn")
	CommitTitleSuffix    string   `yaml:"commit-title-suffix,omitempty"` // Optional suffix to append to generated commit titles
}

func buildCheckoutRepository(steps []string, c *Compiler) []string {
	steps = append(steps, "      - name: Checkout repository\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/checkout")))
	steps = append(steps, "        with:\n")
	steps = append(steps, "          persist-credentials: false\n")
	steps = append(steps, "          fetch-depth: 0\n")
	if c.trialMode {
		if c.trialLogicalRepoSlug != "" {
			steps = append(steps, fmt.Sprintf("          repository: %s\n", c.trialLogicalRepoSlug))
			// trialTargetRepoName := strings.Split(c.trialLogicalRepoSlug, "/")
			// if len(trialTargetRepoName) == 2 {
			// 	steps = append(steps, fmt.Sprintf("          path: %s\n", trialTargetRepoName[1]))
			// }
		}
		steps = append(steps, "          token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}\n")
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

			// Parse title-prefix using shared helper
			pushToBranchConfig.TitlePrefix = parseTitlePrefixFromConfig(configMap)

			// Parse labels using shared helper
			pushToBranchConfig.Labels = parseLabelsFromConfig(configMap)

			// Parse commit-title-suffix (optional)
			if commitTitleSuffix, exists := configMap["commit-title-suffix"]; exists {
				if commitTitleSuffixStr, ok := commitTitleSuffix.(string); ok {
					pushToBranchConfig.CommitTitleSuffix = commitTitleSuffixStr
				}
			}

			// Parse common base fields with default max of 0 (no limit)
			c.parseBaseSafeOutputConfig(configMap, &pushToBranchConfig.BaseSafeOutputConfig, 0)
		}

		return pushToBranchConfig
	}

	return nil
}
