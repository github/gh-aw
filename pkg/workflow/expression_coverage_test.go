//go:build !integration

package workflow

import (
	"testing"
)

// TestParenthesesNodeRender tests the ParenthesesNode Render method
func TestParenthesesNodeRender(t *testing.T) {
	tests := []struct {
		name     string
		child    ConditionNode
		expected string
	}{
		{
			name:     "simple expression",
			child:    &ExpressionNode{Expression: "github.event_name == 'issues'"},
			expected: "(github.event_name == 'issues')",
		},
		{
			name: "nested expression",
			child: &AndNode{
				Left:  &ExpressionNode{Expression: "condition1"},
				Right: &ExpressionNode{Expression: "condition2"},
			},
			expected: "((condition1) && (condition2))",
		},
		{
			name: "function call",
			child: &FunctionCallNode{
				FunctionName: "contains",
				Arguments: []ConditionNode{
					BuildPropertyAccess("github.event.labels"),
					BuildStringLiteral("bug"),
				},
			},
			expected: "(contains(github.event.labels, 'bug'))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &ParenthesesNode{Child: tt.child}
			result := node.Render()
			if result != tt.expected {
				t.Errorf("ParenthesesNode.Render() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestAddDetectionSuccessCheckEmptyCondition tests AddDetectionSuccessCheck with empty condition
func TestAddDetectionSuccessCheckEmptyCondition(t *testing.T) {
	result := AddDetectionSuccessCheck("")
	expected := "needs.detection.outputs.success == 'true'"
	if result != expected {
		t.Errorf("AddDetectionSuccessCheck(\"\") = %v, expected %v", result, expected)
	}
}

// TestAddDetectionSuccessCheckWithExistingCondition tests AddDetectionSuccessCheck with existing condition
func TestAddDetectionSuccessCheckWithExistingCondition(t *testing.T) {
	existingCondition := "github.event.action == 'opened'"
	result := AddDetectionSuccessCheck(existingCondition)
	expected := "(github.event.action == 'opened') && (needs.detection.outputs.success == 'true')"
	if result != expected {
		t.Errorf("AddDetectionSuccessCheck() = %v, expected %v", result, expected)
	}
}

// TestBuildFromAllowedForksEmptyList tests BuildFromAllowedForks with empty list
func TestBuildFromAllowedForksEmptyList(t *testing.T) {
	result := BuildFromAllowedForks([]string{})
	expected := "github.event.pull_request.head.repo.id == github.repository_id"
	if result.Render() != expected {
		t.Errorf("BuildFromAllowedForks([]) = %v, expected %v", result.Render(), expected)
	}
}

// TestBuildFromAllowedForksSingleCondition tests BuildFromAllowedForks with single pattern
func TestBuildFromAllowedForksSingleCondition(t *testing.T) {
	// When only the default condition exists, it should return just that condition
	// This tests the len(conditions) == 1 path
	result := BuildFromAllowedForks([]string{})
	rendered := result.Render()
	// Should not have DisjunctionNode wrapping for single condition
	if rendered != "github.event.pull_request.head.repo.id == github.repository_id" {
		t.Errorf("BuildFromAllowedForks with empty list should return single condition without OR")
	}
}

// TestBuildFromAllowedForksGlobPattern tests BuildFromAllowedForks with glob pattern
func TestBuildFromAllowedForksGlobPattern(t *testing.T) {
	result := BuildFromAllowedForks([]string{"myorg/*"})
	rendered := result.Render()
	// Should include both the default condition AND the glob pattern with OR
	if !containsSubstring(rendered, "github.event.pull_request.head.repo.id == github.repository_id") {
		t.Errorf("BuildFromAllowedForks should include default condition")
	}
	if !containsSubstring(rendered, "startsWith(github.event.pull_request.head.repo.full_name, 'myorg/')") {
		t.Errorf("BuildFromAllowedForks should include glob pattern condition")
	}
	if !containsSubstring(rendered, "||") {
		t.Errorf("BuildFromAllowedForks with multiple conditions should use OR")
	}
}

// TestBuildFromAllowedForksExactMatch tests BuildFromAllowedForks with exact match
func TestBuildFromAllowedForksExactMatch(t *testing.T) {
	result := BuildFromAllowedForks([]string{"myorg/myrepo"})
	rendered := result.Render()
	// Should include both the default condition AND the exact match with OR
	if !containsSubstring(rendered, "github.event.pull_request.head.repo.id == github.repository_id") {
		t.Errorf("BuildFromAllowedForks should include default condition")
	}
	if !containsSubstring(rendered, "github.event.pull_request.head.repo.full_name == 'myorg/myrepo'") {
		t.Errorf("BuildFromAllowedForks should include exact match condition")
	}
	if !containsSubstring(rendered, "||") {
		t.Errorf("BuildFromAllowedForks with multiple conditions should use OR")
	}
}

// TestBuildFromAllowedForksMixedPatterns tests BuildFromAllowedForks with mixed patterns
func TestBuildFromAllowedForksMixedPatterns(t *testing.T) {
	result := BuildFromAllowedForks([]string{"org1/*", "org2/repo1", "org3/*", "org4/repo2"})
	rendered := result.Render()

	// Should include default condition
	if !containsSubstring(rendered, "github.event.pull_request.head.repo.id == github.repository_id") {
		t.Errorf("BuildFromAllowedForks should include default condition")
	}

	// Should include all patterns
	expectedPatterns := []string{
		"startsWith(github.event.pull_request.head.repo.full_name, 'org1/')",
		"github.event.pull_request.head.repo.full_name == 'org2/repo1'",
		"startsWith(github.event.pull_request.head.repo.full_name, 'org3/')",
		"github.event.pull_request.head.repo.full_name == 'org4/repo2'",
	}

	for _, pattern := range expectedPatterns {
		if !containsSubstring(rendered, pattern) {
			t.Errorf("BuildFromAllowedForks should include pattern: %s", pattern)
		}
	}

	// Should use DisjunctionNode with OR operators
	if !containsSubstring(rendered, "||") {
		t.Errorf("BuildFromAllowedForks with multiple conditions should use OR")
	}
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
			name: "TernaryNode",
			node: BuildTernary(
				&ExpressionNode{Expression: "condition"},
				&ExpressionNode{Expression: "true_value"},
				&ExpressionNode{Expression: "false_value"},
			),
			expectedCount: 0,
			description:   "TernaryNode should not recurse (not in visitor)",
		},
		{
			name:          "ContainsNode",
			node:          BuildContains(BuildPropertyAccess("array"), BuildStringLiteral("value")),
			expectedCount: 0,
			description:   "ContainsNode should not be visited (not ExpressionNode)",
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

			if err != nil {
				t.Errorf("VisitExpressionTree() unexpected error: %v", err)
			}

			if count != tt.expectedCount {
				t.Errorf("VisitExpressionTree() visited %d nodes, expected %d: %s", count, tt.expectedCount, tt.description)
			}
		})
	}
}

// TestExpressionParserCurrentWithEmptyTokens tests the current() method edge case
func TestExpressionParserCurrentWithEmptyTokens(t *testing.T) {
	parser := &ExpressionParser{
		tokens: []token{},
		pos:    0,
	}

	result := parser.current()
	if result.kind != tokenEOF {
		t.Errorf("current() with empty tokens should return EOF token, got %v", result.kind)
	}
	if result.pos != -1 {
		t.Errorf("current() with empty tokens should return pos -1, got %d", result.pos)
	}
}

// TestExpressionParserCurrentBeyondLength tests the current() method when pos >= len(tokens)
func TestExpressionParserCurrentBeyondLength(t *testing.T) {
	parser := &ExpressionParser{
		tokens: []token{
			{tokenLiteral, "test", 0},
		},
		pos: 5, // Beyond array length
	}

	result := parser.current()
	if result.kind != tokenEOF {
		t.Errorf("current() with pos beyond length should return EOF token, got %v", result.kind)
	}
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
			if err == nil {
				t.Error("ParseExpression() with empty/whitespace string should return error")
			}
			if err.Error() != "empty expression" {
				t.Errorf("ParseExpression() error = %v, expected 'empty expression'", err)
			}
		})
	}
}
