//go:build !integration

package workflow

import (
	"testing"
)

func TestCompiler_SetFileTracker_Basic(t *testing.T) {
	// Create compiler
	compiler := NewCompiler()

	// Initial state should have nil tracker
	if compiler.fileTracker != nil {
		t.Errorf("Expected initial fileTracker to be nil")
	}

	// Create mock tracker
	mockTracker := &SimpleBasicMockFileTracker{}

	// Set tracker
	compiler.SetFileTracker(mockTracker)

	// Verify tracker was set
	if compiler.fileTracker != mockTracker {
		t.Errorf("Expected tracker to be set")
	}

	// Set to nil
	compiler.SetFileTracker(nil)

	// Verify tracker is nil
	if compiler.fileTracker != nil {
		t.Errorf("Expected tracker to be nil after setting to nil")
	}
}

// SimpleBasicMockFileTracker is a basic implementation for testing
type SimpleBasicMockFileTracker struct {
	tracked []string
}

func (s *SimpleBasicMockFileTracker) TrackCreated(filePath string) {
	s.tracked = append(s.tracked, filePath)
}
