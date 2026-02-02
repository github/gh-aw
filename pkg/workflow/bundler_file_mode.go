// This file provides JavaScript bundling for agentic workflows.
//
// # File Mode Bundler
//
// This file implements a file-based bundling mode for GitHub Script actions that writes
// JavaScript files to disk instead of inlining them in YAML. This approach maximizes
// reuse of helper modules within the same job.
//
// # How it works
//
// 1. CollectScriptFiles - Recursively collects all JavaScript files used by a script
// 2. GenerateWriteScriptsStep - Creates a step that writes all files to /opt/gh-aw/scripts/
// 3. GenerateRequireScript - Converts a script to require from the local filesystem
//
// # Benefits
//
//   - Reduces YAML size by avoiding duplicate inlined code
//   - Maximizes reuse of helper modules within the same job
//   - Makes debugging easier (files exist on disk during execution)
//   - Reduces memory pressure from large bundled strings

package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var fileModeLog = logger.New("workflow:bundler_file_mode")

// ScriptsBasePath is the directory where JavaScript files are written at runtime
// This must match SetupActionDestination since files are copied there by the setup action
const ScriptsBasePath = "/opt/gh-aw/actions"

// SetupActionDestination is the directory where the setup action writes activation scripts
const SetupActionDestination = "/opt/gh-aw/actions"

// ScriptFile represents a JavaScript file to be written to disk
type ScriptFile struct {
	// Path is the relative path within ScriptsBasePath (e.g., "create_issue.cjs")
	Path string
	// Content is the JavaScript content to write
	Content string
	// Hash is a short hash of the content for cache invalidation
	Hash string
}

// ScriptFilesResult contains the collected script files and metadata
type ScriptFilesResult struct {
	// Files is the list of files to write, deduplicated and sorted
	Files []ScriptFile
	// MainScriptPath is the path to the main entry point script
	MainScriptPath string
	// TotalSize is the total size of all files in bytes
	TotalSize int
}

// CollectScriptFiles recursively collects all JavaScript files used by a script.
// It starts from the main script and follows all local require() statements.
// Top-level await patterns (like `await main();`) are patched to work in CommonJS.
//
// Parameters:
//   - scriptName: Name of the main script (e.g., "create_issue")
//   - mainContent: The main script content
//   - sources: Map of all available JavaScript sources (from GetJavaScriptSources())
//
// Returns a ScriptFilesResult with all files needed, or an error if a required file is missing.
//
// Note: This includes the main script in the output. Use CollectScriptDependencies if you
// only want the dependencies (for when the main script is inlined in github-script).
func CollectScriptFiles(scriptName string, mainContent string, sources map[string]string) (*ScriptFilesResult, error) {
	fileModeLog.Printf("Collecting script files for: %s (%d bytes)", scriptName, len(mainContent))

	// Track collected files and avoid duplicates
	collected := make(map[string]*ScriptFile)
	processed := make(map[string]bool)

	// The main script path
	mainPath := scriptName + ".cjs"

	// Patch top-level await patterns to work in CommonJS
	patchedContent := patchTopLevelAwaitForFileMode(mainContent)

	// Add the main script first
	hash := computeShortHash(patchedContent)
	collected[mainPath] = &ScriptFile{
		Path:    mainPath,
		Content: patchedContent,
		Hash:    hash,
	}
	processed[mainPath] = true

	// Recursively collect dependencies
	if err := collectDependencies(mainContent, "", sources, collected, processed); err != nil {
		return nil, err
	}

	// Convert to sorted slice for deterministic output
	var files []ScriptFile
	totalSize := 0
	for _, file := range collected {
		files = append(files, *file)
		totalSize += len(file.Content)
	}

	// Sort by path for consistent output
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	fileModeLog.Printf("Collected %d files, total size: %d bytes", len(files), totalSize)

	return &ScriptFilesResult{
		Files:          files,
		MainScriptPath: mainPath,
		TotalSize:      totalSize,
	}, nil
}

// CollectScriptDependencies collects only the dependencies of a script (not the main script itself).
// This is used when the main script is inlined in github-script but its dependencies
// need to be written to disk.
//
// Parameters:
//   - scriptName: Name of the main script (e.g., "create_issue")
//   - mainContent: The main script content
//   - sources: Map of all available JavaScript sources (from GetJavaScriptSources())
//
// Returns a ScriptFilesResult with only the dependency files, or an error if a required file is missing.
func CollectScriptDependencies(scriptName string, mainContent string, sources map[string]string) (*ScriptFilesResult, error) {
	fileModeLog.Printf("Collecting dependencies for: %s (%d bytes)", scriptName, len(mainContent))

	// Track collected files and avoid duplicates
	collected := make(map[string]*ScriptFile)
	processed := make(map[string]bool)

	// Mark the main script as processed so we don't include it
	mainPath := scriptName + ".cjs"
	processed[mainPath] = true

	// Recursively collect dependencies (but not the main script)
	if err := collectDependencies(mainContent, "", sources, collected, processed); err != nil {
		return nil, err
	}

	// Convert to sorted slice for deterministic output
	var files []ScriptFile
	totalSize := 0
	for _, file := range collected {
		files = append(files, *file)
		totalSize += len(file.Content)
	}

	// Sort by path for consistent output
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	fileModeLog.Printf("Collected %d dependency files, total size: %d bytes", len(files), totalSize)

	return &ScriptFilesResult{
		Files:          files,
		MainScriptPath: mainPath,
		TotalSize:      totalSize,
	}, nil
}

// collectDependencies recursively collects all files required by the given content
func collectDependencies(content string, currentDir string, sources map[string]string, collected map[string]*ScriptFile, processed map[string]bool) error {
	// Regular expression to match require('./...') or require("./...")
	requireRegex := regexp.MustCompile(`require\(['"](\.\.?/[^'"]+)['"]\)`)

	matches := requireRegex.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) <= 1 {
			continue
		}

		requirePath := match[1]

		// Resolve the full path
		var fullPath string
		if currentDir == "" {
			fullPath = requirePath
		} else {
			fullPath = filepath.Join(currentDir, requirePath)
		}

		// Ensure .cjs extension
		if !strings.HasSuffix(fullPath, ".cjs") && !strings.HasSuffix(fullPath, ".js") {
			fullPath += ".cjs"
		}

		// Normalize the path
		fullPath = filepath.Clean(fullPath)
		fullPath = filepath.ToSlash(fullPath)

		// Skip if already processed
		if processed[fullPath] {
			continue
		}
		processed[fullPath] = true

		// Look up in sources
		requiredContent, ok := sources[fullPath]
		if !ok {
			return fmt.Errorf("required file not found in sources: %s", fullPath)
		}

		// Add to collected
		hash := computeShortHash(requiredContent)
		collected[fullPath] = &ScriptFile{
			Path:    fullPath,
			Content: requiredContent,
			Hash:    hash,
		}

		fileModeLog.Printf("Collected dependency: %s (%d bytes)", fullPath, len(requiredContent))

		// Recursively process this file's dependencies
		requiredDir := filepath.Dir(fullPath)
		if err := collectDependencies(requiredContent, requiredDir, sources, collected, processed); err != nil {
			return err
		}
	}

	return nil
}

// computeShortHash computes a short SHA256 hash of the content (first 8 characters)
func computeShortHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])[:8]
}

// patchTopLevelAwaitForFileMode wraps top-level `await main();` calls in an async IIFE.
// CommonJS modules don't support top-level await, so we need to wrap it.
//
// This transforms:
//
//	await main();
//
// Into:
//
//	(async () => { await main(); })();
func patchTopLevelAwaitForFileMode(content string) string {
	// Match `await main();` at the end of the file (with optional whitespace/newlines)
	// This pattern is used in safe output scripts as the entry point
	awaitMainRegex := regexp.MustCompile(`(?m)^await\s+main\s*\(\s*\)\s*;?\s*$`)

	return awaitMainRegex.ReplaceAllString(content, "(async () => { await main(); })();")
}

// GenerateWriteScriptsStep generates the YAML for a step that writes all collected
// JavaScript files to /opt/gh-aw/scripts/. This step should be added once at the
// beginning of the safe_outputs job.
//
// The generated step uses a heredoc to write each file efficiently.
func GenerateWriteScriptsStep(files []ScriptFile) []string {
	if len(files) == 0 {
		return nil
	}

	fileModeLog.Printf("Generating write scripts step for %d files", len(files))

	var steps []string

	steps = append(steps, "      - name: Setup JavaScript files\n")
	steps = append(steps, "        id: setup_scripts\n")
	steps = append(steps, "        shell: bash\n")
	steps = append(steps, "        run: |\n")
	steps = append(steps, fmt.Sprintf("          mkdir -p %s\n", ScriptsBasePath))

	// Write each file using cat with heredoc
	for _, file := range files {
		filePath := fmt.Sprintf("%s/%s", ScriptsBasePath, file.Path)

		// Ensure parent directory exists
		dir := filepath.Dir(filePath)
		if dir != ScriptsBasePath {
			steps = append(steps, fmt.Sprintf("          mkdir -p %s\n", dir))
		}

		// Use base64 encoding to handle special characters safely
		// This is more reliable than heredoc for arbitrary JavaScript content
		steps = append(steps, fmt.Sprintf("          cat > %s << 'EOF_%s'\n", filePath, file.Hash))

		// Write content line by line
		lines := strings.Split(file.Content, "\n")
		for _, line := range lines {
			steps = append(steps, fmt.Sprintf("          %s\n", line))
		}

		steps = append(steps, fmt.Sprintf("          EOF_%s\n", file.Hash))
	}

	return steps
}

// GenerateRequireScript generates the JavaScript code that requires the main script
// from the filesystem instead of inlining the bundled code.
//
// For GitHub Script mode, the script is wrapped in an async IIFE to support
// top-level await patterns used in the JavaScript files (e.g., `await main();`).
// The globals (github, context, core, exec, io) are automatically available
// in the GitHub Script execution context.
func GenerateRequireScript(mainScriptPath string) string {
	fullPath := fmt.Sprintf("%s/%s", ScriptsBasePath, mainScriptPath)
	// Wrap in async IIFE to support top-level await in the required module
	return fmt.Sprintf(`(async () => { await require('%s'); })();`, fullPath)
}

// GitHubScriptGlobalsPreamble is JavaScript code that exposes the github-script
// built-in objects (github, context, core, exec, io) on the global JavaScript object.
// This allows required modules to access these globals via globalThis.
const GitHubScriptGlobalsPreamble = `// Expose github-script globals to required modules
globalThis.github = github;
globalThis.context = context;
globalThis.core = core;
globalThis.exec = exec;
globalThis.io = io;

`

// GetInlinedScriptForFileMode gets the main script content and transforms it for inlining
// in the github-script action while using file mode for dependencies.
//
// This function:
// 1. Adds a preamble to expose github-script globals (github, context, core, exec, io) on globalThis
// 2. Gets the script content from the registry
// 3. Transforms relative require() calls to absolute paths (e.g., './helper.cjs' -> '/opt/gh-aw/scripts/helper.cjs')
// 4. Patches top-level await patterns to work in the execution context
//
// This is different from GenerateRequireScript which just generates a require() call.
// Inlining the main script is necessary because:
// - require() runs in a separate module context without the GitHub Script globals
// - The main script needs access to github, context, core, etc. in its top-level scope
//
// Dependencies are still loaded from files using require() and can access the globals
// via globalThis (e.g., globalThis.github, globalThis.core).
func GetInlinedScriptForFileMode(scriptName string) (string, error) {
	// Get script content from registry
	content := DefaultScriptRegistry.GetSource(scriptName)
	if content == "" {
		return "", fmt.Errorf("script not found in registry: %s", scriptName)
	}

	// Transform relative requires to absolute paths pointing to /opt/gh-aw/scripts/
	transformed := TransformRequiresToAbsolutePath(content, ScriptsBasePath)

	// Patch top-level await patterns
	patched := patchTopLevelAwaitForFileMode(transformed)

	// Add preamble to expose globals to required modules
	result := GitHubScriptGlobalsPreamble + patched

	fileModeLog.Printf("Inlined script %s: %d bytes (transformed from %d)", scriptName, len(result), len(content))

	return result, nil
}

// RewriteScriptForFileMode rewrites a script's require statements to use absolute
// paths from /tmp/gh-aw/scripts/ instead of relative paths.
//
// This transforms:
//
//	const { helper } = require('./helper.cjs');
//
// Into:
//
//	const { helper } = require('/opt/gh-aw/scripts/helper.cjs');
func RewriteScriptForFileMode(content string, currentPath string) string {
	// Regular expression to match local require statements
	requireRegex := regexp.MustCompile(`require\(['"](\.\.?/)([^'"]+)['"]\)`)

	return requireRegex.ReplaceAllStringFunc(content, func(match string) string {
		// Extract the path
		submatches := requireRegex.FindStringSubmatch(match)
		if len(submatches) < 3 {
			return match
		}

		relativePrefix := submatches[1]
		requirePath := submatches[2]

		// Resolve the full path
		var fullPath string
		currentDir := filepath.Dir(currentPath)
		switch relativePrefix {
		case "./":
			if currentDir == "." || currentDir == "" {
				fullPath = requirePath
			} else {
				fullPath = filepath.Join(currentDir, requirePath)
			}
		case "../":
			parentDir := filepath.Dir(currentDir)
			fullPath = filepath.Join(parentDir, requirePath)
		}

		// Normalize
		fullPath = filepath.Clean(fullPath)
		fullPath = filepath.ToSlash(fullPath)

		// Return the rewritten require
		return fmt.Sprintf("require('%s/%s')", ScriptsBasePath, fullPath)
	})
}

// TransformRequiresToAbsolutePath rewrites all relative require statements in content
// to use the specified absolute base path.
//
// This transforms:
//
//	const { helper } = require('./helper.cjs');
//
// Into:
//
//	const { helper } = require('/base/path/helper.cjs');
//
// Parameters:
//   - content: The JavaScript content to transform
//   - basePath: The absolute path to use for requires (e.g., "/opt/gh-aw/safeoutputs")
func TransformRequiresToAbsolutePath(content string, basePath string) string {
	// Regular expression to match local require statements
	requireRegex := regexp.MustCompile(`require\(['"](\.\.?/)([^'"]+)['"]\)`)

	return requireRegex.ReplaceAllStringFunc(content, func(match string) string {
		// Extract the path
		submatches := requireRegex.FindStringSubmatch(match)
		if len(submatches) < 3 {
			return match
		}

		requirePath := submatches[2]

		// Return the rewritten require with the base path
		return fmt.Sprintf("require('%s/%s')", basePath, requirePath)
	})
}

// PrepareFilesForFileMode prepares all collected files for file mode by rewriting
// their require statements to use absolute paths.
func PrepareFilesForFileMode(files []ScriptFile) []ScriptFile {
	result := make([]ScriptFile, len(files))
	for i, file := range files {
		rewritten := RewriteScriptForFileMode(file.Content, file.Path)
		result[i] = ScriptFile{
			Path:    file.Path,
			Content: rewritten,
			Hash:    computeShortHash(rewritten),
		}
	}
	return result
}

// CollectAllJobScriptFiles collects all JavaScript files needed by multiple scripts
// in a single job. This deduplicates common helper files across different safe output types.
//
// Parameters:
//   - scriptNames: List of script names to collect (e.g., ["create_issue", "add_comment"])
//   - sources: Map of all available JavaScript sources
//
// Returns a combined ScriptFilesResult with all deduplicated files.
func CollectAllJobScriptFiles(scriptNames []string, sources map[string]string) (*ScriptFilesResult, error) {
	fileModeLog.Printf("Collecting files for %d scripts: %v", len(scriptNames), scriptNames)

	// Track all collected files across all scripts
	allFiles := make(map[string]*ScriptFile)

	for _, name := range scriptNames {
		// Get the script content from the registry
		content := DefaultScriptRegistry.GetSource(name)
		if content == "" {
			fileModeLog.Printf("Script not found in registry: %s, skipping", name)
			continue
		}

		// Collect only this script's dependencies (not the main script itself)
		// The main script is inlined in the github-script action
		result, err := CollectScriptDependencies(name, content, sources)
		if err != nil {
			return nil, fmt.Errorf("failed to collect dependencies for script %s: %w", name, err)
		}

		// Merge into allFiles
		for _, file := range result.Files {
			if existing, ok := allFiles[file.Path]; ok {
				// Already have this file - verify content matches
				if existing.Hash != file.Hash {
					fileModeLog.Printf("WARNING: File %s has different content from different scripts", file.Path)
				}
			} else {
				allFiles[file.Path] = &ScriptFile{
					Path:    file.Path,
					Content: file.Content,
					Hash:    file.Hash,
				}
			}
		}
	}

	// Convert to sorted slice
	var files []ScriptFile
	totalSize := 0
	for _, file := range allFiles {
		files = append(files, *file)
		totalSize += len(file.Content)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	fileModeLog.Printf("Total collected: %d unique dependency files, %d bytes", len(files), totalSize)

	return &ScriptFilesResult{
		Files:     files,
		TotalSize: totalSize,
	}, nil
}
