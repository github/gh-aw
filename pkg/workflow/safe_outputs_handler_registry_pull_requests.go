package workflow

// pullRequestHandlerRegistry contains pull request lifecycle and review handler builders.
var pullRequestHandlerRegistry = map[string]handlerBuilder{
	"create_pull_request": buildCreatePullRequestHandlerConfig,
	"push_to_pull_request_branch": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.PushToPullRequestBranch == nil {
			return nil
		}
		c := cfg.PushToPullRequestBranch
		maxPatchSize := 4096 // default 4096 KB
		if cfg.MaximumPatchSize > 0 {
			maxPatchSize = cfg.MaximumPatchSize
		}
		if c.MaxPatchSize > 0 {
			maxPatchSize = c.MaxPatchSize
		}
		protectedFilesPolicy := pushToPullRequestBranchProtectedFilesPolicy(c)
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddIfNotEmpty("title_prefix", c.TitlePrefix).
			AddTemplatableStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("if_no_changes", c.IfNoChanges).
			AddIfTrue("ignore_missing_branch_failure", c.IgnoreMissingBranchFailure).
			AddIfNotEmpty("commit_title_suffix", c.CommitTitleSuffix).
			AddDefault("max_patch_size", maxPatchSize).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddIfNotEmpty("head-repo", c.HeadRepoSlug).
			AddIfNotEmpty("base_branch", c.BaseBranch).
			AddTemplatableStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "push-to-pull-request-branch", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			AddDefault("protected_files_policy", protectedFilesPolicy).
			AddStringSlice("protected_files", getAllManifestFiles()).
			AddStringSlice("protected_path_prefixes", getProtectedPathPrefixes()).
			AddDefault("protect_top_level_dot_folders", true).
			AddStringSlice("_protected_files_exclude", defaultProtectedFilesExclude(c.ProtectedFilesExclude)).
			AddStringSlice("allowed_files", c.AllowedFiles).
			AddStringSlice("excluded_files", c.ExcludedFiles).
			AddIfNotEmpty("patch_format", c.PatchFormat).
			AddBoolPtr("fallback_as_pull_request", c.FallbackAsPullRequest).
			AddBoolPtr("signed_commits", c.SignedCommits).
			AddBoolPtr("check_branch_protection", c.CheckBranchProtection).
			AddIfTrue("allow_workflows", c.AllowWorkflows)
		// Use app-minted token if head-github-app is configured; fall back to head-github-token.
		if c.HeadGitHubApp != nil {
			//nolint:gosec // G101: False positive - this is a GitHub Actions expression template, not a hardcoded credential
			builder.AddIfNotEmpty("head-github-token", "${{ steps.safe-outputs-head-app-token.outputs.token }}")
		} else {
			builder.AddIfNotEmpty("head-github-token", c.HeadGitHubToken)
		}
		return builder.Build()
	},
	"update_pull_request": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UpdatePullRequests == nil {
			return nil
		}
		c := cfg.UpdatePullRequests
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddBoolPtrOrDefault("allow_title", c.Title, true).
			AddBoolPtrOrDefault("allow_body", c.Body, true).
			AddBoolPtrOrDefault("update_branch", c.UpdateBranch, false).
			AddBoolPtrOrDefault("update_branch_stacks", c.UpdateBranchStacks, true).
			AddStringPtr("default_operation", c.Operation).
			AddTemplatableBool("footer", getEffectiveFooterForTemplatable(c.Footer, cfg.Footer)).AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "update-pull-request", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"merge_pull_request": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.MergePullRequest == nil {
			return nil
		}
		c := cfg.MergePullRequest
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).AddStringSlice("allowed_branches", c.AllowedBranches).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "merge-pull-request", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"close_pull_request": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.ClosePullRequests == nil {
			return nil
		}
		c := cfg.ClosePullRequests
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "close-pull-request", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"mark_pull_request_as_ready_for_review": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.MarkPullRequestAsReadyForReview == nil {
			return nil
		}
		c := cfg.MarkPullRequestAsReadyForReview
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "mark-pull-request-as-ready-for-review", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"add_reviewer": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AddReviewer == nil {
			return nil
		}
		c := cfg.AddReviewer
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddStringSlice("allowed", c.AllowedReviewers).
			AddStringSlice("allowed_team_reviewers", c.AllowedTeamReviewers).
			AddIfNotEmpty("target", c.Target).AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "add-reviewer", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"dismiss_pull_request_review": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.DismissPullRequestReview == nil {
			return nil
		}
		c := cfg.DismissPullRequestReview
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "dismiss-pull-request-review", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"submit_pull_request_review": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.SubmitPullRequestReview == nil {
			return nil
		}
		c := cfg.SubmitPullRequestReview
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddStringSlice("allowed_events", c.AllowedEvents).
			AddIfTrue("supersede_older_reviews", c.SupersedeOlderReviews).AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "submit-pull-request-review", c.GitHubToken)).
			AddStringPtr("footer", getEffectiveFooterString(c.Footer, cfg.Footer)).
			AddIfNotEmpty("commit_id", c.CommitId).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"resolve_pull_request_review_thread": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.ResolvePullRequestReviewThread == nil {
			return nil
		}
		c := cfg.ResolvePullRequestReviewThread
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "resolve-pull-request-review-thread", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"create_pull_request_review_comment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreatePullRequestReviewComments == nil {
			return nil
		}
		c := cfg.CreatePullRequestReviewComments
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("side", c.Side).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("commit_id", c.CommitId).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "create-pull-request-review-comment", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"reply_to_pull_request_review_comment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.ReplyToPullRequestReviewComment == nil {
			return nil
		}
		c := cfg.ReplyToPullRequestReviewComment
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "reply-to-pull-request-review-comment", c.GitHubToken)).
			AddTemplatableBool("footer", getEffectiveFooterForTemplatable(c.Footer, cfg.Footer)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
}

func buildCreatePullRequestHandlerConfig(cfg *SafeOutputsConfig) map[string]any {
	if cfg.CreatePullRequests == nil {
		return nil
	}
	c := cfg.CreatePullRequests
	builder := newCreatePullRequestHandlerConfigBuilder(cfg, c)
	// Stacked pull requests are enabled by default; only emit the flag when disabled
	// (e.g. GitHub Enterprise Server instances without stacked pull request support).
	if !isStackedPullRequestsEnabled(c) {
		builder.AddDefault("stacked", false)
	}
	// Use app-minted token if head-github-app is configured; fall back to head-github-token.
	if c.HeadGitHubApp != nil {
		//nolint:gosec // G101: False positive - this is a GitHub Actions expression template, not a hardcoded credential
		builder.AddIfNotEmpty("head-github-token", "${{ steps.safe-outputs-head-app-token.outputs.token }}")
	} else {
		builder.AddIfNotEmpty("head-github-token", c.HeadGitHubToken)
	}
	return builder.Build()
}

func newCreatePullRequestHandlerConfigBuilder(cfg *SafeOutputsConfig, c *CreatePullRequestsConfig) *handlerConfigBuilder {
	maxPatchSize := createPullRequestMaxPatchSize(cfg, c)
	maxPatchFiles := createPullRequestMaxPatchFiles(cfg, c)
	protectedFilesPolicy := createPullRequestProtectedFilesPolicy(c)
	return newHandlerConfigBuilder().
		AddTemplatableInt("max", c.Max).
		AddIfTrue("require_temporary_id", c.RequireTemporaryID).
		AddIfNotEmpty("branch_prefix", c.BranchPrefix).
		AddIfNotEmpty("title_prefix", c.TitlePrefix).
		AddIfNotEmpty("body_footer", c.BodyFooter).
		AddTemplatableStringSlice("labels", c.Labels).
		AddStringSlice("fallback_labels", c.FallbackLabels).
		AddTemplatableStringSlice("reviewers", c.Reviewers).
		AddTemplatableStringSlice("team_reviewers", c.TeamReviewers).
		AddTemplatableStringSlice("assignees", c.Assignees).
		AddTemplatableBool("draft", c.Draft).
		AddIfNotEmpty("if_no_changes", c.IfNoChanges).
		AddTemplatableBool("allow_empty", c.AllowEmpty).
		AddTemplatableBool("auto_merge", c.AutoMerge).
		AddIfPositive("expires", c.Expires).
		AddIfNotEmpty("target-repo", c.TargetRepoSlug).
		AddIfNotEmpty("head-repo", c.HeadRepoSlug).
		AddTemplatableStringSlice("allowed_repos", c.AllowedRepos).
		AddTemplatableStringSlice("allowed_base_branches", c.AllowedBaseBranches).
		AddTemplatableStringSlice("allowed_branches", c.AllowedBranches).
		AddDefault("max_patch_size", maxPatchSize).
		AddDefault("max_patch_files", maxPatchFiles).
		AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "create-pull-request", c.GitHubToken)).
		AddTemplatableBool("footer", getEffectiveFooterForTemplatable(c.Footer, cfg.Footer)).
		AddBoolPtr("normalize_closing_keywords", c.NormalizeClosingKeywords).
		AddBoolPtr("fallback_as_issue", c.FallbackAsIssue).
		AddTemplatableBool("auto_close_issue", c.AutoCloseIssue).
		AddIfNotEmpty("base_branch", c.BaseBranch).
		AddDefault("protected_files_policy", protectedFilesPolicy).
		AddStringSlice("protected_files", getAllManifestFiles()).
		AddStringSlice("protected_path_prefixes", getProtectedPathPrefixes()).
		AddDefault("protect_top_level_dot_folders", true).
		AddStringSlice("_protected_files_exclude", defaultProtectedFilesExclude(c.ProtectedFilesExclude)).
		AddStringSlice("allowed_files", c.AllowedFiles).
		AddStringSlice("excluded_files", c.ExcludedFiles).
		AddIfTrue("preserve_branch_name", c.PreserveBranchName).
		AddIfTrue("recreate_ref", c.RecreateRef).
		AddIfNotEmpty("patch_format", c.PatchFormat).
		AddBoolPtr("signed_commits", c.SignedCommits).
		// entity-specific env key name per shared CloseOlderConfig field (see the create_issue handler in safe_outputs_handler_registry_issues.go)
		AddTemplatableBool("close_older_pull_requests", c.Enabled).
		AddIfNotEmpty("close_older_key", c.Key).
		AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
}

func normaliseProtectedFilesPolicy(policy string) string {
	switch policy {
	case "request_review", "request-review":
		return "request-review"
	case "fallback_to_issue", "fallback-to-issue":
		return "fallback-to-issue"
	default:
		return policy
	}
}

func defaultProtectedFilesExclude(excludes []string) []string {
	seen := make(map[string]struct{}, len(excludes)+1)
	result := make([]string, 0, len(excludes)+1)
	for _, file := range append([]string{"CHANGELOG.md"}, excludes...) {
		if file == "" {
			continue
		}
		if _, exists := seen[file]; exists {
			continue
		}
		seen[file] = struct{}{}
		result = append(result, file)
	}
	return result
}

func createPullRequestProtectedFilesPolicy(c *CreatePullRequestsConfig) string {
	if c == nil || c.ManifestFilesPolicy == nil {
		return "request-review"
	}
	return normaliseProtectedFilesPolicy(*c.ManifestFilesPolicy)
}

func pushToPullRequestBranchProtectedFilesPolicy(c *PushToPullRequestBranchConfig) string {
	if c == nil || c.ManifestFilesPolicy == nil {
		return "request-review"
	}
	return normaliseProtectedFilesPolicy(*c.ManifestFilesPolicy)
}

func createPullRequestMaxPatchSize(cfg *SafeOutputsConfig, c *CreatePullRequestsConfig) int {
	maxPatchSize := 4096 // default 4096 KB
	if cfg.MaximumPatchSize > 0 {
		maxPatchSize = cfg.MaximumPatchSize
	}
	if c.MaxPatchSize > 0 {
		maxPatchSize = c.MaxPatchSize
	}
	return maxPatchSize
}

func createPullRequestMaxPatchFiles(cfg *SafeOutputsConfig, c *CreatePullRequestsConfig) int {
	maxPatchFiles := 100 // default 100 unique files
	if cfg.MaximumPatchFiles > 0 {
		maxPatchFiles = cfg.MaximumPatchFiles
	}
	if c.MaxPatchFiles > 0 {
		maxPatchFiles = c.MaxPatchFiles
	}
	return maxPatchFiles
}
