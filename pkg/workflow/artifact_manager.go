package workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var artifactManagerLog = logger.New("workflow:artifact_manager")

// ArtifactManager simulates the behavior of actions/upload-artifact and actions/download-artifact
// to track artifacts and compute actual file locations during compilation.
//
// This manager implements the v4 behavior of GitHub Actions artifacts:
// - Upload: artifacts are immutable, each upload creates a new artifact
// - Download: files extracted directly to path (not path/artifact-name/)
// - Pattern downloads: separate subdirectories unless merge-multiple is used
type ArtifactManager struct {
	// uploads tracks all artifact uploads by job name
	uploads map[string][]*ArtifactUpload

	// downloads tracks all artifact downloads by job name
	downloads map[string][]*ArtifactDownload

	// currentJob tracks the job currently being processed
	currentJob string
}

// ArtifactUpload represents an artifact upload operation
type ArtifactUpload struct {
	// Name is the artifact name (e.g., "agent-artifacts")
	Name string

	// Paths are the file/directory paths being uploaded
	// These can be absolute paths or glob patterns
	Paths []string

	// NormalizedPaths are the paths after common parent directory removal
	// This simulates GitHub Actions behavior where the common parent is stripped
	NormalizedPaths map[string]string

	// IfNoFilesFound specifies behavior when no files match
	// Values: "warn", "error", "ignore"
	IfNoFilesFound string

	// IncludeHiddenFiles determines if hidden files are included
	IncludeHiddenFiles bool

	// JobName is the name of the job uploading this artifact
	JobName string
}

// ArtifactDownload represents an artifact download operation
type ArtifactDownload struct {
	// Name is the artifact name to download (optional if using Pattern)
	Name string

	// Pattern is a glob pattern to match multiple artifacts (v4 feature)
	Pattern string

	// Path is where the artifact will be downloaded
	Path string

	// MergeMultiple determines if multiple artifacts should be merged
	// into the same directory (only applies when using Pattern)
	MergeMultiple bool

	// JobName is the name of the job downloading this artifact
	JobName string

	// DependsOn lists job names this job depends on (from needs:)
	DependsOn []string
}

// ArtifactFile represents a file within an artifact
type ArtifactFile struct {
	// ArtifactName is the name of the artifact containing this file
	ArtifactName string

	// OriginalPath is the path as uploaded
	OriginalPath string

	// DownloadPath is the computed path after download
	DownloadPath string

	// JobName is the job that uploaded this file
	JobName string
}

// NewArtifactManager creates a new artifact manager
func NewArtifactManager() *ArtifactManager {
	return &ArtifactManager{
		uploads:   make(map[string][]*ArtifactUpload),
		downloads: make(map[string][]*ArtifactDownload),
	}
}

// SetCurrentJob sets the job currently being processed
func (am *ArtifactManager) SetCurrentJob(jobName string) {
	artifactManagerLog.Printf("Setting current job: %s", jobName)
	am.currentJob = jobName
}

// GetCurrentJob returns the current job name
func (am *ArtifactManager) GetCurrentJob() string {
	return am.currentJob
}

// RecordUpload records an artifact upload operation
func (am *ArtifactManager) RecordUpload(upload *ArtifactUpload) error {
	if upload.Name == "" {
		return fmt.Errorf("artifact upload must have a name")
	}
	if len(upload.Paths) == 0 {
		return fmt.Errorf("artifact upload must have at least one path")
	}

	// Set the job name if not already set
	if upload.JobName == "" {
		upload.JobName = am.currentJob
	}

	// Compute normalized paths with common parent removed
	upload.NormalizedPaths = computeNormalizedPaths(upload.Paths)

	artifactManagerLog.Printf("Recording upload: artifact=%s, job=%s, paths=%v, normalized=%v",
		upload.Name, upload.JobName, upload.Paths, upload.NormalizedPaths)

	am.uploads[upload.JobName] = append(am.uploads[upload.JobName], upload)
	return nil
}

// RecordDownload records an artifact download operation
func (am *ArtifactManager) RecordDownload(download *ArtifactDownload) error {
	if download.Name == "" && download.Pattern == "" {
		return fmt.Errorf("artifact download must have either name or pattern")
	}
	if download.Path == "" {
		return fmt.Errorf("artifact download must have a path")
	}

	// Set the job name if not already set
	if download.JobName == "" {
		download.JobName = am.currentJob
	}

	artifactManagerLog.Printf("Recording download: name=%s, pattern=%s, job=%s, path=%s",
		download.Name, download.Pattern, download.JobName, download.Path)

	am.downloads[download.JobName] = append(am.downloads[download.JobName], download)
	return nil
}

// computeNormalizedPaths computes normalized paths with common parent directory removed.
// This simulates GitHub Actions behavior where files uploaded with paths like:
//
//	/tmp/gh-aw/aw-prompts/prompt.txt
//	/tmp/gh-aw/aw.patch
//
// are stored in the artifact as:
//
//	aw-prompts/prompt.txt
//	aw.patch
//
// (with common parent /tmp/gh-aw/ removed)
func computeNormalizedPaths(paths []string) map[string]string {
	if len(paths) == 0 {
		return nil
	}

	// If only one path, normalize it relative to its parent
	if len(paths) == 1 {
		path := filepath.Clean(paths[0])
		// Get the base name (file/dir name without parent)
		base := filepath.Base(path)
		result := make(map[string]string)
		result[path] = base
		artifactManagerLog.Printf("Single path normalization: %s -> %s", path, base)
		return result
	}

	// Find common parent directory for multiple paths
	commonParent := findCommonParent(paths)
	artifactManagerLog.Printf("Common parent for %d paths: %s", len(paths), commonParent)

	// Create mapping of original path to normalized path
	normalized := make(map[string]string)
	for _, path := range paths {
		cleanPath := filepath.Clean(path)
		var relativePath string

		if commonParent != "" && commonParent != "." {
			// Remove common parent
			rel, err := filepath.Rel(commonParent, cleanPath)
			if err != nil {
				// If we can't compute relative path, use the base name
				relativePath = filepath.Base(cleanPath)
			} else {
				relativePath = rel
			}
		} else {
			// No common parent, use base name
			relativePath = filepath.Base(cleanPath)
		}

		normalized[cleanPath] = relativePath
		artifactManagerLog.Printf("Path normalization: %s -> %s (parent: %s)", cleanPath, relativePath, commonParent)
	}

	return normalized
}

// findCommonParent finds the common parent directory of multiple paths
func findCommonParent(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return filepath.Dir(filepath.Clean(paths[0]))
	}

	// Clean all paths and split into components
	splitPaths := make([][]string, len(paths))
	for i, p := range paths {
		cleanPath := filepath.Clean(p)
		// Split the full path (not just directory)
		// Handle absolute paths starting with /
		if strings.HasPrefix(cleanPath, string(filepath.Separator)) {
			cleanPath = cleanPath[1:] // Remove leading separator for splitting
		}
		splitPaths[i] = strings.Split(cleanPath, string(filepath.Separator))
	}

	// Find the minimum length among all paths
	minLen := len(splitPaths[0])
	for _, sp := range splitPaths[1:] {
		if len(sp) < minLen {
			minLen = len(sp)
		}
	}

	// Find common prefix by comparing each component
	var commonParts []string
	for i := 0; i < minLen-1; i++ { // minLen-1 to exclude the filename
		part := splitPaths[0][i]
		allMatch := true
		for _, sp := range splitPaths[1:] {
			if sp[i] != part {
				allMatch = false
				break
			}
		}
		if allMatch {
			commonParts = append(commonParts, part)
		} else {
			break
		}
	}

	if len(commonParts) == 0 {
		return ""
	}

	// Reconstruct the path with leading separator if original paths were absolute
	result := filepath.Join(commonParts...)
	if strings.HasPrefix(paths[0], string(filepath.Separator)) {
		result = string(filepath.Separator) + result
	}

	return result
}

// ComputeDownloadPath computes the actual file path after download
// based on GitHub Actions v4 behavior.
//
// Rules:
// - Download by name: files extracted directly to path/ (e.g., path/file.txt)
// - Download by pattern without merge: files in path/artifact-name/ (e.g., path/artifact-1/file.txt)
// - Download by pattern with merge: files extracted directly to path/ (e.g., path/file.txt)
// - Common parent directories are stripped during upload (simulated via NormalizedPaths)
func (am *ArtifactManager) ComputeDownloadPath(download *ArtifactDownload, upload *ArtifactUpload, originalPath string) string {
	// Get the normalized path (with common parent removed) from the upload
	// This simulates how GitHub Actions strips common parent directories
	cleanOriginal := filepath.Clean(originalPath)
	normalizedPath := cleanOriginal

	// If upload has normalized paths, use them
	if upload.NormalizedPaths != nil {
		if normalized, ok := upload.NormalizedPaths[cleanOriginal]; ok {
			normalizedPath = normalized
			artifactManagerLog.Printf("Using normalized path from upload: %s -> %s", cleanOriginal, normalizedPath)
		}
	} else {
		// Fallback: remove leading ./
		normalizedPath = strings.TrimPrefix(originalPath, "./")
	}

	// If downloading by name (not pattern), files go directly to download path
	if download.Name != "" && download.Pattern == "" {
		result := filepath.Join(download.Path, normalizedPath)
		artifactManagerLog.Printf("Download by name: %s -> %s", originalPath, result)
		return result
	}

	// If downloading by pattern with merge-multiple, files go directly to download path
	if download.Pattern != "" && download.MergeMultiple {
		result := filepath.Join(download.Path, normalizedPath)
		artifactManagerLog.Printf("Download by pattern (merge): %s -> %s", originalPath, result)
		return result
	}

	// If downloading by pattern without merge, files go to path/artifact-name/
	if download.Pattern != "" && !download.MergeMultiple {
		result := filepath.Join(download.Path, upload.Name, normalizedPath)
		artifactManagerLog.Printf("Download by pattern (no merge): %s -> %s", originalPath, result)
		return result
	}

	// Default: direct to download path
	result := filepath.Join(download.Path, normalizedPath)
	artifactManagerLog.Printf("Download default: %s -> %s", originalPath, result)
	return result
}

// FindUploadedArtifact finds an uploaded artifact by name from jobs this job depends on
func (am *ArtifactManager) FindUploadedArtifact(artifactName string, dependsOn []string) *ArtifactUpload {
	// Search in all dependent jobs
	for _, jobName := range dependsOn {
		uploads := am.uploads[jobName]
		for _, upload := range uploads {
			if upload.Name == artifactName {
				artifactManagerLog.Printf("Found artifact %s uploaded by job %s", artifactName, jobName)
				return upload
			}
		}
	}

	// If not found in dependencies, search all jobs (for backwards compatibility)
	// This handles cases where dependencies aren't explicitly tracked
	for jobName, uploads := range am.uploads {
		for _, upload := range uploads {
			if upload.Name == artifactName {
				artifactManagerLog.Printf("Found artifact %s uploaded by job %s (global search)", artifactName, jobName)
				return upload
			}
		}
	}

	artifactManagerLog.Printf("Artifact %s not found in any job", artifactName)
	return nil
}

// ValidateDownload validates that a download operation can find its artifact
func (am *ArtifactManager) ValidateDownload(download *ArtifactDownload) error {
	if download.Name != "" {
		// Download by name - must find exact artifact
		upload := am.FindUploadedArtifact(download.Name, download.DependsOn)
		if upload == nil {
			return fmt.Errorf("artifact '%s' downloaded by job '%s' not found in any dependent job",
				download.Name, download.JobName)
		}
		artifactManagerLog.Printf("Validated download: artifact=%s exists in job=%s",
			download.Name, upload.JobName)
	}

	if download.Pattern != "" {
		// Download by pattern - must find at least one matching artifact
		found := false
		for _, jobName := range download.DependsOn {
			uploads := am.uploads[jobName]
			for _, upload := range uploads {
				// Simple pattern matching for now (could be enhanced with glob)
				if matchesPattern(upload.Name, download.Pattern) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			// Try global search
			for _, uploads := range am.uploads {
				for _, upload := range uploads {
					if matchesPattern(upload.Name, download.Pattern) {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("no artifacts matching pattern '%s' found for job '%s'",
				download.Pattern, download.JobName)
		}
		artifactManagerLog.Printf("Validated download: pattern=%s matches at least one artifact",
			download.Pattern)
	}

	return nil
}

// matchesPattern performs simple wildcard pattern matching
// Supports * as wildcard (e.g., "agent-*" matches "agent-artifacts")
func matchesPattern(name, pattern string) bool {
	// If pattern has no wildcard, do exact match
	if !strings.Contains(pattern, "*") {
		return name == pattern
	}

	// Handle leading wildcard: "*suffix"
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(name, suffix)
	}

	// Handle trailing wildcard: "prefix*"
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(name, prefix)
	}

	// Handle middle wildcard: "prefix*suffix"
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		prefix, suffix := parts[0], parts[1]
		// Check that name starts with prefix, ends with suffix, and has something in between
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			// Ensure there's content between prefix and suffix
			// The middle part should be at least as long as the non-overlapping parts
			minLength := len(prefix) + len(suffix)
			return len(name) >= minLength
		}
		return false
	}

	// For more complex patterns, just do exact match
	return name == pattern
}

// GetUploadsForJob returns all uploads for a specific job
func (am *ArtifactManager) GetUploadsForJob(jobName string) []*ArtifactUpload {
	return am.uploads[jobName]
}

// GetDownloadsForJob returns all downloads for a specific job
func (am *ArtifactManager) GetDownloadsForJob(jobName string) []*ArtifactDownload {
	return am.downloads[jobName]
}

// ValidateAllDownloads validates all download operations
func (am *ArtifactManager) ValidateAllDownloads() []error {
	var errors []error

	for jobName, downloads := range am.downloads {
		for _, download := range downloads {
			if err := am.ValidateDownload(download); err != nil {
				errors = append(errors, fmt.Errorf("job %s: %w", jobName, err))
			}
		}
	}

	if len(errors) > 0 {
		artifactManagerLog.Printf("Validation found %d error(s)", len(errors))
	} else {
		artifactManagerLog.Print("All downloads validated successfully")
	}

	return errors
}

// GetAllArtifacts returns all uploaded artifacts
func (am *ArtifactManager) GetAllArtifacts() map[string][]*ArtifactUpload {
	return am.uploads
}

// Reset clears all tracked uploads and downloads
func (am *ArtifactManager) Reset() {
	am.uploads = make(map[string][]*ArtifactUpload)
	am.downloads = make(map[string][]*ArtifactDownload)
	am.currentJob = ""
	artifactManagerLog.Print("Reset artifact manager")
}
