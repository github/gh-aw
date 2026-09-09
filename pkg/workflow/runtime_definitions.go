package workflow

import (
	"sync"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var runtimeDefLog = logger.New("workflow:runtime_definitions")

// Runtime represents configuration for a runtime environment
type Runtime struct {
	ID                string            // Unique identifier (e.g., "node", "python")
	Name              string            // Display name (e.g., "Node.js", "Python")
	ActionRepo        string            // GitHub Actions repository (e.g., "actions/setup-node")
	ActionVersion     string            // Action version (e.g., "v4", without @ prefix)
	VersionField      string            // Field name for version in action (e.g., "node-version")
	DefaultVersion    string            // Default version to use
	Commands          []string          // Commands that indicate this runtime is needed
	ExtraWithFields   map[string]string // Additional 'with' fields for the action
	ManifestFiles     []string          // Package manifest file names for this runtime (matched by filename, no path)
	UnverifiedCreator bool              // True if the action creator is not GitHub-verified; triggers zizmor suppression
}

// RuntimeRequirement represents a detected runtime requirement
type RuntimeRequirement struct {
	Runtime     *Runtime
	Version     string         // Empty string means use default
	ExtraFields map[string]any // Additional 'with' fields from user's setup step (e.g., cache settings)
	GoModFile   string         // Path to go.mod file for Go runtime (Go-specific)
	IfCondition string         // Optional GitHub Actions if condition
	Cooldown    bool           // If false, disables default dependency cooldown behavior for installs associated with this runtime
}

// knownRuntimes is the list of all supported runtime configurations (alphabetically sorted by ID)
var knownRuntimes = []*Runtime{
	{
		ID:             "bun",
		Name:           "Bun",
		ActionRepo:     "oven-sh/setup-bun",
		ActionVersion:  "v2",
		VersionField:   "bun-version",
		DefaultVersion: string(constants.DefaultBunVersion),
		Commands:       []string{"bun", "bunx"},
		ManifestFiles:  []string{"package.json", "bun.lockb", "bunfig.toml"},
	},
	{
		ID:             "deno",
		Name:           "Deno",
		ActionRepo:     "denoland/setup-deno",
		ActionVersion:  "v2",
		VersionField:   "deno-version",
		DefaultVersion: string(constants.DefaultDenoVersion),
		Commands:       []string{"deno"},
		ManifestFiles:  []string{"deno.json", "deno.jsonc", "deno.lock"},
	},
	{
		ID:             "dotnet",
		Name:           ".NET",
		ActionRepo:     "actions/setup-dotnet",
		ActionVersion:  "v4",
		VersionField:   "dotnet-version",
		DefaultVersion: string(constants.DefaultDotNetVersion),
		Commands:       []string{"dotnet"},
		ManifestFiles:  []string{"global.json", "NuGet.Config", "Directory.Packages.props"},
	},
	{
		ID:             "elixir",
		Name:           "Elixir",
		ActionRepo:     "erlef/setup-beam",
		ActionVersion:  "v1",
		VersionField:   "elixir-version",
		DefaultVersion: string(constants.DefaultElixirVersion),
		Commands:       []string{"elixir", "mix", "iex"},
		ExtraWithFields: map[string]string{
			"otp-version": "27",
		},
		ManifestFiles:     []string{"mix.exs", "mix.lock"},
		UnverifiedCreator: true, // erlef org is not GitHub-verified on the Marketplace
	},
	{
		ID:             "go",
		Name:           "Go",
		ActionRepo:     "actions/setup-go",
		ActionVersion:  "v5",
		VersionField:   "go-version",
		DefaultVersion: string(constants.DefaultGoVersion),
		Commands:       []string{"go"},
		ExtraWithFields: map[string]string{
			"cache": "false", // Disable caching to prevent cache poisoning in agentic workflows
		},
		ManifestFiles: []string{"go.mod", "go.sum"},
	},
	{
		ID:            "gh-aw",
		Name:          "gh-aw CLI",
		ActionRepo:    "github/gh-aw-actions/setup-cli",
		ActionVersion: "", // Derived at compile time from the compiler version; see runtime_step_generator.go
		VersionField:  "version",
		// Default version is computed at generation time from the current gh-aw build.
		DefaultVersion: "",
		Commands:       []string{"gh-aw"},
		ManifestFiles:  nil,
	},
	{
		ID:             "haskell",
		Name:           "Haskell",
		ActionRepo:     "haskell-actions/setup",
		ActionVersion:  "v2",
		VersionField:   "ghc-version",
		DefaultVersion: string(constants.DefaultHaskellVersion),
		Commands:       []string{"ghc", "ghci", "cabal", "stack"},
		ManifestFiles:  []string{"stack.yaml", "stack.yaml.lock"},
	},
	{
		ID:             "java",
		Name:           "Java",
		ActionRepo:     "actions/setup-java",
		ActionVersion:  "v4",
		VersionField:   "java-version",
		DefaultVersion: string(constants.DefaultJavaVersion),
		Commands:       []string{"java", "javac", "mvn", "gradle"},
		ExtraWithFields: map[string]string{
			"distribution": "temurin",
		},
		ManifestFiles: []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradle.properties"},
	},
	{
		ID:             "node",
		Name:           "Node.js",
		ActionRepo:     "actions/setup-node",
		ActionVersion:  "v6",
		VersionField:   "node-version",
		DefaultVersion: string(constants.DefaultNodeVersion),
		Commands:       []string{"node", "npm", "npx", "yarn", "pnpm"},
		ExtraWithFields: map[string]string{
			"package-manager-cache": "false", // Disable caching by default to prevent cache poisoning in release workflows
		},
		ManifestFiles: []string{"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "npm-shrinkwrap.json"},
	},
	{
		ID:             "python",
		Name:           "Python",
		ActionRepo:     "actions/setup-python",
		ActionVersion:  "v5",
		VersionField:   "python-version",
		DefaultVersion: string(constants.DefaultPythonVersion),
		Commands:       []string{"python", "python3", "pip", "pip3"},
		ManifestFiles:  []string{"requirements.txt", "Pipfile", "Pipfile.lock", "pyproject.toml", "setup.py", "setup.cfg"},
	},
	{
		ID:             "ruby",
		Name:           "Ruby",
		ActionRepo:     "ruby/setup-ruby",
		ActionVersion:  "v1",
		VersionField:   "ruby-version",
		DefaultVersion: string(constants.DefaultRubyVersion),
		Commands:       []string{"ruby", "gem", "bundle"},
		ManifestFiles:  []string{"Gemfile", "Gemfile.lock"},
	},
	{
		ID:                "uv",
		Name:              "uv",
		ActionRepo:        "astral-sh/setup-uv",
		ActionVersion:     "v5",
		VersionField:      "version",
		DefaultVersion:    "", // Uses latest
		Commands:          []string{"uv", "uvx"},
		ManifestFiles:     []string{"pyproject.toml", "uv.lock"},
		UnverifiedCreator: true, // astral-sh org is not GitHub-verified on the Marketplace
	},
}

// commandToRuntime maps command patterns to runtime configurations
var commandToRuntime map[string]*Runtime

// actionRepoToRuntime maps action repository names to runtime configurations
var actionRepoToRuntime map[string]*Runtime

func init() {
	runtimeDefLog.Printf("Initializing runtime definitions: total_runtimes=%d", len(knownRuntimes))

	// Build the command to runtime mapping
	commandToRuntime = make(map[string]*Runtime)
	for _, runtime := range knownRuntimes {
		for _, cmd := range runtime.Commands {
			commandToRuntime[cmd] = runtime
		}
	}
	runtimeDefLog.Printf("Built command to runtime mapping: total_commands=%d", len(commandToRuntime))

	// Build the action repo to runtime mapping
	actionRepoToRuntime = make(map[string]*Runtime)
	for _, runtime := range knownRuntimes {
		actionRepoToRuntime[runtime.ActionRepo] = runtime
	}
	runtimeDefLog.Printf("Built action repo to runtime mapping: total_actions=%d", len(actionRepoToRuntime))
}

// securityConfigFiles are repository security configuration files that are
// always protected by filename regardless of their location in the repository.
// These complement the path-prefix protection (e.g. ".github/") and ensure
// that files placed at the repo root or in "docs/" are equally protected.
var securityConfigFiles = []string{
	"CODEOWNERS",         // Governs required reviewers; valid at repo root, .github/, or docs/
	"DESIGN.md",          // Captures design-system source of truth consumed by coding agents
	"README.md",          // Primary documentation file often imported by agents as context
	"CONTRIBUTING.md",    // Contribution guidelines; modifying could mislead contributors or agents
	"SECURITY.md",        // Security policy; tampering could suppress vulnerability disclosure
	"CODE_OF_CONDUCT.md", // Community conduct policy
	"CHANGELOG.md",       // Release history; changes should receive human scrutiny
}

// allManifestFilesBaseCache caches the base (no-extra) result of getAllManifestFiles.
// knownRuntimes and securityConfigFiles are package-level constants, so this slice is
// identical on every call when no extra files are provided.
var (
	allManifestFilesBaseOnce  sync.Once
	allManifestFilesBaseCache []string
)

// buildBaseManifestFiles computes the deduplicated union of manifest files from all
// known runtimes plus the security config files. It is the shared computation used by
// both the cached (no-extra) and uncached (with-extra) paths of getAllManifestFiles.
func buildBaseManifestFiles() []string {
	var files []string
	for _, runtime := range knownRuntimes {
		files = append(files, runtime.ManifestFiles...)
	}
	return append(files, securityConfigFiles...)
}

// getAllManifestFiles returns the deduplicated union of all manifest file names
// across all known runtimes, plus repository security configuration files, plus
// any additionally-provided filenames.
// These are matched by basename only (no path comparison).
func getAllManifestFiles(extra ...string) []string {
	if len(extra) == 0 {
		allManifestFilesBaseOnce.Do(func() {
			allManifestFilesBaseCache = sliceutil.MergeUnique(buildBaseManifestFiles())
		})
		result := make([]string, len(allManifestFilesBaseCache))
		copy(result, allManifestFilesBaseCache)
		return result
	}
	return sliceutil.MergeUnique(buildBaseManifestFiles(), extra...)
}

// getProtectedPathPrefixes returns non-dot path prefixes (relative to repo root)
// whose contents are always protected regardless of file basename.
//
// Dot-folder prefixes (e.g. ".github/", ".agents/", ".githooks/", ".husky/")
// are NOT included here because they are already covered by the general
// top-level dot-folder protection rule (protect_top_level_dot_folders).
// Only non-dot path prefixes need to be listed explicitly.
// Any dot-prefix entries in `extra` are also dropped for the same reason.
func getProtectedPathPrefixes(extra ...string) []string {
	var nonDot []string
	for _, p := range extra {
		if len(p) < 2 || p[0] != '.' {
			nonDot = append(nonDot, p)
		}
	}
	return sliceutil.MergeUnique([]string(nil), nonDot...)
}

// getDotFolderExcludes returns the subset of excludeFiles that are top-level
// dot-folder path prefixes (i.e. start with "." and end with "/").
// These are used at compile time to tell the runtime handler which specific
// dot-folders have been opted out of the general top-level-dot-folder protection.
func getDotFolderExcludes(excludeFiles []string) []string {
	var result []string
	for _, f := range excludeFiles {
		// Must start with ".", end with "/", and have at least one char between
		// them (e.g. ".agents/" is valid; "./" is not).
		if len(f) > 2 && f[0] == '.' && f[len(f)-1] == '/' {
			result = append(result, f)
		}
	}
	return result
}

// findRuntimeByID finds a runtime configuration by its ID
func findRuntimeByID(id string) *Runtime {
	runtimeDefLog.Printf("Finding runtime by ID: %s", id)
	for _, runtime := range knownRuntimes {
		if runtime.ID == id {
			runtimeDefLog.Printf("Found runtime: %s (%s)", runtime.ID, runtime.Name)
			return runtime
		}
	}
	runtimeDefLog.Printf("Runtime not found: %s", id)
	return nil
}
