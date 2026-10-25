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

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/logger"
)

var dockerImagesLog = logger.New("cli:docker_images")
var dockerMissingNotice sync.Once

const dockerUnavailableMessage = "Docker is unavailable; Docker-based static analysis will be skipped."
const dockerImageDownloadLifetime = 5 * time.Minute

func isDockerCLIAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		dockerImagesLog.Printf("Docker executable unavailable: %v", err)
		dockerMissingNotice.Do(func() {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Docker is not installed; Docker-based features are unavailable."))
		})
		return false
	}
	return true
}

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
	ZizmorImage      = "ghcr.io/zizmorcore/zizmor:1.31.1@sha256:42d409dbb851d4c5d35a058cf042b828bea1e3f2884378c526eb0974780e18d9"
	PoutineImage     = "ghcr.io/boostsecurityio/poutine:1.1.6@sha256:722a8e0999b583c1540fe2974e691032b2d9d21b9256a17965132b6bfd0081b0"
	ActionlintImage  = "rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667"
	RunnerGuardImage = "ghcr.io/vigilant-llc/runner-guard:3.1.5@sha256:2df426ef96d21f1622e05b21329f26bd263fc46110609cefb6afe43457613ac0"
	SyftImage        = "anchore/syft:v1.55.0@sha256:4cc92b64ca74ffca4c94d4c8fcc01f217731375300be14d0b6eeb2e82eaa110a"
	GrypeImage       = "anchore/grype:v0.121.0@sha256:621d56d1d68106b787022c81546f7e0a36202a25d1239f0c20195fab8c16f7ea"
	GrantImage       = "anchore/grant:v0.6.8@sha256:172463611795f43b77302cdfbd7b3f81295492a7330e0820cfe41c3674920237"
	YamllintImage    = "pipelinecomponents/yamllint:latest@sha256:f7f7b4f8824839e33690a880dbe20944128b0dc9c55522a0b92a29b3a905cecf"
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
	if !isDockerCLIAvailable() {
		return false
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
	if !isDockerCLIAvailable() {
		return false
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
// Returns true if the download was started, false if already downloading, available,
// or Docker is not installed.
// The download can be cancelled by cancelling the provided context.
// The returned join function blocks until the download goroutine exits and returns
// any error that occurred (nil on success or context cancellation).
func StartDockerImageDownload(ctx context.Context, image string) (bool, func() error) {
	ctx = ctxutil.OrBackground(ctx)
	return startDockerImageDownload(ctx, ctx, image)
}

func startDockerImageDownload(probeCtx, downloadCtx context.Context, image string) (bool, func() error) {
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

	if !pullState.mockAvailableInUse && !isDockerCLIAvailable() {
		return false, func() error {
			return &DockerUnavailableError{Message: "Docker is not installed; image downloads are unavailable."}
		}
	}

	// Check if already available (inside lock for atomicity)
	if isDockerImageAvailableUnlocked(probeCtx, image) {
		dockerImagesLog.Printf("Image %s is already available", image)
		return false, func() error { return nil }
	}
	if err := probeCtx.Err(); err != nil {
		return false, func() error { return err }
	}

	dl := &inflightDownload{done: make(chan struct{})}
	pullState.downloading[image] = true
	pullState.inflight[image] = dl

	go runDockerImageDownload(downloadCtx, image, dl)

	return true, func() error {
		<-dl.done
		return dl.err
	}
}

func runDockerImageDownload(ctx context.Context, image string, dl *inflightDownload) {
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
	lastErr = downloadDockerImageWithRetries(ctx, image)
}

func downloadDockerImageWithRetries(ctx context.Context, image string) error {
	dockerImagesLog.Printf("Starting download of image %s", image)
	const maxAttempts = 3
	waitTime := 5
	var lastOutput []byte
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			dockerImagesLog.Printf("Download of image %s cancelled: %v", image, ctx.Err())
			return lastErr
		}
		dockerImagesLog.Printf("Attempt %d of %d: Pulling image %s", attempt, maxAttempts, image)
		output, err := exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput()
		if err == nil {
			dockerImagesLog.Printf("Successfully downloaded image %s", image)
			return nil
		}
		lastErr = err
		lastOutput = output
		if attempt < maxAttempts {
			dockerImagesLog.Printf("Failed to download image %s (attempt %d/%d). Retrying in %ds...", image, attempt, maxAttempts, waitTime)
			timer := time.NewTimer(time.Duration(waitTime) * time.Second)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				dockerImagesLog.Printf("Download of image %s cancelled during retry wait: %v", image, ctx.Err())
				return lastErr
			}
			waitTime *= 2
		}
	}
	dockerImagesLog.Printf("Failed to download image %s after %d attempts: %v\nOutput: %s", image, maxAttempts, lastErr, string(lastOutput))
	return lastErr
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
	return checkAndPrepareDockerImages(ctx, opts, StartDockerImageDownload)
}

// CheckAndPrepareDockerImagesWithDetachedDownloads keeps image probes and
// started pulls bounded by ctx and the download lifetime.
func CheckAndPrepareDockerImagesWithDetachedDownloads(ctx context.Context, opts DockerImagesOptions) error {
	return checkAndPrepareDockerImages(ctx, opts, func(probeCtx context.Context, image string) (bool, func() error) {
		downloadCtx, cancel := context.WithTimeout(probeCtx, dockerImageDownloadLifetime)
		started, join := startDockerImageDownload(probeCtx, downloadCtx, image)
		if !started {
			cancel()
			return started, join
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					dockerImagesLog.Printf("Panic while waiting for Docker image download: %v", r)
				}
			}()
			defer cancel()
			_ = join()
		}()
		return started, join
	})
}

func checkAndPrepareDockerImages(
	ctx context.Context,
	opts DockerImagesOptions,
	startDownload func(context.Context, string) (bool, func() error),
) error {
	if !opts.hasRequestedImage() {
		return nil
	}
	opts = disableAvailableNativeScanners(ctx, opts)
	if !opts.hasRequestedImage() {
		return nil
	}
	if !IsDockerAvailable(ctx) {
		return &DockerUnavailableError{Message: dockerUnavailableMessage}
	}
	missingImages, downloadingImages, err := collectDockerImageStatuses(ctx, opts, startDownload)
	if err != nil {
		return err
	}
	return dockerImagePreparationRetryError(missingImages, downloadingImages)
}

func (opts DockerImagesOptions) hasRequestedImage() bool {
	return opts.Zizmor || opts.Poutine || opts.Actionlint || opts.RunnerGuard ||
		opts.Syft || opts.Grype || opts.Grant || opts.Yamllint
}

func disableAvailableNativeScanners(ctx context.Context, opts DockerImagesOptions) DockerImagesOptions {
	if opts.Zizmor && localScannerPath(ctxutil.OrBackground(ctx), "zizmor") != "" {
		opts.Zizmor = false
	}
	if opts.Poutine && localScannerPath(ctxutil.OrBackground(ctx), "poutine") != "" {
		opts.Poutine = false
	}
	if opts.Actionlint && localScannerPath(ctxutil.OrBackground(ctx), "actionlint") != "" {
		opts.Actionlint = false
	}
	return opts
}

type dockerImageRequest struct {
	use   bool
	image string
	name  string
}

func collectDockerImageStatuses(
	ctx context.Context,
	opts DockerImagesOptions,
	startDownload func(context.Context, string) (bool, func() error),
) ([]string, []string, error) {
	imagesToCheck := []dockerImageRequest{
		{opts.Zizmor, ZizmorImage, "zizmor"},
		{opts.Poutine, PoutineImage, "poutine"},
		{opts.Actionlint, ActionlintImage, "actionlint"},
		{opts.RunnerGuard, RunnerGuardImage, "runner-guard"},
		{opts.Syft, SyftImage, "syft"},
		{opts.Grype, GrypeImage, "grype"},
		{opts.Grant, GrantImage, "grant"},
		{opts.Yamllint, YamllintImage, "yamllint"},
	}
	var missingImages, downloadingImages []string
	for _, img := range imagesToCheck {
		if !img.use || IsDockerImageAvailable(ctx, img.image) {
			continue
		}
		if IsDockerImageDownloading(img.image) {
			downloadingImages = append(downloadingImages, img.name)
			continue
		}
		started, join := startDownload(ctx, img.image)
		if started {
			missingImages = append(missingImages, img.name)
		} else if IsDockerImageDownloading(img.image) {
			downloadingImages = append(downloadingImages, img.name)
		} else if err := join(); err != nil {
			return nil, nil, err
		}
	}
	return missingImages, downloadingImages, nil
}

func dockerImagePreparationRetryError(missingImages, downloadingImages []string) error {
	if len(downloadingImages) == 0 && len(missingImages) == 0 {
		return nil
	}
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
