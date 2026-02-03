// This file provides JavaScript bundling for agentic workflows.
//
// # JavaScript Bundler with Runtime Mode Support
//
// The bundler supports two runtime environments:
//
// 1. GitHub Script Mode (RuntimeModeGitHubScript)
//   - Used for JavaScript embedded in GitHub Actions YAML via actions/github-script
//   - No module system available (no require() or module.exports at runtime)
//   - All local requires must be bundled inline
//   - All module.exports statements are removed
//   - Validation ensures no local requires or module references remain
//
// 2. Node.js Mode (RuntimeModeNodeJS)
//   - Used for standalone Node.js scripts that run on filesystem
//   - Full CommonJS module system available
//   - module.exports statements are preserved
//   - Local requires can remain if modules are available on filesystem
//   - Less aggressive bundling and validation
//
// # Usage
//
// For GitHub Script mode (default for backward compatibility):
//
//	bundled, err := BundleJavaScriptFromSources(mainContent, sources, "")
//	// or explicitly:
//	bundled, err := BundleJavaScriptWithMode(mainContent, sources, "", RuntimeModeGitHubScript)
//
// For Node.js mode:
//
//	bundled, err := BundleJavaScriptWithMode(mainContent, sources, "", RuntimeModeNodeJS)
//
// # Guardrails and Validation
//
// The bundler includes several guardrails based on runtime mode:
//
// - validateNoLocalRequires: Ensures all local requires (./... or ../...) are bundled (GitHub Script mode only)
// - validateNoModuleReferences: Ensures no module.exports or exports.* remain (GitHub Script mode only)
// - removeExports: Strips module.exports from bundled code (GitHub Script mode only)
//
// These validations prevent runtime errors when JavaScript is executed in environments
// without a module system.

package workflow

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var bundlerLog = logger.New("workflow:bundler")

// RuntimeMode represents the JavaScript runtime environment
type RuntimeMode int

const (
	// RuntimeModeGitHubScript indicates JavaScript running in actions/github-script
	// In this mode:
	// - All local requires must be bundled (no module system)
	// - module.exports statements must be removed
	// - No module object references allowed
	RuntimeModeGitHubScript RuntimeMode = iota

	// RuntimeModeNodeJS indicates JavaScript running as a Node.js script
	// In this mode:
	// - module.exports can be preserved
	// - Local requires can be kept if modules are available on filesystem
	// - Full Node.js module system is available
	RuntimeModeNodeJS
)

// String returns a string representation of the RuntimeMode
func (r RuntimeMode) String() string {
	switch r {
	case RuntimeModeGitHubScript:
		return "github-script"
	case RuntimeModeNodeJS:
		return "nodejs"
	default:
		return "unknown"
	}
}

// BundleJavaScriptFromSources bundles JavaScript from in-memory sources
// sources is a map where keys are file paths (e.g., "sanitize.cjs") and values are the content
// mainContent is the main JavaScript content that may contain require() calls
// basePath is the base directory path for resolving relative imports (e.g., "js")
//
// DEPRECATED: Use BundleJavaScriptWithMode instead to specify runtime mode explicitly.
// This function defaults to RuntimeModeGitHubScript for backward compatibility.
//
// Migration guide:
//   - For GitHub Script action (inline in YAML): use BundleJavaScriptWithMode(content, sources, basePath, RuntimeModeGitHubScript)
//   - For Node.js scripts (filesystem-based): use BundleJavaScriptWithMode(content, sources, basePath, RuntimeModeNodeJS)
//
// This function will be maintained for backward compatibility but new code should use BundleJavaScriptWithMode.
func BundleJavaScriptFromSources(mainContent string, sources map[string]string, basePath string) (string, error) {
	return BundleJavaScriptWithMode(mainContent, sources, basePath, RuntimeModeGitHubScript)
}

// BundleJavaScriptWithMode bundles JavaScript from in-memory sources with specified runtime mode
// sources is a map where keys are file paths (e.g., "sanitize.cjs") and values are the content
// mainContent is the main JavaScript content that may contain require() calls
// basePath is the base directory path for resolving relative imports (e.g., "js")
// mode specifies the target runtime environment (GitHub script action vs Node.js)
func BundleJavaScriptWithMode(mainContent string, sources map[string]string, basePath string, mode RuntimeMode) (string, error) {
	bundlerLog.Printf("Bundling JavaScript: source_count=%d, base_path=%s, main_content_size=%d bytes, runtime_mode=%s",
		len(sources), basePath, len(mainContent), mode)

	// Validate that no runtime mode mixing occurs
	if err := validateNoRuntimeMixing(mainContent, sources, mode); err != nil {
		bundlerLog.Printf("Runtime mode validation failed: %v", err)
		return "", err
	}

	// Track already processed files to avoid circular dependencies
	processed := make(map[string]bool)

	// Bundle the main content recursively
	bundled, err := bundleFromSources(mainContent, basePath, sources, processed, mode)
	if err != nil {
		bundlerLog.Printf("Bundling failed: %v", err)
		return "", err
	}

	// Deduplicate require statements (keep only the first occurrence)
	bundled = deduplicateRequires(bundled)

	// Mode-specific processing and validations
	switch mode {
	case RuntimeModeGitHubScript:
		// GitHub Script mode: remove module.exports from final output
		bundled = removeExports(bundled)

		// Inject await main() call for inline execution
		// This allows scripts to export main when used with require(), but still execute
		// when inlined directly in github-script action
		if strings.Contains(bundled, "async function main()") || strings.Contains(bundled, "async function main ()") {
			bundled = bundled + "\nawait main();\n"
			bundlerLog.Print("Injected 'await main()' call for GitHub Script inline execution")
		}

		// Validate all local requires are bundled and module references removed
		if err := validateNoLocalRequires(bundled); err != nil {
			bundlerLog.Printf("Validation failed: %v", err)
			return "", err
		}
		if err := validateNoModuleReferences(bundled); err != nil {
			bundlerLog.Printf("Module reference validation failed: %v", err)
			return "", err
		}

	case RuntimeModeNodeJS:
		// Node.js mode: more permissive, allows module.exports and may allow local requires
		// Local requires are OK if modules will be available on filesystem
		bundlerLog.Print("Node.js mode: module.exports preserved, local requires allowed")
		// Note: We still bundle what we can, but don't fail on remaining requires
	}

	// Log size information about the bundled output
	lines := strings.Split(bundled, "\n")
	var maxLineLength int
	for _, line := range lines {
		if len(line) > maxLineLength {
			maxLineLength = len(line)
		}
	}

	bundlerLog.Printf("Bundling completed: processed_files=%d, output_size=%d bytes, output_lines=%d, max_line_length=%d chars",
		len(processed), len(bundled), len(lines), maxLineLength)
	return bundled, nil
}

// bundleFromSources processes content and recursively bundles its dependencies from the sources map
// The mode parameter controls how module.exports statements are handled
func bundleFromSources(content string, currentPath string, sources map[string]string, processed map[string]bool, mode RuntimeMode) (string, error) {
	bundlerLog.Printf("Processing file for bundling: current_path=%s, content_size=%d bytes, runtime_mode=%s", currentPath, len(content), mode)

	// Regular expression to match require('./...') or require("./...")
	// This matches both single-line and multi-line destructuring:
	// const { x } = require("./file.cjs");
	// const {
	//   x,
	//   y
	// } = require("./file.cjs");
	// Captures the require path where it starts with ./ or ../
	requireRegex := regexp.MustCompile(`(?s)(?:const|let|var)\s+(?:\{[^}]*\}|\w+)\s*=\s*require\(['"](\.\.?/[^'"]+)['"]\);?`)

	// Find all requires and their positions
	matches := requireRegex.FindAllStringSubmatchIndex(content, -1)

	if len(matches) == 0 {
		bundlerLog.Print("No requires found in content")
		// No requires found, return content as-is
		return content, nil
	}

	bundlerLog.Printf("Found %d require statements to process", len(matches))

	var result strings.Builder
	lastEnd := 0

	for _, match := range matches {
		// match[0], match[1] are the start and end of the full match
		// match[2], match[3] are the start and end of the captured group (the path)
		matchStart := match[0]
		matchEnd := match[1]
		pathStart := match[2]
		pathEnd := match[3]

		// Write content before this require
		result.WriteString(content[lastEnd:matchStart])

		// Extract the require path
		requirePath := content[pathStart:pathEnd]

		// Resolve the full path relative to current path
		var fullPath string
		if currentPath == "" {
			fullPath = requirePath
		} else {
			fullPath = filepath.Join(currentPath, requirePath)
		}

		// Ensure .cjs extension
		if !strings.HasSuffix(fullPath, ".cjs") && !strings.HasSuffix(fullPath, ".js") {
			fullPath += ".cjs"
		}

		// Normalize the path (clean up ./ and ../)
		fullPath = filepath.Clean(fullPath)

		// Convert Windows path separators to forward slashes for consistency
		fullPath = filepath.ToSlash(fullPath)

		// Check if we've already processed this file
		if processed[fullPath] {
			bundlerLog.Printf("Skipping already processed file: %s", fullPath)
			// Skip - already inlined
			result.WriteString("// Already inlined: " + requirePath + "\n")
		} else {
			// Mark as processed
			processed[fullPath] = true

			// Look up the required file in sources
			requiredContent, ok := sources[fullPath]
			if !ok {
				bundlerLog.Printf("Required file not found in sources: %s", fullPath)
				return "", fmt.Errorf("required file not found in sources: %s", fullPath)
			}

			bundlerLog.Printf("Inlining file: %s (size: %d bytes)", fullPath, len(requiredContent))

			// Recursively bundle the required file
			requiredDir := filepath.Dir(fullPath)
			bundledRequired, err := bundleFromSources(requiredContent, requiredDir, sources, processed, mode)
			if err != nil {
				return "", err
			}

			// Remove exports from the bundled content based on runtime mode
			var cleanedRequired string
			if mode == RuntimeModeGitHubScript {
				// GitHub Script mode: remove all module.exports
				cleanedRequired = removeExports(bundledRequired)
				bundlerLog.Printf("Processed %s (github-script mode): original_size=%d, after_export_removal=%d",
					fullPath, len(bundledRequired), len(cleanedRequired))
			} else {
				// Node.js mode: preserve module.exports
				cleanedRequired = bundledRequired
				bundlerLog.Printf("Processed %s (nodejs mode): size=%d, module.exports preserved",
					fullPath, len(bundledRequired))
			}

			// Add a comment indicating the inlined file
			fmt.Fprintf(&result, "// === Inlined from %s ===\n", requirePath)
			result.WriteString(cleanedRequired)
			fmt.Fprintf(&result, "// === End of %s ===\n", requirePath)
		}

		lastEnd = matchEnd
	}

	// Write any remaining content after the last require
	result.WriteString(content[lastEnd:])

	return result.String(), nil
}

// removeExports removes module.exports and exports statements from JavaScript code
// This function removes ALL exports, including conditional ones, because GitHub Script
// mode does not support any form of module.exports
func removeExports(content string) string {
	lines := strings.Split(content, "\n")
	var result strings.Builder

	// Regular expressions for export patterns
	moduleExportsRegex := regexp.MustCompile(`^\s*module\.exports\s*=`)
	exportsRegex := regexp.MustCompile(`^\s*exports\.\w+\s*=`)

	// Pattern for inline conditional exports like:
	// ("undefined" != typeof module && module.exports && (module.exports = {...}),
	// This pattern is used by minified code
	inlineConditionalExportRegex := regexp.MustCompile(`\(\s*["']undefined["']\s*!=\s*typeof\s+module\s*&&\s*module\.exports`)

	// Track if we're inside a conditional export block that should be removed
	inConditionalExport := false
	conditionalDepth := 0

	// Track if we're inside an unconditional module.exports block
	inModuleExports := false
	moduleExportsDepth := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for inline conditional export pattern (minified style)
		// These lines should be entirely removed as they only contain the conditional export
		if inlineConditionalExportRegex.MatchString(trimmed) {
			// Skip the entire line - it's an inline conditional export
			continue
		}

		// Check if this starts a conditional export block
		// Pattern: if (typeof module !== "undefined" && module.exports) {
		// These need to be REMOVED for GitHub Script mode
		if strings.Contains(trimmed, "if") &&
			strings.Contains(trimmed, "module") &&
			strings.Contains(trimmed, "exports") &&
			strings.Contains(trimmed, "{") {
			inConditionalExport = true
			conditionalDepth = 1
			// Skip this line - we're removing conditional exports for GitHub Script mode
			continue
		}

		// Track braces if we're in a conditional export - skip all lines until it closes
		if inConditionalExport {
			for _, ch := range trimmed {
				if ch == '{' {
					conditionalDepth++
				} else if ch == '}' {
					conditionalDepth--
					if conditionalDepth == 0 {
						inConditionalExport = false
						// Skip this closing line and continue
						continue
					}
				}
			}
			// Skip all lines inside the conditional export block
			continue
		}

		// Check if this line starts an unconditional module.exports assignment
		if moduleExportsRegex.MatchString(line) {
			// Check if it's a multi-line object export (ends with {)
			if strings.Contains(trimmed, "{") && !strings.Contains(trimmed, "}") {
				// This is a multi-line module.exports = { ... }
				inModuleExports = true
				moduleExportsDepth = 1
				// Skip this line and start tracking the export block
				continue
			} else {
				// Single-line export, skip just this line
				continue
			}
		}

		// Track braces if we're in an unconditional module.exports block
		if inModuleExports {
			// Count braces to track when the export block ends
			for _, ch := range trimmed {
				if ch == '{' {
					moduleExportsDepth++
				} else if ch == '}' {
					moduleExportsDepth--
					if moduleExportsDepth == 0 {
						inModuleExports = false
						// Skip this closing line and continue
						continue
					}
				}
			}
			// Skip all lines inside the export block
			continue
		}

		// Skip lines that are unconditional exports.* assignments
		if exportsRegex.MatchString(line) {
			// Skip this line - it's an unconditional export
			continue
		}

		result.WriteString(line)
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}

// deduplicateRequires removes duplicate require() statements from bundled JavaScript
// For destructured imports from the same module, it merges them into a single require statement
// keeping only the first occurrence of each unique require for non-destructured imports.
// IMPORTANT: Only merges requires that have the same indentation level to avoid moving
// requires across scope boundaries (which would cause "X is not defined" errors)
func deduplicateRequires(content string) string {
	lines := strings.Split(content, "\n")

	// Helper to get indentation level of a line
	getIndentation := func(line string) int {
		count := 0
		for _, ch := range line {
			//nolint:staticcheck // switch would require label for break; if-else is clearer here
			if ch == ' ' {
				count++
			} else if ch == '\t' {
				count += 2 // Treat tab as 2 spaces for comparison
			} else {
				break
			}
		}
		return count
	}

	// Track module imports per indentation level: map[indent]map[moduleName][]names
	moduleImportsByIndent := make(map[int]map[string][]string)
	// Track which lines are require statements to skip during first pass
	requireLines := make(map[int]bool)
	// Track order of first appearance of each module per indentation: map[indent][]moduleName
	moduleOrderByIndent := make(map[int][]string)
	// Track the first line number where we see a require at each indentation
	firstRequireLineByIndent := make(map[int]int)

	// Regular expression to match destructured require statements
	// Matches: const/let/var { name1, name2 } = require('module');
	destructuredRequireRegex := regexp.MustCompile(`^\s*(?:const|let|var)\s+\{\s*([^}]+)\s*\}\s*=\s*require\(['"]([^'"]+)['"]\);?\s*$`)
	// Regular expression to match non-destructured require statements
	// Matches: const/let/var name = require('module');
	simpleRequireRegex := regexp.MustCompile(`^\s*(?:const|let|var)\s+(\w+)\s*=\s*require\(['"]([^'"]+)['"]\);?\s*$`)

	// First pass: collect all require statements grouped by indentation level
	for i, line := range lines {
		indent := getIndentation(line)

		// Try destructured require first
		destructuredMatches := destructuredRequireRegex.FindStringSubmatch(line)
		if len(destructuredMatches) > 2 {
			moduleName := destructuredMatches[2]
			destructuredNames := destructuredMatches[1]

			requireLines[i] = true

			// Initialize map for this indentation level if needed
			if moduleImportsByIndent[indent] == nil {
				moduleImportsByIndent[indent] = make(map[string][]string)
				firstRequireLineByIndent[indent] = i
			}

			// Parse the destructured names (split by comma and trim whitespace)
			names := strings.Split(destructuredNames, ",")
			for _, name := range names {
				name = strings.TrimSpace(name)
				if name != "" {
					moduleImportsByIndent[indent][moduleName] = append(moduleImportsByIndent[indent][moduleName], name)
				}
			}

			// Track order of first appearance at this indentation
			if len(moduleImportsByIndent[indent][moduleName]) == len(names) {
				moduleOrderByIndent[indent] = append(moduleOrderByIndent[indent], moduleName)
			}
			continue
		}

		// Try simple require
		simpleMatches := simpleRequireRegex.FindStringSubmatch(line)
		if len(simpleMatches) > 2 {
			moduleName := simpleMatches[2]
			varName := simpleMatches[1]

			requireLines[i] = true

			// Initialize map for this indentation level if needed
			if moduleImportsByIndent[indent] == nil {
				moduleImportsByIndent[indent] = make(map[string][]string)
				firstRequireLineByIndent[indent] = i
			}

			// For simple requires, store the variable name with a marker
			if _, exists := moduleImportsByIndent[indent][moduleName]; !exists {
				moduleOrderByIndent[indent] = append(moduleOrderByIndent[indent], moduleName)
			}
			moduleImportsByIndent[indent][moduleName] = append(moduleImportsByIndent[indent][moduleName], "VAR:"+varName)
		}
	}

	// Second pass: write output
	var result strings.Builder
	// Track which indentation levels have had their merged requires written
	wroteRequiresByIndent := make(map[int]bool)

	for i, line := range lines {
		indent := getIndentation(line)

		// Skip original require lines, we'll write merged ones at the first require position for each indent level
		if requireLines[i] {
			// Check if this is the first require at this indentation level
			if firstRequireLineByIndent[indent] == i && !wroteRequiresByIndent[indent] {
				// Write all merged require statements for this indentation level
				moduleImports := moduleImportsByIndent[indent]
				moduleOrder := moduleOrderByIndent[indent]

				indentStr := strings.Repeat(" ", indent)

				for _, moduleName := range moduleOrder {
					imports := moduleImports[moduleName]
					if len(imports) == 0 {
						continue
					}

					// Separate VAR: prefixed (simple requires) from destructured imports
					var varNames []string
					var destructuredNames []string
					for _, imp := range imports {
						if strings.HasPrefix(imp, "VAR:") {
							varNames = append(varNames, strings.TrimPrefix(imp, "VAR:"))
						} else {
							destructuredNames = append(destructuredNames, imp)
						}
					}

					// Deduplicate variable names for simple requires
					if len(varNames) > 0 {
						seen := make(map[string]bool)
						var uniqueVarNames []string
						for _, varName := range varNames {
							if !seen[varName] {
								seen[varName] = true
								uniqueVarNames = append(uniqueVarNames, varName)
							}
						}

						// Write simple require(s) - use the first unique variable name
						if len(uniqueVarNames) > 0 {
							varName := uniqueVarNames[0]
							fmt.Fprintf(&result, "%sconst %s = require(\"%s\");\n", indentStr, varName, moduleName)
							bundlerLog.Printf("Keeping simple require: %s at indent %d", moduleName, indent)
						}
					}

					// Handle destructured imports
					if len(destructuredNames) > 0 {
						// Remove duplicates while preserving order
						seen := make(map[string]bool)
						var uniqueImports []string
						for _, imp := range destructuredNames {
							if !seen[imp] {
								seen[imp] = true
								uniqueImports = append(uniqueImports, imp)
							}
						}

						fmt.Fprintf(&result, "%sconst { %s } = require(\"%s\");\n",
							indentStr, strings.Join(uniqueImports, ", "), moduleName)
						bundlerLog.Printf("Merged destructured require for %s at indent %d: %v", moduleName, indent, uniqueImports)
					}
				}
				wroteRequiresByIndent[indent] = true
			}
			// Skip this require line (it's been merged or will be merged)
			continue
		}

		// Keep non-require lines
		result.WriteString(line)
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}
