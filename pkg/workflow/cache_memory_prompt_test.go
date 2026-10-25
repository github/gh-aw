//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheMemoryPromptMissingDataContract(t *testing.T) {
	for _, template := range []string{cacheMemoryPromptFile, cacheMemoryPromptMultiFile} {
		t.Run(template, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("prompts", template))
			require.NoError(t, err)
			prompt := string(content)
			assert.Contains(t, prompt, "If the workflow defines initialization, a full analysis, or another fallback for absent state, follow that path and do **not** call `report_missing_data`")
			assert.Contains(t, prompt, "If the workflow requires cached input and provides no fallback, call `report_missing_data`")
			assert.Contains(t, prompt, "`data_type: \"cache_memory\"` and `reason: \"cache_memory_miss\"`")
			assert.Contains(t, prompt, "report unusable required data with `report_missing_data` unless the workflow explicitly defines recovery")
			assert.NotContains(t, prompt, "If you look for data in the cache and do not find any, call")
		})
	}
}

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
	assert.Empty(t, section.ConditionEnvVar, "Should have no condition")

	// Verify environment variables
	require.NotNil(t, section.EnvVars, "Should have environment variables")
	assert.Equal(t, "/tmp/gh-aw/cache-memory/", section.EnvVars["GH_AW_CACHE_DIR"], "Should have correct cache directory")
	assert.Empty(t, section.EnvVars["GH_AW_CACHE_DESCRIPTION"], "Should have empty description when not provided")
	// No file type restrictions → placeholder is replaced with empty string
	assert.Empty(t, section.EnvVars["GH_AW_ALLOWED_EXTENSIONS"], "Should have empty allowed-extensions when none configured")
}

func TestBuildCacheMemoryPromptSection_SingleDefaultCacheWithAllowedExtensions(t *testing.T) {
	config := &CacheMemoryConfig{
		Caches: []CacheMemoryEntry{
			{
				ID:                "default",
				AllowedExtensions: []string{".json", ".txt"},
			},
		},
	}

	section := buildCacheMemoryPromptSection(config)

	require.NotNil(t, section, "Should return a prompt section")
	require.NotNil(t, section.EnvVars, "Should have environment variables")
	assert.Equal(t, "\nAllowed file extensions: .json, .txt.", section.EnvVars["GH_AW_ALLOWED_EXTENSIONS"],
		"Should include allowed extensions guidance")
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
	assert.Equal(t, "My custom cache", section.EnvVars["GH_AW_CACHE_DESCRIPTION"], "Description should match input")
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
	// Neither cache has AllowedExtensions → placeholder replaced with empty string
	assert.Empty(t, section.EnvVars["GH_AW_ALLOWED_EXTENSIONS"], "Should have empty allowed-extensions when none configured")
}

func TestBuildCacheMemoryPromptSection_MultipleCachesWithAllowedExtensions(t *testing.T) {
	config := &CacheMemoryConfig{
		Caches: []CacheMemoryEntry{
			{ID: "default", AllowedExtensions: []string{".json", ".txt"}},
			{ID: "session", AllowedExtensions: []string{".jsonl"}},
		},
	}

	section := buildCacheMemoryPromptSection(config)

	require.NotNil(t, section, "Should return a prompt section")
	require.NotNil(t, section.EnvVars, "Should have environment variables")
	// Union of .json, .jsonl, .txt — sorted
	assert.Equal(t, "\nAllowed file extensions: .json, .jsonl, .txt.", section.EnvVars["GH_AW_ALLOWED_EXTENSIONS"],
		"Should include union of allowed extensions guidance")
}

func TestBuildCacheMemoryPromptSection_ExtensionOrdering(t *testing.T) {
	tests := []struct {
		name       string
		extensions [][]string
		want       string
	}{
		{"identical lists preserve order and duplicates", [][]string{{".txt", ".json", ".txt"}, {".txt", ".json", ".txt"}}, "\nAllowed file extensions: .txt, .json, .txt."},
		{"different orders produce sorted union", [][]string{{".txt", ".json"}, {".json", ".txt"}}, "\nAllowed file extensions: .json, .txt."},
		{"mixed unrestricted and restricted", [][]string{nil, {".txt"}}, "\nAllowed file extensions: .txt."},
		{"all unrestricted", [][]string{nil, {}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &CacheMemoryConfig{}
			for _, extensions := range tt.extensions {
				config.Caches = append(config.Caches, CacheMemoryEntry{AllowedExtensions: extensions})
			}
			section := buildCacheMemoryPromptSection(config)
			require.NotNil(t, section)
			assert.Equal(t, tt.want, section.EnvVars["GH_AW_ALLOWED_EXTENSIONS"])
			for i, extensions := range tt.extensions {
				assert.Equal(t, extensions, config.Caches[i].AllowedExtensions, "must not mutate configured extensions")
			}
		})
	}
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
