package workflow

// jiraHandlerRegistry contains Jira Cloud handler builders.
var jiraHandlerRegistry = map[string]handlerBuilder{
	"jira_create_issue": func(cfg *SafeOutputsConfig) map[string]any {
		return buildJiraHandlerConfig(cfg.JiraCreateIssue)
	},
	"jira_update_issue": func(cfg *SafeOutputsConfig) map[string]any {
		return buildJiraHandlerConfig(cfg.JiraUpdateIssue)
	},
	"jira_add_comment": func(cfg *SafeOutputsConfig) map[string]any {
		return buildJiraHandlerConfig(cfg.JiraAddComment)
	},
	"jira_add_label": func(cfg *SafeOutputsConfig) map[string]any {
		return buildJiraHandlerConfig(cfg.JiraAddLabel)
	},
}

func buildJiraHandlerConfig(config *JiraSafeOutputConfig) map[string]any {
	if config == nil {
		return nil
	}
	return newHandlerConfigBuilder().
		AddTemplatableInt("max", config.Max).
		AddTemplatableBool("staged", templatableBoolPtrToStringPtr(config.Staged)).
		Build()
}
