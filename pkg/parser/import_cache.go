package parser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var importCacheLog = logger.New("parser:import_cache")

const (
	// ImportCacheDir is the directory where cached imports are stored
	ImportCacheDir   = ".github/aw/imports"
	maxCacheFileSize = 10 * 1024 * 1024
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
			return errors.New("empty component in path")
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
	for _, comp := range []string{owner, repo, sha, sanitizePath(path)} {
		if comp == "." || !filepath.IsLocal(comp) || strings.ContainsAny(comp, `/\`) {
			return fmt.Errorf("component is not a single cache filename: %q", comp)
		}
	}
	importCacheLog.Print("Path validation successful")
	return nil
}

// ImportCache manages cached imported workflow files
type ImportCache struct {
	baseDir  string // Base directory for cache (typically repo root)
	readOnly bool
}

// SetReadOnly prevents downloads from changing the on-disk import cache.
func (c *ImportCache) SetReadOnly(readOnly bool) {
	c.readOnly = readOnly
}

// NewImportCache creates a new import cache instance
func NewImportCache(repoRoot string) *ImportCache {
	importCacheLog.Printf("Creating import cache with base dir: %s", repoRoot)
	return &ImportCache{
		baseDir: repoRoot,
	}
}

// Get retrieves a regular cached file's path. Read it with ReadFile, not os.ReadFile,
// since a returned path is not a filesystem capability and may change after lookup.
// sha parameter should be the resolved commit SHA
func (c *ImportCache) Get(owner, repo, path, sha string) (string, bool) {
	if err := validatePathComponents(owner, repo, path, sha); err != nil {
		importCacheLog.Printf("Cache lookup rejected: %v", err)
		return "", false
	}
	relativeCachePath := filepath.Join(ImportCacheDir, owner, repo, sha, sanitizePath(path))
	fullCachePath := filepath.Join(c.baseDir, relativeCachePath)
	dir, err := c.openCacheDirectory(filepath.Dir(relativeCachePath), false)
	if err != nil {
		importCacheLog.Printf("Cache directory access error: %v", err)
		return "", false
	}
	defer closeImportCacheHandle(dir)
	if _, err := regularCacheFile(dir, filepath.Base(relativeCachePath)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
	if c.readOnly {
		return "", errors.New("cannot write to read-only import cache")
	}

	importCacheLog.Printf("Setting cache entry: %s/%s/%s@%s, size=%d bytes", owner, repo, path, sha, len(content))

	// Validate file size (max 10MB)
	if len(content) > maxCacheFileSize {
		importCacheLog.Printf("Cache set rejected: file size %d exceeds max %d bytes", len(content), maxCacheFileSize)
		return "", fmt.Errorf("file size (%d bytes) exceeds maximum allowed size (%d bytes)", len(content), maxCacheFileSize)
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

	dir, err := c.openCacheDirectory(filepath.Dir(relativeCachePath), true)
	if err != nil {
		importCacheLog.Printf("Failed to create cache directory: %v", err)
		return "", err
	}
	defer closeImportCacheHandle(dir)

	// Ensure .gitattributes file exists in cache root
	if err := c.ensureGitAttributes(); err != nil {
		importCacheLog.Printf("Failed to ensure .gitattributes: %v", err)
		// Non-fatal error - continue with caching
	}

	// Write content to cache file
	if err := writeCacheFileAtomic(dir, filepath.Base(relativeCachePath), content, constants.FilePermSensitive, true); err != nil {
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
	dir, err := c.openCacheDirectory(ImportCacheDir, true)
	if err != nil {
		return err
	}
	defer closeImportCacheHandle(dir)
	if _, err := regularCacheFile(dir, ".gitattributes"); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Create .gitattributes file with content
	content := `# Mark all cached import files as generated
* linguist-generated=true

# Use 'ours' merge strategy to keep local cached versions
* merge=ours
`

	if err := writeCacheFileAtomic(dir, ".gitattributes", []byte(content), constants.FilePermPublic, false); err != nil {
		return err
	}

	importCacheLog.Printf("Created .gitattributes in cache directory: %s", c.GetCacheDir())
	return nil
}
