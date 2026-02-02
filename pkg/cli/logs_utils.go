// This file provides command-line interface functionality for gh-aw.
// This file (logs_utils.go) contains utility functions used by the logs command.
//
// Key responsibilities:
//   - Discovering agentic workflow names from .lock.yml files
//   - Utility functions for slice operations

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var logsUtilsLog = logger.New("cli:logs_utils")

// getAgenticWorkflowNames reads all .lock.yml files and extracts their workflow names
func getAgenticWorkflowNames(verbose bool) ([]string, error) {
	logsUtilsLog.Print("Discovering agentic workflow names from .lock.yml files")
	var workflowNames []string

	// Look for .lock.yml files in .github/workflows directory
	workflowsDir := ".github/workflows"
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No .github/workflows directory found"))
		}
		return workflowNames, nil
	}

	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob .lock.yml files: %w", err)
	}

	for _, file := range files {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Reading workflow file: %s", file)))
		}

		content, err := os.ReadFile(file)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read %s: %v", file, err)))
			}
			continue
		}

		// Extract the workflow name using simple string parsing
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "name:") {
				// Parse the name field
				parts := strings.SplitN(trimmed, ":", 2)
				if len(parts) == 2 {
					name := strings.TrimSpace(parts[1])
					// Remove quotes if present
					name = strings.Trim(name, `"'`)
					if name != "" {
						workflowNames = append(workflowNames, name)
						if verbose {
							fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found agentic workflow: %s", name)))
						}
						break
					}
				}
			}
		}
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d agentic workflows", len(workflowNames))))
	}

	return workflowNames, nil
}
