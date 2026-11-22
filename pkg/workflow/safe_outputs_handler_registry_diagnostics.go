package workflow

// diagnosticHandlerRegistry contains diagnostic, reporting, and no-op handler builders.
var diagnosticHandlerRegistry = map[string]handlerBuilder{
	"request_continuation": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.RequestContinuation == nil {
			return nil
		}
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", cfg.RequestContinuation.Max).
			Build()
	},
	"report_missing_tool": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.MissingTool == nil {
			return nil
		}
		c := cfg.MissingTool
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "report-missing-tool", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"report_missing_data": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.MissingData == nil {
			return nil
		}
		c := cfg.MissingData
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "report-missing-data", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"noop": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.NoOp == nil {
			return nil
		}
		c := cfg.NoOp
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddStringPtr("report-as-issue", c.ReportAsIssue).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"report_incomplete": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.ReportIncomplete == nil {
			return nil
		}
		c := cfg.ReportIncomplete
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "report-incomplete", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"create_report_incomplete_issue": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.ReportIncomplete == nil {
			return nil
		}
		c := cfg.ReportIncomplete
		// If create-issue is explicitly false, skip generating the issue handler.
		// For nil (default) or "true", always include; for expressions, include
		// the handler and embed the expression so it is evaluated at runtime.
		if c.CreateIssue != nil && *c.CreateIssue == "false" {
			return nil
		}
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("title-prefix", c.TitlePrefix).
			AddStringSlice("labels", c.Labels).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "report-incomplete", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		// When create-issue is a GitHub Actions expression, embed it in the handler config.
		// GitHub Actions evaluates the expression before the handler runs; the JavaScript
		// handler then parses the resolved value via parseBoolTemplatable at runtime.
		if c.CreateIssue != nil && isExpression(*c.CreateIssue) {
			builder = builder.AddTemplatableBool("create-issue", c.CreateIssue)
		}
		return builder.Build()
	},
}
