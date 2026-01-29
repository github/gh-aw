//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestTimeoutMinutesDeprecation(t *testing.T) {
	tests := []struct {
		name              string
		frontmatter       map[string]any
		expectedTimeout   string
		expectDeprecation bool
	}{
		{
			name: "timeout-minutes (new format)",
			frontmatter: map[string]any{
				"timeout-minutes": 15,
			},
			expectedTimeout:   "timeout-minutes: 15",
			expectDeprecation: false,
		},
		{
			name: "timeout_minutes (deprecated format)",
			frontmatter: map[string]any{
				"timeout_minutes": 20,
			},
			expectedTimeout:   "timeout_minutes: 20",
			expectDeprecation: true,
		},
		{
			name: "timeout-minutes takes precedence",
			frontmatter: map[string]any{
				"timeout-minutes": 15,
				"timeout_minutes": 20,
			},
			expectedTimeout:   "timeout-minutes: 15",
			expectDeprecation: false,
		},
		{
			name:              "no timeout specified",
			frontmatter:       map[string]any{},
			expectedTimeout:   "",
			expectDeprecation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}

			// Test timeout-minutes extraction
			timeoutHyphen := c.extractTopLevelYAMLSection(tt.frontmatter, "timeout-minutes")
			timeoutUnderscore := c.extractTopLevelYAMLSection(tt.frontmatter, "timeout_minutes")

			// Verify the extraction logic matches our expected behavior
			var actualTimeout string
			if timeoutHyphen != "" {
				actualTimeout = "timeout-minutes: " + strings.TrimPrefix(timeoutHyphen, "timeout-minutes: ")
			} else if timeoutUnderscore != "" {
				actualTimeout = "timeout_minutes: " + strings.TrimPrefix(timeoutUnderscore, "timeout_minutes: ")
			}

			if actualTimeout != tt.expectedTimeout {
				t.Errorf("Expected timeout %q, got %q", tt.expectedTimeout, actualTimeout)
			}
		})
	}
}
