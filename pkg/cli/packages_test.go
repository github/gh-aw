//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestCollectPackageIncludesRecursive tests the recursive include dependency collection
func TestCollectPackageIncludesRecursive(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		setupFiles    map[string]string // file path -> content
		baseDir       string
		expectedCount int
		expectedPaths []string
		expectedError bool
	}{
		{
			name: "single include",
			content: `---
on: push
---
@include shared/common.md

# My Workflow`,
			setupFiles: map[string]string{
				"shared/common.md": `# Common config`,
			},
			expectedCount: 1,
			expectedPaths: []string{"shared/common.md"},
		},
		{
			name: "optional include with missing file",
			content: `@include? shared/optional.md
# My Workflow`,
			setupFiles:    map[string]string{}, // no files
			expectedCount: 1,                   // dependency is recorded even if file missing
			expectedPaths: []string{"shared/optional.md"},
		},
		{
			name: "include with section reference",
			content: `@include shared/config.md#section
# My Workflow`,
			setupFiles: map[string]string{
				"shared/config.md": `# Config`,
			},
			expectedCount: 1,
			expectedPaths: []string{"shared/config.md"}, // section reference stripped
		},
		{
			name: "nested includes",
			content: `@include level1.md
# Workflow`,
			setupFiles: map[string]string{
				"level1.md": `@include level2.md
# Level 1`,
				"level2.md": `# Level 2`,
			},
			expectedCount: 2,
			expectedPaths: []string{"level1.md", "level2.md"},
		},
		{
			name: "duplicate includes skipped",
			content: `@include shared/common.md
@include shared/common.md
# Workflow`,
			setupFiles: map[string]string{
				"shared/common.md": `# Common`,
			},
			expectedCount: 1, // duplicate skipped
			expectedPaths: []string{"shared/common.md"},
		},
		{
			name:          "no includes",
			content:       `# Simple workflow with no includes`,
			setupFiles:    map[string]string{},
			expectedCount: 0,
			expectedPaths: []string{},
		},
		{
			name: "whitespace variations",
			content: `@include  	  shared/file1.md
@include shared/file2.md  
# Workflow`,
			setupFiles: map[string]string{
				"shared/file1.md": `# File 1`,
				"shared/file2.md": `# File 2`,
			},
			expectedCount: 2,
			expectedPaths: []string{"shared/file1.md", "shared/file2.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for test
			tmpDir := testutil.TempDir(t, "test-*")

			// Setup test files
			for path, content := range tt.setupFiles {
				fullPath := filepath.Join(tmpDir, path)
				dir := filepath.Dir(fullPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("Failed to create directory %s: %v", dir, err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to write file %s: %v", fullPath, err)
				}
			}

			// Collect includes
			var dependencies []IncludeDependency
			seen := make(map[string]bool)
			err := collectPackageIncludesRecursive(tt.content, tmpDir, &dependencies, seen, false)

			// Check error expectation
			if tt.expectedError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check count
			if len(dependencies) != tt.expectedCount {
				t.Errorf("Expected %d dependencies, got %d", tt.expectedCount, len(dependencies))
			}

			// Check paths
			foundPaths := make(map[string]bool)
			for _, dep := range dependencies {
				foundPaths[dep.TargetPath] = true
			}
			for _, expectedPath := range tt.expectedPaths {
				if !foundPaths[expectedPath] {
					t.Errorf("Expected to find path %s in dependencies", expectedPath)
				}
			}
		})
	}
}

// TestCollectPackageIncludesRecursive_CircularReference tests handling of circular includes
func TestCollectPackageIncludesRecursive_CircularReference(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Create circular reference: a.md includes b.md, b.md includes a.md
	aContent := `@include b.md
# File A`
	bContent := `@include a.md
# File B`

	if err := os.WriteFile(filepath.Join(tmpDir, "a.md"), []byte(aContent), 0644); err != nil {
		t.Fatalf("Failed to write a.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.md"), []byte(bContent), 0644); err != nil {
		t.Fatalf("Failed to write b.md: %v", err)
	}

	// Collect includes starting from a.md
	var dependencies []IncludeDependency
	seen := make(map[string]bool)
	err := collectPackageIncludesRecursive(aContent, tmpDir, &dependencies, seen, false)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should have exactly 2 dependencies (a.md and b.md), circular reference prevented by seen map
	if len(dependencies) != 2 {
		t.Errorf("Expected 2 dependencies (circular handled by seen map), got %d", len(dependencies))
	}
}

// TestCopyIncludeDependenciesFromPackageWithForce tests copying include dependencies
func TestCopyIncludeDependenciesFromPackageWithForce(t *testing.T) {
	tests := []struct {
		name              string
		dependencies      []IncludeDependency
		setupSourceFiles  map[string]string // source path -> content
		setupTargetFiles  map[string]string // target path -> content
		force             bool
		expectFilesCount  int
		expectOverwritten map[string]bool // target path -> should be overwritten
	}{
		{
			name: "copy single dependency",
			dependencies: []IncludeDependency{
				{
					SourcePath: "source/file1.md",
					TargetPath: "file1.md",
					IsOptional: false,
				},
			},
			setupSourceFiles: map[string]string{
				"source/file1.md": "Content 1",
			},
			setupTargetFiles:  map[string]string{},
			force:             false,
			expectFilesCount:  1,
			expectOverwritten: map[string]bool{},
		},
		{
			name: "skip existing file without force",
			dependencies: []IncludeDependency{
				{
					SourcePath: "source/file1.md",
					TargetPath: "file1.md",
					IsOptional: false,
				},
			},
			setupSourceFiles: map[string]string{
				"source/file1.md": "New Content",
			},
			setupTargetFiles: map[string]string{
				"file1.md": "Old Content",
			},
			force:             false,
			expectFilesCount:  1,
			expectOverwritten: map[string]bool{"file1.md": false}, // should NOT be overwritten
		},
		{
			name: "overwrite existing file with force",
			dependencies: []IncludeDependency{
				{
					SourcePath: "source/file1.md",
					TargetPath: "file1.md",
					IsOptional: false,
				},
			},
			setupSourceFiles: map[string]string{
				"source/file1.md": "New Content",
			},
			setupTargetFiles: map[string]string{
				"file1.md": "Old Content",
			},
			force:             true,
			expectFilesCount:  1,
			expectOverwritten: map[string]bool{"file1.md": true}, // SHOULD be overwritten
		},
		{
			name: "skip optional missing file",
			dependencies: []IncludeDependency{
				{
					SourcePath: "source/optional.md",
					TargetPath: "optional.md",
					IsOptional: true,
				},
			},
			setupSourceFiles:  map[string]string{}, // file doesn't exist
			setupTargetFiles:  map[string]string{},
			force:             false,
			expectFilesCount:  0, // no file should be created
			expectOverwritten: map[string]bool{},
		},
		{
			name: "skip when content identical",
			dependencies: []IncludeDependency{
				{
					SourcePath: "source/file1.md",
					TargetPath: "file1.md",
					IsOptional: false,
				},
			},
			setupSourceFiles: map[string]string{
				"source/file1.md": "Same Content",
			},
			setupTargetFiles: map[string]string{
				"file1.md": "Same Content",
			},
			force:             false,
			expectFilesCount:  1,
			expectOverwritten: map[string]bool{}, // no overwrite needed
		},
		{
			name: "create nested directories",
			dependencies: []IncludeDependency{
				{
					SourcePath: "source/deeply/nested/file.md",
					TargetPath: "deeply/nested/file.md",
					IsOptional: false,
				},
			},
			setupSourceFiles: map[string]string{
				"source/deeply/nested/file.md": "Content",
			},
			setupTargetFiles:  map[string]string{},
			force:             false,
			expectFilesCount:  1,
			expectOverwritten: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directories
			sourceDir := testutil.TempDir(t, "test-*")
			targetDir := testutil.TempDir(t, "test-*")

			// Setup source files
			for path, content := range tt.setupSourceFiles {
				fullPath := filepath.Join(sourceDir, path)
				dir := filepath.Dir(fullPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("Failed to create source directory %s: %v", dir, err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to write source file %s: %v", fullPath, err)
				}
			}

			// Setup target files
			oldContent := make(map[string]string)
			for path, content := range tt.setupTargetFiles {
				fullPath := filepath.Join(targetDir, path)
				dir := filepath.Dir(fullPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("Failed to create target directory %s: %v", dir, err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to write target file %s: %v", fullPath, err)
				}
				oldContent[path] = content
			}

			// Adjust dependency paths to use temp directories
			adjustedDeps := make([]IncludeDependency, len(tt.dependencies))
			for i, dep := range tt.dependencies {
				adjustedDeps[i] = IncludeDependency{
					SourcePath: filepath.Join(sourceDir, dep.SourcePath),
					TargetPath: dep.TargetPath,
					IsOptional: dep.IsOptional,
				}
			}

			// Copy dependencies
			err := copyIncludeDependenciesFromPackageWithForce(adjustedDeps, targetDir, false, tt.force, nil)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Count files in target directory
			fileCount := 0
			filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					fileCount++
				}
				return nil
			})

			if fileCount != tt.expectFilesCount {
				t.Errorf("Expected %d files in target, got %d", tt.expectFilesCount, fileCount)
			}

			// Check overwrites
			for targetPath, shouldOverwrite := range tt.expectOverwritten {
				fullPath := filepath.Join(targetDir, targetPath)
				newContent, err := os.ReadFile(fullPath)
				if err != nil {
					t.Errorf("Failed to read target file %s: %v", targetPath, err)
					continue
				}

				sourceContent := tt.setupSourceFiles[filepath.Join("source", targetPath)]
				oldContentStr := oldContent[targetPath]

				if shouldOverwrite {
					// File should have new content
					if string(newContent) != sourceContent {
						t.Errorf("File %s should be overwritten with new content, but wasn't", targetPath)
					}
				} else {
					// File should have old content (or be unchanged)
					if oldContentStr != "" && string(newContent) != oldContentStr {
						t.Errorf("File %s should NOT be overwritten, but was", targetPath)
					}
				}
			}
		})
	}
}

// TestCopyIncludeDependenciesFromPackageWithForce_FileTracker tests file tracking
func TestCopyIncludeDependenciesFromPackageWithForce_FileTracker(t *testing.T) {
	sourceDir := testutil.TempDir(t, "test-*")
	targetDir := testutil.TempDir(t, "test-*")

	// Create source file
	sourceFile := filepath.Join(sourceDir, "file.md")
	if err := os.WriteFile(sourceFile, []byte("Content"), 0644); err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	tracker, err := NewFileTracker()
	if err != nil {
		t.Fatalf("Failed to create file tracker: %v", err)
	}

	// Test 1: New file should be tracked as created
	deps := []IncludeDependency{
		{
			SourcePath: sourceFile,
			TargetPath: "file.md",
			IsOptional: false,
		},
	}

	err = copyIncludeDependenciesFromPackageWithForce(deps, targetDir, false, false, tracker)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	targetFile := filepath.Join(targetDir, "file.md")
	found := false
	for _, f := range tracker.CreatedFiles {
		if f == targetFile {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("File should be tracked as created, got CreatedFiles: %v", tracker.CreatedFiles)
	}

	// Test 2: Modified file should be tracked as modified
	tracker2, err := NewFileTracker()
	if err != nil {
		t.Fatalf("Failed to create file tracker: %v", err)
	}
	// Update source content
	if err := os.WriteFile(sourceFile, []byte("New Content"), 0644); err != nil {
		t.Fatalf("Failed to update source file: %v", err)
	}

	err = copyIncludeDependenciesFromPackageWithForce(deps, targetDir, false, true, tracker2)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	found = false
	for _, f := range tracker2.ModifiedFiles {
		if f == targetFile {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("File should be tracked as modified, got ModifiedFiles: %v", tracker2.ModifiedFiles)
	}
}

// TestIncludePattern tests the include pattern regex
func TestIncludePattern(t *testing.T) {
	tests := []struct {
		line        string
		shouldMatch bool
		isOptional  bool
		path        string
	}{
		{
			line:        "@include shared/common.md",
			shouldMatch: true,
			isOptional:  false,
			path:        "shared/common.md",
		},
		{
			line:        "@include? shared/optional.md",
			shouldMatch: true,
			isOptional:  true,
			path:        "shared/optional.md",
		},
		{
			line:        "@include  shared/file.md  ",
			shouldMatch: true,
			isOptional:  false,
			path:        "shared/file.md",
		},
		{
			line:        "# Not an include",
			shouldMatch: false,
		},
		{
			line:        "@include file.md#section",
			shouldMatch: true,
			isOptional:  false,
			path:        "file.md#section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			matches := includePattern.FindStringSubmatch(tt.line)

			if tt.shouldMatch && matches == nil {
				t.Errorf("Expected line to match include pattern: %s", tt.line)
				return
			}
			if !tt.shouldMatch && matches != nil {
				t.Errorf("Expected line NOT to match include pattern: %s", tt.line)
				return
			}

			if tt.shouldMatch {
				isOptional := matches[1] == "?"
				if isOptional != tt.isOptional {
					t.Errorf("Expected isOptional=%v, got %v", tt.isOptional, isOptional)
				}

				path := strings.TrimSpace(matches[2])
				if path != tt.path {
					t.Errorf("Expected path=%s, got %s", tt.path, path)
				}
			}
		})
	}
}
