//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecisionCompileSecurityWithoutStrictOrLint(t *testing.T) {
	dir := testutil.TempDir(t, "decision-security-pipeline")
	file := filepath.Join(dir, "decision.md")
	source := `---
strict: false
on:
  workflow_dispatch:
  roles: all
  needs: [upstream]
  decision:
    questions:
      - "PRIVATE_QUESTION_0 ${{ secrets.PRIVATE_DIRECT }}"
      - "PRIVATE_QUESTION_1 ${{ env.PRIVATE_ALIAS }}"
      - "PRIVATE_QUESTION_2 ${{ steps.collect.outputs.value }}"
      - "PRIVATE_QUESTION_3 ${{ needs.upstream.outputs.value }}"
      - "PRIVATE_QUESTION_4 ${{ toJSON(secrets) }}"
      - "PRIVATE_QUESTION_5 ${{ contains(format('{0}', secrets.PRIVATE_NESTED), 'x') }}"
      - "PRIVATE_QUESTION_6 ${{ github.token }}"
      - "PRIVATE_QUESTION_7 ${{ toJSON(github) }}"
  steps:
    - id: collect
      run: echo "value=${{ secrets.PRIVATE_STEP }}" >> "$GITHUB_OUTPUT"
env:
  PRIVATE_ALIAS: ${{ secrets.PRIVATE_ENV }}
jobs:
  upstream:
    runs-on: ubuntu-latest
    outputs:
      value: ${{ steps.collect.outputs.value }}
    steps:
      - id: collect
        run: echo "value=${{ secrets.PRIVATE_NEEDS }}" >> "$GITHUB_OUTPUT"
engine: copilot
---
Perform the requested work.
`
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	err := NewCompiler(WithLint(false)).CompileWorkflow(file)
	require.Error(t, err)
	message := err.Error()
	for i := range 8 {
		location := regexp.QuoteMeta(file) + fmt.Sprintf(`:%d:[1-9][0-9]*: on\.decision\.questions\[%d\]:`, 9+i, i)
		assert.Regexp(t, location, message)
		assert.NotContains(t, message, fmt.Sprintf("PRIVATE_QUESTION_%d", i))
	}
	for _, private := range []string{"PRIVATE_DIRECT", "PRIVATE_ALIAS", "PRIVATE_STEP", "PRIVATE_ENV", "PRIVATE_NEEDS", "PRIVATE_NESTED"} {
		assert.NotContains(t, message, private)
	}
	assert.NotContains(t, message, "${{")
	_, statErr := os.Stat(filepath.Join(dir, "decision.lock.yml"))
	require.True(t, os.IsNotExist(statErr), "unsafe decision must not create a lock file")
}

func TestDecisionCompileSafeAliasesWithoutLint(t *testing.T) {
	dir := testutil.TempDir(t, "decision-safe-pipeline")
	file := filepath.Join(dir, "decision.md")
	source := `---
strict: false
on:
  workflow_dispatch:
  roles: all
  needs: [upstream]
  decision:
    questions:
      - "Is ${{ env.SAFE_ALIAS }} actionable?"
      - "Should ${{ steps.collect.outputs.value }} proceed?"
      - "Does ${{ needs.upstream.outputs.value }} need action?"
  steps:
    - id: collect
      run: echo "value=local" >> "$GITHUB_OUTPUT"
env:
  SAFE_ALIAS: ${{ github.event.issue.title }}
jobs:
  upstream:
    runs-on: ubuntu-latest
    outputs:
      value: ${{ steps.collect.outputs.value }}
    steps:
      - id: collect
        run: echo "value=upstream" >> "$GITHUB_OUTPUT"
engine: copilot
---
Perform the requested work.
`
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	require.NoError(t, NewCompiler(WithLint(false)).CompileWorkflow(file))
}

func TestDecisionCompileRejectsEventControlledProviderEndpoint(t *testing.T) {
	dir := testutil.TempDir(t, "decision-event-endpoint")
	file := filepath.Join(dir, "decision.md")
	source := `---
strict: false
on:
  workflow_dispatch:
  roles: all
  decision:
    model: openai/gpt-5.4
    questions:
      - Should this run?
engine:
  id: copilot
  env:
    OPENAI_BASE_URL: ${{ github.event.issue.title }}
---
Perform the requested work.
`
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	err := NewCompiler(WithLint(false)).CompileWorkflow(file)
	require.ErrorContains(t, err, "event-controlled endpoint")
	_, statErr := os.Stat(filepath.Join(dir, "decision.lock.yml"))
	require.True(t, os.IsNotExist(statErr), "unsafe decision must not create a lock file")
}

func TestDecisionCompileRejectsSecretFromUnsupportedShellOutput(t *testing.T) {
	dir := testutil.TempDir(t, "decision-unsupported-shell-output")
	file := filepath.Join(dir, "decision.md")
	source := `---
strict: false
on:
  workflow_dispatch:
  roles: all
  decision:
    questions:
      - Should ${{ steps.collect.outputs.value }} run?
  steps:
    - id: collect
      shell: pwsh
      env:
        PRIVATE_VALUE: ${{ secrets.PRIVATE_VALUE }}
      run: '"value=$env:PRIVATE_VALUE" >> $env:GITHUB_OUTPUT'
engine: copilot
---
Perform the requested work.
`
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	err := NewCompiler(WithLint(false)).CompileWorkflow(file)
	require.ErrorContains(t, err, "on.decision.questions[0]: credential-derived values are not allowed")
	assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
	_, statErr := os.Stat(filepath.Join(dir, "decision.lock.yml"))
	require.True(t, os.IsNotExist(statErr), "unsafe decision must not create a lock file")
}
