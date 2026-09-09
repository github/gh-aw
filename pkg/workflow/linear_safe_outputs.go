package workflow

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var linearSafeOutputsLog = logger.New("workflow:linear_safe_outputs")

type LinearCreateIssueConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	TeamID               string `yaml:"team-id"`
	ProjectID            string `yaml:"project-id,omitempty"`
}

type LinearTargetConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Target               string `yaml:"target"`
}

type LinearUpdateIssueConfig struct {
	LinearTargetConfig `yaml:",inline"`
	Title              *bool `yaml:"title,omitempty"`
	Body               *bool `yaml:"body,omitempty"`
}

func (c *SafeOutputsConfig) linearCreateIssueMax() *string {
	if c.LinearCreateIssue == nil {
		return nil
	}
	return c.LinearCreateIssue.Max
}

func (c *SafeOutputsConfig) linearAddCommentMax() *string {
	if c.LinearAddComment == nil {
		return nil
	}
	return c.LinearAddComment.Max
}

func (c *SafeOutputsConfig) linearUpdateIssueMax() *string {
	if c.LinearUpdateIssue == nil {
		return nil
	}
	return c.LinearUpdateIssue.Max
}

func preprocessLinearBaseConfig(outputMap map[string]any, key string) {
	configData, _ := outputMap[key].(map[string]any)
	if configData == nil {
		return
	}
	if err := preprocessIntFieldAsString(configData, "max", linearSafeOutputsLog); err != nil {
		linearSafeOutputsLog.Printf("Invalid %s max value: %v", key, err)
	}
	if err := preprocessBoolFieldAsString(configData, "staged", linearSafeOutputsLog); err != nil {
		linearSafeOutputsLog.Printf("Invalid %s staged value: %v", key, err)
	}
}

func parseLinearConfig[T any](outputMap map[string]any, key string) *T {
	preprocessLinearBaseConfig(outputMap, key)
	return parseConfigScaffoldWithPostProcess(outputMap, key, linearSafeOutputsLog,
		func(err error) *T {
			linearSafeOutputsLog.Printf("Failed to unmarshal %s config: %v", key, err)
			return nil
		},
		func(config *T) {
			if base := linearBaseConfig(config); base != nil && base.Max == nil {
				base.Max = defaultIntStr(1)
			}
		})
}

func linearBaseConfig(config any) *BaseSafeOutputConfig {
	switch c := config.(type) {
	case *LinearCreateIssueConfig:
		return &c.BaseSafeOutputConfig
	case *LinearTargetConfig:
		return &c.BaseSafeOutputConfig
	case *LinearUpdateIssueConfig:
		return &c.BaseSafeOutputConfig
	default:
		return nil
	}
}

func (c *Compiler) parseLinearCreateIssueConfig(outputMap map[string]any) *LinearCreateIssueConfig {
	return parseLinearConfig[LinearCreateIssueConfig](outputMap, "linear-create-issue")
}

func (c *Compiler) parseLinearAddCommentConfig(outputMap map[string]any) *LinearTargetConfig {
	return parseLinearConfig[LinearTargetConfig](outputMap, "linear-add-comment")
}

func (c *Compiler) parseLinearUpdateIssueConfig(outputMap map[string]any) *LinearUpdateIssueConfig {
	return parseLinearConfig[LinearUpdateIssueConfig](outputMap, "linear-update-issue")
}

func injectLinearCredentialsIntoProcessorStep(steps []string, config *SafeOutputsConfig) []string {
	if config == nil ||
		(config.LinearCreateIssue == nil && config.LinearAddComment == nil && config.LinearUpdateIssue == nil) {
		return steps
	}

	token := config.LinearToken
	if token == "" {
		token = constants.LinearMCPDefaultTokenExpr
	}
	env := map[string]string{"GH_AW_LINEAR_TOKEN": token}
	if config.LinearCreateIssue != nil {
		env["LINEAR_TEAM_ID"] = constants.LinearTeamIDExpr
		env["LINEAR_PROJECT_ID"] = constants.LinearProjectIDExpr
		for _, name := range []string{"LINEAR_TEAM_ID", "LINEAR_PROJECT_ID"} {
			if value := config.Env[name]; value != "" {
				env[name] = value
			}
		}
	}
	return injectProcessorStepEnv(steps, env)
}

func defaultReportIncompleteCreateIssue(config *SafeOutputsConfig) string {
	if !hasLinearSafeOutputs(config) {
		return "true"
	}
	withoutLinear := *config
	withoutLinear.LinearCreateIssue = nil
	withoutLinear.LinearAddComment = nil
	withoutLinear.LinearUpdateIssue = nil
	if hasNonBuiltinSafeOutputsEnabled(&withoutLinear) {
		return "true"
	}
	return "false"
}
