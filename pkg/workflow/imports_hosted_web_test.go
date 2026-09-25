package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeHostedWebPolicy(t *testing.T) {
	tests := []struct {
		name     string
		top      *HostedWebPolicy
		imported *HostedWebPolicy
		want     *HostedWebPolicy
	}{
		{
			name:     "copies imported policy when top-level policy is absent",
			imported: &HostedWebPolicy{Enabled: true, Allowed: []string{"microsoft.com"}, MaxUses: 5},
			want:     &HostedWebPolicy{Enabled: true, Allowed: []string{"microsoft.com"}, MaxUses: 5},
		},
		{
			name:     "top-level disablement takes precedence",
			top:      &HostedWebPolicy{},
			imported: &HostedWebPolicy{Enabled: true, Allowed: []string{"microsoft.com"}},
			want:     &HostedWebPolicy{},
		},
		{
			name:     "imported disablement does not override an enabled top-level policy",
			top:      &HostedWebPolicy{Enabled: true, Allowed: []string{"archive.org"}},
			imported: &HostedWebPolicy{},
			want:     &HostedWebPolicy{Enabled: true, Allowed: []string{"archive.org"}},
		},
		{
			name:     "ignores disabled import when top-level policy is absent",
			imported: &HostedWebPolicy{},
			want:     nil,
		},
		{
			name:     "merges unique allowed domains while retaining top-level max uses",
			top:      &HostedWebPolicy{Enabled: true, Allowed: []string{"archive.org"}, MaxUses: 1},
			imported: &HostedWebPolicy{Enabled: true, Allowed: []string{"archive.org", "microsoft.com"}, MaxUses: 5},
			want:     &HostedWebPolicy{Enabled: true, Allowed: []string{"archive.org", "microsoft.com"}, MaxUses: 1},
		},
		{
			name:     "merges unique blocked domains",
			top:      &HostedWebPolicy{Enabled: true, Blocked: []string{"ads.example.com"}},
			imported: &HostedWebPolicy{Enabled: true, Blocked: []string{"ads.example.com", "track.example.com"}},
			want:     &HostedWebPolicy{Enabled: true, Blocked: []string{"ads.example.com", "track.example.com"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network := &NetworkPermissions{HostedWeb: tt.top}

			mergeHostedWebPolicy(network, tt.imported)

			assert.Equal(t, tt.want, network.HostedWeb)
			if tt.imported != nil && tt.top == nil {
				assert.NotSame(t, tt.imported, network.HostedWeb)
			}
		})
	}
}

func TestAppendUniqueDomains(t *testing.T) {
	tests := []struct {
		name      string
		domains   []string
		additions []string
		want      []string
	}{
		{name: "nil lists", want: nil},
		{name: "appends new additions", domains: []string{"archive.org"}, additions: []string{"web.archive.org"}, want: []string{"archive.org", "web.archive.org"}},
		{name: "duplicate additions", domains: []string{"archive.org"}, additions: []string{"archive.org"}, want: []string{"archive.org"}},
		{name: "deduplicates existing domains", domains: []string{"archive.org", "archive.org"}, want: []string{"archive.org"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, appendUniqueDomains(tt.domains, tt.additions))
		})
	}
}
