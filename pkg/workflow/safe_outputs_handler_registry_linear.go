package workflow

var linearHandlerRegistry = map[string]handlerBuilder{
	"linear_create_issue": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.LinearCreateIssue == nil {
			return nil
		}
		c := cfg.LinearCreateIssue
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("team_id", c.TeamID).
			AddIfNotEmpty("project_id", c.ProjectID).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"linear_add_comment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.LinearAddComment == nil {
			return nil
		}
		c := cfg.LinearAddComment
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"linear_update_issue": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.LinearUpdateIssue == nil {
			return nil
		}
		c := cfg.LinearUpdateIssue
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target)
		if c.Title != nil && *c.Title {
			builder.AddDefault("allow_title", true)
		}
		if c.Body != nil && *c.Body {
			builder.AddDefault("allow_body", true)
		}
		return builder.
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
}
