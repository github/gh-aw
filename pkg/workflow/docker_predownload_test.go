//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestDockerImagePredownload(t *testing.T) {
	pinnedGhAwNodeImage := resolveContainerImage(constants.DefaultGhAwNodeImage, nil)

	// Representative sample - tests key docker image predownload scenarios
	tests := []struct {
		name           string
		frontmatter    string
		expectedImages []string
		manifestImages []string
		expectStep     bool
		localOnly      bool
	}{
		{
			name: "Local-only policy reaches agent download step",
			frontmatter: `---
on: issues
engine: claude
docker-image-pull-policy: never
tools:
  github:
---

# Test
Test workflow.`,
			expectStep: true,
			localOnly:  true,
		},
		{
			name: "GitHub tool generates image download step",
			frontmatter: `---
on: issues
engine: claude
tools:
  github:
---

# Test
Test workflow.`,
			expectedImages: []string{
				"ghcr.io/github/github-mcp-server:" + string(constants.DefaultGitHubMCPServerVersion),
			},
			expectStep: true,
		},
		{
			name: "GitHub remote mode does not generate GitHub MCP docker image but still downloads MCP gateway",
			frontmatter: `---
on: issues
engine: claude
tools:
  github:
    mode: remote
---

# Test
Test workflow.`,
			expectedImages: []string{
				"ghcr.io/github/gh-aw-mcpg:" + string(constants.DefaultMCPGatewayVersion),
			},
			expectStep: true,
		},
		{
			name: "Custom MCP server with container",
			frontmatter: `---
on: issues
strict: false
engine: claude
mcp-servers:
  custom-tool:
    container: myorg/custom-mcp:v1.0.0
    allowed: ["*"]
---

# Test
Test workflow with custom MCP container.`,
			expectedImages: []string{
				"ghcr.io/github/github-mcp-server:" + string(constants.DefaultGitHubMCPServerVersion),
				"ghcr.io/github/gh-aw-mcpg:" + string(constants.DefaultMCPGatewayVersion),
				"myorg/custom-mcp:v1.0.0",
			},
			expectStep: true,
		},
		{
			name: "Safe outputs predownloads gh-aw-node",
			frontmatter: `---
on: issues
engine: claude
strict: false
safe-outputs:
  create-issue:
network:
  allowed: ["api.github.com"]
---

# Test
Test workflow - safe outputs MCP server without GitHub tool.`,
			expectedImages: []string{
				pinnedGhAwNodeImage,
				"ghcr.io/github/gh-aw-mcpg:" + string(constants.DefaultMCPGatewayVersion),
			},
			manifestImages: []string{
				constants.DefaultGhAwNodeImage,
			},
			expectStep: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test files
			tmpDir := testutil.TempDir(t, "docker-predownload-test")

			// Write test workflow file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			yaml, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			// Check if the "Download container images" step exists
			hasStep := strings.Contains(string(yaml), "Download container images")
			if hasStep != tt.expectStep {
				t.Errorf("Expected step existence: %v, got: %v", tt.expectStep, hasStep)
			}

			// If we expect a step, verify the images are present
			if tt.expectStep {
				if got := strings.Contains(string(yaml), "GH_AW_DOCKER_IMAGE_PULL_POLICY: never"); got != tt.localOnly {
					t.Errorf("local-only download policy present = %v, want %v", got, tt.localOnly)
				}
				// Verify the script call is present
				if !strings.Contains(string(yaml), "bash \"${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh\"") {
					t.Error("Expected to find 'bash \"${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh\"' script call in generated YAML")
				}
				for _, expectedImage := range tt.expectedImages {
					// Check that the image is being passed as an argument to the script
					if !strings.Contains(string(yaml), expectedImage) {
						t.Errorf("Expected to find image '%s' in generated YAML", expectedImage)
					}
				}

				if len(tt.manifestImages) > 0 {
					manifest, err := ExtractGHAWManifestFromLockFile(string(yaml))
					if err != nil {
						t.Fatalf("Failed to parse gh-aw-manifest: %v", err)
					}
					if manifest == nil {
						t.Fatal("Expected gh-aw-manifest to be present")
					}

					for _, expectedImage := range tt.manifestImages {
						foundImage := false
						for _, container := range manifest.Containers {
							if container.Image == expectedImage {
								foundImage = true
								break
							}
						}
						if !foundImage {
							t.Fatalf("Expected gh-aw-manifest containers to include %q, got %#v", expectedImage, manifest.Containers)
						}
					}
				}
			}
		})
	}
}

func TestDockerImagePredownloadOrdering(t *testing.T) {
	// Test that the "Download container images" step comes before "Start MCP Gateway"
	frontmatter := `---
on: issues
engine: claude
tools:
  github:
---

# Test
Test workflow.`

	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "docker-predownload-ordering-test")

	// Write test workflow file
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	yaml, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(yaml)

	// Find the positions of both steps
	downloadPos := strings.Index(yamlStr, "Download container images")
	setupPos := strings.Index(yamlStr, "Start MCP Gateway")

	if downloadPos == -1 {
		t.Fatal("Expected 'Download container images' step not found")
	}

	if setupPos == -1 {
		t.Fatal("Expected 'Start MCP Gateway' step not found")
	}

	// Verify the download step comes before setup step
	if downloadPos > setupPos {
		t.Errorf("Expected 'Download container images' to come before 'Start MCP Gateway', but found it after")
	}
}
