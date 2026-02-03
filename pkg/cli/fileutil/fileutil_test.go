//go:build !integration

package fileutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestFileExists(t *testing.T) {
	// Create a temporary directory
	tmpDir := testutil.TempDir(t, "test-*")

	// Test with existing file
	testFile := filepath.Join(tmpDir, "testfile.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if !FileExists(testFile) {
		t.Errorf("FileExists() = false, want true for existing file")
	}

	// Test with non-existent file
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.txt")
	if FileExists(nonExistentFile) {
		t.Errorf("FileExists() = true, want false for non-existent file")
	}

	// Test with directory (should return false)
	testDir := filepath.Join(tmpDir, "testdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	if FileExists(testDir) {
		t.Errorf("FileExists() = true, want false for directory")
	}
}

func TestDirExists(t *testing.T) {
	// Create a temporary directory
	tmpDir := testutil.TempDir(t, "test-*")

	// Test with existing directory
	testDir := filepath.Join(tmpDir, "testdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	if !DirExists(testDir) {
		t.Errorf("DirExists() = false, want true for existing directory")
	}

	// Test with non-existent directory
	nonExistentDir := filepath.Join(tmpDir, "nonexistent")
	if DirExists(nonExistentDir) {
		t.Errorf("DirExists() = true, want false for non-existent directory")
	}

	// Test with file (should return false)
	testFile := filepath.Join(tmpDir, "testfile.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if DirExists(testFile) {
		t.Errorf("DirExists() = true, want false for file")
	}
}

func TestIsDirEmpty(t *testing.T) {
	// Create a temporary directory
	tmpDir := testutil.TempDir(t, "test-*")

	// Test with empty directory
	emptyDir := filepath.Join(tmpDir, "empty")
	if err := os.Mkdir(emptyDir, 0755); err != nil {
		t.Fatalf("Failed to create empty directory: %v", err)
	}

	if !IsDirEmpty(emptyDir) {
		t.Errorf("IsDirEmpty() = false, want true for empty directory")
	}

	// Test with non-empty directory
	nonEmptyDir := filepath.Join(tmpDir, "nonempty")
	if err := os.Mkdir(nonEmptyDir, 0755); err != nil {
		t.Fatalf("Failed to create non-empty directory: %v", err)
	}

	testFile := filepath.Join(nonEmptyDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if IsDirEmpty(nonEmptyDir) {
		t.Errorf("IsDirEmpty() = true, want false for non-empty directory")
	}

	// Test with non-existent directory (should return true)
	nonExistentDir := filepath.Join(tmpDir, "nonexistent")
	if !IsDirEmpty(nonExistentDir) {
		t.Errorf("IsDirEmpty() = false, want true for non-existent directory")
	}
}

func TestCopyFile(t *testing.T) {
	// Create a temporary directory
	tmpDir := testutil.TempDir(t, "test-*")

	// Test successful copy
	srcFile := filepath.Join(tmpDir, "source.txt")
	srcContent := []byte("test content for copy")
	if err := os.WriteFile(srcFile, srcContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstFile := filepath.Join(tmpDir, "destination.txt")
	if err := CopyFile(srcFile, dstFile); err != nil {
		t.Errorf("CopyFile() error = %v, want nil", err)
	}

	// Verify destination file exists and has same content
	if !FileExists(dstFile) {
		t.Errorf("Destination file does not exist after copy")
	}

	dstContent, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(dstContent) != string(srcContent) {
		t.Errorf("Destination file content = %q, want %q", dstContent, srcContent)
	}

	// Test copy with non-existent source
	nonExistentSrc := filepath.Join(tmpDir, "nonexistent.txt")
	invalidDst := filepath.Join(tmpDir, "invalid.txt")
	if err := CopyFile(nonExistentSrc, invalidDst); err == nil {
		t.Errorf("CopyFile() with non-existent source: error = nil, want error")
	}

	// Test copy to invalid destination (subdirectory doesn't exist)
	invalidDstDir := filepath.Join(tmpDir, "nonexistent", "destination.txt")
	if err := CopyFile(srcFile, invalidDstDir); err == nil {
		t.Errorf("CopyFile() with invalid destination: error = nil, want error")
	}
}

func TestCalculateDirectorySize(t *testing.T) {
	// Create a temporary directory
	tmpDir := testutil.TempDir(t, "test-*")

	// Test with empty directory
	emptyDir := filepath.Join(tmpDir, "empty")
	if err := os.Mkdir(emptyDir, 0755); err != nil {
		t.Fatalf("Failed to create empty directory: %v", err)
	}

	size := CalculateDirectorySize(emptyDir)
	if size != 0 {
		t.Errorf("CalculateDirectorySize(empty) = %d, want 0", size)
	}

	// Test with directory containing files
	testDir := filepath.Join(tmpDir, "withfiles")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create files with known sizes
	file1 := filepath.Join(testDir, "file1.txt")
	file1Content := []byte("12345") // 5 bytes
	if err := os.WriteFile(file1, file1Content, 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	file2 := filepath.Join(testDir, "file2.txt")
	file2Content := []byte("1234567890") // 10 bytes
	if err := os.WriteFile(file2, file2Content, 0644); err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	expectedSize := int64(len(file1Content) + len(file2Content))
	actualSize := CalculateDirectorySize(testDir)
	if actualSize != expectedSize {
		t.Errorf("CalculateDirectorySize() = %d, want %d", actualSize, expectedSize)
	}

	// Test with nested directories
	nestedDir := filepath.Join(testDir, "nested")
	if err := os.Mkdir(nestedDir, 0755); err != nil {
		t.Fatalf("Failed to create nested directory: %v", err)
	}

	file3 := filepath.Join(nestedDir, "file3.txt")
	file3Content := []byte("abc") // 3 bytes
	if err := os.WriteFile(file3, file3Content, 0644); err != nil {
		t.Fatalf("Failed to create file3: %v", err)
	}

	expectedSizeWithNested := expectedSize + int64(len(file3Content))
	actualSizeWithNested := CalculateDirectorySize(testDir)
	if actualSizeWithNested != expectedSizeWithNested {
		t.Errorf("CalculateDirectorySize(with nested) = %d, want %d", actualSizeWithNested, expectedSizeWithNested)
	}

	// Test with non-existent directory (should return 0)
	nonExistentDir := filepath.Join(tmpDir, "nonexistent")
	nonExistentSize := CalculateDirectorySize(nonExistentDir)
	if nonExistentSize != 0 {
		t.Errorf("CalculateDirectorySize(non-existent) = %d, want 0", nonExistentSize)
	}
}
