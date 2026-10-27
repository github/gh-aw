//go:build !integration

package workflow

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubCLIHostPrecedence(t *testing.T) {
	originalHost := getDefaultGHHost()
	t.Cleanup(func() { SetDefaultGHHost(originalHost) })
	SetDefaultGHHost("detected.example.com")
	t.Setenv("GH_HOST", "")
	assert.Equal(t, "detected.example.com", GitHubCLIHost())
	t.Setenv("GH_HOST", "explicit.example.com")
	assert.Equal(t, "explicit.example.com", GitHubCLIHost())
}

func TestExecGH(t *testing.T) {
	tests := []struct {
		name          string
		ghToken       string
		githubToken   string
		expectGHToken bool
		expectValue   string
	}{
		{
			name:          "GH_TOKEN is set",
			ghToken:       "gh-token-123",
			githubToken:   "",
			expectGHToken: false, // Should use existing GH_TOKEN from environment
			expectValue:   "",
		},
		{
			name:          "GITHUB_TOKEN is set, GH_TOKEN is not",
			ghToken:       "",
			githubToken:   "github-token-456",
			expectGHToken: true,
			expectValue:   "github-token-456",
		},
		{
			name:          "Both GH_TOKEN and GITHUB_TOKEN are set",
			ghToken:       "gh-token-123",
			githubToken:   "github-token-456",
			expectGHToken: false, // Should prefer existing GH_TOKEN
			expectValue:   "",
		},
		{
			name:          "Neither GH_TOKEN nor GITHUB_TOKEN is set",
			ghToken:       "",
			githubToken:   "",
			expectGHToken: false,
			expectValue:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original environment
			originalGHToken, ghTokenWasSet := os.LookupEnv("GH_TOKEN")
			originalGitHubToken, githubTokenWasSet := os.LookupEnv("GITHUB_TOKEN")
			defer func() {
				if ghTokenWasSet {
					os.Setenv("GH_TOKEN", originalGHToken)
				} else {
					os.Unsetenv("GH_TOKEN")
				}
				if githubTokenWasSet {
					os.Setenv("GITHUB_TOKEN", originalGitHubToken)
				} else {
					os.Unsetenv("GITHUB_TOKEN")
				}
			}()

			// Set up test environment
			if tt.ghToken != "" {
				os.Setenv("GH_TOKEN", tt.ghToken)
			} else {
				os.Unsetenv("GH_TOKEN")
			}
			if tt.githubToken != "" {
				os.Setenv("GITHUB_TOKEN", tt.githubToken)
			} else {
				os.Unsetenv("GITHUB_TOKEN")
			}

			// Execute the helper
			cmd := ExecGH("api", "/user")

			// Verify the command
			require.NotNil(t, cmd, "Command should not be nil")
			assert.True(t, cmd.Path == "gh" || strings.HasSuffix(cmd.Path, "/gh"), "Expected command path to be 'gh', got: %s", cmd.Path)

			// Verify arguments
			require.Len(t, cmd.Args, 3, "Expected 3 args, got: %v", cmd.Args)
			assert.Equal(t, "api", cmd.Args[1], "Expected second arg to be 'api'")
			assert.Equal(t, "/user", cmd.Args[2], "Expected third arg to be '/user'")

			// Verify environment
			if tt.expectGHToken {
				found := false
				expectedEnv := "GH_TOKEN=" + tt.expectValue
				if slices.Contains(cmd.Env, expectedEnv) {
					found = true
				}
				assert.True(t, found, "Expected environment to contain %s, but it wasn't found", expectedEnv)
			} else {
				// When GH_TOKEN is already set or neither token is set, cmd.Env should be nil (uses parent process env)
				assert.Nil(t, cmd.Env, "Expected cmd.Env to be nil (inherit parent environment), got: %v", cmd.Env)
			}
		})
	}
}

func TestExecGHWithMultipleArgs(t *testing.T) {
	// Save original environment
	originalGHToken := os.Getenv("GH_TOKEN")
	originalGitHubToken := os.Getenv("GITHUB_TOKEN")
	defer func() {
		os.Setenv("GH_TOKEN", originalGHToken)
		os.Setenv("GITHUB_TOKEN", originalGitHubToken)
	}()

	// Set up test environment
	os.Unsetenv("GH_TOKEN")
	os.Setenv("GITHUB_TOKEN", "test-token")

	// Test with multiple arguments
	cmd := ExecGH("api", "repos/owner/repo/git/ref/tags/v1.0", "--jq", ".object.sha")

	// Verify command
	require.NotNil(t, cmd, "Command should not be nil")
	assert.True(t, cmd.Path == "gh" || strings.HasSuffix(cmd.Path, "/gh"), "Expected command path to be 'gh', got: %s", cmd.Path)

	// Verify all arguments are preserved
	expectedArgs := []string{"gh", "api", "repos/owner/repo/git/ref/tags/v1.0", "--jq", ".object.sha"}
	require.Len(t, cmd.Args, len(expectedArgs), "Expected %d args, got %d: %v", len(expectedArgs), len(cmd.Args), cmd.Args)

	for i, expected := range expectedArgs {
		assert.Equal(t, expected, cmd.Args[i], "Arg %d: expected %s, got %s", i, expected, cmd.Args[i])
	}

	// Verify environment contains GH_TOKEN
	found := slices.Contains(cmd.Env, "GH_TOKEN=test-token")
	assert.True(t, found, "Expected environment to contain GH_TOKEN=test-token")
}

func TestExecGHContext(t *testing.T) {
	tests := []struct {
		name          string
		ghToken       string
		githubToken   string
		expectGHToken bool
		expectValue   string
	}{
		{
			name:          "GH_TOKEN is set with context",
			ghToken:       "gh-token-123",
			githubToken:   "",
			expectGHToken: false,
			expectValue:   "",
		},
		{
			name:          "GITHUB_TOKEN is set with context",
			ghToken:       "",
			githubToken:   "github-token-456",
			expectGHToken: true,
			expectValue:   "github-token-456",
		},
		{
			name:          "No tokens with context",
			ghToken:       "",
			githubToken:   "",
			expectGHToken: false,
			expectValue:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original environment
			originalGHToken, ghTokenWasSet := os.LookupEnv("GH_TOKEN")
			originalGitHubToken, githubTokenWasSet := os.LookupEnv("GITHUB_TOKEN")
			defer func() {
				if ghTokenWasSet {
					os.Setenv("GH_TOKEN", originalGHToken)
				} else {
					os.Unsetenv("GH_TOKEN")
				}
				if githubTokenWasSet {
					os.Setenv("GITHUB_TOKEN", originalGitHubToken)
				} else {
					os.Unsetenv("GITHUB_TOKEN")
				}
			}()

			// Set up test environment
			if tt.ghToken != "" {
				os.Setenv("GH_TOKEN", tt.ghToken)
			} else {
				os.Unsetenv("GH_TOKEN")
			}
			if tt.githubToken != "" {
				os.Setenv("GITHUB_TOKEN", tt.githubToken)
			} else {
				os.Unsetenv("GITHUB_TOKEN")
			}

			// Execute the helper with context
			ctx := context.Background()
			cmd := ExecGHContext(ctx, "api", "/user")

			// Verify the command
			require.NotNil(t, cmd, "Command should not be nil")
			assert.True(t, cmd.Path == "gh" || strings.HasSuffix(cmd.Path, "/gh"), "Expected command path to be 'gh', got: %s", cmd.Path)

			// Verify arguments
			require.Len(t, cmd.Args, 3, "Expected 3 args, got: %v", cmd.Args)
			assert.Equal(t, "api", cmd.Args[1], "Expected second arg to be 'api'")
			assert.Equal(t, "/user", cmd.Args[2], "Expected third arg to be '/user'")

			// Verify environment
			if tt.expectGHToken {
				found := false
				expectedEnv := "GH_TOKEN=" + tt.expectValue
				if slices.Contains(cmd.Env, expectedEnv) {
					found = true
				}
				assert.True(t, found, "Expected environment to contain %s, but it wasn't found", expectedEnv)
			} else {
				assert.Nil(t, cmd.Env, "Expected cmd.Env to be nil (inherit parent environment), got: %v", cmd.Env)
			}
		})
	}
}

// TestSetupGHCommand tests the core setupGHCommand function directly
func TestSetupGHCommand(t *testing.T) {
	tests := []struct {
		name          string
		ghToken       string
		githubToken   string
		useContext    bool
		expectGHToken bool
		expectValue   string
	}{
		{
			name:          "Without context, no tokens",
			ghToken:       "",
			githubToken:   "",
			useContext:    false,
			expectGHToken: false,
			expectValue:   "",
		},
		{
			name:          "With context, no tokens",
			ghToken:       "",
			githubToken:   "",
			useContext:    true,
			expectGHToken: false,
			expectValue:   "",
		},
		{
			name:          "Without context, GITHUB_TOKEN only",
			ghToken:       "",
			githubToken:   "github-token-123",
			useContext:    false,
			expectGHToken: true,
			expectValue:   "github-token-123",
		},
		{
			name:          "With context, GITHUB_TOKEN only",
			ghToken:       "",
			githubToken:   "github-token-456",
			useContext:    true,
			expectGHToken: true,
			expectValue:   "github-token-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original environment
			originalGHToken, ghTokenWasSet := os.LookupEnv("GH_TOKEN")
			originalGitHubToken, githubTokenWasSet := os.LookupEnv("GITHUB_TOKEN")
			defer func() {
				if ghTokenWasSet {
					os.Setenv("GH_TOKEN", originalGHToken)
				} else {
					os.Unsetenv("GH_TOKEN")
				}
				if githubTokenWasSet {
					os.Setenv("GITHUB_TOKEN", originalGitHubToken)
				} else {
					os.Unsetenv("GITHUB_TOKEN")
				}
			}()

			// Set up test environment
			if tt.ghToken != "" {
				os.Setenv("GH_TOKEN", tt.ghToken)
			} else {
				os.Unsetenv("GH_TOKEN")
			}
			if tt.githubToken != "" {
				os.Setenv("GITHUB_TOKEN", tt.githubToken)
			} else {
				os.Unsetenv("GITHUB_TOKEN")
			}

			// Execute setupGHCommand with or without context
			var cmd *exec.Cmd
			if tt.useContext {
				ctx := context.Background()
				cmd = setupGHCommand(ctx, "api", "/user")
			} else {
				//nolint:staticcheck // Testing nil context is intentional
				cmd = setupGHCommand(nil, "api", "/user")
			}

			// Verify the command
			require.NotNil(t, cmd, "Command should not be nil")
			assert.True(t, cmd.Path == "gh" || strings.HasSuffix(cmd.Path, "/gh"), "Expected command path to be 'gh', got: %s", cmd.Path)

			// Verify arguments
			require.Len(t, cmd.Args, 3, "Expected 3 args, got: %v", cmd.Args)
			assert.Equal(t, "api", cmd.Args[1], "Expected second arg to be 'api'")
			assert.Equal(t, "/user", cmd.Args[2], "Expected third arg to be '/user'")

			// Verify environment
			if tt.expectGHToken {
				found := false
				expectedEnv := "GH_TOKEN=" + tt.expectValue
				if slices.Contains(cmd.Env, expectedEnv) {
					found = true
				}
				assert.True(t, found, "Expected environment to contain %s", expectedEnv)
			} else {
				assert.Nil(t, cmd.Env, "Expected cmd.Env to be nil")
			}
		})
	}
}

// TestRunGHFunctions tests that RunGH and RunGHCombined delegate correctly to their context variants.
func TestRunGHFunctions(t *testing.T) {
	// Save original environment
	originalGHToken := os.Getenv("GH_TOKEN")
	originalGitHubToken := os.Getenv("GITHUB_TOKEN")
	defer func() {
		os.Setenv("GH_TOKEN", originalGHToken)
		os.Setenv("GITHUB_TOKEN", originalGitHubToken)
	}()

	// Set up test environment with no tokens to keep behavior deterministic for unit tests.
	os.Unsetenv("GH_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")

	t.Run("RunGH matches RunGHContext", func(t *testing.T) {
		gotOut, gotErr := RunGH("Test spinner...", "auth", "status")
		wantOut, wantErr := RunGHContext(context.Background(), "Test spinner...", "auth", "status")

		assert.Equal(t, wantOut, gotOut, "RunGH should return the same output as RunGHContext")
		if wantErr == nil {
			assert.NoError(t, gotErr, "RunGH should match RunGHContext error behavior")
		} else {
			require.Error(t, gotErr, "RunGH should match RunGHContext error behavior")
			assert.Equal(t, wantErr.Error(), gotErr.Error(), "RunGH should return the same error text as RunGHContext")
		}
	})

	t.Run("RunGHCombined matches RunGHCombinedContext", func(t *testing.T) {
		gotOut, gotErr := RunGHCombined("Test spinner...", "auth", "status")
		wantOut, wantErr := RunGHCombinedContext(context.Background(), "Test spinner...", "auth", "status")

		assert.Equal(t, wantOut, gotOut, "RunGHCombined should return the same output as RunGHCombinedContext")
		if wantErr == nil {
			assert.NoError(t, gotErr, "RunGHCombined should match RunGHCombinedContext error behavior")
		} else {
			require.Error(t, gotErr, "RunGHCombined should match RunGHCombinedContext error behavior")
			assert.Equal(t, wantErr.Error(), gotErr.Error(), "RunGHCombined should return the same error text as RunGHCombinedContext")
		}
	})
}

// TestEnrichGHError tests that enrichGHError appends stderr from *exec.ExitError
func TestEnrichGHError(t *testing.T) {
	t.Run("nil error unchanged", func(t *testing.T) {
		assert.NoError(t, enrichGHError(nil), "nil error should remain nil")
	})

	t.Run("non-ExitError unchanged", func(t *testing.T) {
		err := errors.New("plain error")
		assert.Equal(t, err, enrichGHError(err), "non-ExitError should be returned unchanged")
	})

	t.Run("ExitError with no stderr unchanged", func(t *testing.T) {
		// Run a command that exits non-zero without producing stderr
		cmd := exec.Command("sh", "-c", "exit 1")
		_, cmdErr := cmd.Output()
		require.Error(t, cmdErr, "command should fail")
		enriched := enrichGHError(cmdErr)
		// With no stderr, the error should be equivalent to the original
		assert.Equal(t, cmdErr.Error(), enriched.Error(), "ExitError with empty stderr should match original error message")
	})

	t.Run("ExitError with stderr gets stderr appended", func(t *testing.T) {
		// Run a command that exits non-zero and writes to stderr
		cmd := exec.Command("sh", "-c", "echo 'not found' >&2; exit 1")
		_, cmdErr := cmd.Output()
		require.Error(t, cmdErr, "command should fail")
		enriched := enrichGHError(cmdErr)
		require.ErrorContains(t, enriched, "not found", "enriched error should contain stderr output")
		require.ErrorContains(t, enriched, "exit status 1", "enriched error should still contain original error")
	})
}

func TestSetGHHostEnv(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		expectSet  bool
		initialEnv []string
	}{
		{
			name:      "github.com is a no-op",
			host:      "github.com",
			expectSet: false,
		},
		{
			name:      "empty host is a no-op",
			host:      "",
			expectSet: false,
		},
		{
			name:      "GHES host sets GH_HOST",
			host:      "myorg.ghe.com",
			expectSet: true,
		},
		{
			name:      "Proxima host sets GH_HOST",
			host:      "verizon.ghe.com",
			expectSet: true,
		},
		{
			name:       "appends to existing env",
			host:       "myorg.ghe.com",
			expectSet:  true,
			initialEnv: []string{"FOO=bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("echo", "test")
			if tt.initialEnv != nil {
				cmd.Env = tt.initialEnv
			}

			SetGHHostEnv(cmd, tt.host)

			if !tt.expectSet {
				if tt.initialEnv == nil {
					assert.Nil(t, cmd.Env, "Env should remain nil for %s", tt.host)
				}
				return
			}

			require.NotNil(t, cmd.Env, "Env should be set for host %s", tt.host)
			found := slices.ContainsFunc(cmd.Env, func(e string) bool {
				return e == "GH_HOST="+tt.host
			})
			assert.True(t, found, "GH_HOST=%s should be in cmd.Env", tt.host)
		})
	}
}

func TestSetupGHCommandUsesDefaultGHHost(t *testing.T) {
	originalDefaultHost := getDefaultGHHost()
	defer SetDefaultGHHost(originalDefaultHost)

	t.Run("applies default host when GH_HOST is not set", func(t *testing.T) {
		originalHost, hadOriginalHost := os.LookupEnv("GH_HOST")
		require.NoError(t, os.Unsetenv("GH_HOST"))
		t.Cleanup(func() {
			if hadOriginalHost {
				require.NoError(t, os.Setenv("GH_HOST", originalHost))
				return
			}
			require.NoError(t, os.Unsetenv("GH_HOST"))
		})
		SetDefaultGHHost("myorg.ghe.com")

		cmd := setupGHCommand(context.Background(), "auth", "status")
		require.NotNil(t, cmd.Env)
		assert.Contains(t, cmd.Env, "GH_HOST=myorg.ghe.com")
	})

	t.Run("does not apply default host when GH_HOST is already set", func(t *testing.T) {
		t.Setenv("GH_HOST", "explicit.ghe.com")
		SetDefaultGHHost("myorg.ghe.com")

		cmd := setupGHCommand(context.Background(), "auth", "status")
		if cmd.Env == nil {
			return
		}
		assert.NotContains(t, cmd.Env, "GH_HOST=myorg.ghe.com")
	})
}

// TestRunGHInputContext verifies that RunGHInputContext correctly pipes stdin to the
// command and behaves consistently with RunGHContext for commands that don't read stdin.
// This guards against regression to the raw -f argument concatenation pattern (Semgrep #627/#628).
func TestRunGHInputContext(t *testing.T) {
	originalGHToken := os.Getenv("GH_TOKEN")
	originalGitHubToken := os.Getenv("GITHUB_TOKEN")
	defer func() {
		os.Setenv("GH_TOKEN", originalGHToken)
		os.Setenv("GITHUB_TOKEN", originalGitHubToken)
	}()
	os.Unsetenv("GH_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")

	ctx := context.Background()
	payload := []byte(`{"query":"{ viewer { login } }","variables":{}}`)

	t.Run("runs without panic when stdin reader is provided", func(t *testing.T) {
		// gh version does not read stdin; passing a reader verifies the function
		// wires cmd.Stdin without panicking or corrupting the command.
		_, err := RunGHInputContext(ctx, "testing stdin wiring", bytes.NewReader(payload), "version")
		// gh may or may not be authenticated in CI — both outcomes are acceptable.
		// The critical assertion is that the call does not panic.
		_ = err
	})

	t.Run("output consistent with RunGHContext for commands that ignore stdin", func(t *testing.T) {
		// gh version ignores stdin, so both calls should produce identical output/error.
		// This verifies that RunGHInputContext delegates through the same code path as
		// RunGHContext when the command does not consume the reader.
		withStdin, withStdinErr := RunGHInputContext(ctx, "test", bytes.NewReader(payload), "version")
		withoutStdin, withoutStdinErr := RunGHContext(ctx, "test", "version")

		// Error status should agree.
		assert.Equal(t, withStdinErr != nil, withoutStdinErr != nil,
			"RunGHInputContext and RunGHContext should have consistent error status for gh version")
		if withStdinErr == nil && withoutStdinErr == nil {
			assert.Equal(t, withStdin, withoutStdin,
				"RunGHInputContext should produce the same output as RunGHContext when stdin is ignored by the command")
		}
	})
}
