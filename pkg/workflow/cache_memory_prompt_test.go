//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCacheMemoryPromptSection_SingleDefaultCache(t *testing.T) {
	config := &CacheMemoryConfig{
		Caches: []CacheMemoryEntry{
			{
				ID:          "default",
				Key:         "",
				Description: "",
			},
		},
	}

	section := buildCacheMemoryPromptSection(config)

	require.NotNil(t, section, "Should return a prompt section for single default cache")
	assert.True(t, section.IsFile, "Should use template file for single default cache")
	assert.Equal(t, cacheMemoryPromptFile, section.Content, "Should reference cache memory prompt file")
	assert.Empty(t, section.ShellCondition, "Should have no shell condition")

	// Verify environment variables
	require.NotNil(t, section.EnvVars, "Should have environment variables")
	assert.Equal(t, "/tmp/gh-aw/cache-memory/", section.EnvVars["GH_AW_CACHE_DIR"], "Should have correct cache directory")
	assert.Empty(t, section.EnvVars["GH_AW_CACHE_DESCRIPTION"], "Should have empty description when not provided")
}

func TestBuildCacheMemoryPromptSection_SingleDefaultCacheWithDescription(t *testing.T) {
	config := &CacheMemoryConfig{
		Caches: []CacheMemoryEntry{
			{
				ID:          "default",
				Key:         "",
				Description: "My custom cache",
			},
		},
	}

	section := buildCacheMemoryPromptSection(config)

	require.NotNil(t, section, "Should return a prompt section")
	assert.True(t, section.IsFile, "Should use template file")
	assert.Equal(t, cacheMemoryPromptFile, section.Content, "Should reference cache memory prompt file")

	// Verify environment variables include description
	require.NotNil(t, section.EnvVars, "Should have environment variables")
	assert.Equal(t, "/tmp/gh-aw/cache-memory/", section.EnvVars["GH_AW_CACHE_DIR"], "Should have correct cache directory")
	assert.Equal(t, " My custom cache", section.EnvVars["GH_AW_CACHE_DESCRIPTION"], "Description should be prefixed with space")
}

func TestBuildCacheMemoryPromptSection_MultipleCaches(t *testing.T) {
	config := &CacheMemoryConfig{
		Caches: []CacheMemoryEntry{
			{
				ID:          "default",
				Key:         "memory-default",
				Description: "",
			},
			{
				ID:          "session",
				Key:         "memory-session",
				Description: "Session-specific cache",
			},
		},
	}

	section := buildCacheMemoryPromptSection(config)

	require.NotNil(t, section, "Should return a prompt section for multiple caches")
	assert.True(t, section.IsFile, "Should use template file for multiple caches")
	assert.Equal(t, cacheMemoryPromptMultiFile, section.Content, "Should reference template file")

	// Verify environment variables are set
	require.NotNil(t, section.EnvVars, "Should have environment variables for template substitution")
	assert.Contains(t, section.EnvVars, "GH_AW_CACHE_LIST", "Should have cache list env var")
	assert.Contains(t, section.EnvVars, "GH_AW_CACHE_EXAMPLES", "Should have cache examples env var")
	assert.Contains(t, section.EnvVars, "GH_AW_ALLOWED_EXTENSIONS", "Should have allowed extensions env var")

	// Verify cache list content
	cacheList := section.EnvVars["GH_AW_CACHE_LIST"]
	assert.Contains(t, cacheList, "- **default**: `/tmp/gh-aw/cache-memory/`", "Should list default cache")
	assert.Contains(t, cacheList, "- **session**: `/tmp/gh-aw/cache-memory-session/` - Session-specific cache", "Should list session cache with description")

	// Verify cache examples content
	cacheExamples := section.EnvVars["GH_AW_CACHE_EXAMPLES"]
	assert.Contains(t, cacheExamples, "/tmp/gh-aw/cache-memory/notes.txt", "Should have examples for default cache")
	assert.Contains(t, cacheExamples, "/tmp/gh-aw/cache-memory-session/notes.txt", "Should have examples for session cache")
}

func TestBuildCacheMemoryPromptSection_SingleNonDefaultCache(t *testing.T) {
	config := &CacheMemoryConfig{
		Caches: []CacheMemoryEntry{
			{
				ID:          "custom",
				Key:         "memory-custom",
				Description: "Custom cache",
			},
		},
	}

	section := buildCacheMemoryPromptSection(config)

	require.NotNil(t, section, "Should return a prompt section")
	assert.True(t, section.IsFile, "Should use template file for non-default single cache")
	assert.Equal(t, "cache_memory_prompt_multi.md", section.Content, "Should reference template file")

	// Verify environment variables
	require.NotNil(t, section.EnvVars, "Should have environment variables for template substitution")
	assert.Contains(t, section.EnvVars, "GH_AW_CACHE_LIST", "Should have cache list env var")

	// Verify cache list content
	cacheList := section.EnvVars["GH_AW_CACHE_LIST"]
	assert.Contains(t, cacheList, "- **custom**: `/tmp/gh-aw/cache-memory-custom/` - Custom cache", "Should list custom cache")
}

func TestBuildCacheMemoryPromptSection_NilConfig(t *testing.T) {
	section := buildCacheMemoryPromptSection(nil)
	assert.Nil(t, section, "Should return nil for nil config")
}

func TestBuildCacheMemoryPromptSection_EmptyCaches(t *testing.T) {
	config := &CacheMemoryConfig{
		Caches: []CacheMemoryEntry{},
	}

	section := buildCacheMemoryPromptSection(config)
	assert.Nil(t, section, "Should return nil for empty caches array")
}

func TestBuildCacheMemoryPromptSection_MultipleCachesWithMixedDescriptions(t *testing.T) {
	config := &CacheMemoryConfig{
		Caches: []CacheMemoryEntry{
			{
				ID:          "default",
				Key:         "",
				Description: "Main cache",
			},
			{
				ID:          "temp",
				Key:         "",
				Description: "",
			},
			{
				ID:          "persistent",
				Key:         "",
				Description: "Long-term storage",
			},
		},
	}

	section := buildCacheMemoryPromptSection(config)

	require.NotNil(t, section, "Should return a prompt section")
	assert.True(t, section.IsFile, "Should use template file for multiple caches")
	assert.Equal(t, "cache_memory_prompt_multi.md", section.Content, "Should reference template file")

	// Verify environment variables are set
	require.NotNil(t, section.EnvVars, "Should have environment variables")

	// Verify all caches are listed in cache list env var
	cacheList := section.EnvVars["GH_AW_CACHE_LIST"]
	assert.Contains(t, cacheList, "- **default**: `/tmp/gh-aw/cache-memory/` - Main cache", "Should list default with description")
	assert.Contains(t, cacheList, "- **temp**: `/tmp/gh-aw/cache-memory-temp/`\n", "Should list temp without description")
	assert.Contains(t, cacheList, "- **persistent**: `/tmp/gh-aw/cache-memory-persistent/` - Long-term storage", "Should list persistent with description")

	// Verify examples for all caches in cache examples env var
	cacheExamples := section.EnvVars["GH_AW_CACHE_EXAMPLES"]
	assert.Contains(t, cacheExamples, "/tmp/gh-aw/cache-memory/notes.txt", "Should have examples for default")
	assert.Contains(t, cacheExamples, "/tmp/gh-aw/cache-memory-temp/notes.txt", "Should have examples for temp")
	assert.Contains(t, cacheExamples, "/tmp/gh-aw/cache-memory-persistent/notes.txt", "Should have examples for persistent")
}
