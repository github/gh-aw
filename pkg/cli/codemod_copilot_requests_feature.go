package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var copilotRequestsFeatureCodemodLog = logger.New("cli:codemod_copilot_requests_feature")

// getCopilotRequestsFeatureToPermissionsCodemod migrates features.copilot-requests to permissions.copilot-requests: write.
func getCopilotRequestsFeatureToPermissionsCodemod() Codemod {
	return Codemod{
		ID:           "features-copilot-requests-to-permissions",
		Name:         "Migrate features.copilot-requests to permissions",
		Description:  "Removes deprecated features.copilot-requests and adds permissions.copilot-requests: write when enabled.",
		IntroducedIn: "1.0.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			featuresAny, hasFeatures := frontmatter["features"]
			if !hasFeatures {
				return content, false, nil
			}
			featuresMap, ok := featuresAny.(map[string]any)
			if !ok {
				return content, false, nil
			}

			featureValue, hasFeature := featuresMap["copilot-requests"]
			if !hasFeature {
				return content, false, nil
			}

			enabled, isBool := featureValue.(bool)
			if !isBool {
				return content, false, nil
			}

			if enabled && !canSafelyAddCopilotRequestsPermission(frontmatter) {
				copilotRequestsFeatureCodemodLog.Print("Skipping migration: unable to safely add permissions.copilot-requests")
				return content, false, nil
			}

			addPermission := enabled && !frontmatterHasCopilotRequestsPermission(frontmatter)
			newContent, applied, err := applyFrontmatterLineTransform(content, func(lines []string) ([]string, bool) {
				result, modified := removeFieldFromBlock(lines, "copilot-requests", "features")
				if !modified {
					return lines, false
				}

				if addPermission {
					result = ensureCopilotRequestsWritePermission(result)
				}

				return result, true
			})
			if applied {
				if addPermission {
					copilotRequestsFeatureCodemodLog.Print("Migrated features.copilot-requests to permissions.copilot-requests: write")
				} else {
					copilotRequestsFeatureCodemodLog.Print("Removed deprecated features.copilot-requests")
				}
			}
			return newContent, applied, err
		},
	}
}

func frontmatterHasCopilotRequestsPermission(frontmatter map[string]any) bool {
	permissionsAny, hasPermissions := frontmatter["permissions"]
	if !hasPermissions {
		return false
	}
	permissionsMap, ok := permissionsAny.(map[string]any)
	if !ok {
		return false
	}
	_, hasCopilotRequests := permissionsMap["copilot-requests"]
	return hasCopilotRequests
}

func canSafelyAddCopilotRequestsPermission(frontmatter map[string]any) bool {
	permissionsAny, hasPermissions := frontmatter["permissions"]
	if !hasPermissions {
		return true
	}
	if _, ok := permissionsAny.(map[string]any); ok {
		return true
	}

	strValue, ok := permissionsAny.(string)
	if !ok {
		return false
	}
	trimmed := strings.TrimSpace(strValue)
	return trimmed == "" || trimmed == "{}"
}

func ensureCopilotRequestsWritePermission(lines []string) []string {
	return ensureCopilotRequestsPermission(lines, "write", false)
}

func ensureCopilotRequestsPermission(lines []string, level string, replaceExisting bool) []string {
	permissionsIdx := -1
	permissionsIndent := ""
	permissionsEnd := len(lines)
	for i, line := range lines {
		if isTopLevelKey(line) && strings.HasPrefix(strings.TrimSpace(line), "permissions:") {
			permissionsIdx = i
			permissionsIndent = getIndentation(line)
			for j := i + 1; j < len(lines); j++ {
				if isTopLevelKey(lines[j]) {
					permissionsEnd = j
					break
				}
			}
			break
		}
	}
	if permissionsIdx == -1 {
		insertAt := findPermissionsInsertIndex(lines)
		result := make([]string, 0, len(lines)+2)
		result = append(result, lines[:insertAt]...)
		result = append(result, "permissions:", "  copilot-requests: "+level)
		result = append(result, lines[insertAt:]...)
		return result
	}
	trimmedPermissionsLine := strings.TrimSpace(lines[permissionsIdx])
	inlineValue := strings.TrimSpace(strings.TrimPrefix(trimmedPermissionsLine, "permissions:"))
	if inlineValue != "" && !strings.HasPrefix(inlineValue, "#") {
		// Extract the value before any comment
		valuePart := inlineValue
		if beforeComment, _, hasComment := strings.Cut(inlineValue, "#"); hasComment {
			valuePart = strings.TrimSpace(beforeComment)
		}
		if valuePart == "{}" {
			result := make([]string, 0, len(lines)+1)
			result = append(result, lines[:permissionsIdx]...)
			result = append(result, permissionsIndent+"permissions:")
			result = append(result, permissionsIndent+"  copilot-requests: "+level)
			result = append(result, lines[permissionsIdx+1:]...)
			return result
		}
		return lines
	}
	for i := permissionsIdx + 1; i < permissionsEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if parseYAMLMapKey(trimmed) == "copilot-requests" {
			if replaceExisting {
				result := append([]string(nil), lines...)
				result[i] = getIndentation(lines[i]) + "copilot-requests: " + level
				return result
			}
			return lines
		}
	}

	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:permissionsEnd]...)
	result = append(result, permissionsIndent+"  copilot-requests: "+level)
	result = append(result, lines[permissionsEnd:]...)
	return result
}
