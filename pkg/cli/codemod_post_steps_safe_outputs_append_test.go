//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostStepsSafeOutputsAppendCodemod(t *testing.T) {
	t.Parallel()
	mod := getPostStepsSafeOutputsAppendCodemod()
	assert.Equal(t, "post-steps-safe-outputs-append", mod.ID)
	assert.NotEmpty(t, mod.Name)
	assert.NotEmpty(t, mod.Description)
	assert.NotEmpty(t, mod.IntroducedIn)
	assert.False(t, mod.Guided)
	require.NotNil(t, mod.Apply)

	content := `---
on: workflow_dispatch # keep trigger comment
safe-outputs:
  add-comment:
post-steps:
  - name: Record result # keep step comment
    if: success()
    run: |
      echo '{"type":"add_comment","body":"Done","item_number":42}' >> "$GH_AW_SAFE_OUTPUTS"
  - run: echo "unrelated"
---

# Keep markdown exactly
`
	doc, err := aw.Parse(content)
	require.NoError(t, err)
	got, applied, err := mod.Apply(content, doc.Frontmatter)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Contains(t, got, `safeoutputs add_comment '{"body":"Done","item_number":42}'`)
	assert.Contains(t, got, "on: workflow_dispatch # keep trigger comment")
	assert.Contains(t, got, "  - name: Record result # keep step comment")
	assert.Contains(t, got, "    if: success()")
	assert.Contains(t, got, `  - run: echo "unrelated"`)
	assert.Contains(t, got, "---\n\n# Keep markdown exactly\n")
	updated, err := aw.Parse(got)
	require.NoError(t, err)
	require.NoError(t, workflow.ValidatePostStepsSafeOutputs(updated.Frontmatter["post-steps"].([]any)))
	again, applied, err := mod.Apply(got, updated.Frontmatter)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, got, again)
}

func TestRewritePostStepSafeOutputsAppend(t *testing.T) {
	t.Parallel()
	payload := `'{"type":"create_issue","title":"Result","body":"Done"}'`
	for _, target := range []string{
		`"$GH_AW_SAFE_OUTPUTS"`, `"${GH_AW_SAFE_OUTPUTS}"`, `$GH_AW_SAFE_OUTPUTS`,
		`${GH_AW_SAFE_OUTPUTS}`, `"$GH_AW_AGENT_OUTPUT"`, `${GH_AW_AGENT_OUTPUT}`,
		`/tmp/gh-aw/agent_output.json`, `"/tmp/gh-aw/agent_output.json"`,
	} {
		t.Run(target, func(t *testing.T) {
			got, ok := rewritePostStepSafeOutputsAppend("echo " + payload + " >> " + target + "\n")
			require.True(t, ok)
			assert.Equal(t, "safeoutputs create_issue '{\"body\":\"Done\",\"title\":\"Result\"}'\n", got)
		})
	}
	got, ok := rewritePostStepSafeOutputsAppend(`printf '%s\n' '{"type":"add_comment","body":"Line\nIt\u0027s $(literal)","item_number":1}' >> "$GH_AW_SAFE_OUTPUTS"`)
	require.True(t, ok)
	assert.Equal(t, `safeoutputs add_comment '{"body":"Line\nIt\u0027s $(literal)","item_number":1}'`, got)
}

func TestRewritePostStepSafeOutputsAppend_UnsafeNoop(t *testing.T) {
	t.Parallel()
	for _, run := range []string{
		`cat "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"noop"}' > "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"noop"}' >> "$GH_AW_SAFE_OUTPUTS_OTHER"`,
		`echo '{"type":"noop"}' >> "$GH_AW_SAFE_OUTPUTS".bak`,
		`echo '{"type":"noop"}' >> "/tmp/gh-aw/agent_output.json`,
		`echo '{"type":"noop"}' >> /tmp/gh-aw/agent_output.json"`,
		`echo "$PAYLOAD" >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo -e '{"type":"noop"}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"add_comment","body":"line\nbreak"}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"noop","body":"${{ github.event.issue.title }}"}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"noop"}' >> "$GH_AW_SAFE_OUTPUTS"; echo done`,
		"echo '{\"type\":\"noop\"}' >> \"$GH_AW_SAFE_OUTPUTS\"\ncat \"$GH_AW_SAFE_OUTPUTS\"",
		`echo '{"type":"noop"}' 2>> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"noop"}' >> "$GH_AW_SAFE_OUTPUTS" 2>&1`,
		`echo '{"type":"noop"}' | tee -a "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"noop"}' >> "$(get_path)"`,
		`echo '{"type":"noop"}' >> "$GH_AW_SAFE_OUTPUTS" &`,
		`echo '{"type":"noop"}' >> "$GH_AW_SAFE_OUTPUTS" || true`,
		`printf '%b\n' '{"type":"noop"}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`printf '%s\n' '{"type":"noop"}' extra >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo 'null' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '[]' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":null}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"--help"}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"noop; id"}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"missing_tool","tool":"test"}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"create_pull_request","title":"Result"}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo '{"type":"noop"}{}' >> "$GH_AW_SAFE_OUTPUTS"`,
		`echo 'not JSON' >> "$GH_AW_SAFE_OUTPUTS"`,
		`safeoutputs noop '{"message":"Done"}'`,
	} {
		t.Run(run, func(t *testing.T) {
			got, ok := rewritePostStepSafeOutputsAppend(run)
			assert.False(t, ok)
			assert.Equal(t, run, got)
		})
	}
}

func TestPostStepsSafeOutputsAppendCodemod_NoOp(t *testing.T) {
	t.Parallel()
	for _, content := range []string{
		"---\non: workflow_dispatch\n---\n",
		"---\npost-steps:\n  - run: echo done\n---\n",
		"---\npost-steps:\n  - shell: pwsh\n    run: |\n      echo '{\"type\":\"noop\"}' >> \"$GH_AW_SAFE_OUTPUTS\"\n---\n",
		"---\npost-steps:\n  - run: echo preceding\n  - run: |\n      echo '{\"type\":\"noop\"}' >> \"$GH_AW_SAFE_OUTPUTS\"\n---\n",
		"---\npost-steps:\n  - if: steps.parse.outputs.ok\n    run: |\n      echo '{\"type\":\"noop\"}' >> \"$GH_AW_SAFE_OUTPUTS\"\n---\n",
		"---\npost-steps:\n  - env:\n      CUSTOM: value\n    run: |\n      echo '{\"type\":\"noop\"}' >> \"$GH_AW_SAFE_OUTPUTS\"\n---\n",
		"---\nsteps:\n  - run: |\n      echo '{\"type\":\"noop\"}' >> \"$GH_AW_SAFE_OUTPUTS\"\n---\n",
	} {
		doc, err := aw.Parse(content)
		require.NoError(t, err)
		got, applied, err := getPostStepsSafeOutputsAppendCodemod().Apply(content, doc.Frontmatter)
		require.NoError(t, err)
		assert.False(t, applied)
		assert.Equal(t, content, got)
	}

}

func TestPostStepsSafeOutputsAppendCodemod_Compile(t *testing.T) {
	t.Parallel()
	content := `---
on: workflow_dispatch
engine: claude
strict: false
permissions:
  contents: read
safe-outputs:
  add-comment:
post-steps:
  - name: Record result
    run: |
      echo '{"type":"add_comment","body":"Done","item_number":42}' >> "$GH_AW_SAFE_OUTPUTS"
  - name: Upload report
    uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f
    with:
      name: report
      path: report.txt
---
# Agent
Report results.
`
	doc, err := aw.Parse(content)
	require.NoError(t, err)
	fixed, applied, err := getPostStepsSafeOutputsAppendCodemod().Apply(content, doc.Frontmatter)
	require.NoError(t, err)
	require.True(t, applied)
	path := filepath.Join(t.TempDir(), "workflow.md")
	require.NoError(t, os.WriteFile(path, []byte(fixed), 0o600))
	require.NoError(t, workflow.NewCompiler().CompileWorkflow(path))
	lock, err := os.ReadFile(filepath.Join(filepath.Dir(path), "workflow.lock.yml"))
	require.NoError(t, err)
	// Ignore frontmatter embedded in comments; these are actual emitted steps.
	text := string(lock)
	previous := -1
	for _, step := range []string{
		"      - name: Execute Claude Code CLI",
		"      - name: Record result",
		"      - name: Stop MCP Gateway",
		"      - name: Redact secrets",
		"      - name: Copy Safe Outputs",
		"      - name: Ingest agent output",
		"      - name: Upload report",
	} {
		index := strings.Index(text, step)
		require.NotEqual(t, -1, index, "missing step %s", step)
		require.Greater(t, index, previous, "step out of order: %s", step)
		previous = index
	}
	assert.Equal(t, 1, strings.Count(text, "      - name: Record result"))
}

func TestPostStepsSafeOutputsAppendCodemod_FixOutput(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workflow.md")
	content := "---\npost-steps:\n  - run: |\n      echo '{\"type\":\"noop\",\"message\":\"Done\"}' >> \"$GH_AW_SAFE_OUTPUTS\"\n---\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	mods := []Codemod{getPostStepsSafeOutputsAppendCodemod(), getPostStepsSafeOutputsGuidedErrorCodemod()}
	for _, write := range []bool{false, true} {
		fixed, changes, err := processWorkflowFileWithInfo(path, mods, write, false)
		require.NoError(t, err)
		assert.True(t, fixed)
		assert.Equal(t, []string{mods[0].Name}, changes)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		if write {
			assert.Contains(t, string(data), `safeoutputs noop '{"message":"Done"}'`)
		} else {
			assert.Equal(t, content, string(data))
		}
	}
}

func TestRewritePostStepSafeOutputsAppend_ShellArgument(t *testing.T) {
	t.Parallel()
	run := `printf '%s\n' '{"type":"add_comment","body":"It\u0027s $(not_executed); ` + "`not_executed`" + `","item_number":42,"extra":{"n":9007199254740993}}' >> "$GH_AW_SAFE_OUTPUTS"`
	rewritten, ok := rewritePostStepSafeOutputsAppend(run)
	require.True(t, ok)
	// A shell function stands in for the transport; no service or GitHub mutation
	// is involved. It verifies the real shell receives one intact JSON argument.
	command := exec.Command("bash", "-c", `safeoutputs() { test "$#" -eq 2 || exit 1; printf '%s\n' "$1" "$2"; }; `+rewritten)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	assert.Equal(t, "add_comment\n"+`{"body":"It\u0027s $(not_executed); `+"`not_executed`"+`","extra":{"n":9007199254740993},"item_number":42}`+"\n", string(output))
}

func TestPostStepsSafeOutputsAppendCodemod_ManualBlockerIsAtomic(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workflow.md")
	content := "---\npost-steps:\n  - run: |\n      echo '{\"type\":\"noop\",\"message\":\"Done\"}' >> \"$GH_AW_SAFE_OUTPUTS\"\n  - run: cat \"$GH_AW_SAFE_OUTPUTS\"\n---\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	mods := []Codemod{getPostStepsSafeOutputsAppendCodemod(), getPostStepsSafeOutputsGuidedErrorCodemod()}
	fixed, _, err := processWorkflowFileWithInfo(path, mods, true, false)
	require.Error(t, err)
	var guided *GuidedError
	require.ErrorAs(t, err, &guided)
	assert.False(t, fixed)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func FuzzRewritePostStepSafeOutputsAppend(f *testing.F) {
	f.Add(`echo '{"type":"noop","message":"Done"}' >> "$GH_AW_SAFE_OUTPUTS"`)
	f.Add(`printf '%s\n' '{"type":"add_comment","body":"It\u0027s ready","item_number":1}' >> "${GH_AW_SAFE_OUTPUTS}"`)
	f.Add(`cat "$GH_AW_SAFE_OUTPUTS"`)
	f.Fuzz(func(t *testing.T, run string) {
		got, applied := rewritePostStepSafeOutputsAppend(run)
		if !applied {
			require.Equal(t, run, got)
			return
		}
		require.True(t, workflow.IsLiteralSafeOutputsToolStep(map[string]any{"run": got}))
		again, applied := rewritePostStepSafeOutputsAppend(got)
		require.False(t, applied)
		require.Equal(t, got, again)
	})
}
