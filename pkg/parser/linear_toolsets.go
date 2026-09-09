package parser

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

var linearToolsets = map[string][]string{
	"attachments":    {"extract_images", "get_attachment"},
	"comments":       {"list_comments"},
	"customers":      {"list_customers"},
	"cycles":         {"list_cycles"},
	"diffs":          {"get_diff", "get_diff_threads", "list_diffs"},
	"documentation":  {"search_documentation"},
	"documents":      {"get_document", "list_documents"},
	"initiatives":    {"get_initiative", "list_initiatives"},
	"issues":         {"get_issue", "get_issue_status", "list_issue_labels", "list_issue_statuses", "list_issues"},
	"milestones":     {"get_milestone", "list_milestones"},
	"projects":       {"get_project", "list_project_labels", "list_projects"},
	"status_updates": {"get_status_updates"},
	"teams":          {"get_team", "list_teams"},
	"users":          {"get_user", "list_users"},
}

var linearToolsetNames = func() []string {
	names := make([]string, 0, len(linearToolsets)+1)
	names = append(names, "all")
	for name := range linearToolsets {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}()

// ParseLinearToolsets validates and expands Linear toolsets into MCP tool names.
func ParseLinearToolsets(value any) ([]string, error) {
	var names []string
	switch typed := value.(type) {
	case string:
		names = []string{typed}
	case []any:
		names = make([]string, 0, len(typed))
		for _, item := range typed {
			name, ok := item.(string)
			if !ok {
				return nil, errors.New("tools.linear.toolsets must contain only toolset names")
			}
			names = append(names, name)
		}
	default:
		return nil, errors.New("tools.linear.toolsets must be a toolset name or a non-empty array of toolset names")
	}
	if len(names) == 0 {
		return nil, errors.New("tools.linear.toolsets must be a toolset name or a non-empty array of toolset names")
	}

	seenToolsets := make(map[string]struct{}, len(names))
	hasAll := false
	for _, name := range names {
		if name == "" {
			return nil, errors.New("tools.linear.toolsets must not contain empty strings")
		}
		if strings.TrimSpace(name) != name {
			return nil, fmt.Errorf("tools.linear.toolsets must not contain whitespace-padded names; got %q", name)
		}
		if _, duplicate := seenToolsets[name]; duplicate {
			return nil, fmt.Errorf("tools.linear.toolsets contains duplicate toolset %q", name)
		}
		seenToolsets[name] = struct{}{}
		if name == "all" {
			hasAll = true
		} else if _, ok := linearToolsets[name]; !ok {
			return nil, fmt.Errorf("unknown Linear toolset %q; valid toolsets are: %s", name, strings.Join(linearToolsetNames, ", "))
		}
	}
	if hasAll {
		return []string{"*"}, nil
	}

	var tools []string
	for _, name := range names {
		tools = append(tools, linearToolsets[name]...)
	}
	slices.Sort(tools)
	return slices.Compact(tools), nil
}

// ValidateLinearAllowedForToolsets checks that every allowed pattern selects
// at least one tool from the configured Linear toolsets. The all toolset accepts
// every allowed pattern to remain compatible with tools added by Linear.
func ValidateLinearAllowedForToolsets(allowed, toolsetTools []string) error {
	if slices.Contains(toolsetTools, "*") {
		return nil
	}
	for _, pattern := range allowed {
		matches := false
		for _, tool := range toolsetTools {
			if matched, err := path.Match(pattern, tool); err == nil && matched {
				matches = true
				break
			}
		}
		if !matches {
			return fmt.Errorf("tools.linear.allowed pattern %q does not match any tool in the configured toolsets", pattern)
		}
	}
	return nil
}
