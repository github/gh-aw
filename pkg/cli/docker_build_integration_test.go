//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDockerfile_Exists verifies the Dockerfile exists and has expected content
func TestDockerfile_Exists(t *testing.T) {
	// Get the repository root
	repoRoot, err := getRepoRoot()
	if err != nil {
		t.Fatalf("Failed to get repo root: %v", err)
	}

	dockerfilePath := filepath.Join(repoRoot, "Dockerfile")

	// Check if Dockerfile exists
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		t.Fatal("Dockerfile does not exist at repository root")
	}

	// Read Dockerfile content
	content, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("Failed to read Dockerfile: %v", err)
	}

	contentStr := string(content)

	// Verify essential components are present
	requiredComponents := []string{
		"FROM alpine:",           // Alpine base image
		"github-cli",             // GitHub CLI package
		"git",                    // Git package
		"jq",                     // jq package
		"bash",                   // Bash package
		"ARG BINARY",             // Build argument for binary
		"ENTRYPOINT [\"gh-aw\"]", // Entrypoint
	}

	for _, component := range requiredComponents {
		if !strings.Contains(contentStr, component) {
			t.Errorf("Dockerfile is missing required component: %s", component)
		}
	}
}

// TestMakefile_DockerTargets verifies Docker targets exist in Makefile
func TestMakefile_DockerTargets(t *testing.T) {
	// Get the repository root
	repoRoot, err := getRepoRoot()
	if err != nil {
		t.Fatalf("Failed to get repo root: %v", err)
	}

	makefilePath := filepath.Join(repoRoot, "Makefile")

	// Read Makefile content
	content, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatalf("Failed to read Makefile: %v", err)
	}

	contentStr := string(content)

	// Verify Docker targets are present
	requiredTargets := []string{
		".PHONY: docker-build",
		".PHONY: docker-build-multiarch",
		".PHONY: docker-test",
		".PHONY: docker-push",
		".PHONY: docker-clean",
	}

	for _, target := range requiredTargets {
		if !strings.Contains(contentStr, target) {
			t.Errorf("Makefile is missing required Docker target: %s", target)
		}
	}

	// Verify Docker-related variables
	requiredVars := []string{
		"DOCKER_IMAGE=",
		"DOCKER_PLATFORMS=",
	}

	for _, varDef := range requiredVars {
		if !strings.Contains(contentStr, varDef) {
			t.Errorf("Makefile is missing required Docker variable: %s", varDef)
		}
	}
}

// TestDockerBuild_WithMake tests building Docker image using Makefile
func TestDockerBuild_WithMake(t *testing.T) {
	if os.Getenv("SKIP_DOCKER_TESTS") != "" {
		t.Skip("Skipping Docker build test (SKIP_DOCKER_TESTS is set)")
	}

	// Check if Docker is available
	if !isDockerAvailable() {
		t.Skip("Docker is not available, skipping Docker build test")
	}

	// Get the repository root
	repoRoot, err := getRepoRoot()
	if err != nil {
		t.Fatalf("Failed to get repo root: %v", err)
	}

	// First, we need to build the Linux binary
	t.Log("Building Linux binary for Docker...")
	buildCmd := exec.Command("make", "build-linux")
	buildCmd.Dir = repoRoot
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Logf("Build output: %s", buildOutput)
		t.Fatalf("Failed to build Linux binary: %v", err)
	}

	// Verify the Linux binary was created
	binaryPath := filepath.Join(repoRoot, "gh-aw-linux-amd64")
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Fatalf("Linux binary not found at %s", binaryPath)
	}

	// Build Docker image using Makefile
	t.Log("Building Docker image with make docker-build...")
	dockerBuildCmd := exec.Command("make", "docker-build")
	dockerBuildCmd.Dir = repoRoot
	dockerOutput, err := dockerBuildCmd.CombinedOutput()
	if err != nil {
		t.Logf("Docker build output: %s", dockerOutput)
		t.Fatalf("Failed to build Docker image: %v", err)
	}

	t.Log("Docker image built successfully")

	// Clean up Docker image after test
	t.Cleanup(func() {
		cleanCmd := exec.Command("make", "docker-clean")
		cleanCmd.Dir = repoRoot
		_ = cleanCmd.Run() // Ignore cleanup errors
	})
}

// TestDockerImage_RunsSuccessfully tests that the built Docker image runs
func TestDockerImage_RunsSuccessfully(t *testing.T) {
	if os.Getenv("SKIP_DOCKER_TESTS") != "" {
		t.Skip("Skipping Docker run test (SKIP_DOCKER_TESTS is set)")
	}

	// Check if Docker is available
	if !isDockerAvailable() {
		t.Skip("Docker is not available, skipping Docker run test")
	}

	// Get the repository root
	repoRoot, err := getRepoRoot()
	if err != nil {
		t.Fatalf("Failed to get repo root: %v", err)
	}

	// Build the Docker image first (reuse from previous test or build fresh)
	buildCmd := exec.Command("make", "build-linux")
	buildCmd.Dir = repoRoot
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build Linux binary: %v", err)
	}

	dockerBuildCmd := exec.Command("make", "docker-build")
	dockerBuildCmd.Dir = repoRoot
	if err := dockerBuildCmd.Run(); err != nil {
		t.Fatalf("Failed to build Docker image: %v", err)
	}

	// Test running the Docker image with --help
	t.Log("Testing Docker image with --help...")
	dockerRunCmd := exec.Command("docker", "run", "--rm", "ghcr.io/github/gh-aw:latest", "--help")
	output, err := dockerRunCmd.CombinedOutput()
	if err != nil {
		t.Logf("Docker run output: %s", output)
		t.Fatalf("Failed to run Docker image: %v", err)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "GitHub Agentic Workflows") {
		t.Errorf("Docker image output does not contain expected help text. Output: %s", outputStr)
	}

	// Test running with --version
	t.Log("Testing Docker image with --version...")
	dockerVersionCmd := exec.Command("docker", "run", "--rm", "ghcr.io/github/gh-aw:latest", "--version")
	versionOutput, err := dockerVersionCmd.CombinedOutput()
	if err != nil {
		t.Logf("Docker version output: %s", versionOutput)
		t.Fatalf("Failed to run Docker image with --version: %v", err)
	}

	versionStr := string(versionOutput)
	// Check for either 'gh-aw version' or 'gh aw version' (the actual version format)
	if !strings.Contains(versionStr, "version") {
		t.Errorf("Docker image version output unexpected. Output: %s", versionStr)
	}

	// Clean up Docker image after test
	t.Cleanup(func() {
		cleanCmd := exec.Command("make", "docker-clean")
		cleanCmd.Dir = repoRoot
		_ = cleanCmd.Run() // Ignore cleanup errors
	})
}

// TestDockerImage_HasRequiredTools verifies required tools are in the image
func TestDockerImage_HasRequiredTools(t *testing.T) {
	if os.Getenv("SKIP_DOCKER_TESTS") != "" {
		t.Skip("Skipping Docker tools test (SKIP_DOCKER_TESTS is set)")
	}

	// Check if Docker is available
	if !isDockerAvailable() {
		t.Skip("Docker is not available, skipping Docker tools test")
	}

	// Get the repository root
	repoRoot, err := getRepoRoot()
	if err != nil {
		t.Fatalf("Failed to get repo root: %v", err)
	}

	// Build the Docker image
	buildCmd := exec.Command("make", "build-linux")
	buildCmd.Dir = repoRoot
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build Linux binary: %v", err)
	}

	dockerBuildCmd := exec.Command("make", "docker-build")
	dockerBuildCmd.Dir = repoRoot
	if err := dockerBuildCmd.Run(); err != nil {
		t.Fatalf("Failed to build Docker image: %v", err)
	}

	// Test required tools
	requiredTools := []string{"gh", "git", "jq", "bash"}

	for _, tool := range requiredTools {
		t.Run(tool, func(t *testing.T) {
			// Use --entrypoint to override the default entrypoint and run shell command
			cmd := exec.Command("docker", "run", "--rm", "--entrypoint", "sh",
				"ghcr.io/github/gh-aw:latest", "-c", "which "+tool)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("Tool %s not found in Docker image. Output: %s", tool, output)
			}
		})
	}

	// Clean up Docker image after test
	t.Cleanup(func() {
		cleanCmd := exec.Command("make", "docker-clean")
		cleanCmd.Dir = repoRoot
		_ = cleanCmd.Run() // Ignore cleanup errors
	})
}

// Helper function to get repository root
func getRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
