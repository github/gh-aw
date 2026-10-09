package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type WorkQueueIssuesConfig struct {
	Label       string `json:"label"`
	StatusField string `json:"status-field,omitempty"`
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
	result := &WorkQueueIssuesConfig{Label: "work"}
	if fields, ok := value.(map[string]any); ok {
		if label, ok := fields["label"].(string); ok {
			result.Label = label
		}
		if field, ok := fields["status-field"].(string); ok {
			result.StatusField = field
		}
	}
	return result
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
		return errors.New("tools.work-queue.issues must be true, false, or an object with label and optional status-field")
	}
	for key, value := range fields {
		if key != "label" && key != "status-field" {
			return fmt.Errorf("tools.work-queue.issues: unsupported field %q; use label or status-field", key)
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" || len(text) > 256 || strings.Contains(text, "${{") || strings.IndexFunc(text, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return fmt.Errorf("tools.work-queue.issues.%s must be a nonblank literal of at most 256 bytes", key)
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
		"            await main({ core, githubClient: github, context });\n",
	)
}
