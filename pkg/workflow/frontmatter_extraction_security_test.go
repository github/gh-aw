//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractFirewallConfig tests the extraction of firewall configuration from frontmatter
func TestExtractFirewallConfig(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts ssl-bump boolean field", func(t *testing.T) {
		firewallObj := map[string]any{
			"ssl-bump": true,
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.True(t, config.Enabled, "Should be enabled")
		assert.True(t, config.SSLBump, "Should have ssl-bump enabled")
	})

	t.Run("extracts allow-urls string array", func(t *testing.T) {
		firewallObj := map[string]any{
			"ssl-bump": true,
			"allow-urls": []any{
				"https://github.com/githubnext/*",
				"https://api.github.com/repos/*",
			},
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.True(t, config.SSLBump, "Should have ssl-bump enabled")
		assert.Len(t, config.AllowURLs, 2, "Should have 2 allow-urls")
		assert.Equal(t, "https://github.com/githubnext/*", config.AllowURLs[0], "First URL should match")
		assert.Equal(t, "https://api.github.com/repos/*", config.AllowURLs[1], "Second URL should match")
	})

	t.Run("extracts cleanup-script string field", func(t *testing.T) {
		firewallObj := map[string]any{
			"cleanup-script": "/custom/cleanup.sh",
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.Equal(t, "/custom/cleanup.sh", config.CleanupScript, "Should extract cleanup-script")
	})

	t.Run("extracts all fields together", func(t *testing.T) {
		firewallObj := map[string]any{
			"args":           []any{"--custom-arg", "value"},
			"version":        "v1.0.0",
			"log-level":      "debug",
			"ssl-bump":       true,
			"allow-urls":     []any{"https://example.com/*"},
			"cleanup-script": "/path/to/cleanup.sh",
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.True(t, config.Enabled, "Should be enabled")
		assert.Len(t, config.Args, 2, "Should have 2 args")
		assert.Equal(t, "v1.0.0", config.Version, "Should extract version")
		assert.Equal(t, "debug", config.LogLevel, "Should extract log-level")
		assert.True(t, config.SSLBump, "Should have ssl-bump enabled")
		assert.Len(t, config.AllowURLs, 1, "Should have 1 allow-url")
		assert.Equal(t, "https://example.com/*", config.AllowURLs[0], "Should extract allow-url")
		assert.Equal(t, "/path/to/cleanup.sh", config.CleanupScript, "Should extract cleanup-script")
	})

	t.Run("ssl-bump defaults to false when not specified", func(t *testing.T) {
		firewallObj := map[string]any{
			"version": "v1.0.0",
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.False(t, config.SSLBump, "ssl-bump should default to false")
	})

	t.Run("allow-urls defaults to empty when not specified", func(t *testing.T) {
		firewallObj := map[string]any{
			"ssl-bump": true,
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.Nil(t, config.AllowURLs, "allow-urls should be nil when not specified")
	})

	t.Run("cleanup-script defaults to empty when not specified", func(t *testing.T) {
		firewallObj := map[string]any{
			"version": "v1.0.0",
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.Empty(t, config.CleanupScript, "cleanup-script should be empty when not specified")
	})

	t.Run("handles non-string values in allow-urls gracefully", func(t *testing.T) {
		firewallObj := map[string]any{
			"allow-urls": []any{
				"https://github.com/*",
				123, // Invalid: number instead of string
				"https://api.github.com/*",
			},
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.Len(t, config.AllowURLs, 2, "Should skip non-string values")
		assert.Equal(t, "https://github.com/*", config.AllowURLs[0], "First valid URL should be extracted")
		assert.Equal(t, "https://api.github.com/*", config.AllowURLs[1], "Second valid URL should be extracted")
	})

	t.Run("handles non-boolean ssl-bump gracefully", func(t *testing.T) {
		firewallObj := map[string]any{
			"ssl-bump": "true", // String instead of boolean
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.False(t, config.SSLBump, "Should ignore non-boolean ssl-bump value")
	})

	t.Run("handles non-string cleanup-script gracefully", func(t *testing.T) {
		firewallObj := map[string]any{
			"cleanup-script": 123, // Number instead of string
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.Empty(t, config.CleanupScript, "Should ignore non-string cleanup-script value")
	})

	t.Run("handles non-array allow-urls gracefully", func(t *testing.T) {
		firewallObj := map[string]any{
			"allow-urls": "https://github.com/*", // String instead of array
		}

		config := compiler.extractFirewallConfig(firewallObj)
		require.NotNil(t, config, "Should extract firewall config")
		assert.Nil(t, config.AllowURLs, "Should ignore non-array allow-urls value")
	})
}
