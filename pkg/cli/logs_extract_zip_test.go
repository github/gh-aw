//go:build !integration

package cli

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractZipFileSuccess tests successful extraction of a zip file
func TestExtractZipFileSuccess(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	require.ErrorContains(t, err, "invalid file path", "Error should mention invalid path")
}

// TestExtractZipFileUsesSensitivePermissions tests that ZIP permissions do not override the local permission policy
func TestExtractZipFileUsesSensitivePermissions(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for extraction
	tempDir := t.TempDir()

	// Create an in-memory zip with restrictive file mode
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// Create a file with owner-only permissions
	header := &zip.FileHeader{
		Name:   "executable.sh",
		Method: zip.Deflate,
	}
	header.SetMode(0600)

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

	// Verify the extracted file uses sensitive permissions despite the restrictive zip mode
	extractedPath := filepath.Join(tempDir, "executable.sh")
	info, err := os.Stat(extractedPath)
	require.NoError(t, err, "Failed to stat extracted file")

	assert.Equal(t, constants.FilePermSensitive, info.Mode().Perm(), "File should have sensitive permissions")
}

// TestExtractZipFileWithNestedDirectories tests extraction with nested paths
func TestExtractZipFileWithNestedDirectories(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

		// Extract to a read-only destination (will fail on create in most environments).
		//
		// Note: When running as root (or with elevated permissions), creating files inside a
		// 0555 directory may still succeed depending on the platform and filesystem.
		// In that case, we skip this assertion rather than make the suite flaky.
		readOnlyDir := filepath.Join(tempDir, "readonly")
		err = os.MkdirAll(readOnlyDir, 0555) // Read-only directory
		require.NoError(t, err)

		// Try to extract - should fail and return error
		err = extractZipFile(zipReader.File[0], readOnlyDir, false)
		if err == nil {
			// Likely running with elevated privileges.
			t.Skip("expected extraction to fail in read-only directory, but it succeeded (likely elevated privileges)")
		}
		assert.ErrorContains(t, err, "failed to create", "Error should mention creation failure")
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

func TestUnzipFileWithLimits(t *testing.T) {
	t.Parallel()

	zipPath := filepath.Join(t.TempDir(), "multi-entry.zip")
	zipFile, err := os.Create(zipPath)
	require.NoError(t, err)

	zipWriter := zip.NewWriter(zipFile)
	for name, content := range map[string]string{
		"first.txt":  "first",
		"second.txt": "second",
	} {
		writer, err := zipWriter.Create(name)
		require.NoError(t, err)
		_, err = writer.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zipWriter.Close())
	require.NoError(t, zipFile.Close())

	tests := []struct {
		name          string
		maxEntryCount int
		maxTotalSize  uint64
		wantError     string
	}{
		{
			name:          "entry count",
			maxEntryCount: 1,
			maxTotalSize:  maxZipTotalSize,
			wantError:     "zip archive contains too many entries",
		},
		{
			name:          "total uncompressed size",
			maxEntryCount: 2,
			maxTotalSize:  uint64(len("first") + len("second") - 1),
			wantError:     "zip archive exceeds total uncompressed size limit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destDir := t.TempDir()
			err := unzipFileWithLimits(zipPath, destDir, false, tt.maxEntryCount, tt.maxTotalSize)
			require.ErrorContains(t, err, tt.wantError)

			entries, err := os.ReadDir(destDir)
			require.NoError(t, err)
			assert.Empty(t, entries, "archive validation should happen before extracting any entries")
		})
	}

	t.Run("exact total and entry count", func(t *testing.T) {
		destDir := t.TempDir()
		require.NoError(t, unzipFileWithLimits(zipPath, destDir, false, 2, uint64(len("first")+len("second"))))
		for name, content := range map[string]string{"first.txt": "first", "second.txt": "second"} {
			extracted, err := os.ReadFile(filepath.Join(destDir, name))
			require.NoError(t, err)
			assert.Equal(t, content, string(extracted))
		}
	})
}

func TestUnzipRejectsHighCompressionRatioArchive(t *testing.T) {
	t.Parallel()

	const uncompressedSize = 2 * 1024 * 1024
	zipPath := filepath.Join(t.TempDir(), "compression-bomb.zip")
	zipFile, err := os.Create(zipPath)
	require.NoError(t, err)

	zipWriter := zip.NewWriter(zipFile)
	writer, err := zipWriter.Create("repeated.txt")
	require.NoError(t, err)
	_, err = writer.Write(bytes.Repeat([]byte("A"), uncompressedSize))
	require.NoError(t, err)
	require.NoError(t, zipWriter.Close())
	require.NoError(t, zipFile.Close())

	archive, err := zip.OpenReader(zipPath)
	require.NoError(t, err)
	require.Len(t, archive.File, 1)
	require.Less(t, archive.File[0].CompressedSize64, uint64(uncompressedSize/100))
	require.NoError(t, archive.Close())

	destDir := t.TempDir()
	err = unzipFileWithLimits(zipPath, destDir, false, maxZipEntryCount, uncompressedSize/2)
	require.ErrorContains(t, err, "zip archive exceeds total uncompressed size limit")

	entries, err := os.ReadDir(destDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "archive validation should happen before decompressing entries")
}

func TestUnzipFileDirectoryLimit(t *testing.T) {
	t.Parallel()

	for _, directoryEntry := range []bool{false, true} {
		t.Run(fmt.Sprintf("directory entries %t", directoryEntry), func(t *testing.T) {
			zipPath := filepath.Join(t.TempDir(), "directories.zip")
			archive, err := os.Create(zipPath)
			require.NoError(t, err)
			writer := zip.NewWriter(archive)
			for i := 0; i <= maxZipDirectoryCount/100; i++ {
				name := fmt.Sprintf("%d/", i) + strings.Repeat("d/", 99)
				if !directoryEntry {
					name += "empty.txt"
				}
				_, err := writer.Create(name)
				require.NoError(t, err)
			}
			require.NoError(t, writer.Close())
			require.NoError(t, archive.Close())

			destDir := t.TempDir()
			require.ErrorContains(t, unzipFile(zipPath, destDir, false), "zip archive requires too many directories")
			entries, err := os.ReadDir(destDir)
			require.NoError(t, err)
			assert.Empty(t, entries, "directory validation must happen before extraction")
		})
	}
}

func TestExtractZipFileWithLimit(t *testing.T) {
	t.Parallel()

	buf := new(bytes.Buffer)
	writer := zip.NewWriter(buf)
	entry, err := writer.Create("nested/test.txt")
	require.NoError(t, err)
	_, err = entry.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	for _, budget := range []uint64{0, 3, 4, 5, 6} {
		t.Run(fmt.Sprintf("budget %d", budget), func(t *testing.T) {
			destDir := t.TempDir()
			written, err := extractZipFileWithLimit(reader.File[0], destDir, false, budget)
			if budget < 5 {
				require.ErrorContains(t, err, fmt.Sprintf("exceeded size limit (%d bytes)", budget))
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, min(uint64(5), budget+1), written)
			content, err := os.ReadFile(filepath.Join(destDir, "nested", "test.txt"))
			require.NoError(t, err)
			assert.Equal(t, "hello"[:int(written)], string(content))
		})
	}

	t.Run("per-entry metadata limit", func(t *testing.T) {
		file := *reader.File[0]
		file.UncompressedSize64 = maxZipEntrySize + 1
		destDir := t.TempDir()
		written, err := extractZipFileWithLimit(&file, destDir, false, maxZipTotalSize)
		require.ErrorContains(t, err, "file too large in zip")
		assert.Zero(t, written)
		entries, err := os.ReadDir(destDir)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})
}
