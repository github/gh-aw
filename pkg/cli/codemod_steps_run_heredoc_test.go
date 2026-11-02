//go:build !integration

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepsRunCodemodHeredocBoundaries(t *testing.T) {
	t.Parallel()
	expression := "${{ github.event.issue.title }}"
	for _, test := range []struct {
		name, run string
		wantRun   string
		wantError bool
	}{
		{"expanding body", "cat <<EOF\n" + expression + "\nEOF\n", "cat <<EOF\n$EXPR_GITHUB_EVENT_ISSUE_TITLE\nEOF\n", false},
		{"hash body", "cat <<EOF\n# " + expression + "\nEOF\n", "cat <<EOF\n# $EXPR_GITHUB_EVENT_ISSUE_TITLE\nEOF\n", false},
		{"inline hash body", "cat <<EOF\ntext # " + expression + "\nEOF\n", "cat <<EOF\ntext # $EXPR_GITHUB_EVENT_ISSUE_TITLE\nEOF\n", false},
		{"quoted body", "cat <<'EOF'\n" + expression + "\nEOF\n", "", false},
		{"double quoted body", "cat <<\"EOF\"\n" + expression + "\nEOF\n", "", false},
		{"partially quoted custom delimiter", "cat <<END'_DATA'\n" + expression + "\nEND_DATA\n", "", false},
		{"tab stripped body", "cat <<-CUSTOM\n\t# " + expression + "\n\tCUSTOM\n", "cat <<-CUSTOM\n\t# $EXPR_GITHUB_EVENT_ISSUE_TITLE\n\tCUSTOM\n", false},
		{"continued unquoted delimiter", "cat <<EO\\\nF\n" + expression + "\nEOF\n", "cat <<EO\\\nF\n$EXPR_GITHUB_EVENT_ISSUE_TITLE\nEOF\n", false},
		{"commands after quoted close", "cat <<'EOF'\n" + expression + "\nEOF\nprintf '%s' \"" + expression + "\"\n", "cat <<'EOF'\n" + expression + "\nEOF\nprintf '%s' \"$EXPR_GITHUB_EVENT_ISSUE_TITLE\"\n", false},
		{"nested quoted body", "cat <<OUTER\n$(cat <<'INNER'\n" + expression + "\nINNER\n)\n# " + expression + "\nOUTER\n", "cat <<OUTER\n$(cat <<'INNER'\n" + expression + "\nINNER\n)\n# $EXPR_GITHUB_EVENT_ISSUE_TITLE\nOUTER\n", false},
		{"multiline quote hash", "printf '%s' \"first\n# " + expression + "\"\n", "printf '%s' \"first\n# $EXPR_GITHUB_EVENT_ISSUE_TITLE\"\n", false},
		{"mismatched close", "cat <<'EOF'\n" + expression + "\nNOT_EOF\n", "", true},
		{"unproven escaped delimiter", "cat <<$'E\\x4fF'\nEOF\n" + expression + "\nE\\x4fF\n", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			content := heredocCodemodSource(test.run)
			document, err := aw.Parse(content)
			require.NoError(t, err)
			transformation := getStepsRunSecretsToEnvCodemod()
			result, applied, err := transformation.Apply(content, document.Frontmatter)
			if test.wantError {
				require.ErrorContains(t, err, "steps[0].run")
				assert.Contains(t, err.Error(), "manually")
				assert.NotContains(t, err.Error(), expression)
				assert.Equal(t, content, result, "failed migrations must be atomic")
				assert.False(t, applied)
				return
			}
			require.NoError(t, err)
			if test.wantRun == "" {
				assert.False(t, applied)
				assert.Equal(t, content, result)
			} else {
				require.True(t, applied)
				updated, err := aw.Parse(result)
				require.NoError(t, err)
				step := updated.Frontmatter["steps"].([]any)[0].(map[string]any)
				assert.Equal(t, test.wantRun, step["run"])
				assert.Equal(t, expression, step["env"].(map[string]any)["EXPR_GITHUB_EVENT_ISSUE_TITLE"])
				assert.Contains(t, result, "# keep this comment")
				assert.True(t, strings.HasSuffix(result, "Preserve the Markdown body.\n"))
				again, changed, err := transformation.Apply(result, updated.Frontmatter)
				require.NoError(t, err)
				assert.False(t, changed)
				assert.Equal(t, result, again)
			}
		})
	}
}

func TestStepsRunCodemodHeredocGuidedFailureIsAtomic(t *testing.T) {
	t.Parallel()
	content := heredocCodemodSource("cat <<'EOF'\n${{ github.ref }}\nWRONG\n")
	content = strings.Replace(content, "steps:\n", "steps:\n  - run: echo ${{ github.actor }}\n", 1)
	_, _, err := applyCodemodsUntilStable(content, "workflow.md", []Codemod{getStepsRunSecretsToEnvCodemod()})
	var guided *GuidedError
	require.ErrorAs(t, err, &guided)
	assert.Contains(t, err.Error(), "no changes to this workflow are written")
}

func TestStepsRunCodemodExpandingBodyTreatsValuesAsData(t *testing.T) {
	t.Parallel()
	run := "cat <<CUSTOM\n# ${{ github.event.issue.title }}\nCUSTOM\n"
	step := map[string]any{"run": run}
	require.NoError(t, hoistStepRunExpressions(step))
	command := exec.Command("bash", "-c", step["run"].(string))
	command.Env = append(os.Environ(), "EXPR_GITHUB_EVENT_ISSUE_TITLE=$(printf evaluated)")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Equal(t, "# $(printf evaluated)\n", string(output))
}

func heredocCodemodSource(run string) string {
	return "---\non: push\nsteps:\n  - name: heredoc # keep this comment\n    run: |\n      " +
		strings.ReplaceAll(strings.TrimSuffix(run, "\n"), "\n", "\n      ") + "\n---\nPreserve the Markdown body.\n"
}

func FuzzStepsRunCodemodHeredocs(f *testing.F) {
	f.Add("CUSTOM", false, false)
	f.Add("END_DATA", true, true)
	f.Fuzz(func(t *testing.T, rawDelimiter string, quoted, hash bool) {
		delimiter := "HEREDOC_" + sanitizeHoistName(rawDelimiter)
		opener := delimiter
		if quoted {
			opener = "'" + delimiter + "'"
		}
		prefix := ""
		if hash {
			prefix = "# "
		}
		run := fmt.Sprintf("cat <<%s\n%s${{ github.ref }}\n%s\n", opener, prefix, delimiter)
		step := map[string]any{"run": run}
		require.NoError(t, hoistStepRunExpressions(step))
		if quoted {
			assert.Equal(t, run, step["run"])
			assert.NotContains(t, step, "env")
		} else {
			assert.NotContains(t, step["run"], "${{")
			assert.Contains(t, step["run"], "$EXPR_GITHUB_REF")
		}
	})
}
