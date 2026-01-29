//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewArtifactManager(t *testing.T) {
	am := NewArtifactManager()
	assert.NotNil(t, am)
	assert.NotNil(t, am.uploads)
	assert.NotNil(t, am.downloads)
	assert.Empty(t, am.currentJob)
}

func TestSetCurrentJob(t *testing.T) {
	am := NewArtifactManager()
	am.SetCurrentJob("test-job")
	assert.Equal(t, "test-job", am.GetCurrentJob())
}

func TestRecordUpload(t *testing.T) {
	tests := []struct {
		name      string
		upload    *ArtifactUpload
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid upload",
			upload: &ArtifactUpload{
				Name:    "test-artifact",
				Paths:   []string{"/tmp/test.txt"},
				JobName: "test-job",
			},
			wantError: false,
		},
		{
			name: "upload without name",
			upload: &ArtifactUpload{
				Paths:   []string{"/tmp/test.txt"},
				JobName: "test-job",
			},
			wantError: true,
			errorMsg:  "artifact upload must have a name",
		},
		{
			name: "upload without paths",
			upload: &ArtifactUpload{
				Name:    "test-artifact",
				Paths:   []string{},
				JobName: "test-job",
			},
			wantError: true,
			errorMsg:  "artifact upload must have at least one path",
		},
		{
			name: "upload with multiple paths",
			upload: &ArtifactUpload{
				Name:    "multi-path-artifact",
				Paths:   []string{"/tmp/file1.txt", "/tmp/file2.txt"},
				JobName: "test-job",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			am := NewArtifactManager()
			err := am.RecordUpload(tt.upload)

			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				uploads := am.GetUploadsForJob(tt.upload.JobName)
				assert.Len(t, uploads, 1)
				assert.Equal(t, tt.upload.Name, uploads[0].Name)
			}
		})
	}
}

func TestRecordUploadUsesCurrentJob(t *testing.T) {
	am := NewArtifactManager()
	am.SetCurrentJob("current-job")

	upload := &ArtifactUpload{
		Name:  "test-artifact",
		Paths: []string{"/tmp/test.txt"},
		// JobName not set - should use current job
	}

	err := am.RecordUpload(upload)
	require.NoError(t, err)
	assert.Equal(t, "current-job", upload.JobName)

	uploads := am.GetUploadsForJob("current-job")
	assert.Len(t, uploads, 1)
}

func TestRecordDownload(t *testing.T) {
	tests := []struct {
		name      string
		download  *ArtifactDownload
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid download by name",
			download: &ArtifactDownload{
				Name:    "test-artifact",
				Path:    "/tmp/download",
				JobName: "test-job",
			},
			wantError: false,
		},
		{
			name: "valid download by pattern",
			download: &ArtifactDownload{
				Pattern: "agent-*",
				Path:    "/tmp/download",
				JobName: "test-job",
			},
			wantError: false,
		},
		{
			name: "download without name or pattern",
			download: &ArtifactDownload{
				Path:    "/tmp/download",
				JobName: "test-job",
			},
			wantError: true,
			errorMsg:  "artifact download must have either name or pattern",
		},
		{
			name: "download without path",
			download: &ArtifactDownload{
				Name:    "test-artifact",
				JobName: "test-job",
			},
			wantError: true,
			errorMsg:  "artifact download must have a path",
		},
		{
			name: "download with merge-multiple",
			download: &ArtifactDownload{
				Pattern:       "build-*",
				Path:          "/tmp/builds",
				MergeMultiple: true,
				JobName:       "test-job",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			am := NewArtifactManager()
			err := am.RecordDownload(tt.download)

			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				downloads := am.GetDownloadsForJob(tt.download.JobName)
				assert.Len(t, downloads, 1)
			}
		})
	}
}

func TestRecordDownloadUsesCurrentJob(t *testing.T) {
	am := NewArtifactManager()
	am.SetCurrentJob("current-job")

	download := &ArtifactDownload{
		Name: "test-artifact",
		Path: "/tmp/download",
		// JobName not set - should use current job
	}

	err := am.RecordDownload(download)
	require.NoError(t, err)
	assert.Equal(t, "current-job", download.JobName)

	downloads := am.GetDownloadsForJob("current-job")
	assert.Len(t, downloads, 1)
}

func TestComputeDownloadPath(t *testing.T) {
	tests := []struct {
		name         string
		download     *ArtifactDownload
		upload       *ArtifactUpload
		originalPath string
		expectedPath string
	}{
		{
			name: "download by name - direct path",
			download: &ArtifactDownload{
				Name: "agent-artifacts",
				Path: "/tmp/download",
			},
			upload: &ArtifactUpload{
				Name: "agent-artifacts",
			},
			originalPath: "file.txt",
			expectedPath: "/tmp/download/file.txt",
		},
		{
			name: "download by name - nested file",
			download: &ArtifactDownload{
				Name: "agent-artifacts",
				Path: "/tmp/download",
			},
			upload: &ArtifactUpload{
				Name: "agent-artifacts",
			},
			originalPath: "subdir/file.txt",
			expectedPath: "/tmp/download/subdir/file.txt",
		},
		{
			name: "download by pattern with merge - direct path",
			download: &ArtifactDownload{
				Pattern:       "build-*",
				Path:          "/tmp/builds",
				MergeMultiple: true,
			},
			upload: &ArtifactUpload{
				Name: "build-linux",
			},
			originalPath: "app.exe",
			expectedPath: "/tmp/builds/app.exe",
		},
		{
			name: "download by pattern without merge - artifact subdirectory",
			download: &ArtifactDownload{
				Pattern:       "build-*",
				Path:          "/tmp/builds",
				MergeMultiple: false,
			},
			upload: &ArtifactUpload{
				Name: "build-linux",
			},
			originalPath: "app.exe",
			expectedPath: "/tmp/builds/build-linux/app.exe",
		},
		{
			name: "download by pattern without merge - nested file",
			download: &ArtifactDownload{
				Pattern:       "agent-*",
				Path:          "/tmp/agents",
				MergeMultiple: false,
			},
			upload: &ArtifactUpload{
				Name: "agent-output",
			},
			originalPath: "logs/output.json",
			expectedPath: "/tmp/agents/agent-output/logs/output.json",
		},
		{
			name: "download with leading ./ in original path",
			download: &ArtifactDownload{
				Name: "test-artifact",
				Path: "/tmp/test",
			},
			upload: &ArtifactUpload{
				Name: "test-artifact",
			},
			originalPath: "./data/file.txt",
			expectedPath: "/tmp/test/data/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			am := NewArtifactManager()
			result := am.ComputeDownloadPath(tt.download, tt.upload, tt.originalPath)
			assert.Equal(t, tt.expectedPath, result)
		})
	}
}

func TestFindUploadedArtifact(t *testing.T) {
	am := NewArtifactManager()

	// Setup: create uploads in different jobs
	am.SetCurrentJob("job1")
	err := am.RecordUpload(&ArtifactUpload{
		Name:    "artifact-1",
		Paths:   []string{"/tmp/file1.txt"},
		JobName: "job1",
	})
	require.NoError(t, err)

	am.SetCurrentJob("job2")
	err = am.RecordUpload(&ArtifactUpload{
		Name:    "artifact-2",
		Paths:   []string{"/tmp/file2.txt"},
		JobName: "job2",
	})
	require.NoError(t, err)

	tests := []struct {
		name         string
		artifactName string
		dependsOn    []string
		expectFound  bool
		expectedJob  string
	}{
		{
			name:         "find artifact in dependencies",
			artifactName: "artifact-1",
			dependsOn:    []string{"job1"},
			expectFound:  true,
			expectedJob:  "job1",
		},
		{
			name:         "find artifact with multiple dependencies",
			artifactName: "artifact-2",
			dependsOn:    []string{"job1", "job2"},
			expectFound:  true,
			expectedJob:  "job2",
		},
		{
			name:         "artifact not in dependencies but exists",
			artifactName: "artifact-1",
			dependsOn:    []string{"job2"},
			expectFound:  true,
			expectedJob:  "job1",
		},
		{
			name:         "artifact does not exist",
			artifactName: "nonexistent",
			dependsOn:    []string{"job1", "job2"},
			expectFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := am.FindUploadedArtifact(tt.artifactName, tt.dependsOn)

			if tt.expectFound {
				assert.NotNil(t, result, "Expected to find artifact")
				assert.Equal(t, tt.artifactName, result.Name)
				assert.Equal(t, tt.expectedJob, result.JobName)
			} else {
				assert.Nil(t, result, "Expected not to find artifact")
			}
		})
	}
}

func TestValidateDownload(t *testing.T) {
	am := NewArtifactManager()

	// Setup: create uploads
	am.SetCurrentJob("upload-job")
	err := am.RecordUpload(&ArtifactUpload{
		Name:    "test-artifact",
		Paths:   []string{"/tmp/file.txt"},
		JobName: "upload-job",
	})
	require.NoError(t, err)

	err = am.RecordUpload(&ArtifactUpload{
		Name:    "build-linux",
		Paths:   []string{"/tmp/linux.exe"},
		JobName: "upload-job",
	})
	require.NoError(t, err)

	err = am.RecordUpload(&ArtifactUpload{
		Name:    "build-windows",
		Paths:   []string{"/tmp/windows.exe"},
		JobName: "upload-job",
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		download  *ArtifactDownload
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid download by name",
			download: &ArtifactDownload{
				Name:      "test-artifact",
				Path:      "/tmp/download",
				JobName:   "download-job",
				DependsOn: []string{"upload-job"},
			},
			wantError: false,
		},
		{
			name: "invalid download - artifact not found",
			download: &ArtifactDownload{
				Name:      "nonexistent-artifact",
				Path:      "/tmp/download",
				JobName:   "download-job",
				DependsOn: []string{"upload-job"},
			},
			wantError: true,
			errorMsg:  "not found in any dependent job",
		},
		{
			name: "valid download by pattern",
			download: &ArtifactDownload{
				Pattern:   "build-*",
				Path:      "/tmp/builds",
				JobName:   "download-job",
				DependsOn: []string{"upload-job"},
			},
			wantError: false,
		},
		{
			name: "invalid download - pattern matches nothing",
			download: &ArtifactDownload{
				Pattern:   "logs-*",
				Path:      "/tmp/tests",
				JobName:   "download-job",
				DependsOn: []string{"upload-job"},
			},
			wantError: true,
			errorMsg:  "no artifacts matching pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := am.ValidateDownload(tt.download)

			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateAllDownloads(t *testing.T) {
	am := NewArtifactManager()

	// Setup: create uploads
	am.SetCurrentJob("upload-job")
	err := am.RecordUpload(&ArtifactUpload{
		Name:    "artifact-1",
		Paths:   []string{"/tmp/file1.txt"},
		JobName: "upload-job",
	})
	require.NoError(t, err)

	// Setup: create downloads (some valid, some invalid)
	am.SetCurrentJob("download-job")
	err = am.RecordDownload(&ArtifactDownload{
		Name:      "artifact-1",
		Path:      "/tmp/download1",
		JobName:   "download-job",
		DependsOn: []string{"upload-job"},
	})
	require.NoError(t, err)

	err = am.RecordDownload(&ArtifactDownload{
		Name:      "nonexistent",
		Path:      "/tmp/download2",
		JobName:   "download-job",
		DependsOn: []string{"upload-job"},
	})
	require.NoError(t, err)

	// Validate all downloads
	errors := am.ValidateAllDownloads()

	// Should have 1 error (nonexistent artifact)
	assert.Len(t, errors, 1)
	assert.Contains(t, errors[0].Error(), "nonexistent")
	assert.Contains(t, errors[0].Error(), "not found")
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		matches []string
		noMatch []string
	}{
		{
			name:    "exact match",
			pattern: "artifact",
			matches: []string{"artifact"},
			noMatch: []string{"artifact-1", "test-artifact", "other"},
		},
		{
			name:    "leading wildcard",
			pattern: "*-artifact",
			matches: []string{"test-artifact", "my-artifact"},
			noMatch: []string{"artifact", "artifact-test"},
		},
		{
			name:    "trailing wildcard",
			pattern: "build-*",
			matches: []string{"build-linux", "build-windows", "build-"},
			noMatch: []string{"build", "test-build-linux"},
		},
		{
			name:    "middle wildcard",
			pattern: "build-*-x64",
			matches: []string{"build-linux-x64", "build-windows-x64"},
			noMatch: []string{"build-x64", "build-linux-arm64"},
		},
		{
			name:    "wildcard matches all",
			pattern: "*",
			matches: []string{"anything", "test", "build-linux"},
			noMatch: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range tt.matches {
				assert.True(t, matchesPattern(name, tt.pattern),
					"Expected %s to match pattern %s", name, tt.pattern)
			}
			for _, name := range tt.noMatch {
				assert.False(t, matchesPattern(name, tt.pattern),
					"Expected %s NOT to match pattern %s", name, tt.pattern)
			}
		})
	}
}

func TestReset(t *testing.T) {
	am := NewArtifactManager()

	// Add some data
	am.SetCurrentJob("test-job")
	err := am.RecordUpload(&ArtifactUpload{
		Name:    "test-artifact",
		Paths:   []string{"/tmp/file.txt"},
		JobName: "test-job",
	})
	require.NoError(t, err)

	err = am.RecordDownload(&ArtifactDownload{
		Name:    "test-artifact",
		Path:    "/tmp/download",
		JobName: "test-job",
	})
	require.NoError(t, err)

	// Verify data exists
	assert.Len(t, am.uploads, 1)
	assert.Len(t, am.downloads, 1)
	assert.Equal(t, "test-job", am.currentJob)

	// Reset
	am.Reset()

	// Verify everything is cleared
	assert.Empty(t, am.uploads)
	assert.Empty(t, am.downloads)
	assert.Empty(t, am.currentJob)
}

func TestComplexWorkflowScenario(t *testing.T) {
	am := NewArtifactManager()

	// Job 1: Upload agent artifacts
	am.SetCurrentJob("agent")
	err := am.RecordUpload(&ArtifactUpload{
		Name:    "agent-artifacts",
		Paths:   []string{"/tmp/gh-aw/aw-prompts/prompt.txt", "/tmp/gh-aw/patch/aw.patch"},
		JobName: "agent",
	})
	require.NoError(t, err)

	// Job 2: Download agent artifacts for safe outputs
	am.SetCurrentJob("safe_outputs")
	err = am.RecordDownload(&ArtifactDownload{
		Name:      "agent-artifacts",
		Path:      "/tmp/gh-aw/",
		JobName:   "safe_outputs",
		DependsOn: []string{"agent"},
	})
	require.NoError(t, err)

	// Validate downloads
	errors := am.ValidateAllDownloads()
	assert.Empty(t, errors, "Expected no validation errors")

	// Test path computation
	download := am.GetDownloadsForJob("safe_outputs")[0]
	upload := am.FindUploadedArtifact("agent-artifacts", []string{"agent"})
	require.NotNil(t, upload)

	// Files should be extracted directly to download path (v4 behavior)
	promptPath := am.ComputeDownloadPath(download, upload, "aw-prompts/prompt.txt")
	assert.Equal(t, "/tmp/gh-aw/aw-prompts/prompt.txt", promptPath)

	patchPath := am.ComputeDownloadPath(download, upload, "patch/aw.patch")
	assert.Equal(t, "/tmp/gh-aw/patch/aw.patch", patchPath)
}

func TestMultipleArtifactsPatternDownload(t *testing.T) {
	am := NewArtifactManager()

	// Job 1: Upload multiple build artifacts
	am.SetCurrentJob("build")
	for _, platform := range []string{"linux", "windows", "macos"} {
		err := am.RecordUpload(&ArtifactUpload{
			Name:    "build-" + platform,
			Paths:   []string{"/build/" + platform + "/app"},
			JobName: "build",
		})
		require.NoError(t, err)
	}

	// Job 2: Download all build artifacts with pattern (no merge)
	am.SetCurrentJob("test")
	err := am.RecordDownload(&ArtifactDownload{
		Pattern:       "build-*",
		Path:          "/tmp/artifacts",
		MergeMultiple: false,
		JobName:       "test",
		DependsOn:     []string{"build"},
	})
	require.NoError(t, err)

	// Validate
	errors := am.ValidateAllDownloads()
	assert.Empty(t, errors)

	// Test path computation for each artifact
	download := am.GetDownloadsForJob("test")[0]

	linuxUpload := am.FindUploadedArtifact("build-linux", []string{"build"})
	require.NotNil(t, linuxUpload)
	linuxPath := am.ComputeDownloadPath(download, linuxUpload, "linux/app")
	assert.Equal(t, "/tmp/artifacts/build-linux/linux/app", linuxPath)

	windowsUpload := am.FindUploadedArtifact("build-windows", []string{"build"})
	require.NotNil(t, windowsUpload)
	windowsPath := am.ComputeDownloadPath(download, windowsUpload, "windows/app")
	assert.Equal(t, "/tmp/artifacts/build-windows/windows/app", windowsPath)
}

func TestPatternDownloadWithMerge(t *testing.T) {
	am := NewArtifactManager()

	// Upload multiple artifacts
	am.SetCurrentJob("job1")
	err := am.RecordUpload(&ArtifactUpload{
		Name:    "logs-part1",
		Paths:   []string{"/logs/part1.txt"},
		JobName: "job1",
	})
	require.NoError(t, err)

	err = am.RecordUpload(&ArtifactUpload{
		Name:    "logs-part2",
		Paths:   []string{"/logs/part2.txt"},
		JobName: "job1",
	})
	require.NoError(t, err)

	// Download with merge
	am.SetCurrentJob("job2")
	err = am.RecordDownload(&ArtifactDownload{
		Pattern:       "logs-*",
		Path:          "/tmp/all-logs",
		MergeMultiple: true,
		JobName:       "job2",
		DependsOn:     []string{"job1"},
	})
	require.NoError(t, err)

	// Validate
	errors := am.ValidateAllDownloads()
	assert.Empty(t, errors)

	// With merge, files go directly to path (no artifact subdirectories)
	download := am.GetDownloadsForJob("job2")[0]

	part1Upload := am.FindUploadedArtifact("logs-part1", []string{"job1"})
	require.NotNil(t, part1Upload)
	part1Path := am.ComputeDownloadPath(download, part1Upload, "part1.txt")
	assert.Equal(t, "/tmp/all-logs/part1.txt", part1Path)

	part2Upload := am.FindUploadedArtifact("logs-part2", []string{"job1"})
	require.NotNil(t, part2Upload)
	part2Path := am.ComputeDownloadPath(download, part2Upload, "part2.txt")
	assert.Equal(t, "/tmp/all-logs/part2.txt", part2Path)
}

// TestCommonParentStripping tests that common parent directories are stripped
// when multiple files are uploaded, simulating GitHub Actions behavior
func TestCommonParentStripping(t *testing.T) {
	am := NewArtifactManager()
	am.SetCurrentJob("upload-job")

	// Upload files with common parent /tmp/gh-aw/
	err := am.RecordUpload(&ArtifactUpload{
		Name: "test-artifact",
		Paths: []string{
			"/tmp/gh-aw/aw-prompts/prompt.txt",
			"/tmp/gh-aw/aw.patch",
		},
		JobName: "upload-job",
	})
	require.NoError(t, err)

	uploads := am.GetUploadsForJob("upload-job")
	require.Len(t, uploads, 1)
	upload := uploads[0]

	// Verify normalized paths have common parent stripped
	assert.NotNil(t, upload.NormalizedPaths)
	assert.Equal(t, "aw-prompts/prompt.txt", upload.NormalizedPaths["/tmp/gh-aw/aw-prompts/prompt.txt"])
	assert.Equal(t, "aw.patch", upload.NormalizedPaths["/tmp/gh-aw/aw.patch"])

	// Verify download paths use normalized paths
	am.SetCurrentJob("download-job")
	download := &ArtifactDownload{
		Name:      "test-artifact",
		Path:      "/workspace",
		JobName:   "download-job",
		DependsOn: []string{"upload-job"},
	}

	// Download should use the normalized paths (with common parent stripped)
	promptPath := am.ComputeDownloadPath(download, upload, "/tmp/gh-aw/aw-prompts/prompt.txt")
	assert.Equal(t, "/workspace/aw-prompts/prompt.txt", promptPath)

	patchPath := am.ComputeDownloadPath(download, upload, "/tmp/gh-aw/aw.patch")
	assert.Equal(t, "/workspace/aw.patch", patchPath)
}

// TestCommonParentStrippingNestedPaths tests common parent stripping with nested paths
func TestCommonParentStrippingNestedPaths(t *testing.T) {
	am := NewArtifactManager()
	am.SetCurrentJob("build")

	// Upload files with deeper nesting
	err := am.RecordUpload(&ArtifactUpload{
		Name: "build-outputs",
		Paths: []string{
			"/home/runner/work/project/dist/app.js",
			"/home/runner/work/project/dist/styles.css",
			"/home/runner/work/project/dist/assets/logo.png",
		},
		JobName: "build",
	})
	require.NoError(t, err)

	upload := am.GetUploadsForJob("build")[0]

	// Common parent should be /home/runner/work/project/dist
	assert.NotNil(t, upload.NormalizedPaths)
	assert.Equal(t, "app.js", upload.NormalizedPaths["/home/runner/work/project/dist/app.js"])
	assert.Equal(t, "styles.css", upload.NormalizedPaths["/home/runner/work/project/dist/styles.css"])
	assert.Equal(t, "assets/logo.png", upload.NormalizedPaths["/home/runner/work/project/dist/assets/logo.png"])
}

// TestCommonParentStrippingSingleFile tests that single file uploads work correctly
func TestCommonParentStrippingSingleFile(t *testing.T) {
	am := NewArtifactManager()
	am.SetCurrentJob("job1")

	// Upload single file
	err := am.RecordUpload(&ArtifactUpload{
		Name:    "single-file",
		Paths:   []string{"/tmp/gh-aw/report.pdf"},
		JobName: "job1",
	})
	require.NoError(t, err)

	upload := am.GetUploadsForJob("job1")[0]

	// Single file should be normalized to just its base name
	assert.NotNil(t, upload.NormalizedPaths)
	assert.Equal(t, "report.pdf", upload.NormalizedPaths["/tmp/gh-aw/report.pdf"])

	// Download should use the normalized path
	download := &ArtifactDownload{
		Name:      "single-file",
		Path:      "/downloads",
		JobName:   "job2",
		DependsOn: []string{"job1"},
	}

	path := am.ComputeDownloadPath(download, upload, "/tmp/gh-aw/report.pdf")
	assert.Equal(t, "/downloads/report.pdf", path)
}

// TestCommonParentStrippingNoCommonParent tests files with no common parent
func TestCommonParentStrippingNoCommonParent(t *testing.T) {
	am := NewArtifactManager()
	am.SetCurrentJob("job1")

	// Upload files from completely different paths
	err := am.RecordUpload(&ArtifactUpload{
		Name: "mixed-files",
		Paths: []string{
			"/tmp/file1.txt",
			"/var/file2.txt",
		},
		JobName: "job1",
	})
	require.NoError(t, err)

	upload := am.GetUploadsForJob("job1")[0]

	// No common parent (beyond root), should use base names
	assert.NotNil(t, upload.NormalizedPaths)
	assert.Equal(t, "file1.txt", upload.NormalizedPaths["/tmp/file1.txt"])
	assert.Equal(t, "file2.txt", upload.NormalizedPaths["/var/file2.txt"])
}

// TestCommonParentWithPatternDownload tests common parent stripping with pattern downloads
func TestCommonParentWithPatternDownload(t *testing.T) {
	am := NewArtifactManager()

	// Job 1: Upload with common parent
	am.SetCurrentJob("build")
	err := am.RecordUpload(&ArtifactUpload{
		Name: "build-linux",
		Paths: []string{
			"/build/output/linux/app",
			"/build/output/linux/lib.so",
		},
		JobName: "build",
	})
	require.NoError(t, err)

	// Job 2: Download with pattern
	am.SetCurrentJob("deploy")
	download := &ArtifactDownload{
		Pattern:       "build-*",
		Path:          "/deploy",
		MergeMultiple: false,
		JobName:       "deploy",
		DependsOn:     []string{"build"},
	}

	upload := am.GetUploadsForJob("build")[0]

	// With pattern download (no merge), files go to path/artifact-name/normalized-path
	appPath := am.ComputeDownloadPath(download, upload, "/build/output/linux/app")
	assert.Equal(t, "/deploy/build-linux/app", appPath)

	libPath := am.ComputeDownloadPath(download, upload, "/build/output/linux/lib.so")
	assert.Equal(t, "/deploy/build-linux/lib.so", libPath)
}
