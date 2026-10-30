package lint

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func integrityTestRun(t *testing.T, rule Rule, input Input) []Diagnostic {
	t.Helper()
	runner, err := New(rule)
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	return diagnostics
}

func TestActionPinReferences(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tests := []struct {
		name     string
		uses     string
		reusable bool
		message  string
	}{
		{name: "remote SHA", uses: "actions/checkout@" + sha},
		{name: "uppercase SHA", uses: "actions/checkout@" + strings.ToUpper(sha)},
		{name: "remote action subdirectory", uses: "github/gh-aw/actions/setup@" + sha},
		{name: "local action", uses: "./.github/actions/setup"},
		{name: "local root action", uses: "./"},
		{name: "Docker digest", uses: "docker://alpine@sha256:" + strings.Repeat("b", 64)},
		{name: "tagged Docker digest", uses: "docker://ghcr.io/owner/image:v1@sha256:" + strings.Repeat("b", 64)},
		{name: "Docker registry port", uses: "docker://localhost:5000/a/image@sha512:" + strings.Repeat("b", 128)},
		{name: "Docker sha384", uses: "docker://image@sha384:" + strings.Repeat("b", 96)},
		{name: "reusable remote", uses: "owner/repo/.github/workflows/build.yml@" + sha, reusable: true},
		{name: "reusable local", uses: "./.github/workflows/build.yaml", reusable: true},
		{name: "tag", uses: "actions/checkout@v4", message: "mutable or abbreviated"},
		{name: "branch", uses: "actions/checkout@releases/v4", message: "mutable or abbreviated"},
		{name: "short SHA", uses: "actions/checkout@abc1234", message: "mutable or abbreviated"},
		{name: "reusable tag", uses: "owner/repo/.github/workflows/build.yml@v1", reusable: true, message: "mutable or abbreviated"},
		{name: "Docker tag", uses: "docker://alpine:3", message: "is mutable"},
		{name: "Docker implicit latest", uses: "docker://alpine", message: "is mutable"},
		{name: "missing ref", uses: "actions/checkout", message: "Malformed"},
		{name: "empty ref", uses: "actions/checkout@", message: "Malformed"},
		{name: "empty owner", uses: "/checkout@" + sha, message: "Malformed"},
		{name: "URL", uses: "https://github.com/actions/checkout@" + sha, message: "Malformed"},
		{name: "remote expression", uses: "actions/checkout@${{ secrets.PRIVATE_REF }}", message: "Malformed"},
		{name: "whitespace", uses: "actions/checkout@" + sha + " ", message: "Malformed"},
		{name: "local NEL", uses: "./action\u0085suffix", message: "Malformed"},
		{name: "local line separator", uses: "./action\u2028suffix", message: "Malformed"},
		{name: "local paragraph separator", uses: "./action\u2029suffix", message: "Malformed"},
		{name: "local unicode space", uses: "./action\u00a0suffix", message: "Malformed"},
		{name: "local expression", uses: "./${{ secrets.PRIVATE_REF }}", message: "Malformed"},
		{name: "quoted version comment", uses: "actions/checkout@" + sha + " # v4", message: "Malformed"},
		{name: "reference line separator", uses: "actions/checkout@branch\u2028name", message: "Malformed"},
		{name: "extra at", uses: "actions/checkout@v4@" + sha, message: "Malformed"},
		{name: "invalid git ref", uses: "actions/checkout@branch..name", message: "Malformed"},
		{name: "invalid git ref component", uses: "actions/checkout@refs/.hidden", message: "Malformed"},
		{name: "invalid git ref suffix", uses: "actions/checkout@branch.lock", message: "Malformed"},
		{name: "empty remote segment", uses: "owner//action@" + sha, message: "Malformed"},
		{name: "remote traversal", uses: "owner/repo/../action@" + sha, message: "Malformed"},
		{name: "local traversal", uses: "./../action", message: "Malformed"},
		{name: "local ref", uses: "./.github/actions/setup@v1", message: "Malformed"},
		{name: "empty Docker image", uses: "docker://@sha256:" + strings.Repeat("b", 64), message: "Malformed Docker"},
		{name: "short Docker digest", uses: "docker://alpine@sha256:abc123", message: "Malformed Docker"},
		{name: "invalid Docker hex", uses: "docker://alpine@sha256:" + strings.Repeat("z", 64), message: "Malformed Docker"},
		{name: "empty Docker tag", uses: "docker://alpine:", message: "Malformed Docker"},
		{name: "invalid Docker name", uses: "docker://alpine//other@sha256:" + strings.Repeat("b", 64), message: "Malformed Docker"},
		{name: "reusable Docker", uses: "docker://alpine@sha256:" + strings.Repeat("b", 64), reusable: true, message: "Malformed"},
		{name: "reusable action path", uses: "owner/repo/action@" + sha, reusable: true, message: "Malformed"},
		{name: "reusable nested path", uses: "./.github/workflows/sub/build.yml", reusable: true, message: "Malformed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "jobs.test.steps[0].uses"
			job := &actions.Job{ID: "test", RunsOn: "ubuntu-latest", Steps: []actions.Step{actions.ActionStep{Uses: tt.uses}}}
			source := fmt.Sprintf("jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: %s\n", strconv.Quote(tt.uses))
			if tt.reusable {
				path = "jobs.test.uses"
				job = &actions.Job{ID: "test", Uses: tt.uses}
				source = fmt.Sprintf("jobs:\n  test:\n    uses: %s\n", strconv.Quote(tt.uses))
			}
			parsed, err := actions.Parse([]byte(source))
			require.NoError(t, err)
			for _, workflow := range []*actions.Workflow{{Jobs: []*actions.Job{job}}, parsed} {
				diagnostics := integrityTestRun(t, ActionPinRule(), Input{Workflow: workflow})
				if tt.message == "" {
					assert.Empty(t, diagnostics)
					continue
				}
				require.Len(t, diagnostics, 1)
				assert.Equal(t, "action-pin", diagnostics[0].RuleID)
				severity := Warning
				if strings.HasPrefix(tt.message, "Malformed") {
					severity = Error
				}
				assert.Equal(t, severity, diagnostics[0].Severity)
				assert.Equal(t, path, diagnostics[0].Path)
				assert.Contains(t, diagnostics[0].Message, tt.message)
				assert.NotContains(t, diagnostics[0].Message, tt.uses)
				assert.NotContains(t, diagnostics[0].Message, "PRIVATE_REF")
			}
		})
	}
}

func TestActionPinTypedAndRawSteps(t *testing.T) {
	workflow := &actions.Workflow{Jobs: []*actions.Job{{
		ID: "test",
		Steps: []actions.Step{
			actions.ActionStep{Uses: "actions/checkout@v4"},
			actions.RawStep{Fields: map[string]any{"uses": "actions/checkout@v4"}},
			actions.RunStep{Run: "echo test"},
			actions.RawStep{Fields: map[string]any{"run": "echo test"}},
		},
	}}}
	diagnostics := integrityTestRun(t, ActionPinRule(), Input{Workflow: workflow})
	require.Len(t, diagnostics, 2)
	assert.Equal(t, "jobs.test.steps[0].uses", diagnostics[0].Path)
	assert.Equal(t, "jobs.test.steps[1].uses", diagnostics[1].Path)
	assert.Equal(t, diagnostics[0].Message, diagnostics[1].Message)
}

func TestActionPinGeneratedReferences(t *testing.T) {
	t.Parallel()
	document, err := aw.Parse("---\non: workflow_dispatch\n---\nTest.\n")
	require.NoError(t, err)
	const generated = "github/gh-aw-actions/setup@v0.93.5"
	const malformed = "github/gh-aw-actions/setup@"
	workflow := &actions.Workflow{Jobs: []*actions.Job{
		{ID: "test", Steps: []actions.Step{
			actions.ActionStep{Uses: generated},
			actions.RawStep{Fields: map[string]any{"uses": generated}},
			actions.ActionStep{Uses: "github/gh-aw-actions/setup@main"},
			actions.ActionStep{Uses: "example/action@v1"},
			actions.ActionStep{Uses: malformed},
		}},
		{ID: "reuse", Uses: "example/repo/.github/workflows/test.yml@v1"},
	}}
	diagnostics := integrityTestRun(t, ActionPinRule(), Input{
		Workflow: workflow, Document: document,
		GeneratedActionRefs: map[string]struct{}{
			generated: {}, malformed: {}, "example/repo/.github/workflows/test.yml@v1": {},
		},
	})
	require.Len(t, diagnostics, 6)
	severities := make(map[string]Severity)
	for _, diagnostic := range diagnostics {
		severities[diagnostic.Path] = diagnostic.Severity
	}
	assert.Equal(t, Warning, severities["jobs.test.steps[0].uses"])
	assert.Equal(t, Warning, severities["jobs.test.steps[1].uses"])
	assert.Equal(t, Error, severities["jobs.test.steps[2].uses"])
	assert.Equal(t, Error, severities["jobs.test.steps[3].uses"])
	assert.Equal(t, Error, severities["jobs.test.steps[4].uses"])
	assert.Equal(t, Error, severities["jobs.reuse.uses"])
}

func TestActionPinFormattingAcrossASTs(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, reference := range []string{
		"actions/checkout@" + sha,
		"owner/repo/.github/workflows/build.yml@" + sha,
		"./.github/actions/setup",
		"docker://alpine@sha256:" + strings.Repeat("a", 64),
	} {
		for _, quote := range []string{"", "'", `"`} {
			t.Run(reference+quote, func(t *testing.T) {
				source := "jobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: " + quote + reference + quote + " # version label\n"
				workflow, err := actions.Parse([]byte(source))
				require.NoError(t, err)
				document, err := aw.Parse("---\nstrict: false\n" + source + "---\n")
				require.NoError(t, err)
				for _, input := range []Input{{Workflow: workflow}, {Document: document}} {
					assert.Empty(t, integrityTestRun(t, ActionPinRule(), input))
				}
			})
		}
	}
}

func TestActionPinControlAndEncoding(t *testing.T) {
	for _, uses := range []string{"./action\x00", "./action\x1b", "./action\x7f", "./action\xff"} {
		workflow := &actions.Workflow{Jobs: []*actions.Job{{
			ID: "check", Steps: []actions.Step{actions.ActionStep{Uses: uses}},
		}}}
		diagnostics := integrityTestRun(t, ActionPinRule(), Input{Workflow: workflow})
		require.Len(t, diagnostics, 1)
		assert.Equal(t, Error, diagnostics[0].Severity)
		assert.Contains(t, diagnostics[0].Message, "Malformed")
	}
}

func TestActionPinStrictContext(t *testing.T) {
	workflow := &actions.Workflow{Jobs: []*actions.Job{{ID: "test", Uses: "owner/repo/.github/workflows/build.yml@main"}}}
	tests := []struct {
		name     string
		document *aw.Document
		severity Severity
	}{
		{name: "native Actions", severity: Warning},
		{name: "AW default", document: &aw.Document{}, severity: Error},
		{name: "AW strict", document: &aw.Document{Frontmatter: map[string]any{"strict": true}}, severity: Error},
		{name: "AW non-strict", document: &aw.Document{Frontmatter: map[string]any{"strict": false}}, severity: Warning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := integrityTestRun(t, ActionPinRule(), Input{Workflow: workflow, Document: tt.document})
			require.Len(t, diagnostics, 1)
			assert.Equal(t, tt.severity, diagnostics[0].Severity)
		})
	}
	for _, strict := range []any{nil, "false", 0, map[string]any{}} {
		_, err := ActionPinRule().Check(context.Background(), Input{
			Workflow: workflow,
			Document: &aw.Document{Frontmatter: map[string]any{"strict": strict}},
		})
		require.ErrorContains(t, err, "strict to be a boolean")
	}
	assert.Equal(t, Warning, ActionPinRule().Severity)
	assert.Equal(t, Error, JobTimeoutRule().Severity)
}

func TestJobTimeoutParsedValues(t *testing.T) {
	tests := []struct {
		name        string
		timeout     string
		wantFinding bool
	}{
		{name: "absent"},
		{name: "positive", timeout: "10"},
		{name: "explicit zero", timeout: "0", wantFinding: true},
		{name: "expression", timeout: "${{ fromJSON(vars.PRIVATE_TIMEOUT) }}", wantFinding: true},
		{name: "numeric expression", timeout: "${{ 10 }}", wantFinding: true},
		{name: "quoted numeric", timeout: `"10"`, wantFinding: true},
		{name: "quoted fractional", timeout: `"1.5"`, wantFinding: true},
		{name: "quoted negative", timeout: `"-1"`, wantFinding: true},
		{name: "noninteger string", timeout: `"not-a-number"`, wantFinding: true},
		{name: "quoted boolean", timeout: `"true"`, wantFinding: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeout := ""
			if tt.timeout != "" {
				timeout = "    timeout-minutes: " + tt.timeout + "\n"
			}
			workflow, err := actions.Parse([]byte("jobs:\n  test:\n    runs-on: ubuntu-latest\n" + timeout + "    steps:\n      - run: echo test\n"))
			require.NoError(t, err)
			diagnostics := integrityTestRun(t, JobTimeoutRule(), Input{Workflow: workflow})
			if !tt.wantFinding {
				assert.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "job-timeout", diagnostics[0].RuleID)
			assert.Equal(t, Error, diagnostics[0].Severity)
			assert.Equal(t, "jobs.test.timeout-minutes", diagnostics[0].Path)
			assert.NotContains(t, diagnostics[0].Message, "PRIVATE_TIMEOUT")
		})
	}
	for _, timeout := range []string{"-1", "1.5", "true", "[]", "{}"} {
		_, err := actions.Parse([]byte("jobs:\n  test:\n    runs-on: ubuntu-latest\n    timeout-minutes: " + timeout + "\n    steps:\n      - run: echo test\n"))
		require.Error(t, err, "unrepresentable invalid timeout must fail parsing, not succeed without a finding")
	}
}

func TestJobTimeoutConstructedValues(t *testing.T) {
	tests := []struct {
		name    string
		job     actions.Job
		finding bool
	}{
		{name: "absent"},
		{name: "positive", job: actions.Job{TimeoutMinutes: 5}},
		{name: "explicit positive", job: actions.Job{TimeoutMinutes: 5, TimeoutMinutesSet: true}},
		{name: "explicit zero", job: actions.Job{TimeoutMinutesSet: true}, finding: true},
		{name: "negative", job: actions.Job{TimeoutMinutes: -1}, finding: true},
		{name: "expression", job: actions.Job{TimeoutMinutesExpression: actions.Context("vars.PRIVATE_TIMEOUT")}, finding: true},
		{name: "noninteger expression", job: actions.Job{TimeoutMinutesExpression: actions.Number(1.5)}, finding: true},
		{name: "boolean", job: actions.Job{TimeoutMinutesExpression: actions.Boolean(false)}, finding: true},
		{name: "literal string", job: actions.Job{TimeoutMinutesExpression: actions.Literal("5")}, finding: true},
		{name: "conflicting values", job: actions.Job{TimeoutMinutes: 5, TimeoutMinutesExpression: actions.Context("vars.TIMEOUT")}, finding: true},
		{name: "malformed expression", job: actions.Job{TimeoutMinutesExpression: actions.Context("")}, finding: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.job.ID = "test"
			diagnostics := integrityTestRun(t, JobTimeoutRule(), Input{Workflow: &actions.Workflow{Jobs: []*actions.Job{&tt.job}}})
			if tt.finding {
				require.Len(t, diagnostics, 1)
			} else {
				assert.Empty(t, diagnostics)
			}
		})
	}
	workflow, err := actions.Parse([]byte("jobs:\n  call:\n    uses: ./.github/workflows/build.yml\n"))
	require.NoError(t, err)
	assert.Empty(t, integrityTestRun(t, JobTimeoutRule(), Input{Workflow: workflow}))
}

func TestJobTimeoutExplicitZeroRoundTrip(t *testing.T) {
	for _, timeout := range []string{"", "    timeout-minutes: 0\n", "    timeout-minutes: 5\n"} {
		source := "jobs:\n  test:\n    runs-on: ubuntu-latest\n" + timeout + "    steps:\n      - run: echo test\n"
		workflow, err := actions.Parse([]byte(source))
		require.NoError(t, err)
		assert.Equal(t, timeout != "", workflow.Jobs[0].TimeoutMinutesSet)
		emitted, err := actions.Emit(workflow)
		require.NoError(t, err)
		assert.Equal(t, source, emitted)
		workflow.Name = "edited"
		emitted, err = actions.Emit(workflow)
		require.NoError(t, err)
		roundTrip, err := actions.Parse([]byte(emitted))
		require.NoError(t, err)
		assert.Equal(t, workflow.Jobs[0].TimeoutMinutesSet, roundTrip.Jobs[0].TimeoutMinutesSet)
		assert.Equal(t, workflow.Jobs[0].TimeoutMinutes, roundTrip.Jobs[0].TimeoutMinutes)
		if timeout == "" {
			assert.NotContains(t, emitted, "timeout-minutes")
		} else {
			assert.Contains(t, emitted, strings.TrimSpace(timeout))
		}
	}
	require.Error(t, actions.ValidateJob(&actions.Job{ID: "call", Uses: "./.github/workflows/build.yml", TimeoutMinutesSet: true}))
	require.Error(t, actions.ValidateJob(&actions.Job{
		ID: "test", RunsOn: "ubuntu-latest", Steps: []actions.Step{actions.RunStep{Run: "true"}},
		TimeoutMinutesSet: true, TimeoutMinutesExpression: actions.Context("5"),
	}))
}

func TestIntegrityMalformedInputAndCancellation(t *testing.T) {
	for _, rule := range []Rule{ActionPinRule(), JobTimeoutRule()} {
		t.Run(rule.ID, func(t *testing.T) {
			for _, input := range []Input{{}, {Workflow: &actions.Workflow{Jobs: []*actions.Job{nil}}}} {
				_, err := rule.Check(context.Background(), input)
				require.Error(t, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := rule.Check(ctx, Input{Workflow: &actions.Workflow{}})
			require.ErrorIs(t, err, context.Canceled)
			ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
			defer cancel()
			_, err = rule.Check(ctx, Input{Workflow: &actions.Workflow{}})
			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
	}
	for _, step := range []actions.Step{
		nil, (*actions.ActionStep)(nil), actions.ActionStep{}, actions.RawStep{},
		actions.RawStep{Fields: map[string]any{"uses": 42}},
		actions.RawStep{Fields: map[string]any{"uses": "private-value", "run": "private-script"}},
		actions.ActionStep{Uses: "private-value", With: map[string]actions.Expression{"token": actions.Context("${{ private-expression }}")}},
	} {
		workflow := &actions.Workflow{Jobs: []*actions.Job{{
			ID: "test", Steps: []actions.Step{step, actions.ActionStep{Uses: "actions/checkout@v4"}},
		}}}
		diagnostics, err := ActionPinRule().Check(context.Background(), Input{Workflow: workflow})
		require.Error(t, err)
		require.Len(t, diagnostics, 1, "valid later steps must still be inspected")
		assert.Contains(t, err.Error(), "jobs.test.steps[0]")
		assert.NotContains(t, err.Error(), "private")
	}
}

func TestIntegrityCancellationDuringTraversal(t *testing.T) {
	for _, rule := range []Rule{ActionPinRule(), JobTimeoutRule()} {
		ctx := &integrityCancelContext{Context: context.Background(), remaining: 5}
		workflow := &actions.Workflow{Jobs: []*actions.Job{
			{ID: "first", TimeoutMinutes: -1, Steps: []actions.Step{actions.ActionStep{Uses: "actions/checkout@v4"}}},
			{ID: "second", TimeoutMinutes: -1, Steps: []actions.Step{actions.ActionStep{Uses: "actions/checkout@v4"}}},
		}}
		_, err := rule.Check(ctx, Input{Workflow: workflow})
		require.ErrorIs(t, err, context.Canceled)
	}
}

type integrityCancelContext struct {
	context.Context
	remaining int
}

func (ctx *integrityCancelContext) Err() error {
	ctx.remaining--
	if ctx.remaining <= 0 {
		return context.Canceled
	}
	return nil
}
