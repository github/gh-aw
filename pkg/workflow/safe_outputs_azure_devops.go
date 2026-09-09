package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var azureDevOpsSafeOutputsLog = logger.New("workflow:safe_outputs_azure_devops")

type AzureDevOpsArtifactLinkConfig struct {
	Enabled    bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Repository string `yaml:"repository,omitempty" json:"repository,omitempty"`
	Branch     string `yaml:"branch,omitempty" json:"branch,omitempty"`
}

type CreateWorkItemConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	WorkItemType         string                        `yaml:"work-item-type,omitempty"`
	DescriptionField     string                        `yaml:"description-field,omitempty"`
	AreaPath             string                        `yaml:"area-path,omitempty"`
	IterationPath        string                        `yaml:"iteration-path,omitempty"`
	Assignee             string                        `yaml:"assignee,omitempty"`
	Tags                 []string                      `yaml:"tags,omitempty"`
	AllowedTags          []string                      `yaml:"allowed-tags,omitempty"`
	CustomFields         map[string]string             `yaml:"custom-fields,omitempty"`
	ArtifactLink         AzureDevOpsArtifactLinkConfig `yaml:"artifact-link,omitempty"`
}

type UpdateWorkItemConfig struct {
	BaseSafeOutputConfig     `yaml:",inline"`
	Status                   bool     `yaml:"status,omitempty"`
	Title                    bool     `yaml:"title,omitempty"`
	Body                     bool     `yaml:"body,omitempty"`
	MarkdownBody             bool     `yaml:"markdown-body,omitempty"`
	TitlePrefix              string   `yaml:"title-prefix,omitempty"`
	TagPrefix                string   `yaml:"tag-prefix,omitempty"`
	Target                   any      `yaml:"target,omitempty"`
	AreaPath                 bool     `yaml:"area-path,omitempty"`
	IterationPath            bool     `yaml:"iteration-path,omitempty"`
	Assignee                 bool     `yaml:"assignee,omitempty"`
	Tags                     bool     `yaml:"tags,omitempty"`
	AllowedTags              []string `yaml:"allowed-tags,omitempty"`
	AllowedAreaPrefixes      []string `yaml:"allowed-area-prefixes,omitempty"`
	AllowedIterationPrefixes []string `yaml:"allowed-iteration-prefixes,omitempty"`
}

type CommentOnWorkItemConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Target               any `yaml:"target,omitempty"`
}

type AssignWorkItemConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Target               any      `yaml:"target,omitempty"`
	Allowed              []string `yaml:"allowed,omitempty"`
	Blocked              []string `yaml:"blocked,omitempty"`
}

type LinkWorkItemsConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Target               any      `yaml:"target,omitempty"`
	AllowedLinkTypes     []string `yaml:"allowed-link-types,omitempty"`
}

type UploadWorkItemAttachmentConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Target               any      `yaml:"target,omitempty"`
	MaxFileSize          int64    `yaml:"max-file-size,omitempty"`
	AllowedExtensions    []string `yaml:"allowed-extensions,omitempty"`
	CommentPrefix        string   `yaml:"comment-prefix,omitempty"`
}

func parseAzureDevOpsConfig[T any](c *Compiler, outputMap map[string]any, key string, defaultMax int, postProcess func(*T)) *T {
	config := parseConfigScaffold(outputMap, key, azureDevOpsSafeOutputsLog, func(err error) *T {
		azureDevOpsSafeOutputsLog.Printf("Failed to parse %s configuration: %v", key, err)
		return nil
	})
	if config == nil {
		return nil
	}
	if configMap, ok := outputMap[key].(map[string]any); ok {
		switch typed := any(config).(type) {
		case *CreateWorkItemConfig:
			c.parseBaseSafeOutputConfig(configMap, &typed.BaseSafeOutputConfig, defaultMax)
		case *UpdateWorkItemConfig:
			c.parseBaseSafeOutputConfig(configMap, &typed.BaseSafeOutputConfig, defaultMax)
		case *CommentOnWorkItemConfig:
			c.parseBaseSafeOutputConfig(configMap, &typed.BaseSafeOutputConfig, defaultMax)
		case *AssignWorkItemConfig:
			c.parseBaseSafeOutputConfig(configMap, &typed.BaseSafeOutputConfig, defaultMax)
		case *LinkWorkItemsConfig:
			c.parseBaseSafeOutputConfig(configMap, &typed.BaseSafeOutputConfig, defaultMax)
		case *UploadWorkItemAttachmentConfig:
			c.parseBaseSafeOutputConfig(configMap, &typed.BaseSafeOutputConfig, defaultMax)
		}
	}
	if postProcess != nil {
		postProcess(config)
	}
	return config
}

func (c *Compiler) parseCreateWorkItemConfig(outputMap map[string]any) *CreateWorkItemConfig {
	return parseAzureDevOpsConfig(c, outputMap, "ado-create-work-item", 1, func(config *CreateWorkItemConfig) {
		if config.WorkItemType == "" {
			config.WorkItemType = "Task"
		}
		if config.ArtifactLink.Branch == "" {
			config.ArtifactLink.Branch = "main"
		}
	})
}

func (c *Compiler) parseUpdateWorkItemConfig(outputMap map[string]any) *UpdateWorkItemConfig {
	return parseAzureDevOpsConfig[UpdateWorkItemConfig](c, outputMap, "ado-update-work-item", 1, nil)
}

func (c *Compiler) parseCommentOnWorkItemConfig(outputMap map[string]any) *CommentOnWorkItemConfig {
	return parseAzureDevOpsConfig[CommentOnWorkItemConfig](c, outputMap, "ado-comment-on-work-item", 1, nil)
}

func (c *Compiler) parseAssignWorkItemConfig(outputMap map[string]any) *AssignWorkItemConfig {
	return parseAzureDevOpsConfig[AssignWorkItemConfig](c, outputMap, "ado-assign-work-item", 1, nil)
}

func (c *Compiler) parseLinkWorkItemsConfig(outputMap map[string]any) *LinkWorkItemsConfig {
	return parseAzureDevOpsConfig[LinkWorkItemsConfig](c, outputMap, "ado-link-work-items", 5, nil)
}

func (c *Compiler) parseUploadWorkItemAttachmentConfig(outputMap map[string]any) *UploadWorkItemAttachmentConfig {
	return parseAzureDevOpsConfig(c, outputMap, "ado-upload-workitem-attachment", 1, func(config *UploadWorkItemAttachmentConfig) {
		if config.MaxFileSize == 0 {
			config.MaxFileSize = 5 * 1024 * 1024
		}
	})
}

func addAzureDevOpsTarget(builder *handlerConfigBuilder, target any) *handlerConfigBuilder {
	if target != nil {
		builder.AddDefault("target", target)
	}
	return builder
}

var azureDevOpsWorkItemHandlerRegistry = map[string]handlerBuilder{
	"ado_create_work_item": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CreateWorkItems == nil {
			return nil
		}

		c := cfg.CreateWorkItems
		return newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfNotEmpty("work_item_type", c.WorkItemType).
			AddIfNotEmpty("description_field", c.DescriptionField).
			AddIfNotEmpty("area_path", c.AreaPath).
			AddIfNotEmpty("iteration_path", c.IterationPath).
			AddIfNotEmpty("assignee", c.Assignee).
			AddStringSlice("tags", c.Tags).
			AddStringSlice("allowed_tags", c.AllowedTags).
			AddDefault("custom_fields", c.CustomFields).
			AddDefault("artifact_link", c.ArtifactLink).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged)).
			Build()
	},
	"ado_update_work_item": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UpdateWorkItems == nil {
			return nil
		}
		c := cfg.UpdateWorkItems
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddIfTrue("status", c.Status).
			AddIfTrue("title", c.Title).
			AddIfTrue("body", c.Body).
			AddIfTrue("markdown_body", c.MarkdownBody).
			AddIfNotEmpty("title_prefix", c.TitlePrefix).
			AddIfNotEmpty("tag_prefix", c.TagPrefix).
			AddIfTrue("area_path", c.AreaPath).
			AddIfTrue("iteration_path", c.IterationPath).
			AddIfTrue("assignee", c.Assignee).
			AddIfTrue("tags", c.Tags).
			AddStringSlice("allowed_tags", c.AllowedTags).
			AddStringSlice("allowed_area_prefixes", c.AllowedAreaPrefixes).
			AddStringSlice("allowed_iteration_prefixes", c.AllowedIterationPrefixes).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		return addAzureDevOpsTarget(builder, c.Target).Build()
	},
	"ado_comment_on_work_item": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.CommentOnWorkItems == nil {
			return nil
		}
		c := cfg.CommentOnWorkItems
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		return addAzureDevOpsTarget(builder, c.Target).Build()
	},
	"ado_assign_work_item": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.AssignWorkItems == nil {
			return nil
		}
		c := cfg.AssignWorkItems
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddStringSlice("allowed", c.Allowed).
			AddStringSlice("blocked", c.Blocked).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		return addAzureDevOpsTarget(builder, c.Target).Build()
	},
	"ado_link_work_items": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.LinkWorkItems == nil {
			return nil
		}
		c := cfg.LinkWorkItems
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddStringSlice("allowed_link_types", c.AllowedLinkTypes).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		return addAzureDevOpsTarget(builder, c.Target).Build()
	},
	"ado_upload_workitem_attachment": func(cfg *SafeOutputsConfig) map[string]any {
		if cfg.UploadWorkItemAttachments == nil {
			return nil
		}
		c := cfg.UploadWorkItemAttachments
		builder := newHandlerConfigBuilder().
			AddTemplatableInt("max", c.Max).
			AddDefault("max_file_size", c.MaxFileSize).
			AddStringSlice("allowed_extensions", c.AllowedExtensions).
			AddIfNotEmpty("comment_prefix", c.CommentPrefix).
			AddTemplatableBool("staged", templatableBoolPtrToStringPtr(c.Staged))
		return addAzureDevOpsTarget(builder, c.Target).Build()
	},
}

func appendAzureDevOpsTargetConstraint(constraints *[]string, target any) {
	if target != nil {
		*constraints = append(*constraints, fmt.Sprintf("Target: %v.", target))
	}
}

func createWorkItemConstraints(config *CreateWorkItemConfig) []string {
	return buildConstraints(config, func(config *CreateWorkItemConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d work item(s) can be created.")
		appendStringConstraint(constraints, config.WorkItemType, "Work item type: %q.")
		appendStringConstraint(constraints, config.AreaPath, "Area path: %q.")
		if len(config.AllowedTags) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these agent-provided tags are allowed: %s.", formatStringList(config.AllowedTags)))
		}
	})
}

func updateWorkItemConstraints(config *UpdateWorkItemConfig) []string {
	return buildConstraints(config, func(config *UpdateWorkItemConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d work item(s) can be updated.")
		appendAzureDevOpsTargetConstraint(constraints, config.Target)
		var fields []string
		for _, field := range []struct {
			name    string
			enabled bool
		}{
			{"state", config.Status},
			{"title", config.Title},
			{"body", config.Body},
			{"area_path", config.AreaPath},
			{"iteration_path", config.IterationPath},
			{"assignee", config.Assignee},
			{"tags", config.Tags},
		} {
			if field.enabled {
				fields = append(fields, field.name)
			}
		}
		if len(fields) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these fields can be updated: %s.", formatStringList(fields)))
		}
		if len(config.AllowedAreaPrefixes) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Area paths must match these prefixes: %s.", formatStringList(config.AllowedAreaPrefixes)))
		}
		if len(config.AllowedIterationPrefixes) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Iteration paths must match these prefixes: %s.", formatStringList(config.AllowedIterationPrefixes)))
		}
	})
}

func commentOnWorkItemConstraints(config *CommentOnWorkItemConfig) []string {
	return buildConstraints(config, func(config *CommentOnWorkItemConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d work-item comment(s) can be added.")
		appendAzureDevOpsTargetConstraint(constraints, config.Target)
	})
}

func assignWorkItemConstraints(config *AssignWorkItemConfig) []string {
	return buildConstraints(config, func(config *AssignWorkItemConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d work item(s) can be assigned.")
		appendAzureDevOpsTargetConstraint(constraints, config.Target)
		if len(config.Allowed) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these assignees are allowed: %s.", formatStringList(config.Allowed)))
		}
		if len(config.Blocked) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("These assignee patterns are blocked: %s.", formatStringList(config.Blocked)))
		}
	})
}

func linkWorkItemsConstraints(config *LinkWorkItemsConfig) []string {
	return buildConstraints(config, func(config *LinkWorkItemsConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d work-item link(s) can be created.")
		appendAzureDevOpsTargetConstraint(constraints, config.Target)
		if len(config.AllowedLinkTypes) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these link types are allowed: %s.", formatStringList(config.AllowedLinkTypes)))
		}
	})
}

func uploadWorkItemAttachmentConstraints(config *UploadWorkItemAttachmentConfig) []string {
	return buildConstraints(config, func(config *UploadWorkItemAttachmentConfig, constraints *[]string) {
		appendMaxConstraint(constraints, config.Max, "Maximum %d work-item attachment(s) can be uploaded.")
		appendAzureDevOpsTargetConstraint(constraints, config.Target)
		if config.MaxFileSize > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Maximum attachment size: %d bytes.", config.MaxFileSize))
		}
		if len(config.AllowedExtensions) > 0 {
			*constraints = append(*constraints, fmt.Sprintf("Only these file extensions are allowed: %s.", formatStringList(config.AllowedExtensions)))
		}
	})
}
