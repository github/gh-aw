//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidCacheID(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		valid bool
	}{
		// Valid IDs
		{"simple word", "default", true},
		{"plain word", "session", true},
		{"with hyphen", "my-cache", true},
		{"with underscore", "my_cache", true},
		{"alphanumeric", "cache123", true},
		{"mixed", "A1-b2_C3", true},
		{"exactly 64 chars", strings.Repeat("a", 64), true},

		// Invalid IDs
		{"empty string", "", false},
		{"65 chars", strings.Repeat("a", 65), false},
		{"path traversal dot-dot-slash", "../etc", false},
		{"path traversal nested", "../../etc/passwd", false},
		{"forward slash", "cache/sub", false},
		{"backslash", `cache\sub`, false},
		{"dot separator", "cache.mem", false},
		{"space", "cache mem", false},
		{"colon", "cache:mem", false},
		{"wildcard asterisk", "cache*", false},
		{"question mark", "cache?", false},
		{"angle brackets", "cache<mem>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidCacheID(tt.id), "isValidCacheID(%q)", tt.id)
		})
	}
}

func TestParseCacheMemoryEntry_InvalidID(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name string
		id   string
	}{
		{"path traversal nested", "../../etc"},
		{"forward slash", "cache/sub"},
		{"backslash", `cache\sub`},
		{"dot separator", "cache.mem"},
		{"space", "cache mem"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := map[string]any{
				"cache-memory": []any{
					map[string]any{
						"id": tt.id,
					},
				},
			}

			_, err := compiler.extractCacheMemoryConfigFromMap(tools)
			require.Error(t, err, "Should reject invalid cache ID %q", tt.id)
			require.ErrorContains(t, err, "invalid cache-memory id", "Error message should identify the problem")
		})
	}
}

func TestCacheMemoryDirFor_ValidIDs(t *testing.T) {
	assert.Equal(t, "/tmp/gh-aw/cache-memory", cacheMemoryDirFor("default"))
	assert.Equal(t, "/tmp/gh-aw/cache-memory", cacheMemoryDirFor(""))
	assert.Equal(t, "/tmp/gh-aw/cache-memory-session", cacheMemoryDirFor("session"))
	assert.Equal(t, "/tmp/gh-aw/cache-memory-my-cache", cacheMemoryDirFor("my-cache"))
}

func TestCacheMemoryPathHelpersRejectInvalidIDs(t *testing.T) {
	for _, invalidID := range []string{"../../etc", "bad/id"} {
		assert.Empty(t, cacheMemoryDirFor(invalidID))
		assert.Empty(t, cacheMemoryActionDirFor(invalidID))
		assert.Empty(t, cacheMemoryPromptDirFor(invalidID))
	}
}
