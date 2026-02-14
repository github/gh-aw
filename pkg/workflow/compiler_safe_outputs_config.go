package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var compilerSafeOutputsConfigLog = logger.New("workflow:compiler_safe_outputs_config")

// getEffectiveFooter returns the effective footer value for a config
// If the local footer is set, use it; otherwise fall back to global footer
// Returns nil if neither is set (default to true in JavaScript)
func getEffectiveFooter(localFooter *bool, globalFooter *bool) *bool {
	if localFooter != nil {
		return localFooter
	}
	return globalFooter
}

// handlerConfigBuilder provides a fluent API for building handler configurations
type handlerConfigBuilder struct {
	config map[string]any
}

// newHandlerConfigBuilder creates a new handler config builder
func newHandlerConfigBuilder() *handlerConfigBuilder {
	return &handlerConfigBuilder{
		config: map[string]any{},
	}
}

// AddIfPositive adds an integer field only if the value is greater than 0
func (b *handlerConfigBuilder) AddIfPositive(key string, value int) *handlerConfigBuilder {
	if value > 0 {
		b.config[key] = value
	}
	return b
}

// AddIfNotEmpty adds a string field only if the value is not empty
func (b *handlerConfigBuilder) AddIfNotEmpty(key string, value string) *handlerConfigBuilder {
	if value != "" {
		b.config[key] = value
	}
	return b
}

// AddIfTrue adds a boolean field only if the value is true
func (b *handlerConfigBuilder) AddIfTrue(key string, value bool) *handlerConfigBuilder {
	if value {
		b.config[key] = true
	}
	return b
}

// AddStringSlice adds a string slice field only if the slice is not empty
func (b *handlerConfigBuilder) AddStringSlice(key string, value []string) *handlerConfigBuilder {
	if len(value) > 0 {
		b.config[key] = value
	}
	return b
}

// AddBoolPtr adds a boolean pointer field only if the pointer is not nil
func (b *handlerConfigBuilder) AddBoolPtr(key string, value *bool) *handlerConfigBuilder {
	if value != nil {
		b.config[key] = *value
	}
	return b
}

// AddBoolPtrOrDefault adds a boolean field, using default if pointer is nil
func (b *handlerConfigBuilder) AddBoolPtrOrDefault(key string, value *bool, defaultValue bool) *handlerConfigBuilder {
	if value != nil {
		b.config[key] = *value
	} else {
		b.config[key] = defaultValue
	}
	return b
}

// AddStringPtr adds a string pointer field only if the pointer is not nil
func (b *handlerConfigBuilder) AddStringPtr(key string, value *string) *handlerConfigBuilder {
	if value != nil {
		b.config[key] = *value
	}
	return b
}

// AddDefault adds a field with a default value unconditionally
func (b *handlerConfigBuilder) AddDefault(key string, value any) *handlerConfigBuilder {
	b.config[key] = value
	return b
}

// Build returns the built configuration map
func (b *handlerConfigBuilder) Build() map[string]any {
	return b.config
}

// handlerBuilder is a function that builds a handler config from SafeOutputsConfig
type handlerBuilder func(*SafeOutputsConfig) map[string]any

// handlerRegistry maps handler names to their builder functions
var handlerRegistry = map[string]handlerBuilder{
	"create_issue": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreateIssues == nil {
			return nil
		}
		c := cfg.CreateIssues
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddStringSlice("allowed_labels", c.AllowedLabels).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfPositive("expires", c.Expires).
			AddStringSlice("labels", c.Labels).
			AddIfNotEmpty("title_prefix", c.TitlePrefix).
			AddStringSlice("assignees", c.Assignees).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddIfTrue("group", c.Group).
			AddIfTrue("close_older_issues", c.CloseOlderIssues).
			AddBoolPtr("footer", getEffectiveFooter(c.Footer, cfg.Footer)).
			Build()
	},
	"add_comment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AddComments == nil {
			return nil
		}
		c := cfg.AddComments
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddIfTrue("hide_older_comments", c.HideOlderComments).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			Build()
	},
	"create_discussion": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreateDiscussions == nil {
			return nil
		}
		c := cfg.CreateDiscussions
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("category", c.Category).
			AddIfNotEmpty("title_prefix", c.TitlePrefix).
			AddStringSlice("labels", c.Labels).
			AddStringSlice("allowed_labels", c.AllowedLabels).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddIfTrue("close_older_discussions", c.CloseOlderDiscussions).
			AddIfNotEmpty("required_category", c.RequiredCategory).
			AddIfPositive("expires", c.Expires).
			AddBoolPtr("fallback_to_issue", c.FallbackToIssue).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddBoolPtr("footer", getEffectiveFooter(c.Footer, cfg.Footer)).
			Build()
	},
	"close_issue": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CloseIssues == nil {
			return nil
		}
		c := cfg.CloseIssues
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			Build()
	},
	"close_discussion": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CloseDiscussions == nil {
			return nil
		}
		c := cfg.CloseDiscussions
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			Build()
	},
	"add_labels": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AddLabels == nil {
			return nil
		}
		c := cfg.AddLabels
		config := newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddStringSlice("allowed", c.Allowed).
			AddIfNotEmpty("target", c.Target).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			Build()
		// If config is empty, it means add_labels was explicitly configured with no options
		// (null config), which means "allow any labels". Return non-nil empty map to
		// indicate the handler is enabled.
		if len(config) == 0 {
			// Return empty map so handler is included in config
			return make(map[string]any)
		}
		return config
	},
	"remove_labels": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.RemoveLabels == nil {
			return nil
		}
		c := cfg.RemoveLabels
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddStringSlice("allowed", c.Allowed).
			AddIfNotEmpty("target", c.Target).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			Build()
	},
	"update_issue": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UpdateIssues == nil {
			return nil
		}
		c := cfg.UpdateIssues
		builder := newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("target", c.Target)
		// Boolean pointer fields indicate which fields can be updated
		if c.Status != nil {
			builder.AddDefault("allow_status", true)
		}
		if c.Title != nil {
			builder.AddDefault("allow_title", true)
		}
		// Body uses boolean value mode - add the actual boolean value
		builder.AddBoolPtrOrDefault("allow_body", c.Body, true)
		return builder.
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddBoolPtr("footer", getEffectiveFooter(c.Footer, cfg.Footer)).
			Build()
	},
	"update_discussion": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UpdateDiscussions == nil {
			return nil
		}
		c := cfg.UpdateDiscussions
		builder := newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("target", c.Target)
		// Boolean pointer fields indicate which fields can be updated
		if c.Title != nil {
			builder.AddDefault("allow_title", true)
		}
		if c.Body != nil {
			builder.AddDefault("allow_body", true)
		}
		if c.Labels != nil {
			builder.AddDefault("allow_labels", true)
		}
		return builder.
			AddStringSlice("allowed_labels", c.AllowedLabels).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddBoolPtr("footer", getEffectiveFooter(c.Footer, cfg.Footer)).
			Build()
	},
	"link_sub_issue": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.LinkSubIssue == nil {
			return nil
		}
		c := cfg.LinkSubIssue
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddStringSlice("parent_required_labels", c.ParentRequiredLabels).
			AddIfNotEmpty("parent_title_prefix", c.ParentTitlePrefix).
			AddStringSlice("sub_required_labels", c.SubRequiredLabels).
			AddIfNotEmpty("sub_title_prefix", c.SubTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			Build()
	},
	"update_release": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UpdateRelease == nil {
			return nil
		}
		c := cfg.UpdateRelease
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddBoolPtr("footer", getEffectiveFooter(c.Footer, cfg.Footer)).
			Build()
	},
	"create_pull_request_review_comment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreatePullRequestReviewComments == nil {
			return nil
		}
		c := cfg.CreatePullRequestReviewComments
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("side", c.Side).
			AddIfNotEmpty("target", c.Target).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddStringPtr("footer", c.Footer).
			Build()
	},
	"submit_pull_request_review": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.SubmitPullRequestReview == nil {
			return nil
		}
		c := cfg.SubmitPullRequestReview
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddBoolPtr("footer", getEffectiveFooter(c.Footer, cfg.Footer)).
			Build()
	},
	"resolve_pull_request_review_thread": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.ResolvePullRequestReviewThread == nil {
			return nil
		}
		c := cfg.ResolvePullRequestReviewThread
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			Build()
	},
	"create_pull_request": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreatePullRequests == nil {
			return nil
		}
		c := cfg.CreatePullRequests
		maxPatchSize := 1024 // default 1024 KB
		if cfg.MaximumPatchSize > 0 {
			maxPatchSize = cfg.MaximumPatchSize
		}
		builder := newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("title_prefix", c.TitlePrefix).
			AddStringSlice("labels", c.Labels).
			AddBoolPtr("draft", c.Draft).
			AddIfNotEmpty("if_no_changes", c.IfNoChanges).
			AddIfTrue("allow_empty", c.AllowEmpty).
			AddIfTrue("auto_merge", c.AutoMerge).
			AddIfPositive("expires", c.Expires).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			AddDefault("max_patch_size", maxPatchSize).
			AddBoolPtr("footer", getEffectiveFooter(c.Footer, cfg.Footer)).
			AddBoolPtr("fallback_as_issue", c.FallbackAsIssue)
		// Add base_branch - use custom value if specified, otherwise use github.ref_name
		if c.BaseBranch != "" {
			builder.AddDefault("base_branch", c.BaseBranch)
		} else {
			builder.AddDefault("base_branch", "${{ github.ref_name }}")
		}
		return builder.Build()
	},
	"push_to_pull_request_branch": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.PushToPullRequestBranch == nil {
			return nil
		}
		c := cfg.PushToPullRequestBranch
		maxPatchSize := 1024 // default 1024 KB
		if cfg.MaximumPatchSize > 0 {
			maxPatchSize = cfg.MaximumPatchSize
		}
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddIfNotEmpty("title_prefix", c.TitlePrefix).
			AddStringSlice("labels", c.Labels).
			AddIfNotEmpty("if_no_changes", c.IfNoChanges).
			AddIfNotEmpty("commit_title_suffix", c.CommitTitleSuffix).
			AddDefault("base_branch", "${{ github.ref_name }}").
			AddDefault("max_patch_size", maxPatchSize).
			Build()
	},
	"update_pull_request": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UpdatePullRequests == nil {
			return nil
		}
		c := cfg.UpdatePullRequests
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddBoolPtrOrDefault("allow_title", c.Title, true).
			AddBoolPtrOrDefault("allow_body", c.Body, true).
			AddStringPtr("default_operation", c.Operation).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			Build()
	},
	"close_pull_request": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.ClosePullRequests == nil {
			return nil
		}
		c := cfg.ClosePullRequests
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("target", c.Target).
			AddStringSlice("required_labels", c.RequiredLabels).
			AddIfNotEmpty("required_title_prefix", c.RequiredTitlePrefix).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			Build()
	},
	"hide_comment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.HideComment == nil {
			return nil
		}
		c := cfg.HideComment
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddStringSlice("allowed_reasons", c.AllowedReasons).
			AddIfNotEmpty("target-repo", c.TargetRepoSlug).
			AddStringSlice("allowed_repos", c.AllowedRepos).
			Build()
	},
	"dispatch_workflow": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.DispatchWorkflow == nil {
			return nil
		}
		c := cfg.DispatchWorkflow
		builder := newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddStringSlice("workflows", c.Workflows)

		// Add workflow_files map if it has entries
		if len(c.WorkflowFiles) > 0 {
			builder.AddDefault("workflow_files", c.WorkflowFiles)
		}

		return builder.Build()
	},
	"missing_tool": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.MissingTool == nil {
			return nil
		}
		c := cfg.MissingTool
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			Build()
	},
	"missing_data": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.MissingData == nil {
			return nil
		}
		c := cfg.MissingData
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			Build()
	},
	// Note: "noop" is intentionally NOT included here because it is always processed
	// by a dedicated standalone step (see notify_comment.go buildConclusionJob).
	// Adding it to the handler manager would create duplicate configuration overhead.
	"autofix_code_scanning_alert": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AutofixCodeScanningAlert == nil {
			return nil
		}
		c := cfg.AutofixCodeScanningAlert
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("github-token", c.GitHubToken).
			Build()
	},
	// Note: create_project, update_project and create_project_status_update are handled by the unified handler,
	// not the separate project handler manager, so they are included in this registry.
	"create_project": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreateProjects == nil {
			return nil
		}
		c := cfg.CreateProjects
		builder := newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("target_owner", c.TargetOwner).
			AddIfNotEmpty("title_prefix", c.TitlePrefix).
			AddIfNotEmpty("github-token", c.GitHubToken)
		if len(c.Views) > 0 {
			builder.AddDefault("views", c.Views)
		}
		if len(c.FieldDefinitions) > 0 {
			builder.AddDefault("field_definitions", c.FieldDefinitions)
		}
		return builder.Build()
	},
	"update_project": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UpdateProjects == nil {
			return nil
		}
		c := cfg.UpdateProjects
		builder := newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("github-token", c.GitHubToken).
			AddIfNotEmpty("project", c.Project)
		if len(c.Views) > 0 {
			builder.AddDefault("views", c.Views)
		}
		if len(c.FieldDefinitions) > 0 {
			builder.AddDefault("field_definitions", c.FieldDefinitions)
		}
		return builder.Build()
	},
	"create_project_status_update": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreateProjectStatusUpdates == nil {
			return nil
		}
		c := cfg.CreateProjectStatusUpdates
		return newHandlerConfigBuilder().
			AddIfPositive("max", c.Max).
			AddIfNotEmpty("github-token", c.GitHubToken).
			AddIfNotEmpty("project", c.Project).
			Build()
	},
}

func (c *Compiler) addHandlerManagerConfigEnvVar(steps *[]string, data *WorkflowData) {
	if data.SafeOutputs == nil {
		compilerSafeOutputsConfigLog.Print("No safe-outputs configuration, skipping handler manager config")
		return
	}

	compilerSafeOutputsConfigLog.Print("Building handler manager configuration for safe-outputs")
	config := make(map[string]map[string]any)

	// Build configuration for each handler using the registry
	for handlerName, builder := range handlerRegistry {
		handlerConfig := builder(data.SafeOutputs)
		// Include handler if:
		// 1. It returns a non-nil config (explicitly enabled, even if empty)
		// 2. For auto-enabled handlers, include even with empty config
		if handlerConfig != nil {
			compilerSafeOutputsConfigLog.Printf("Adding %s handler configuration", handlerName)
			config[handlerName] = handlerConfig
		}
	}

	// Only add the env var if there are handlers to configure
	if len(config) > 0 {
		compilerSafeOutputsConfigLog.Printf("Marshaling handler config with %d handlers", len(config))
		configJSON, err := json.Marshal(config)
		if err != nil {
			consolidatedSafeOutputsLog.Printf("Failed to marshal handler config: %v", err)
			return
		}
		// Escape the JSON for YAML (handle quotes and special chars)
		configStr := string(configJSON)
		*steps = append(*steps, fmt.Sprintf("          GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: %q\n", configStr))
		compilerSafeOutputsConfigLog.Printf("Added handler config env var: size=%d bytes", len(configStr))
	} else {
		compilerSafeOutputsConfigLog.Print("No handlers configured, skipping config env var")
	}
}

// addAllSafeOutputConfigEnvVars adds environment variables for all enabled safe output types
