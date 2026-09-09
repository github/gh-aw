//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAddAllowedToNetwork documents the observed behavior of
// addAllowedToNetwork across a range of inputs: a plain network block
// followed by a top-level sibling, a network block with no following
// sibling, an empty domain list, a nested network block, and a network
// block containing a comment line.
func TestAddAllowedToNetwork(t *testing.T) {
	t.Parallel()
	t.Run("network block followed by top-level sibling", func(t *testing.T) {
		t.Parallel()
		lines := []string{
			"network:",
			"  mode: defaults",
			"tools:",
			"  bash: true",
		}
		got := addAllowedToNetwork(lines, []string{"example.com", "api.example.com"})
		want := []string{
			"  allowed:",
			"    - example.com",
			"    - api.example.com",
		}
		assert.Equal(t, want, got)
	})

	t.Run("network block with no following top-level sibling appends at end", func(t *testing.T) {
		t.Parallel()
		lines := []string{
			"tools:",
			"  bash: true",
			"network:",
			"  mode: defaults",
		}
		got := addAllowedToNetwork(lines, []string{"example.com"})
		want := []string{
			"tools:",
			"  bash: true",
			"  allowed:",
			"    - example.com",
			"network:",
			"  mode: defaults",
		}
		assert.Equal(t, want, got)
	})

	t.Run("empty domain list still adds allowed key", func(t *testing.T) {
		t.Parallel()
		lines := []string{
			"network:",
			"  mode: defaults",
		}
		got := addAllowedToNetwork(lines, nil)
		want := []string{
			"  allowed:",
		}
		assert.Equal(t, want, got)
	})

	t.Run("no network block present appends allowed with empty indent", func(t *testing.T) {
		t.Parallel()
		lines := []string{
			"tools:",
			"  bash: true",
		}
		got := addAllowedToNetwork(lines, []string{"example.com"})
		want := []string{
			"tools:",
			"  bash: true",
			"  allowed:",
			"    - example.com",
		}
		assert.Equal(t, want, got)
	})

	t.Run("nested network block under a parent key", func(t *testing.T) {
		t.Parallel()
		lines := []string{
			"mcp-servers:",
			"  server1:",
			"    network:",
			"      mode: defaults",
			"    other: true",
		}
		got := addAllowedToNetwork(lines, []string{"example.com"})
		want := []string{
			"mcp-servers:",
			"  server1:",
			"      allowed:",
			"        - example.com",
			"    network:",
			"      mode: defaults",
			"    other: true",
		}
		assert.Equal(t, want, got)
	})

	t.Run("comment line inside network block is skipped, then top-level sibling ends the block", func(t *testing.T) {
		t.Parallel()
		lines := []string{
			"network:",
			"  # a comment",
			"",
			"  mode: defaults",
			"tools:",
			"  bash: true",
		}
		got := addAllowedToNetwork(lines, []string{"example.com"})
		want := []string{
			"  allowed:",
			"    - example.com",
		}
		assert.Equal(t, want, got)
	})

	t.Run("multiple domains preserve order", func(t *testing.T) {
		t.Parallel()
		lines := []string{
			"network:",
			"  mode: defaults",
			"tools:",
			"  bash: true",
		}
		got := addAllowedToNetwork(lines, []string{"a.com", "b.com", "c.com"})
		want := []string{
			"  allowed:",
			"    - a.com",
			"    - b.com",
			"    - c.com",
		}
		assert.Equal(t, want, got)
	})
}
