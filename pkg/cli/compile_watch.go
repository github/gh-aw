package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileWatchLog = logger.New("cli:compile_watch")

// watchAndCompileWorkflows watches for changes to workflow files and recompiles them automatically
func watchAndCompileWorkflows(ctx context.Context, markdownFile string, compiler *workflow.Compiler, verbose bool) error {
	// Find git root for consistent behavior
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return fmt.Errorf("watch mode requires being in a git repository: %w", err)
	}

	workflowsDir := filepath.Join(gitRoot, constants.GetWorkflowDir())
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		return fmt.Errorf("the %s directory does not exist in git root (%s)", constants.GetWorkflowDir(), gitRoot)
	}

	// If a specific file is provided, watch only that file and its directory
	if markdownFile != "" {
		if !filepath.IsAbs(markdownFile) {
			markdownFile = filepath.Join(workflowsDir, markdownFile)
		}
		if _, err := os.Stat(markdownFile); os.IsNotExist(err) {
			return fmt.Errorf("specified markdown file does not exist: %s", markdownFile)
		}
	}

	// Build dependency graph for intelligent recompilation
	depGraph := NewDependencyGraph(workflowsDir)
	compileWatchLog.Print("Building dependency graph for watch mode...")
	if err := depGraph.BuildGraph(compiler); err != nil {
		compileWatchLog.Printf("Warning: failed to build dependency graph: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to build dependency graph: %v", err)))
	} else {
		compileWatchLog.Printf("Dependency graph built successfully: %d workflows", len(depGraph.nodes))
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Dependency graph built: %d workflows", len(depGraph.nodes))))
		}
	}

	// Set up file system watcher with buffered events for better handling of burst activity
	watcher, err := fsnotify.NewBufferedWatcher(100)
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer func() {
		if err := watcher.Close(); err != nil {
			compileWatchLog.Printf("Failed to close file watcher: %v", err)
		}
	}()

	// addWatchPath adds a path to the watcher with platform-specific configuration.
	// On Windows, uses a larger buffer (64KB) to prevent event overflow in busy directories.
	addWatchPath := func(path string) error {
		if runtime.GOOS == "windows" {
			return watcher.AddWith(path, fsnotify.WithBufferSize(64*1024))
		}
		return watcher.Add(path)
	}

	// Add the workflows directory to the watcher
	if err := addWatchPath(workflowsDir); err != nil {
		return fmt.Errorf("failed to watch directory %s: %w", workflowsDir, err)
	}

	// Also watch subdirectories for include files (recursive watching)
	walkErr := filepath.Walk(workflowsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors but continue walking
		}
		if info.IsDir() && path != workflowsDir {
			// Add subdirectories to the watcher
			if err := addWatchPath(path); err != nil {
				compileWatchLog.Printf("Failed to watch subdirectory %s: %v", path, err)
			} else {
				compileWatchLog.Printf("Watching subdirectory: %s", path)
			}
		}
		return nil
	})
	if walkErr != nil {
		compileWatchLog.Printf("Failed to walk subdirectories: %v", walkErr)
	}

	// Always emit the begin pattern for task integration
	if markdownFile != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Watching for file changes to %s...", markdownFile)))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Watching for file changes in %s...", workflowsDir)))
	}

	if verbose {
		fmt.Fprintln(os.Stderr, "Press Ctrl+C to stop watching.")
	}

	// Debouncing setup
	const debounceDelay = 300 * time.Millisecond
	var debounceTimer *time.Timer
	var debounceMu sync.Mutex
	modifiedFiles := make(map[string]struct{})

	// Compile initially if no specific file provided
	if markdownFile == "" {
		fmt.Fprintln(os.Stderr, "Watching for file changes")
		if verbose {
			fmt.Fprintln(os.Stderr, "🔨 Initial compilation of all workflow files...")
		}
		stats, err := compileAllWorkflowFiles(ctx, compiler, workflowsDir, verbose)
		if err != nil {
			// Always show initial compilation errors, not just in verbose mode
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Initial compilation failed: %v", err)))
		}
		// Print summary instead of just "Recompiled"
		printCompilationSummary(stats, false)
	} else {
		// Reset warning count before compilation
		compiler.ResetWarningCount()

		// Track compilation statistics for single file
		stats := &CompilationStats{}

		fmt.Fprintln(os.Stderr, "Watching for file changes")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatProgressMessageStderr(fmt.Sprintf("Initial compilation of %s...", markdownFile)))
		}

		// Use compileSingleFile to handle both regular workflows and campaign files
		compileSingleFile(ctx, compiler, markdownFile, stats, verbose, false)

		// Get warning count from compiler
		stats.Warnings = compiler.GetWarningCount()

		// Print summary instead of just "Recompiled"
		printCompilationSummary(stats, false)
	}

	// Main watch loop
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return errors.New("watcher channel closed")
			}

			// Filter out Chmod events (noisy and usually not useful for workflow changes)
			if event.Has(fsnotify.Chmod) {
				continue
			}

			// Only process markdown files and ignore lock files
			if !strings.HasSuffix(event.Name, ".md") {
				continue
			}

			// If watching a specific file, only process that file
			if markdownFile != "" && event.Name != markdownFile {
				continue
			}

			compileWatchLog.Printf("Detected change: %s (%s)", event.Name, event.Op.String())
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Detected change: %s (%s)", event.Name, event.Op.String())))
			}

			// Handle file operations
			switch {
			case event.Has(fsnotify.Remove):
				// Handle file deletion
				handleFileDeleted(event.Name, verbose)
				// Remove from dependency graph
				depGraph.RemoveWorkflow(event.Name)
			case event.Has(fsnotify.Write) || event.Has(fsnotify.Create):
				// Handle file modification or creation - add to debounced compilation
				func() {
					debounceMu.Lock()
					defer debounceMu.Unlock()
					modifiedFiles[event.Name] = struct{}{}

					// Reset debounce timer
					if debounceTimer != nil {
						debounceTimer.Stop()
					}
					debounceTimer = time.AfterFunc(debounceDelay, func() {
						filesToCompile := func() []string {
							debounceMu.Lock()
							defer debounceMu.Unlock()
							files := make([]string, 0, len(modifiedFiles))
							for file := range modifiedFiles {
								files = append(files, file)
							}
							// Clear the modifiedFiles map
							modifiedFiles = make(map[string]struct{})
							return files
						}()

						// Compile the modified files using dependency graph
						compileModifiedFilesWithDependencies(ctx, compiler, depGraph, filesToCompile, verbose)
					})
				}()
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return errors.New("watcher error channel closed")
			}
			compileWatchLog.Printf("Watcher error: %v", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Watcher error: %v", err)))
			}

		case <-ctx.Done():
			if verbose {
				fmt.Fprintln(os.Stderr, "\n🛑 Stopping watch mode...")
			}
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return nil
		}
	}
}
