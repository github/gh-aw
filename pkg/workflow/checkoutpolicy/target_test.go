package checkoutpolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBase(t *testing.T) {
	for _, tt := range []struct {
		repository, ref string
		want            bool
	}{
		{"", "", true},
		{" ${{github.repository}} ", " ${{ github.event.pull_request.base.sha }} ", true},
		{"", "${{github.event.pull_request.base.ref}}", true},
		{"${{ github['repository'] }}", "${{ github['event']['pull_request']['base']['sha'] }}", true},
		{"", "${{ github.event['pull_request'].base['ref'] }}", true},
		{"", "${{ github['event.pull_request.base.sha'] }}", false},
		{"", "${{ github.event['pull_request.base'].sha }}", false},
		{"", "${{ github.event.pull_request['base.ref'] }}", false},
		{"", "${{ github['event.pull_request.base.ref'] }}", false},
		{"", "${{ GitHub.event.pull_request.base.sha }}", false},
		{"${{ GitHub.repository }}", "", false},
		{"owner/repo", "", false},
		{"", "main", false},
		{"", "${{ github.event.pull_request.head.sha }}", false},
		{"", "${{ github.event.pull_request.base.sha || github.event.pull_request.head.sha }}", false},
		{"${{ github.repository }}suffix", "", false},
		{"${{ github.repository }}${{ github.repository }}", "", false},
		{"${{ github.${{ inputs.key }} }}", "", false},
	} {
		t.Run(tt.repository+"/"+tt.ref, func(t *testing.T) {
			assert.Equal(t, tt.want, Base(tt.repository, tt.ref))
		})
	}
}

func TestLiteralPair(t *testing.T) {
	for _, tt := range []struct {
		repository, ref string
		want            bool
	}{
		{"owner/repo", "main", true},
		{"Owner/Repo", "refs/heads/release", true},
		{"owner/repo", "0123456789abcdef0123456789abcdef01234567", true},
		{"owner/repo", "", false},
		{"owner/*", "main", false},
		{"${{ github.repository }}", "main", false},
		{"owner/repo", "${{ github.event.pull_request.base.sha }}", false},
		{"owner/repo", "refs/pull/42/head", false},
		{"owner/repo", "refs/pull/42/merge", false},
		{"owner/repo", "release/*", false},
		{"owner/repo", "main:other", false},
		{"owner/repo", " main", false},
		{"owner/repo", "main\n", false},
	} {
		t.Run(tt.repository+"/"+tt.ref, func(t *testing.T) {
			assert.Equal(t, tt.want, LiteralPair(tt.repository, tt.ref))
		})
	}
}
