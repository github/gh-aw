package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var mergePullRequestLog = logger.New("workflow:merge_pull_request")

// MergePullRequestConfig holds configuration for merging pull requests with policy checks.
type MergePullRequestConfig struct {
	BaseSafeOutputConfig          `yaml:",inline"`
	SafeOutputTargetConfig        `yaml:",inline"`
	SafeOutputAllowedLabelsConfig `yaml:",inline"` // Deprecated: allowed-labels is migrated to required-labels
	RequiredLabels                []string         `yaml:"required-labels,omitempty"`       // Labels that must ALL be present on the PR
	AllowedBranches               []string         `yaml:"allowed-branches,omitempty"`      // Glob patterns for source branch names; PR branch must match one
	RequiredTitlePrefix           string           `yaml:"required-title-prefix,omitempty"` // Title prefix the PR must have
	RequiredChecks                []string         `yaml:"required-checks,omitempty"`       // Exact check-run names required on the evaluated head
	RequiredApprovers             []string         `yaml:"required-approvers,omitempty"`    // At least one independent human from this allowlist must approve the head
}

// parseMergePullRequestConfig handles merge-pull-request configuration.
func (c *Compiler) parseMergePullRequestConfig(outputMap map[string]any) *MergePullRequestConfig {
	configData, exists := outputMap["merge-pull-request"]
	if !exists {
		return nil
	}

	mergePullRequestLog.Print("Parsing merge-pull-request config")
	cfg := &MergePullRequestConfig{}
	if configMap, ok := configData.(map[string]any); ok {
		cfg.RequiredLabels = ParseStringArrayFromConfig(configMap, "required-labels", mergePullRequestLog)
		if len(cfg.RequiredLabels) == 0 {
			// Deprecated: allowed-labels is migrated to required-labels by the codemod
			cfg.RequiredLabels = ParseStringArrayFromConfig(configMap, "allowed-labels", mergePullRequestLog)
		}
		targetConfig, _ := ParseTargetConfig(configMap)
		cfg.SafeOutputTargetConfig = targetConfig
		cfg.AllowedBranches = ParseStringArrayFromConfig(configMap, "allowed-branches", mergePullRequestLog)
		cfg.RequiredTitlePrefix = extractStringFromMap(configMap, "required-title-prefix", mergePullRequestLog)
		cfg.RequiredChecks = ParseStringArrayFromConfig(configMap, "required-checks", mergePullRequestLog)
		cfg.RequiredApprovers = ParseStringArrayFromConfig(configMap, "required-approvers", mergePullRequestLog)
		c.parseBaseSafeOutputConfig(configMap, &cfg.BaseSafeOutputConfig, 1)
		mergePullRequestLog.Printf("Parsed merge-pull-request config: requiredLabels=%v, allowedBranches=%v, requiredTitlePrefix=%q", cfg.RequiredLabels, cfg.AllowedBranches, cfg.RequiredTitlePrefix)
		return cfg
	}

	// merge-pull-request: null enables defaults
	cfg.Max = defaultIntStr(1)
	return cfg
}

// validateMergePullRequestPolicies checks merge filters and workflow-owned evidence requirements.
func validateMergePullRequestPolicies(config *SafeOutputsConfig, workflowName string) error {
	if err := validateSafeOutputsMergePullRequest(config); err != nil {
		return err
	}
	if config == nil || config.MergePullRequest == nil {
		return nil
	}
	cfg := config.MergePullRequest
	for field, values := range map[string][]string{
		"required-checks":    cfg.RequiredChecks,
		"required-approvers": cfg.RequiredApprovers,
	} {
		for i, value := range values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("safe-outputs.merge-pull-request.%s[%d] cannot be empty. Expected a non-empty string value", field, i)
			}
		}
	}
	for _, login := range cfg.RequiredApprovers {
		if !parser.IsValidGitHubIdentifier(login) {
			return fmt.Errorf("safe-outputs.merge-pull-request.required-approvers contains invalid GitHub username %q. Expected a human account login without @ prefix", login)
		}
	}
	if config.CreateCheckRun != nil {
		checkName := config.CreateCheckRun.Name
		if checkName == "" {
			checkName = workflowName
		}
		switch checkName {
		case "":
			checkName = "Agent Check"
		case workflowName:
			checkName += " (Result)"
		}
		if slices.Contains(cfg.RequiredChecks, checkName) {
			return fmt.Errorf("safe-outputs.merge-pull-request.required-checks contains agent-controlled create-check-run name %q. Use an independent CI check name instead", checkName)
		}
	}
	return nil
}
