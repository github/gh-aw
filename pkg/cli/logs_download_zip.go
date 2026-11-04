// This file provides command-line interface functionality for gh-aw.
// This file (logs_download_zip.go) contains functions for extracting zip
// archives downloaded from GitHub Actions, including protections against
// path traversal (zip slip) and decompression bombs.

package cli

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var logsDownloadZipLog = logger.New("cli:logs_download_zip")

const maxZipEntrySize = 1 * 1024 * 1024 * 1024 // 1GB
const maxZipTotalSize = 1 * 1024 * 1024 * 1024 // 1GB
const maxZipEntryCount = 10000
const maxZipDirectoryCount = 10000

// unzipFile extracts a zip file to a destination directory
func unzipFile(zipPath, destDir string, verbose bool) error {
	return unzipFileWithLimits(zipPath, destDir, verbose, maxZipEntryCount, maxZipTotalSize)
}

func unzipFileWithLimits(zipPath, destDir string, verbose bool, maxEntryCount int, maxTotalSize uint64) (extractErr error) {
	// Open the zip file
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer func() {
		if err := r.Close(); extractErr == nil && err != nil {
			extractErr = fmt.Errorf("failed to close zip file: %w", err)
		}
	}()

	logsDownloadZipLog.Printf("Extracting %d entries from %s to %s", len(r.File), zipPath, destDir)

	if len(r.File) > maxEntryCount {
		return fmt.Errorf("zip archive contains too many entries (max %d)", maxEntryCount)
	}

	var declaredSize uint64
	directories := make(map[string]struct{})
	for _, f := range r.File {
		if f.UncompressedSize64 > maxZipEntrySize {
			return fmt.Errorf("file too large in zip (>1GB): %s (%d bytes)", f.Name, f.UncompressedSize64)
		}
		if f.UncompressedSize64 > maxTotalSize-declaredSize {
			return fmt.Errorf("zip archive exceeds total uncompressed size limit (%d bytes)", maxTotalSize)
		}
		declaredSize += f.UncompressedSize64

		dir := filepath.Clean(f.Name)
		if !f.FileInfo().IsDir() {
			dir = filepath.Dir(dir)
		}
		for dir != "." && dir != string(os.PathSeparator) {
			if _, exists := directories[dir]; exists {
				break
			}
			directories[dir] = struct{}{}
			if len(directories) > maxZipDirectoryCount {
				return fmt.Errorf("zip archive requires too many directories (max %d)", maxZipDirectoryCount)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// Extract each file in the zip
	remainingSize := maxTotalSize
	for _, f := range r.File {
		written, err := extractZipFileWithLimit(f, destDir, verbose, remainingSize)
		if err != nil {
			return err
		}
		remainingSize -= written
	}

	return nil
}

// extractZipFile extracts a single file from a zip archive
func extractZipFile(f *zip.File, destDir string, verbose bool) error {
	_, err := extractZipFileWithLimit(f, destDir, verbose, maxZipEntrySize)
	return err
}

func extractZipFileWithLimit(f *zip.File, destDir string, verbose bool, maxSize uint64) (uint64, error) {
	// #nosec G305 -- Path traversal is prevented by filepath.Clean and prefix check below
	// Validate file name doesn't contain path traversal attempts
	cleanName := filepath.Clean(f.Name)
	if strings.Contains(cleanName, "..") {
		logsDownloadZipLog.Printf("Rejected zip entry with path traversal attempt: %s", f.Name)
		return 0, fmt.Errorf("invalid file path in zip (contains ..): %s", f.Name)
	}

	// Construct the full path for the file
	filePath := filepath.Join(destDir, cleanName)

	// Prevent zip slip vulnerability - ensure extracted path is within destDir
	cleanDest := filepath.Clean(destDir)
	if !strings.HasPrefix(filepath.Clean(filePath), cleanDest+string(os.PathSeparator)) && filepath.Clean(filePath) != cleanDest {
		logsDownloadZipLog.Printf("Rejected zip entry escaping destination (zip slip): %s", f.Name)
		return 0, fmt.Errorf("invalid file path in zip (outside destination): %s", f.Name)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Extracting: "+cleanName))
	}

	// Create directory if it's a directory entry
	if f.FileInfo().IsDir() {
		return 0, os.MkdirAll(filePath, constants.DirPermPublic)
	}

	// Decompression bomb protection - limit individual file size to 1GB
	// #nosec G110 -- Decompression bomb is mitigated by size check below
	if f.UncompressedSize64 > maxZipEntrySize {
		logsDownloadZipLog.Printf("Rejected oversized zip entry (decompression bomb guard): %s (%d bytes)", f.Name, f.UncompressedSize64)
		return 0, fmt.Errorf("file too large in zip (>1GB): %s (%d bytes)", f.Name, f.UncompressedSize64)
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(filePath), constants.DirPermPublic); err != nil {
		return 0, fmt.Errorf("failed to create directory: %w", err)
	}

	return extractZipFileContents(f, filePath, min(maxSize, maxZipEntrySize))
}

func extractZipFileContents(f *zip.File, filePath string, maxSize uint64) (writtenBytes uint64, extractErr error) {
	// Open the file in the zip
	srcFile, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer func() {
		if err := srcFile.Close(); extractErr == nil && err != nil {
			extractErr = fmt.Errorf("failed to close file in zip: %w", err)
		}
	}()

	// Create the destination file
	destFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, constants.FilePermSensitive)
	if err != nil {
		return 0, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		// Handle errors from closing the writable file to prevent data loss
		// Data written to a file may be cached in memory and only flushed when the file is closed.
		// If Close() fails and the error is ignored, data loss can occur silently.
		if err := destFile.Close(); extractErr == nil && err != nil {
			extractErr = fmt.Errorf("failed to close destination file: %w", err)
		}
	}()

	// Copy the content with size limit enforcement.
	// Limit to maxFileSize+1 bytes: if exactly maxFileSize+1 bytes can be read
	// the archive is over the limit and must be rejected.
	limitedReader := io.LimitReader(srcFile, int64(maxSize)+1)
	written, err := io.Copy(destFile, limitedReader)
	if err != nil {
		extractErr = fmt.Errorf("failed to extract file: %w", err)
		return uint64(written), extractErr
	}
	writtenBytes = uint64(written)

	// Verify we didn't exceed the size limit.
	// written == maxSize+1 means the reader was not exhausted, i.e. the
	// actual content is larger than the allowed size.
	if uint64(written) > maxSize {
		extractErr = fmt.Errorf("file extraction exceeded size limit (%d bytes): %s", maxSize, f.Name)
		return writtenBytes, extractErr
	}

	return writtenBytes, nil
}
