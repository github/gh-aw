//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestGitHubRemoteModeConfiguration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "github-remote-test")

	compiler := NewCompiler()

	tests := []struct {
		name          string
		frontmatter   string
		expectedType  string // "remote" or "local"
		expectedURL   string
		expectedToken string
		engineType    string
	}{
		{
			name: "Remote mode with default token",
			frontmatter: `---
on: issues
permissions:
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
tools:
  github:
    mode: remote
    toolsets: [issues]
---`,
			expectedType:  "remote",
			expectedURL:   "https://api.githubcopilot.com/mcp/",
			expectedToken: "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			engineType:    "claude",
		},
		{
			name: "Remote mode with custom token",
			frontmatter: `---
on: issues
permissions:
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
tools:
  github:
    mode: remote
    github-token: "${{ secrets.CUSTOM_PAT }}"
    toolsets: [issues]
---`,
			expectedType:  "remote",
			expectedURL:   "https://api.githubcopilot.com/mcp/",
			expectedToken: "${{ secrets.CUSTOM_PAT }}",
			engineType:    "claude",
		},
		{
			name: "Remote mode with read-only",
			frontmatter: `---
on: issues
permissions:
  issues: read
engine: claude
strict: false
tools:
  github:
    mode: remote
    read-only: true
    toolsets: [issues]
---`,
			expectedType:  "remote",
			expectedURL:   "https://api.githubcopilot.com/mcp/",
			expectedToken: "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			engineType:    "claude",
		},
		{
			name: "Local mode (default)",
			frontmatter: `---
on: issues
permissions:
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
tools:
  github:
    toolsets: [issues]
---`,
			expectedType:  "local",
			expectedURL:   "",
			expectedToken: "",
			engineType:    "claude",
		},
		{
			name: "Copilot remote mode with default token",
			frontmatter: `---
on: issues
permissions:
  issues: write
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
tools:
  github:
    mode: remote
    toolsets: [issues]
---`,
			expectedType:  "remote",
			expectedURL:   "https://api.githubcopilot.com/mcp/",
			expectedToken: "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			engineType:    "copilot",
		},
		{
			name: "Copilot remote mode with read-only",
			frontmatter: `---
on: issues
permissions:
  issues: read
engine: copilot
strict: false
tools:
  github:
    mode: remote
    read-only: true
    toolsets: [issues]
---`,
			expectedType:  "remote",
			expectedURL:   "https://api.githubcopilot.com/mcp/",
			expectedToken: "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			engineType:    "copilot",
		},
		{
			name: "Codex remote mode with default token",
			frontmatter: `---
on: issues
permissions:
  issues: write
engine: codex
features:
  dangerous-permissions-write: true
strict: false
tools:
  github:
    mode: remote
    read-only: false
    toolsets: [issues]
---`,
			expectedType:  "remote",
			expectedURL:   "https://api.githubcopilot.com/mcp/",
			expectedToken: "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			engineType:    "codex",
		},
		{
			name: "Codex remote mode with custom token",
			frontmatter: `---
on: issues
permissions:
  issues: write
engine: codex
features:
  dangerous-permissions-write: true
strict: false
tools:
  github:
    mode: remote
    read-only: false
    github-token: "${{ secrets.CUSTOM_PAT }}"
    toolsets: [issues]
---`,
			expectedType:  "remote",
			expectedURL:   "https://api.githubcopilot.com/mcp/",
			expectedToken: "${{ secrets.CUSTOM_PAT }}",
			engineType:    "codex",
		},
		{
			name: "Codex remote mode with read-only",
			frontmatter: `---
on: issues
permissions:
  issues: read
engine: codex
strict: false
tools:
  github:
    mode: remote
    read-only: true
    toolsets: [issues]
---`,
			expectedType:  "remote",
			expectedURL:   "https://api.githubcopilot.com/mcp-readonly/",
			expectedToken: "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			engineType:    "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test GitHub Remote Mode

This is a test workflow for GitHub remote mode configuration.
`

			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Unexpected error compiling workflow: %v", err)
			}

			// Replace the file extension to .lock.yml
			lockFile := stringutil.MarkdownToLockFile(testFile)
			// Read the generated lock file
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContent := string(content)

			// Check the MCP configuration based on expected type
			switch tt.expectedType {
			case "remote":
				// Codex uses TOML format, others use JSON
				if tt.engineType == "codex" {
					if tt.expectedURL != "" && !strings.Contains(lockContent, `url = "`+tt.expectedURL+`"`) {
						t.Errorf("Expected URL %s but didn't find it in:\n%s", tt.expectedURL, lockContent)
					}
					// Check for bearer_token_env_var instead of Authorization header
					if !strings.Contains(lockContent, `bearer_token_env_var = "GH_AW_GITHUB_TOKEN"`) {
						t.Errorf("Expected bearer_token_env_var but didn't find it in:\n%s", lockContent)
					}
					// For read-only mode, the endpoint URL should include /mcp-readonly/
					// No need to check for X-MCP-Readonly header since we use the endpoint URL
					// Should NOT contain old-style type = "http"
					if strings.Contains(lockContent, `type = "http"`) {
						t.Errorf("Expected no 'type = \"http\"' (old format) but found it in:\n%s", lockContent)
					}
					// Should NOT contain Docker configuration
					if strings.Contains(lockContent, `command = "docker"`) {
						t.Errorf("Expected no Docker command but found it in:\n%s", lockContent)
					}
				} else {
					// Check for JSON format
					if !strings.Contains(lockContent, `"type": "http"`) {
						t.Errorf("Expected HTTP configuration but didn't find 'type: http' in:\n%s", lockContent)
					}
					if tt.expectedURL != "" && !strings.Contains(lockContent, tt.expectedURL) {
						t.Errorf("Expected URL %s but didn't find it in:\n%s", tt.expectedURL, lockContent)
					}
					// For Copilot engine, check for new ${} syntax
					if tt.engineType == "copilot" {
						if !strings.Contains(lockContent, `"Authorization": "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}"`) {
							t.Errorf("Expected Authorization header with ${GITHUB_PERSONAL_ACCESS_TOKEN} syntax but didn't find it in:\n%s", lockContent)
						}
						if !strings.Contains(lockContent, `"GITHUB_PERSONAL_ACCESS_TOKEN": "\${GITHUB_MCP_SERVER_TOKEN}"`) {
							t.Errorf("Expected env section with GITHUB_PERSONAL_ACCESS_TOKEN passthrough but didn't find it in:\n%s", lockContent)
						}
					} else {
						// Security fix: For other engines, check for shell variable in Authorization header
						// and GitHub expression in env block
						if !strings.Contains(lockContent, `"Authorization": "Bearer $GITHUB_MCP_SERVER_TOKEN"`) {
							t.Errorf("Expected Authorization header with shell variable but didn't find it in:\n%s", lockContent)
						}
						if tt.expectedToken != "" {
							if !strings.Contains(lockContent, `GITHUB_MCP_SERVER_TOKEN: `+tt.expectedToken) {
								t.Errorf("Expected env block with token %s but didn't find it in:\n%s", tt.expectedToken, lockContent)
							}
						}
					}
					// Check for X-MCP-Readonly header if this is a read-only test
					if strings.Contains(tt.name, "read-only") {
						if !strings.Contains(lockContent, `"X-MCP-Readonly": "true"`) {
							t.Errorf("Expected X-MCP-Readonly header but didn't find it in:\n%s", lockContent)
						}
					}
					// Should NOT contain Docker configuration
					if strings.Contains(lockContent, `"command": "docker"`) {
						t.Errorf("Expected no Docker command but found it in:\n%s", lockContent)
					}
				}
			case "local":
				// Should contain Docker or local configuration
				switch tt.engineType {
				case "copilot":
					if !strings.Contains(lockContent, `"type": "stdio"`) {
						t.Errorf("Expected Copilot stdio type but didn't find it in:\n%s", lockContent)
					}
				case "codex":
					// Codex uses TOML format for Docker
					if !strings.Contains(lockContent, `command = "docker"`) {
						t.Errorf("Expected Docker command but didn't find it in:\n%s", lockContent)
					}
				default:
					// For Claude, check for container field in MCP gateway config
					if !strings.Contains(lockContent, `"container": "ghcr.io/github/github-mcp-server:`) {
						t.Errorf("Expected container field but didn't find it in:\n%s", lockContent)
					}
				}
				if !strings.Contains(lockContent, `ghcr.io/github/github-mcp-server:v0.30.3`) {
					t.Errorf("Expected Docker image but didn't find it in:\n%s", lockContent)
				}
				// Should NOT contain HTTP type
				if tt.engineType == "codex" {
					if strings.Contains(lockContent, `type = "http"`) {
						t.Errorf("Expected no HTTP type but found it in:\n%s", lockContent)
					}
					if strings.Contains(lockContent, `experimental_use_rmcp_client`) {
						t.Errorf("Expected no experimental_use_rmcp_client flag but found it in:\n%s", lockContent)
					}
				} else {
					if strings.Contains(lockContent, `"type": "http"`) {
						t.Errorf("Expected no HTTP type but found it in:\n%s", lockContent)
					}
				}
			}
		})
	}
}

func TestGitHubRemoteModeHelperFunctions(t *testing.T) {
	t.Run("getGitHubType extracts mode correctly", func(t *testing.T) {
		githubTool := map[string]any{
			"mode":    "remote",
			"allowed": []string{"list_issues"},
		}

		githubType := getGitHubType(githubTool)
		if githubType != "remote" {
			t.Errorf("Expected mode 'remote', got '%s'", githubType)
		}
	})

	t.Run("getGitHubType returns default local when no mode", func(t *testing.T) {
		githubTool := map[string]any{
			"allowed": []string{"list_issues"},
		}

		githubType := getGitHubType(githubTool)
		if githubType != "local" {
			t.Errorf("Expected default mode 'local', got '%s'", githubType)
		}
	})

	t.Run("getGitHubToken extracts custom token correctly", func(t *testing.T) {
		githubTool := map[string]any{
			"github-token": "${{ secrets.CUSTOM_PAT }}",
			"allowed":      []string{"list_issues"},
		}

		token := getGitHubToken(githubTool)
		if token != "${{ secrets.CUSTOM_PAT }}" {
			t.Errorf("Expected token '${{ secrets.CUSTOM_PAT }}', got '%s'", token)
		}
	})

	t.Run("getGitHubToken returns empty string when no token", func(t *testing.T) {
		githubTool := map[string]any{
			"allowed": []string{"list_issues"},
		}

		token := getGitHubToken(githubTool)
		if token != "" {
			t.Errorf("Expected empty token, got '%s'", token)
		}
	})
}

// TestCopilotGitHubRemotePersonalAccessTokenExport tests that when using Copilot engine
// with GitHub remote MCP, the workflow correctly exports GITHUB_PERSONAL_ACCESS_TOKEN
// and passes it to the Docker container. This fixes the authentication issue where the
// MCP gateway validates ${VAR} references in headers at config load time.
func TestCopilotGitHubRemotePersonalAccessTokenExport(t *testing.T) {
	tmpDir := testutil.TempDir(t, "copilot-github-remote-pat-test")
	compiler := NewCompiler()

	frontmatter := `---
on: issues
permissions:
  issues: read
engine: copilot
strict: false
tools:
  github:
    mode: remote
    toolsets: [issues]
---`

	testContent := frontmatter + `

# Test Copilot GitHub Remote PAT Export

This tests that GITHUB_PERSONAL_ACCESS_TOKEN is exported and passed to Docker.
`

	testFile := filepath.Join(tmpDir, "copilot-github-remote-pat-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	err := compiler.CompileWorkflow(testFile)
	if err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Check that GITHUB_PERSONAL_ACCESS_TOKEN is exported in the shell
	if !strings.Contains(lockContent, `export GITHUB_PERSONAL_ACCESS_TOKEN="$GITHUB_MCP_SERVER_TOKEN"`) {
		t.Errorf("Expected 'export GITHUB_PERSONAL_ACCESS_TOKEN=\"$GITHUB_MCP_SERVER_TOKEN\"' but didn't find it in lock file")
	}

	// Check that GITHUB_PERSONAL_ACCESS_TOKEN is passed to Docker with -e flag
	if !strings.Contains(lockContent, "-e GITHUB_PERSONAL_ACCESS_TOKEN") {
		t.Errorf("Expected '-e GITHUB_PERSONAL_ACCESS_TOKEN' in Docker command but didn't find it in lock file")
	}

	// Check that the MCP config still uses the ${} syntax
	if !strings.Contains(lockContent, `"Authorization": "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}"`) {
		t.Errorf("Expected Authorization header with ${GITHUB_PERSONAL_ACCESS_TOKEN} syntax but didn't find it in lock file")
	}

	// Check that the env section still defines the variable
	if !strings.Contains(lockContent, `"GITHUB_PERSONAL_ACCESS_TOKEN": "\${GITHUB_MCP_SERVER_TOKEN}"`) {
		t.Errorf("Expected env section with GITHUB_PERSONAL_ACCESS_TOKEN but didn't find it in lock file")
	}
}

// TestClaudeGitHubRemoteNoPersonalAccessToken tests that when using Claude engine
// with GitHub remote MCP, the workflow does NOT export GITHUB_PERSONAL_ACCESS_TOKEN
// since Claude uses a different auth pattern (direct $GITHUB_MCP_SERVER_TOKEN).
func TestClaudeGitHubRemoteNoPersonalAccessToken(t *testing.T) {
	tmpDir := testutil.TempDir(t, "claude-github-remote-no-pat-test")
	compiler := NewCompiler()

	frontmatter := `---
on: issues
permissions:
  issues: read
engine: claude
strict: false
tools:
  github:
    mode: remote
    toolsets: [issues]
---`

	testContent := frontmatter + `

# Test Claude GitHub Remote No PAT Export

This tests that GITHUB_PERSONAL_ACCESS_TOKEN is NOT exported for Claude.
`

	testFile := filepath.Join(tmpDir, "claude-github-remote-no-pat-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	err := compiler.CompileWorkflow(testFile)
	if err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Check that GITHUB_PERSONAL_ACCESS_TOKEN is NOT exported for Claude
	if strings.Contains(lockContent, `export GITHUB_PERSONAL_ACCESS_TOKEN`) {
		t.Errorf("Did not expect 'export GITHUB_PERSONAL_ACCESS_TOKEN' in Claude workflow but found it")
	}

	// Check that GITHUB_PERSONAL_ACCESS_TOKEN is NOT passed to Docker
	if strings.Contains(lockContent, "-e GITHUB_PERSONAL_ACCESS_TOKEN") {
		t.Errorf("Did not expect '-e GITHUB_PERSONAL_ACCESS_TOKEN' in Claude workflow Docker command but found it")
	}

	// Claude should use direct $GITHUB_MCP_SERVER_TOKEN in Authorization header
	if !strings.Contains(lockContent, `"Authorization": "Bearer $GITHUB_MCP_SERVER_TOKEN"`) {
		t.Errorf("Expected Authorization header with $GITHUB_MCP_SERVER_TOKEN for Claude but didn't find it")
	}
}
