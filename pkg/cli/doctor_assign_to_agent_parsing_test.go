//go:build !integration

package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDoctorAssignmentRepositoryFromRemote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		remote string
		repo   string
	}{
		{name: "HTTPS", remote: "https://github.com/octo/repo.git", repo: "octo/repo"},
		{name: "HTTPS userinfo resembles hostname", remote: "https://alice%40ghe.example.com@ghe.example.com/octo/repo.git", repo: "octo/repo"},
		{name: "HTTPS token resembles hostname", remote: "https://ghe.example.com:never-echo-this-value@ghe.example.com/octo/repo.git", repo: "octo/repo"},
		{name: "HTTPS port", remote: "https://ghe.example.com:8443/octo/repo.git", repo: "octo/repo"},
		{name: "SSH scheme", remote: "ssh://git@ghe.example.com/octo/repo.git", repo: "octo/repo"},
		{name: "SSH scheme port", remote: "ssh://git@ghe.example.com:2222/octo/repo.git", repo: "octo/repo"},
		{name: "SSH scheme username resembles hostname", remote: "ssh://ghe.example.com@ghe.example.com:2222/octo/repo.git", repo: "octo/repo"},
		{name: "SCP", remote: "git@ghe.example.com:octo/repo.git", repo: "octo/repo"},
		{name: "SCP username resembles hostname", remote: "ghe.example.com@ghe.example.com:octo/repo.git", repo: "octo/repo"},
		{name: "SCP without username", remote: "ghe.example.com:octo/repo.git", repo: "octo/repo"},
		{name: "no suffix", remote: "https://github.com/octo/repo", repo: "octo/repo"},
		{name: "malformed authenticated URL", remote: "https://never-echo-this-value%zz@ghe.example.com/octo/repo.git"},
		{name: "missing host", remote: "ssh:///octo/repo.git"},
		{name: "local path", remote: "/checkout/octo/repo.git"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.repo, doctorAssignmentRepositoryFromRemote(tc.remote))
		})
	}
}

func TestDoctorAssignmentSecretNamesFlattenedFallbacks(t *testing.T) {
	t.Parallel()
	for _, token := range []string{
		"${{ secrets.FIRST || secrets.SECOND || secrets.THIRD || secrets.FOURTH }}",
		"${{ (secrets.FIRST || secrets.SECOND) || (secrets.THIRD || secrets.FOURTH) }}",
	} {
		names, ok := doctorAssignmentSecretNames(token)
		assert.True(t, ok)
		assert.Equal(t, []string{"FIRST", "SECOND", "THIRD", "FOURTH"}, names)
	}
}

func TestDoctorAssignmentCredentialFlattenedFallbacks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		token     string
		secrets   map[string]struct{}
		lookupErr error
		status    string
		secret    string
	}{
		{name: "third available", token: "${{ secrets.FIRST || secrets.SECOND || secrets.THIRD }}", secrets: map[string]struct{}{"THIRD": {}}, status: "configured", secret: "THIRD"},
		{name: "fourth available", token: "${{ secrets.FIRST || secrets.SECOND || secrets.THIRD || secrets.FOURTH }}", secrets: map[string]struct{}{"FOURTH": {}}, status: "configured", secret: "FOURTH"},
		{name: "earlier available wins", token: "${{ secrets.FIRST || secrets.SECOND || secrets.THIRD }}", secrets: map[string]struct{}{"SECOND": {}, "THIRD": {}}, status: "configured", secret: "SECOND"},
		{name: "automatic token blocks third", token: "${{ secrets.FIRST || github.token || secrets.THIRD }}", secrets: map[string]struct{}{"THIRD": {}}, status: "missing"},
		{name: "unknown earlier metadata blocks third", token: "${{ secrets.FIRST || secrets.SECOND || secrets.THIRD }}", secrets: map[string]struct{}{"THIRD": {}}, lookupErr: errors.New("HTTP 403"), status: "unknown"},
		{name: "unknown earlier expression blocks third", token: "${{ secrets.FIRST || inputs.token || secrets.THIRD }}", secrets: map[string]struct{}{"THIRD": {}}, status: "unknown"},
		{name: "unknown third expression is not dropped", token: "${{ secrets.FIRST || secrets.SECOND || inputs.token || secrets.FOURTH }}", secrets: map[string]struct{}{"FOURTH": {}}, status: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, secret, _ := doctorAssignmentCredential(tc.token, tc.secrets, tc.lookupErr)
			assert.Equal(t, tc.status, status)
			assert.Equal(t, tc.secret, secret)
		})
	}
}
