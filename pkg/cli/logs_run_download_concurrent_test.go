//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildConcurrentDownloadParams_RepoOverride(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		repoOverride string
		wantHost     string
		wantOwner    string
		wantRepo     string
	}{
		{name: "owner and repo", repoOverride: "owner/repo", wantOwner: "owner", wantRepo: "repo"},
		{name: "host owner and repo", repoOverride: "ghe.example/owner/repo", wantHost: "ghe.example", wantOwner: "owner", wantRepo: "repo"},
		{name: "empty component", repoOverride: "owner/"},
		{name: "empty host-qualified component", repoOverride: "ghe.example/owner/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := buildConcurrentDownloadParams("", false, tt.repoOverride, nil, false, nil)
			assert.Equal(t, tt.wantHost, params.dlHost)
			assert.Equal(t, tt.wantOwner, params.dlOwner)
			assert.Equal(t, tt.wantRepo, params.dlRepo)
		})
	}
}
