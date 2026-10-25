//go:build !integration

package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPCompileDebugRecordsReplayIncomplete(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{
		"workflow:prompts Checking something risky",
		"namespace:message ordinary diagnostic",
		"bootstrap Checking something risky",
	} {
		for _, ending := range []string{"", "\n", "\ncli:test trailing debug +1ms\n"} {
			t.Run(prefix+ending, func(t *testing.T) {
				t.Parallel()
				diagnostics := &mcpCompileDiagnostics{}
				input := prefix + "\n✗ real compile error\n  remediation steps" + ending
				for _, char := range []byte(input) {
					_, err := diagnostics.Write([]byte{char})
					require.NoError(t, err)
				}
				diagnostics.finishLine()
				assert.True(t, strings.HasPrefix(diagnostics.failureMessage(errors.New("exit status 1")),
					"| ✗ real compile error\n| remediation steps"))
				assert.Empty(t, diagnostics.debugLines)
				assert.Zero(t, diagnostics.debugBytes)
			})
		}
	}
}

func TestMCPCompileDebugRecordsRealCompilerDiagnostics(t *testing.T) {
	t.Parallel()
	for _, diagnostic := range []string{
		"safe-outputs.github-app can post comments that re-trigger this workflow via issue_comment,",
		"Previously-approved action(s) removed:",
	} {
		for _, following := range []string{
			"  preserve this continuation\n",
			"⚠ real warning\n  warning remediation\n",
			"✗ real error\n  error remediation\n",
			"shellcheck findings in test.lock.yml:\nscript: line 1: SC2086 quote variable\n\n",
		} {
			t.Run(diagnostic+following, func(t *testing.T) {
				t.Parallel()
				diagnostics := &mcpCompileDiagnostics{}
				input := "⚠ original warning\n  original remediation\n" + diagnostic + "\n" + following
				for _, char := range []byte(input) {
					_, err := diagnostics.Write([]byte{char})
					require.NoError(t, err)
				}
				diagnostics.finishLine()
				assert.Contains(t, diagnostics.warnings.String(), "original warning\noriginal remediation\n"+diagnostic)
				switch {
				case strings.Contains(following, "⚠"):
					assert.Contains(t, diagnostics.warnings.String(), "real warning\nwarning remediation")
				case strings.Contains(following, "✗"):
					assert.Equal(t, "✗ real error\nerror remediation", diagnostics.message)
				case strings.Contains(following, "shellcheck"):
					assert.Contains(t, diagnostics.shellcheck.String(), "SC2086 quote variable")
					assert.NotContains(t, diagnostics.warnings.String(), "SC2086")
				default:
					assert.Contains(t, diagnostics.warnings.String(), diagnostic+"\npreserve this continuation")
				}
				assert.False(t, diagnostics.omitted)
			})
		}
	}
}

func TestMCPCompileDebugRecordsDoNotConfirmAcrossDiagnostics(t *testing.T) {
	t.Parallel()
	for _, following := range []string{
		"✗ real error\n  remediation +1ms\n",
		"⚠ real warning\n  remediation +1ms\n",
		"shellcheck findings in test.lock.yml:\nscript: line 1: SC2086 quote variable +1ms\n\n",
	} {
		t.Run(following, func(t *testing.T) {
			t.Parallel()
			diagnostics := &mcpCompileDiagnostics{}
			_, err := diagnostics.Write([]byte("workflow:prompts interrupted record\n" + following))
			require.NoError(t, err)
			diagnostics.finishLine()
			switch {
			case strings.HasPrefix(following, "✗"):
				assert.Equal(t, "✗ real error\nremediation +1ms", diagnostics.message)
			case strings.HasPrefix(following, "⚠"):
				assert.Contains(t, diagnostics.warnings.String(), "real warning\nremediation +1ms")
			default:
				assert.Contains(t, diagnostics.shellcheck.String(), "SC2086 quote variable +1ms")
			}
			assert.False(t, diagnostics.omitted)
		})
	}
}

func TestMCPCompileDebugRecordsDelimiterFreeNamespace(t *testing.T) {
	t.Parallel()
	for _, duration := range []string{"5µs", "380μs", "1ms"} {
		t.Run(duration, func(t *testing.T) {
			t.Parallel()
			diagnostics := &mcpCompileDiagnostics{}
			input := "⚠ real warning\n  original remediation\n" +
				"\x1b[36mbootstrap\x1b[0m Checking YAML:\n" +
				"  workflow_dispatch: null\n\n  warning: debug payload\n" +
				"  schedule: daily +" + duration + "\n  remaining remediation\n"
			for _, char := range []byte(input) {
				_, err := diagnostics.Write([]byte{char})
				require.NoError(t, err)
			}
			diagnostics.finishLine()
			assert.Equal(t, "⚠ real warning\noriginal remediation\nremaining remediation", diagnostics.warnings.String())
			assert.Empty(t, diagnostics.message)
			assert.True(t, diagnostics.omitted)
		})
	}
}

func TestMCPCompileDebugRecordsBoundProvisionalBuffer(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{"\n", "  YAML payload without suffix\n", strings.Repeat("x", maxMCPCompileDiagnosticBytes*2) + "\n"} {
		t.Run(payload[:min(len(payload), 24)], func(t *testing.T) {
			t.Parallel()
			diagnostics := &mcpCompileDiagnostics{}
			_, err := diagnostics.Write([]byte("workflow:prompts truncated record\n"))
			require.NoError(t, err)
			for range maxMCPCompileDiagnosticBytes + 1 {
				_, err := diagnostics.Write([]byte(payload))
				require.NoError(t, err)
				assert.LessOrEqual(t, diagnostics.debugBytes, maxMCPCompileDiagnosticBytes)
				assert.LessOrEqual(t, len(diagnostics.debugLines), maxMCPCompileDiagnosticBytes)
			}
			_, err = diagnostics.Write([]byte("✗ real error\n  remediation\n"))
			require.NoError(t, err)
			diagnostics.finishLine()
			assert.True(t, strings.HasPrefix(diagnostics.failureMessage(errors.New("exit status 1")), "| ✗ real error\n| remediation"))
			assert.Empty(t, diagnostics.debugLines)
		})
	}
}
