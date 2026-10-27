package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/require"
)

func TestCompilerLintEnabledAndOptOut(t *testing.T) {
	t.Parallel()
	content := `name: Haiku
on: workflow_dispatch
permissions:
  contents: read
jobs:
  print-message:
    runs-on: ubuntu-latest
    timeout-minutes: 1
    steps:
      - run: echo "${{ inputs.message }}"
`
	data := &WorkflowData{}
	compiler := NewCompiler(WithLint(true))
	err := compiler.lintCompiledWorkflow(data, content, "haiku.md")
	require.ErrorContains(t, err, "[template-injection]")
	require.ErrorContains(t, err, "CTR-006")
	require.NoError(t, NewCompiler(WithLint(false)).lintCompiledWorkflow(data, content, "haiku.md"))

	safe := `name: Haiku
on: workflow_dispatch
permissions:
  contents: read
jobs:
  print-message:
    runs-on: ubuntu-latest
    timeout-minutes: 1
    steps:
      - env:
          MESSAGE: ${{ inputs.message }}
        run: printf '%s\n' "$MESSAGE"
`
	require.NoError(t, compiler.lintCompiledWorkflow(data, safe, "haiku.md"))
}

func TestCompilerLintRejectsInvalidGeneratedAST(t *testing.T) {
	t.Parallel()
	require.ErrorContains(t, NewCompiler(WithLint(true)).lintCompiledWorkflow(&WorkflowData{}, "jobs: [", "test.md"), "cannot parse generated Actions AST")
}

func TestCompilerLintAuthenticatedWebhookPipeline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, payload string
		safeOutput    bool
		wantError     bool
	}{
		{"ordinary job", `"$GH_AW_AGENT_OUTPUT"`, false, true},
		{"safe-output endpoint", `"$GH_AW_AGENT_OUTPUT"`, true, false},
		{"secret payload consumed by integration", `<(printf '%s' "$WEBHOOK")`, true, false},
		{"integration still rejects logging", `"$GH_AW_AGENT_OUTPUT"; echo "$WEBHOOK"; jq -c . "$GH_AW_AGENT_OUTPUT"`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := `on: workflow_dispatch
jobs:
  report_security_finding:
    runs-on: ubuntu-latest
    steps:
      - env:
          WEBHOOK: ${{ secrets.PYDANTIC_AI_TRIAGE_SLACK_WEBHOOK_URL }}
        run: |
          jq -c '.items[] | select(.type == "report_security_finding") | {text: "\(.summary)\n\n\(.details)"}' \
            ` + tc.payload + ` | curl --fail -sS -H 'Content-Type: application/json' --data @- "$WEBHOOK"
`
			data := &WorkflowData{}
			if tc.safeOutput {
				data.SafeOutputs = &SafeOutputsConfig{Jobs: map[string]*SafeJobConfig{"report-security-finding": {}}}
			}
			err := NewCompiler(WithLint(true)).lintCompiledWorkflow(data, content, "webhook.md")
			if tc.wantError {
				require.ErrorContains(t, err, "[secret-leak]")
				require.ErrorContains(t, err, "CTR-017")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCompilerLintImportedAuthenticatedWebhook(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shared := filepath.Join(dir, "notification.md")
	source := filepath.Join(dir, "webhook.md")
	require.NoError(t, os.WriteFile(source, []byte(`---
on: workflow_dispatch
imports: [notification.md]
tools:
  github: false
---
Report findings privately.
`), 0o600))
	for _, disclose := range []bool{false, true} {
		payload := `"$GH_AW_AGENT_OUTPUT"`
		if disclose {
			payload = `"$GH_AW_AGENT_OUTPUT"; echo "$WEBHOOK"; jq -c . "$GH_AW_AGENT_OUTPUT"`
		}
		require.NoError(t, os.WriteFile(shared, []byte(`---
safe-outputs:
  jobs:
    report-security-finding:
      description: Privately report a security finding
      runs-on: ubuntu-latest
      max: 1
      permissions: {}
      steps:
        - env:
            WEBHOOK: ${{ secrets.PRIVATE_ENDPOINT }}
          run: |
            jq -c '.items[] | select(.type == "report_security_finding") | {text: "\(.summary)\n\n\(.details)"}' \
              `+payload+` | curl --fail -sS -H 'Content-Type: application/json' --data @- "$WEBHOOK"
---
Report findings privately.
`), 0o600))
		compiler := NewCompiler(WithLint(true))
		compiler.gitRoot = dir
		compiler.SetReadOnly(true)
		compiler.SetSkipValidation(true)
		err := compiler.CompileWorkflow(source)
		if !disclose {
			require.NoError(t, err)
			lock, err := os.ReadFile(filepath.Join(dir, "webhook.lock.yml"))
			require.NoError(t, err)
			require.NotContains(t, string(lock), "authenticated-endpoint")
		} else {
			require.ErrorContains(t, err, "[secret-leak]")
		}
	}
}

func TestCompilerLintImportedCredentialResolver(t *testing.T) {
	t.Parallel()
	for _, disclose := range []bool{false, true} {
		t.Run(fmt.Sprintf("disclose=%t", disclose), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			source := filepath.Join(dir, "credential.md")
			shared := filepath.Join(dir, "control.md")
			require.NoError(t, os.WriteFile(source, []byte(`---
on: workflow_dispatch
imports: [control.md]
tools:
  github: false
---
Read the target repository.
`), 0o600))
			consumer := `    - uses: actions/checkout@v6
      with:
        persist-credentials: false
        token: ${{ steps.cao_target_read_credential.outputs.token }}
`
			if disclose {
				consumer += `    - env:
        FORWARDED: ${{ steps.cao_target_read_credential.outputs.token }}
      run: echo "$FORWARDED"
`
			}
			require.NoError(t, os.WriteFile(shared, []byte(`---
steps:
    - id: cao_target_read_credential
      env:
        APP_TOKEN: ${{ secrets.APP_TOKEN }}
        PAT: ${{ secrets.READ_TOKEN }}
        WORKFLOW_TOKEN: ${{ github.token }}
      run: |
        set -euo pipefail
        token="$APP_TOKEN"
        if [ -z "$token" ]; then token="$PAT"; fi
        if [ -z "$token" ]; then token="$WORKFLOW_TOKEN"; fi
        echo "::add-mask::$token"
        echo "token=$token" >> "$GITHUB_OUTPUT"
`+consumer+`---
Shared credential resolver.
`), 0o600))
			compiler := NewCompiler(WithLint(true), WithNoEmit(true))
			compiler.gitRoot = dir
			compiler.SetReadOnly(true)
			compiler.SetSkipValidation(true)
			err := compiler.CompileWorkflow(source)
			if disclose {
				require.ErrorContains(t, err, "[secret-leak]")
				require.ErrorContains(t, err, "CTR-017")
			} else {
				require.NoError(t, err)
			}
			_, err = os.Stat(filepath.Join(dir, "credential.lock.yml"))
			require.True(t, os.IsNotExist(err), "no-emit compilation must not write a lock file")
		})
	}
}

func TestCompilerLintPreservesThreatSuppressions(t *testing.T) {
	t.Parallel()
	content := "jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ inputs.message }}'\n"
	for _, active := range []bool{false, true} {
		expires := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
		if active {
			expires = time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
		}
		data := &WorkflowData{ParsedFrontmatter: &FrontmatterConfig{
			ThreatDetectionSuppressions: []ThreatDetectionSuppression{{
				Rule: "CTR-006", Reason: "Test suppression", Expires: expires,
			}},
		}}
		err := NewCompiler(WithLint(true)).lintCompiledWorkflow(data, content, "test.md")
		if active {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "[template-injection]")
		}
	}
}

func TestCompilerLintWarningsRemainAdvisory(t *testing.T) {
	t.Parallel()
	content := "jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action@main\n"
	compiler := NewCompiler(WithLint(true))
	require.NoError(t, compiler.lintCompiledWorkflow(&WorkflowData{}, content, "test.md"))
	require.Positive(t, compiler.GetWarningCount())
}

func TestCompilerLintGeneratedPRCheckoutSteps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pr-checkout.md")
	source := `---
on:
  workflow_dispatch:
  admission: true
engine: copilot
permissions:
  contents: read
checkout:
  fetch: ["*"]
  fetch-depth: 0
tools:
  github: false
safe-outputs:
  create-pull-request:
  push-to-pull-request-branch:
---
Maintain the repository.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	compiler := NewCompiler(WithLint(true))
	compiler.gitRoot = dir
	compiler.SetReadOnly(true)
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(path))
	require.Zero(t, compiler.GetWarningCount())
	lock, err := os.ReadFile(filepath.Join(dir, "pr-checkout.lock.yml"))
	require.NoError(t, err)
	require.Contains(t, string(lock), "persist-credentials: true")
	require.Contains(t, string(lock), "Configure Git credentials")
	require.NotEmpty(t, compiler.prCheckoutLintBlock)
	require.NoError(t, os.WriteFile(path, []byte("---\non: workflow_dispatch\ntools:\n  github: false\n---\nNo PR checkout.\n"), 0o600))
	require.NoError(t, compiler.CompileWorkflow(path))
	require.Empty(t, compiler.prCheckoutLintBlock, "compiler reuse must not preserve a previous workflow's exception")
}

func TestCompilerLintGeneratedPRCheckoutTransformations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, checkout, safeOutputsRunsOn, wantLockContent string
	}{
		{
			name: "cross-repository checkout",
			checkout: `checkout:
  - repository: octo-org/target-repository
    path: target
`,
			wantLockContent: "repository: octo-org/target-repository",
		},
		{
			name:              "Windows runner",
			safeOutputsRunsOn: "  runs-on: windows-latest\n",
			wantLockContent:   "shell: bash",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "pr-checkout.md")
			source := `---
on:
  workflow_dispatch:
  admission: true
engine: copilot
permissions:
  contents: read
` + tc.checkout + `tools:
  github: false
safe-outputs:
` + tc.safeOutputsRunsOn + `  create-pull-request:
  push-to-pull-request-branch:
---
Maintain the repository.
`
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			compiler := NewCompiler(WithLint(true))
			compiler.gitRoot = dir
			compiler.SetReadOnly(true)
			compiler.SetSkipValidation(true)
			require.NoError(t, compiler.CompileWorkflow(path))
			require.Zero(t, compiler.GetWarningCount())
			lock, err := os.ReadFile(filepath.Join(dir, "pr-checkout.lock.yml"))
			require.NoError(t, err)
			require.Contains(t, string(lock), tc.wantLockContent)
		})
	}
}

func TestCompilerLintUsesEffectiveStrictness(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"authored", "cli", "repository"} {
		t.Run(policy, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			document, err := aw.Parse("---\nstrict: false\non:\n  workflow_run:\n    workflows: [Build]\n    branches: ['*']\n---\nTest.\n")
			require.NoError(t, err)
			original, err := aw.Emit(document)
			require.NoError(t, err)
			compiler := NewCompiler(WithLint(true))
			compiler.gitRoot = dir
			if policy == "cli" {
				compiler.SetStrictMode(true)
			}
			if policy == "repository" {
				configDir := filepath.Join(dir, ".github", "workflows")
				require.NoError(t, os.MkdirAll(configDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(configDir, "aw.json"), []byte(`{"strict":true}`), 0o600))
			}
			data := &WorkflowData{SourceDocument: document, RawFrontmatter: document.FrontmatterResult().Frontmatter}
			err = compiler.lintCompiledWorkflow(data, "on:\n  workflow_run:\n    workflows: [Build]\n    branches: ['*']\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n", "test.md")
			if policy == "authored" {
				require.NoError(t, err)
				require.Positive(t, compiler.GetWarningCount())
			} else {
				require.ErrorContains(t, err, "[workflow-run-scope]")
			}
			after, err := aw.Emit(document)
			require.NoError(t, err)
			require.Equal(t, original, after)
			require.Equal(t, false, data.RawFrontmatter["strict"])
		})
	}
}

func TestCompilerLintAllowsOnlyWellFormedMutableActionRefs(t *testing.T) {
	t.Parallel()
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow=%t", allow), func(t *testing.T) {
			t.Parallel()
			document, err := aw.Parse("---\non: workflow_dispatch\n---\nTest.\n")
			require.NoError(t, err)
			data := &WorkflowData{SourceDocument: document}
			compiler := NewCompiler(WithLint(true))
			compiler.SetAllowActionRefs(allow)
			err = compiler.lintCompiledWorkflow(data, "on: workflow_dispatch\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action@v1\n", "test.md")
			if allow {
				require.NoError(t, err)
				require.Positive(t, compiler.GetWarningCount())
			} else {
				require.ErrorContains(t, err, "[action-pin]")
			}
			require.ErrorContains(t, compiler.lintCompiledWorkflow(data,
				"on: workflow_dispatch\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action\n", "test.md"), "Malformed action")
		})
	}
}

func TestCompilerLintUnavailableJavaScriptDoesNotCountAsWarning(t *testing.T) {
	t.Parallel()
	document, err := aw.Parse("---\non: workflow_dispatch\ngraders:\n  quality:\n    script: 'return 1;'\n---\nTest.\n")
	require.NoError(t, err)
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%t", dryRun), func(t *testing.T) {
			t.Parallel()
			compiler := NewCompiler(WithLint(true))
			compiler.SetDryRun(dryRun)
			require.NoError(t, compiler.lintCompiledWorkflow(&WorkflowData{SourceDocument: document},
				"on: workflow_dispatch\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n", "test.md"))
			require.Zero(t, compiler.GetWarningCount())
		})
	}
}

func TestCompilerLintSupportedWorkflowShapes(t *testing.T) {
	t.Parallel()
	for _, shape := range []struct {
		name, frontmatter string
	}{
		{"custom-grader", "graders:\n  quality:\n    script: 'return 1;'\n"},
		{"sandbox-awf", "sandbox: awf\n"},
		{"sandbox-default", "sandbox: default\n"},
		{"object-imports", "imports: {aw: [shared.md]}\n"},
		{"empty-object-imports", "imports: {}\n"},
		{"empty-aw-imports", "imports: {aw: []}\n"},
	} {
		for _, mode := range []string{"normal", "no-emit", "dry-run"} {
			t.Run(shape.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\ntools:\n  bash: [echo]\n---\nShared instructions.\n"), 0o600))
				path := filepath.Join(dir, "test.md")
				source := "---\non: workflow_dispatch\nengine: copilot\npermissions:\n  contents: read\ntools:\n  github: false\n" + shape.frontmatter + "---\nTest supported source shapes.\n"
				require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
				compiler := NewCompiler(WithLint(true), WithNoEmit(mode == "no-emit"))
				compiler.gitRoot = dir
				compiler.SetReadOnly(true)
				compiler.SetSkipValidation(true)
				compiler.SetDryRun(mode == "dry-run")
				require.NoError(t, compiler.CompileWorkflow(path))
				lock := filepath.Join(dir, "test.lock.yml")
				if mode == "no-emit" {
					require.NoFileExists(t, lock)
				} else {
					require.FileExists(t, lock)
				}
			})
		}
	}
}

func TestCompilerLintRunsBeforeEmission(t *testing.T) {
	t.Parallel()
	for _, noEmit := range []bool{false, true} {
		for _, skipLint := range []bool{false, true} {
			t.Run(fmt.Sprintf("noEmit=%t/skipLint=%t", noEmit, skipLint), func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := filepath.Join(dir, "test.md")
				require.NoError(t, os.WriteFile(path, []byte(`---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
jobs:
  print-message:
    runs-on: ubuntu-latest
    timeout-minutes: 1
    steps:
      - run: echo "${{ secrets.TEST_SECRET }}"
---
Test.
`), 0o600))
				compiler := NewCompiler(WithNoEmit(noEmit), WithLint(!skipLint))
				compiler.SetReadOnly(true)
				compiler.SetSkipValidation(true)
				compiler.SetStrictMode(false)
				data, err := compiler.ParseWorkflowFile(path)
				require.NoError(t, err)
				err = compiler.CompileWorkflowData(data, path)
				if !skipLint {
					require.ErrorContains(t, err, "[secret-leak]")
					require.NoFileExists(t, filepath.Join(dir, "test.lock.yml"))
				} else {
					require.NoError(t, err)
					if noEmit {
						require.NoFileExists(t, filepath.Join(dir, "test.lock.yml"))
					} else {
						require.FileExists(t, filepath.Join(dir, "test.lock.yml"))
					}
				}
			})
		}
	}
}
