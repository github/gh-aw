//go:build !integration

package parser

import (
	"os"
	"testing"
)

// TestMain sets up the test environment
func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	os.Exit(code)
}
