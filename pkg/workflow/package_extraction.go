// This file provides generic package extraction utilities for agentic workflows.
//
// # Package Extraction Framework
//
// This file provides a reusable, configurable framework for extracting package names
// from command strings. The PackageExtractor type eliminates code duplication by
// providing a single abstraction that handles different package managers (npm, pip,
// uv, go, etc.) with minimal configuration.
//
// # Purpose and Benefits
//
// The PackageExtractor pattern exists to:
//   - Prevent code duplication across package manager extraction functions
//   - Provide a consistent interface for command parsing
//   - Centralize package name extraction logic
//   - Reduce maintenance burden by having a single implementation
//
// # Usage Pattern
//
// Instead of implementing custom extraction logic, configure a PackageExtractor
// with the appropriate settings for your package manager:
//
//	extractor := PackageExtractor{
//	    CommandNames:       []string{"pip", "pip3"},
//	    RequiredSubcommand: "install",
//	    TrimSuffixes:       "&|;",
//	}
//	packages := extractor.ExtractPackages("pip install requests")
//	// Returns: []string{"requests"}
//
// # Package Manager Examples
//
// NPM (npx):
//
//	extractor := PackageExtractor{
//	    CommandNames:       []string{"npx"},
//	    RequiredSubcommand: "",           // No subcommand needed
//	    TrimSuffixes:       "&|;",
//	}
//	packages := extractor.ExtractPackages("npx @playwright/mcp@latest")
//	// Returns: []string{"@playwright/mcp@latest"}
//
// Python (pip):
//
//	extractor := PackageExtractor{
//	    CommandNames:       []string{"pip", "pip3"},
//	    RequiredSubcommand: "install",    // Must have "install" subcommand
//	    TrimSuffixes:       "&|;",
//	}
//	packages := extractor.ExtractPackages("pip install requests==2.28.0")
//	// Returns: []string{"requests==2.28.0"}
//
// Go (with multiple subcommands):
//
//	extractor := PackageExtractor{
//	    CommandNames:        []string{"go"},
//	    RequiredSubcommands: []string{"install", "get"},
//	    TrimSuffixes:        "&|;",
//	}
//	packages := extractor.ExtractPackages("go install github.com/user/tool@v1.0.0")
//	// Returns: []string{"github.com/user/tool@v1.0.0"}
//
// Python (uv):
//
//	extractor := PackageExtractor{
//	    CommandNames:       []string{"uvx"},
//	    RequiredSubcommand: "",
//	    TrimSuffixes:       "&|;",
//	}
//	packages := extractor.ExtractPackages("uvx black")
//	// Returns: []string{"black"}
//
// # Best Practices
//
//   - ALWAYS use PackageExtractor instead of reimplementing extraction logic
//   - Configure CommandNames for all variations of the command (e.g., ["pip", "pip3"])
//   - Set RequiredSubcommand only when the package manager requires it (e.g., "install" for pip)
//   - Include common shell operators in TrimSuffixes (typically "&|;")
//   - For special cases, use the exported FindPackageName method to reuse logic
//
// # Configuration Details
//
//   - CommandNames: List of command names to match (case-sensitive)
//   - RequiredSubcommand: Subcommand that must appear before the package name
//     (empty string if package comes directly after command)
//   - TrimSuffixes: Characters to remove from the end of package names
//     (useful for shell operators like "&", "|", ";")
//
// For package-specific extraction implementations, see:
//   - npm.go (npx packages)
//   - pip.go (pip and uv packages)
//   - dependabot.go (go packages)
//
// For package validation, see validation.go.

package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("workflow:package_extraction")

// PackageExtractor provides a configurable framework for extracting package names
// from command-line strings. It can be configured to handle different package
// managers (npm, pip, uv, go) by setting the appropriate command names and options.
//
// This type is the core of the package extraction pattern. Use it instead of
// writing custom parsing logic to avoid code duplication.
//
// Configuration:
//   - Set CommandNames to all variants of the command (e.g., ["pip", "pip3"])
//   - Set RequiredSubcommand if the package manager requires a subcommand
//     (e.g., "install" for pip, "get" for go)
//   - Set TrimSuffixes to remove shell operators from package names
//     (typically "&|;")
//
// Examples:
//
//	// For npx (no subcommand):
//	extractor := PackageExtractor{
//	    CommandNames:       []string{"npx"},
//	    RequiredSubcommand: "",
//	    TrimSuffixes:       "&|;",
//	}
//
//	// For pip (with "install" subcommand):
//	extractor := PackageExtractor{
//	    CommandNames:       []string{"pip", "pip3"},
//	    RequiredSubcommand: "install",
//	    TrimSuffixes:       "&|;",
//	}
type PackageExtractor struct {
	// CommandNames is the list of command names to look for.
	// Include all variations of the command (e.g., ["pip", "pip3"]).
	// Matching is case-sensitive and exact.
	//
	// Examples:
	//   - ["npx"] for npm packages
	//   - ["pip", "pip3"] for Python packages
	//   - ["go"] for Go packages
	//   - ["uvx"] for uv tool packages
	CommandNames []string

	// RequiredSubcommand is the subcommand that must follow the command name
	// before the package name appears. Set to empty string if the package name
	// comes directly after the command.
	//
	// Examples:
	//   - "install" for pip (pip install <package>)
	//   - "get" for go (go get <package>)
	//   - "" for npx (npx <package>)
	//
	// Deprecated: Use RequiredSubcommands for multiple subcommand support.
	// This field is maintained for backward compatibility.
	RequiredSubcommand string

	// RequiredSubcommands is a list of subcommands that can follow the command name
	// before the package name appears. If any of these subcommands is found, the
	// package name following it will be extracted. Set to empty slice if the package
	// name comes directly after the command.
	//
	// This field takes precedence over RequiredSubcommand if both are set.
	//
	// Examples:
	//   - ["install"] for pip (pip install <package>)
	//   - ["install", "get"] for go (go install <pkg> or go get <pkg>)
	//   - [] for npx (npx <package>)
	RequiredSubcommands []string

	// TrimSuffixes is a string of characters to trim from the end of package names.
	// This is useful for removing shell operators that may appear after package names
	// in command strings.
	//
	// Recommended value: "&|;" (covers common shell operators)
	//
	// Examples:
	//   - "pip install requests;" → extracts "requests" (trims ";")
	//   - "npx playwright&" → extracts "playwright" (trims "&")
	TrimSuffixes string
}

// ExtractPackages extracts package names from command strings using the configured
// extraction rules. It processes multi-line command strings and returns all found
// package names.
//
// This is the main entry point for package extraction. Call this method with your
// command string(s) after configuring the PackageExtractor.
//
// The extraction process:
//  1. Split commands by newlines
//  2. Split each line into words
//  3. Find command name matches (from CommandNames)
//  4. If RequiredSubcommand is set, look for that subcommand
//  5. Skip flags (words starting with -)
//  6. Extract package name and trim configured suffixes
//  7. Return first package found per command invocation
//
// Multi-line commands are supported:
//
//	commands := `pip install requests
//	pip install numpy`
//	packages := extractor.ExtractPackages(commands)
//	// Returns: []string{"requests", "numpy"}
//
// Flags are automatically skipped:
//
//	packages := extractor.ExtractPackages("pip install --upgrade requests")
//	// Returns: []string{"requests"}
//
// Shell operators are automatically trimmed:
//
//	packages := extractor.ExtractPackages("npx playwright;")
//	// Returns: []string{"playwright"}
//
// Example usage with pip:
//
//	extractor := PackageExtractor{
//	    CommandNames:       []string{"pip", "pip3"},
//	    RequiredSubcommand: "install",
//	    TrimSuffixes:       "&|;",
//	}
//	packages := extractor.ExtractPackages("pip install requests==2.28.0")
//	// Returns: []string{"requests==2.28.0"}
//
// Example usage with npx:
//
//	extractor := PackageExtractor{
//	    CommandNames:       []string{"npx"},
//	    RequiredSubcommand: "",
//	    TrimSuffixes:       "&|;",
//	}
//	packages := extractor.ExtractPackages("npx @playwright/mcp@latest")
//	// Returns: []string{"@playwright/mcp@latest"}
func (pe *PackageExtractor) ExtractPackages(commands string) []string {
	subcommands := pe.getRequiredSubcommands()
	if pkgLog.Enabled() {
		pkgLog.Printf("Extracting packages from commands using %v (subcommands: %v)",
			pe.CommandNames, subcommands)
	}

	var packages []string
	lines := strings.Split(commands, "\n")

	for _, line := range lines {
		words := strings.Fields(line)
		for i, word := range words {
			// Check if this word matches one of our command names
			if !pe.isCommandName(word) {
				continue
			}

			// If we have required subcommands, find any of them first
			if len(subcommands) > 0 {
				pkg := pe.extractWithSubcommands(words, i, subcommands)
				if pkg != "" {
					pkgLog.Printf("Extracted package with subcommand: %s", pkg)
					packages = append(packages, pkg)
				}
			} else {
				// No subcommand required - package comes directly after command
				pkg := pe.extractDirectPackage(words, i)
				if pkg != "" {
					pkgLog.Printf("Extracted direct package: %s", pkg)
					packages = append(packages, pkg)
				}
			}
		}
	}

	pkgLog.Printf("Total packages extracted: %d", len(packages))
	return packages
}

// getRequiredSubcommands returns the list of required subcommands.
// It prefers RequiredSubcommands if set, otherwise falls back to RequiredSubcommand.
func (pe *PackageExtractor) getRequiredSubcommands() []string {
	if len(pe.RequiredSubcommands) > 0 {
		return pe.RequiredSubcommands
	}
	if pe.RequiredSubcommand != "" {
		return []string{pe.RequiredSubcommand}
	}
	return nil
}

// isCommandName checks if the given word matches any of the configured command names
func (pe *PackageExtractor) isCommandName(word string) bool {
	for _, cmdName := range pe.CommandNames {
		if word == cmdName {
			return true
		}
	}
	return false
}

// extractWithSubcommands extracts a package name when any of the required subcommands must be present
// (e.g., "pip install <package>" or "go get <package>")
func (pe *PackageExtractor) extractWithSubcommands(words []string, commandIndex int, subcommands []string) string {
	// Look for any of the required subcommands after the command name
	for j := commandIndex + 1; j < len(words); j++ {
		for _, subcommand := range subcommands {
			if words[j] == subcommand {
				// Found a matching subcommand - now find the package name
				return pe.findPackageName(words, j+1)
			}
		}
	}
	return ""
}

// extractDirectPackage extracts a package name that comes directly after the command
// (e.g., "npx <package>")
func (pe *PackageExtractor) extractDirectPackage(words []string, commandIndex int) string {
	if commandIndex+1 >= len(words) {
		return ""
	}
	return pe.findPackageName(words, commandIndex+1)
}

// findPackageName finds and processes the package name starting at the given index.
// It skips flags (words starting with -) and returns the first non-flag word,
// trimming configured suffixes.
//
// This method is exported to allow special-case extraction patterns (like uv)
// to reuse the package finding logic.
func (pe *PackageExtractor) FindPackageName(words []string, startIndex int) string {
	return pe.findPackageName(words, startIndex)
}

// findPackageName is the internal implementation of FindPackageName
func (pe *PackageExtractor) findPackageName(words []string, startIndex int) string {
	for i := startIndex; i < len(words); i++ {
		pkg := words[i]
		// Skip flags (start with - or --)
		if strings.HasPrefix(pkg, "-") {
			continue
		}
		// Trim configured suffixes (e.g., shell operators)
		if pe.TrimSuffixes != "" {
			pkg = strings.TrimRight(pkg, pe.TrimSuffixes)
		}
		return pkg
	}
	return ""
}
