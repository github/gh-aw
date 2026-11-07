//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestExpressionNode_Render(t *testing.T) {
	expr := &ExpressionNode{Expression: "github.event_name == 'issues'"}
	expected := "github.event_name == 'issues'"
	if result := expr.Render(); result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestAndNode_Render(t *testing.T) {
	left := &ExpressionNode{Expression: "condition1"}
	right := &ExpressionNode{Expression: "condition2"}
	andNode := &AndNode{Left: left, Right: right}

	expected := "(condition1) && (condition2)"
	if result := andNode.Render(); result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestOrNode_Render(t *testing.T) {
	left := &ExpressionNode{Expression: "condition1"}
	right := &ExpressionNode{Expression: "condition2"}
	orNode := &OrNode{Left: left, Right: right}

	expected := "condition1 || condition2"
	if result := orNode.Render(); result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestNotNode_Render(t *testing.T) {
	child := &ExpressionNode{Expression: "github.event_name == 'issues'"}
	notNode := &NotNode{Child: child}

	expected := "!(github.event_name == 'issues')"
	if result := notNode.Render(); result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestDisjunctionNode_Render(t *testing.T) {
	tests := []struct {
		name     string
		terms    []ConditionNode
		expected string
	}{
		{
			name:     "empty terms",
			terms:    []ConditionNode{},
			expected: "",
		},
		{
			name: "single term",
			terms: []ConditionNode{
				&ExpressionNode{Expression: "condition1"},
			},
			expected: "condition1",
		},
		{
			name: "two terms",
			terms: []ConditionNode{
				&ExpressionNode{Expression: "condition1"},
				&ExpressionNode{Expression: "condition2"},
			},
			expected: "condition1 || condition2",
		},
		{
			name: "multiple terms",
			terms: []ConditionNode{
				&ExpressionNode{Expression: "github.event_name == 'issues'"},
				&ExpressionNode{Expression: "github.event_name == 'pull_request'"},
				&ExpressionNode{Expression: "github.event_name == 'issue_comment'"},
			},
			expected: "github.event_name == 'issues' || github.event_name == 'pull_request' || github.event_name == 'issue_comment'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disjunctionNode := &DisjunctionNode{Terms: tt.terms}
			if result := disjunctionNode.Render(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestComplexExpressionTree(t *testing.T) {
	// Test: (condition1 && condition2) || !(condition3)
	condition1 := &ExpressionNode{Expression: "github.event_name == 'issues'"}
	condition2 := &ExpressionNode{Expression: "github.event.action == 'opened'"}
	condition3 := &ExpressionNode{Expression: "github.event.pull_request.draft == true"}

	andNode := &AndNode{Left: condition1, Right: condition2}
	notNode := &NotNode{Child: condition3}
	orNode := &OrNode{Left: andNode, Right: notNode}

	// OrNode{AndNode{ExprNode,ExprNode}, NotNode{ExprNode}}: OR no longer wraps children,
	// AND wraps ExpressionNodes but not NotNode.
	expected := "(github.event_name == 'issues') && (github.event.action == 'opened') || !(github.event.pull_request.draft == true)"
	if result := orNode.Render(); result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestBuildConditionTree(t *testing.T) {
	tests := []struct {
		name              string
		existingCondition string
		draftCondition    string
		expectedPattern   string
	}{
		{
			name:              "empty existing condition",
			existingCondition: "",
			draftCondition:    "draft_condition",
			expectedPattern:   "draft_condition",
		},
		{
			name:              "both conditions present",
			existingCondition: "existing_condition",
			draftCondition:    "draft_condition",
			expectedPattern:   "(existing_condition) && (draft_condition)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildConditionTree(tt.existingCondition, tt.draftCondition)
			if rendered := result.Render(); rendered != tt.expectedPattern {
				t.Errorf("Expected '%s', got '%s'", tt.expectedPattern, rendered)
			}
		})
	}
}

func TestBuildReactionConditionForTargetsExcludesPullRequests(t *testing.T) {
	result := BuildReactionConditionForTargets(true, false, true, false)
	rendered := result.Render()

	if strings.Contains(rendered, "github.event_name == 'pull_request'") {
		t.Errorf("Expected pull_request event to be excluded when pull request reactions are disabled, got: %s", rendered)
	}
	if strings.Contains(rendered, "github.event_name == 'pull_request_review_comment'") {
		t.Errorf("Expected pull_request_review_comment event to be excluded when pull request reactions are disabled, got: %s", rendered)
	}
	if strings.Contains(rendered, "github.event_name == 'pull_request_review'") {
		t.Errorf("Expected pull_request_review event to be excluded when pull request reactions are disabled, got: %s", rendered)
	}
	if !strings.Contains(rendered, "github.event_name == 'issues'") {
		t.Errorf("Expected issues event to remain when issue reactions are enabled, got: %s", rendered)
	}
}

func TestBuildReactionConditionExcludesPullRequestReview(t *testing.T) {
	result := BuildReactionConditionForTargets(true, true, true, false)
	rendered := result.Render()

	if strings.Contains(rendered, "github.event_name == 'pull_request_review'") {
		t.Errorf("Expected pull_request_review to be excluded from reaction condition, got: %s", rendered)
	}
	if !strings.Contains(rendered, "github.event_name == 'issues'") {
		t.Errorf("Expected issues event to remain in reaction condition, got: %s", rendered)
	}
	if !strings.Contains(rendered, "github.event_name == 'pull_request' && github.event.pull_request.head.repo.id == github.repository_id") {
		t.Errorf("Expected pull_request with fork guard to remain in reaction condition, got: %s", rendered)
	}
	if !strings.Contains(rendered, "github.event_name == 'discussion'") {
		t.Errorf("Expected discussion event to remain in reaction condition, got: %s", rendered)
	}
}

func TestBuildStatusCommentConditionExcludesPullRequestReview(t *testing.T) {
	result := BuildStatusCommentCondition(true, true, true, false)
	rendered := result.Render()

	if strings.Contains(rendered, "github.event_name == 'pull_request_review'") {
		t.Errorf("Expected pull_request_review to be excluded from status comment condition, got: %s", rendered)
	}
	if !strings.Contains(rendered, "github.event_name == 'issues'") {
		t.Errorf("Expected issues event to remain in status comment condition, got: %s", rendered)
	}
	if !strings.Contains(rendered, "github.event_name == 'pull_request' && github.event.pull_request.head.repo.id == github.repository_id") {
		t.Errorf("Expected pull_request with fork guard to remain in status comment condition, got: %s", rendered)
	}
	if !strings.Contains(rendered, "github.event_name == 'discussion'") {
		t.Errorf("Expected discussion event to remain in status comment condition, got: %s", rendered)
	}
}

func TestBuildStatusCommentConditionExcludesPullRequestReviewWhenPullRequestsDisabled(t *testing.T) {
	result := BuildStatusCommentCondition(true, false, true, false)
	rendered := result.Render()

	if strings.Contains(rendered, "github.event_name == 'pull_request_review'") {
		t.Errorf("Expected pull_request_review event to be excluded when includePullRequests is false, got: %s", rendered)
	}
}

func TestBuildReactionConditionForTargetsIncludesWorkflowDispatchForCentralized(t *testing.T) {
	result := BuildReactionConditionForTargets(true, false, true, true)
	rendered := result.Render()

	if !strings.Contains(rendered, "github.event_name == 'workflow_dispatch'") {
		t.Errorf("Expected workflow_dispatch branch for centralized reaction condition, got: %s", rendered)
	}
	if !strings.Contains(rendered, "fromJSON(github.event.inputs.aw_context || '{}').event_type == 'issue_comment'") {
		t.Errorf("Expected aw_context source event condition for issue_comment, got: %s", rendered)
	}
	if strings.Contains(rendered, "fromJSON(github.event.inputs.aw_context || '{}').event_type == 'pull_request'") {
		t.Errorf("Did not expect pull_request aw_context condition when includePullRequests is false, got: %s", rendered)
	}
}

func TestBuildStatusCommentConditionIncludesWorkflowDispatchForCentralized(t *testing.T) {
	result := BuildStatusCommentCondition(true, true, true, true)
	rendered := result.Render()

	if !strings.Contains(rendered, "github.event_name == 'workflow_dispatch'") {
		t.Errorf("Expected workflow_dispatch branch for centralized status-comment condition, got: %s", rendered)
	}
	if strings.Contains(rendered, "fromJSON(github.event.inputs.aw_context || '{}').event_type == 'pull_request_review'") {
		t.Errorf("Expected aw_context pull_request_review source event condition to be excluded, got: %s", rendered)
	}
}

func TestBuildReactionConditionForTargetsNoTargetsReturnsFalse(t *testing.T) {
	result := BuildReactionConditionForTargets(false, false, false, false)
	rendered := RenderCondition(result)

	if rendered != "false" {
		t.Errorf("Expected false condition when all reaction targets are disabled, got: %s", rendered)
	}
}

func TestBuildStatusCommentConditionNoTargetsReturnsFalseWithDispatch(t *testing.T) {
	result := BuildStatusCommentCondition(false, false, false, true)
	rendered := RenderCondition(result)

	if rendered != "false" {
		t.Errorf("Expected false condition when all status-comment targets are disabled, got: %s", rendered)
	}
}

func TestFunctionCallNode_Render(t *testing.T) {
	tests := []struct {
		name     string
		function string
		args     []ConditionNode
		expected string
	}{
		{
			name:     "contains function with two arguments",
			function: "contains",
			args: []ConditionNode{
				&PropertyAccessNode{PropertyPath: "github.event.issue.labels"},
				&StringLiteralNode{Value: "bug"},
			},
			expected: "contains(github.event.issue.labels, 'bug')",
		},
		{
			name:     "startsWith function",
			function: "startsWith",
			args: []ConditionNode{
				&PropertyAccessNode{PropertyPath: "github.ref"},
				&StringLiteralNode{Value: "refs/heads/"},
			},
			expected: "startsWith(github.ref, 'refs/heads/')",
		},
		{
			name:     "function with no arguments",
			function: "always",
			args:     []ConditionNode{},
			expected: "always()",
		},
		{
			name:     "function with multiple arguments",
			function: "format",
			args: []ConditionNode{
				&StringLiteralNode{Value: "Hello {0}"},
				&PropertyAccessNode{PropertyPath: "github.actor"},
			},
			expected: "format('Hello {0}', github.actor)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &FunctionCallNode{
				FunctionName: tt.function,
				Arguments:    tt.args,
			}
			if result := node.Render(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestPropertyAccessNode_Render(t *testing.T) {
	tests := []struct {
		name     string
		property string
		expected string
	}{
		{
			name:     "simple property",
			property: "github.actor",
			expected: "github.actor",
		},
		{
			name:     "nested property",
			property: "github.event.issue.number",
			expected: "github.event.issue.number",
		},
		{
			name:     "deep nested property",
			property: "github.event.pull_request.head.sha",
			expected: "github.event.pull_request.head.sha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &PropertyAccessNode{PropertyPath: tt.property}
			if result := node.Render(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestStringLiteralNode_Render(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{
			name:     "simple string",
			value:    "hello",
			expected: "'hello'",
		},
		{
			name:     "string with spaces",
			value:    "hello world",
			expected: "'hello world'",
		},
		{
			name:     "empty string",
			value:    "",
			expected: "''",
		},
		{
			name:     "string with special characters",
			value:    "issue-123",
			expected: "'issue-123'",
		},
		{
			name:     "string with single quote",
			value:    "can't-repro",
			expected: "'can''t-repro'",
		},
		{
			name:     "string with multiple single quotes",
			value:    "it's a bug (it's real)",
			expected: "'it''s a bug (it''s real)'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &StringLiteralNode{Value: tt.value}
			if result := node.Render(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestBooleanLiteralNode_Render(t *testing.T) {
	tests := []struct {
		name     string
		value    bool
		expected string
	}{
		{
			name:     "true value",
			value:    true,
			expected: "true",
		},
		{
			name:     "false value",
			value:    false,
			expected: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &BooleanLiteralNode{Value: tt.value}
			if result := node.Render(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestComparisonNode_Render(t *testing.T) {
	tests := []struct {
		name     string
		left     ConditionNode
		operator string
		right    ConditionNode
		expected string
	}{
		{
			name:     "equality comparison",
			left:     &PropertyAccessNode{PropertyPath: "github.event.action"},
			operator: "==",
			right:    &StringLiteralNode{Value: "opened"},
			expected: "github.event.action == 'opened'",
		},
		{
			name:     "inequality comparison",
			left:     &PropertyAccessNode{PropertyPath: "github.event.issue.number"},
			operator: "!=",
			right:    &ExpressionNode{Expression: "0"},
			expected: "github.event.issue.number != 0",
		},
		{
			name:     "greater than comparison",
			left:     &PropertyAccessNode{PropertyPath: "github.event.issue.comments"},
			operator: ">",
			right:    &ExpressionNode{Expression: "5"},
			expected: "github.event.issue.comments > 5",
		},
		{
			name:     "less than or equal comparison",
			left:     &PropertyAccessNode{PropertyPath: "github.run_number"},
			operator: "<=",
			right:    &ExpressionNode{Expression: "100"},
			expected: "github.run_number <= 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &ComparisonNode{
				Left:     tt.left,
				Operator: tt.operator,
				Right:    tt.right,
			}
			if result := node.Render(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// TestHelperFunctions tests the helper functions for building expressions
func TestHelperFunctions(t *testing.T) {
	t.Run("BuildPropertyAccess", func(t *testing.T) {
		node := BuildPropertyAccess("github.event.action")
		expected := "github.event.action"
		if result := node.Render(); result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

	t.Run("BuildStringLiteral", func(t *testing.T) {
		node := BuildStringLiteral("opened")
		expected := "'opened'"
		if result := node.Render(); result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

	t.Run("BuildBooleanLiteral", func(t *testing.T) {
		node := BuildBooleanLiteral(true)
		expected := "true"
		if result := node.Render(); result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

	t.Run("BuildEquals", func(t *testing.T) {
		node := BuildEquals(
			BuildPropertyAccess("github.event.action"),
			BuildStringLiteral("opened"),
		)
		expected := "github.event.action == 'opened'"
		if result := node.Render(); result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

	t.Run("BuildFunctionCall", func(t *testing.T) {
		node := BuildFunctionCall("startsWith",
			BuildPropertyAccess("github.ref"),
			BuildStringLiteral("refs/heads/"),
		)
		expected := "startsWith(github.ref, 'refs/heads/')"
		if result := node.Render(); result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

}

// TestConvenienceHelpers tests the convenience helper functions
func TestConvenienceHelpers(t *testing.T) {
	t.Run("BuildEventTypeEquals", func(t *testing.T) {
		node := BuildEventTypeEquals("push")
		expected := "github.event_name == 'push'"
		if result := node.Render(); result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

}

// TestRealWorldExpressionPatterns tests common expression patterns used in GitHub Actions
func TestRealWorldExpressionPatterns(t *testing.T) {
	tests := []struct {
		name       string
		expression ConditionNode
		expected   string
	}{
		{
			name: "run on main branch only",
			expression: BuildEquals(
				BuildPropertyAccess("github.ref"),
				BuildStringLiteral("refs/heads/main"),
			),
			expected: "github.ref == 'refs/heads/main'",
		},
		{
			name: "skip draft PRs",
			expression: &AndNode{
				Left: BuildEventTypeEquals("pull_request"),
				Right: &NotNode{
					Child: BuildPropertyAccess("github.event.pull_request.draft"),
				},
			},
			// ComparisonNode is not wrapped in AND; NotNode is wrapped (YAML ! safety)
			expected: "github.event_name == 'pull_request' && (!(github.event.pull_request.draft))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := tt.expression.Render(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// TestExpressionNodeWithDescription tests ExpressionNode with description field
func TestExpressionNodeWithDescription(t *testing.T) {
	tests := []struct {
		name        string
		expression  string
		description string
		expected    string
	}{
		{
			name:        "expression without description",
			expression:  "github.event_name == 'issues'",
			description: "",
			expected:    "github.event_name == 'issues'",
		},
		{
			name:        "expression with description",
			expression:  "github.event_name == 'issues'",
			description: "Check if this is an issue event",
			expected:    "github.event_name == 'issues'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &ExpressionNode{
				Expression:  tt.expression,
				Description: tt.description,
			}
			if result := expr.Render(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// TestDisjunctionNodeMultiline tests multiline rendering functionality
func TestDisjunctionNodeMultiline(t *testing.T) {
	tests := []struct {
		name      string
		terms     []ConditionNode
		multiline bool
		expected  string
	}{
		{
			name: "single line rendering (default)",
			terms: []ConditionNode{
				&ExpressionNode{Expression: "github.event_name == 'issues'", Description: "Check if this is an issue event"},
				&ExpressionNode{Expression: "github.event_name == 'pull_request'", Description: "Check if this is a pull request event"},
			},
			multiline: false,
			expected:  "github.event_name == 'issues' || github.event_name == 'pull_request'",
		},
		{
			name: "multiline rendering with comments",
			terms: []ConditionNode{
				&ExpressionNode{Expression: "github.event_name == 'issues'", Description: "Check if this is an issue event"},
				&ExpressionNode{Expression: "github.event_name == 'pull_request'", Description: "Check if this is a pull request event"},
			},
			multiline: true,
			expected:  "# Check if this is an issue event\ngithub.event_name == 'issues' ||\n# Check if this is a pull request event\ngithub.event_name == 'pull_request'",
		},
		{
			name: "multiline rendering without comments",
			terms: []ConditionNode{
				&ExpressionNode{Expression: "github.event_name == 'issues'"},
				&ExpressionNode{Expression: "github.event_name == 'pull_request'"},
			},
			multiline: true,
			expected:  "github.event_name == 'issues' ||\ngithub.event_name == 'pull_request'",
		},
		{
			name: "multiline rendering with mixed comment presence",
			terms: []ConditionNode{
				&ExpressionNode{Expression: "github.event_name == 'issues'", Description: "Check if this is an issue event"},
				&ExpressionNode{Expression: "github.event_name == 'pull_request'"},
				&ExpressionNode{Expression: "github.event_name == 'issue_comment'", Description: "Check if this is an issue comment event"},
			},
			multiline: true,
			expected:  "# Check if this is an issue event\ngithub.event_name == 'issues' ||\ngithub.event_name == 'pull_request' ||\n# Check if this is an issue comment event\ngithub.event_name == 'issue_comment'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disjunctionNode := &DisjunctionNode{
				Terms:     tt.terms,
				Multiline: tt.multiline,
			}
			if result := disjunctionNode.Render(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// TestRenderMultilineMethod tests the RenderMultiline method directly
func TestRenderMultilineMethod(t *testing.T) {
	tests := []struct {
		name     string
		terms    []ConditionNode
		expected string
	}{
		{
			name:     "empty terms",
			terms:    []ConditionNode{},
			expected: "",
		},
		{
			name: "single term",
			terms: []ConditionNode{
				&ExpressionNode{Expression: "condition1", Description: "First condition"},
			},
			expected: "condition1",
		},
		{
			name: "multiple terms with comments",
			terms: []ConditionNode{
				&ExpressionNode{Expression: "github.event_name == 'issues'", Description: "Handle issue events"},
				&ExpressionNode{Expression: "github.event_name == 'pull_request'", Description: "Handle PR events"},
				&ExpressionNode{Expression: "github.event_name == 'issue_comment'", Description: "Handle comment events"},
			},
			expected: "# Handle issue events\ngithub.event_name == 'issues' ||\n# Handle PR events\ngithub.event_name == 'pull_request' ||\n# Handle comment events\ngithub.event_name == 'issue_comment'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disjunctionNode := &DisjunctionNode{Terms: tt.terms}
			if result := disjunctionNode.RenderMultiline(); result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// TestHelperFunctionsForMultiline tests the new helper functions
func TestHelperFunctionsForMultiline(t *testing.T) {
	t.Run("BuildDisjunction with multiline", func(t *testing.T) {
		term1 := &ExpressionNode{Expression: "github.event_name == 'issues'", Description: "Handle issue events"}
		term2 := &ExpressionNode{Expression: "github.event_name == 'pull_request'", Description: "Handle PR events"}

		disjunction := BuildDisjunction(true, term1, term2)

		if !disjunction.Multiline {
			t.Error("Expected Multiline to be true")
		}

		expected := "# Handle issue events\ngithub.event_name == 'issues' ||\n# Handle PR events\ngithub.event_name == 'pull_request'"
		if result := disjunction.Render(); result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

	t.Run("BuildDisjunction with single term", func(t *testing.T) {
		term := &ExpressionNode{Expression: "github.event_name == 'issues'", Description: "Handle issue events"}

		// Test with multiline=false
		disjunctionSingle := BuildDisjunction(false, term)
		expected := "github.event_name == 'issues'"
		if result := disjunctionSingle.Render(); result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}

		// Test with multiline=true - should still render as single term without OR
		disjunctionMulti := BuildDisjunction(true, term)
		if result := disjunctionMulti.Render(); result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})
}

func TestBuildNotFromFork(t *testing.T) {
	result := BuildNotFromFork()
	rendered := result.Render()

	expected := "github.event.pull_request.head.repo.id == github.repository_id"
	if rendered != expected {
		t.Errorf("Expected '%s', got '%s'", expected, rendered)
	}
}

// TestParseExpression tests the expression parser with various input patterns
func TestParseExpression(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "simple literal",
			input:    "github.event_name == 'issues'",
			expected: "github.event_name == 'issues'",
			wantErr:  false,
		},
		{
			name:     "simple AND",
			input:    "condition1 && condition2",
			expected: "(condition1) && (condition2)",
			wantErr:  false,
		},
		{
			name:     "simple OR",
			input:    "condition1 || condition2",
			expected: "condition1 || condition2",
			wantErr:  false,
		},
		{
			name:     "simple NOT",
			input:    "!condition1",
			expected: "!(condition1)",
			wantErr:  false,
		},
		{
			name:     "parenthesized expression",
			input:    "(condition1)",
			expected: "condition1",
			wantErr:  false,
		},
		{
			name:     "AND has higher precedence than OR",
			input:    "a || b && c",
			expected: "a || (b) && (c)",
			wantErr:  false,
		},
		{
			name:     "parentheses override precedence",
			input:    "(a || b) && c",
			expected: "(a || b) && (c)",
			wantErr:  false,
		},
		{
			name:     "complex expression with multiple operators",
			input:    "(github.event_name == 'issues') && (github.event.action == 'opened') || !(github.event.pull_request.draft == true)",
			expected: "(github.event_name == 'issues') && (github.event.action == 'opened') || !(github.event.pull_request.draft == true)",
			wantErr:  false,
		},
		{
			name:     "multiple NOTs",
			input:    "!!condition1",
			expected: "!(!(condition1))",
			wantErr:  false,
		},
		{
			name:     "nested parentheses",
			input:    "((a && b) || (c && d))",
			expected: "(a) && (b) || (c) && (d)",
			wantErr:  false,
		},
		{
			name:     "whitespace handling",
			input:    "  a  &&  b  ",
			expected: "(a) && (b)",
			wantErr:  false,
		},
		{
			name:     "expression with quotes",
			input:    "github.event_name == 'pull_request' && github.event.action == 'opened'",
			expected: "(github.event_name == 'pull_request') && (github.event.action == 'opened')",
			wantErr:  false,
		},
		{
			name:    "missing closing parenthesis",
			input:   "(condition1",
			wantErr: true,
		},
		{
			name:    "missing opening parenthesis",
			input:   "condition1)",
			wantErr: true,
		},
		{
			name:    "empty expression",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only operators",
			input:   "&&",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseExpression(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseExpression() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseExpression() unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Errorf("ParseExpression() returned nil result")
				return
			}

			rendered := result.Render()
			if rendered != tt.expected {
				t.Errorf("ParseExpression() = '%s', want '%s'", rendered, tt.expected)
			}
		})
	}
}

// TestParseExpressionIntegration tests parsing and rendering the tree
func TestParseExpressionIntegration(t *testing.T) {
	input := "(github.event_name == 'issues') && (github.event.action == 'opened') || !(contains(github.event.labels, 'wip'))"

	// Parse the expression
	tree, err := ParseExpression(input)
	if err != nil {
		t.Fatalf("ParseExpression() error: %v", err)
	}

	// Verify the tree structure by rendering
	rendered := tree.Render()
	expectedRendered := "(github.event_name == 'issues') && (github.event.action == 'opened') || !(contains(github.event.labels, 'wip'))"
	if rendered != expectedRendered {
		t.Errorf("Rendered = '%s', expected '%s'", rendered, expectedRendered)
	}
}

// TestBreakLongExpression tests the expression line breaking functionality
func TestBreakLongExpression(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		wantLines  int
		maxLen     int // max length of any single line
	}{
		{
			name:       "short expression stays single line",
			expression: "github.event_name == 'issues'",
			wantLines:  1,
			maxLen:     50,
		},
		{
			name:       "long expression gets broken",
			expression: "github.event_name == 'issues' || github.event_name == 'issue_comment' || github.event_name == 'pull_request_comment' || github.event_name == 'pull_request_review_comment'",
			wantLines:  2, // Should break at || operator when line gets too long
			maxLen:     120,
		},
		{
			name:       "very long expression with multiple operators",
			expression: "github.event_name == 'issues' || github.event_name == 'issue_comment' || github.event_name == 'pull_request_comment' || github.event_name == 'pull_request_review_comment' || (github.event_name == 'pull_request') && (github.event.pull_request.head.repo.full_name == github.repository)",
			wantLines:  3, // Should break at multiple points
			maxLen:     120,
		},
		{
			name:       "expression with quoted strings",
			expression: "contains(github.event.issue.body, 'very long string that should not be broken even though it is quite long') && github.event.action == 'opened'",
			wantLines:  2, // Should break after the quoted string
			maxLen:     120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := BreakLongExpression(tt.expression)

			if len(lines) != tt.wantLines {
				t.Errorf("BreakLongExpression() got %d lines, want %d\nLines: %v", len(lines), tt.wantLines, lines)
			}

			// Check that no line exceeds maximum length
			for i, line := range lines {
				if len(line) > tt.maxLen {
					t.Errorf("Line %d exceeds maximum length %d: %d chars\nLine: %s", i, tt.maxLen, len(line), line)
				}
			}

			// Verify that joined lines equal normalized original (whitespace differences allowed)
			joined := strings.Join(lines, " ")
			originalNorm := strings.Join(strings.Fields(tt.expression), " ")
			joinedNorm := strings.Join(strings.Fields(joined), " ")

			if joinedNorm != originalNorm {
				t.Errorf("Joined lines don't match original expression\nOriginal: %s\nJoined:   %s", originalNorm, joinedNorm)
			}
		})
	}
}

// TestBreakAtParentheses tests breaking expressions at parentheses boundaries
func TestBreakAtParentheses(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		wantLines  int
	}{
		{
			name:       "short expression with parentheses",
			expression: "(github.event_name == 'issues') && (github.event.action == 'opened')",
			wantLines:  1, // Should stay as single line
		},
		{
			name:       "long expression with function calls",
			expression: "(contains(github.event.issue.labels, 'bug') && contains(github.event.issue.labels, 'priority-high')) || (contains(github.event.pull_request.labels, 'urgent'))",
			wantLines:  2, // Should break at logical operator after parentheses
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := BreakAtParentheses(tt.expression)

			if len(lines) != tt.wantLines {
				t.Errorf("BreakAtParentheses() got %d lines, want %d\nLines: %v", len(lines), tt.wantLines, lines)
			}

			// Verify that joined lines equal normalized original
			joined := strings.Join(lines, " ")
			originalNorm := strings.Join(strings.Fields(tt.expression), " ")
			joinedNorm := strings.Join(strings.Fields(joined), " ")

			if joinedNorm != originalNorm {
				t.Errorf("Joined lines don't match original expression\nOriginal: %s\nJoined:   %s", originalNorm, joinedNorm)
			}
		})
	}
}

// TestMultilineExpressionEquivalence tests that multiline expressions are equivalent to single-line expressions
func TestMultilineExpressionEquivalence(t *testing.T) {
	tests := []struct {
		name          string
		singleLine    string
		multiLine     string
		shouldBeEqual bool
	}{
		{
			name:       "simple equivalent expressions",
			singleLine: "github.event_name == 'issues' || github.event.action == 'opened'",
			multiLine: `github.event_name == 'issues' ||
github.event.action == 'opened'`,
			shouldBeEqual: true,
		},
		{
			name:       "complex equivalent expressions with extra whitespace",
			singleLine: "github.event_name == 'issues' || github.event_name == 'pull_request' && github.event.action == 'opened'",
			multiLine: `github.event_name == 'issues'   ||   
github.event_name == 'pull_request'    &&    
github.event.action == 'opened'`,
			shouldBeEqual: true,
		},
		{
			name:          "different expressions should not be equal",
			singleLine:    "github.event_name == 'issues'",
			multiLine:     `github.event_name == 'pull_request'`,
			shouldBeEqual: false,
		},
		{
			name:       "reaction condition equivalence",
			singleLine: "github.event_name == 'issues' || github.event_name == 'issue_comment' || (github.event_name == 'pull_request') && (github.event.pull_request.head.repo.full_name == github.repository)",
			multiLine: `github.event_name == 'issues' ||
github.event_name == 'issue_comment' ||
(github.event_name == 'pull_request') &&
(github.event.pull_request.head.repo.full_name == github.repository)`,
			shouldBeEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			singleNorm := strings.Join(strings.Fields(tt.singleLine), " ")
			multiNorm := strings.Join(strings.Fields(tt.multiLine), " ")

			isEqual := singleNorm == multiNorm
			if isEqual != tt.shouldBeEqual {
				t.Errorf("Expression equivalence: got %v, expected %v\nSingle: %s\nMulti:  %s\nSingle normalized: %s\nMulti normalized:  %s",
					isEqual, tt.shouldBeEqual, tt.singleLine, tt.multiLine, singleNorm, multiNorm)
			}
		})
	}
}

// TestLongExpressionBreakingDetailed tests automatic line breaking of expressions longer than 120 characters
func TestLongExpressionBreakingDetailed(t *testing.T) {
	tests := []struct {
		name       string
		expression string
	}{
		{
			name:       "reaction condition expression",
			expression: "github.event_name == 'issues' || github.event_name == 'issue_comment' || github.event_name == 'pull_request_comment' || github.event_name == 'pull_request_review_comment' || (github.event_name == 'pull_request') && (github.event.pull_request.head.repo.full_name == github.repository)",
		},
		{
			name:       "complex nested expression",
			expression: "((contains(github.event.issue.body, '/test-bot')) || (contains(github.event.comment.body, '/test-bot'))) || (contains(github.event.pull_request.body, '/test-bot'))",
		},
		{
			name:       "multiple function calls",
			expression: "contains(github.event.issue.labels, 'bug') && contains(github.event.issue.labels, 'priority-high') && contains(github.event.issue.labels, 'needs-review')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that expressions longer than 120 chars get broken into multiple lines
			if len(tt.expression) <= 120 {
				t.Skipf("Expression is not long enough (%d chars) to test breaking", len(tt.expression))
			}

			lines := BreakLongExpression(tt.expression)

			// Should have more than one line for long expressions
			if len(lines) <= 1 {
				t.Errorf("Expected multiple lines for long expression, got %d lines", len(lines))
			}

			// Each line should be within reasonable length
			for i, line := range lines {
				if len(line) > 120 {
					t.Errorf("Line %d is too long (%d chars): %s", i, len(line), line)
				}
			}

			// Most importantly: verify equivalence
			joined := strings.Join(lines, " ")
			originalNorm := strings.Join(strings.Fields(tt.expression), " ")
			joinedNorm := strings.Join(strings.Fields(joined), " ")

			if joinedNorm != originalNorm {
				t.Errorf("Broken expression is not equivalent to original\nOriginal: %s\nBroken:   %s\nJoined:   %s\nOriginal normalized: %s\nJoined normalized:   %s",
					tt.expression, strings.Join(lines, "\n"), joined, originalNorm, joinedNorm)
			}
		})
	}
}

// TestExpressionBreakingWithQuotes tests that quotes are handled correctly during line breaking
func TestExpressionBreakingWithQuotes(t *testing.T) {
	tests := []struct {
		name       string
		expression string
	}{
		{
			name:       "single quoted strings",
			expression: "contains(github.event.issue.body, 'this is a very long string that should not be broken even though it contains || and && operators') && github.event.action == 'opened'",
		},
		{
			name:       "double quoted strings",
			expression: `contains(github.event.issue.body, "this is a very long string that should not be broken even though it contains || and && operators") && github.event.action == "opened"`,
		},
		{
			name:       "mixed quotes",
			expression: `contains(github.event.issue.body, 'single quoted || string') && contains(github.event.comment.body, "double quoted && string")`,
		},
		{
			name:       "escaped quotes",
			expression: `contains(github.event.issue.body, 'string with \\'escaped\\' quotes || and operators') && github.event.action == 'opened'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := BreakLongExpression(tt.expression)

			// Verify that quotes are preserved and no breaking happens inside quoted strings
			joined := strings.Join(lines, " ")
			originalNorm := strings.Join(strings.Fields(tt.expression), " ")
			joinedNorm := strings.Join(strings.Fields(joined), " ")

			if joinedNorm != originalNorm {
				t.Errorf("Expression with quotes not preserved correctly\nOriginal: %s\nJoined:   %s", originalNorm, joinedNorm)
			}

			// Check that no line contains half of a quoted string
			for _, line := range lines {
				singleQuotes := strings.Count(line, "'")
				doubleQuotes := strings.Count(line, `"`)

				// Count non-escaped quotes
				nonEscapedSingle := singleQuotes - strings.Count(line, `\'`)
				nonEscapedDouble := doubleQuotes - strings.Count(line, `\"`)

				if nonEscapedSingle%2 != 0 {
					t.Errorf("Line has unmatched single quotes: %s", line)
				}
				if nonEscapedDouble%2 != 0 {
					t.Errorf("Line has unmatched double quotes: %s", line)
				}
			}
		})
	}
}
