package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type WorkQueueIssuesConfig struct {
	Label             string `json:"label"`
	TitlePrefix       string `json:"title_prefix,omitempty"`
	OrphanedAfterDays int    `json:"orphaned_after_days"`
}

func workQueueProjectorPermissions() *Permissions {
	permissions := NewPermissions()
	permissions.Set(PermissionContents, PermissionWrite)
	permissions.Set(PermissionIssues, PermissionWrite)
	permissions.Set(PermissionActions, PermissionRead)
	return permissions
}

func (c *Compiler) workQueueProjectorTokenSteps(data *WorkflowData, job string) []string {
	if data.SafeOutputs == nil || data.SafeOutputs.GitHubApp == nil {
		return nil
	}
	steps := c.buildGitHubAppTokenMintStepForJob(
		job, data.SafeOutputs.GitHubApp, workQueueProjectorPermissions(), "",
		"", "Generate Work queue projector token", "work-queue-projector-token",
	)
	if job == "conclusion" {
		steps = injectStepCondition([]string{strings.Join(steps, "")}, BuildFunctionCall("always"))
	}
	return steps
}

func (c *Compiler) addWorkQueueProjectorToken(steps *[]string, data *WorkflowData) {
	if data.SafeOutputs == nil || data.SafeOutputs.GitHubApp == nil {
		c.addSafeOutputGitHubTokenForConfig(steps, data, "")
		return
	}
	token := "${{ steps.work-queue-projector-token.outputs.token }}"
	if data.SafeOutputs.GitHubApp.shouldIgnoreMissingKey() {
		token = combineTokenExpressions(token, resolveSafeOutputGitHubToken(data.SafeOutputs.GitHubToken))
	}
	*steps = append(*steps, fmt.Sprintf("          github-token: %s\n", token))
}

func workQueueIssuesConfig(data *WorkflowData) *WorkQueueIssuesConfig {
	if data == nil {
		return nil
	}
	config, ok := data.Tools["work-queue"].(map[string]any)
	if !ok {
		return nil
	}
	value, exists := config["issues"]
	if !exists || value == false {
		return nil
	}
	result := &WorkQueueIssuesConfig{Label: "work", OrphanedAfterDays: 30}
	if fields, ok := value.(map[string]any); ok {
		if label, ok := fields["label"].(string); ok {
			result.Label = label
		}
		if prefix, ok := fields["title_prefix"].(string); ok {
			result.TitlePrefix = prefix
		}
		if days, ok := workQueueOrphanedAfterDays(fields["orphaned_after_days"]); ok {
			result.OrphanedAfterDays = days
		}
	}
	return result
}

func workQueueOrphanedAfterDays(value any) (int, bool) {
	switch days := value.(type) {
	case int:
		return days, days >= 1 && days <= 3650
	case float64:
		if days != float64(int(days)) {
			return 0, false
		}
		integer := int(days)
		return integer, integer >= 1 && integer <= 3650
	default:
		return 0, false
	}
}

// WorkQueueIssueMirroringEnabled reports whether any compiled workflow enables
// work queue Issue mirroring.
func WorkQueueIssueMirroringEnabled(workflowDataList []*WorkflowData) bool {
	for _, data := range workflowDataList {
		if workQueueIssuesConfig(data) != nil {
			return true
		}
	}
	return false
}

func validateWorkQueueIssuesConfig(data *WorkflowData) error {
	config, ok := data.Tools["work-queue"].(map[string]any)
	if !ok {
		return nil
	}
	value, exists := config["issues"]
	if !exists {
		return nil
	}
	if _, ok := value.(bool); ok {
		return nil
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return errors.New("tools.work-queue.issues must be true, false, or an object with optional label, title_prefix, and orphaned_after_days fields")
	}
	for key, value := range fields {
		if key == "orphaned_after_days" {
			if _, ok := workQueueOrphanedAfterDays(value); !ok {
				return errors.New("tools.work-queue.issues.orphaned_after_days must be an integer between 1 and 3650")
			}
			continue
		}
		if key != "label" && key != "title_prefix" {
			return fmt.Errorf("tools.work-queue.issues: unsupported field %q; use label, title_prefix, or orphaned_after_days", key)
		}
		text, ok := value.(string)
		maxBytes := 33
		if key == "title_prefix" {
			maxBytes = 80
		}
		if !ok || strings.TrimSpace(text) == "" || len(text) > maxBytes || strings.Contains(text, "${{") || strings.IndexFunc(text, func(r rune) bool { return r < 32 || (r >= 127 && r <= 159) }) >= 0 {
			return fmt.Errorf("tools.work-queue.issues.%s must be a nonblank literal of at most %d bytes", key, maxBytes)
		}
	}
	return nil
}

func configureWorkQueueIssues(data *WorkflowData, policy *WorkQueuePolicyConfig) error {
	if issues := workQueueIssuesConfig(data); issues != nil {
		encoded, err := json.Marshal(issues)
		if err != nil {
			return fmt.Errorf("encode work queue Issues configuration: %w", err)
		}
		policy.IssuesJSON = string(encoded)
	}
	return nil
}

func workQueueIssuesEnvironment(data *WorkflowData) []string {
	config := workQueueIssuesConfig(data)
	if config == nil || !isWorkQueueParticipant(data) {
		return nil
	}
	return []string{
		fmt.Sprintf("          GH_AW_WORK_QUEUE_ISSUES: %q\n", data.WorkQueuePolicy.IssuesJSON),
		"          GH_AW_WORK_QUEUE_CHECKED_TRANSPORT: \"graphql\"\n",
	}
}

func (c *Compiler) buildWorkQueueIssuesStep(data *WorkflowData) []string {
	if workQueueIssuesConfig(data) == nil || !isWorkQueueParticipant(data) {
		return nil
	}
	steps := c.workQueueProjectorTokenSteps(data, "conclusion")
	steps = append(steps,
		"      - name: Project this run's work queue Issues\n",
		"        id: work_queue_issues\n",
		"        if: always()\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/github-script")),
	)
	steps = append(steps, workQueuePolicyEnvironment(data)...)
	var staged *TemplatableBool
	if data.SafeOutputs != nil {
		staged = data.SafeOutputs.Staged
	}
	if value := resolveSafeOutputsStagedValue(c.trialMode, staged); value != nil {
		steps = append(steps, "          GH_AW_SAFE_OUTPUTS_STAGED: "+fmt.Sprintf("%q", *value)+"\n")
	}
	steps = append(steps, "          GH_AW_WORK_QUEUE_JOB_RESULTS: ${{ toJSON(needs) }}\n", "        with:\n")
	c.addWorkQueueProjectorToken(&steps, data)
	return append(steps,
		"          script: |\n",
		"            const { main } = require('${{ runner.temp }}/gh-aw/actions/work_queue_issues.cjs');\n",
		"            const result = await main({ core, githubClient: github, context });\n",
		"            if (result.pending?.length) core.warning('Work queue is committed; Issue synchronization remains pending. Inspect the exact terminal request before using work-queue sync-issues --request-id.');\n",
	)
}
