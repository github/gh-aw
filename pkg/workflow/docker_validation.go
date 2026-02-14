// This file provides Docker image validation for agentic workflows.
//
// # Docker Image Validation
//
// This file validates Docker container images used in MCP configurations.
// Validation ensures that Docker images specified in workflows exist and are accessible,
// preventing runtime failures due to typos or non-existent images.
//
// # Validation Functions
//
//   - validateDockerImage() - Validates a single Docker image exists and is accessible
//
// # Validation Pattern: Warning vs Error
//
// Docker image validation returns errors for all failure cases. The caller
// (validateContainerImages) collects these and surfaces them as compiler warnings:
//   - If Docker is not installed, returns an error
//   - If the Docker daemon is not running, returns an error (with fast timeout check)
//   - If an image cannot be pulled due to authentication (private repo), validation passes
//   - If an image truly doesn't exist, returns an error
//   - Verbose mode provides detailed validation feedback
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates Docker images
//   - It checks container image accessibility
//   - It validates Docker-specific configurations
//
// For Docker image collection functions, see docker.go.
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var dockerValidationLog = logger.New("workflow:docker_validation")

// dockerDaemonCheckTimeout is how long to wait for `docker info` to respond.
// If the daemon isn't running, this prevents long hangs on every docker command.
const dockerDaemonCheckTimeout = 3 * time.Second

// Cached result of Docker daemon availability check.
// Checked once per process to avoid repeated slow checks when daemon is down.
var (
	dockerDaemonOnce      sync.Once
	dockerDaemonAvailable bool
)

// isDockerDaemonRunning checks if the Docker daemon is responsive.
// Uses a short timeout to avoid hanging when Docker is installed but the daemon is stopped.
// Results are cached for the process lifetime.
func isDockerDaemonRunning() bool {
	dockerDaemonOnce.Do(func() {
		dockerValidationLog.Print("Checking if Docker daemon is running")
		ctx, cancel := context.WithTimeout(context.Background(), dockerDaemonCheckTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "docker", "info")
		cmd.Stdout = nil
		cmd.Stderr = nil
		err := cmd.Run()

		dockerDaemonAvailable = err == nil
		if !dockerDaemonAvailable {
			dockerValidationLog.Printf("Docker daemon not running or not responsive: %v", err)
		} else {
			dockerValidationLog.Print("Docker daemon is running")
		}
	})
	return dockerDaemonAvailable
}

// validateDockerImage checks if a Docker image exists and is accessible.
// Returns an error if Docker is not installed, the daemon is not running,
// or the image cannot be found. The caller treats these as warnings.
func validateDockerImage(image string, verbose bool) error {
	dockerValidationLog.Printf("Validating Docker image: %s", image)

	// Check if docker CLI is available on PATH
	_, err := exec.LookPath("docker")
	if err != nil {
		dockerValidationLog.Print("Docker not installed, cannot validate image")
		return fmt.Errorf("Docker not installed - could not validate container image '%s'. Install Docker or remove container-based tools", image)
	}

	// Check if Docker daemon is actually running (cached check with short timeout).
	// This prevents multi-minute hangs when Docker Desktop is installed but not running,
	// which is common on macOS development machines.
	if !isDockerDaemonRunning() {
		dockerValidationLog.Print("Docker daemon not running, cannot validate image")
		return fmt.Errorf("Docker daemon not running - could not validate container image '%s'. Start Docker Desktop or remove container-based tools", image)
	}

	// Try to inspect the image (will succeed if image exists locally)
	cmd := exec.Command("docker", "image", "inspect", image)
	output, err := cmd.CombinedOutput()

	if err == nil {
		// Image exists locally
		dockerValidationLog.Printf("Docker image found locally: %s", image)
		_ = output // Suppress unused variable warning
		return nil
	}

	dockerValidationLog.Printf("Docker image not found locally, attempting to pull: %s", image)

	// Image doesn't exist locally, try to pull it with retry logic
	maxAttempts := 3
	waitTime := 5 // seconds

	var lastOutput string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		dockerValidationLog.Printf("Attempt %d of %d: Pulling image %s", attempt, maxAttempts, image)

		pullCmd := exec.Command("docker", "pull", image)
		pullOutput, pullErr := pullCmd.CombinedOutput()
		outputStr := strings.TrimSpace(string(pullOutput))

		if pullErr == nil {
			// Successfully pulled
			dockerValidationLog.Printf("Successfully pulled image %s on attempt %d", image, attempt)
			return nil
		}

		lastOutput = outputStr

		// Check if the error is due to authentication issues for existing private repositories
		// We need to distinguish between:
		// 1. "repository does not exist" - should fail validation immediately
		// 2. "authentication required" for existing repos - should pass (private repo)
		if (strings.Contains(outputStr, "denied") ||
			strings.Contains(outputStr, "unauthorized") ||
			strings.Contains(outputStr, "authentication required")) &&
			!strings.Contains(outputStr, "does not exist") &&
			!strings.Contains(outputStr, "not found") {
			// This is likely a private image that requires authentication
			// Don't fail validation for private/authenticated images
			dockerValidationLog.Printf("Image %s appears to be private/authenticated, skipping validation", image)
			return nil
		}

		// Check for non-retryable errors (image doesn't exist)
		if strings.Contains(outputStr, "does not exist") ||
			strings.Contains(outputStr, "not found") ||
			strings.Contains(outputStr, "manifest unknown") {
			// These errors won't be resolved by retrying
			dockerValidationLog.Printf("Image %s does not exist (non-retryable error)", image)
			return fmt.Errorf("container image '%s' not found and could not be pulled: %s. Please verify the image name and tag.\n\nExample:\ntools:\n  my-tool:\n    container: \"node:20\"\n\nOr:\ntools:\n  my-tool:\n    container: \"ghcr.io/owner/image:latest\"\n\nSee: %s", image, outputStr, constants.DocsToolsURL)
		}

		// If not the last attempt, wait and retry (likely network error)
		if attempt < maxAttempts {
			dockerValidationLog.Printf("Failed to pull image %s (attempt %d/%d). Retrying in %ds...", image, attempt, maxAttempts, waitTime)
			time.Sleep(time.Duration(waitTime) * time.Second)
			waitTime *= 2 // Exponential backoff
		}
	}

	// All attempts failed with retryable errors
	return fmt.Errorf("container image '%s' not found and could not be pulled after %d attempts: %s. Please verify the image name and tag.\n\nExample:\ntools:\n  my-tool:\n    container: \"node:20\"\n\nOr:\ntools:\n  my-tool:\n    container: \"ghcr.io/owner/image:latest\"\n\nSee: %s", image, maxAttempts, lastOutput, constants.DocsToolsURL)
}
