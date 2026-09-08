//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsShellcheckableShell tests the shell filter.
func TestIsShellcheckableShell(t *testing.T) {
	t.Parallel()
	t.Run("empty shell defaults to bash", func(t *testing.T) {
		assert.True(t, isShellcheckableShell(""))
	})
	t.Run("bash is checkable", func(t *testing.T) {
		assert.True(t, isShellcheckableShell("bash"))
	})
	t.Run("BASH is checkable (case insensitive)", func(t *testing.T) {
		assert.True(t, isShellcheckableShell("BASH"))
	})
	t.Run("sh is checkable", func(t *testing.T) {
		assert.True(t, isShellcheckableShell("sh"))
	})
	t.Run("pwsh is not checkable", func(t *testing.T) {
		assert.False(t, isShellcheckableShell("pwsh"))
	})
	t.Run("powershell is not checkable", func(t *testing.T) {
		assert.False(t, isShellcheckableShell("powershell"))
	})
	t.Run("python is not checkable", func(t *testing.T) {
		assert.False(t, isShellcheckableShell("python"))
	})
	t.Run("cmd is not checkable", func(t *testing.T) {
		assert.False(t, isShellcheckableShell("cmd"))
	})
}

// TestShellcheckShell verifies the --shell= argument selection.
func TestShellcheckShell(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "bash", shellcheckShell(""))
	assert.Equal(t, "bash", shellcheckShell("bash"))
	assert.Equal(t, "sh", shellcheckShell("sh"))
	// Any other value should fall back to bash.
	assert.Equal(t, "bash", shellcheckShell("zsh"))
}

// TestExtractRunStepsFromLockFile tests YAML parsing and step extraction.
func TestExtractRunStepsFromLockFile(t *testing.T) {
	t.Parallel()
	t.Run("extracts bash and sh steps", func(t *testing.T) {
		content := `
jobs:
  build:
    steps:
      - name: bash step
        run: echo hello
      - name: sh step
        shell: sh
        run: echo sh
      - name: pwsh step
        shell: pwsh
        run: Write-Host hi
      - name: uses step
        uses: actions/checkout@v4
`
		tmpFile := writeTempLockFile(t, content)
		steps, err := extractRunStepsFromLockFile(tmpFile)
		require.NoError(t, err)
		require.Len(t, steps, 2)
		assert.Equal(t, "bash step", steps[0].Name)
		assert.Equal(t, "echo hello", steps[0].Script)
		assert.Empty(t, steps[0].Shell) // no shell field or defaults → empty (=default bash)
		assert.Equal(t, "sh step", steps[1].Name)
		assert.Equal(t, "sh", steps[1].Shell)
	})

	t.Run("returns empty slice when no run steps", func(t *testing.T) {
		content := `
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
`
		tmpFile := writeTempLockFile(t, content)
		steps, err := extractRunStepsFromLockFile(tmpFile)
		require.NoError(t, err)
		assert.Empty(t, steps)
	})

	t.Run("returns empty slice when no jobs", func(t *testing.T) {
		content := `name: empty workflow`
		tmpFile := writeTempLockFile(t, content)
		steps, err := extractRunStepsFromLockFile(tmpFile)
		require.NoError(t, err)
		assert.Empty(t, steps)
	})

	t.Run("handles multiple jobs", func(t *testing.T) {
		content := `
jobs:
  job1:
    steps:
      - name: step1
        run: echo job1
  job2:
    steps:
      - name: step2
        run: echo job2
`
		tmpFile := writeTempLockFile(t, content)
		steps, err := extractRunStepsFromLockFile(tmpFile)
		require.NoError(t, err)
		assert.Len(t, steps, 2)
	})

	t.Run("skips steps whose job-level default shell is pwsh", func(t *testing.T) {
		content := `
jobs:
  windows-job:
    defaults:
      run:
        shell: pwsh
    steps:
      - name: ps step
        run: Write-Host hi
      - name: explicit bash step
        shell: bash
        run: echo hello
`
		tmpFile := writeTempLockFile(t, content)
		steps, err := extractRunStepsFromLockFile(tmpFile)
		require.NoError(t, err)
		// ps step has effective shell=pwsh → skipped; explicit bash step is kept.
		require.Len(t, steps, 1)
		assert.Equal(t, "explicit bash step", steps[0].Name)
		assert.Equal(t, "bash", steps[0].Shell)
	})

	t.Run("inherits workflow-level default shell", func(t *testing.T) {
		content := `
defaults:
  run:
    shell: sh
jobs:
  build:
    steps:
      - name: inherited sh step
        run: echo sh
`
		tmpFile := writeTempLockFile(t, content)
		steps, err := extractRunStepsFromLockFile(tmpFile)
		require.NoError(t, err)
		require.Len(t, steps, 1)
		assert.Equal(t, "inherited sh step", steps[0].Name)
		assert.Equal(t, "sh", steps[0].Shell)
	})

	t.Run("job default overrides workflow default", func(t *testing.T) {
		content := `
defaults:
  run:
    shell: bash
jobs:
  windows-job:
    defaults:
      run:
        shell: pwsh
    steps:
      - name: ps step
        run: Write-Host hi
`
		tmpFile := writeTempLockFile(t, content)
		steps, err := extractRunStepsFromLockFile(tmpFile)
		require.NoError(t, err)
		// job default (pwsh) overrides workflow default (bash) → step is skipped
		assert.Empty(t, steps)
	})

	t.Run("skips steps whose workflow-level default shell is pwsh", func(t *testing.T) {
		content := `
defaults:
  run:
    shell: pwsh
jobs:
  build:
    steps:
      - name: ps step
        run: Write-Host hi
`
		tmpFile := writeTempLockFile(t, content)
		steps, err := extractRunStepsFromLockFile(tmpFile)
		require.NoError(t, err)
		assert.Empty(t, steps)
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		_, err := extractRunStepsFromLockFile("/nonexistent/file.lock.yml")
		require.Error(t, err)
	})

	t.Run("returns error for invalid YAML", func(t *testing.T) {
		tmpFile := writeTempLockFile(t, "{{invalid yaml: [")
		_, err := extractRunStepsFromLockFile(tmpFile)
		require.Error(t, err)
	})
}

// TestSanitizeGHAExpressions verifies that ${{ ... }} expressions are replaced
// with a shell-safe placeholder before shellcheck runs.
func TestSanitizeGHAExpressions(t *testing.T) {
	t.Parallel()
	t.Run("replaces simple expression", func(t *testing.T) {
		assert.Equal(t, `echo __GHA_EXPR__`, sanitizeGHAExpressions(`echo ${{ github.actor }}`))
	})
	t.Run("replaces expression in quoted string", func(t *testing.T) {
		assert.Equal(t, `echo "__GHA_EXPR__"`, sanitizeGHAExpressions(`echo "${{ github.actor }}"`))
	})
	t.Run("replaces multiple expressions", func(t *testing.T) {
		result := sanitizeGHAExpressions(`echo ${{ github.actor }} at ${{ github.ref }}`)
		assert.Equal(t, `echo __GHA_EXPR__ at __GHA_EXPR__`, result)
	})
	t.Run("replaces expression in single-quoted string", func(t *testing.T) {
		result := sanitizeGHAExpressions(`echo '${{ github.actor }}'`)
		assert.Equal(t, `echo '__GHA_EXPR__'`, result)
	})
	t.Run("leaves plain shell script unchanged", func(t *testing.T) {
		script := "echo hello\nls -la"
		assert.Equal(t, script, sanitizeGHAExpressions(script))
	})
	t.Run("handles expression with nested braces", func(t *testing.T) {
		result := sanitizeGHAExpressions(`echo ${{ fromJSON(steps.out.outputs.data)['key'] }}`)
		assert.Equal(t, `echo __GHA_EXPR__`, result)
	})
}

// TestStepLabel tests the diagnostic label helper.
func TestStepLabel(t *testing.T) {
	t.Parallel()
	t.Run("includes step name when set", func(t *testing.T) {
		info := runStepInfo{Name: "my step", LockFile: "/a/b/foo.lock.yml"}
		label := stepLabel(info)
		assert.Contains(t, label, "foo.lock.yml")
		assert.Contains(t, label, "my step")
	})
	t.Run("uses lock file basename when name is empty", func(t *testing.T) {
		info := runStepInfo{Name: "", LockFile: "/a/b/foo.lock.yml"}
		label := stepLabel(info)
		assert.Equal(t, "foo.lock.yml", label)
	})
}

// TestDefaultIgnoreCodes verifies the well-known false-positive codes are present.
func TestDefaultIgnoreCodes(t *testing.T) {
	t.Parallel()
	assert.Contains(t, shellcheckDefaultIgnoreCodes, "SC2016")
	assert.Contains(t, shellcheckDefaultIgnoreCodes, "SC1090")
	assert.Contains(t, shellcheckDefaultIgnoreCodes, "SC1091")
	assert.Contains(t, shellcheckDefaultIgnoreCodes, "SC2002")
	assert.Contains(t, shellcheckDefaultIgnoreCodes, "SC2129")
	assert.Contains(t, shellcheckDefaultIgnoreCodes, "SC2153")
	assert.Contains(t, shellcheckDefaultIgnoreCodes, "SC2154")
}

// TestRunShellcheckOnLockFilesSkipsWhenUnavailable verifies that the function
// returns nil (no error) and does not panic when shellcheck is not in PATH and
// Docker is also unavailable. We use a PATH override to hide both binaries.
func TestRunShellcheckOnLockFilesSkipsWhenUnavailable(t *testing.T) {
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", "") // ensure neither shellcheck nor docker can be found

	err := runShellcheckOnLockFilesAndResources(context.Background(), []string{"/fake/file.lock.yml"}, nil, false, false)
	assert.NoError(t, err)
}

// TestRunShellcheckOnLockFilesEmpty returns nil for an empty list.
func TestRunShellcheckOnLockFilesEmpty(t *testing.T) {
	t.Parallel()
	err := runShellcheckOnLockFilesAndResources(context.Background(), nil, nil, false, false)
	assert.NoError(t, err)
}

func TestRunShellcheckOnScriptVerboseDoesNotEmitInvocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell script stub")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "shellcheck")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	origPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })
	require.NoError(t, os.Setenv("PATH", dir))

	out, err := runShellcheckOnScript(runStepInfo{
		Name:     "native step",
		Script:   "echo hello",
		Shell:    "bash",
		LockFile: "/tmp/test.lock.yml",
	}, shellcheckDefaultIgnoreCodes, true)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestRunShellcheckOnScriptViaDocker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell script stub")
	}

	t.Run("success passes stdin and args", func(t *testing.T) {
		restore := stubDockerCommand(t, `#!/bin/sh
set -eu
printf '%s\n' "$*" > "$DOCKER_ARGS_FILE"
cat > "$DOCKER_STDIN_FILE"
exit 0
`)
		defer restore()

		info := runStepInfo{
			Name:     "docker step",
			Script:   `echo "${{ github.actor }}"`,
			Shell:    "bash",
			LockFile: "/tmp/test.lock.yml",
		}
		out, err := runShellcheckOnScriptViaDocker(context.Background(), info, []string{"SC2016"}, true)
		require.NoError(t, err)
		assert.Empty(t, out)

		argsText, readErr := os.ReadFile(os.Getenv("DOCKER_ARGS_FILE"))
		require.NoError(t, readErr)
		assert.Contains(t, string(argsText), "run --rm -i "+ShellcheckImage)
		assert.Contains(t, string(argsText), "--shell=bash")
		assert.Contains(t, string(argsText), "--exclude=SC2016")
		assert.True(t, strings.HasSuffix(strings.TrimSpace(string(argsText)), "-"))

		stdinText, readErr := os.ReadFile(os.Getenv("DOCKER_STDIN_FILE"))
		require.NoError(t, readErr)
		assert.Equal(t, "echo \"__GHA_EXPR__\"", string(stdinText))
	})

	t.Run("shellcheck exit code one is reported as findings", func(t *testing.T) {
		restore := stubDockerCommand(t, `#!/bin/sh
echo "-:1:1: warning: test finding [SC1000]"
exit 1
`)
		defer restore()

		info := runStepInfo{
			Name:     "docker step",
			Script:   "echo hello",
			Shell:    "bash",
			LockFile: "/tmp/test.lock.yml",
		}
		out, err := runShellcheckOnScriptViaDocker(context.Background(), info, nil, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shellcheck found issues")
		assert.Contains(t, string(out), "shellcheck findings in")
	})

	t.Run("docker failure includes stderr", func(t *testing.T) {
		restore := stubDockerCommand(t, `#!/bin/sh
echo "cannot connect to daemon" 1>&2
exit 2
`)
		defer restore()

		info := runStepInfo{
			Name:     "docker step",
			Script:   "echo hello",
			Shell:    "bash",
			LockFile: "/tmp/test.lock.yml",
		}
		_, err := runShellcheckOnScriptViaDocker(context.Background(), info, nil, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shellcheck (docker) failed")
		assert.Contains(t, err.Error(), "cannot connect to daemon")
	})
}

func TestRunShellcheckOnLockFiles_UsesDockerFallbackWhenBinaryMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell script stub")
	}

	lockFile := writeTempLockFile(t, `
jobs:
  build:
    steps:
      - name: lint me
        run: echo hello
`)

	restore := stubDockerCommand(t, `#!/bin/sh
set -eu
if [ "${1:-}" = "info" ]; then
  exit 0
fi
if [ "${1:-}" = "run" ]; then
  echo run >> "$DOCKER_CALLS_FILE"
  cat >/dev/null
  exit 0
fi
exit 2
`)
	defer restore()

	origPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", origPath) })
	// Ensure native shellcheck cannot be discovered.
	require.NoError(t, os.Setenv("PATH", filepath.Dir(os.Getenv("DOCKER_BIN"))))

	err := runShellcheckOnLockFilesAndResources(context.Background(), []string{lockFile}, nil, false, false)
	require.NoError(t, err)

	calls, readErr := os.ReadFile(os.Getenv("DOCKER_CALLS_FILE"))
	require.NoError(t, readErr)
	assert.Contains(t, string(calls), "run")
}

// TestRunShellcheckOnLockFiles_MultiStep verifies that the parallel fan-out
// checks every run step, keeps diagnostics attributable to their steps, and
// applies strict/non-strict error aggregation correctly (reports-all-then-errors
// rather than failing on the first issue).
func TestRunShellcheckOnLockFiles_MultiStep(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell script stub")
	}

	// Lock file with three bash run steps; step-b intentionally contains a
	// script that the stub reports as having a finding.
	lockFile := writeTempLockFile(t, `
jobs:
  build:
    steps:
      - name: step-a
        run: echo ok
      - name: step-b
        run: echo bad_syntax_trigger
      - name: step-c
        run: echo ok
`)

	// Docker stub: records every invocation in DOCKER_CALLS_FILE, emits a
	// synthetic finding (exit 1) for scripts containing "bad_syntax_trigger".
	// Use absolute path for cat since PATH is restricted to the stub directory.
	restore := stubDockerCommand(t, `#!/bin/sh
if [ "${1:-}" = "info" ]; then
  exit 0
fi
echo run >> "$DOCKER_CALLS_FILE"
script=$(/usr/bin/cat)
case "$script" in
  *bad_syntax_trigger*)
    printf 'script:1:1: warning: SC1000 synthetic finding\n'
    exit 1
    ;;
esac
exit 0
`)
	defer restore()

	origPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", origPath) })
	// Ensure native shellcheck cannot be discovered so Docker path is used.
	require.NoError(t, os.Setenv("PATH", filepath.Dir(os.Getenv("DOCKER_BIN"))))

	t.Run("non-strict: all steps checked, no error returned", func(t *testing.T) {
		_ = os.Remove(os.Getenv("DOCKER_CALLS_FILE")) // may not exist on first run

		err := runShellcheckOnLockFilesAndResources(context.Background(), []string{lockFile}, nil, false, false)
		require.NoError(t, err, "non-strict mode should not return error even when steps have findings")

		calls, readErr := os.ReadFile(os.Getenv("DOCKER_CALLS_FILE"))
		require.NoError(t, readErr)
		assert.Equal(t, 3, strings.Count(string(calls), "run"), "all three steps should be invoked")
	})

	t.Run("strict: all steps checked, error returned after all complete", func(t *testing.T) {
		_ = os.Remove(os.Getenv("DOCKER_CALLS_FILE")) // reset between sub-tests

		err := runShellcheckOnLockFilesAndResources(context.Background(), []string{lockFile}, nil, false, true)
		require.Error(t, err, "strict mode should return error when a step has findings")
		assert.Contains(t, err.Error(), "strict mode")

		calls, readErr := os.ReadFile(os.Getenv("DOCKER_CALLS_FILE"))
		require.NoError(t, readErr)
		// Reports-all-then-errors: all three steps must have been checked even
		// though step-b triggered a finding.
		assert.Equal(t, 3, strings.Count(string(calls), "run"), "all three steps should be invoked even in strict mode")
	})
}

func TestRunShellcheckOnLockFilesAndResources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell script stub")
	}

	restore := stubDockerCommand(t, `#!/bin/sh
	if [ "${1:-}" = "info" ]; then
	  exit 0
	fi
	test "$(/usr/bin/cat)" = "echo frontmatter script"
	`)
	defer restore()

	origPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", origPath) })
	require.NoError(t, os.Setenv("PATH", filepath.Dir(os.Getenv("DOCKER_BIN"))))

	err := runShellcheckOnLockFilesAndResources(context.Background(), nil, []workflow.ShellScriptResource{{
		Name:   "mcp-scripts.example",
		Script: "echo frontmatter script",
		Shell:  "bash",
	}}, false, true)
	require.NoError(t, err)
}

func stubDockerCommand(t *testing.T, script string) func() {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))

	argsFile := filepath.Join(dir, "docker-args.txt")
	stdinFile := filepath.Join(dir, "docker-stdin.txt")
	callsFile := filepath.Join(dir, "docker-calls.txt")

	origDocker := dockerCommandContext
	dockerCommandContext = exec.CommandContext

	origPath := os.Getenv("PATH")
	origArgs := os.Getenv("DOCKER_ARGS_FILE")
	origStdin := os.Getenv("DOCKER_STDIN_FILE")
	origCalls := os.Getenv("DOCKER_CALLS_FILE")
	origBin := os.Getenv("DOCKER_BIN")

	require.NoError(t, os.Setenv("PATH", dir+string(os.PathListSeparator)+origPath))
	require.NoError(t, os.Setenv("DOCKER_ARGS_FILE", argsFile))
	require.NoError(t, os.Setenv("DOCKER_STDIN_FILE", stdinFile))
	require.NoError(t, os.Setenv("DOCKER_CALLS_FILE", callsFile))
	require.NoError(t, os.Setenv("DOCKER_BIN", bin))

	return func() {
		dockerCommandContext = origDocker
		_ = os.Setenv("PATH", origPath)
		_ = os.Setenv("DOCKER_ARGS_FILE", origArgs)
		_ = os.Setenv("DOCKER_STDIN_FILE", origStdin)
		_ = os.Setenv("DOCKER_CALLS_FILE", origCalls)
		_ = os.Setenv("DOCKER_BIN", origBin)
	}
}

// writeTempLockFile writes content to a temporary *.lock.yml file and returns its path.
func writeTempLockFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
