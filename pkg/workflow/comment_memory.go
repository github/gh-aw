package workflow

import "github.com/github/gh-aw/pkg/logger"

var commentMemoryLog = logger.New("workflow:comment_memory")

// CommentMemoryConfig holds parsed tools.comment-memory configuration.
type CommentMemoryConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Target               string   `yaml:"target,omitempty"`        // Target: "triggering" (default), "*" or explicit issue/PR number
	TargetRepoSlug       string   `yaml:"target-repo,omitempty"`   // Target repository in owner/repo format
	AllowedRepos         []string `yaml:"allowed-repos,omitempty"` // Additional allowed repositories
	MemoryID             string   `yaml:"memory-id,omitempty"`     // Default memory identifier when item does not provide memory_id
}

const commentMemoryHandlerKey = "comment_memory"

func buildCommentMemoryHandlerConfig(config *CommentMemoryConfig, globalFooter *bool, globalBodyFooter string) map[string]any {
	if config == nil {
		return nil
	}
	return newHandlerConfigBuilder().
		AddTemplatableInt("max", config.Max).
		AddIfNotEmpty("target", config.Target).
		AddIfNotEmpty("target-repo", config.TargetRepoSlug).
		AddStringSlice("allowed_repos", config.AllowedRepos).
		AddIfNotEmpty("memory_id", config.MemoryID).
		AddIfNotEmpty("body_footer", appendBodyFooters(config.BodyFooter, globalBodyFooter)).
		AddTemplatableBool("footer", getEffectiveFooterForTemplatable(config.Footer, globalFooter)).
		AddIfNotEmpty("github-token", resolveHandlerGitHubTokenWithStepID(config.GitHubApp, "comment-memory-app-token", config.GitHubToken)).
		AddTemplatableBool("staged", templatableBoolPtrToStringPtr(config.Staged)).
		Build()
}

// extractCommentMemoryConfig extracts comment-memory configuration from tools section.
func (c *Compiler) extractCommentMemoryConfig(toolsConfig *ToolsConfig) *CommentMemoryConfig {
	if toolsConfig == nil || toolsConfig.CommentMemory == nil {
		return nil
	}
	return c.parseCommentMemoryConfigValue(toolsConfig.CommentMemory.Raw)
}

// parseCommentMemoryConfigValue handles comment-memory configuration values.
func (c *Compiler) parseCommentMemoryConfigValue(rawConfig any) *CommentMemoryConfig {
	switch v := rawConfig.(type) {
	case nil:
		commentMemoryLog.Print("comment-memory explicitly disabled with null")
		return nil
	case bool:
		if !v {
			commentMemoryLog.Print("comment-memory explicitly disabled with false")
			return nil
		}
		return &CommentMemoryConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{
				Max: defaultIntStr(1),
			},
			MemoryID: "default",
		}
	}

	commentMemoryLog.Print("Parsing comment-memory configuration")

	configData, _ := rawConfig.(map[string]any)
	if err := preprocessIntFieldAsString(configData, "max", commentMemoryLog); err != nil {
		commentMemoryLog.Printf("Invalid max value: %v", err)
		return nil
	}
	if err := preprocessBoolFieldAsString(configData, "footer", commentMemoryLog); err != nil {
		commentMemoryLog.Printf("Invalid footer value: %v", err)
		return nil
	}

	var config CommentMemoryConfig
	normalizedOutputMap := map[string]any{"comment-memory": rawConfig}
	if err := unmarshalConfig(normalizedOutputMap, "comment-memory", &config, commentMemoryLog); err != nil {
		commentMemoryLog.Printf("Failed to unmarshal config: %v", err)
		config = CommentMemoryConfig{}
	}

	if config.Max == nil {
		config.Max = defaultIntStr(1)
	}
	if config.MemoryID == "" {
		config.MemoryID = "default"
	}

	return &config
}
