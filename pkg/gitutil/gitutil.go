package gitutil

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var log = logger.New("gitutil:gitutil")

// IsAuthError checks if an error message indicates an authentication issue.
// This is used to detect when GitHub API calls fail due to missing or invalid credentials.
func IsAuthError(errMsg string) bool {
	log.Printf("Checking if error is auth-related: %s", errMsg)
	lowerMsg := strings.ToLower(errMsg)
	isAuth := strings.Contains(lowerMsg, "gh_token") ||
		strings.Contains(lowerMsg, "github_token") ||
		strings.Contains(lowerMsg, "authentication") ||
		strings.Contains(lowerMsg, "not logged into") ||
		strings.Contains(lowerMsg, "unauthorized") ||
		strings.Contains(lowerMsg, "forbidden") ||
		strings.Contains(lowerMsg, "permission denied")
	if isAuth {
		log.Print("Detected authentication error")
	}
	return isAuth
}

// IsHexString checks if a string contains only hexadecimal characters.
// This is used to validate Git commit SHAs and other hexadecimal identifiers.
func IsHexString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
