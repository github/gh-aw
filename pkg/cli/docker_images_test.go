//go:build !integration

package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func useDockerFallback(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestCheckAndPrepareDockerImages_NoToolsRequested(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	// When no tools are requested, should return nil
	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{})
	if err != nil {
		t.Errorf("Expected no error when no tools requested, got: %v", err)
	}
}

func TestCheckAndPrepareDockerImages_ImageAlreadyDownloading(t *testing.T) {
	useDockerFallback(t)
	// Reset state before test
	ResetDockerPullState()

	// Mock the image as not available
	SetMockImageAvailable(ZizmorImage, false)
	// Simulate an image that's already downloading
	SetDockerImageDownloading(ZizmorImage, true)

	// Should return an error indicating to retry
	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true})
	if err == nil {
		t.Error("Expected error when image is downloading, got nil")
	}

	// Error message should mention downloading and retry
	if err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "downloading") && !strings.Contains(errMsg, "retry") {
			t.Errorf("Expected error to mention downloading and retry, got: %s", errMsg)
		}
	}

	// Clean up
	ResetDockerPullState()
}

func TestDockerImageDownloadState(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	testImage := "test/image:latest"

	// Initially should not be downloading
	if IsDockerImageDownloading(testImage) {
		t.Error("Expected image to not be downloading initially")
	}

	// Set as downloading
	SetDockerImageDownloading(testImage, true)
	if !IsDockerImageDownloading(testImage) {
		t.Error("Expected image to be downloading after setting")
	}

	// Unset
	SetDockerImageDownloading(testImage, false)
	if IsDockerImageDownloading(testImage) {
		t.Error("Expected image to not be downloading after unsetting")
	}
}

func TestResetDockerPullState(t *testing.T) {
	// Set some state
	SetDockerImageDownloading("test/image1:latest", true)
	SetDockerImageDownloading("test/image2:latest", true)
	SetMockImageAvailable("test/image1:latest", true)

	// Reset
	ResetDockerPullState()

	// Verify all state is cleared
	if IsDockerImageDownloading("test/image1:latest") {
		t.Error("Expected image1 to not be downloading after reset")
	}
	if IsDockerImageDownloading("test/image2:latest") {
		t.Error("Expected image2 to not be downloading after reset")
	}
}

func TestDockerImageConstants(t *testing.T) {
	t.Parallel()
	// Verify constants are defined correctly
	if ZizmorImage == "" {
		t.Error("ZizmorImage constant should not be empty")
	}
	if PoutineImage == "" {
		t.Error("PoutineImage constant should not be empty")
	}
	if ActionlintImage == "" {
		t.Error("ActionlintImage constant should not be empty")
	}
	if RunnerGuardImage == "" {
		t.Error("RunnerGuardImage constant should not be empty")
	}
	if SyftImage == "" {
		t.Error("SyftImage constant should not be empty")
	}
	if GrypeImage == "" {
		t.Error("GrypeImage constant should not be empty")
	}
	if GrantImage == "" {
		t.Error("GrantImage constant should not be empty")
	}
	if ShellcheckImage == "" {
		t.Error("ShellcheckImage constant should not be empty")
	}

	// Verify they are docker image references
	expectedImages := map[string]string{
		"zizmor":       ZizmorImage,
		"poutine":      PoutineImage,
		"actionlint":   ActionlintImage,
		"runner-guard": RunnerGuardImage,
		"syft":         SyftImage,
		"grype":        GrypeImage,
		"grant":        GrantImage,
		"shellcheck":   ShellcheckImage,
	}

	for name, image := range expectedImages {
		if !strings.Contains(image, "/") && !strings.Contains(image, ":") {
			t.Errorf("%s image %s does not look like a Docker image reference", name, image)
		}
	}
}

func TestCheckAndPrepareDockerImages_MultipleImages(t *testing.T) {
	useDockerFallback(t)
	// Reset state before test
	ResetDockerPullState()

	// Mock all images as not available
	SetMockImageAvailable(ZizmorImage, false)
	SetMockImageAvailable(PoutineImage, false)
	SetMockImageAvailable(ActionlintImage, false)

	// Simulate multiple images already downloading
	SetDockerImageDownloading(ZizmorImage, true)
	SetDockerImageDownloading(PoutineImage, true)

	// Request all tools
	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true, Poutine: true, Actionlint: true})
	if err == nil {
		t.Error("Expected error when images are downloading, got nil")
	}

	// Error should mention downloading images
	if err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "downloading") && !strings.Contains(errMsg, "retry") {
			t.Errorf("Expected error to mention downloading and retry, got: %s", errMsg)
		}
	}

	// Clean up
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_RetryMessageFormat(t *testing.T) {
	useDockerFallback(t)
	// Reset state before test
	ResetDockerPullState()

	// Mock the image as not available
	SetMockImageAvailable(ZizmorImage, false)
	// Simulate zizmor downloading
	SetDockerImageDownloading(ZizmorImage, true)

	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true})
	if err == nil {
		t.Fatal("Expected error when image is downloading")
	}

	errMsg := err.Error()

	// Verify the message contains key elements
	expectations := []string{
		"Docker images are being downloaded",
		"Please wait and retry",
		"Currently downloading",
		"Retry in 15-30 seconds",
	}

	for _, expected := range expectations {
		if !strings.Contains(errMsg, expected) {
			t.Errorf("Expected error message to contain '%s', got: %s", expected, errMsg)
		}
	}

	// Clean up
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_StartedDownloadingMessage(t *testing.T) {
	useDockerFallback(t)
	// Reset state before test
	ResetDockerPullState()

	// Mock the image as not available
	SetMockImageAvailable(ZizmorImage, false)
	// Simulate that we just started downloading by checking the message format
	// when the image is marked as downloading
	SetDockerImageDownloading(ZizmorImage, true)

	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true})
	if err == nil {
		t.Fatal("Expected error when image is downloading")
	}

	errMsg := err.Error()

	// Should contain zizmor since it's downloading
	if !strings.Contains(errMsg, "zizmor") {
		t.Errorf("Expected error message to mention zizmor, got: %s", errMsg)
	}

	// Clean up
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_ImageAlreadyAvailable(t *testing.T) {
	useDockerFallback(t)
	// Reset state before test
	ResetDockerPullState()

	// Mock the image as available
	SetMockImageAvailable(ZizmorImage, true)

	// Should not return an error since the image is available
	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true})
	if err != nil {
		t.Errorf("Expected no error when image is available, got: %v", err)
	}

	// Clean up
	ResetDockerPullState()
}

func TestIsDockerImageAvailable_WithMockedState(t *testing.T) {
	// This tests the state tracking without actually checking Docker
	ResetDockerPullState()

	// By default, a random image shouldn't be marked as downloading
	testImage := "nonexistent/test:v1.0.0"
	if IsDockerImageDownloading(testImage) {
		t.Error("Random image should not be marked as downloading by default")
	}

	// Set it as downloading
	SetDockerImageDownloading(testImage, true)
	if !IsDockerImageDownloading(testImage) {
		t.Error("Image should be marked as downloading after SetDockerImageDownloading")
	}

	// Clean up
	ResetDockerPullState()
}

func TestMockImageAvailability(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	testImage := "test/mock-image:v1.0.0"

	// Mock the image as available
	SetMockImageAvailable(testImage, true)
	if !IsDockerImageAvailable(context.Background(), testImage) {
		t.Error("Mocked image should be reported as available")
	}

	// Mock the same image as not available
	SetMockImageAvailable(testImage, false)
	if IsDockerImageAvailable(context.Background(), testImage) {
		t.Error("Mocked image should be reported as not available")
	}

	// Clean up
	ResetDockerPullState()
}

func TestIsDockerAvailable_NilContext(t *testing.T) {
	ResetDockerPullState()
	SetMockDockerAvailable(true)

	//nolint:staticcheck // Intentionally validating nil context handling behavior.
	if !IsDockerAvailable(nil) {
		t.Error("Expected IsDockerAvailable to handle nil context")
	}

	ResetDockerPullState()
}

func TestIsDockerImageAvailable_NilContext(t *testing.T) {
	ResetDockerPullState()

	testImage := "test/nil-context-image:v1.0.0"
	SetMockImageAvailable(testImage, true)

	//nolint:staticcheck // Intentionally validating nil context handling behavior.
	if !IsDockerImageAvailable(nil, testImage) {
		t.Error("Expected IsDockerImageAvailable to handle nil context")
	}

	ResetDockerPullState()
}

func TestStartDockerImageDownload_ConcurrentCalls(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	testImage := "test/concurrent-image:v1.0.0"

	// Mock the image as not available
	SetMockImageAvailable(testImage, false)

	// Use a cancellable context so we can stop background goroutines after assertions.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Track how many times StartDockerImageDownload returns true (indicating it started a download)
	const numGoroutines = 10
	type result struct {
		started bool
		join    func() error
	}
	results := make(chan result, numGoroutines)

	// Use a channel to synchronize all goroutines to start at roughly the same time
	startChan := make(chan struct{})

	// Launch multiple goroutines that all try to start downloading the same image
	for range numGoroutines {
		go func() {
			<-startChan // Wait for the signal to start
			s, j := StartDockerImageDownload(ctx, testImage)
			results <- result{s, j}
		}()
	}

	// Signal all goroutines to start simultaneously
	close(startChan)

	// Collect all results
	started := make([]bool, 0, numGoroutines)
	joins := make([]func() error, 0, numGoroutines)
	for range numGoroutines {
		r := <-results
		started = append(started, r.started)
		joins = append(joins, r.join)
	}

	// Count how many goroutines successfully started a download
	downloadCount := 0
	for _, didStart := range started {
		if didStart {
			downloadCount++
		}
	}

	// Only ONE goroutine should have successfully started the download
	if downloadCount != 1 {
		t.Errorf("Expected exactly 1 goroutine to start download, but %d did", downloadCount)
	}

	// Verify the image is marked as downloading
	if !IsDockerImageDownloading(testImage) {
		t.Error("Expected image to be marked as downloading")
	}

	// Cancel context and join all download goroutines to prevent goroutine leaks.
	cancel()
	for _, j := range joins {
		j() //nolint:errcheck // error ignored intentionally: test only checks concurrency, not pull result
	}

	// Clean up
	ResetDockerPullState()
}

func TestStartDockerImageDownload_ConcurrentCallsWithAvailableImage(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	testImage := "test/concurrent-available-image:v1.0.0"

	// Mock the image as available
	SetMockImageAvailable(testImage, true)

	// Track how many times StartDockerImageDownload returns true
	const numGoroutines = 10
	started := make([]bool, numGoroutines)

	// Use a channel to synchronize all goroutines
	startChan := make(chan struct{})
	doneChan := make(chan int, numGoroutines)

	// Launch multiple goroutines
	for i := range numGoroutines {
		go func(index int) {
			<-startChan
			started[index], _ = StartDockerImageDownload(context.Background(), testImage)
			doneChan <- index
		}(i)
	}

	// Signal all goroutines to start
	close(startChan)

	// Wait for all to finish
	for range numGoroutines {
		<-doneChan
	}

	// Count successful starts
	downloadCount := 0
	for _, didStart := range started {
		if didStart {
			downloadCount++
		}
	}

	// NO goroutine should have started a download since image is available
	if downloadCount != 0 {
		t.Errorf("Expected 0 goroutines to start download for available image, but %d did", downloadCount)
	}

	// Verify the image is NOT marked as downloading
	if IsDockerImageDownloading(testImage) {
		t.Error("Expected image to not be marked as downloading since it's available")
	}

	// Clean up
	ResetDockerPullState()
}

func TestStartDockerImageDownload_RaceWithExternalDownload(t *testing.T) {
	// This test simulates the scenario where an image becomes available
	// (e.g., externally downloaded) between when we check availability
	// and when we mark it as downloading
	ResetDockerPullState()

	testImage := "test/race-image:v1.0.0"

	// Initially not available
	SetMockImageAvailable(testImage, false)

	// Use a cancellable context so we can stop background goroutines after assertions.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start multiple goroutines attempting to download
	const numGoroutines = 5
	type result struct {
		started bool
		join    func() error
	}
	resultsCh := make(chan result, numGoroutines)

	for range numGoroutines {
		go func() {
			s, j := StartDockerImageDownload(ctx, testImage)
			resultsCh <- result{s, j}
		}()
	}

	// Collect results
	downloadStarts := 0
	joins := make([]func() error, 0, numGoroutines)
	for range numGoroutines {
		r := <-resultsCh
		if r.started {
			downloadStarts++
		}
		joins = append(joins, r.join)
	}

	// Should only have one successful start
	if downloadStarts != 1 {
		t.Errorf("Expected exactly 1 download to start, got %d", downloadStarts)
	}

	// Cancel context and join all download goroutines to prevent goroutine leaks.
	cancel()
	for _, j := range joins {
		j() //nolint:errcheck // error ignored intentionally: test only checks concurrency, not pull result
	}

	// Clean up
	ResetDockerPullState()
}

func TestStartDockerImageDownload_ContextCancellation(t *testing.T) {
	// Test that download respects context cancellation
	ResetDockerPullState()

	testImage := "test/cancel-image:v1.0.0"
	SetMockImageAvailable(testImage, false)

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Start the download
	started, join := StartDockerImageDownload(ctx, testImage)
	if !started {
		t.Fatal("Expected download to start")
	}

	// Verify it's marked as downloading
	if !IsDockerImageDownloading(testImage) {
		t.Error("Expected image to be marked as downloading")
	}

	// Cancel the context immediately
	cancel()

	// Join the download goroutine and ensure cleanup is complete
	join()

	// The image should no longer be marked as downloading after cancellation
	if IsDockerImageDownloading(testImage) {
		t.Error("Expected image to not be downloading after context cancellation")
	}

	// Clean up
	ResetDockerPullState()
}

func TestStartDockerImageDownload_JoinPointForExistingDownload(t *testing.T) {
	ResetDockerPullState()

	testImage := "test/join-existing:v1.0.0"
	SetMockImageAvailable(testImage, false)

	ctx, cancel := context.WithCancel(context.Background())

	startedFirst, joinFirst := StartDockerImageDownload(ctx, testImage)
	if !startedFirst {
		t.Fatal("Expected first call to start download")
	}

	startedSecond, joinSecond := StartDockerImageDownload(ctx, testImage)
	if startedSecond {
		t.Fatal("Expected second call to observe existing download")
	}

	secondJoined := make(chan struct{})
	go func() {
		defer close(secondJoined)
		joinSecond()
	}()

	select {
	case <-secondJoined:
		t.Fatal("Expected second join to block while shared download is still running")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	joinSecond()
	joinFirst()

	if IsDockerImageDownloading(testImage) {
		t.Error("Expected image to not be marked as downloading after joined cancellation")
	}

	ResetDockerPullState()
}

func TestStartDockerImageDownload_JoinPointNoopWhenImageAvailable(t *testing.T) {
	ResetDockerPullState()

	testImage := "test/join-noop:v1.0.0"
	SetMockImageAvailable(testImage, true)

	started, join := StartDockerImageDownload(context.Background(), testImage)
	if started {
		t.Fatal("Expected download not to start for already-available image")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		join()
	}()

	select {
	case <-done:
		// expected: join is a no-op when no goroutine was started
	case <-time.After(2 * time.Second):
		t.Fatal("Expected join to return immediately when image is available")
	}

	ResetDockerPullState()
}

func TestStartDockerImageDownload_NilContext(t *testing.T) {
	ResetDockerPullState()

	testImage := "test/nil-context-download:v1.0.0"
	SetMockImageAvailable(testImage, true)

	//nolint:staticcheck // Intentionally validating nil context handling behavior.
	started, _ := StartDockerImageDownload(nil, testImage)
	if started {
		t.Error("Expected download not to start for available image with nil context")
	}

	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_DockerUnavailable(t *testing.T) {
	useDockerFallback(t)
	// Reset state before test
	ResetDockerPullState()

	// Mock Docker as unavailable
	SetMockDockerAvailable(false)

	// Should return a clear error about Docker not being available
	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true})
	if err == nil {
		t.Fatal("Expected error when Docker is unavailable, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "docker is not available") {
		t.Errorf("Expected error to mention 'docker is not available', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "cannot connect to Docker daemon") {
		t.Errorf("Expected error to mention 'cannot connect to Docker daemon', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "zizmor") {
		t.Errorf("Expected error to mention 'zizmor', got: %s", errMsg)
	}
	if strings.Contains(errMsg, "being downloaded") {
		t.Errorf("Expected error NOT to say 'being downloaded' when Docker is unavailable, got: %s", errMsg)
	}
	// Error message should use parameter syntax, not CLI flag syntax
	if strings.Contains(errMsg, "--zizmor") {
		t.Errorf("Expected error NOT to use CLI flag syntax '--zizmor', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "zizmor: false") {
		t.Errorf("Expected error to suggest 'zizmor: false', got: %s", errMsg)
	}

	// Clean up
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_DockerUnavailable_MultipleTools(t *testing.T) {
	useDockerFallback(t)
	// Reset state before test
	ResetDockerPullState()

	// Mock Docker as unavailable
	SetMockDockerAvailable(false)

	// Request multiple tools
	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true, Actionlint: true})
	if err == nil {
		t.Fatal("Expected error when Docker is unavailable, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "docker is not available") {
		t.Errorf("Expected error to mention 'docker is not available', got: %s", errMsg)
	}
	// Both tools should be mentioned
	if !strings.Contains(errMsg, "zizmor") {
		t.Errorf("Expected error to mention 'zizmor', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "actionlint") {
		t.Errorf("Expected error to mention 'actionlint', got: %s", errMsg)
	}
	// Error message should use parameter syntax, not CLI flag syntax
	if strings.Contains(errMsg, "--zizmor") || strings.Contains(errMsg, "--actionlint") {
		t.Errorf("Expected error NOT to use CLI flag syntax, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "zizmor: false") {
		t.Errorf("Expected error to suggest 'zizmor: false', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "actionlint: false") {
		t.Errorf("Expected error to suggest 'actionlint: false', got: %s", errMsg)
	}

	// Clean up
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_DockerUnavailable_NoTools(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	// Mock Docker as unavailable
	SetMockDockerAvailable(false)

	// When no tools requested, should return nil even if Docker is unavailable
	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{})
	if err != nil {
		t.Errorf("Expected no error when no tools requested (even with Docker unavailable), got: %v", err)
	}

	// Clean up
	ResetDockerPullState()
}

func TestIsDockerAvailable_MockTrue(t *testing.T) {
	ResetDockerPullState()
	SetMockDockerAvailable(true)
	if !IsDockerAvailable(context.Background()) {
		t.Error("Expected IsDockerAvailable to return true when mocked as available")
	}
	ResetDockerPullState()
}

func TestIsDockerAvailable_MockFalse(t *testing.T) {
	ResetDockerPullState()
	SetMockDockerAvailable(false)
	if IsDockerAvailable(context.Background()) {
		t.Error("Expected IsDockerAvailable to return false when mocked as unavailable")
	}
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_DockerUnavailable_ReturnsTypedError(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()
	SetMockDockerAvailable(false)

	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Actionlint: true})
	if err == nil {
		t.Fatal("Expected error when Docker is unavailable, got nil")
	}

	// Verify the error is the typed DockerUnavailableError so callers can distinguish
	// it from transient errors (e.g., images downloading).
	var dockerUnavailableErr *DockerUnavailableError
	if !errors.As(err, &dockerUnavailableErr) {
		t.Errorf("Expected error to be *DockerUnavailableError, got %T: %v", err, err)
	}

	// Clean up
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_RunnerGuardImageDownloading(t *testing.T) {
	useDockerFallback(t)
	// Reset state before test
	ResetDockerPullState()

	// Mock runner-guard image as not available
	SetMockImageAvailable(RunnerGuardImage, false)

	// Simulate multiple images already downloading
	SetDockerImageDownloading(ZizmorImage, true)
	SetDockerImageDownloading(PoutineImage, true)
	SetDockerImageDownloading(RunnerGuardImage, true)

	// Request all tools, including runner-guard
	err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true, Poutine: true, Actionlint: true, RunnerGuard: true})
	if err == nil {
		t.Error("Expected error when images are downloading, got nil")
	}

	// Error should mention downloading images and runner-guard
	if err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "downloading") && !strings.Contains(errMsg, "retry") {
			t.Errorf("Expected error to mention downloading and retry, got: %s", errMsg)
		}
		if !strings.Contains(errMsg, RunnerGuardImage) && !strings.Contains(errMsg, "runner-guard") {
			t.Errorf("Expected error to mention runner-guard image %q or \"runner-guard\", got: %s", RunnerGuardImage, errMsg)
		}
	}

	// Clean up
	ResetDockerPullState()
}
