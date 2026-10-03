package workflow

// workflowHandlerRegistry contains CI, workflow-triggering, artifact, and coverage handler builders.
var workflowHandlerRegistry = map[string]handlerBuilder{
	"approve_workflow_run": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.ApproveWorkflowRun == nil {
			return nil
		}
		c := cfg.ApproveWorkflowRun
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddDefault("comment", c.Comment).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddTemplatableJSONSlice("allowed_pull_requests", c.AllowedPullRequests).
			AddStringSlice("allowed_workflows", c.AllowedWorkflows).
			AddStringSlice("protected_files", getAllManifestFiles()).
			AddStringSlice("protected_path_prefixes", getProtectedPathPrefixes()).
			AddDefault("protect_top_level_dot_folders", true).
			AddStringSlice("_protected_files_exclude", c.ProtectedFilesExclude).
			AddIfNotEmpty("github-token", resolveApproveWorkflowRunGitHubToken(cfg, c)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"create_code_scanning_alert": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreateCodeScanningAlerts == nil {
			return nil
		}
		c := cfg.CreateCodeScanningAlerts
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("driver", c.Driver).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "create-code-scanning-alert", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"create_check_run": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreateCheckRun == nil {
			return nil
		}
		c := cfg.CreateCheckRun
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddIfNotEmpty("name", c.Name).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		if c.Output != nil {
			builder.
				AddIfNotEmpty("output_title", c.Output.Title).
				AddIfNotEmpty("output_summary", c.Output.Summary)
		}
		// Use resolveHandlerGitHubToken so the per-handler github-app pattern is consistent
		// with all other handlers: when github-app is set the compiler mints a dedicated
		// {key}-app-token step; otherwise fall back to the explicit github-token.
		builder.AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "create-check-run", c.GitHubToken))
		return builder.Build()
	},
	"dispatch_workflow": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.DispatchWorkflow == nil {
			return nil
		}
		c := cfg.DispatchWorkflow
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddStringSlice("workflows", c.Workflows).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddTemplatableStringSlice("allowed_repos", c.AllowedRepos).
			AddTemplatableStringSlice("allowed_refs", c.AllowedRefs)
		if cfg.WorkQueueEnabled {
			builder.AddDefault("work_queue_enabled", true)
		}

		// Add workflow_files map if it has entries
		if len(c.WorkflowFiles) > 0 {
			builder.AddDefault("workflow_files", c.WorkflowFiles)
		}

		// Add aw_context_workflows list if it has entries
		if len(c.AwContextWorkflows) > 0 {
			builder.AddStringSlice("aw_context_workflows", c.AwContextWorkflows)
		}

		builder.AddIfNotEmpty("target-ref", c.TargetRef)
		builder.AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "dispatch-workflow", c.GitHubToken))
		builder.AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		return builder.Build()
	},
	"dispatch_repository": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.DispatchRepository == nil || len(cfg.DispatchRepository.Tools) == 0 {
			return nil
		}
		// Serialize each tool as a sub-map
		tools := make(map[string]any, len(cfg.DispatchRepository.Tools))
		for toolKey, tool := range cfg.DispatchRepository.Tools {
			toolConfig := newHandlerConfigBuilder().
				AddIfNotEmpty("workflow", tool.Workflow).
				AddIfNotEmpty("event_type", tool.EventType).
				AddIfNotEmpty("repository", tool.Repository).
				AddStringSlice("allowed_repositories", tool.AllowedRepositories).
				AddTemplatableInt("max", tool.Max).
				AddIfNotEmpty("github-token", resolveHandlerGitHubTokenWithStepID(tool.GitHubApp, dispatchRepositoryToolAppTokenStepID(toolKey), tool.GitHubToken)).
				AddTemplatableBool("staged", templatableBoolPtrToStringPtr(tool.Staged)).
				Build()
			tools[toolKey] = toolConfig
		}
		return map[string]any{"tools": tools}
	},
	"call_workflow": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CallWorkflow == nil {
			return nil
		}
		c := cfg.CallWorkflow
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddStringSlice("workflows", c.Workflows)

		// Add workflow_files map if it has entries
		if len(c.WorkflowFiles) > 0 {
			builder.AddDefault("workflow_files", c.WorkflowFiles)
		}

		builder.AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		return builder.Build()
	},
	"autofix_code_scanning_alert": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AutofixCodeScanningAlert == nil {
			return nil
		}
		c := cfg.AutofixCodeScanningAlert
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "autofix-code-scanning-alert", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"upload_code_coverage": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UploadCodeCoverage == nil {
			return nil
		}
		c := cfg.UploadCodeCoverage
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "upload-code-coverage", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"upload_asset": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UploadAssets == nil {
			return nil
		}
		c := cfg.UploadAssets
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("branch", c.BranchName).
			AddIfPositive("max-size", c.MaxSizeKB).
			AddStringSlice("allowed-exts", c.AllowedExts).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "upload-asset", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"upload_artifact": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UploadArtifact == nil {
			return nil
		}
		c := cfg.UploadArtifact
		b := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfPositive("max-uploads", c.MaxUploads).
			AddTemplatableInt("retention-days", c.RetentionDays).
			AddTemplatableBool("skip-archive", c.SkipArchive).
			AddIfNotEmpty("github-token", resolveHandlerGitHubToken(c.GitHubApp, "upload-artifact", c.GitHubToken)).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		if c.MaxSizeBytes > 0 {
			b = b.AddDefault("max-size-bytes", c.MaxSizeBytes)
		}
		if len(c.AllowedPaths) > 0 {
			b = b.AddStringSlice("allowed-paths", c.AllowedPaths)
		}
		if c.Defaults != nil {
			if c.Defaults.IfNoFiles != "" {
				b = b.AddIfNotEmpty("default-if-no-files", c.Defaults.IfNoFiles)
			}
		}
		if c.Filters != nil {
			if len(c.Filters.Include) > 0 {
				b = b.AddStringSlice("filters-include", c.Filters.Include)
			}
			if len(c.Filters.Exclude) > 0 {
				b = b.AddStringSlice("filters-exclude", c.Filters.Exclude)
			}
		}
		return b.Build()
	},
}
