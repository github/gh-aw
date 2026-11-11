package workflow

// commentHandlerRegistry contains comment handler builders.
var commentHandlerRegistry = map[string]handlerBuilder{
	"add_security_advisory_comment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AddSecurityAdvisoryComment == nil {
			return nil
		}
		c := cfg.AddSecurityAdvisoryComment
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("github-token", resolveSecurityAdvisoryGitHubToken(cfg, "add-security-advisory-comment", &c.BaseSafeOutputConfig)).
			AddTemplatableBool("footer", getEffectiveFooterForTemplatable(c.Footer, cfg.Footer)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"add_comment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AddComments == nil {
			return nil
		}
		c := cfg.AddComments
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfPositive("max_links", c.MaxLinks).
			AddBoolPtr("collapse_data", c.CollapseData).
			AddIfNotEmpty("target", c.Target).
			AddTemplatableBool("hide_older_comments", c.HideOlderComments).
			AddTemplatableBool("replace_older_comments", c.ReplaceOlderComments).
			AddStringSlice("hide_older_comments_match", c.HideOlderCommentsMatch).
			AddBoolPtr("discussions", c.Discussions).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddTemplatableStringSlice("allowed_repos", c.AllowedRepos).
			AddTemplatableStringSlice("allows_comment_ids", c.AllowedCommentIDs).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "add-comment", c.GitHubToken)).
			AddTemplatableBool("footer", getEffectiveFooterForTemplatable(c.Footer, cfg.Footer)).
			AddBoolPtr("normalize_closing_keywords", c.NormalizeClosingKeywords).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"hide_comment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.HideComment == nil {
			return nil
		}
		c := cfg.HideComment
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddStringSlice("allowed_reasons", c.AllowedReasons).AddIfNotEmpty("target", c.Target).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "hide-comment", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
}
