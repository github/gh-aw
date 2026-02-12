//go:build !integration

package workflow

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
				for _, env := range cmd.Env {
					if env == expectedEnv {
						found = true
						break
					}
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
	found := false
	for _, env := range cmd.Env {
		if env == "GH_TOKEN=test-token" {
			found = true
			break
		}
	}
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
				for _, env := range cmd.Env {
					if env == expectedEnv {
						found = true
						break
					}
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
				for _, env := range cmd.Env {
					if env == expectedEnv {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected environment to contain %s", expectedEnv)
			} else {
				assert.Nil(t, cmd.Env, "Expected cmd.Env to be nil")
			}
		})
	}
}

// TestRunGHWithSpinner tests the core runGHWithSpinner function
// Note: This test validates the function exists and handles arguments correctly
// Actual spinner behavior is tested via RunGH and RunGHCombined
func TestRunGHWithSpinnerHelperExists(t *testing.T) {
	// Save original environment
	originalGHToken := os.Getenv("GH_TOKEN")
	originalGitHubToken := os.Getenv("GITHUB_TOKEN")
	defer func() {
		os.Setenv("GH_TOKEN", originalGHToken)
		os.Setenv("GITHUB_TOKEN", originalGitHubToken)
	}()

	// Set up test environment - no tokens so command won't actually execute
	os.Unsetenv("GH_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")

	// Test that the function exists and can be called
	// We use a command that will fail quickly without credentials
	// to verify the integration works
	tests := []struct {
		name     string
		combined bool
	}{
		{
			name:     "Test stdout mode",
			combined: false,
		},
		{
			name:     "Test combined mode",
			combined: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the function can be called
			// We expect it to fail since gh command requires auth
			_, err := runGHWithSpinner("Test spinner...", tt.combined, "auth", "status")
			// We don't care about the error - we just want to verify the function exists
			// and doesn't panic when called
			_ = err
		})
	}
}
