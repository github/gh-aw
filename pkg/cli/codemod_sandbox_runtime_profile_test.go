//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSandboxRuntimeProfileCodemod(t *testing.T) {
	t.Parallel()
	codemod := getSandboxRuntimeProfileCodemod()

	assert.Equal(t, "sandbox-runtime-profiles", codemod.ID)
	assert.NotEmpty(t, codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.42.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestSandboxRuntimeProfileCodemod_Migrations(t *testing.T) {
	t.Parallel()
	codemod := getSandboxRuntimeProfileCodemod()

	tests := []struct {
		name            string
		content         string
		frontmatter     map[string]any
		expectApplied   bool
		expectContains  []string
		expectExcludes  []string
		expectErrSubstr string
	}{
		{
			name: "sudo: false is dropped and keeps the secure default",
			content: `---
on: workflow_dispatch
sandbox:
  agent:
    id: awf
    sudo: false
---

# Test`,
			frontmatter: map[string]any{
				"sandbox": map[string]any{
					"agent": map[string]any{"id": "awf", "sudo": false},
				},
			},
			expectApplied:  true,
			expectContains: []string{"    id: awf"},
			expectExcludes: []string{"sudo:", "runtime:"},
		},
		{
			name: "legacy-security: enable becomes runtime: docker-sudo-iptables",
			content: `---
on: workflow_dispatch
sandbox:
  agent:
    id: awf
    legacy-security: enable
---

# Test`,
			frontmatter: map[string]any{
				"sandbox": map[string]any{
					"agent": map[string]any{"id": "awf", "legacy-security": "enable"},
				},
			},
			expectApplied:  true,
			expectContains: []string{"    runtime: docker-sudo-iptables"},
			expectExcludes: []string{"legacy-security:"},
		},
		{
			name: "sudo: true without a runtime becomes docker-sudo-iptables",
			content: `---
on: workflow_dispatch
sandbox:
  agent:
    sudo: true
    allow-host-ports: [9000]
---

# Test`,
			frontmatter: map[string]any{
				"sandbox": map[string]any{
					"agent": map[string]any{"sudo": true, "allow-host-ports": []any{9000}},
				},
			},
			expectApplied:  true,
			expectContains: []string{"    runtime: docker-sudo-iptables", "    allow-host-ports: [9000]"},
			expectExcludes: []string{"sudo:"},
		},
		{
			name: "sudo: true with runtime: docker becomes docker-sudo-iptables",
			content: `---
on: workflow_dispatch
sandbox:
  agent:
    runtime: docker
    sudo: true
---

# Test`,
			frontmatter: map[string]any{
				"sandbox": map[string]any{
					"agent": map[string]any{"runtime": "docker", "sudo": true},
				},
			},
			expectApplied:  true,
			expectExcludes: []string{"sudo:"},
		},
		{
			name: "rewritten runtime line keeps its trailing comment",
			content: `---
on: workflow_dispatch
sandbox:
  agent:
    runtime: docker # keep this note
    sudo: true
---

# Test`,
			frontmatter: map[string]any{
				"sandbox": map[string]any{
					"agent": map[string]any{"runtime": "docker", "sudo": true},
				},
			},
			expectApplied:  true,
			expectContains: []string{"    runtime: docker-sudo-iptables # keep this note"},
			expectExcludes: []string{"sudo:"},
		},
		{
			name: "workflow without sandbox.agent is untouched",
			content: `---
on: workflow_dispatch
engine: copilot
---

# Test`,
			frontmatter:   map[string]any{"engine": "copilot"},
			expectApplied: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, applied, err := codemod.Apply(tt.content, tt.frontmatter)

			if tt.expectErrSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectErrSubstr)
				assert.False(t, applied)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectApplied, applied)
			if !tt.expectApplied {
				assert.Equal(t, tt.content, result)
				return
			}
			for _, expected := range tt.expectContains {
				assert.Contains(t, result, expected)
			}
			for _, excluded := range tt.expectExcludes {
				assert.NotContains(t, result, excluded)
			}
		})
	}
}

// TestSandboxRuntimeProfileCodemod_RemovesEmptySandboxBlock verifies that dropping the
// only key under sandbox.agent also removes the now-empty parent mappings, which would
// otherwise fail schema validation as a null value.
func TestSandboxRuntimeProfileCodemod_RemovesEmptySandboxBlock(t *testing.T) {
	t.Parallel()
	codemod := getSandboxRuntimeProfileCodemod()

	content := `---
on: workflow_dispatch
sandbox:
  agent:
    sudo: false
engine: copilot
---

# Test`

	frontmatter := map[string]any{
		"sandbox": map[string]any{
			"agent": map[string]any{"sudo": false},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "sandbox:")
	assert.NotContains(t, result, "agent:")
	assert.Contains(t, result, "engine: copilot")
}
