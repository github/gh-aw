//go:build !integration

package cli

import (
	"errors"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
)

func TestCompileWorkflowParseErrorClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		err         error
		warningType string
	}{
		{"shared", &workflow.SharedWorkflowError{Path: "shared.md"}, "shared_workflow"},
		{"redirect", &workflow.RedirectOnlyWorkflowError{Path: "old.md", Target: "owner/repo/new.md"}, "redirect_only_workflow"},
		{"parse", errors.New("invalid frontmatter"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compileWorkflowParseError(newCompileWorkflowFileResult("workflow.md"), tt.err, compileWorkflowFileOptions{jsonOutput: true})
			if result.validationResult.Workflow != "workflow.md" || result.workflowData != nil {
				t.Fatalf("unexpected workflow metadata: %+v", result)
			}
			if tt.warningType != "" {
				if !result.success || !result.validationResult.Valid || len(result.validationResult.Errors) != 0 {
					t.Fatalf("skipped workflow was not valid: %+v", result)
				}
				if len(result.validationResult.Warnings) != 1 || result.validationResult.Warnings[0].Type != tt.warningType {
					t.Fatalf("unexpected skip warning: %+v", result.validationResult.Warnings)
				}
			} else if result.success || result.validationResult.Valid || len(result.validationResult.Errors) != 1 || result.validationResult.Errors[0].Type != "parse_error" || result.validationResult.Errors[0].Message != tt.err.Error() {
				t.Fatalf("unexpected parse failure: %+v", result)
			}
		})
	}
}

func TestExtractSafeOutputLabels_IncludesLabelCommand(t *testing.T) {
	t.Parallel()
	data := &workflow.WorkflowData{
		SafeOutputs: &workflow.SafeOutputsConfig{
			CreateIssues: &workflow.CreateIssuesConfig{
				Labels:                        []string{"bug"},
				SafeOutputAllowedLabelsConfig: workflow.SafeOutputAllowedLabelsConfig{AllowedLabels: []string{"triage"}},
			},
			AddLabels: &workflow.AddLabelsConfig{
				SafeOutputAllowBlockConfig: workflow.SafeOutputAllowBlockConfig{
					Allowed: []string{"automation"},
				},
			},
		},
		LabelCommand: []string{"deploy"},
	}

	got := extractSafeOutputLabels(data)
	want := []string{"bug", "triage", "automation", "deploy"}

	if len(got) != len(want) {
		t.Fatalf("expected %d labels, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected labels %v, got %v", want, got)
		}
	}
}

func TestExtractSafeOutputLabels_DeduplicatesAcrossSources(t *testing.T) {
	t.Parallel()
	data := &workflow.WorkflowData{
		SafeOutputs: &workflow.SafeOutputsConfig{
			CreatePullRequests: &workflow.CreatePullRequestsConfig{
				Labels: []string{"deploy"},
			},
		},
		LabelCommand: []string{"deploy"},
	}

	got := extractSafeOutputLabels(data)
	if len(got) != 1 || got[0] != "deploy" {
		t.Fatalf("expected deduplicated labels [deploy], got %v", got)
	}
}
