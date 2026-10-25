package workflow

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var lspManagerLog = logger.New("workflow:lsp_manager")

// LSPServerConfig defines a single language server entry under top-level frontmatter "lsp:".
type LSPServerConfig struct {
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	FileExtensions map[string]string `json:"fileExtensions,omitempty"`
	// Version pins the package version for the language server. When set, it overrides the
	// built-in default version for known LSP servers. Accepts standard semver version strings
	// (e.g. "5.3.0") without a leading "v". Has no effect for custom servers not in the
	// built-in install spec table.
	Version string `json:"version,omitempty"`
}

// LSPManager handles LSP configuration normalization, validation, and generation.
type LSPManager struct {
	servers map[string]LSPServerConfig
}

func NewLSPManager(servers map[string]LSPServerConfig) *LSPManager {
	// Sort keys for deterministic normalization order so that when two keys
	// collapse to the same lowercase value (e.g. "TypeScript" and "typescript"),
	// the lexicographically first original key always wins and the duplicate is
	// logged rather than silently lost.
	keys := make([]string, 0, len(servers))
	for k := range servers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	normalized := make(map[string]LSPServerConfig, len(servers))
	for _, key := range keys {
		language := strings.TrimSpace(strings.ToLower(key))
		if language == "" {
			lspManagerLog.Printf("Skipping invalid LSP language key: %q", key)
			continue
		}
		if _, exists := normalized[language]; exists {
			lspManagerLog.Printf("Duplicate LSP language key %q (normalizes to %q): entry ignored", key, language)
			continue
		}
		config := servers[key]
		config.Command = strings.TrimSpace(config.Command)
		normalized[language] = config
	}
	return &LSPManager{servers: normalized}
}

func (m *LSPManager) HasServers() bool {
	return m != nil && len(m.servers) > 0
}

func (m *LSPManager) Validate() error {
	if !m.HasServers() {
		return nil
	}
	for language, config := range m.servers {
		if config.Command == "" {
			return fmt.Errorf("lsp.%s.command is required", language)
		}
		if len(config.FileExtensions) == 0 {
			return fmt.Errorf("lsp.%s.fileExtensions must define at least one file extension mapping", language)
		}
	}
	return nil
}

func (m *LSPManager) CopilotLSPServers() map[string]LSPServerConfig {
	if !m.HasServers() {
		return nil
	}
	result := make(map[string]LSPServerConfig, len(m.servers))
	maps.Copy(result, m.servers)
	return result
}

// GenerateInstallSteps generates GitHub Actions steps that install the LSP server
// binary dependencies for all configured LSP servers.
//
// For npm-based servers the generated install command respects the workflow's
// runtime-manager settings:
//   - workflowData.RunInstallScripts (runtimes.node.run-install-scripts) controls
//     whether --ignore-scripts is omitted (default: scripts disabled).
//   - resolveRuntimeCooldown (runtimes.node.cooldown) controls whether
//     NPM_CONFIG_MIN_RELEASE_AGE is injected (default: cooldown enabled).
//
// Pass nil for workflowData to get secure defaults (--ignore-scripts, cooldown on).
func (m *LSPManager) GenerateInstallSteps(workflowData *WorkflowData) []GitHubActionStep {
	if !m.HasServers() {
		return nil
	}

	// Determine npm install flags from runtime-manager settings.
	// Defaults match the runtime manager's secure defaults:
	//   - --ignore-scripts ON (supply-chain protection)
	//   - cooldown ON (NPM_CONFIG_MIN_RELEASE_AGE)
	runInstallScripts := false
	cooldownEnabled := true
	if workflowData != nil {
		runInstallScripts = workflowData.RunInstallScripts
		cooldownEnabled = resolveRuntimeCooldown(workflowData, "node")
	}

	langs := make([]string, 0, len(m.servers))
	for language := range m.servers {
		langs = append(langs, language)
	}
	sort.Strings(langs)

	var steps []GitHubActionStep
	for _, language := range langs {
		spec, ok := lspInstallSpecs[language]
		if !ok {
			continue
		}

		// Resolve effective version: frontmatter overrides the spec default.
		// Strip any leading 'v' prefix so both "0.17.0" and "v0.17.0" are handled
		// consistently, avoiding malformed version strings like "@vv0.17.0".
		config := m.servers[language]
		effectiveVersion := strings.TrimPrefix(config.Version, "v")

		var step GitHubActionStep
		if len(spec.NpmPackages) > 0 {
			step = generateNpmLSPInstallStep(spec, effectiveVersion, runInstallScripts, cooldownEnabled)
		} else {
			step = generateNonNpmLSPInstallStep(language, spec, effectiveVersion)
		}
		steps = append(steps, step)
	}

	return steps
}

func generateNpmLSPInstallStep(spec lspInstallSpec, effectiveVersion string, runInstallScripts, cooldownEnabled bool) GitHubActionStep {
	// npm-based LSP server: build install command from runtime-manager settings.
	args := []string{"npm", "install", "-g"}
	if !runInstallScripts {
		args = append(args, "--ignore-scripts")
	}
	// Pin each npm package to its version. The primary (last) package is the
	// LSP server binary itself; its version can be overridden via the frontmatter
	// 'version' field. All other packages use their hardcoded default version.
	primaryPkg := spec.NpmPackages[len(spec.NpmPackages)-1]
	for _, pkg := range spec.NpmPackages {
		ver := spec.NpmPackageVersions[pkg]
		if pkg == primaryPkg && effectiveVersion != "" {
			ver = effectiveVersion
		}
		if ver != "" {
			args = append(args, pkg+"@"+ver)
		} else {
			args = append(args, pkg)
		}
	}
	installCmd := strings.Join(args, " ")
	step := GitHubActionStep{
		"      - name: " + spec.StepName,
		"        run: " + installCmd,
	}
	if cooldownEnabled {
		step = append(step,
			"        env:",
			fmt.Sprintf("          NPM_CONFIG_MIN_RELEASE_AGE: '%d'", npmDefaultCooldownDays),
		)
	}
	step = append(step, "        timeout-minutes: 10")
	return step
}

func generateNonNpmLSPInstallStep(language string, spec lspInstallSpec, effectiveVersion string) GitHubActionStep {
	// Non-npm LSP server (go install, gem install, rustup): build versioned command.
	var installCmd string
	switch language {
	case "go":
		ver := spec.DefaultVersion
		if effectiveVersion != "" {
			ver = effectiveVersion
		}
		if ver != "" {
			installCmd = "go install golang.org/x/tools/gopls@v" + ver
		} else {
			installCmd = "go install golang.org/x/tools/gopls@latest"
		}
	case "ruby":
		ver := spec.DefaultVersion
		if effectiveVersion != "" {
			ver = effectiveVersion
		}
		if ver != "" {
			installCmd = "gem install solargraph -v " + ver
		} else {
			installCmd = "gem install solargraph"
		}
	default:
		installCmd = "rustup component add rust-analyzer"
	}
	return GitHubActionStep{
		"      - name: " + spec.StepName,
		"        run: " + installCmd,
		"        timeout-minutes: 10",
	}
}

// RuntimeRequirements returns the set of runtime requirements for all configured LSP
// servers. These are returned as [RuntimeRequirement] values so that the caller can
// feed them into the standard runtime manager (DetectRuntimeRequirements /
// GenerateRuntimeSetupSteps), which emits properly SHA-pinned setup actions.
//
// Only languages that have a matching entry in knownRuntimes are included; languages
// whose runtime is not tracked by the runtime manager (e.g. "rust") are silently
// skipped — their install commands still appear in GenerateInstallSteps.
//
// Note: Node.js-based LSP servers (bash, php, python, typescript, yaml) map to the
// "node" runtime, but the Copilot engine already sets up Node.js unconditionally via
// BuildNpmEngineInstallStepsWithAWF. Returning "node" here is correct and harmless:
// DetectRuntimeRequirements deduplicates by runtime ID, so at most one Node.js setup
// step is emitted regardless of how many node-based LSP servers are configured.
func (m *LSPManager) RuntimeRequirements() []RuntimeRequirement {
	if !m.HasServers() {
		return nil
	}

	seen := make(map[string]struct{})
	var result []RuntimeRequirement

	langs := make([]string, 0, len(m.servers))
	for language := range m.servers {
		langs = append(langs, language)
	}
	sort.Strings(langs)

	for _, language := range langs {
		spec, ok := lspInstallSpecs[language]
		if !ok || spec.RuntimeID == "" {
			continue
		}
		if _, ok := seen[spec.RuntimeID]; ok {
			continue
		}
		seen[spec.RuntimeID] = struct{}{}
		runtime := findRuntimeByID(spec.RuntimeID)
		if runtime == nil {
			lspManagerLog.Printf("LSP language %q specifies unknown runtime ID %q; skipping runtime requirement", language, spec.RuntimeID)
			continue
		}
		result = append(result, RuntimeRequirement{
			Runtime:  runtime,
			Version:  "",
			Cooldown: true,
		})
	}
	return result
}

type lspInstallSpec struct {
	StepName           string
	NpmPackages        []string          // Non-nil: install these packages globally via npm (respects RunInstallScripts + cooldown)
	NpmPackageVersions map[string]string // Default pinned version for each npm package; key = package name
	DefaultVersion     string            // Default pinned version for non-npm installs (go, gem)
	RuntimeID          string            // runtime manager ID for the runtime needed to run this LSP server
}

var lspInstallSpecs = map[string]lspInstallSpec{
	"bash": {
		StepName:           "Install Bash LSP dependencies",
		NpmPackages:        []string{"bash-language-server"},
		NpmPackageVersions: map[string]string{"bash-language-server": "5.4.0"},
		RuntimeID:          "node",
	},
	"go": {
		StepName:       "Install Go LSP dependencies",
		DefaultVersion: "0.18.1",
		RuntimeID:      "go",
	},
	"php": {
		StepName:           "Install PHP LSP dependencies",
		NpmPackages:        []string{"intelephense"},
		NpmPackageVersions: map[string]string{"intelephense": "1.14.1"},
		RuntimeID:          "node",
	},
	"python": {
		StepName:           "Install Python LSP dependencies",
		NpmPackages:        []string{"pyright"},
		NpmPackageVersions: map[string]string{"pyright": "1.1.399"},
		RuntimeID:          "node",
	},
	"ruby": {
		StepName:       "Install Ruby LSP dependencies",
		DefaultVersion: "0.50.0",
		RuntimeID:      "ruby",
	},
	"rust": {
		StepName:  "Install Rust LSP dependencies",
		RuntimeID: "", // Rust is not in knownRuntimes; runtime setup is done via rustup
	},
	"typescript": {
		StepName:    "Install TypeScript LSP dependencies",
		NpmPackages: []string{"typescript", "typescript-language-server"},
		NpmPackageVersions: map[string]string{
			"typescript":                 "5.8.3",
			"typescript-language-server": "4.3.3",
		},
		RuntimeID: "node",
	},
	"yaml": {
		StepName:           "Install YAML LSP dependencies",
		NpmPackages:        []string{"yaml-language-server"},
		NpmPackageVersions: map[string]string{"yaml-language-server": "1.15.0"},
		RuntimeID:          "node",
	},
}
