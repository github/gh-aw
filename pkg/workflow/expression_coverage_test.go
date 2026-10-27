//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildFromAllowedForksEmptyList tests BuildFromAllowedForks with empty list
func TestBuildFromAllowedForksEmptyList(t *testing.T) {
	result := BuildFromAllowedForks([]string{})
	expected := "github.event.pull_request.head.repo.id == github.repository_id"
	assert.Equal(t, expected, result.Render(), "BuildFromAllowedForks([]) rendered output mismatch")
}

// TestBuildFromAllowedForksSingleCondition tests BuildFromAllowedForks with single pattern
func TestBuildFromAllowedForksSingleCondition(t *testing.T) {
	// When only the default condition exists, it should return just that condition
	// This tests the len(conditions) == 1 path
	result := BuildFromAllowedForks([]string{})
	rendered := result.Render()
	// Should not have DisjunctionNode wrapping for single condition
	assert.Equal(t, "github.event.pull_request.head.repo.id == github.repository_id", rendered,
		"BuildFromAllowedForks with empty list should return single condition without OR")
}

// TestBuildFromAllowedForksGlobPattern tests BuildFromAllowedForks with glob pattern
func TestBuildFromAllowedForksGlobPattern(t *testing.T) {
	result := BuildFromAllowedForks([]string{"myorg/*"})
	rendered := result.Render()
	// Should include both the default condition AND the glob pattern with OR
	assert.Contains(t, rendered, "github.event.pull_request.head.repo.id == github.repository_id",
		"BuildFromAllowedForks should include default condition")
	assert.Contains(t, rendered, "startsWith(github.event.pull_request.head.repo.full_name, 'myorg/')",
		"BuildFromAllowedForks should include glob pattern condition")
	assert.Contains(t, rendered, "||",
		"BuildFromAllowedForks with multiple conditions should use OR")
}

// TestBuildFromAllowedForksExactMatch tests BuildFromAllowedForks with exact match
func TestBuildFromAllowedForksExactMatch(t *testing.T) {
	result := BuildFromAllowedForks([]string{"myorg/myrepo"})
	rendered := result.Render()
	// Should include both the default condition AND the exact match with OR
	assert.Contains(t, rendered, "github.event.pull_request.head.repo.id == github.repository_id",
		"BuildFromAllowedForks should include default condition")
	assert.Contains(t, rendered, "github.event.pull_request.head.repo.full_name == 'myorg/myrepo'",
		"BuildFromAllowedForks should include exact match condition")
	assert.Contains(t, rendered, "||",
		"BuildFromAllowedForks with multiple conditions should use OR")
}

// TestBuildFromAllowedForksMixedPatterns tests BuildFromAllowedForks with mixed patterns
func TestBuildFromAllowedForksMixedPatterns(t *testing.T) {
	result := BuildFromAllowedForks([]string{"org1/*", "org2/repo1", "org3/*", "org4/repo2"})
	rendered := result.Render()

	// Should include default condition
	assert.Contains(t, rendered, "github.event.pull_request.head.repo.id == github.repository_id",
		"BuildFromAllowedForks should include default condition")

	// Should include all patterns
	expectedPatterns := []string{
		"startsWith(github.event.pull_request.head.repo.full_name, 'org1/')",
		"github.event.pull_request.head.repo.full_name == 'org2/repo1'",
		"startsWith(github.event.pull_request.head.repo.full_name, 'org3/')",
		"github.event.pull_request.head.repo.full_name == 'org4/repo2'",
	}

	for _, pattern := range expectedPatterns {
		assert.Contains(t, rendered, pattern, "BuildFromAllowedForks should include pattern")
	}

	// Should use DisjunctionNode with OR operators
	assert.Contains(t, rendered, "||", "BuildFromAllowedForks with multiple conditions should use OR")
}

// TestVisitExpressionTreeWithDifferentNodeTypes tests VisitExpressionTree with various node types
func TestVisitExpressionTreeWithDifferentNodeTypes(t *testing.T) {
	tests := []struct {
		name          string
		node          ConditionNode
		expectedCount int
		description   string
	}{
		{
			name:          "nil node",
			node:          nil,
			expectedCount: 0,
			description:   "should handle nil node",
		},
		{
			name: "ComparisonNode",
			node: &ComparisonNode{
				Left:     BuildPropertyAccess("github.event.action"),
				Operator: "==",
				Right:    BuildStringLiteral("opened"),
			},
			expectedCount: 0,
			description:   "ComparisonNode should not be visited (not ExpressionNode)",
		},
		{
			name:          "PropertyAccessNode",
			node:          BuildPropertyAccess("github.event.action"),
			expectedCount: 0,
			description:   "PropertyAccessNode should not be visited (not ExpressionNode)",
		},
		{
			name:          "StringLiteralNode",
			node:          BuildStringLiteral("test"),
			expectedCount: 0,
			description:   "StringLiteralNode should not be visited (not ExpressionNode)",
		},
		{
			name:          "FunctionCallNode",
			node:          BuildFunctionCall("contains", BuildPropertyAccess("array"), BuildStringLiteral("value")),
			expectedCount: 0,
			description:   "FunctionCallNode should not be visited (not ExpressionNode)",
		},
		{
			name: "DisjunctionNode with multiple terms",
			node: &DisjunctionNode{
				Terms: []ConditionNode{
					&ExpressionNode{Expression: "term1"},
					&ExpressionNode{Expression: "term2"},
					&ExpressionNode{Expression: "term3"},
				},
			},
			expectedCount: 3,
			description:   "DisjunctionNode should visit all ExpressionNode terms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var count int
			err := VisitExpressionTree(tt.node, func(expr *ExpressionNode) error {
				count++
				return nil
			})

			require.NoError(t, err, "VisitExpressionTree() unexpected error: %s", tt.description)
			assert.Equal(t, tt.expectedCount, count, "VisitExpressionTree() visited node count mismatch: %s", tt.description)
		})
	}
}

func TestExpressionParserRejectsTrailingTokens(t *testing.T) {
	_, err := ParseExpression("github.ref github.sha")
	require.Error(t, err)
}

// TestParseExpressionEmptyString tests ParseExpression with empty string
func TestParseExpressionEmptyString(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "whitespace only",
			input: "   \t\n  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseExpression(tt.input)
			require.Error(t, err, "ParseExpression() with empty/whitespace string should return error")
			require.ErrorContains(t, err, "empty expression",
				"ParseExpression(%q) unexpected error message", tt.input)
		})
	}
}
