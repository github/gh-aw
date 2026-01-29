//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestValidateSafeOutputsTarget(t *testing.T) {
	tests := []struct {
		name    string
		config  *SafeOutputsConfig
		wantErr bool
		errText string
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "empty config",
			config:  &SafeOutputsConfig{},
			wantErr: false,
		},
		{
			name: "valid triggering target",
			config: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "triggering",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid empty target (defaults to triggering)",
			config: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid wildcard target",
			config: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "*",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid numeric target",
			config: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "123",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid GitHub expression",
			config: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "${{ github.event.issue.number }}",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid target - event",
			config: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "event",
						},
					},
				},
			},
			wantErr: true,
			errText: "invalid target value for update-issue: \"event\"",
		},
		{
			name: "invalid target - negative number",
			config: &SafeOutputsConfig{
				CloseIssues: &CloseEntityConfig{
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						Target: "-1",
					},
				},
			},
			wantErr: true,
			errText: "invalid target value for close-issue: \"-1\"",
		},
		{
			name: "invalid target - zero",
			config: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{},
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						Target: "0",
					},
				},
			},
			wantErr: true,
			errText: "invalid target value for add-labels: \"0\"",
		},
		{
			name: "invalid target - leading zeros",
			config: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{},
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						Target: "0123",
					},
				},
			},
			wantErr: true,
			errText: "invalid target value for add-labels: \"0123\"",
		},
		{
			name: "invalid target - random string",
			config: &SafeOutputsConfig{
				UpdateDiscussions: &UpdateDiscussionsConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "random-string",
						},
					},
				},
			},
			wantErr: true,
			errText: "invalid target value for update-discussion: \"random-string\"",
		},
		{
			name: "multiple configs with valid targets",
			config: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "triggering",
						},
					},
				},
				CloseIssues: &CloseEntityConfig{
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						Target: "*",
					},
				},
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{},
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						Target: "456",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "multiple configs with one invalid target",
			config: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "triggering",
						},
					},
				},
				CloseIssues: &CloseEntityConfig{
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						Target: "invalid",
					},
				},
			},
			wantErr: true,
			errText: "invalid target value for close-issue: \"invalid\"",
		},
		{
			name: "valid target for update-pull-request",
			config: &SafeOutputsConfig{
				UpdatePullRequests: &UpdatePullRequestsConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						SafeOutputTargetConfig: SafeOutputTargetConfig{
							Target: "${{ github.event.pull_request.number }}",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid target for close-discussion",
			config: &SafeOutputsConfig{
				CloseDiscussions: &CloseEntityConfig{
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						Target: "789",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid target for add-reviewer",
			config: &SafeOutputsConfig{
				AddReviewer: &AddReviewerConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{},
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						Target: "*",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid target for assign-milestone",
			config: &SafeOutputsConfig{
				AssignMilestone: &AssignMilestoneConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{},
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						Target: "triggering",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSafeOutputsTarget(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSafeOutputsTarget() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errText != "" {
				if !strings.Contains(err.Error(), tt.errText) {
					t.Errorf("validateSafeOutputsTarget() error = %v, should contain %q", err, tt.errText)
				}
			}
		})
	}
}

func TestValidateTargetValue(t *testing.T) {
	tests := []struct {
		name       string
		configName string
		target     string
		wantErr    bool
		errText    string
	}{
		{
			name:       "empty target",
			configName: "test-config",
			target:     "",
			wantErr:    false,
		},
		{
			name:       "triggering",
			configName: "test-config",
			target:     "triggering",
			wantErr:    false,
		},
		{
			name:       "wildcard",
			configName: "test-config",
			target:     "*",
			wantErr:    false,
		},
		{
			name:       "positive integer",
			configName: "test-config",
			target:     "42",
			wantErr:    false,
		},
		{
			name:       "large number",
			configName: "test-config",
			target:     "999999",
			wantErr:    false,
		},
		{
			name:       "GitHub expression",
			configName: "test-config",
			target:     "${{ github.event.issue.number }}",
			wantErr:    false,
		},
		{
			name:       "complex GitHub expression",
			configName: "test-config",
			target:     "${{ github.event.pull_request.number || github.event.issue.number }}",
			wantErr:    false,
		},
		{
			name:       "invalid - event",
			configName: "update-issue",
			target:     "event",
			wantErr:    true,
			errText:    "invalid target value for update-issue: \"event\"",
		},
		{
			name:       "invalid - zero",
			configName: "test-config",
			target:     "0",
			wantErr:    true,
			errText:    "invalid target value",
		},
		{
			name:       "invalid - negative",
			configName: "test-config",
			target:     "-5",
			wantErr:    true,
			errText:    "invalid target value",
		},
		{
			name:       "invalid - float",
			configName: "test-config",
			target:     "3.14",
			wantErr:    true,
			errText:    "invalid target value",
		},
		{
			name:       "invalid - leading zeros",
			configName: "test-config",
			target:     "007",
			wantErr:    true,
			errText:    "invalid target value",
		},
		{
			name:       "invalid - random string",
			configName: "test-config",
			target:     "something-else",
			wantErr:    true,
			errText:    "invalid target value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTargetValue(tt.configName, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTargetValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errText != "" {
				if !strings.Contains(err.Error(), tt.errText) {
					t.Errorf("validateTargetValue() error = %v, should contain %q", err, tt.errText)
				}
			}
		})
	}
}

func TestIsGitHubExpression(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{
			name: "simple expression",
			s:    "${{ github.event.issue.number }}",
			want: true,
		},
		{
			name: "complex expression",
			s:    "${{ github.event.pull_request.number || github.event.issue.number }}",
			want: true,
		},
		{
			name: "nested expression",
			s:    "${{ fromJSON(github.event.issue.body).number }}",
			want: true,
		},
		{
			name: "not an expression",
			s:    "event",
			want: false,
		},
		{
			name: "incomplete expression - missing opening",
			s:    "incomplete }}",
			want: false,
		},
		{
			name: "incomplete expression - missing closing",
			s:    "${{ incomplete",
			want: false,
		},
		{
			name: "empty expression",
			s:    "${{}}",
			want: false,
		},
		{
			name: "empty string",
			s:    "",
			want: false,
		},
		{
			name: "wrong order - closing before opening",
			s:    "}} some ${{ text",
			want: false,
		},
		{
			name: "text with embedded markers but invalid order",
			s:    "text }} more ${{ stuff",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGitHubExpression(tt.s)
			if got != tt.want {
				t.Errorf("isGitHubExpression() = %v, want %v", got, tt.want)
			}
		})
	}
}
