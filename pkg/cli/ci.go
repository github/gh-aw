package cli

import (
	"os"

	"github.com/github/gh-aw/pkg/logger"
)

var ciLog = logger.New("cli:ci")

// IsRunningInCI checks if we're running in a CI environment
func IsRunningInCI() bool {
	// Common CI environment variables
	ciVars := []string{
		"CI",
		"CONTINUOUS_INTEGRATION",
		"GITHUB_ACTIONS",
	}

	for _, v := range ciVars {
		if os.Getenv(v) != "" {
			ciLog.Printf("CI environment detected via %s", v)
			return true
		}
	}
	ciLog.Print("No CI environment detected")
	return false
}
