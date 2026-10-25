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
	workflowsDir, markdownFile, err := resolveCompileWatchPaths(markdownFile)
	if err != nil {
		return err
	}
	depGraph := buildCompileWatchDependencyGraph(workflowsDir, compiler, verbose)
	watcher, err := fsnotify.NewBufferedWatcher(100)
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer watcher.Close()
	if err := addCompileWatchDirectories(watcher, workflowsDir); err != nil {
		return err
	}
	printCompileWatchStart(workflowsDir, markdownFile, verbose)
	debounce := &compileWatchDebouncer{modifiedFiles: make(map[string]struct{})}
	compileWatchInitialFiles(ctx, compiler, workflowsDir, markdownFile, verbose)
	return runCompileWatchLoop(ctx, watcher, compiler, depGraph, debounce, markdownFile, verbose)
}

func resolveCompileWatchPaths(markdownFile string) (string, string, error) {
	// Find git root for consistent behavior
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return "", "", fmt.Errorf("watch mode requires being in a git repository: %w", err)
	}

	workflowsDir := filepath.Join(gitRoot, constants.GetWorkflowDir())
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		return "", "", fmt.Errorf("the %s directory does not exist in git root (%s)", constants.GetWorkflowDir(), gitRoot)
	}

	// If a specific file is provided, watch only that file and its directory
	if markdownFile != "" {
		if !filepath.IsAbs(markdownFile) {
			markdownFile = filepath.Join(workflowsDir, markdownFile)
		}
		if _, err := os.Stat(markdownFile); os.IsNotExist(err) {
			return "", "", fmt.Errorf("specified markdown file does not exist: %s", markdownFile)
		}
	}
	return workflowsDir, markdownFile, nil
}

func buildCompileWatchDependencyGraph(workflowsDir string, compiler *workflow.Compiler, verbose bool) *DependencyGraph {
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
	return depGraph
}

func addCompileWatchDirectories(watcher *fsnotify.Watcher, workflowsDir string) error {
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
	return nil
}

func printCompileWatchStart(workflowsDir, markdownFile string, verbose bool) {
	// Always emit the begin pattern for task integration
	if markdownFile != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Watching for file changes to %s...", markdownFile)))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Watching for file changes in %s...", workflowsDir)))
	}

	if verbose {
		fmt.Fprintln(os.Stderr, "Press Ctrl+C to stop watching.")
	}
}

func compileWatchInitialFiles(ctx context.Context, compiler *workflow.Compiler, workflowsDir, markdownFile string, verbose bool) {
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
}

func runCompileWatchLoop(ctx context.Context, watcher *fsnotify.Watcher, compiler *workflow.Compiler, depGraph *DependencyGraph, debounce *compileWatchDebouncer, markdownFile string, verbose bool) error {
	// Main watch loop
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return errors.New("watcher channel closed")
			}

			handleCompileWatchEvent(ctx, event, compiler, depGraph, debounce, markdownFile, verbose)

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
			if debounce.timer != nil {
				debounce.timer.Stop()
			}
			return nil
		}
	}
}

func handleCompileWatchEvent(ctx context.Context, event fsnotify.Event, compiler *workflow.Compiler, depGraph *DependencyGraph, debounce *compileWatchDebouncer, markdownFile string, verbose bool) {
	if event.Has(fsnotify.Chmod) || !strings.HasSuffix(event.Name, ".md") {
		return
	}
	if markdownFile != "" && event.Name != markdownFile {
		return
	}
	compileWatchLog.Printf("Detected change: %s (%s)", event.Name, event.Op.String())
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Detected change: %s (%s)", event.Name, event.Op.String())))
	}
	switch {
	case event.Has(fsnotify.Remove):
		handleFileDeleted(event.Name, verbose)
		depGraph.RemoveWorkflow(event.Name)
	case event.Has(fsnotify.Write) || event.Has(fsnotify.Create):
		debounce.queue(event.Name, ctx, compiler, depGraph, verbose)
	}
}

type compileWatchDebouncer struct {
	mu            sync.Mutex
	timer         *time.Timer
	modifiedFiles map[string]struct{}
}

func (d *compileWatchDebouncer) queue(name string, ctx context.Context, compiler *workflow.Compiler, depGraph *DependencyGraph, verbose bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.modifiedFiles[name] = struct{}{}
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(300*time.Millisecond, func() {
		filesToCompile := d.takeModifiedFiles()
		compileModifiedFilesWithDependencies(ctx, compiler, depGraph, filesToCompile, verbose)
	})
}

func (d *compileWatchDebouncer) takeModifiedFiles() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	files := make([]string, 0, len(d.modifiedFiles))
	for file := range d.modifiedFiles {
		files = append(files, file)
	}
	d.modifiedFiles = make(map[string]struct{})
	return files
}
