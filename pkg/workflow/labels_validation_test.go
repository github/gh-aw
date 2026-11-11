//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateLabels(t *testing.T) {
	tests := []struct {
		name      string
		labels    []string
		shouldErr bool
		errorMsg  string
	}{
		{
			name:      "valid labels",
			labels:    []string{"automation", "security", "ci"},
			shouldErr: false,
		},
		{
			name:      "no labels",
			labels:    []string{},
			shouldErr: false,
		},
		{
			name:      "single valid label",
			labels:    []string{"docs"},
			shouldErr: false,
		},
		{
			name:      "empty label",
			labels:    []string{"automation", "", "security"},
			shouldErr: true,
			errorMsg:  "labels[1] is empty",
		},
		{
			name:      "label with leading whitespace",
			labels:    []string{"automation", " security", "ci"},
			shouldErr: true,
			errorMsg:  "labels[1] has leading or trailing whitespace",
		},
		{
			name:      "label with trailing whitespace",
			labels:    []string{"automation", "security ", "ci"},
			shouldErr: true,
			errorMsg:  "labels[1] has leading or trailing whitespace",
		},
		{
			name:      "label with both leading and trailing whitespace",
			labels:    []string{" automation "},
			shouldErr: true,
			errorMsg:  "labels[0] has leading or trailing whitespace",
		},
		{
			name:      "whitespace-only label",
			labels:    []string{"automation", "   ", "security"},
			shouldErr: true,
			errorMsg:  "labels[1] has leading or trailing whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				ParsedFrontmatter: &FrontmatterConfig{
					Labels: tt.labels,
				},
			}

			err := validateLabels(workflowData)

			if tt.shouldErr {
				require.Error(t, err, "Expected validation to fail")
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected validation to pass")
			}
		})
	}
}

func TestValidateLabels_NilFrontmatter(t *testing.T) {
	// Test with nil ParsedFrontmatter
	workflowData := &WorkflowData{
		ParsedFrontmatter: nil,
	}

	err := validateLabels(workflowData)
	assert.NoError(t, err, "Should handle nil frontmatter gracefully")
}

func TestValidateLabels_NilWorkflowData(t *testing.T) {
	// Test with nil workflowData
	err := validateLabels(nil)
	assert.NoError(t, err, "Should handle nil workflowData gracefully")
}

func TestCompileRejectsLongLabelNames(t *testing.T) {
	for _, field := range []string{
		"labels: [%q]",
		"safe-outputs:\n  create-issue:\n    labels: [%q]",
	} {
		t.Run(field, func(t *testing.T) {
			workflowPath := filepath.Join(t.TempDir(), "long-label.md")
			content := "---\non: workflow_dispatch\n" +
				fmt.Sprintf(field, strings.Repeat("a", 54)) +
				"\n---\nTest label validation.\n"
			require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0600))

			err := NewCompiler().CompileWorkflow(workflowPath)
			require.ErrorContains(t, err, "maxLength: got 54, want 50")
			_, err = os.Stat(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}
