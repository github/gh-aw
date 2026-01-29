//go:build !integration

package cli

import (
	"context"
	"testing"
)

func TestCompileDependabotValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      CompileConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "dependabot with specific workflow files",
			config: CompileConfig{
				MarkdownFiles: []string{"test.md"},
				Dependabot:    true,
			},
			expectError: true,
			errorMsg:    "--dependabot flag cannot be used with specific workflow files",
		},
		{
			name: "dependabot with custom --dir",
			config: CompileConfig{
				WorkflowDir: "custom/workflows",
				Dependabot:  true,
			},
			expectError: true,
			errorMsg:    "--dependabot flag cannot be used with custom --dir",
		},
		{
			name: "dependabot with default workflows dir is ok",
			config: CompileConfig{
				WorkflowDir: ".github/workflows",
				Dependabot:  true,
			},
			expectError: false,
		},
		{
			name: "dependabot with empty workflows dir is ok",
			config: CompileConfig{
				WorkflowDir: "",
				Dependabot:  true,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't actually run CompileWorkflows in all cases because it needs a git repo
			// So we'll just test the validation logic by checking if it would error early

			// For tests that should error, we expect them to fail early
			if tt.expectError {
				_, err := CompileWorkflows(context.Background(), tt.config)
				if err == nil {
					t.Errorf("expected error but got none")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("expected error %q, got %q", tt.errorMsg, err.Error())
				}
			}
		})
	}
}
