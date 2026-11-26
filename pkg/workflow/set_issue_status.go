package workflow

import "github.com/github/gh-aw/pkg/logger"

var setIssueStatusLog = logger.New("workflow:set_issue_status")

// SetIssueStatusConfig controls planning-status changes independently of legacy issue state.
type SetIssueStatusConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	SafeOutputFilterConfig `yaml:",inline"`
	Allowed                []any `yaml:"allowed,omitempty"`
	AllowClose             bool  `yaml:"allow-close,omitempty"`
	AllowReopen            bool  `yaml:"allow-reopen,omitempty"`
}

func (c *Compiler) parseSetIssueStatusConfig(outputMap map[string]any) *SetIssueStatusConfig {
	return parseConfigScaffold(outputMap, "set-issue-status", setIssueStatusLog,
		func(err error) *SetIssueStatusConfig {
			setIssueStatusLog.Printf("Failed to unmarshal set-issue-status config, disabling handler: %v", err)
			return nil
		})
}
