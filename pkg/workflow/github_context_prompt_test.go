//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestGitHubContextPrompt_UsesAwContextFallbacks(t *testing.T) {
	assertContains := func(expected string) {
		t.Helper()
		if !strings.Contains(githubContextPromptText, expected) {
			t.Fatalf("expected github context prompt to contain %q", expected)
		}
	}

	assertContains("github.event.issue.number || (github.aw.context.item_type == 'issue' && github.aw.context.item_number)")
	assertContains("github.event.discussion.number || (github.aw.context.item_type == 'discussion' && github.aw.context.item_number)")
	assertContains("github.event.pull_request.number || (github.aw.context.item_type == 'pull_request' && github.aw.context.item_number)")
	assertContains("github.event.comment.id || github.aw.context.comment_id")
}

func TestGitHubContextPrompt_CompilesConditionalValues(t *testing.T) {
	extractor := NewExpressionExtractor()
	if _, err := extractor.ExtractExpressions(githubContextPromptText); err != nil {
		t.Fatal(err)
	}
	compiled := extractor.ReplaceExpressionsWithEnvVars(githubContextPromptText)
	conditions := 0
	for line := range strings.SplitSeq(compiled, "\n") {
		if strings.HasPrefix(line, "{{#if ") {
			conditions++
			if !strings.HasPrefix(line, "{{#if __GH_AW_") {
				t.Errorf("condition must use an evaluated expression, got %q", line)
			}
		}
	}
	if conditions != 8 {
		t.Errorf("expected 8 context conditions, got %d", conditions)
	}
}

func TestGitHubContextPrompt_PassesValuesToActionsRenderer(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		Name:        "context",
		WorkflowID:  "context",
		ParsedTools: NewTools(map[string]any{"github": map[string]any{}}),
	}
	var output strings.Builder
	if err := compiler.generatePrompt(&output, data, false, nil); err != nil {
		t.Fatal(err)
	}
	_, rendering, found := strings.Cut(output.String(), "- name: Interpolate variables and render templates\n")
	if !found {
		t.Fatal("missing Actions template rendering step")
	}
	rendering, _, _ = strings.Cut(rendering, "- name: Substitute placeholders\n")
	extractor := NewExpressionExtractor()
	mappings, err := extractor.ExtractExpressions(githubContextPromptText)
	if err != nil {
		t.Fatal(err)
	}
	for _, mapping := range mappings {
		expected := mapping.EnvVar + ": " + mapping.environmentValue()
		if !strings.Contains(rendering, expected) {
			t.Errorf("Actions renderer is missing context value %q", expected)
		}
	}
}
