package lint

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintSuppressionComments(t *testing.T) {
	const unsafe = `echo '${{ github.event.comment.body }}'`
	const ignore = "# gh-aw: ignore[template-injection] -- Reviewed false positive"
	for _, tt := range []struct {
		name, steps string
		count       int
		invalid     bool
	}{
		{"unsuppressed", "      - run: " + unsafe + "\n", 1, false},
		{"following field", "      - name: Reviewed\n        " + ignore + "\n        run: " + unsafe + "\n", 0, false},
		{"first item field", "      " + ignore + "\n      - run: " + unsafe + "\n", 0, false},
		{"other comment", "      " + ignore + "\n      # Explanation\n      - run: " + unsafe + "\n", 0, false},
		{"sibling step", "      " + ignore + "\n      - run: " + unsafe + "\n      - run: " + unsafe + "\n", 1, false},
		{"sibling field", "      - name: Reviewed\n        " + ignore + "\n        env: {BODY: public}\n        run: " + unsafe + "\n", 1, false},
		{"multiple rules", "      # gh-aw: ignore[template-injection, secret-leak] -- Reviewed\n      - run: " + unsafe + `; echo '${{ secrets.FIXTURE }}'` + "\n", 0, false},
		{"other rule survives", "      " + ignore + "\n      - run: " + unsafe + `; echo '${{ secrets.FIXTURE }}'` + "\n", 1, false},
		{"block scalar", "      - run: |\n          " + ignore + "\n          " + unsafe + "\n", 1, false},
		{"quoted text", "      - name: '" + ignore + "'\n        run: " + unsafe + "\n", 1, false},
		{"ordinary gh-aw comment", "      # gh-aw: daily triage\n      - run: " + unsafe + "\n", 1, false},
		{"ordinary gh-aw inline comment", "      - run: " + unsafe + " # gh-aw: daily triage\n", 1, false},
		{"malformed ignore syntax", "      # gh-aw: ignore template-injection -- Reviewed\n      - run: " + unsafe + "\n", 1, true},
		{"unknown rule", "      # gh-aw: ignore[typo-rule] -- Reviewed\n      - run: " + unsafe + "\n", 1, true},
		{"missing reason", "      # gh-aw: ignore[template-injection]\n      - run: " + unsafe + "\n", 1, true},
		{"blank reason", "      # gh-aw: ignore[template-injection] --   \n      - run: " + unsafe + "\n", 1, true},
		{"empty list", "      # gh-aw: ignore[] -- Reviewed\n      - run: " + unsafe + "\n", 1, true},
		{"wildcard", "      # gh-aw: ignore[*] -- Reviewed\n      - run: " + unsafe + "\n", 1, true},
		{"trailing comma", "      # gh-aw: ignore[template-injection,] -- Reviewed\n      - run: " + unsafe + "\n", 1, true},
		{"inline comment", "      - run: " + unsafe + " " + ignore + "\n", 1, true},
		{"blank line", "      " + ignore + "\n\n      - run: " + unsafe + "\n", 1, true},
		{"wrong indent", "    " + ignore + "\n      - run: " + unsafe + "\n", 1, true},
		{"trailing directive", "      - run: " + unsafe + "\n      " + ignore + "\n", 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, target := range []ASTTargets{ActionsAST, AWAST} {
				input := ruleContractInput(t, contractJob+"    steps:\n"+tt.steps, target, false)
				runner, err := Default().Select("template-injection", "secret-leak")
				require.NoError(t, err)
				diagnostics, err := runner.Run(context.Background(), input)
				if tt.invalid {
					require.ErrorContains(t, err, "invalid lint suppression")
					assert.NotContains(t, err.Error(), "Reviewed")
				} else {
					require.NoError(t, err)
				}
				diagnostics = slices.DeleteFunc(diagnostics, func(diagnostic Diagnostic) bool { return diagnostic.Severity != Error })
				assert.Len(t, diagnostics, tt.count, "%s", target)
			}
		})
	}
}

func TestLintSuppressionMappingScope(t *testing.T) {
	const source = contractJob + "    # gh-aw: ignore[template-injection] -- Reviewed job steps\n    steps:\n" +
		"      - run: echo '${{ github.event.comment.body }}'\n" +
		"      - run: echo '${{ github.event.issue.title }}'\n"
	for _, newline := range []string{"\n", "\r\n"} {
		for _, target := range []ASTTargets{ActionsAST, AWAST} {
			runner, err := Default().Select("template-injection")
			require.NoError(t, err)
			input := ruleContractInput(t, strings.ReplaceAll(source, "\n", newline), target, false)
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			assert.Empty(t, diagnostics)
		}
	}
}

func TestLintSuppressionDottedSibling(t *testing.T) {
	const source = contractJob + "    services:\n" +
		"      # gh-aw: ignore[unpinned-images] -- Reviewed service\n" +
		"      db: {image: 'node:latest'}\n" +
		"      'db.other': {image: 'node:latest'}\n" + contractSafeSteps
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		runner, err := Default().Select("unpinned-images")
		require.NoError(t, err)
		diagnostics, err := runner.Run(context.Background(), ruleContractInput(t, source, target, false))
		require.NoError(t, err)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, "jobs.check.services.db.other.image", diagnostics[0].Path)
	}
}

func TestLintSuppressionDoesNotHideAnalysisErrors(t *testing.T) {
	const source = contractJob + "    steps:\n      # gh-aw: ignore[test-failure] -- Reviewed\n      - run: echo public\n"
	rule := Rule{ID: "test-failure", Description: "test", Severity: Error, Check: func(context.Context, Input) ([]Diagnostic, error) {
		return []Diagnostic{{Path: "jobs.check.steps[0].run", Message: "finding"}}, errors.New("analysis failed")
	}}
	runner, err := New(rule, TemplateInjectionRule())
	require.NoError(t, err)
	runner, err = runner.Select("template-injection")
	require.NoError(t, err)
	_, err = runner.Run(context.Background(), ruleContractInput(t, source, ActionsAST, false))
	require.NoError(t, err, "registered but unselected extension IDs remain valid")
	runner, err = New(rule)
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), ruleContractInput(t, source, ActionsAST, false))
	require.ErrorContains(t, err, "analysis failed")
	assert.Empty(t, diagnostics)
}

func TestLintSuppressionReflectsGraphEdits(t *testing.T) {
	const source = contractJob + "    steps:\n      # gh-aw: ignore[template-injection] -- Reviewed\n      - run: echo '${{ github.event.comment.body }}'\n"
	input := ruleContractInput(t, source, ActionsAST, false)
	input.Workflow.Jobs[0].Steps = append([]actions.Step{actions.RawStep{Fields: map[string]any{
		"run": "echo '${{ github.event.issue.title }}'",
	}}}, input.Workflow.Jobs[0].Steps...)
	runner, err := Default().Select("template-injection")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.True(t, strings.HasPrefix(diagnostics[0].Path, "jobs.check.steps[0]"), "suppression follows its original field, not a stale index")
}

func TestLintSuppressionAliases(t *testing.T) {
	for _, aliasSuppressed := range []bool{false, true} {
		source := contractJob + "    container: &base\n" +
			"      # gh-aw: ignore[unpinned-images] -- Reviewed anchor\n" +
			"      image: node:latest\n" + contractSafeSteps +
			"  other:\n    runs-on: ubuntu-latest\n"
		if aliasSuppressed {
			source += "    # gh-aw: ignore[unpinned-images] -- Reviewed alias\n"
		}

		source += "    container: *base\n" + contractSafeSteps
		for _, target := range []ASTTargets{ActionsAST, AWAST} {
			runner, err := Default().Select("unpinned-images")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), ruleContractInput(t, source, target, false))
			require.NoError(t, err)
			assert.Equal(t, !aliasSuppressed, len(diagnostics) > 0, "anchor directives must not carry over to aliases")
		}
	}
}

func TestLintSuppressionCompanionOwnership(t *testing.T) {
	const source = contractJob + "    steps:\n      # gh-aw: ignore[template-injection] -- Authored source only\n" +
		"      - run: echo '${{ github.event.comment.body }}'\n"
	input := ruleContractInput(t, source, AWAST, false)
	generated := strings.ReplaceAll(source, "      # gh-aw: ignore[template-injection] -- Authored source only\n", "")
	graph, err := actions.Parse([]byte(generated))
	require.NoError(t, err)
	input.Workflow = graph
	runner, err := Default().Select("template-injection")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, diagnostics, 1, "an authored path must not suppress a generated field that happens to have the same path")
}

func TestLintSuppressionTypedRun(t *testing.T) {
	const unsafe = "echo '${{ github.event.comment.body }}'"
	for _, tt := range []struct {
		name, marker string
		suppressed   bool
		invalid      bool
	}{
		{"ordinary string", "", false, false},
		{"ordinary comment", "        # gh-aw: daily triage\n", false, false},
		{"directive", "        # gh-aw: ignore[template-injection] -- Reviewed\n", true, false},
		{"malformed directive", "        # gh-aw: ignore[template-injection]\n", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := ruleContractInput(t, "name: 'gh-aw: fixture'\n"+contractJob+"    steps:\n      - id: reviewed\n"+tt.marker+
				"        run: "+unsafe+"\n", ActionsAST, false)
			run := actions.RunStep{Run: unsafe, StepOptions: actions.StepOptions{
				ID: "reviewed", Outputs: []actions.OutputDecl{{Name: "result"}},
			}}
			input.Workflow.Jobs[0].Steps[0] = run
			input.Workflow.Jobs[0].Steps = append(input.Workflow.Jobs[0].Steps, actions.RunStep{
				Run: unsafe, StepOptions: actions.StepOptions{Env: map[string]actions.Expression{
					"RESULT": actions.OutputRef{Job: "check", Step: "reviewed", Name: "result"},
				}},
			})
			runner, err := Default().Select("template-injection")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			if tt.invalid {
				require.ErrorContains(t, err, "invalid lint suppression")
			} else {
				require.NoError(t, err)
			}
			diagnostics = slices.DeleteFunc(diagnostics, func(diagnostic Diagnostic) bool { return diagnostic.Severity != Error })
			want := 2
			if tt.suppressed {
				want = 1
				require.Len(t, diagnostics, want)
				assert.Equal(t, "jobs.check.steps[1].run", diagnostics[0].Path)
			}
			assert.Len(t, diagnostics, want)
			assert.Equal(t, run, input.Workflow.Jobs[0].Steps[0], "lint must preserve typed steps and declarations")
			_, err = actions.Emit(input.Workflow)
			require.ErrorContains(t, err, "typed run steps")
		})
	}
}

func TestLintSuppressionAliasDottedSibling(t *testing.T) {
	const source = contractJob + "    container: &base\n      image: node:latest\n" +
		"    services:\n      # gh-aw: ignore[unpinned-images] -- Reviewed alias\n" +
		"      db: *base\n      'db.other': {image: node:latest}\n" + contractSafeSteps
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		runner, err := Default().Select("unpinned-images")
		require.NoError(t, err)
		diagnostics, err := runner.Run(context.Background(), ruleContractInput(t, source, target, false))
		require.NoError(t, err)
		require.Len(t, diagnostics, 2)
		assert.Equal(t, "jobs.check.container.image", diagnostics[0].Path)
		assert.Equal(t, "jobs.check.services.db.other.image", diagnostics[1].Path)
	}
}

func TestLintSuppressionMergedAlias(t *testing.T) {
	const source = contractJob + "    container: &image {image: node:latest}\n" +
		"    services:\n      seed: &base\n        <<: *image\n        env: {PUBLIC: fixture}\n" +
		"      # gh-aw: ignore[unpinned-images] -- Reviewed merged alias\n" +
		"      db: *base\n      'db.other': {image: node:latest}\n" + contractSafeSteps
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		runner, err := Default().Select("unpinned-images")
		require.NoError(t, err)
		diagnostics, err := runner.Run(context.Background(), ruleContractInput(t, source, target, false))
		require.NoError(t, err)
		require.Len(t, diagnostics, 3)
		assert.Equal(t, "jobs.check.container.image", diagnostics[0].Path)
		assert.Equal(t, "jobs.check.services.db.other.image", diagnostics[1].Path)
		assert.Equal(t, "jobs.check.services.seed.image", diagnostics[2].Path)
	}
}

func TestLintSuppressionMixedSourceImages(t *testing.T) {
	const source = "---\nmcp-servers:\n  fixture:\n" +
		"    # gh-aw: ignore[image-argument] -- Reviewed MCP image\n    container: '-fixture'\n" +
		contractJob + "    # gh-aw: ignore[image-argument] -- Authored job only\n    container: '-fixture'\n" +
		contractSafeSteps + "---\nPrompt.\n"
	document, err := aw.Parse(source)
	require.NoError(t, err)
	graph, err := actions.Parse([]byte(contractJob + "    container: '-fixture'\n" + contractSafeSteps))
	require.NoError(t, err)
	runner, err := Default().Select("image-argument")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Workflow: graph, Document: document})
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "jobs.check.container", diagnostics[0].Path)
	assert.Equal(t, ActionsAST, diagnostics[0].Source)
}

func TestLintSuppressionExplicitFindingOwnership(t *testing.T) {
	const source = contractJob + "    steps:\n      # gh-aw: ignore[test-mixed] -- Authored field only\n" +
		"      - run: echo public\n"
	input := ruleContractInput(t, source, AWAST, false)
	graph, err := actions.Parse([]byte(contractJob + "    steps:\n      - run: echo public\n"))
	require.NoError(t, err)
	input.Workflow = graph
	rule := Rule{
		ID: "test-mixed", Description: "test mixed source ownership", Severity: Error, Targets: ActionsAST | AWAST,
		Check: func(context.Context, Input) ([]Diagnostic, error) {
			return []Diagnostic{
				{Source: AWAST, Path: "jobs.check.steps[0].run", Message: "authored"},
				{Source: ActionsAST, Path: "jobs.check.steps[0].run", Message: "generated"},
			}, nil
		},
	}
	runner, err := New(rule)
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "generated", diagnostics[0].Message)
}
