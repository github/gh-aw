//go:build !js && !wasm

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
//   - validateDockerImageWithMode() - Validates a single Docker image exists and is accessible
//
// # Validation Pattern: Graceful Degradation
//
// Docker image validation degrades gracefully when Docker is unavailable.
// The caller (validateContainerImages) collects errors and surfaces them as compiler warnings:
//   - If Docker is not installed, validation is silently skipped (debug log only)
//   - If the Docker daemon is not running (detected at startup via `docker info`), validation is silently skipped
//   - If the Docker daemon becomes unreachable mid-process (detected during `docker pull`),
//     a single visible warning is emitted, the daemon state is cached as unavailable, and all
//     remaining images are skipped without further retries
//   - If an image cannot be pulled due to authentication (private repo), validation passes
//   - If an image truly doesn't exist, returns an error
//   - Read-only compilation only inspects local images and never pulls missing images
//   - Detailed validation logging is available via debug logging when enabled
//
// This design ensures that `gh aw compile --validate` does not require Docker
// at compile time. Docker availability is a runtime concern.
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
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/syncutil"
)

var dockerValidationLog = logger.New("workflow:docker_validation")

// dockerDaemonCheckTimeout is how long to wait for `docker info` to respond.
// If the daemon isn't running, this prevents long hangs on every docker command.
const dockerDaemonCheckTimeout = 3 * time.Second

// dockerDaemonLoader caches the result of the Docker daemon availability check.
// Using OnceLoader[bool] gives thread-safe one-shot initialisation with the
// ability to override the cached value via Override when a docker command
// (e.g. docker pull) later discovers the daemon is not reachable.
var dockerDaemonLoader syncutil.OnceLoader[bool]

// isDockerDaemonRunning checks if the Docker daemon is responsive.
// Uses a short timeout to avoid hanging when Docker is installed but the daemon is stopped.
// Results are cached via dockerDaemonLoader; the cached value can be overridden by
// markDockerDaemonUnavailable when a later docker command detects the daemon is unreachable.
func isDockerDaemonRunning() bool {
	available, _ := dockerDaemonLoader.Get(func() (bool, error) {
		dockerValidationLog.Print("Checking if Docker daemon is running")
		ctx, cancel := context.WithTimeout(context.Background(), dockerDaemonCheckTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "docker", "info")
		cmd.Stdout = nil
		cmd.Stderr = nil
		err := cmd.Run()

		if err != nil {
			dockerValidationLog.Printf("Docker daemon not running or not responsive: %v", err)
			return false, nil //nolint:nilerr // intentional: cache false without an error
		}
		dockerValidationLog.Print("Docker daemon is running")
		return true, nil
	})
	return available
}

// markDockerDaemonUnavailable records that the Docker daemon is not reachable.
// Subsequent calls to isDockerDaemonRunning will return false immediately, so
// image validation for remaining tools is skipped without further retries.
// Callers are responsible for emitting any user-visible warning.
func markDockerDaemonUnavailable() {
	dockerDaemonLoader.Override(false, nil)
}

// validateDockerImageWithMode checks if a Docker image exists and is accessible.
// When Docker is not installed or the daemon is not running, validation is
// silently skipped (returns nil) so that compile-time validation does not
// depend on Docker availability. If requireDocker is true, returns an error
// instead of skipping when Docker is unavailable. Returns an error only when
// Docker is available and the image cannot be found. The caller treats these
// as warnings.
func validateDockerImageWithMode(image string, verbose bool, requireDocker bool, readOnly bool) error {
	dockerValidationLog.Printf("Validating Docker image: %s", image)

	// Reject names starting with '-' to prevent argument injection
	if strings.HasPrefix(image, "-") {
		return fmt.Errorf("container image name '%s' is invalid: names must not start with '-'", image)
	}

	available, err := dockerImageValidationAvailable(image, requireDocker)
	if err != nil || !available {
		return err
	}

	cmd := exec.Command("docker", "image", "inspect", image)
	inspectOutput, err := cmd.CombinedOutput()
	if err == nil {
		dockerValidationLog.Printf("Docker image found locally: %s", image)
		return nil
	}

	if readOnly {
		return fmt.Errorf("container image '%s' could not be validated locally: %s (%w). Read-only compilation does not pull images; ensure the image is already present locally and the Docker daemon is accessible", image, strings.TrimSpace(string(inspectOutput)), err)
	}

	return pullDockerImageForValidation(image, requireDocker)
}

func dockerImageValidationAvailable(image string, requireDocker bool) (bool, error) {
	// Check if docker CLI is available on PATH.
	// If Docker is not installed, skip validation silently — compile is a source
	// transformation step and should not require Docker at authoring time.
	// When requireDocker is true, return an error instead of skipping.
	_, err := exec.LookPath("docker")
	if err != nil {
		if requireDocker {
			return false, fmt.Errorf("docker not installed - could not validate container image '%s'. Install Docker or omit the --validate-images flag to skip container image validation", image)
		}
		dockerValidationLog.Print("Docker not installed, skipping container image validation")
		return false, nil
	}

	// Check if Docker daemon is actually running (cached check with short timeout).
	// If the daemon is not running (common on CI runners like ubuntu-slim, or when
	// Docker Desktop is stopped), skip validation silently instead of emitting a
	// warning. Image accessibility is a runtime concern, not a compile-time one.
	// When requireDocker is true, return an error instead of skipping.
	if !isDockerDaemonRunning() {
		if requireDocker {
			return false, fmt.Errorf("docker daemon not running - could not validate container image '%s'. Start the Docker daemon or omit the --validate-images flag to skip container image validation", image)
		}
		dockerValidationLog.Print("Docker daemon not running, skipping container image validation")
		return false, nil
	}

	return true, nil
}

func pullDockerImageForValidation(image string, requireDocker bool) error {
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

		if done, err := classifyDockerImagePullFailure(image, outputStr, requireDocker); done {
			return err
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

// classifyDockerImagePullFailure reports whether validation should stop retrying.
func classifyDockerImagePullFailure(image, outputStr string, requireDocker bool) (bool, error) {
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
		return true, nil
	}

	// Check if the error means the Docker daemon became unreachable mid-process.
	// This can happen when `docker info` succeeded earlier but the daemon stopped
	// (or was never fully operational) by the time we issue docker pull.
	// Treat this as a daemon-unavailable condition: update the cached state so
	// subsequent images skip immediately, emit a single warning, and return nil
	// (or an error when requireDocker is true).
	// Use case-insensitive matching to handle Docker version differences.
	outputLower := strings.ToLower(outputStr)
	if strings.Contains(outputLower, "cannot connect to the docker daemon") ||
		strings.Contains(outputLower, "is the docker daemon running") {
		markDockerDaemonUnavailable()
		if requireDocker {
			return true, fmt.Errorf("docker daemon not running - could not validate container image '%s'. Start the Docker daemon or omit the --validate-images flag to skip container image validation", image)
		}
		dockerValidationLog.Printf("Docker daemon not reachable during pull of %s, skipping container image validation", image)
		return true, nil
	}

	// Check for non-retryable errors (image doesn't exist)
	if strings.Contains(outputStr, "does not exist") ||
		strings.Contains(outputStr, "not found") ||
		strings.Contains(outputStr, "manifest unknown") {
		// These errors won't be resolved by retrying
		dockerValidationLog.Printf("Image %s does not exist (non-retryable error)", image)
		return true, fmt.Errorf("container image '%s' not found and could not be pulled: %s. Please verify the image name and tag.\n\nExample:\ntools:\n  my-tool:\n    container: \"node:20\"\n\nOr:\ntools:\n  my-tool:\n    container: \"ghcr.io/owner/image:latest\"\n\nSee: %s", image, outputStr, constants.DocsToolsURL)
	}
	return false, nil
}
