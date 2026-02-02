// This file provides NPM package extraction utilities for agentic workflows.
//
// # NPM Package Extraction
//
// This file provides utilities to extract NPM package names from workflow data
// for packages used with npx (Node Package Execute). The extracted packages
// can be validated by the validation functions in validation.go.
//
// # Extraction Functions
//
//   - extractNpxPackages() - Extracts npm packages used with npx launcher
//   - extractNpxFromCommands() - Parses command strings to find npx packages
//
// For package validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var npmLog = logger.New("workflow:npm")

// extractNpxPackages extracts npx package names from workflow data
func extractNpxPackages(workflowData *WorkflowData) []string {
	npmLog.Print("Extracting npx packages from workflow data")
	packages := collectPackagesFromWorkflow(workflowData, extractNpxFromCommands, "npx")
	npmLog.Printf("Extracted %d npx packages", len(packages))
	return packages
}

// extractNpxFromCommands extracts npx package names from command strings
func extractNpxFromCommands(commands string) []string {
	extractor := PackageExtractor{
		CommandNames:       []string{"npx"},
		RequiredSubcommand: "",
		TrimSuffixes:       "&|;",
	}
	return extractor.ExtractPackages(commands)
}
