//go:build !integration

package workflow

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExperimentalFeatureNoticeClassification(t *testing.T) {
	t.Parallel()
	for _, batch := range []bool{false, true} {
		compiler := NewCompiler()
		compiler.SetBatchMode(batch)
		data := &WorkflowData{
			Features:    map[string]any{"gh-aw-detection": true},
			LSP:         map[string]LSPServerConfig{"typescript": {}},
			SafeOutputs: &SafeOutputsConfig{Steer: true},
		}
		var output bytes.Buffer
		compiler.emitExperimentalFeatureWarningsTo(data, &output)
		assert.Equal(t, 2, compiler.GetWarningCount())
		assert.Equal(t, 2, compiler.GetExperimentalWarningCount())
		if batch {
			assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()["Using experimental feature: lsp"])
			assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()["Using experimental feature: safe-outputs steer"])
		} else {
			assert.Contains(t, output.String(), "Using experimental feature: lsp")
			assert.Contains(t, output.String(), "Using experimental feature: safe-outputs steer")
		}
	}
}

func TestExperimentalWarningAccounting(t *testing.T) {
	t.Parallel()
	for _, batch := range []bool{false, true} {
		compiler := NewCompiler()
		compiler.SetBatchMode(batch)
		compiler.IncrementWarningCount()
		assert.Zero(t, compiler.GetExperimentalWarningCount())
		compiler.IncrementExperimentalWarningCount()
		assert.Equal(t, 2, compiler.GetWarningCount())
		assert.Equal(t, 1, compiler.GetExperimentalWarningCount())
		compiler.ResetWarningCount()
		assert.Zero(t, compiler.GetWarningCount())
		assert.Zero(t, compiler.GetExperimentalWarningCount())
	}
}

func TestSkipIfDecisionExperimentalNoticeClassification(t *testing.T) {
	t.Parallel()
	for _, batch := range []bool{false, true} {
		for _, data := range []*WorkflowData{
			{},
			{SkipIfDecision: []string{}},
			{SkipIfDecision: []string{"Done?"}, IsDetectionRun: true},
			{SkipIfDecision: []string{"Done?"}, IsEvalsRun: true},
		} {
			compiler := NewCompiler()
			compiler.SetBatchMode(batch)
			var output bytes.Buffer
			compiler.emitExperimentalFeatureWarningsTo(data, &output)
			assert.Empty(t, output.String())
			assert.Zero(t, compiler.GetExperimentalWarningCount())
			assert.Empty(t, compiler.GetExperimentalFeatureUsage())
		}
		compiler := NewCompiler()
		compiler.SetBatchMode(batch)
		var output bytes.Buffer
		compiler.emitExperimentalFeatureWarningsTo(&WorkflowData{SkipIfDecision: []string{"Done?"}}, &output)
		assert.Equal(t, 1, compiler.GetWarningCount())
		assert.Equal(t, 1, compiler.GetExperimentalWarningCount())
		const warning = "Using experimental feature: skip-if-decision"
		if batch {
			assert.Empty(t, output.String())
			assert.Equal(t, map[string]int{warning: 1}, compiler.GetExperimentalFeatureUsage())
		} else {
			assert.Contains(t, output.String(), warning)
			assert.Empty(t, compiler.GetExperimentalFeatureUsage())
		}
	}
}

func TestDurableExperimentalNoticeClassification(t *testing.T) {
	t.Parallel()
	for _, batch := range []bool{false, true} {
		for _, engine := range []string{"copilot", "claude", "codex", "gemini", "agy", "pi", "custom"} {
			compiler := NewCompiler()
			compiler.SetBatchMode(batch)
			data := &WorkflowData{EngineConfig: &EngineConfig{ID: engine}}
			var output bytes.Buffer
			compiler.emitExperimentalFeatureWarningsTo(data, &output)
			assert.Empty(t, output.String())
			assert.Zero(t, compiler.GetExperimentalWarningCount())
			assert.Empty(t, compiler.GetExperimentalFeatureUsage())

			data.EngineConfig.Durable = &DurableConfig{RetentionDays: 7}
			compiler.emitExperimentalFeatureWarningsTo(data, &output)
			assert.Equal(t, 1, compiler.GetWarningCount(), "engine: %s, batch: %t", engine, batch)
			assert.Equal(t, 1, compiler.GetExperimentalWarningCount())
			const warning = "Using experimental feature: engine.durable"
			if batch {
				assert.Empty(t, output.String())
				assert.Equal(t, map[string]int{warning: 1}, compiler.GetExperimentalFeatureUsage())
			} else {
				assert.Contains(t, output.String(), warning)
				assert.Empty(t, compiler.GetExperimentalFeatureUsage())
			}
		}
	}
}

func TestStableSafeOutputsNoExperimentalWarning(t *testing.T) {
	t.Parallel()
	for _, batch := range []bool{false, true} {
		compiler := NewCompiler()
		compiler.SetBatchMode(batch)
		data := &WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				DispatchRepository: &DispatchRepositoryConfig{},
				ApproveWorkflowRun: &ApproveWorkflowRunConfig{},
				ReplaceLabel:       &ReplaceLabelConfig{},
				UploadCodeCoverage: &UploadCodeCoverageConfig{},
			},
		}
		var output bytes.Buffer
		compiler.emitExperimentalFeatureWarningsTo(data, &output)
		assert.Empty(t, output.String(), "batch mode: %t", batch)
		assert.Zero(t, compiler.GetWarningCount(), "batch mode: %t", batch)
		assert.Zero(t, compiler.GetExperimentalWarningCount(), "batch mode: %t", batch)
		assert.Empty(t, compiler.GetExperimentalFeatureUsage(), "batch mode: %t", batch)

		data.SafeOutputs.Steer = true
		compiler.emitExperimentalFeatureWarningsTo(data, &output)
		assert.Equal(t, 1, compiler.GetWarningCount(), "batch mode: %t", batch)
		assert.Equal(t, 1, compiler.GetExperimentalWarningCount(), "batch mode: %t", batch)
		const warning = "Using experimental feature: safe-outputs steer"
		if batch {
			assert.Empty(t, output.String())
			assert.Equal(t, map[string]int{warning: 1}, compiler.GetExperimentalFeatureUsage())
		} else {
			assert.Contains(t, output.String(), warning)
			assert.Empty(t, compiler.GetExperimentalFeatureUsage())
		}
	}
}

func TestMergePullRequestExperimentalNoticeClassification(t *testing.T) {
	t.Parallel()
	for _, batch := range []bool{false, true} {
		compiler := NewCompiler()
		compiler.SetBatchMode(batch)
		data := &WorkflowData{
			SafeOutputs: &SafeOutputsConfig{MergePullRequest: &MergePullRequestConfig{}},
		}
		var output bytes.Buffer
		compiler.emitExperimentalFeatureWarningsTo(data, &output)
		assert.Equal(t, 1, compiler.GetWarningCount(), "batch mode: %t", batch)
		assert.Equal(t, 1, compiler.GetExperimentalWarningCount(), "batch mode: %t", batch)
		const warning = "Using experimental feature: merge-pull-request"
		if batch {
			assert.Empty(t, output.String())
			assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()[warning])
		} else {
			assert.Contains(t, output.String(), warning)
			assert.Empty(t, compiler.GetExperimentalFeatureUsage())
		}
	}
}

func TestIssueIntentExperimentalNoticeClassification(t *testing.T) {
	t.Parallel()
	enabled, disabled := true, false
	tests := []struct {
		name        string
		safeOutputs func() *SafeOutputsConfig
		wantWarning bool
	}{
		{
			name: "enabled on close-issue",
			safeOutputs: func() *SafeOutputsConfig {
				so := &SafeOutputsConfig{CloseIssues: &CloseIssuesConfig{}, AddLabels: &AddLabelsConfig{}}
				so.CloseIssues.IssueIntent = &enabled
				return so
			},
			wantWarning: true,
		},
		{
			name: "enabled on add-labels",
			safeOutputs: func() *SafeOutputsConfig {
				so := &SafeOutputsConfig{CloseIssues: &CloseIssuesConfig{}, AddLabels: &AddLabelsConfig{}}
				so.AddLabels.IssueIntent = &enabled
				return so
			},
			wantWarning: true,
		},
		{
			name: "enabled on assign-to-agent",
			safeOutputs: func() *SafeOutputsConfig {
				so := &SafeOutputsConfig{AssignToAgent: &AssignToAgentConfig{}}
				so.AssignToAgent.IssueIntent = &enabled
				return so
			},
			wantWarning: true,
		},
		{
			name: "enabled on assign-to-user",
			safeOutputs: func() *SafeOutputsConfig {
				so := &SafeOutputsConfig{AssignToUser: &AssignToUserConfig{}}
				so.AssignToUser.IssueIntent = &enabled
				return so
			},
			wantWarning: true,
		},
		{
			name: "enabled on set-issue-type",
			safeOutputs: func() *SafeOutputsConfig {
				so := &SafeOutputsConfig{SetIssueType: &SetIssueTypeConfig{}}
				so.SetIssueType.IssueIntent = &enabled
				return so
			},
			wantWarning: true,
		},
		{
			name: "enabled on set-issue-field",
			safeOutputs: func() *SafeOutputsConfig {
				so := &SafeOutputsConfig{SetIssueField: &SetIssueFieldConfig{}}
				so.SetIssueField.IssueIntent = &enabled
				return so
			},
			wantWarning: true,
		},
		{
			name: "enabled on multiple outputs counts once",
			safeOutputs: func() *SafeOutputsConfig {
				so := &SafeOutputsConfig{AddLabels: &AddLabelsConfig{}, AssignToUser: &AssignToUserConfig{}}
				so.AddLabels.IssueIntent = &enabled
				so.AssignToUser.IssueIntent = &enabled
				return so
			},
			wantWarning: true,
		},
		{
			name: "explicitly disabled on all outputs",
			safeOutputs: func() *SafeOutputsConfig {
				so := &SafeOutputsConfig{
					CloseIssues:   &CloseIssuesConfig{},
					AddLabels:     &AddLabelsConfig{},
					AssignToAgent: &AssignToAgentConfig{},
					AssignToUser:  &AssignToUserConfig{},
					SetIssueType:  &SetIssueTypeConfig{},
					SetIssueField: &SetIssueFieldConfig{},
				}
				so.CloseIssues.IssueIntent = &disabled
				so.AddLabels.IssueIntent = &disabled
				so.AssignToAgent.IssueIntent = &disabled
				so.AssignToUser.IssueIntent = &disabled
				so.SetIssueType.IssueIntent = &disabled
				so.SetIssueField.IssueIntent = &disabled
				return so
			},
		},
		{
			name: "omitted",
			safeOutputs: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{CloseIssues: &CloseIssuesConfig{}, SetIssueField: &SetIssueFieldConfig{}}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiler := NewCompiler()
			data := &WorkflowData{SafeOutputs: tt.safeOutputs()}
			var output bytes.Buffer
			compiler.emitExperimentalFeatureWarningsTo(data, &output)
			if tt.wantWarning {
				assert.Contains(t, output.String(), "Using experimental feature: issue-intent", "issue-intent: true should emit the experimental warning")
				assert.Equal(t, 1, compiler.GetExperimentalWarningCount(), "exactly one experimental warning should be counted")
			} else {
				assert.NotContains(t, output.String(), "issue-intent", "issue-intent warning should not be emitted")
				assert.Zero(t, compiler.GetExperimentalWarningCount(), "no experimental warnings should be counted")
			}
		})
	}
}
