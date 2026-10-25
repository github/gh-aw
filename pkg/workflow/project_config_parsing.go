package workflow

import "github.com/github/gh-aw/pkg/logger"

// parseProjectViews parses the "views" list from a project config map.
// Only views with both name and layout fields are included; invalid ones are skipped.
func parseProjectViews(configMap map[string]any, debugLog *logger.Logger) []ProjectView {
	viewsData, exists := configMap["views"]
	if !exists {
		return nil
	}
	viewsList, ok := viewsData.([]any)
	if !ok {
		return nil
	}

	var views []ProjectView
	for i, viewItem := range viewsList {
		viewMap, ok := viewItem.(map[string]any)
		if !ok {
			continue
		}
		view := parseProjectView(viewMap)
		// Only add view if it has required fields.
		if view.Name != "" && view.Layout != "" {
			views = append(views, view)
			debugLog.Printf("Parsed view %d: %s (%s)", i+1, view.Name, view.Layout)
		} else {
			debugLog.Printf("Skipping invalid view %d: missing required fields", i+1)
		}
	}
	return views
}

func parseProjectView(viewMap map[string]any) ProjectView {
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

	return view
}

// parseProjectFieldDefinitions parses the "field-definitions" (or "field_definitions") list
// from a project config map. Only fields with both name and data-type are included.
func parseProjectFieldDefinitions(configMap map[string]any, debugLog *logger.Logger) []ProjectFieldDefinition {
	fieldsData, hasFields := configMap["field-definitions"]
	if !hasFields {
		// Allow underscore variant as well
		fieldsData, hasFields = configMap["field_definitions"]
	}
	if !hasFields {
		return nil
	}
	fieldsList, ok := fieldsData.([]any)
	if !ok {
		return nil
	}

	var fields []ProjectFieldDefinition
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
			fields = append(fields, field)
			debugLog.Printf("Parsed field definition %d: %s (%s)", i+1, field.Name, field.DataType)
		}
	}
	return fields
}
