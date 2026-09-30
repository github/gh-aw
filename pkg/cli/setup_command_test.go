//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSetupAuthWithRuntime(t *testing.T) {
	t.Parallel()
	called := 0
	err := runSetupAuthWithRuntime(SetupAuthOptions{Ctx: context.Background()}, setupRepositoryRuntime{
		checkAuth: func(context.Context) error {
			called++
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, called)
}

func TestRunSetupAuthWithRuntime_JSONOutput(t *testing.T) {
	output := captureSetupStdout(t, func() error {
		return runSetupAuthWithRuntime(SetupAuthOptions{Ctx: context.Background(), JSON: true}, setupRepositoryRuntime{
			checkAuth: func(context.Context) error { return nil },
		})
	})

	var result SetupAuthResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result.Authenticated)
}

func TestRunSetupRepositoryCheck_AttachedCheckout(t *testing.T) {
	// The attached checkout path changes the process-wide working directory.
	repoDir := initBootstrapGitRepo(t)
	err := runSetupRepositoryCheckWithRuntime(normalizeSetupRepositoryCheckOptions(SetupRepositoryCheckOptions{
		Ctx:  context.Background(),
		Repo: "octo/platform-ops",
		Dir:  repoDir,
	}), setupRepositoryRuntime{
		checkAuth:          func(context.Context) error { return nil },
		ownerType:          func(context.Context, string) (string, error) { return "Organization", nil },
		repoExists:         func(context.Context, string) (bool, error) { return true, nil },
		dirOriginRepo:      func(string) (string, error) { return "octo/platform-ops", nil },
		checkCleanWorktree: func(bool) error { return nil },
	})
	require.NoError(t, err)
}

func TestRunSetupRepositoryCheck_JSONOutput(t *testing.T) {
	repoDir := initBootstrapGitRepo(t)
	output := captureSetupStdout(t, func() error {
		return runSetupRepositoryCheckWithRuntime(normalizeSetupRepositoryCheckOptions(SetupRepositoryCheckOptions{
			Ctx:              context.Background(),
			Repo:             "octo/platform-ops",
			Dir:              repoDir,
			RequireOwnerType: "org",
			JSON:             true,
		}), setupRepositoryRuntime{
			checkAuth:          func(context.Context) error { return nil },
			ownerType:          func(context.Context, string) (string, error) { return "Organization", nil },
			repoExists:         func(context.Context, string) (bool, error) { return true, nil },
			dirOriginRepo:      func(string) (string, error) { return "octo/platform-ops", nil },
			checkCleanWorktree: func(bool) error { return nil },
		})
	})

	var result SetupRepositoryCheckResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Equal(t, "octo/platform-ops", result.Repository)
	assert.Equal(t, repoDir, result.Directory)
	assert.True(t, result.Authenticated)
	assert.True(t, result.RepositoryExists)
	assert.Equal(t, "org", result.OwnerType)
	assert.Equal(t, "org", result.RequiredOwnerType)
	assert.True(t, result.CheckoutAttached)
	assert.False(t, result.CloneNeeded)
	require.NotNil(t, result.CleanWorktree)
	assert.True(t, *result.CleanWorktree)
}

func TestRunSetupRepositoryCheck_EnforcesOwnerTypeRequirement(t *testing.T) {
	t.Parallel()
	repoDir := initBootstrapGitRepo(t)
	err := runSetupRepositoryCheckWithRuntime(normalizeSetupRepositoryCheckOptions(SetupRepositoryCheckOptions{
		Ctx:              context.Background(),
		Repo:             "octo/platform-ops",
		Dir:              repoDir,
		RequireOwnerType: "user",
	}), setupRepositoryRuntime{
		checkAuth:          func(context.Context) error { return nil },
		ownerType:          func(context.Context, string) (string, error) { return "Organization", nil },
		repoExists:         func(context.Context, string) (bool, error) { return true, nil },
		dirOriginRepo:      func(string) (string, error) { return "octo/platform-ops", nil },
		checkCleanWorktree: func(bool) error { return nil },
	})
	require.Error(t, err)
	assert.Equal(t, "owner octo is org, but --require-owner-type=user was requested", err.Error())
}

func TestRunSetupRepositoryCheck_RequiresExistingRepository(t *testing.T) {
	t.Parallel()
	err := runSetupRepositoryCheckWithRuntime(normalizeSetupRepositoryCheckOptions(SetupRepositoryCheckOptions{
		Ctx:  context.Background(),
		Repo: "octo/platform-ops",
	}), setupRepositoryRuntime{
		checkAuth:  func(context.Context) error { return nil },
		ownerType:  func(context.Context, string) (string, error) { return "Organization", nil },
		repoExists: func(context.Context, string) (bool, error) { return false, nil },
	})
	require.Error(t, err)
	assert.Equal(t, "repository octo/platform-ops does not exist", err.Error())
}

func TestRunSetupRepositoryCheck_PropagatesCleanWorktreeError(t *testing.T) {
	// The attached checkout path changes the process-wide working directory.
	repoDir := initBootstrapGitRepo(t)
	wantErr := errors.New("working directory has uncommitted changes, please commit or stash them first")

	err := runSetupRepositoryCheckWithRuntime(normalizeSetupRepositoryCheckOptions(SetupRepositoryCheckOptions{
		Ctx:  context.Background(),
		Repo: "octo/platform-ops",
		Dir:  repoDir,
	}), setupRepositoryRuntime{
		checkAuth: func(context.Context) error { return nil },
		ownerType: func(context.Context, string) (string, error) {
			return "Organization", nil
		},
		repoExists:    func(context.Context, string) (bool, error) { return true, nil },
		dirOriginRepo: func(string) (string, error) { return "octo/platform-ops", nil },
		checkCleanWorktree: func(bool) error {
			return wantErr
		},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestRunSetupRepositoryCheck_RejectsNonExistentNestedCheckoutPath(t *testing.T) {
	t.Parallel()
	parentRepoDir := initBootstrapGitRepo(t)
	nestedDir := filepath.Join(parentRepoDir, "new-checkout")

	err := runSetupRepositoryCheckWithRuntime(normalizeSetupRepositoryCheckOptions(SetupRepositoryCheckOptions{
		Ctx:  context.Background(),
		Repo: "octo/platform-ops",
		Dir:  nestedDir,
	}), setupRepositoryRuntime{
		checkAuth:  func(context.Context) error { return nil },
		ownerType:  func(context.Context, string) (string, error) { return "Organization", nil },
		repoExists: func(context.Context, string) (bool, error) { return true, nil },
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "is inside a different git checkout rooted at")
	require.ErrorContains(t, err, parentRepoDir)
}

func TestCreateSetupRepository_UsesSupportedFlags(t *testing.T) {
	fakeBin := t.TempDir()
	argsLog := filepath.Join(fakeBin, "gh-args.log")
	fakeGH := filepath.Join(fakeBin, "gh")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + argsLog + "\"\n" +
		"if [ \"$1\" = \"repo\" ] && [ \"$2\" = \"create\" ]; then\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(script), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	require.NoError(t, createSetupRepository(context.Background(), "octo/platform-ops", "private"))

	logData, err := os.ReadFile(argsLog)
	require.NoError(t, err)
	logText := string(logData)
	assert.Contains(t, logText, "repo create octo/platform-ops --private")
	assert.NotContains(t, logText, "--confirm")
	assert.NotContains(t, logText, "--clone=false")
}

func TestCheckSetupRepositoryOwnerType_FallsBackToOrgsEndpoint(t *testing.T) {
	fakeBin := t.TempDir()
	fakeGH := filepath.Join(fakeBin, "gh")
	script := `#!/bin/sh
if [ "$1" = "api" ] && [ "$2" = "users/octo" ]; then
  echo "Not Found" >&2
  exit 1
fi
if [ "$1" = "api" ] && [ "$2" = "orgs/octo" ]; then
  echo "Organization"
  exit 0
fi
exit 1
`
	require.NoError(t, os.WriteFile(fakeGH, []byte(script), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ownerType, err := checkSetupRepositoryOwnerType(context.Background(), "octo")
	require.NoError(t, err)
	assert.Equal(t, "Organization", ownerType)
}

func TestRunSetupRepositoryCheck_AcceptsCaseInsensitiveSlugMatch(t *testing.T) {
	t.Parallel()
	repoDir := initBootstrapGitRepo(t)
	err := runSetupRepositoryCheckWithRuntime(normalizeSetupRepositoryCheckOptions(SetupRepositoryCheckOptions{
		Ctx:  context.Background(),
		Repo: "octo/platform-ops",
		Dir:  repoDir,
	}), setupRepositoryRuntime{
		checkAuth:          func(context.Context) error { return nil },
		ownerType:          func(context.Context, string) (string, error) { return "Organization", nil },
		repoExists:         func(context.Context, string) (bool, error) { return true, nil },
		dirOriginRepo:      func(string) (string, error) { return "Octo/Platform-Ops", nil },
		checkCleanWorktree: func(bool) error { return nil },
	})
	require.NoError(t, err)
}

func TestValidateSetupRepositoryCheckOptions_RejectsEmptyRepoComponents(t *testing.T) {
	t.Parallel()
	tests := []SetupRepositoryCheckOptions{
		{Repo: "/"},
		{Repo: "/repo"},
		{Repo: "owner/"},
	}

	for _, tt := range tests {
		if err := validateSetupRepositoryCheckOptions(tt); err == nil {
			t.Fatalf("expected invalid repo slug error for %q", tt.Repo)
		}
	}
}

func captureSetupStdout(t *testing.T, fn func() error) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := fn()
	require.NoError(t, runErr)

	require.NoError(t, w.Close())
	os.Stdout = oldStdout
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(data)
}
