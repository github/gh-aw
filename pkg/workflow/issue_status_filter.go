package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// IssueStatusFilterConfig matches the captured transition, not the issue snapshot.
type IssueStatusFilterConfig struct {
	StatusIDs        []int64  `json:"status-ids,omitempty"`
	StatusCategories []string `json:"status-categories,omitempty"`
}

func extractIssueStatusFilter(frontmatter map[string]any) (*IssueStatusFilterConfig, error) {
	on, _ := frontmatter["on"].(map[string]any)
	issues, _ := on["issues"].(map[string]any)
	ids, hasIDs := issues["status-ids"]
	categories, hasCategories := issues["status-categories"]
	if !hasIDs && !hasCategories {
		return nil, nil
	}
	filterFields := map[string]any{}
	if hasIDs {
		filterFields["status-ids"] = ids
	}
	if hasCategories {
		filterFields["status-categories"] = categories
	}
	raw, err := json.Marshal(filterFields)
	if err != nil {
		return nil, fmt.Errorf("invalid on.issues status filters: %w", err)
	}
	var config IssueStatusFilterConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("invalid on.issues status filters: %w", err)
	}
	if hasIDs && len(config.StatusIDs) == 0 || hasCategories && len(config.StatusCategories) == 0 {
		return nil, errors.New("on.issues status filters must be nonempty arrays")
	}
	for _, id := range config.StatusIDs {
		if id <= 0 || id > 9007199254740991 {
			return nil, errors.New("on.issues.status-ids must contain positive safe integers")
		}
	}
	for _, category := range config.StatusCategories {
		if !slices.Contains([]string{"backlog", "planned", "started", "completed", "not_planned", "duplicate"}, category) {
			return nil, fmt.Errorf("invalid on.issues.status-categories value %q", category)
		}
	}
	types := parseStringSliceAny(issues["types"], filtersLog)
	if !slices.Contains(types, "status_changed") {
		return nil, errors.New("on.issues status filters require explicit types including status_changed")
	}
	return &config, nil
}

func (c *Compiler) appendIssueStatusFilterStep(data *WorkflowData, steps []string) []string {
	if data.IssueStatusFilter == nil {
		return steps
	}
	configJSON, _ := json.Marshal(data.IssueStatusFilter) //nolint:jsonmarshalignoredeerror // Only integer and string slices.
	steps = append(steps,
		"      - name: Check issue status filters\n",
		"        id: check_issue_status\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		fmt.Sprintf("          GH_AW_ISSUE_STATUS_FILTER: %q\n", string(configJSON)),
		"        with:\n",
		"          script: |\n",
	)
	return append(steps, generateGitHubScriptWithRequire("check_issue_status.cjs"))
}
