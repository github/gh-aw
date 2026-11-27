//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	yamlv3 "go.yaml.in/yaml/v3"
)

func TestGenerateMaintenanceWorkflow_LabelTriggers_Disabled(t *testing.T) {
	workflowDataList := []*WorkflowData{
		{
			Name: "test-workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{Expires: 48},
			},
		},
	}

	tmpDir := t.TempDir()
	falseVal := false
	cfg := &RepoConfig{
		Maintenance: &MaintenanceConfig{LabelTriggers: &falseVal},
	}
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       cfg,
		RepoSlug:         "",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	if err != nil {
		t.Fatalf("Expected maintenance workflow to be generated: %v", err)
	}
	yaml := string(content)

	// Label-event trigger should be absent
	if strings.Contains(yaml, "  issues:\n    types: [labeled]") {
		t.Error("When label_triggers is false the issues labeled trigger should not be present")
	}

	// The pull_request labeled trigger should never be present (removed)
	if strings.Contains(yaml, "  pull_request:\n    types: [labeled]") {
		t.Error("pull_request labeled trigger should never be present (issues-only)")
	}

	// The label_disable_agentic_workflow job should be absent
	if strings.Contains(yaml, "label_disable_agentic_workflow:") {
		t.Error("When label_triggers is false the label_disable_agentic_workflow job should not be present")
	}

	// The label_apply_safe_outputs job should be absent
	if strings.Contains(yaml, "label_apply_safe_outputs:") {
		t.Error("When label_triggers is false the label_apply_safe_outputs job should not be present")
	}
}

func TestGenerateMaintenanceWorkflow_LabelTriggers_Default(t *testing.T) {
	workflowDataList := []*WorkflowData{
		{
			Name: "test-workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{Expires: 48},
			},
		},
	}

	tmpDir := t.TempDir()
	// Default: LabelTriggers is nil (omitted) → treated as false (opt-in semantics) → jobs absent
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       nil,
		RepoSlug:         "",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	if err != nil {
		t.Fatalf("Expected maintenance workflow to be generated: %v", err)
	}
	yaml := string(content)

	// Issues labeled trigger should NOT be present by default (opt-in required)
	if strings.Contains(yaml, "  issues:\n    types: [labeled]") {
		t.Error("By default (no config) the issues labeled trigger should NOT be present — label_triggers must be explicitly enabled")
	}

	// The label_disable_agentic_workflow job should NOT be present by default
	if strings.Contains(yaml, "label_disable_agentic_workflow:") {
		t.Error("By default (no config) the label_disable_agentic_workflow job should NOT be present — label_triggers must be explicitly enabled")
	}

	// The label_apply_safe_outputs job should NOT be present by default
	if strings.Contains(yaml, "label_apply_safe_outputs:") {
		t.Error("By default (no config) the label_apply_safe_outputs job should NOT be present — label_triggers must be explicitly enabled")
	}
}

func TestGenerateMaintenanceWorkflow_LabelTriggers_ExplicitTrue(t *testing.T) {
	workflowDataList := []*WorkflowData{
		{
			Name: "test-workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{Expires: 48},
			},
		},
	}

	tmpDir := t.TempDir()
	trueVal := true
	cfg := &RepoConfig{
		Maintenance: &MaintenanceConfig{LabelTriggers: &trueVal},
	}
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       cfg,
		RepoSlug:         "",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	if err != nil {
		t.Fatalf("Expected maintenance workflow to be generated: %v", err)
	}
	yaml := string(content)

	// Issues labeled trigger should be present when explicitly enabled
	if !strings.Contains(yaml, "  issues:\n    types: [labeled]") {
		t.Error("When label_triggers: true the issues labeled trigger should be present")
	}

	// pull_request labeled trigger should never be present (issues-only by design)
	if strings.Contains(yaml, "  pull_request:\n    types: [labeled]") {
		t.Error("pull_request labeled trigger should never be present (issues-only)")
	}

	// The label_disable_agentic_workflow job should be present when explicitly enabled
	if !strings.Contains(yaml, "label_disable_agentic_workflow:") {
		t.Error("When label_triggers: true the label_disable_agentic_workflow job should be present")
	}

	// The label_apply_safe_outputs job should be present when explicitly enabled
	if !strings.Contains(yaml, "label_apply_safe_outputs:") {
		t.Error("When label_triggers: true the label_apply_safe_outputs job should be present")
	}

	// Verify label_apply_safe_outputs job has an explicit step id and if condition so that
	// the operation step only runs when the permission check passes
	applySafeIdx := strings.Index(yaml, "\n  label_apply_safe_outputs:")
	if applySafeIdx != -1 {
		applySection := yaml[applySafeIdx:min(applySafeIdx+2000, len(yaml))]
		if !strings.Contains(applySection, "id: check_permissions") {
			t.Errorf("label_apply_safe_outputs permission check step should have id: check_permissions in:\n%s", applySection[:min(500, len(applySection))])
		}
		if !strings.Contains(applySection, "steps.check_permissions.outcome == 'success'") {
			t.Errorf("label_apply_safe_outputs operation step should have if: steps.check_permissions.outcome == 'success' in:\n%s", applySection[:min(500, len(applySection))])
		}
	}
}

func TestMaintenanceLabelJobsQuoteActionPins(t *testing.T) {
	actionPin := getCachedActionPinFromResolver("actions/github-script", nil)
	expectedActionRef, _, _ := strings.Cut(actionPin, " # ")
	opts := buildMaintenanceWorkflowYAMLOptions{runsOnValue: "ubuntu-latest"}
	for _, tc := range []struct {
		name    string
		jobName string
		step    string
		build   func(buildMaintenanceWorkflowYAMLOptions, string) (string, error)
	}{
		{
			name:    "disable workflow",
			jobName: "label_disable_agentic_workflow",
			step:    "Disable agentic workflow",
			build:   buildMaintenanceLabelDisableJob,
		},
		{
			name:    "apply safe outputs",
			jobName: "label_apply_safe_outputs",
			step:    "Apply safe outputs from referenced run",
			build:   buildMaintenanceLabelApplySafeOutputsJob,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, err := tc.build(opts, "./actions/setup")
			require.NoError(t, err)
			require.Contains(t, content, "uses: '"+expectedActionRef+"' # ")
			var parsed struct {
				Jobs map[string]struct {
					Steps []struct {
						Name string `yaml:"name"`
						Uses string `yaml:"uses"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yamlv3.Unmarshal([]byte("jobs:\n"+content), &parsed))
			job, ok := parsed.Jobs[tc.jobName]
			require.True(t, ok, "expected generated job %q", tc.jobName)
			for _, step := range job.Steps {
				if step.Name == tc.step {
					require.Equal(t, expectedActionRef, step.Uses)
					return
				}
			}
			t.Fatalf("expected step %q in generated job %q", tc.step, tc.jobName)
		})
	}
}

func TestQuoteYAMLActionPin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pin     string
		wantRef string
	}{
		{"plain", "owner/action@sha", "owner/action@sha"},
		{"version comment", "owner/action@sha # v1", "owner/action@sha"},
		{"apostrophes", "owner/action@ref'with'quotes # v1's label", "owner/action@ref'with'quotes"},
		{"YAML punctuation", "owner/action@ref: [value] # v1", "owner/action@ref: [value]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quoted, err := quoteYAMLActionPin(tc.pin)
			require.NoError(t, err)
			var parsed struct {
				Uses string `yaml:"uses"`
			}
			require.NoError(t, yamlv3.Unmarshal([]byte("uses: "+quoted+"\n"), &parsed))
			require.Equal(t, tc.wantRef, parsed.Uses)
		})
	}
}

func TestQuoteYAMLActionPinRejectsUnsafeStrings(t *testing.T) {
	for _, pin := range []string{
		"owner/action@sha\nrun: injected",
		"owner/action@sha # v1\nrun: injected",
		"owner/action@sha # v1\rrun: injected",
		"owner/action@sha # v1\u0085run: injected",
		"owner/action@sha # v1\u2028run: injected",
		"owner/action@sha # v1\u2029run: injected",
		"owner/action@sha\x00",
		"owner/action@${{ github.token }}",
		"owner/action@sha # ${{ github.token }}",
	} {
		t.Run(pin, func(t *testing.T) {
			quoted, err := quoteYAMLActionPin(pin)
			require.ErrorContains(t, err, "invalid action pin")
			require.Empty(t, quoted)
		})
	}
}

type unsafeActionPinResolver string

func (r unsafeActionPinResolver) ResolveSHA(context.Context, string, string) (string, error) {
	return string(r), nil
}

func TestMaintenanceLabelJobsRejectUnsafeActionPins(t *testing.T) {
	opts := buildMaintenanceWorkflowYAMLOptions{
		runsOnValue: "ubuntu-latest",
		resolver:    unsafeActionPinResolver("sha # v1\n        run: injected"),
	}
	for _, build := range []func(buildMaintenanceWorkflowYAMLOptions, string) (string, error){
		buildMaintenanceLabelDisableJob,
		buildMaintenanceLabelApplySafeOutputsJob,
		buildMaintenanceLabelTriggeredJobs,
	} {
		content, err := build(opts, "./actions/setup")
		require.ErrorContains(t, err, "invalid action pin")
		require.Empty(t, content)
	}
	content, err := buildMaintenanceWorkflowYAML(context.Background(), opts)
	require.ErrorContains(t, err, "invalid action pin")
	require.Empty(t, content)
}

func TestMaintenanceWorkflowRejectsUnsafeActionPinsWithoutLabelTriggers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *MaintenanceConfig
	}{
		{name: "default jobs"},
		{
			name: "permission check without close expired jobs",
			config: &MaintenanceConfig{DisabledJobs: []string{
				"close-expired-entities",
			}},
		},
		{
			name: "label jobs individually disabled",
			config: &MaintenanceConfig{DisabledJobs: []string{
				"label_disable_agentic_workflow",
				"label_apply_safe_outputs",
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, pin := range []string{
				"sha\n        run: injected",
				"sha\rrun: injected",
				"sha\u0085run: injected",
				"sha\u2028run: injected",
				"sha\u2029run: injected",
				"sha\x00",
				"${{ github.token }}",
			} {
				opts := buildMaintenanceWorkflowYAMLOptions{
					actionMode:          ActionModeDev,
					runsOnValue:         "ubuntu-latest",
					resolver:            unsafeActionPinResolver(pin),
					disableLabelTrigger: tc.name != "label jobs individually disabled",
					maintenanceConfig:   tc.config,
				}
				content, err := buildMaintenanceWorkflowYAML(context.Background(), opts)
				require.ErrorContains(t, err, "invalid action pin")
				require.Empty(t, content)
			}
		})
	}
}

func TestGenerateMaintenanceWorkflow_DefaultRejectsUnsafeActionPin(t *testing.T) {
	dir := t.TempDir()
	cache := NewActionCache(dir)
	_, version, _ := strings.Cut(getActionPin("actions/github-script"), " # ")
	cache.Set("actions/github-script", version, "sha\n        run: injected")
	_, nodeVersion, _ := strings.Cut(getActionPin("actions/setup-node"), " # ")
	cache.Set("actions/setup-node", nodeVersion, strings.Repeat("a", 40))
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{{
			Name:           "test",
			ActionResolver: NewActionResolver(cache),
			SafeOutputs:    &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{Expires: 48}},
		}},
		WorkflowDir: dir,
		Version:     "dev",
		ActionMode:  ActionModeDev,
	})
	require.ErrorContains(t, err, "invalid action pin")
	_, err = os.Stat(filepath.Join(dir, "agentics-maintenance.yml"))
	require.True(t, os.IsNotExist(err))
}

type maintenanceActionPinsResolver map[string]string

func (r maintenanceActionPinsResolver) ResolveSHA(_ context.Context, repo, _ string) (string, error) {
	return r[repo], nil
}

func TestMaintenanceWorkflowValidatesOnlyResolvedActionPins(t *testing.T) {
	for _, tc := range []struct {
		repo string
		mode ActionMode
	}{
		{"actions/github-script", ActionModeDev},
		{"actions/setup-node", ActionModeDev},
		{"github/gh-aw/actions/setup", ActionModeRelease},
		{"github/gh-aw-actions/setup-cli", ActionModeRelease},
	} {
		t.Run(tc.repo, func(t *testing.T) {
			content, err := buildMaintenanceWorkflowYAML(context.Background(), buildMaintenanceWorkflowYAMLOptions{
				actionMode:          tc.mode,
				version:             "v1.0.0",
				runsOnValue:         "ubuntu-latest",
				disableLabelTrigger: true,
				resolver:            maintenanceActionPinsResolver{tc.repo: "sha\n        run: injected"},
			})
			require.ErrorContains(t, err, "maintenance action "+tc.repo+": invalid action pin")
			require.Empty(t, content)
		})
	}
	opts := buildMaintenanceWorkflowYAMLOptions{
		actionMode:          ActionModeRelease,
		version:             "dev",
		runsOnValue:         "ubuntu-latest",
		disableLabelTrigger: true,
	}
	expected, err := buildMaintenanceWorkflowYAML(context.Background(), opts)
	require.NoError(t, err)
	// setup-node is used only by dev-mode jobs, so this pin must not be resolved.
	opts.resolver = maintenanceActionPinsResolver{"actions/setup-node": "sha\n        run: injected"}
	content, err := buildMaintenanceWorkflowYAML(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, expected, content)
}

func TestGenerateMaintenanceWorkflow_LabelTriggers_ActionMappings(t *testing.T) {
	pin := getActionPin("actions/github-script")
	actionRef, version, _ := strings.Cut(pin, " # ")
	_, sha, _ := strings.Cut(actionRef, "@")
	for _, tc := range []struct {
		name     string
		mappings map[string]string
		expected string
	}{
		{
			name:     "prefix",
			expected: "'mirror/actions-github-script@" + sha + "' # " + version,
		},
		{
			name:     "exact mapping takes precedence over prefix",
			mappings: map[string]string{"actions/github-script@" + version: "internal/github-script@" + sha},
			expected: "'internal/github-script@" + sha + "'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			enabled := true
			require.NoError(t, GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
				WorkflowDataList: []*WorkflowData{{
					Name:        "test",
					SafeOutputs: &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{Expires: 48}},
				}},
				WorkflowDir: dir,
				Version:     "v1.0.0",
				ActionMode:  ActionModeDev,
				RepoConfig: &RepoConfig{
					Maintenance:       &MaintenanceConfig{LabelTriggers: &enabled},
					ActionPins:        tc.mappings,
					ActionPinPrefixes: map[string]string{"actions/": "mirror/actions-"},
				},
			}))
			content, err := os.ReadFile(filepath.Join(dir, "agentics-maintenance.yml"))
			require.NoError(t, err)
			jobs := parseMaintenanceJobs(t, string(content))
			for _, jobName := range []string{"label_disable_agentic_workflow", "label_apply_safe_outputs"} {
				require.Contains(t, jobs[jobName], "uses: "+tc.expected)
				require.NotContains(t, jobs[jobName], "uses: 'actions/github-script@")
			}
		})
	}
}

func TestGenerateMaintenanceWorkflow_DisabledJobs(t *testing.T) {
	workflowDataList := []*WorkflowData{
		{
			Name: "test-workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{Expires: 48},
			},
		},
	}

	tmpDir := t.TempDir()
	trueVal := true
	cfg := &RepoConfig{
		Maintenance: &MaintenanceConfig{
			LabelTriggers: &trueVal,
			DisabledJobs: []string{
				"close-expired-entities",
				"apply_safe_outputs",
				"label_disable_agentic_workflow",
				"label_apply_safe_outputs",
			},
		},
	}
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       cfg,
		RepoSlug:         "",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	if err != nil {
		t.Fatalf("Expected maintenance workflow to be generated: %v", err)
	}
	yaml := string(content)

	if strings.Contains(yaml, "close-expired-discussions:") {
		t.Error("close-expired-discussions job should be omitted when close-expired-entities is disabled in aw.json")
	}
	if strings.Contains(yaml, "close-expired-issues:") {
		t.Error("close-expired-issues job should be omitted when close-expired-entities is disabled in aw.json")
	}
	if strings.Contains(yaml, "close-expired-pull-requests:") {
		t.Error("close-expired-pull-requests job should be omitted when close-expired-entities is disabled in aw.json")
	}
	if strings.Contains(yaml, "apply_safe_outputs:") {
		t.Error("apply_safe_outputs job should be omitted when disabled in aw.json")
	}
	if strings.Contains(yaml, "label_disable_agentic_workflow:") {
		t.Error("label_disable_agentic_workflow job should be omitted when disabled in aw.json")
	}
	if strings.Contains(yaml, "label_apply_safe_outputs:") {
		t.Error("label_apply_safe_outputs job should be omitted when disabled in aw.json")
	}
	if strings.Contains(yaml, "  issues:\n    types: [labeled]") {
		t.Error("issues:labeled trigger should be omitted when all label-triggered jobs are disabled")
	}

	var workflowDoc maintenanceWorkflowDocument
	require.NoError(t, yamlv3.Unmarshal(content, &workflowDoc), "generated maintenance workflow should be valid YAML")
	require.Equal(
		t,
		"${{ inputs.run_url }}",
		workflowDoc.On.WorkflowCall.Outputs.AppliedRunURL.Value,
		"workflow_call applied_run_url should fall back to inputs.run_url",
	)
	require.Contains(t, yaml, "workflow_call falls back to inputs.run_url when apply_safe_outputs is disabled; other triggers leave this empty", "generated output description should document the fallback scope")
}

func TestGenerateMaintenanceWorkflow_DisabledJobs_PartialLabelTrigger(t *testing.T) {
	workflowDataList := []*WorkflowData{
		{
			Name: "test-workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{Expires: 48},
			},
		},
	}

	tmpDir := t.TempDir()
	trueVal := true
	cfg := &RepoConfig{
		Maintenance: &MaintenanceConfig{
			LabelTriggers: &trueVal,
			DisabledJobs:  []string{"label_disable_agentic_workflow"},
		},
	}
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
		ActionTag:        "",
		RepoConfig:       cfg,
		RepoSlug:         "",
	})
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	require.NoError(t, err)
	yaml := string(content)

	require.Contains(t, yaml, "  issues:\n    types: [labeled]", "issues:labeled trigger should remain when any label-triggered job is still enabled")
	require.NotContains(t, yaml, "label_disable_agentic_workflow:", "disabled label job should be omitted")
	require.Contains(t, yaml, "label_apply_safe_outputs:", "remaining label-triggered job should still be emitted")
}
