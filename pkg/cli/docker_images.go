package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var dockerImagesLog = logger.New("cli:docker_images")

// DockerImages defines the Docker images used by the compile tool's static analysis scanners
const (
	ZizmorImage     = "ghcr.io/zizmorcore/zizmor:latest"
	PoutineImage    = "ghcr.io/boostsecurityio/poutine:latest"
	ActionlintImage = "rhysd/actionlint:latest"
)

// dockerPullState tracks the state of docker pull operations
type dockerPullState struct {
	mu                 sync.RWMutex
	downloading        map[string]bool // image -> is currently downloading
	mockAvailable      map[string]bool // for testing: override IsDockerImageAvailable
	mockAvailableInUse bool            // for testing: whether to use mockAvailable
}

var pullState = &dockerPullState{
	downloading:   make(map[string]bool),
	mockAvailable: make(map[string]bool),
}

// isDockerImageAvailableUnlocked checks if a Docker image is available locally
// This function must be called with pullState.mu held (either RLock or Lock)
func isDockerImageAvailableUnlocked(image string) bool {
	// Check if we're in mock mode (for testing)
	if pullState.mockAvailableInUse {
		available := pullState.mockAvailable[image]
		dockerImagesLog.Printf("Mock: Checking if image %s is available: %v", image, available)
		return available
	}

	// For non-mock mode, we need to execute docker command
	// This is safe to do under lock since it's just a subprocess call
	cmd := exec.Command("docker", "image", "inspect", image)
	// Suppress output - we only care about exit code
	cmd.Stdout = nil
	cmd.Stderr = nil
	err := cmd.Run()
	available := err == nil
	dockerImagesLog.Printf("Checking if image %s is available: %v", image, available)
	return available
}

// IsDockerImageAvailable checks if a Docker image is available locally
func IsDockerImageAvailable(image string) bool {
	pullState.mu.RLock()
	defer pullState.mu.RUnlock()
	return isDockerImageAvailableUnlocked(image)
}

// IsDockerImageDownloading checks if a Docker image is currently being downloaded
func IsDockerImageDownloading(image string) bool {
	pullState.mu.RLock()
	defer pullState.mu.RUnlock()
	return pullState.downloading[image]
}

// StartDockerImageDownload starts downloading a Docker image in the background
// Returns true if download was started, false if already downloading or available
// The download can be cancelled by cancelling the provided context
func StartDockerImageDownload(ctx context.Context, image string) bool {
	// Check availability and downloading status atomically under lock
	pullState.mu.Lock()
	defer pullState.mu.Unlock()

	// Check if already available (inside lock for atomicity)
	if isDockerImageAvailableUnlocked(image) {
		dockerImagesLog.Printf("Image %s is already available", image)
		return false
	}

	// Check if already downloading
	if pullState.downloading[image] {
		dockerImagesLog.Printf("Image %s is already downloading", image)
		return false
	}

	pullState.downloading[image] = true

	// Start the download in a goroutine with retry logic
	go func() {
		dockerImagesLog.Printf("Starting download of image %s", image)

		// Retry configuration
		maxAttempts := 3
		waitTime := 5 // seconds

		var lastErr error
		var lastOutput []byte

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			// Check if context was cancelled
			if ctx.Err() != nil {
				dockerImagesLog.Printf("Download of image %s cancelled: %v", image, ctx.Err())
				pullState.mu.Lock()
				delete(pullState.downloading, image)
				pullState.mu.Unlock()
				return
			}

			dockerImagesLog.Printf("Attempt %d of %d: Pulling image %s", attempt, maxAttempts, image)

			cmd := exec.CommandContext(ctx, "docker", "pull", image)
			output, err := cmd.CombinedOutput()

			if err == nil {
				// Success
				dockerImagesLog.Printf("Successfully downloaded image %s", image)
				pullState.mu.Lock()
				delete(pullState.downloading, image)
				pullState.mu.Unlock()
				return
			}

			lastErr = err
			lastOutput = output

			// If not the last attempt, wait and retry
			if attempt < maxAttempts {
				dockerImagesLog.Printf("Failed to download image %s (attempt %d/%d). Retrying in %ds...", image, attempt, maxAttempts, waitTime)

				// Use context-aware sleep
				select {
				case <-time.After(time.Duration(waitTime) * time.Second):
					// Continue to next retry
				case <-ctx.Done():
					// Context cancelled during sleep
					dockerImagesLog.Printf("Download of image %s cancelled during retry wait: %v", image, ctx.Err())
					pullState.mu.Lock()
					delete(pullState.downloading, image)
					pullState.mu.Unlock()
					return
				}

				waitTime *= 2 // Exponential backoff
			}
		}

		// All attempts failed
		dockerImagesLog.Printf("Failed to download image %s after %d attempts: %v\nOutput: %s", image, maxAttempts, lastErr, string(lastOutput))

		pullState.mu.Lock()
		delete(pullState.downloading, image)
		pullState.mu.Unlock()
	}()

	return true
}

// CheckAndPrepareDockerImages checks if required Docker images are available
// for the requested static analysis tools. If any are not available, it starts
// downloading them and returns a message indicating the LLM should retry.
//
// Returns:
//   - nil if all required images are available
//   - error with retry message if any images are downloading or need to be downloaded
func CheckAndPrepareDockerImages(ctx context.Context, useZizmor, usePoutine, useActionlint bool) error {
	var missingImages []string
	var downloadingImages []string

	// Check which images are needed and their availability
	imagesToCheck := []struct {
		use   bool
		image string
		name  string
	}{
		{useZizmor, ZizmorImage, "zizmor"},
		{usePoutine, PoutineImage, "poutine"},
		{useActionlint, ActionlintImage, "actionlint"},
	}

	for _, img := range imagesToCheck {
		if !img.use {
			continue
		}

		if IsDockerImageAvailable(img.image) {
			continue
		}

		if IsDockerImageDownloading(img.image) {
			downloadingImages = append(downloadingImages, img.name)
		} else {
			// Start download
			StartDockerImageDownload(ctx, img.image)
			missingImages = append(missingImages, img.name)
		}
	}

	// If any images are downloading or were just started
	if len(downloadingImages) > 0 || len(missingImages) > 0 {
		var msg strings.Builder
		msg.WriteString("Docker images are being downloaded. Please wait and retry the compile command.\n\n")

		if len(missingImages) > 0 {
			msg.WriteString("Started downloading: ")
			msg.WriteString(strings.Join(missingImages, ", "))
			msg.WriteString("\n")
		}

		if len(downloadingImages) > 0 {
			msg.WriteString("Currently downloading: ")
			msg.WriteString(strings.Join(downloadingImages, ", "))
			msg.WriteString("\n")
		}

		msg.WriteString("\nRetry in 15-30 seconds.")

		return errors.New(msg.String())
	}

	return nil
}

// isDockerAvailable checks if Docker is available on the system
func isDockerAvailable() bool {
	cmd := exec.Command("docker", "version")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// ResetDockerPullState resets the internal pull state (for testing)
func ResetDockerPullState() {
	pullState.mu.Lock()
	defer pullState.mu.Unlock()
	pullState.downloading = make(map[string]bool)
	pullState.mockAvailable = make(map[string]bool)
	pullState.mockAvailableInUse = false
}

// ValidateMCPServerDockerAvailability validates that Docker is available for MCP server operations
// that require static analysis tools
func ValidateMCPServerDockerAvailability() error {
	if !isDockerAvailable() {
		return fmt.Errorf("docker is not available - required for zizmor, poutine, and actionlint static analysis tools")
	}
	return nil
}

// SetDockerImageDownloading sets the downloading state for an image (for testing)
func SetDockerImageDownloading(image string, downloading bool) {
	pullState.mu.Lock()
	defer pullState.mu.Unlock()
	if downloading {
		pullState.downloading[image] = true
	} else {
		delete(pullState.downloading, image)
	}
}

// SetMockImageAvailable sets the mock availability for an image (for testing)
func SetMockImageAvailable(image string, available bool) {
	pullState.mu.Lock()
	defer pullState.mu.Unlock()
	pullState.mockAvailableInUse = true
	pullState.mockAvailable[image] = available
}

// PrintDockerPullStatus prints the current pull status to stderr (for debugging)
func PrintDockerPullStatus() {
	pullState.mu.RLock()
	defer pullState.mu.RUnlock()
	if len(pullState.downloading) > 0 {
		fmt.Fprintf(os.Stderr, "Currently downloading images: %v\n", pullState.downloading)
	}
}
