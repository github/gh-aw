//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunHasEvals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		setup    func(t *testing.T, dir string)
		expected bool
	}{
		{
			name: "root-level evals.jsonl (flattenSingleFileArtifacts output)",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, constants.EvalsResultFilename.String()), []byte("{}"), 0600))
			},
			expected: true,
		},
		{
			name: "evals/evals.jsonl (un-flattened artifact directory)",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				evalsDir := filepath.Join(dir, constants.EvalsArtifactName.String())
				require.NoError(t, os.Mkdir(evalsDir, 0700))
				require.NoError(t, os.WriteFile(filepath.Join(evalsDir, constants.EvalsResultFilename.String()), []byte("{}"), 0600))
			},
			expected: true,
		},
		{
			name: "hash-prefixed {hash}-evals/evals.jsonl (workflow_call variant)",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				evalsDir := filepath.Join(dir, "abc123-"+constants.EvalsArtifactName.String())
				require.NoError(t, os.Mkdir(evalsDir, 0700))
				require.NoError(t, os.WriteFile(filepath.Join(evalsDir, constants.EvalsResultFilename.String()), []byte("{}"), 0600))
			},
			expected: true,
		},
		{
			name: "evals/ directory exists but contains no evals.jsonl",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				evalsDir := filepath.Join(dir, constants.EvalsArtifactName.String())
				require.NoError(t, os.Mkdir(evalsDir, 0700))
				require.NoError(t, os.WriteFile(filepath.Join(evalsDir, "other.txt"), []byte("data"), 0600))
			},
			expected: false,
		},
		{
			name: "usage/evals.jsonl (compact usage artifact)",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				usageDir := filepath.Join(dir, constants.UsageArtifactName.String())
				require.NoError(t, os.Mkdir(usageDir, 0700))
				require.NoError(t, os.WriteFile(filepath.Join(usageDir, constants.EvalsResultFilename.String()), []byte("{}"), 0600))
			},
			expected: true,
		},
		{
			name: "hash-prefixed {hash}-usage/evals.jsonl (workflow_call compact usage artifact)",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				usageDir := filepath.Join(dir, "abc123-"+constants.UsageArtifactName.String())
				require.NoError(t, os.Mkdir(usageDir, 0700))
				require.NoError(t, os.WriteFile(filepath.Join(usageDir, constants.EvalsResultFilename.String()), []byte("{}"), 0600))
			},
			expected: true,
		},
		{
			name:     "empty directory",
			setup:    func(t *testing.T, dir string) {},
			expected: false,
		},
		{
			name: "non-existent directory",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.RemoveAll(dir))
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tc.setup(t, dir)
			assert.Equal(t, tc.expected, runHasEvals(dir, false))
		})
	}
}

func TestRunContainsSafeOutputType(t *testing.T) {
	t.Parallel()

	t.Run("matches normalized type and skips malformed items", func(t *testing.T) {
		t.Parallel()
		runDir := t.TempDir()
		output := `{"items":[[],{"type":"create-issue"}]}`
		require.NoError(t, os.WriteFile(filepath.Join(runDir, constants.AgentOutputFilename.String()), []byte(output), 0o600))

		found, err := runContainsSafeOutputType(runDir, "create_issue", false)

		require.NoError(t, err)
		assert.True(t, found)
	})

	t.Run("returns error for malformed JSON", func(t *testing.T) {
		t.Parallel()
		runDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(runDir, constants.AgentOutputFilename.String()), []byte(`{"items":[`), 0o600))

		found, err := runContainsSafeOutputType(runDir, "create_issue", false)

		require.Error(t, err)
		assert.False(t, found)
	})
}
