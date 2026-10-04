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

// unzipFile extracts a zip file to a destination directory
func unzipFile(zipPath, destDir string, verbose bool) error {
	// Open the zip file
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer r.Close()

	logsDownloadZipLog.Printf("Extracting %d entries from %s to %s", len(r.File), zipPath, destDir)

	// Extract each file in the zip
	for _, f := range r.File {
		if err := extractZipFile(f, destDir, verbose); err != nil {
			return err
		}
	}

	return nil
}

// extractZipFile extracts a single file from a zip archive
func extractZipFile(f *zip.File, destDir string, verbose bool) (extractErr error) {
	// #nosec G305 -- Path traversal is prevented by filepath.Clean and prefix check below
	// Validate file name doesn't contain path traversal attempts
	cleanName := filepath.Clean(f.Name)
	if strings.Contains(cleanName, "..") {
		logsDownloadZipLog.Printf("Rejected zip entry with path traversal attempt: %s", f.Name)
		return fmt.Errorf("invalid file path in zip (contains ..): %s", f.Name)
	}

	// Construct the full path for the file
	filePath := filepath.Join(destDir, cleanName)

	// Prevent zip slip vulnerability - ensure extracted path is within destDir
	cleanDest := filepath.Clean(destDir)
	if !strings.HasPrefix(filepath.Clean(filePath), cleanDest+string(os.PathSeparator)) && filepath.Clean(filePath) != cleanDest {
		logsDownloadZipLog.Printf("Rejected zip entry escaping destination (zip slip): %s", f.Name)
		return fmt.Errorf("invalid file path in zip (outside destination): %s", f.Name)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Extracting: "+cleanName))
	}

	// Create directory if it's a directory entry
	if f.FileInfo().IsDir() {
		return os.MkdirAll(filePath, constants.DirPermPublic)
	}

	// Decompression bomb protection - limit individual file size to 1GB
	// #nosec G110 -- Decompression bomb is mitigated by size check below
	const maxFileSize = 1 * 1024 * 1024 * 1024 // 1GB
	if f.UncompressedSize64 > maxFileSize {
		logsDownloadZipLog.Printf("Rejected oversized zip entry (decompression bomb guard): %s (%d bytes)", f.Name, f.UncompressedSize64)
		return fmt.Errorf("file too large in zip (>1GB): %s (%d bytes)", f.Name, f.UncompressedSize64)
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(filePath), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open the file in the zip
	srcFile, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer srcFile.Close()

	// Create the destination file
	destFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, constants.FilePermPublic)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
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
	limitedReader := io.LimitReader(srcFile, int64(maxFileSize)+1)
	written, err := io.Copy(destFile, limitedReader)
	if err != nil {
		extractErr = fmt.Errorf("failed to extract file: %w", err)
		return extractErr
	}

	// Verify we didn't exceed the size limit.
	// written == maxFileSize+1 means the reader was not exhausted, i.e. the
	// actual content is larger than maxFileSize.
	if written > int64(maxFileSize) {
		extractErr = fmt.Errorf("file extraction exceeded size limit: %s", f.Name)
		return extractErr
	}

	return nil
}
