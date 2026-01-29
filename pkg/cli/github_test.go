//go:build !integration

package cli

import (
	"testing"
)

func TestGetGitHubHost(t *testing.T) {
	tests := []struct {
		name         string
		serverURL    string
		ghHost       string
		expectedHost string
	}{
		{
			name:         "defaults to github.com",
			serverURL:    "",
			ghHost:       "",
			expectedHost: "https://github.com",
		},
		{
			name:         "uses GITHUB_SERVER_URL when set",
			serverURL:    "https://github.enterprise.com",
			ghHost:       "",
			expectedHost: "https://github.enterprise.com",
		},
		{
			name:         "uses GH_HOST when GITHUB_SERVER_URL not set",
			serverURL:    "",
			ghHost:       "https://github.company.com",
			expectedHost: "https://github.company.com",
		},
		{
			name:         "GITHUB_SERVER_URL takes precedence over GH_HOST",
			serverURL:    "https://github.enterprise.com",
			ghHost:       "https://github.company.com",
			expectedHost: "https://github.enterprise.com",
		},
		{
			name:         "removes trailing slash from GITHUB_SERVER_URL",
			serverURL:    "https://github.enterprise.com/",
			ghHost:       "",
			expectedHost: "https://github.enterprise.com",
		},
		{
			name:         "removes trailing slash from GH_HOST",
			serverURL:    "",
			ghHost:       "https://github.company.com/",
			expectedHost: "https://github.company.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set test env vars (always set to ensure clean state)
			t.Setenv("GITHUB_SERVER_URL", tt.serverURL)
			t.Setenv("GH_HOST", tt.ghHost)

			// Test
			host := getGitHubHost()
			if host != tt.expectedHost {
				t.Errorf("Expected host '%s', got '%s'", tt.expectedHost, host)
			}
		})
	}
}
