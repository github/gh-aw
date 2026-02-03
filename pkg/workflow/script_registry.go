// This file provides a ScriptRegistry for managing JavaScript script bundling.
//
// # Script Registry Pattern
//
// The ScriptRegistry eliminates the repetitive sync.Once pattern found throughout
// the codebase for lazy script bundling. Instead of declaring separate variables
// and getter functions for each script, register scripts once and retrieve them
// by name with runtime mode verification.
//
// # Before (repetitive pattern):
//
//	var (
//	    createIssueScript     string
//	    createIssueScriptOnce sync.Once
//	)
//
//	func getCreateIssueScript() string {
//	    createIssueScriptOnce.Do(func() {
//	        sources := GetJavaScriptSources()
//	        bundled, err := BundleJavaScriptFromSources(createIssueScriptSource, sources, "")
//	        if err != nil {
//	            createIssueScript = createIssueScriptSource
//	        } else {
//	            createIssueScript = bundled
//	        }
//	    })
//	    return createIssueScript
//	}
//
// # After (using registry with runtime mode verification):
//
//	// Registration at package init
//	DefaultScriptRegistry.RegisterWithMode("create_issue", createIssueScriptSource, RuntimeModeGitHubScript)
//
//	// Usage anywhere with mode verification
//	script := DefaultScriptRegistry.GetWithMode("create_issue", RuntimeModeGitHubScript)
//
// # Benefits
//
//   - Eliminates ~15 lines of boilerplate per script (variable pair + getter function)
//   - Centralizes bundling logic
//   - Consistent error handling
//   - Thread-safe lazy initialization
//   - Easy to add new scripts
//   - Runtime mode verification prevents mismatches between registration and usage
//
// # Runtime Mode Verification
//
// The GetWithMode() method verifies that the requested runtime mode matches the mode
// the script was registered with. This catches configuration errors at compile time
// rather than at runtime. If there's a mismatch, a warning is logged but the script
// is still returned to avoid breaking workflows.

package workflow

import (
	"fmt"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
)

var registryLog = logger.New("workflow:script_registry")

// scriptEntry holds the source and bundled versions of a script
type scriptEntry struct {
	source     string
	bundled    string
	mode       RuntimeMode // Runtime mode for bundling
	actionPath string      // Optional path to custom action (e.g., "./actions/create-issue")
	once       sync.Once
}

// ScriptRegistry manages lazy bundling of JavaScript scripts.
// It provides a centralized place to register source scripts and retrieve
// bundled versions on-demand with caching.
//
// Thread-safe: All operations use internal synchronization.
//
// Usage:
//
//	registry := NewScriptRegistry()
//	registry.Register("my_script", myScriptSource)
//	bundled := registry.Get("my_script")
type ScriptRegistry struct {
	mu      sync.RWMutex
	scripts map[string]*scriptEntry
}

// NewScriptRegistry creates a new empty script registry.
func NewScriptRegistry() *ScriptRegistry {
	registryLog.Print("Creating new script registry")
	return &ScriptRegistry{
		scripts: make(map[string]*scriptEntry),
	}
}

// Register adds a script source to the registry.
// The script will be bundled lazily on first access via Get().
// Scripts registered this way default to RuntimeModeGitHubScript.
//
// Parameters:
//   - name: Unique identifier for the script (e.g., "create_issue", "add_comment")
//   - source: The raw JavaScript source code (typically from go:embed)
//
// If a script with the same name already exists, it will be overwritten.
// This is useful for testing but should be avoided in production.
//
// Returns an error if validation fails.
func (r *ScriptRegistry) Register(name string, source string) error {
	return r.RegisterWithMode(name, source, RuntimeModeGitHubScript)
}

// RegisterWithMode adds a script source to the registry with a specific runtime mode.
// The script will be bundled lazily on first access via Get().
// Performs compile-time validation to ensure the script follows runtime mode conventions.
//
// Parameters:
//   - name: Unique identifier for the script (e.g., "create_issue", "add_comment")
//   - source: The raw JavaScript source code (typically from go:embed)
//   - mode: Runtime mode for bundling (GitHub Script or Node.js)
//
// If a script with the same name already exists, it will be overwritten.
// This is useful for testing but should be avoided in production.
//
// Compile-time validations:
//   - GitHub Script mode: validates no execSync usage (should use exec instead)
//   - Node.js mode: validates no GitHub Actions globals (core.*, exec.*, github.*)
//
// Returns an error if validation fails, allowing the caller to handle gracefully
// instead of crashing the process.
func (r *ScriptRegistry) RegisterWithMode(name string, source string, mode RuntimeMode) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if registryLog.Enabled() {
		registryLog.Printf("Registering script: %s (%d bytes, mode: %s)", name, len(source), mode)
	}

	// Perform compile-time validation based on runtime mode
	if err := validateNoExecSync(name, source, mode); err != nil {
		return fmt.Errorf("script registration validation failed for %q: %w", name, err)
	}

	if err := validateNoGitHubScriptGlobals(name, source, mode); err != nil {
		return fmt.Errorf("script registration validation failed for %q: %w", name, err)
	}

	r.scripts[name] = &scriptEntry{
		source:     source,
		mode:       mode,
		actionPath: "", // No custom action by default
	}

	return nil
}

// RegisterWithAction registers a script with both inline code and a custom action path.
// This allows the compiler to choose between inline mode (using actions/github-script)
// or custom action mode (using the provided action path).
//
// Parameters:
//   - name: Unique identifier for the script (e.g., "create_issue")
//   - source: The raw JavaScript source code (for inline mode)
//   - mode: Runtime mode for bundling (GitHub Script or Node.js)
//   - actionPath: Path to custom action (e.g., "./actions/create-issue" for development)
//
// The actionPath should be a relative path from the repository root for development mode.
// In the future, this can be extended to support versioned references like
// "github/gh-aw/.github/actions/create-issue@SHA" for release mode.
//
// Returns an error if validation fails, allowing the caller to handle gracefully
// instead of crashing the process.
func (r *ScriptRegistry) RegisterWithAction(name string, source string, mode RuntimeMode, actionPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if registryLog.Enabled() {
		registryLog.Printf("Registering script with action: %s (%d bytes, mode: %s, action: %s)",
			name, len(source), mode, actionPath)
	}

	// Perform compile-time validation based on runtime mode
	if err := validateNoExecSync(name, source, mode); err != nil {
		return fmt.Errorf("script registration validation failed for %q: %w", name, err)
	}

	if err := validateNoGitHubScriptGlobals(name, source, mode); err != nil {
		return fmt.Errorf("script registration validation failed for %q: %w", name, err)
	}

	r.scripts[name] = &scriptEntry{
		source:     source,
		mode:       mode,
		actionPath: actionPath,
	}

	return nil
}

// GetActionPath retrieves the custom action path for a script, if registered.
// Returns an empty string if the script doesn't have a custom action path.
func (r *ScriptRegistry) GetActionPath(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.scripts[name]
	if !exists {
		if registryLog.Enabled() {
			registryLog.Printf("GetActionPath: script not found: %s", name)
		}
		return ""
	}
	if registryLog.Enabled() && entry.actionPath != "" {
		registryLog.Printf("GetActionPath: returning action path for %s: %s", name, entry.actionPath)
	}
	return entry.actionPath
}

// Get retrieves a bundled script by name.
// Bundling is performed lazily on first access and cached for subsequent calls.
//
// If bundling fails, the original source is returned as a fallback.
// If the script is not registered, an empty string is returned.
//
// Thread-safe: Multiple goroutines can call Get concurrently.
//
// DEPRECATED: Use GetWithMode instead to specify the expected runtime mode.
// This allows the compiler to verify the runtime mode matches the registered mode.
func (r *ScriptRegistry) Get(name string) string {
	r.mu.RLock()
	entry, exists := r.scripts[name]
	r.mu.RUnlock()

	if !exists {
		if registryLog.Enabled() {
			registryLog.Printf("Script not found: %s", name)
		}
		return ""
	}

	entry.once.Do(func() {
		if registryLog.Enabled() {
			registryLog.Printf("Bundling script: %s (mode: %s)", name, entry.mode)
		}

		sources := GetJavaScriptSources()
		bundled, err := BundleJavaScriptWithMode(entry.source, sources, "", entry.mode)
		if err != nil {
			registryLog.Printf("Bundling failed for %s, using source as-is: %v", name, err)
			entry.bundled = entry.source
		} else {
			if registryLog.Enabled() {
				registryLog.Printf("Successfully bundled %s: %d bytes", name, len(bundled))
			}
			entry.bundled = bundled
		}
	})

	return entry.bundled
}

// GetWithMode retrieves a bundled script by name with runtime mode verification.
// Bundling is performed lazily on first access and cached for subsequent calls.
//
// The expectedMode parameter allows the compiler to verify that the registered runtime mode
// matches what the caller expects. If there's a mismatch, a warning is logged but the script
// is still returned to avoid breaking existing workflows.
//
// If bundling fails, the original source is returned as a fallback.
// If the script is not registered, an empty string is returned.
//
// Thread-safe: Multiple goroutines can call GetWithMode concurrently.
func (r *ScriptRegistry) GetWithMode(name string, expectedMode RuntimeMode) string {
	r.mu.RLock()
	entry, exists := r.scripts[name]
	r.mu.RUnlock()

	if !exists {
		if registryLog.Enabled() {
			registryLog.Printf("Script not found: %s", name)
		}
		return ""
	}

	// Verify the runtime mode matches what the caller expects
	if entry.mode != expectedMode {
		registryLog.Printf("WARNING: Runtime mode mismatch for script %s: registered as %s but requested as %s",
			name, entry.mode, expectedMode)
	}

	entry.once.Do(func() {
		if registryLog.Enabled() {
			registryLog.Printf("Bundling script: %s (mode: %s)", name, entry.mode)
		}

		sources := GetJavaScriptSources()
		bundled, err := BundleJavaScriptWithMode(entry.source, sources, "", entry.mode)
		if err != nil {
			registryLog.Printf("Bundling failed for %s, using source as-is: %v", name, err)
			entry.bundled = entry.source
		} else {
			if registryLog.Enabled() {
				registryLog.Printf("Successfully bundled %s: %d bytes", name, len(bundled))
			}
			entry.bundled = bundled
		}
	})

	return entry.bundled
}

// GetSource retrieves the original (unbundled) source for a script.
// Useful for testing or when bundling is not needed.
func (r *ScriptRegistry) GetSource(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.scripts[name]
	if !exists {
		return ""
	}
	return entry.source
}

// Has checks if a script is registered in the registry.
func (r *ScriptRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.scripts[name]
	return exists
}

// Names returns a list of all registered script names.
// Useful for debugging and testing.
func (r *ScriptRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.scripts))
	for name := range r.scripts {
		names = append(names, name)
	}
	return names
}

// DefaultScriptRegistry is the global script registry used by the workflow package.
// Scripts are registered during package initialization via init() functions.
var DefaultScriptRegistry = NewScriptRegistry()

// GetScript retrieves a bundled script from the default registry.
// This is a convenience function equivalent to DefaultScriptRegistry.Get(name).
//
// DEPRECATED: Use GetScriptWithMode to specify the expected runtime mode.
func GetScript(name string) string {
	return DefaultScriptRegistry.Get(name)
}

// GetScriptWithMode retrieves a bundled script from the default registry with mode verification.
// This is a convenience function equivalent to DefaultScriptRegistry.GetWithMode(name, mode).
func GetScriptWithMode(name string, mode RuntimeMode) string {
	return DefaultScriptRegistry.GetWithMode(name, mode)
}

// GetAllScriptFilenames returns a sorted list of all .cjs filenames from the JavaScript sources.
// This is used by the build system to discover which files need to be embedded in custom actions.
// The returned list includes all .cjs files found in pkg/workflow/js/, including dependencies.
func GetAllScriptFilenames() []string {
	registryLog.Print("Getting all script filenames from JavaScript sources")
	sources := GetJavaScriptSources()
	filenames := make([]string, 0, len(sources))

	for filename := range sources {
		// Only include .cjs files (exclude .json and other files)
		if strings.HasSuffix(filename, ".cjs") {
			filenames = append(filenames, filename)
		}
	}

	registryLog.Printf("Found %d .cjs files in JavaScript sources", len(filenames))

	// Sort for consistency
	sortedFilenames := make([]string, len(filenames))
	copy(sortedFilenames, filenames)
	// Using a simple sort to avoid importing sort package issues
	for i := 0; i < len(sortedFilenames); i++ {
		for j := i + 1; j < len(sortedFilenames); j++ {
			if sortedFilenames[i] > sortedFilenames[j] {
				sortedFilenames[i], sortedFilenames[j] = sortedFilenames[j], sortedFilenames[i]
			}
		}
	}

	return sortedFilenames
}
