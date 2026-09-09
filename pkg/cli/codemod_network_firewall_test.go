//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetNetworkFirewallCodemod(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	// Verify codemod metadata
	assert.Equal(t, "network-firewall-migration", codemod.ID)
	assert.Equal(t, "Migrate network.firewall to sandbox.agent", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.1.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestNetworkFirewallCodemod_RemovesFirewallTrue(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall: true
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": true,
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "firewall:", "Should remove firewall field")
	assert.Contains(t, result, "sandbox:", "Should add sandbox block")
	assert.Contains(t, result, "agent: awf", "Should add sandbox.agent: awf")
}

func TestNetworkFirewallCodemod_RemovesFirewallFalse(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall: false
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": false,
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "firewall:", "Should remove firewall field")
	assert.Contains(t, result, "sandbox:", "Should add sandbox block")
	assert.Contains(t, result, "agent: false", "Should convert firewall false to sandbox.agent: false")
	assert.NotContains(t, result, "dangerously-disable-sandbox-agent", "Codemod must not silently enable the sandbox opt-out; operator must provide it")
}

func TestNetworkFirewallCodemod_NoNetworkField(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestNetworkFirewallCodemod_NoFirewallField(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  allowed-domains:
    - example.com
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"allowed-domains": []any{"example.com"},
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestNetworkFirewallCodemod_SkipsWhenSandboxExists(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall: true
sandbox:
  agent: custom
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": true,
		},
		"sandbox": map[string]any{
			"agent": "custom",
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "firewall:", "Should remove firewall field")
	// Should not add another sandbox block since one exists
	assert.Contains(t, result, "agent: custom", "Should preserve existing sandbox config")
}

func TestNetworkFirewallCodemod_MigratesFirewallFalseIntoExistingSandbox(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall: false
sandbox:
  mcp: true
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": false,
		},
		"sandbox": map[string]any{
			"mcp": true,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "firewall:", "Should remove firewall field")
	assert.Contains(t, result, "sandbox:", "Should preserve existing sandbox block")
	assert.Contains(t, result, "mcp: true", "Should preserve existing sandbox settings")
	assert.Contains(t, result, "agent: false", "Should migrate firewall false to sandbox.agent: false")
	assert.NotContains(t, result, "dangerously-disable-sandbox-agent", "Codemod must not silently enable the sandbox opt-out; operator must provide it")
}

func TestNetworkFirewallCodemod_PreservesExistingSandboxDisableFeature(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall: false
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  mcp: true
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": false,
		},
		"features": map[string]any{
			"dangerously-disable-sandbox-agent": true,
		},
		"sandbox": map[string]any{
			"mcp": true,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "dangerously-disable-sandbox-agent: true")
}

func TestNetworkFirewallCodemod_MigratesFirewallVersionIntoExistingSandbox(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall:
    version: 0.9
sandbox:
  mcp: true
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": map[string]any{
				"version": 0.9,
			},
		},
		"sandbox": map[string]any{
			"mcp": true,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "firewall:", "Should remove firewall field")
	assert.Contains(t, result, "sandbox:", "Should preserve existing sandbox block")
	assert.Contains(t, result, "mcp: true", "Should preserve existing sandbox settings")
	assert.Contains(t, result, "agent:", "Should add sandbox.agent object")
	assert.Contains(t, result, "id: awf", "Should set sandbox.agent.id")
	assert.Contains(t, result, `version: "0.9"`, "Should migrate firewall.version to sandbox.agent.version")
}

func TestNetworkFirewallCodemod_PreservesOtherNetworkFields(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall: true
  allowed-domains:
    - example.com
  dns:
    - 8.8.8.8
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall":        true,
			"allowed-domains": []any{"example.com"},
			"dns":             []any{"8.8.8.8"},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "firewall:", "Should remove firewall field")
	assert.Contains(t, result, "allowed-domains:", "Should preserve allowed-domains")
	assert.Contains(t, result, "dns:", "Should preserve dns")
	assert.Contains(t, result, "sandbox:", "Should add sandbox block")
}

func TestNetworkFirewallCodemod_PreservesMarkdown(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall: true
---

# Test Workflow

This is a test workflow with:
- Multiple lines
- Markdown formatting

` + "```yaml" + `
key: value
` + "```"

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": true,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "- Multiple lines")
	assert.Contains(t, result, "```yaml")
}

func TestNetworkFirewallCodemod_PreservesComments(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
# Network configuration
network:
  firewall: true  # Enable firewall
  # Other settings
  allowed-domains:
    - example.com
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall":        true,
			"allowed-domains": []any{"example.com"},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Network configuration")
	assert.Contains(t, result, "# Other settings")
	assert.NotContains(t, result, "# Enable firewall", "Should remove comment on firewall line")
}

func TestNetworkFirewallCodemod_FirewallWithNestedProperties(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall:
    enabled: true
    strict: false
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": map[string]any{
				"enabled": true,
				"strict":  false,
			},
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "firewall:")
	assert.NotContains(t, result, "enabled:")
	assert.NotContains(t, result, "strict:")
	assert.Contains(t, result, "sandbox:", "Should add sandbox block")
	assert.Contains(t, result, "agent: awf", "Should convert firewall object to sandbox.agent: awf")
}

func TestNetworkFirewallCodemod_NullFirewallAddsSandboxAgent(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall: null
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": nil,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "firewall:", "Should remove firewall field")
	assert.Contains(t, result, "sandbox:", "Should add sandbox block")
	assert.Contains(t, result, "agent: awf", "Should convert firewall null to sandbox.agent: awf")
}

func TestNetworkFirewallCodemod_PreservesFirewallVersionInSandboxAgent(t *testing.T) {
	t.Parallel()
	codemod := getNetworkFirewallCodemod()

	content := `---
on: workflow_dispatch
network:
  firewall:
    version: v1.2.3
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"firewall": map[string]any{
				"version": "v1.2.3",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "firewall:", "Should remove firewall field")
	assert.Contains(t, result, "sandbox:", "Should add sandbox block")
	assert.Contains(t, result, "id: awf", "Should create sandbox.agent object")
	assert.Contains(t, result, `version: "v1.2.3"`, "Should preserve firewall.version as sandbox.agent.version")
}

func TestNormalizeFirewallVersion_Float32Uses32BitPrecision(t *testing.T) {
	t.Parallel()
	version, ok := normalizeFirewallVersion(float32(0.9))
	require.True(t, ok)
	assert.Equal(t, "0.9", version)
}
