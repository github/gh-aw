//go:build !integration

package workflow

import (
	"context"
	"os"
	"strings"
	"testing"
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
			if cmd.Path != "gh" && !strings.HasSuffix(cmd.Path, "/gh") {
				t.Errorf("Expected command path to be 'gh', got: %s", cmd.Path)
			}

			// Verify arguments
			if len(cmd.Args) != 3 || cmd.Args[1] != "api" || cmd.Args[2] != "/user" {
				t.Errorf("Expected args [gh api /user], got: %v", cmd.Args)
			}

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
				if !found {
					t.Errorf("Expected environment to contain %s, but it wasn't found", expectedEnv)
				}
			} else {
				// When GH_TOKEN is already set or neither token is set, cmd.Env should be nil (uses parent process env)
				if cmd.Env != nil {
					t.Errorf("Expected cmd.Env to be nil (inherit parent environment), got: %v", cmd.Env)
				}
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
	if cmd.Path != "gh" && !strings.HasSuffix(cmd.Path, "/gh") {
		t.Errorf("Expected command path to be 'gh', got: %s", cmd.Path)
	}

	// Verify all arguments are preserved
	expectedArgs := []string{"gh", "api", "repos/owner/repo/git/ref/tags/v1.0", "--jq", ".object.sha"}
	if len(cmd.Args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d: %v", len(expectedArgs), len(cmd.Args), cmd.Args)
	}

	for i, expected := range expectedArgs {
		if i >= len(cmd.Args) || cmd.Args[i] != expected {
			t.Errorf("Arg %d: expected %s, got %s", i, expected, cmd.Args[i])
		}
	}

	// Verify environment contains GH_TOKEN
	found := false
	for _, env := range cmd.Env {
		if env == "GH_TOKEN=test-token" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected environment to contain GH_TOKEN=test-token")
	}
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
			if cmd.Path != "gh" && !strings.HasSuffix(cmd.Path, "/gh") {
				t.Errorf("Expected command path to be 'gh', got: %s", cmd.Path)
			}

			// Verify arguments
			if len(cmd.Args) != 3 || cmd.Args[1] != "api" || cmd.Args[2] != "/user" {
				t.Errorf("Expected args [gh api /user], got: %v", cmd.Args)
			}

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
				if !found {
					t.Errorf("Expected environment to contain %s, but it wasn't found", expectedEnv)
				}
			} else {
				if cmd.Env != nil {
					t.Errorf("Expected cmd.Env to be nil (inherit parent environment), got: %v", cmd.Env)
				}
			}
		})
	}
}
