package workflow

// assignmentHandlerRegistry contains assignment and agent session handler builders.
var assignmentHandlerRegistry = map[string]handlerBuilder{
	"assign_to_agent": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AssignToAgent == nil {
			return nil
		}
		c := cfg.AssignToAgent
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("name", c.DefaultAgent).
			AddIfNotEmpty("model", c.DefaultModel).
			AddIfNotEmpty("reasoning-effort", c.ReasoningEffort).
			AddIfNotEmpty("custom-agent", c.DefaultCustomAgent).
			AddIfNotEmpty("custom-instructions", c.DefaultCustomInstructions).
			AddStringSlice("allowed", c.Allowed).
			AddBoolPtr("issue_intent", c.IssueIntent).
			AddIfTrue("ignore-if-error", c.IgnoreIfError).
			AddIfNotEmpty("target", c.Target).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed-repos", c.AllowedRepos).
			AddIfNotEmpty("pull-request-repo", c.PullRequestRepoSlug).
			AddStringSlice("allowed-pull-request-repos", c.AllowedPullRequestRepos).
			AddIfNotEmpty("base-branch", c.BaseBranch).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "assign-to-agent", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"assign_to_user": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AssignToUser == nil {
			return nil
		}
		c := cfg.AssignToUser
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddStringSlice("allowed", c.Allowed).
			AddStringSlice("blocked", c.Blocked).
			AddIfNotEmpty("target", c.Target).AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "assign-to-user", c.GitHubToken)).
			AddTemplatableBool("unassign_first", c.UnassignFirst).
			AddBoolPtr("issue_intent", c.IssueIntent).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"unassign_from_user": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UnassignFromUser == nil {
			return nil
		}
		c := cfg.UnassignFromUser
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddStringSlice("allowed", c.Allowed).
			AddStringSlice("blocked", c.Blocked).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "unassign-from-user", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"create_agent_session": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreateAgentSessions == nil {
			return nil
		}
		c := cfg.CreateAgentSessions
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("base", c.Base).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "create-agent-session", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
}
