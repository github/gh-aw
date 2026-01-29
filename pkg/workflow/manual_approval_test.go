//go:build !integration

package workflow

import (
	"testing"
)

func TestExtractManualApprovalFromOn(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		want        string
		wantErr     bool
	}{
		{
			name:        "simple string on - no manual approval",
			frontmatter: map[string]any{"on": "push"},
			want:        "",
			wantErr:     false,
		},
		{
			name:        "no on section",
			frontmatter: map[string]any{},
			want:        "",
			wantErr:     false,
		},
		{
			name: "manual-approval in on section",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
					"manual-approval":   "production",
				},
			},
			want:    "production",
			wantErr: false,
		},
		{
			name: "manual-approval with different environment",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"types": []string{"opened"},
					},
					"manual-approval": "staging",
				},
			},
			want:    "staging",
			wantErr: false,
		},
		{
			name: "invalid manual-approval type",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
					"manual-approval":   123, // not a string
				},
			},
			want:    "",
			wantErr: true,
		},
		{
			name:        "invalid on section type",
			frontmatter: map[string]any{"on": 123},
			want:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			got, err := c.extractManualApprovalFromOn(tt.frontmatter)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractManualApprovalFromOn() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractManualApprovalFromOn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProcessManualApprovalConfiguration(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  map[string]any
		wantApproval string
		wantErr      bool
	}{
		{
			name: "valid manual-approval",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
					"manual-approval":   "production",
				},
			},
			wantApproval: "production",
			wantErr:      false,
		},
		{
			name: "no manual-approval",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
				},
			},
			wantApproval: "",
			wantErr:      false,
		},
		{
			name: "invalid manual-approval",
			frontmatter: map[string]any{
				"on": map[string]any{
					"manual-approval": 123,
				},
			},
			wantApproval: "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			workflowData := &WorkflowData{}
			err := c.processManualApprovalConfiguration(tt.frontmatter, workflowData)

			if (err != nil) != tt.wantErr {
				t.Errorf("processManualApprovalConfiguration() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if workflowData.ManualApproval != tt.wantApproval {
				t.Errorf("workflowData.ManualApproval = %v, want %v", workflowData.ManualApproval, tt.wantApproval)
			}
		})
	}
}
