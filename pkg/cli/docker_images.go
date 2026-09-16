package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/logger"
)

var dockerImagesLog = logger.New("cli:docker_images")

// DockerUnavailableError is returned when the Docker daemon is not accessible.
// This is distinct from transient errors (e.g., images being downloaded) and signals
// that Docker is not installed or not running on the host system.
// Callers can use errors.As to check for this type and take appropriate action,
// such as skipping static analysis but still running the compile step.
type DockerUnavailableError struct {
	Message string
}

func (e *DockerUnavailableError) Error() string {
	return e.Message
}

// DockerImages defines the Docker images used by the compile tool's static analysis scanners
const (
	ZizmorImage      = "ghcr.io/zizmorcore/zizmor:1.30.1@sha256:a2eb396d886c053073405c7a980f2139ba2248ec172243cfa3841e57196e8101"
	PoutineImage     = "ghcr.io/boostsecurityio/poutine:1.1.6@sha256:722a8e0999b583c1540fe2974e691032b2d9d21b9256a17965132b6bfd0081b0"
	ActionlintImage  = "rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667"
	RunnerGuardImage = "ghcr.io/vigilant-llc/runner-guard:3.1.5@sha256:2df426ef96d21f1622e05b21329f26bd263fc46110609cefb6afe43457613ac0"
	SyftImage        = "anchore/syft:v1.51.1@sha256:95fe0835e5bebc6f8b1f8acef68d47d63d594ef4c0f25c097ff853b23cbac74c"
	GrypeImage       = "anchore/grype:v0.118.0@sha256:8a93fc48da96bd6ec5981279d099b69de11541dc68fdf222fb9161f8ff284af7"
	GrantImage       = "anchore/grant:v0.6.8@sha256:172463611795f43b77302cdfbd7b3f81295492a7330e0820cfe41c3674920237"
	YamllintImage    = "pipelinecomponents/yamllint:latest@sha256:5ab5eb7da0ed5e606b07c1723fc8b275e925189f70ac259b26b7329cb5f8f44d"
	// ShellcheckImage is the Docker fallback for shellcheck when the binary is not installed.
	// Used automatically when the shellcheck binary is not found in PATH (e.g. on Windows).
	ShellcheckImage = "koalaman/shellcheck:v0.11.0@sha256:61862eba1fcf09a484ebcc6feea46f1782532571a34ed51fedf90dd25f925a8d"
)

// inflightDownload holds the join channel and result for an in-progress pull.
// err is written by the download goroutine before done is closed; it is safe
// to read after receiving from done.
type inflightDownload struct {
	done chan struct{}
	err  error
}

// dockerPullState tracks the state of docker pull operations
type dockerPullState struct {
	mu                  sync.RWMutex
	downloading         map[string]bool // image -> is currently downloading
	inflight            map[string]*inflightDownload
	mockAvailable       map[string]bool // for testing: override IsDockerImageAvailable
	mockAvailableInUse  bool            // for testing: whether to use mockAvailable
	mockDockerAvailable bool            // for testing: override IsDockerAvailable (default true)
}

var pullState = &dockerPullState{
	downloading:         make(map[string]bool),
	inflight:            make(map[string]*inflightDownload),
	mockAvailable:       make(map[string]bool),
	mockDockerAvailable: true,
}

// isDockerImageAvailableUnlocked checks if a Docker image is available locally
// This function must be called with pullState.mu held (either RLock or Lock)
func isDockerImageAvailableUnlocked(ctx context.Context, image string) bool {
	ctx = ctxutil.OrBackground(ctx)

	// Check if we're in mock mode (for testing)
	if pullState.mockAvailableInUse {
		available := pullState.mockAvailable[image]
		dockerImagesLog.Printf("Mock: Checking if image %s is available: %v", image, available)
		return available
	}

	// For non-mock mode, we need to execute docker command
	// This is safe to do under lock since it's just a subprocess call
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	// Suppress output - we only care about exit code
	cmd.Stdout = nil
	cmd.Stderr = nil
	err := cmd.Run()
	available := err == nil
	dockerImagesLog.Printf("Checking if image %s is available: %v", image, available)
	return available
}

// IsDockerImageAvailable checks if a Docker image is available locally
func IsDockerImageAvailable(ctx context.Context, image string) bool {
	ctx = ctxutil.OrBackground(ctx)

	pullState.mu.RLock()
	defer pullState.mu.RUnlock()
	return isDockerImageAvailableUnlocked(ctx, image)
}

// IsDockerImageDownloading checks if a Docker image is currently being downloaded
func IsDockerImageDownloading(image string) bool {
	pullState.mu.RLock()
	defer pullState.mu.RUnlock()
	return pullState.downloading[image]
}

// IsDockerAvailable checks if the Docker daemon is running and accessible
func IsDockerAvailable(ctx context.Context) bool {
	ctx = ctxutil.OrBackground(ctx)

	mockEnabled, mockAvailable := func() (bool, bool) {
		pullState.mu.RLock()
		defer pullState.mu.RUnlock()
		if pullState.mockAvailableInUse {
			return true, pullState.mockDockerAvailable
		}
		return false, false
	}()
	if mockEnabled {
		dockerImagesLog.Printf("Mock: Docker available: %v", mockAvailable)
		return mockAvailable
	}

	cmd := exec.CommandContext(ctx, "docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	err := cmd.Run()
	available := err == nil
	dockerImagesLog.Printf("Docker daemon available: %v", available)
	return available
}

// StartDockerImageDownload starts downloading a Docker image in the background.
// Returns true if the download was started, false if already downloading or available.
// The download can be cancelled by cancelling the provided context.
// The returned join function blocks until the download goroutine exits and returns
// any error that occurred (nil on success or context cancellation).
func StartDockerImageDownload(ctx context.Context, image string) (bool, func() error) {
	ctx = ctxutil.OrBackground(ctx)

	// Check availability and downloading status atomically under lock
	pullState.mu.Lock()
	defer pullState.mu.Unlock()

	// Check if already downloading
	if pullState.downloading[image] {
		dockerImagesLog.Printf("Image %s is already downloading", image)
		dl := pullState.inflight[image]
		return false, func() error {
			if dl != nil {
				<-dl.done
				return dl.err
			}
			return nil
		}
	}

	// Check if already available (inside lock for atomicity)
	if isDockerImageAvailableUnlocked(ctx, image) {
		dockerImagesLog.Printf("Image %s is already available", image)
		return false, func() error { return nil }
	}

	dl := &inflightDownload{done: make(chan struct{})}
	pullState.downloading[image] = true
	pullState.inflight[image] = dl

	// Start the download in a goroutine with retry logic.
	// Defer order (LIFO): recover+set-error runs first, cleanup runs second,
	// close(done) runs last so callers only unblock after dl.err is set.
	go func() {
		var lastErr error
		defer close(dl.done)
		defer func() {
			pullState.mu.Lock()
			defer pullState.mu.Unlock()
			delete(pullState.downloading, image)
			delete(pullState.inflight, image)
		}()
		defer func() {
			if r := recover(); r != nil {
				lastErr = fmt.Errorf("panic in docker image download for %s: %v", image, r)
				dockerImagesLog.Printf("Panic in docker image download for %s (recovered): %v", image, r)
			}
			dl.err = lastErr
		}()

		dockerImagesLog.Printf("Starting download of image %s", image)

		// Retry configuration
		maxAttempts := 3
		waitTime := 5 // seconds

		var lastOutput []byte

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			// Check if context was cancelled
			if ctx.Err() != nil {
				dockerImagesLog.Printf("Download of image %s cancelled: %v", image, ctx.Err())
				return
			}

			dockerImagesLog.Printf("Attempt %d of %d: Pulling image %s", attempt, maxAttempts, image)

			cmd := exec.CommandContext(ctx, "docker", "pull", image)
			output, err := cmd.CombinedOutput()

			if err == nil {
				// Success
				dockerImagesLog.Printf("Successfully downloaded image %s", image)
				return
			}

			lastErr = err
			lastOutput = output

			// If not the last attempt, wait and retry
			if attempt < maxAttempts {
				dockerImagesLog.Printf("Failed to download image %s (attempt %d/%d). Retrying in %ds...", image, attempt, maxAttempts, waitTime)

				// Use context-aware sleep
				timer := time.NewTimer(time.Duration(waitTime) * time.Second)
				select {
				case <-timer.C:
					// Continue to next retry
				case <-ctx.Done():
					timer.Stop()
					// Context cancelled during sleep
					dockerImagesLog.Printf("Download of image %s cancelled during retry wait: %v", image, ctx.Err())
					return
				}

				waitTime *= 2 // Exponential backoff
			}
		}

		// All attempts failed
		dockerImagesLog.Printf("Failed to download image %s after %d attempts: %v\nOutput: %s", image, maxAttempts, lastErr, string(lastOutput))
	}()

	return true, func() error {
		<-dl.done
		return dl.err
	}
}

// DockerImagesOptions specifies which Docker-based static analysis tools are requested.
type DockerImagesOptions struct {
	Zizmor, Poutine, Actionlint, RunnerGuard, Syft, Grype, Grant, Yamllint bool
}

// CheckAndPrepareDockerImages checks if required Docker images are available
// for the requested static analysis tools. If any are not available, it starts
// downloading them and returns a message indicating the LLM should retry.
//
// Returns:
//   - nil if all required images are available
//   - error if Docker is unavailable or images are downloading/need to be downloaded
func CheckAndPrepareDockerImages(ctx context.Context, opts DockerImagesOptions) error {
	// If no tools requested, nothing to do
	if !opts.Zizmor && !opts.Poutine && !opts.Actionlint && !opts.RunnerGuard && !opts.Syft && !opts.Grype && !opts.Grant && !opts.Yamllint {
		return nil
	}

	// Check if Docker daemon is available before attempting any image operations
	if !IsDockerAvailable(ctx) {
		var requestedTools []string
		var paramsList []string
		if opts.Zizmor {
			tool := "zizmor"
			requestedTools = append(requestedTools, tool)
			paramsList = append(paramsList, tool+": false")
		}
		if opts.Poutine {
			tool := "poutine"
			requestedTools = append(requestedTools, tool)
			paramsList = append(paramsList, tool+": false")
		}
		if opts.Actionlint {
			tool := "actionlint"
			requestedTools = append(requestedTools, tool)
			paramsList = append(paramsList, tool+": false")
		}
		if opts.RunnerGuard {
			tool := "runner-guard"
			requestedTools = append(requestedTools, tool)
			paramsList = append(paramsList, tool+": false")
		}
		if opts.Syft {
			tool := "syft"
			requestedTools = append(requestedTools, tool)
			paramsList = append(paramsList, tool+": false")
		}
		if opts.Grype {
			tool := "grype"
			requestedTools = append(requestedTools, tool)
			paramsList = append(paramsList, tool+": false")
		}
		if opts.Grant {
			tool := "grant"
			requestedTools = append(requestedTools, tool)
			paramsList = append(paramsList, tool+": false")
		}
		if opts.Yamllint {
			tool := "yamllint"
			requestedTools = append(requestedTools, tool)
			paramsList = append(paramsList, tool+": false")
		}
		verb := "requires"
		if len(requestedTools) > 1 {
			verb = "require"
		}
		return &DockerUnavailableError{
			Message: fmt.Sprintf("docker is not available (cannot connect to Docker daemon). %s %s Docker. Please install and start Docker, or set %s to skip static analysis", strings.Join(requestedTools, " and "), verb, strings.Join(paramsList, " and ")),
		}
	}

	var missingImages []string
	var downloadingImages []string

	// Check which images are needed and their availability
	imagesToCheck := []struct {
		use   bool
		image string
		name  string
	}{
		{opts.Zizmor, ZizmorImage, "zizmor"},
		{opts.Poutine, PoutineImage, "poutine"},
		{opts.Actionlint, ActionlintImage, "actionlint"},
		{opts.RunnerGuard, RunnerGuardImage, "runner-guard"},
		{opts.Syft, SyftImage, "syft"},
		{opts.Grype, GrypeImage, "grype"},
		{opts.Grant, GrantImage, "grant"},
		{opts.Yamllint, YamllintImage, "yamllint"},
	}

	for _, img := range imagesToCheck {
		if !img.use {
			continue
		}

		if IsDockerImageAvailable(ctx, img.image) {
			continue
		}

		if IsDockerImageDownloading(img.image) {
			downloadingImages = append(downloadingImages, img.name)
		} else {
			// Start download
			_, _ = StartDockerImageDownload(ctx, img.image)
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
