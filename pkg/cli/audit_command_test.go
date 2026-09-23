//go:build !integration

package cli

import (
	"testing"

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
			name:       "empty flag leaves components untouched",
			repoFlag:   "",
			components: parser.GitHubURLComponents{},
		},
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
			err := applyAuditRepoFlag(tt.repoFlag, &components)
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
