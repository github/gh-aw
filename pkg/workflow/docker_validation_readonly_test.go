//go:build !integration && !js && !wasm

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateContainerImages_ReadOnly(t *testing.T) {
	tests := []struct {
		name          string
		readOnly      bool
		noEmit        bool
		requireDocker bool
		inspectResult string
		pullResult    string
		errorContains string
		wantPull      bool
	}{
		{
			name:          "read-only missing image",
			readOnly:      true,
			inspectResult: "missing",
			errorContains: "Read-only compilation does not pull images",
		},
		{
			name:          "read-only cached image",
			readOnly:      true,
			inspectResult: "cached",
		},
		{
			name:          "read-only inspect failure",
			readOnly:      true,
			inspectResult: "error",
			errorContains: "permission denied",
		},
		{
			name:          "read-only required missing image",
			readOnly:      true,
			requireDocker: true,
			inspectResult: "missing",
			errorContains: "Read-only compilation does not pull images",
		},
		{
			name:          "ordinary compile pulls missing image",
			inspectResult: "missing",
			wantPull:      true,
		},
		{
			name:          "ordinary no-emit still pulls missing image",
			noEmit:        true,
			inspectResult: "missing",
			wantPull:      true,
		},
		{
			name:          "ordinary cached image",
			inspectResult: "cached",
		},
		{
			name:          "ordinary missing registry image reports error",
			inspectResult: "missing",
			pullResult:    "missing",
			errorContains: "manifest unknown",
			wantPull:      true,
		},
		{
			name:          "ordinary private image preserves graceful validation",
			inspectResult: "missing",
			pullResult:    "private",
			wantPull:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := setupReadOnlyFakeDocker(t)
			t.Setenv("FAKE_DOCKER_INSPECT_RESULT", tt.inspectResult)
			t.Setenv("FAKE_DOCKER_PULL_RESULT", tt.pullResult)

			compiler := NewCompiler()
			compiler.SetNoEmit(tt.noEmit)
			compiler.SetReadOnly(tt.readOnly)
			compiler.SetRequireDocker(tt.requireDocker)
			err := compiler.validateContainerImages(&WorkflowData{
				Tools: map[string]any{
					"test-mcp": map[string]any{
						"container": "example/mcp",
						"version":   "latest",
					},
				},
			})
			if tt.errorContains != "" {
				require.ErrorContains(t, err, tt.errorContains)
				require.ErrorContains(t, err, "tool 'test-mcp'")
				if tt.readOnly {
					require.ErrorContains(t, err, "ensure the image is already present locally")
				}
			} else {
				require.NoError(t, err)
			}

			log, err := os.ReadFile(logPath)
			require.NoError(t, err)
			want := "info\nimage inspect example/mcp:latest\n"
			if tt.wantPull {
				want += "pull example/mcp:latest\n"
			}
			require.Equal(t, want, string(log), "read-only image validation must never invoke docker pull")
		})
	}
}

func TestCompileWorkflow_ReadOnlyContainerValidation(t *testing.T) {
	logPath := setupReadOnlyFakeDocker(t)
	t.Setenv("FAKE_DOCKER_INSPECT_RESULT", "missing")
	t.Chdir(filepath.Dir(logPath))
	const content = `---
on: push
strict: false
engine: claude
mcp-servers:
  test-mcp:
    type: stdio
    container: example/mcp:latest
    allowed: ["test_function"]
---
# Read-only container validation
Test the default validation path without pulling an uncached image.
`
	require.NoError(t, os.WriteFile("workflow.md", []byte(content), 0o600))
	compiler := NewCompiler(WithSkipValidation(false))
	compiler.SetNoEmit(true)
	compiler.SetReadOnly(true)
	require.NoError(t, compiler.CompileWorkflow("workflow.md"), "missing local images remain compiler warnings")
	require.Positive(t, compiler.GetWarningCount(), "missing local images should produce an actionable warning")
	log, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(log), "image inspect example/mcp:latest\n")
	require.NotContains(t, string(log), "pull ", "the default validation path must not pull images")
}

func TestValidateDockerImage_MissingRegistryImage(t *testing.T) {
	logPath := setupReadOnlyFakeDocker(t)
	t.Setenv("FAKE_DOCKER_INSPECT_RESULT", "missing")
	t.Setenv("FAKE_DOCKER_PULL_RESULT", "missing")

	err := validateDockerImageWithMode("example/mcp:nonexistent", false, false, false)
	require.ErrorContains(t, err, "manifest unknown")
	require.ErrorContains(t, err, "container image 'example/mcp:nonexistent' not found")

	log, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Equal(t, "info\nimage inspect example/mcp:nonexistent\npull example/mcp:nonexistent\n", string(log))
}

func setupReadOnlyFakeDocker(t *testing.T) string {
	t.Helper()
	// Keep the fake executable and its log inside the checkout.
	scratchDir, err := os.MkdirTemp(".", ".docker-validation-tests-")
	require.NoError(t, err)
	dir, err := filepath.Abs(scratchDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(dir))
	})
	logPath := filepath.Join(dir, "commands.log")
	const script = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_DOCKER_LOG"
case "$1" in
  info) exit 0 ;;
  image)
    case "$FAKE_DOCKER_INSPECT_RESULT" in
      cached) exit 0 ;;
      error) echo 'permission denied' >&2; exit 1 ;;
      *) echo 'No such image' >&2; exit 1 ;;
    esac ;;
  pull)
    case "$FAKE_DOCKER_PULL_RESULT" in
      missing) echo 'manifest unknown' >&2; exit 1 ;;
      private) echo 'authentication required' >&2; exit 1 ;;
      *) exit 0 ;;
    esac ;;
esac
exit 2
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DOCKER_LOG", logPath)
	resetDockerDaemonStateForTest()
	t.Cleanup(resetDockerDaemonStateForTest)
	return logPath
}
