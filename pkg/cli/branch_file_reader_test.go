//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadRemoteRepoBranchFileContextUsesGETMethod(t *testing.T) {
	for _, tt := range []struct {
		name     string
		repo     string
		hostname string
		wantHost string
	}{
		{name: "default host", repo: "github/gh-aw"},
		{name: "repository host", repo: "github.example.com/github/gh-aw", wantHost: "github.example.com"},
		{name: "explicit host", repo: "github/gh-aw", hostname: "github.example.com", wantHost: "github.example.com"},
		{name: "explicit public host", repo: "github/gh-aw", hostname: "github.com", wantHost: "github.com"},
		{name: "host override", repo: "github.example.com/github/gh-aw", hostname: "other.example.com", wantHost: "other.example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeBinDir := t.TempDir()
			argsLog := filepath.Join(fakeBinDir, "gh-args.log")
			t.Setenv("EXPERIMENT_GH_ARGS_LOG", argsLog)
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$EXPERIMENT_GH_ARGS_LOG\"\nprintf '\\n' >> \"$EXPERIMENT_GH_ARGS_LOG\"\necho aGVsbG8=\n"
			require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "gh"), []byte(script), 0o755))
			t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GH_HOST", "ambient.example.com")

			content, err := readRemoteRepoBranchFileContext(context.Background(), tt.repo, "evals/{owner}+test#1", "nested dir/evals#1.jsonl", tt.hostname)
			require.NoError(t, err)
			assert.Equal(t, "hello", string(content))

			expected := []string{"api", "repos/github/gh-aw/contents/nested%20dir/evals%231.jsonl",
				"--method", "GET", "--raw-field", "ref=evals/{owner}+test#1", "--jq", ".content"}
			if tt.wantHost != "" {
				expected = append(expected, "--hostname", tt.wantHost)
			}

			assert.Equal(t, [][]string{expected}, readExperimentGHArgs(t, argsLog))
		})
	}
}

func TestReadRemoteRepoBranchFileNotFound(t *testing.T) {
	fakeBinDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "gh"),
		[]byte("#!/bin/sh\necho 'HTTP 404: Not Found' >&2\nexit 1\n"), 0o755))
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := readRemoteRepoBranchFile("octo/repo", "experiments/missing", "state.jsonl", "")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReadRemoteRepoBranchFileContextRejectsUnsafePaths(t *testing.T) {
	for _, filePath := range []string{"../secret", "nested/../../secret", "/secret"} {
		t.Run(filePath, func(t *testing.T) {
			argsLog := installExperimentFetchFakeGH(t, "", "")

			_, err := readRemoteRepoBranchFileContext(context.Background(), "octo/repo", "main", filePath, "")

			require.ErrorContains(t, err, "invalid file path")
			require.NotErrorIs(t, err, os.ErrNotExist)
			_, err = os.Stat(argsLog)
			assert.ErrorIs(t, err, os.ErrNotExist, "unsafe paths must not invoke gh")
		})
	}
}

func TestRepositoryAPIArgsValidatesEndpoint(t *testing.T) {
	for _, endpoint := range []string{"contents/../secret", "contents/nested/../../secret", "/branches"} {
		t.Run(endpoint, func(t *testing.T) {
			args, err := repositoryAPIArgs("octo/repo", "", endpoint)

			require.ErrorContains(t, err, "invalid endpoint")
			assert.Nil(t, args)
		})
	}

	for _, endpoint := range []string{
		"branches/experiments%2F%7Bowner%7D+test%231",
		"contents/nested%20dir/evals%231.jsonl",
		"contents/literal%252e%252e/secret",
	} {
		t.Run(endpoint, func(t *testing.T) {
			args, err := repositoryAPIArgs("github.example.com/octo/repo", "", endpoint, "--paginate")

			require.NoError(t, err)
			assert.Equal(t, []string{"api", "repos/octo/repo/" + endpoint, "--paginate", "--hostname", "github.example.com"}, args)
		})
	}
}

func TestReadRemoteRepoBranchFileContextPreservesLiteralEscapes(t *testing.T) {
	argsLog := installExperimentFetchFakeGH(t, "", "")

	_, err := readRemoteRepoBranchFileContext(context.Background(), "octo/repo", "evals/literal+test#1",
		"nested/%2e%2e/evals.jsonl", "")

	require.NoError(t, err)
	assert.Equal(t, [][]string{
		{"api", "repos/octo/repo/contents/nested/%252e%252e/evals.jsonl",
			"--method", "GET", "--raw-field", "ref=evals/literal+test#1", "--jq", ".content"},
	}, readExperimentGHArgs(t, argsLog))
}
