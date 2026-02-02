// This file provides Python package extraction for agentic workflows.
//
// # Python Package Extraction
//
// This file extracts Python package names from workflow configurations using pip and uv
// package managers. Extraction functions parse commands and configuration to identify
// packages that will be installed at runtime.
//
// # Extraction Functions
//
//   - extractPipPackages() - Extracts pip packages from workflow configuration
//   - extractPipFromCommands() - Extracts pip packages from command strings
//   - extractUvPackages() - Extracts uv packages from workflow configuration
//   - extractUvFromCommands() - Extracts uv packages from command strings
//
// # When to Add Extraction Here
//
// Add extraction to this file when:
//   - It parses Python/pip ecosystem package names
//   - It identifies packages from shell commands
//   - It extracts packages from workflow steps
//   - It detects uv package manager usage
//
// For package validation functions, see pip_validation.go.
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var pipLog = logger.New("workflow:pip")

// extractPipPackages extracts pip package names from workflow data
func extractPipPackages(workflowData *WorkflowData) []string {
	pipLog.Print("Extracting pip packages from workflow data")
	packages := collectPackagesFromWorkflow(workflowData, extractPipFromCommands, "")
	pipLog.Printf("Extracted %d pip packages", len(packages))
	return packages
}

// extractPipFromCommands extracts pip package names from command strings
func extractPipFromCommands(commands string) []string {
	extractor := PackageExtractor{
		CommandNames:       []string{"pip", "pip3"},
		RequiredSubcommand: "install",
		TrimSuffixes:       "&|;",
	}
	return extractor.ExtractPackages(commands)
}

// extractUvPackages extracts uv package names from workflow data
func extractUvPackages(workflowData *WorkflowData) []string {
	pipLog.Print("Extracting uv packages from workflow data")
	packages := collectPackagesFromWorkflow(workflowData, extractUvFromCommands, "")
	pipLog.Printf("Extracted %d uv packages", len(packages))
	return packages
}

// extractUvFromCommands extracts uv package names from command strings
func extractUvFromCommands(commands string) []string {
	pipLog.Printf("Extracting uv packages from commands: line_count=%d", len(strings.Split(commands, "\n")))
	var packages []string
	lines := strings.Split(commands, "\n")

	uvxExtractor := PackageExtractor{
		CommandNames:       []string{"uvx"},
		RequiredSubcommand: "",
		TrimSuffixes:       "&|;",
	}

	uvPipHelper := PackageExtractor{TrimSuffixes: "&|;"}

	for _, line := range lines {
		words := strings.Fields(line)
		for i, word := range words {
			// Check for "uvx <package>" pattern
			if word == "uvx" && i+1 < len(words) {
				pkg := uvxExtractor.FindPackageName(words, i+1)
				if pkg != "" {
					packages = append(packages, pkg)
				}
			} else if word == "uv" && i+2 < len(words) && words[i+1] == "pip" {
				// Check for "uv pip install <package>" pattern
				for j := i + 2; j < len(words); j++ {
					if words[j] == "install" {
						pkg := uvPipHelper.FindPackageName(words, j+1)
						if pkg != "" {
							packages = append(packages, pkg)
						}
						break
					}
				}
			}
		}
	}

	return packages
}
