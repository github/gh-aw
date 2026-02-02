//go:build !integration

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/sliceutil"
)

func TestCheckAndPrepareDockerImages_NoToolsRequested(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	// When no tools are requested, should return nil
	err := CheckAndPrepareDockerImages(context.Background(), false, false, false)
	if err != nil {
		t.Errorf("Expected no error when no tools requested, got: %v", err)
	}
}

func TestCheckAndPrepareDockerImages_ImageAlreadyDownloading(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	// Mock the image as not available
	SetMockImageAvailable(ZizmorImage, false)
	// Simulate an image that's already downloading
	SetDockerImageDownloading(ZizmorImage, true)

	// Should return an error indicating to retry
	err := CheckAndPrepareDockerImages(context.Background(), true, false, false)
	if err == nil {
		t.Error("Expected error when image is downloading, got nil")
	}

	// Error message should mention downloading and retry
	if err != nil {
		errMsg := err.Error()
		if !sliceutil.ContainsAny(errMsg, "downloading", "retry") {
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

	// Verify they are docker image references
	expectedImages := map[string]string{
		"zizmor":     ZizmorImage,
		"poutine":    PoutineImage,
		"actionlint": ActionlintImage,
	}

	for name, image := range expectedImages {
		if !sliceutil.ContainsAny(image, "/", ":") {
			t.Errorf("%s image %s does not look like a Docker image reference", name, image)
		}
	}
}

func TestCheckAndPrepareDockerImages_MultipleImages(t *testing.T) {
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
	err := CheckAndPrepareDockerImages(context.Background(), true, true, true)
	if err == nil {
		t.Error("Expected error when images are downloading, got nil")
	}

	// Error should mention downloading images
	if err != nil {
		errMsg := err.Error()
		if !sliceutil.ContainsAny(errMsg, "downloading", "retry") {
			t.Errorf("Expected error to mention downloading and retry, got: %s", errMsg)
		}
	}

	// Clean up
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_RetryMessageFormat(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	// Mock the image as not available
	SetMockImageAvailable(ZizmorImage, false)
	// Simulate zizmor downloading
	SetDockerImageDownloading(ZizmorImage, true)

	err := CheckAndPrepareDockerImages(context.Background(), true, false, false)
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
		if !sliceutil.ContainsAny(errMsg, expected) {
			t.Errorf("Expected error message to contain '%s', got: %s", expected, errMsg)
		}
	}

	// Clean up
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_StartedDownloadingMessage(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	// Mock the image as not available
	SetMockImageAvailable(ZizmorImage, false)
	// Simulate that we just started downloading by checking the message format
	// when the image is marked as downloading
	SetDockerImageDownloading(ZizmorImage, true)

	err := CheckAndPrepareDockerImages(context.Background(), true, false, false)
	if err == nil {
		t.Fatal("Expected error when image is downloading")
	}

	errMsg := err.Error()

	// Should contain zizmor since it's downloading
	if !sliceutil.ContainsAny(errMsg, "zizmor") {
		t.Errorf("Expected error message to mention zizmor, got: %s", errMsg)
	}

	// Clean up
	ResetDockerPullState()
}

func TestCheckAndPrepareDockerImages_ImageAlreadyAvailable(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	// Mock the image as available
	SetMockImageAvailable(ZizmorImage, true)

	// Should not return an error since the image is available
	err := CheckAndPrepareDockerImages(context.Background(), true, false, false)
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
	if !IsDockerImageAvailable(testImage) {
		t.Error("Mocked image should be reported as available")
	}

	// Mock the same image as not available
	SetMockImageAvailable(testImage, false)
	if IsDockerImageAvailable(testImage) {
		t.Error("Mocked image should be reported as not available")
	}

	// Clean up
	ResetDockerPullState()
}

func TestStartDockerImageDownload_ConcurrentCalls(t *testing.T) {
	// Reset state before test
	ResetDockerPullState()

	testImage := "test/concurrent-image:v1.0.0"

	// Mock the image as not available
	SetMockImageAvailable(testImage, false)

	// Track how many times StartDockerImageDownload returns true (indicating it started a download)
	const numGoroutines = 10
	started := make([]bool, numGoroutines)

	// Use a channel to synchronize all goroutines to start at roughly the same time
	startChan := make(chan struct{})
	doneChan := make(chan int, numGoroutines)

	// Launch multiple goroutines that all try to start downloading the same image
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			<-startChan // Wait for the signal to start
			started[index] = StartDockerImageDownload(context.Background(), testImage)
			doneChan <- index
		}(i)
	}

	// Signal all goroutines to start simultaneously
	close(startChan)

	// Wait for all goroutines to finish
	for i := 0; i < numGoroutines; i++ {
		<-doneChan
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
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			<-startChan
			started[index] = StartDockerImageDownload(context.Background(), testImage)
			doneChan <- index
		}(i)
	}

	// Signal all goroutines to start
	close(startChan)

	// Wait for all to finish
	for i := 0; i < numGoroutines; i++ {
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

	// Start multiple goroutines attempting to download
	const numGoroutines = 5
	results := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			results <- StartDockerImageDownload(context.Background(), testImage)
		}()
	}

	// Collect results
	downloadStarts := 0
	for i := 0; i < numGoroutines; i++ {
		if <-results {
			downloadStarts++
		}
	}

	// Should only have one successful start
	if downloadStarts != 1 {
		t.Errorf("Expected exactly 1 download to start, got %d", downloadStarts)
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
	started := StartDockerImageDownload(ctx, testImage)
	if !started {
		t.Fatal("Expected download to start")
	}

	// Verify it's marked as downloading
	if !IsDockerImageDownloading(testImage) {
		t.Error("Expected image to be marked as downloading")
	}

	// Cancel the context immediately
	cancel()

	// Wait a bit for the goroutine to notice the cancellation
	time.Sleep(100 * time.Millisecond)

	// The image should no longer be marked as downloading after cancellation
	if IsDockerImageDownloading(testImage) {
		t.Error("Expected image to not be downloading after context cancellation")
	}

	// Clean up
	ResetDockerPullState()
}
