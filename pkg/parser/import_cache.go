package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var importCacheLog = logger.New("parser:import_cache")

const (
	// ImportCacheDir is the directory where cached imports are stored
	ImportCacheDir = ".github/aw/imports"
)

// sanitizePath converts a file path to a safe filename by using filepath.Clean
// and replacing directory separators with underscores
func sanitizePath(path string) string {
	// Clean the path to remove any ".." or other suspicious elements
	cleaned := filepath.Clean(path)
	// Replace directory separators with underscores to create a flat filename
	// This prevents directory traversal while preserving path uniqueness
	sanitized := strings.ReplaceAll(cleaned, string(filepath.Separator), "_")
	return sanitized
}

// validatePathComponents validates that path components don't contain malicious sequences
func validatePathComponents(owner, repo, path, sha string) error {
	components := []string{owner, repo, path, sha}
	for _, comp := range components {
		// Check for empty components
		if comp == "" {
			importCacheLog.Print("Path validation failed: empty component detected")
			return fmt.Errorf("empty component in path")
		}
		// Check for path traversal attempts
		if strings.Contains(comp, "..") {
			importCacheLog.Printf("Path validation failed: path traversal attempt in component: %s", comp)
			return fmt.Errorf("component contains '..' sequence: %s", comp)
		}
		// Check for absolute paths
		if filepath.IsAbs(comp) {
			importCacheLog.Printf("Path validation failed: absolute path in component: %s", comp)
			return fmt.Errorf("component is absolute path: %s", comp)
		}
	}
	importCacheLog.Print("Path validation successful")
	return nil
}

// ImportCache manages cached imported workflow files
type ImportCache struct {
	baseDir string // Base directory for cache (typically repo root)
}

// NewImportCache creates a new import cache instance
func NewImportCache(repoRoot string) *ImportCache {
	importCacheLog.Printf("Creating import cache with base dir: %s", repoRoot)
	return &ImportCache{
		baseDir: repoRoot,
	}
}

// Get retrieves a cached file path if it exists
// sha parameter should be the resolved commit SHA
func (c *ImportCache) Get(owner, repo, path, sha string) (string, bool) {
	// Use SHA-based approach: cache files are stored by commit SHA
	// Cache path: .github/aw/imports/owner/repo/sha/sanitized_path.md
	sanitizedPath := sanitizePath(path)
	relativeCachePath := filepath.Join(ImportCacheDir, owner, repo, sha, sanitizedPath)
	fullCachePath := filepath.Join(c.baseDir, relativeCachePath)

	// Check if the cached file exists
	if _, err := os.Stat(fullCachePath); err != nil {
		if os.IsNotExist(err) {
			importCacheLog.Printf("Cache miss: %s/%s/%s@%s", owner, repo, path, sha)
		} else {
			// Log other types of errors (permissions, I/O issues, etc.)
			importCacheLog.Printf("Cache access error for %s/%s/%s@%s: %v", owner, repo, path, sha, err)
		}
		return "", false
	}

	importCacheLog.Printf("Cache hit: %s/%s/%s@%s -> %s", owner, repo, path, sha, fullCachePath)
	return fullCachePath, true
}

// Set stores a new cache entry by saving the content to the cache directory
// sha parameter should be the resolved commit SHA
func (c *ImportCache) Set(owner, repo, path, sha string, content []byte) (string, error) {
	importCacheLog.Printf("Setting cache entry: %s/%s/%s@%s, size=%d bytes", owner, repo, path, sha, len(content))

	// Validate file size (max 10MB)
	const maxFileSize = 10 * 1024 * 1024
	if len(content) > maxFileSize {
		importCacheLog.Printf("Cache set rejected: file size %d exceeds max %d bytes", len(content), maxFileSize)
		return "", fmt.Errorf("file size (%d bytes) exceeds maximum allowed size (%d bytes)", len(content), maxFileSize)
	}

	// Validate path components to prevent path traversal
	if err := validatePathComponents(owner, repo, path, sha); err != nil {
		importCacheLog.Printf("Cache set rejected: invalid path components: %v", err)
		return "", fmt.Errorf("invalid path components: %w", err)
	}

	// Use SHA in path for consistent caching
	// This ensures that different refs pointing to the same commit reuse the same cache
	sanitizedPath := sanitizePath(path)
	relativeCachePath := filepath.Join(ImportCacheDir, owner, repo, sha, sanitizedPath)
	fullCachePath := filepath.Join(c.baseDir, relativeCachePath)

	// Ensure directory exists
	dir := filepath.Dir(fullCachePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		importCacheLog.Printf("Failed to create cache directory: %v", err)
		return "", err
	}

	// Ensure .gitattributes file exists in cache root
	if err := c.ensureGitAttributes(); err != nil {
		importCacheLog.Printf("Failed to ensure .gitattributes: %v", err)
		// Non-fatal error - continue with caching
	}

	// Write content to cache file
	if err := os.WriteFile(fullCachePath, content, 0644); err != nil {
		importCacheLog.Printf("Failed to write cache file: %v", err)
		return "", err
	}

	importCacheLog.Printf("Cached import: %s/%s/%s@%s -> %s", owner, repo, path, sha, fullCachePath)
	return fullCachePath, nil
}

// GetCacheDir returns the base cache directory path
func (c *ImportCache) GetCacheDir() string {
	return filepath.Join(c.baseDir, ImportCacheDir)
}

// ensureGitAttributes creates the .gitattributes file in the cache directory if it doesn't exist
func (c *ImportCache) ensureGitAttributes() error {
	gitAttributesPath := filepath.Join(c.GetCacheDir(), ".gitattributes")

	// Check if .gitattributes already exists
	if _, err := os.Stat(gitAttributesPath); err == nil {
		// File already exists, nothing to do
		return nil
	}

	// Ensure cache root directory exists
	cacheDir := c.GetCacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	// Create .gitattributes file with content
	content := `# Mark all cached import files as generated
* linguist-generated=true

# Use 'ours' merge strategy to keep local cached versions
* merge=ours
`

	if err := os.WriteFile(gitAttributesPath, []byte(content), 0644); err != nil {
		return err
	}

	importCacheLog.Printf("Created .gitattributes in cache directory: %s", gitAttributesPath)
	return nil
}
