package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var fileTrackerLog = logger.New("cli:file_tracker")

// FileTracker keeps track of files created or modified during workflow operations
// to enable proper staging and rollback functionality
type FileTracker struct {
	CreatedFiles    []string
	ModifiedFiles   []string
	OriginalContent map[string][]byte // Store original content for rollback
	gitRoot         string
}

// NewFileTracker creates a new file tracker
func NewFileTracker() (*FileTracker, error) {
	fileTrackerLog.Print("Creating new file tracker")
	gitRoot, err := findGitRoot()
	if err != nil {
		fileTrackerLog.Printf("Failed to find git root: %v", err)
		return nil, fmt.Errorf("file tracker requires being in a git repository: %w", err)
	}
	fileTrackerLog.Printf("File tracker initialized with git root: %s", gitRoot)
	return &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         gitRoot,
	}, nil
}

// TrackCreated adds a file to the created files list
func (ft *FileTracker) TrackCreated(filePath string) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}
	fileTrackerLog.Printf("Tracking created file: %s", absPath)
	ft.CreatedFiles = append(ft.CreatedFiles, absPath)
}

// TrackModified adds a file to the modified files list and stores its original content
func (ft *FileTracker) TrackModified(filePath string) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	// Store original content if not already stored
	if _, exists := ft.OriginalContent[absPath]; !exists {
		if content, err := os.ReadFile(absPath); err == nil {
			ft.OriginalContent[absPath] = content
			fileTrackerLog.Printf("Tracking modified file: %s (stored %d bytes)", absPath, len(content))
		} else {
			fileTrackerLog.Printf("Tracking modified file: %s (failed to store original: %v)", absPath, err)
		}
	}

	ft.ModifiedFiles = append(ft.ModifiedFiles, absPath)
}

// GetAllFiles returns all tracked files (created and modified)
func (ft *FileTracker) GetAllFiles() []string {
	all := make([]string, 0, len(ft.CreatedFiles)+len(ft.ModifiedFiles))
	all = append(all, ft.CreatedFiles...)
	all = append(all, ft.ModifiedFiles...)
	return all
}

// StageAllFiles stages all tracked files using git add
func (ft *FileTracker) StageAllFiles(verbose bool) error {
	allFiles := ft.GetAllFiles()
	fileTrackerLog.Printf("Staging %d tracked files", len(allFiles))
	if len(allFiles) == 0 {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No files to stage"))
		}
		return nil
	}

	console.LogVerbose(verbose, fmt.Sprintf("Staging %d files...", len(allFiles)))
	if verbose {
		for _, file := range allFiles {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("  - %s", file)))
		}
	}

	// Stage all files in a single git add command
	args := append([]string{"add"}, allFiles...)
	cmd := exec.Command("git", args...)
	cmd.Dir = ft.gitRoot
	if err := cmd.Run(); err != nil {
		fileTrackerLog.Printf("Failed to stage files: %v", err)
		return fmt.Errorf("failed to stage files: %w", err)
	}

	fileTrackerLog.Printf("Successfully staged all files")
	return nil
}

// RollbackCreatedFiles deletes all files that were created during the operation
func (ft *FileTracker) RollbackCreatedFiles(verbose bool) error {
	if len(ft.CreatedFiles) == 0 {
		return nil
	}

	fileTrackerLog.Printf("Rolling back %d created files", len(ft.CreatedFiles))
	console.LogVerbose(verbose, fmt.Sprintf("Rolling back %d created files...", len(ft.CreatedFiles)))

	var errors []string
	for _, file := range ft.CreatedFiles {
		console.LogVerbose(verbose, fmt.Sprintf("  - Deleting %s", file))
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			fileTrackerLog.Printf("Failed to delete %s: %v", file, err)
			errors = append(errors, fmt.Sprintf("failed to delete %s: %v", file, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("rollback errors: %s", strings.Join(errors, "; "))
	}

	fileTrackerLog.Print("Successfully rolled back created files")
	return nil
}

// RollbackModifiedFiles restores all modified files to their original state
func (ft *FileTracker) RollbackModifiedFiles(verbose bool) error {
	if len(ft.ModifiedFiles) == 0 {
		return nil
	}

	console.LogVerbose(verbose, fmt.Sprintf("Rolling back %d modified files...", len(ft.ModifiedFiles)))

	var errors []string
	for _, file := range ft.ModifiedFiles {
		console.LogVerbose(verbose, fmt.Sprintf("  - Restoring %s", file))

		// Restore original content if we have it
		if originalContent, exists := ft.OriginalContent[file]; exists {
			// Use owner-only read/write permissions (0600) for security best practices
			if err := os.WriteFile(file, originalContent, 0600); err != nil {
				errors = append(errors, fmt.Sprintf("failed to restore %s: %v", file, err))
			}
		} else {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No original content stored for %s", file)))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("rollback errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

// RollbackAllFiles rolls back both created and modified files
func (ft *FileTracker) RollbackAllFiles(verbose bool) error {
	var errors []string

	if err := ft.RollbackCreatedFiles(verbose); err != nil {
		errors = append(errors, fmt.Sprintf("created files rollback: %v", err))
	}

	if err := ft.RollbackModifiedFiles(verbose); err != nil {
		errors = append(errors, fmt.Sprintf("modified files rollback: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("rollback errors: %s", strings.Join(errors, "; "))
	}

	return nil
}
