package workflow

import "github.com/github/gh-aw/pkg/logger"

var updateProjectLog = logger.New("workflow:update_project")

// ProjectView defines a project view configuration
type ProjectView struct {
	Name          string `yaml:"name" json:"name"`
	Layout        string `yaml:"layout" json:"layout"`
	Filter        string `yaml:"filter,omitempty" json:"filter,omitempty"`
	VisibleFields []int  `yaml:"visible-fields,omitempty" json:"visible_fields,omitempty"`
	Description   string `yaml:"description,omitempty" json:"description,omitempty"`
}

// ProjectFieldDefinition defines a project custom field configuration
// used by update_project operation=create_fields.
type ProjectFieldDefinition struct {
	Name     string   `yaml:"name" json:"name"`
	DataType string   `yaml:"data-type" json:"data_type"`
	Options  []string `yaml:"options,omitempty" json:"options,omitempty"`
}

// UpdateProjectConfig holds configuration for unified project board management
type UpdateProjectConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	GitHubToken          string                   `yaml:"github-token,omitempty"`
	Project              string                   `yaml:"project,omitempty"` // Default project URL for operations
	Views                []ProjectView            `yaml:"views,omitempty"`
	FieldDefinitions     []ProjectFieldDefinition `yaml:"field-definitions,omitempty" json:"field_definitions,omitempty"`
}

// parseUpdateProjectConfig handles update-project configuration
func (c *Compiler) parseUpdateProjectConfig(outputMap map[string]any) *UpdateProjectConfig {
	if configData, exists := outputMap["update-project"]; exists {
		updateProjectLog.Print("Parsing update-project configuration")
		updateProjectConfig := &UpdateProjectConfig{}
		updateProjectConfig.Max = 10 // Default max is 10

		if configMap, ok := configData.(map[string]any); ok {
			// Parse base config (max, github-token)
			c.parseBaseSafeOutputConfig(configMap, &updateProjectConfig.BaseSafeOutputConfig, 10)

			// Parse github-token override if specified
			if token, exists := configMap["github-token"]; exists {
				if tokenStr, ok := token.(string); ok {
					updateProjectConfig.GitHubToken = tokenStr
					updateProjectLog.Print("Using custom GitHub token for update-project")
				}
			}

			// Parse project URL override if specified
			if project, exists := configMap["project"]; exists {
				if projectStr, ok := project.(string); ok {
					updateProjectConfig.Project = projectStr
					updateProjectLog.Printf("Using custom project URL for update-project: %s", projectStr)
				}
			}

			// Parse views if specified
			if viewsData, exists := configMap["views"]; exists {
				if viewsList, ok := viewsData.([]any); ok {
					for i, viewItem := range viewsList {
						if viewMap, ok := viewItem.(map[string]any); ok {
							view := ProjectView{}

							// Parse name (required)
							if name, exists := viewMap["name"]; exists {
								if nameStr, ok := name.(string); ok {
									view.Name = nameStr
								}
							}

							// Parse layout (required)
							if layout, exists := viewMap["layout"]; exists {
								if layoutStr, ok := layout.(string); ok {
									view.Layout = layoutStr
								}
							}

							// Parse filter (optional)
							if filter, exists := viewMap["filter"]; exists {
								if filterStr, ok := filter.(string); ok {
									view.Filter = filterStr
								}
							}

							// Parse visible-fields (optional)
							if visibleFields, exists := viewMap["visible-fields"]; exists {
								if fieldsList, ok := visibleFields.([]any); ok {
									for _, field := range fieldsList {
										if fieldInt, ok := field.(int); ok {
											view.VisibleFields = append(view.VisibleFields, fieldInt)
										}
									}
								}
							}

							// Parse description (optional)
							if description, exists := viewMap["description"]; exists {
								if descStr, ok := description.(string); ok {
									view.Description = descStr
								}
							}

							// Only add view if it has required fields
							if view.Name != "" && view.Layout != "" {
								updateProjectConfig.Views = append(updateProjectConfig.Views, view)
								updateProjectLog.Printf("Parsed view %d: %s (%s)", i+1, view.Name, view.Layout)
							} else {
								updateProjectLog.Printf("Skipping invalid view %d: missing required fields", i+1)
							}
						}
					}
				}
			}

			// Parse field-definitions if specified
			fieldsData, hasFields := configMap["field-definitions"]
			if !hasFields {
				// Allow underscore variant as well
				fieldsData, hasFields = configMap["field_definitions"]
			}
			if hasFields {
				if fieldsList, ok := fieldsData.([]any); ok {
					for i, fieldItem := range fieldsList {
						fieldMap, ok := fieldItem.(map[string]any)
						if !ok {
							continue
						}

						field := ProjectFieldDefinition{}

						if name, exists := fieldMap["name"]; exists {
							if nameStr, ok := name.(string); ok {
								field.Name = nameStr
							}
						}

						dataType, hasDataType := fieldMap["data-type"]
						if !hasDataType {
							dataType = fieldMap["data_type"]
						}
						if dataTypeStr, ok := dataType.(string); ok {
							field.DataType = dataTypeStr
						}

						if options, exists := fieldMap["options"]; exists {
							if optionsList, ok := options.([]any); ok {
								for _, opt := range optionsList {
									if optStr, ok := opt.(string); ok {
										field.Options = append(field.Options, optStr)
									}
								}
							}
						}

						if field.Name != "" && field.DataType != "" {
							updateProjectConfig.FieldDefinitions = append(updateProjectConfig.FieldDefinitions, field)
							updateProjectLog.Printf("Parsed field definition %d: %s (%s)", i+1, field.Name, field.DataType)
						}
					}
				}
			}
		}

		updateProjectLog.Printf("Parsed update-project config: max=%d, hasCustomToken=%v, hasCustomProject=%v, viewCount=%d, fieldDefinitionCount=%d",
			updateProjectConfig.Max, updateProjectConfig.GitHubToken != "", updateProjectConfig.Project != "", len(updateProjectConfig.Views), len(updateProjectConfig.FieldDefinitions))
		return updateProjectConfig
	}
	updateProjectLog.Print("No update-project configuration found")
	return nil
}
