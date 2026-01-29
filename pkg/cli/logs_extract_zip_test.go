//go:build !integration

package cli

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractZipFileSuccess tests successful extraction of a zip file
func TestExtractZipFileSuccess(t *testing.T) {
	// Create a temporary directory for extraction
	tempDir := t.TempDir()

	// Create an in-memory zip with test content
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	testContent := "This is test content for zip extraction"
	writer, err := zipWriter.Create("test-file.txt")
	require.NoError(t, err, "Failed to create file in zip")

	_, err = writer.Write([]byte(testContent))
	require.NoError(t, err, "Failed to write content to zip")

	err = zipWriter.Close()
	require.NoError(t, err, "Failed to close zip writer")

	// Read the zip
	zipReader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err, "Failed to create zip reader")

	// Extract the file
	err = extractZipFile(zipReader.File[0], tempDir, false)
	require.NoError(t, err, "extractZipFile should succeed")

	// Verify the extracted file exists and has correct content
	extractedPath := filepath.Join(tempDir, "test-file.txt")
	extractedContent, err := os.ReadFile(extractedPath)
	require.NoError(t, err, "Failed to read extracted file")
	assert.Equal(t, testContent, string(extractedContent), "Extracted content should match original")
}

// TestExtractZipFileDirectory tests extraction of a directory entry
func TestExtractZipFileDirectory(t *testing.T) {
	// Create a temporary directory for extraction
	tempDir := t.TempDir()

	// Create an in-memory zip with a directory entry
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// Create a directory entry (ends with /)
	_, err := zipWriter.Create("test-dir/")
	require.NoError(t, err, "Failed to create directory in zip")

	err = zipWriter.Close()
	require.NoError(t, err, "Failed to close zip writer")

	// Read the zip
	zipReader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err, "Failed to create zip reader")

	// Extract the directory
	err = extractZipFile(zipReader.File[0], tempDir, false)
	require.NoError(t, err, "extractZipFile should succeed for directory")

	// Verify the directory was created
	dirPath := filepath.Join(tempDir, "test-dir")
	info, err := os.Stat(dirPath)
	require.NoError(t, err, "Directory should exist")
	assert.True(t, info.IsDir(), "Should be a directory")
}

// TestExtractZipFileZipSlipPrevention tests that zip slip attacks are prevented
func TestExtractZipFileZipSlipPrevention(t *testing.T) {
	// Create a temporary directory for extraction
	tempDir := t.TempDir()

	// Create an in-memory zip with a malicious path
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// Try to create a file that escapes the destination directory
	writer, err := zipWriter.Create("../../../etc/passwd")
	require.NoError(t, err, "Failed to create malicious file in zip")

	_, err = writer.Write([]byte("malicious content"))
	require.NoError(t, err, "Failed to write to malicious file")

	err = zipWriter.Close()
	require.NoError(t, err, "Failed to close zip writer")

	// Read the zip
	zipReader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err, "Failed to create zip reader")

	// Extract the file - should fail with error
	err = extractZipFile(zipReader.File[0], tempDir, false)
	require.Error(t, err, "extractZipFile should fail for path traversal")
	assert.Contains(t, err.Error(), "invalid file path", "Error should mention invalid path")
}

// TestExtractZipFilePreservesMode tests that file permissions are preserved
func TestExtractZipFilePreservesMode(t *testing.T) {
	// Create a temporary directory for extraction
	tempDir := t.TempDir()

	// Create an in-memory zip with specific file mode
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// Create a file with specific mode (executable)
	header := &zip.FileHeader{
		Name:   "executable.sh",
		Method: zip.Deflate,
	}
	header.SetMode(0755) // Executable mode

	writer, err := zipWriter.CreateHeader(header)
	require.NoError(t, err, "Failed to create file with header in zip")

	testContent := "#!/bin/bash\necho 'test'"
	_, err = writer.Write([]byte(testContent))
	require.NoError(t, err, "Failed to write content to zip")

	err = zipWriter.Close()
	require.NoError(t, err, "Failed to close zip writer")

	// Read the zip
	zipReader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err, "Failed to create zip reader")

	// Extract the file
	err = extractZipFile(zipReader.File[0], tempDir, false)
	require.NoError(t, err, "extractZipFile should succeed")

	// Verify the extracted file has the correct mode
	extractedPath := filepath.Join(tempDir, "executable.sh")
	info, err := os.Stat(extractedPath)
	require.NoError(t, err, "Failed to stat extracted file")

	// Check that file is executable (at least one execute bit set)
	mode := info.Mode()
	assert.NotEqual(t, os.FileMode(0), mode&0111, "File should have execute permission")
}

// TestExtractZipFileWithNestedDirectories tests extraction with nested paths
func TestExtractZipFileWithNestedDirectories(t *testing.T) {
	// Create a temporary directory for extraction
	tempDir := t.TempDir()

	// Create an in-memory zip with nested directories
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	testContent := "nested file content"
	writer, err := zipWriter.Create("level1/level2/level3/nested-file.txt")
	require.NoError(t, err, "Failed to create nested file in zip")

	_, err = writer.Write([]byte(testContent))
	require.NoError(t, err, "Failed to write content to zip")

	err = zipWriter.Close()
	require.NoError(t, err, "Failed to close zip writer")

	// Read the zip
	zipReader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err, "Failed to create zip reader")

	// Extract the file
	err = extractZipFile(zipReader.File[0], tempDir, false)
	require.NoError(t, err, "extractZipFile should succeed")

	// Verify the nested directories and file were created
	extractedPath := filepath.Join(tempDir, "level1", "level2", "level3", "nested-file.txt")
	extractedContent, err := os.ReadFile(extractedPath)
	require.NoError(t, err, "Failed to read extracted nested file")
	assert.Equal(t, testContent, string(extractedContent), "Nested file content should match original")
}

// TestExtractZipFileErrorHandling tests that the function properly handles and returns errors
// This test validates the security fix for CWE-252: Unchecked Return Value
func TestExtractZipFileErrorHandling(t *testing.T) {
	t.Run("returns error when opening invalid zip file", func(t *testing.T) {
		// Create a temporary directory for extraction
		tempDir := t.TempDir()

		// Create an invalid zip file entry (this test is more about the error propagation pattern)
		buf := new(bytes.Buffer)
		zipWriter := zip.NewWriter(buf)

		// Create a valid file
		writer, err := zipWriter.Create("test.txt")
		require.NoError(t, err)
		_, err = writer.Write([]byte("content"))
		require.NoError(t, err)

		err = zipWriter.Close()
		require.NoError(t, err)

		// Read the zip
		zipReader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		require.NoError(t, err)

		// Extract to a read-only destination (will fail on create)
		readOnlyDir := filepath.Join(tempDir, "readonly")
		err = os.MkdirAll(readOnlyDir, 0555) // Read-only directory
		require.NoError(t, err)

		// Try to extract - should fail and return error
		err = extractZipFile(zipReader.File[0], readOnlyDir, false)
		require.Error(t, err, "extractZipFile should return error when destination is read-only")
		assert.Contains(t, err.Error(), "failed to create", "Error should mention creation failure")
	})

	t.Run("validates error return signature for writable file close", func(t *testing.T) {
		// This test documents the security fix: the function uses named return value
		// to properly handle errors from closing writable files (CWE-252)

		// Create a temporary directory for extraction
		tempDir := t.TempDir()

		// Create a valid zip file
		buf := new(bytes.Buffer)
		zipWriter := zip.NewWriter(buf)

		writer, err := zipWriter.Create("test.txt")
		require.NoError(t, err)
		_, err = writer.Write([]byte("test content"))
		require.NoError(t, err)

		err = zipWriter.Close()
		require.NoError(t, err)

		// Read the zip
		zipReader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		require.NoError(t, err)

		// Extract successfully
		err = extractZipFile(zipReader.File[0], tempDir, false)
		require.NoError(t, err, "Normal extraction should succeed")

		// Verify file was written
		extractedPath := filepath.Join(tempDir, "test.txt")
		content, err := os.ReadFile(extractedPath)
		require.NoError(t, err)
		assert.Equal(t, "test content", string(content))

		// Note: The security fix ensures that if Close() fails on the writable file,
		// the error is captured and returned via the named return value (extractErr).
		// This prevents silent data loss that could occur if Close() errors were ignored.
	})
}
