//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindDynamicCheckoutSecretsExpressions(t *testing.T) {
	tests := []struct {
		name        string
		expressions []string
		wantLen     int
	}{
		{
			name:        "no expressions",
			expressions: nil,
			wantLen:     0,
		},
		{
			name:        "safe expression referencing inputs only",
			expressions: []string{"${{ fromJSON(inputs.checkouts) }}"},
			wantLen:     0,
		},
		{
			name:        "safe expression referencing env",
			expressions: []string{"${{ fromJSON(env.CHECKOUTS_JSON) }}"},
			wantLen:     0,
		},
		{
			name:        "direct secrets reference",
			expressions: []string{"${{ fromJSON(format('[{\"repository\":\"{0}\",\"github-token\":\"{1}\"}]', inputs.repo, secrets.MY_TOKEN)) }}"},
			wantLen:     1,
		},
		{
			name:        "quoted string literal mentioning secrets is not flagged",
			expressions: []string{"${{ fromJSON('secrets.NOT_REAL') }}"},
			wantLen:     0,
		},
		{
			name:        "quoted string literal mentioning bracket-style secrets is not flagged",
			expressions: []string{"${{ fromJSON('secrets[\"NOT_REAL\"]') }}"},
			wantLen:     0,
		},
		{
			name: "multiple expressions — only the dangerous one flagged",
			expressions: []string{
				"${{ fromJSON(inputs.checkouts) }}",
				"${{ fromJSON(format('[{0}]', secrets.OTHER_TOKEN)) }}",
			},
			wantLen: 1,
		},
		{
			name:        "bracket-indexed secrets reference",
			expressions: []string{"${{ fromJSON(format('[{0}]', secrets['MY_TOKEN'])) }}"},
			wantLen:     1,
		},
		{
			name:        "bracket-indexed secrets reference with double quotes",
			expressions: []string{"${{ fromJSON(format('[{0}]', secrets[\"MY_TOKEN\"])) }}"},
			wantLen:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findDynamicCheckoutSecretsExpressions(tt.expressions)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

func TestValidateDynamicCheckoutSecretsUsage(t *testing.T) {
	tests := []struct {
		name           string
		expressions    []string
		rawFrontmatter map[string]any
		strictMode     bool
		wantError      bool
		errorContains  string
		wantWarning    bool
	}{
		{
			name:        "no dynamic checkout expressions",
			expressions: nil,
			strictMode:  true,
			wantError:   false,
		},
		{
			name:        "safe dynamic checkout expression",
			expressions: []string{"${{ fromJSON(inputs.checkouts) }}"},
			strictMode:  true,
			wantError:   false,
		},
		{
			name:           "secrets reference in strict mode errors",
			expressions:    []string{"${{ fromJSON(format('[{0}]', secrets.MY_TOKEN)) }}"},
			rawFrontmatter: map[string]any{},
			strictMode:     true,
			wantError:      true,
			errorContains:  "strict mode",
		},
		{
			name:           "secrets reference in non-strict mode warns",
			expressions:    []string{"${{ fromJSON(format('[{0}]', secrets.MY_TOKEN)) }}"},
			rawFrontmatter: map[string]any{"strict": false},
			strictMode:     false,
			wantError:      false,
			wantWarning:    true,
		},
		{
			name: "duplicate secrets reference across expressions is deduplicated",
			expressions: []string{
				"${{ fromJSON(format('[{0}]', secrets.MY_TOKEN)) }}",
				"${{ fromJSON(format('[{0}]', secrets.MY_TOKEN)) }}",
			},
			rawFrontmatter: map[string]any{},
			strictMode:     true,
			wantError:      true,
			errorContains:  "strict mode",
		},
		{
			name:          "nil raw frontmatter still errors in strict mode",
			expressions:   []string{"${{ fromJSON(format('[{0}]', secrets.MY_TOKEN)) }}"},
			strictMode:    true,
			wantError:     true,
			errorContains: "strict mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = tt.strictMode
			workflowData := &WorkflowData{
				CheckoutExpressions: tt.expressions,
				RawFrontmatter:      tt.rawFrontmatter,
			}

			warningsBefore := compiler.GetWarningCount()
			err := compiler.validateDynamicCheckoutSecretsUsage(workflowData)

			if tt.wantError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}
			require.NoError(t, err)
			if tt.wantWarning {
				assert.Greater(t, compiler.GetWarningCount(), warningsBefore)
			} else {
				assert.Equal(t, warningsBefore, compiler.GetWarningCount())
			}
		})
	}
}
