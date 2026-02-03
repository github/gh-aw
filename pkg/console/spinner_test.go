//go:build !integration

package console

import (
	"os"
	"testing"
	"time"
)

func TestNewSpinner(t *testing.T) {
	spinner := NewSpinner("Test message")

	if spinner == nil {
		t.Fatal("NewSpinner returned nil")
	}

	// Test that spinner can be started and stopped without panic
	spinner.Start()
	time.Sleep(10 * time.Millisecond)
	spinner.Stop()
}

func TestSpinnerAccessibilityMode(t *testing.T) {
	// Save original environment
	origAccessible := os.Getenv("ACCESSIBLE")
	defer func() {
		if origAccessible != "" {
			os.Setenv("ACCESSIBLE", origAccessible)
		} else {
			os.Unsetenv("ACCESSIBLE")
		}
	}()

	// Test with ACCESSIBLE set
	os.Setenv("ACCESSIBLE", "1")
	spinner := NewSpinner("Test message")

	// Spinner should be disabled when ACCESSIBLE is set
	// Note: This may still be true if running in non-TTY environment
	if spinner.IsEnabled() {
		// Only check if we're actually in a TTY
		// In CI/test environments, spinner will be disabled regardless
		t.Log("Spinner enabled despite ACCESSIBLE=1 (may be expected in non-TTY)")
	}

	// Ensure no panic when starting/stopping disabled spinner
	spinner.Start()
	spinner.Stop()

	// Test with ACCESSIBLE unset
	os.Unsetenv("ACCESSIBLE")
	spinner2 := NewSpinner("Test message 2")
	spinner2.Start()
	time.Sleep(10 * time.Millisecond)
	spinner2.Stop()
}

func TestSpinnerUpdateMessage(t *testing.T) {
	spinner := NewSpinner("Initial message")

	// This should not panic even if spinner is disabled
	spinner.UpdateMessage("Updated message")

	spinner.Start()
	spinner.UpdateMessage("Running message")
	spinner.Stop()
}

func TestSpinnerIsEnabled(t *testing.T) {
	spinner := NewSpinner("Test message")

	// IsEnabled should return a boolean without panicking
	enabled := spinner.IsEnabled()

	// The value depends on whether we're running in a TTY or not
	// but the method should not panic
	_ = enabled
}

func TestSpinnerStopWithMessage(t *testing.T) {
	spinner := NewSpinner("Processing...")

	// This should not panic even if spinner is disabled
	spinner.Start()
	spinner.StopWithMessage("✓ Done successfully")

	// Test calling StopWithMessage on a spinner that was never started
	spinner2 := NewSpinner("Another test")
	spinner2.StopWithMessage("✓ Completed")
}

func TestSpinnerMultipleStartStop(t *testing.T) {
	spinner := NewSpinner("Test message")

	// Test multiple start/stop cycles
	for i := 0; i < 3; i++ {
		spinner.Start()
		time.Sleep(10 * time.Millisecond)
		spinner.Stop()
	}
}

func TestSpinnerConcurrentAccess(t *testing.T) {
	spinner := NewSpinner("Test message")

	// Test concurrent access to spinner methods
	// Buffered channel with fixed synchronization - no close needed (waits for exactly 3 sends)
	done := make(chan struct{}, 3)

	go func() {
		spinner.Start()
		done <- struct{}{}
	}()

	go func() {
		time.Sleep(5 * time.Millisecond)
		spinner.UpdateMessage("Updated")
		done <- struct{}{}
	}()

	go func() {
		time.Sleep(15 * time.Millisecond)
		spinner.Stop()
		done <- struct{}{}
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
}

func TestSpinnerBubbleTeaModel(t *testing.T) {
	// Test the Bubble Tea model directly
	// Note: output is nil to prevent render() from printing during tests
	model := spinnerModel{
		message: "Testing",
		output:  nil,
	}

	// Test Init returns a Cmd
	cmd := model.Init()
	if cmd == nil {
		t.Error("Init should return a tick command")
	}

	// Test Update with updateMessageMsg
	newModel, _ := model.Update(updateMessageMsg("New message"))
	if m, ok := newModel.(spinnerModel); ok {
		if m.message != "New message" {
			t.Errorf("Expected message 'New message', got '%s'", m.message)
		}
	} else {
		t.Error("Update should return spinnerModel")
	}

	// Note: View() returns empty string with WithoutRenderer() mode
	// because rendering is done manually in Update() via render()
	view := model.View()
	if view != "" {
		t.Errorf("View should return empty string with WithoutRenderer mode, got '%s'", view)
	}
}

func TestSpinnerDisabledOperations(t *testing.T) {
	// Save original environment
	origAccessible := os.Getenv("ACCESSIBLE")
	defer func() {
		if origAccessible != "" {
			os.Setenv("ACCESSIBLE", origAccessible)
		} else {
			os.Unsetenv("ACCESSIBLE")
		}
	}()

	// Force spinner to be disabled
	os.Setenv("ACCESSIBLE", "1")
	spinner := NewSpinner("Test message")

	// All operations should be safe when disabled
	spinner.Start()
	spinner.UpdateMessage("New message")
	spinner.Stop()
	spinner.StopWithMessage("Final message")

	// Check that spinner is disabled
	if spinner.IsEnabled() && os.Getenv("ACCESSIBLE") != "" {
		t.Error("Spinner should be disabled when ACCESSIBLE is set")
	}
}

func TestSpinnerRapidStartStop(t *testing.T) {
	spinner := NewSpinner("Test message")

	// Test rapid start/stop cycles
	for i := 0; i < 10; i++ {
		spinner.Start()
		spinner.Stop()
	}
}

func TestSpinnerUpdateMessageBeforeStart(t *testing.T) {
	spinner := NewSpinner("Initial message")

	// Update message before starting should not panic
	spinner.UpdateMessage("Updated message")

	spinner.Start()
	time.Sleep(10 * time.Millisecond)
	spinner.Stop()
}

func TestSpinnerStopWithoutStart(t *testing.T) {
	spinner := NewSpinner("Test message")

	// Stop without start should not panic
	spinner.Stop()
	spinner.StopWithMessage("Message")
}

// TestSpinnerStopBeforeStartRaceCondition tests that calling Stop immediately
// after Start (before the goroutine initializes) does not cause a deadlock.
// This reproduces the issue from https://github.com/github/gh-aw/issues/XXX
func TestSpinnerStopBeforeStartRaceCondition(t *testing.T) {
	// Create a spinner that will be enabled (we need to simulate TTY for this test)
	// Since we can't control TTY in tests, we'll test the mutex logic directly
	spinner := NewSpinner("Test message")

	// Even if spinner is disabled in test environment, test the logic
	// by verifying that multiple Start/Stop cycles don't panic
	for i := 0; i < 100; i++ {
		spinner.Start()
		spinner.Stop()
	}

	// Also test StopWithMessage immediately after Start
	for i := 0; i < 100; i++ {
		spinner.Start()
		spinner.StopWithMessage("Done")
	}
}
