//go:build !integration

package cli

import (
	"strings"
	"testing"
)

func TestIsRunningInCodespace(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{
			name:     "CODESPACES=true",
			envValue: "true",
			want:     true,
		},
		{
			name:     "CODESPACES=TRUE (uppercase)",
			envValue: "TRUE",
			want:     true,
		},
		{
			name:     "CODESPACES=True (mixed case)",
			envValue: "True",
			want:     true,
		},
		{
			name:     "CODESPACES=false",
			envValue: "false",
			want:     false,
		},
		{
			name:     "CODESPACES not set",
			envValue: "",
			want:     false,
		},
		{
			name:     "CODESPACES=other",
			envValue: "other",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set test value
			if tt.envValue != "" {
				t.Setenv("CODESPACES", tt.envValue)
			} else {
				// Explicitly unset the variable for the "not set" test case
				t.Setenv("CODESPACES", "")
			}

			got := isRunningInCodespace()
			if got != tt.want {
				t.Errorf("isRunningInCodespace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIs403PermissionError(t *testing.T) {
	tests := []struct {
		name     string
		errorMsg string
		want     bool
	}{
		{
			name:     "403 status code",
			errorMsg: "HTTP 403 Forbidden",
			want:     true,
		},
		{
			name:     "forbidden keyword",
			errorMsg: "access forbidden",
			want:     true,
		},
		{
			name:     "permission denied",
			errorMsg: "permission denied",
			want:     true,
		},
		{
			name:     "403 in middle of message",
			errorMsg: "server returned 403 error",
			want:     true,
		},
		{
			name:     "uppercase FORBIDDEN",
			errorMsg: "FORBIDDEN",
			want:     true,
		},
		{
			name:     "mixed case Permission Denied",
			errorMsg: "Permission Denied",
			want:     true,
		},
		{
			name:     "404 error (not 403)",
			errorMsg: "HTTP 404 Not Found",
			want:     false,
		},
		{
			name:     "generic error",
			errorMsg: "something went wrong",
			want:     false,
		},
		{
			name:     "empty error",
			errorMsg: "",
			want:     false,
		},
		{
			name:     "permission but not denied",
			errorMsg: "checking permissions",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := is403PermissionError(tt.errorMsg)
			if got != tt.want {
				t.Errorf("is403PermissionError(%q) = %v, want %v", tt.errorMsg, got, tt.want)
			}
		})
	}
}

func TestGetCodespacePermissionErrorMessage(t *testing.T) {
	msg := getCodespacePermissionErrorMessage()

	// Test that the message contains key information
	requiredStrings := []string{
		"Codespace",
		"actions:write",
		"workflows:write",
		"GitHub Actions workflows",
		"Solutions:",
		"devcontainer.json",
		"unset GH_TOKEN",
		"gh auth login",
	}

	for _, required := range requiredStrings {
		if !strings.Contains(msg, required) {
			t.Errorf("getCodespacePermissionErrorMessage() missing required string: %q", required)
		}
	}

	// Test that message is not empty
	if len(strings.TrimSpace(msg)) == 0 {
		t.Error("getCodespacePermissionErrorMessage() returned empty message")
	}
}
