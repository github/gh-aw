//go:build !integration

package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runBotConditions(t *testing.T, input Input) []Diagnostic {
	t.Helper()
	runner, err := New(BotConditionsRule())
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	for _, diagnostic := range diagnostics {
		assert.Equal(t, "bot-conditions", diagnostic.RuleID)
		assert.Equal(t, Warning, diagnostic.Severity)
		assert.NotContains(t, diagnostic.Message, "dependabot[bot]", "diagnostics must not echo condition literals")
	}
	return diagnostics
}

func botConditionPaths(diagnostics []Diagnostic) []string {
	var paths []string
	for _, diagnostic := range diagnostics {
		paths = append(paths, diagnostic.Path)
	}
	return paths
}

func TestBotConditionsActions(t *testing.T) {
	tests := []struct {
		name, trigger, jobIf, stepIf string
		paths                        []string
	}{
		{name: "actor equals bot", trigger: "pull_request_target", jobIf: "github.actor == 'dependabot[bot]'", paths: []string{"jobs.check.if"}},
		{name: "wrapped and reversed", trigger: "workflow_run", jobIf: "${{ 'Dependabot[bot]' == github.actor }}", paths: []string{"jobs.check.if"}},
		{name: "triggering actor on step", trigger: "issue_comment", stepIf: "github.triggering_actor == 'renovate[bot]'", paths: []string{"jobs.check.steps[0].if"}},
		{name: "sender login index", trigger: "pull_request_review", jobIf: "github['event'].sender.login == 'dependabot[bot]'", paths: []string{"jobs.check.if"}},
		{name: "startsWith prefix", trigger: "pull_request_target", jobIf: "startsWith(github.actor, 'dependabot')", paths: []string{"jobs.check.if"}},
		{name: "contains any bot", trigger: "pull_request_review_comment", jobIf: "contains(github.actor, '[bot]')", paths: []string{"jobs.check.if"}},
		{name: "fromJSON allowlist", trigger: "pull_request_target", jobIf: `contains(fromJSON('["octocat","dependabot[bot]"]'), github.actor)`, paths: []string{"jobs.check.if"}},
		{name: "known bot id", trigger: "pull_request_target", jobIf: "github.actor_id == 49699333", paths: []string{"jobs.check.if"}},
		{name: "or bypasses association", trigger: "issue_comment", jobIf: "github.event.comment.author_association == 'OWNER' || github.actor == 'dependabot[bot]'", paths: []string{"jobs.check.if"}},
		{name: "machine user bot login", trigger: "issue_comment", jobIf: "github.event.comment.author_association == 'OWNER' || github.actor == 'example-bot'", paths: []string{"jobs.check.if"}},
		{name: "author check scoped to other event", trigger: "issue_comment", jobIf: "(github.event.comment.author_association == 'OWNER' || github.actor == 'example[bot]') && (github.event_name != 'pull_request' || github.event.pull_request.user.login == 'example[bot]')", paths: []string{"jobs.check.if"}},
		{name: "double negation", trigger: "pull_request_target", jobIf: "!(github.actor != 'dependabot[bot]')", paths: []string{"jobs.check.if"}},
		{name: "and with unrelated check", trigger: "pull_request_target", jobIf: "github.actor == 'dependabot[bot]' && github.repository == github.event.pull_request.head.repo.full_name", paths: []string{"jobs.check.if"}},
		{name: "author exclusion is not a guard", trigger: "pull_request_target", jobIf: "github.actor == 'dependabot[bot]' && github.event.pull_request.user.login != 'evil'", paths: []string{"jobs.check.if"}},
		{name: "negated nested author exclusion", trigger: "issue_comment", jobIf: "!(github.actor != 'dependabot[bot]' || (github.event_name == 'pull_request' && github.event.pull_request.user.login != 'dependabot[bot]'))", paths: []string{"jobs.check.if"}},
		{name: "association beside bot in conjunct", trigger: "issue_comment", jobIf: "(github.event_name != 'issue_comment' || github.event.comment.author_association == 'OWNER' || github.actor == 'github-actions[bot]') && github.event_name == 'issue_comment'", paths: []string{"jobs.check.if"}},

		{name: "unprivileged trigger", trigger: "pull_request", jobIf: "github.actor == 'dependabot[bot]'"},
		{name: "push trigger", trigger: "push", jobIf: "github.actor == 'dependabot[bot]'"},
		{name: "pull request author", trigger: "pull_request_target", jobIf: "github.event.pull_request.user.login == 'dependabot[bot]'"},
		{name: "author guarded conjunction", trigger: "pull_request_target", jobIf: "github.actor == 'dependabot[bot]' && github.event.pull_request.user.login == 'dependabot[bot]'"},
		{name: "author required in nested conjunction", trigger: "pull_request_target", jobIf: "github.actor == 'dependabot[bot]' && (github.repository == 'o/r' && github.event.pull_request.user.login == 'dependabot[bot]')"},
		{name: "negated author guard", trigger: "pull_request_target", jobIf: "!(github.actor != 'dependabot[bot]' || github.event.pull_request.user.login != 'dependabot[bot]')"},
		{name: "author association allowlist", trigger: "issue_comment", jobIf: `github.actor == 'dependabot[bot]' && contains(fromJSON('["OWNER","MEMBER"]'), github.event.comment.author_association)`},
		{name: "human login containing bot", trigger: "pull_request_target", jobIf: "github.actor == 'abbot'"},
		{name: "bot exclusion", trigger: "pull_request_target", jobIf: "github.actor != 'dependabot[bot]'"},
		{name: "negated equality", trigger: "pull_request_target", jobIf: "!(github.actor == 'dependabot[bot]')"},
		{name: "human actor", trigger: "pull_request_target", jobIf: "github.actor == 'octocat'"},
		{name: "unknown numeric id", trigger: "pull_request_target", jobIf: "github.actor_id == 12345"},
		{name: "non-boolean position", trigger: "pull_request_target", jobIf: "format('{0}', github.actor == 'dependabot[bot]') == 'false'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "on: " + test.trigger + "\njobs:\n  check:\n    runs-on: ubuntu-latest\n"
			if test.jobIf != "" {
				source += "    if: >-\n      " + test.jobIf + "\n"
			}
			source += "    steps:\n      - run: echo ok\n"
			if test.stepIf != "" {
				source += "        if: >-\n          " + test.stepIf + "\n"
			}
			workflow, err := actions.Parse([]byte(source))
			require.NoError(t, err)
			assert.Equal(t, test.paths, botConditionPaths(runBotConditions(t, Input{Workflow: workflow})))
		})
	}
}

func TestBotConditionsActorOutsideConditions(t *testing.T) {
	workflow, err := actions.Parse([]byte(`on: pull_request_target
jobs:
  check:
    runs-on: ubuntu-latest
    env:
      BOT: ${{ github.actor == 'dependabot[bot]' }}
    steps:
      - run: echo "$ACTOR"
        env:
          ACTOR: ${{ github.actor }}
`))
	require.NoError(t, err)
	assert.Empty(t, runBotConditions(t, Input{Workflow: workflow}))
}

func TestBotConditionsAuthoredDocument(t *testing.T) {
	document, err := aw.Parse(`---
on:
  slash_command:
    name: fix
if: github.actor == 'dependabot[bot]'
jobs:
  merge:
    runs-on: ubuntu-latest
    steps:
      - if: ${{ github.triggering_actor == 'renovate[bot]' }}
        run: echo merge
      - if: github.event.pull_request.user.login == 'renovate[bot]'
        run: echo safe
---
Prompt.
`)
	require.NoError(t, err)
	assert.Equal(t, []string{"if", "jobs.merge.steps[0].if"}, botConditionPaths(runBotConditions(t, Input{Document: document})))

	safe, err := aw.Parse("---\non: schedule\nif: github.actor == 'dependabot[bot]'\n---\nPrompt.\n")
	require.NoError(t, err)
	assert.Empty(t, runBotConditions(t, Input{Document: safe}))
}

func TestBotConditionsInvalidConditionAndCancellation(t *testing.T) {
	document, err := aw.Parse("---\non: pull_request_target\nif: github.actor ==\n---\nPrompt.\n")
	require.NoError(t, err)
	runner, err := New(BotConditionsRule())
	require.NoError(t, err)
	_, err = runner.Run(context.Background(), Input{Document: document})
	require.Error(t, err, "unparsable conditions must not be silently accepted")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, Input{Document: document})
	require.ErrorIs(t, err, context.Canceled)
}
