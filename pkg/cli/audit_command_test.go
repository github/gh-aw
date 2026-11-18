//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyAuditRepoFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		repoFlag      string
		components    parser.GitHubURLComponents
		expectedOwner string
		expectedRepo  string
		expectError   bool
	}{
		{
			name:          "owner already resolved from URL wins",
			repoFlag:      "flag-owner/flag-repo",
			components:    parser.GitHubURLComponents{Owner: "url-owner", Repo: "url-repo"},
			expectedOwner: "url-owner",
			expectedRepo:  "url-repo",
		},
		{
			name:          "flag populates owner and repo",
			repoFlag:      "octo/repo",
			components:    parser.GitHubURLComponents{},
			expectedOwner: "octo",
			expectedRepo:  "repo",
		},
		{
			name:        "missing slash is rejected",
			repoFlag:    "octo",
			components:  parser.GitHubURLComponents{},
			expectError: true,
		},
		{
			name:        "empty owner is rejected",
			repoFlag:    "/repo",
			components:  parser.GitHubURLComponents{},
			expectError: true,
		},
		{
			name:        "empty repo is rejected",
			repoFlag:    "octo/",
			components:  parser.GitHubURLComponents{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			components := tt.components
			err := applyAuditRepoFlag(t.Context(), tt.repoFlag, &components)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedOwner, components.Owner)
			assert.Equal(t, tt.expectedRepo, components.Repo)
		})
	}
}

func TestApplyAuditRepoFlagDetectsCurrentRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh uses a shell script")
	}
	for _, tc := range []struct {
		name, ghResponse, origin, expectedOwner string
	}{
		{name: "gh selected repository", ghResponse: "printf 'selected/repo\\n'", expectedOwner: "selected"},
		{name: "local origin fallback", ghResponse: "exit 1", origin: "https://github.com/local/repo.git", expectedOwner: "local"},
		{name: "no repository available", ghResponse: "exit 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("GH_REPO", "")
			t.Setenv("GH_HOST", "github.com")
			t.Setenv("GITHUB_SERVER_URL", "https://github.com")
			ClearCurrentRepoSlugCache()
			t.Cleanup(ClearCurrentRepoSlugCache)
			require.NoError(t, exec.Command("git", "init", "--quiet", "--template=").Run())
			if tc.origin != "" {
				require.NoError(t, exec.Command("git", "remote", "add", "origin", tc.origin).Run())
			}
			bin := filepath.Join(dir, "bin")
			require.NoError(t, os.Mkdir(bin, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\n"+tc.ghResponse+"\n"), 0o700))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			components, err := parser.ParseRunURLExtended("12345")
			require.NoError(t, err)
			err = applyAuditRepoFlag(t.Context(), "", components)
			if tc.expectedOwner == "" {
				require.ErrorContains(t, err, "failed to detect current repository")
				require.ErrorContains(t, err, "--repo owner/repo")
				require.ErrorContains(t, err, "run URL")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedOwner, components.Owner)
			assert.Equal(t, "repo", components.Repo)
			assert.Equal(t, int64(12345), components.Number)

			requests, err := resolveGroupedAuditRunRequests(t.Context(), []string{"12345", "12346"}, "")
			require.NoError(t, err)
			require.Len(t, requests, 2)
			for _, request := range requests {
				assert.Equal(t, tc.expectedOwner, request.owner)
				assert.Equal(t, "repo", request.repo)
			}
		})
	}
}

func TestAuditRepositoryDetectionCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh and git use shell scripts")
	}
	for _, mode := range []string{"single", "diff", "grouped"} {
		for _, lookup := range []string{"gh", "git", "already canceled"} {
			t.Run(mode+"/"+lookup, func(t *testing.T) {
				dir := t.TempDir()
				marker := filepath.Join(dir, "git-called")
				t.Setenv("AUDIT_TEST_GIT_MARKER", marker)
				t.Setenv("GH_REPO", "")
				ghScript := "#!/bin/sh\nexec sleep 10\n"
				if lookup == "git" {
					ghScript = "#!/bin/sh\nexit 1\n"
				}
				require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(ghScript), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\necho called > \"$AUDIT_TEST_GIT_MARKER\"\nexec sleep 10\n"), 0o700))
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

				cmd := NewAuditCommand()
				ctx := t.Context()
				expectedErr := context.DeadlineExceeded
				if lookup == "already canceled" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
					expectedErr = context.Canceled
				}
				cmd.SetContext(ctx)
				require.NoError(t, cmd.Flags().Set("timeout", "100ms"))
				args := []string{"12345"}
				if mode != "single" {
					args = append(args, "12346")
				}
				if mode == "grouped" {
					require.NoError(t, cmd.Flags().Set("group", "true"))
				}

				start := time.Now()
				err := runAuditCommand(cmd, args)
				require.ErrorIs(t, err, expectedErr)
				assert.Less(t, time.Since(start), 2*time.Second, "repository lookup must honor the audit deadline")
				if lookup == "git" {
					assert.FileExists(t, marker)
				} else {
					assert.NoFileExists(t, marker, "cancellation must prevent fallback to git")
				}
			})
		}
	}
}

func TestResolveAuditCommandArgs(t *testing.T) {
	t.Parallel()
	t.Run("positional args pass through", func(t *testing.T) {
		t.Parallel()
		args, handled, err := resolveAuditCommandArgs([]string{"123"}, false)
		require.NoError(t, err)
		assert.False(t, handled)
		assert.Equal(t, []string{"123"}, args)
	})

	t.Run("no args without stdin is an error", func(t *testing.T) {
		t.Parallel()
		_, handled, err := resolveAuditCommandArgs(nil, false)
		require.Error(t, err)
		assert.False(t, handled)
	})

	t.Run("stdin with positional args is an error", func(t *testing.T) {
		t.Parallel()
		_, handled, err := resolveAuditCommandArgs([]string{"123"}, true)
		require.Error(t, err)
		assert.False(t, handled)
	})
}

func TestGetAuditCommandOptions(t *testing.T) {
	t.Parallel()
	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		cmd := NewAuditCommand()
		opts, err := getAuditCommandOptions(cmd)
		require.NoError(t, err)
		assert.Equal(t, defaultLogsOutputDir, opts.outputDir)
		assert.Equal(t, "pretty", opts.format)
		assert.False(t, opts.parse)
		assert.False(t, opts.evalsOnly)
		assert.False(t, opts.group)
		assert.False(t, opts.noBaseline)
		assert.Empty(t, opts.artifacts)
	})

	t.Run("no baseline", func(t *testing.T) {
		t.Parallel()
		cmd := NewAuditCommand()
		require.NoError(t, cmd.Flags().Set("no-baseline", "true"))
		opts, err := getAuditCommandOptions(cmd)
		require.NoError(t, err)
		assert.True(t, opts.noBaseline)
	})

	t.Run("group", func(t *testing.T) {
		t.Parallel()
		cmd := NewAuditCommand()
		require.NoError(t, cmd.Flags().Set("group", "true"))
		opts, err := getAuditCommandOptions(cmd)
		require.NoError(t, err)
		assert.True(t, opts.group)
	})

	t.Run("evals with narrowed artifacts adds the usage artifact set", func(t *testing.T) {
		t.Parallel()
		cmd := NewAuditCommand()
		require.NoError(t, cmd.Flags().Set("evals", "true"))
		require.NoError(t, cmd.Flags().Set("artifacts", "agent"))
		opts, err := getAuditCommandOptions(cmd)
		require.NoError(t, err)
		assert.True(t, opts.evalsOnly)
		assert.Equal(t, applyEvalsArtifact([]string{"agent"}, true), opts.artifacts)
	})

	t.Run("evals with default artifacts keeps the default set", func(t *testing.T) {
		t.Parallel()
		cmd := NewAuditCommand()
		require.NoError(t, cmd.Flags().Set("evals", "true"))
		opts, err := getAuditCommandOptions(cmd)
		require.NoError(t, err)
		assert.Empty(t, opts.artifacts)
	})

	t.Run("variant without experiment is rejected", func(t *testing.T) {
		t.Parallel()
		cmd := NewAuditCommand()
		require.NoError(t, cmd.Flags().Set("variant", "b"))
		_, err := getAuditCommandOptions(cmd)
		require.Error(t, err)
	})

	t.Run("invalid runtime is rejected", func(t *testing.T) {
		t.Parallel()
		cmd := NewAuditCommand()
		require.NoError(t, cmd.Flags().Set("runtime", "not-a-runtime"))
		_, err := getAuditCommandOptions(cmd)
		require.Error(t, err)
	})
}

func TestRegisterAuditCommandFlags(t *testing.T) {
	t.Parallel()
	cmd := NewAuditCommand()
	for _, name := range []string{"output", "json", "repo", "parse", "format", "artifacts", "stdin", "experiment", "variant", "runtime", "evals", "group", "no-baseline"} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "expected flag %q to be registered", name)
	}
}
