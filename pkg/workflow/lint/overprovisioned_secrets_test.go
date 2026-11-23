package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runOverprovisionedSecrets(t *testing.T, input Input) []Diagnostic {
	t.Helper()
	runner, err := Default().Select("overprovisioned-secrets")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	return diagnostics
}

func TestOverprovisionedSecretsActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   map[string]Severity
	}{
		{
			name:   "remote reusable workflow inherits secrets",
			source: "jobs:\n  call:\n    uses: octo/shared/.github/workflows/deploy.yml@0123456789abcdef0123456789abcdef01234567\n    secrets: inherit\n",
			want:   map[string]Severity{"jobs.call.secrets": Error},
		},
		{
			name:   "local reusable workflow inherits secrets",
			source: "jobs:\n  call:\n    uses: ./.github/workflows/deploy.yml\n    secrets: inherit\n",
			want:   map[string]Severity{"jobs.call.secrets": Warning},
		},
		{
			name:   "explicit named secrets are safe",
			source: "jobs:\n  call:\n    uses: octo/shared/.github/workflows/deploy.yml@0123456789abcdef0123456789abcdef01234567\n    secrets:\n      token: ${{ secrets.DEPLOY_TOKEN }}\n",
		},
		{
			name:   "toJSON of secrets passed to reusable workflow",
			source: "jobs:\n  call:\n    uses: ./.github/workflows/deploy.yml\n    secrets:\n      all: ${{ toJSON(secrets) }}\n",
			want:   map[string]Severity{"jobs.call.secrets.all": Error},
		},
		{
			name:   "dynamic index and whole context in run",
			source: contractJob + "    steps:\n      - with: {value: '${{ " + "secrets" + "[inputs.name] }}'}\n        uses: actions/github-script@v8\n      - run: |\n          echo start\n          echo '${{ toJson(" + "secrets" + ") }}' | jq length\n",
			want:   map[string]Severity{"jobs.check.steps[0].with.value": Error, "jobs.check.steps[1].run": Error},
		},
		{
			name:   "literal index and dereference are safe",
			source: contractJob + "    env:\n      A: ${{ secrets['DEPLOY_TOKEN'] }}\n      B: ${{ secrets.DEPLOY_TOKEN || github.token }}\n      C: ${{ format('{0}', secrets.OTHER) }}\n" + contractSafeSteps,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			workflow, err := actions.Parse([]byte(tt.source))
			require.NoError(t, err)
			diagnostics := runOverprovisionedSecrets(t, Input{Workflow: workflow})
			got := make(map[string]Severity, len(diagnostics))
			for _, diagnostic := range diagnostics {
				got[diagnostic.Path] = diagnostic.Severity
				assert.NotContains(t, diagnostic.Message, "DEPLOY_TOKEN")
				if diagnostic.Path == "jobs.check.steps[1].run" {
					assert.Equal(t, 2, diagnostic.Line)
				}
			}
			if tt.want == nil {
				tt.want = map[string]Severity{}
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOverprovisionedSecretsIgnoresCompilerGeneratedInherit(t *testing.T) {
	t.Parallel()
	source := "jobs:\n  authored:\n    uses: octo/shared/.github/workflows/a.yml@0123456789abcdef0123456789abcdef01234567\n    secrets: inherit\n" +
		"  call_worker:\n    uses: ./.github/workflows/worker.lock.yml\n    secrets: inherit\n"
	workflow, err := actions.Parse([]byte(source))
	require.NoError(t, err)

	diagnostics := runOverprovisionedSecrets(t, Input{Workflow: workflow, AuthoredJobs: map[string]any{"authored": map[string]any{}}})
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "jobs.authored.secrets", diagnostics[0].Path)

	document, err := aw.Parse("---\non: issues\n---\nPrompt.\n")
	require.NoError(t, err)
	assert.Empty(t, runOverprovisionedSecrets(t, Input{Workflow: workflow, Document: document}),
		"jobs absent from the authored document are compiler-generated")
}

func TestOverprovisionedSecretsAuthored(t *testing.T) {
	t.Parallel()
	document, err := aw.Parse("---\non: issues\njobs:\n  remote:\n    uses: octo/shared/.github/workflows/a.yml@main\n    secrets: inherit\n" +
		"  local:\n    uses: ./.github/workflows/b.yml\n    secrets: inherit\n" +
		"  named:\n    uses: octo/shared/.github/workflows/c.yml@main\n    secrets: {token: '${{ secrets.TOKEN }}'}\n" +
		"steps:\n  - env: {ALL: '${{ toJSON(secrets) }}'}\n    run: ./setup.sh\n---\n" +
		"Summarize ${{ github.event.issue.number }}.\n")
	require.NoError(t, err)
	diagnostics := runOverprovisionedSecrets(t, Input{Document: document})
	got := make(map[string]Severity, len(diagnostics))
	for _, diagnostic := range diagnostics {
		got[diagnostic.Path] = diagnostic.Severity
	}
	assert.Equal(t, map[string]Severity{
		"jobs.remote.secrets": Error,
		"jobs.local.secrets":  Warning,
		"steps[0].env.ALL":    Error,
	}, got)
}

func TestOverprovisionedSecretsCancellation(t *testing.T) {
	t.Parallel()
	workflow, err := actions.Parse([]byte(contractJob + contractSafeSteps))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = checkOverprovisionedSecrets(ctx, Input{Workflow: workflow})
	require.ErrorIs(t, err, context.Canceled)
}
