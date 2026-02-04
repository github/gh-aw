package workflow

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var orchestratorEngineLog = logger.New("workflow:compiler_orchestrator_engine")

// engineSetupResult holds the results of engine configuration and validation
type engineSetupResult struct {
	engineSetting      string
	engineConfig       *EngineConfig
	agenticEngine      CodingAgentEngine
	networkPermissions *NetworkPermissions
	sandboxConfig      *SandboxConfig
	importsResult      *parser.ImportsResult
}

// setupEngineAndImports configures the AI engine, processes imports, and validates network/sandbox settings.
// This function handles:
// - Engine extraction and validation
// - Import processing and merging
// - Network permissions setup
// - Sandbox configuration
// - Strict mode validations
func (c *Compiler) setupEngineAndImports(result *parser.FrontmatterResult, cleanPath string, content []byte, markdownDir string) (*engineSetupResult, error) {
	orchestratorEngineLog.Printf("Setting up engine and processing imports")

	// Extract AI engine setting from frontmatter
	engineSetting, engineConfig := c.ExtractEngineConfig(result.Frontmatter)

	// Extract network permissions from frontmatter
	networkPermissions := c.extractNetworkPermissions(result.Frontmatter)

	// Default to 'defaults' ecosystem if no network permissions specified
	if networkPermissions == nil {
		networkPermissions = &NetworkPermissions{
			Allowed: []string{"defaults"},
		}
	}

	// Extract sandbox configuration from frontmatter
	sandboxConfig := c.extractSandboxConfig(result.Frontmatter)

	// Save the initial strict mode state to restore it after this workflow is processed
	// This ensures that strict mode from one workflow doesn't affect other workflows
	initialStrictMode := c.strictMode

	// Check strict mode in frontmatter
	// Priority: CLI flag > frontmatter > schema default (true)
	if !c.strictMode {
		// CLI flag not set, check frontmatter
		if strictValue, exists := result.Frontmatter["strict"]; exists {
			// Frontmatter explicitly sets strict mode
			if strictBool, ok := strictValue.(bool); ok {
				c.strictMode = strictBool
			}
		} else {
			// Neither CLI nor frontmatter set - use schema default (true)
			c.strictMode = true
		}
	}

	// Perform strict mode validations
	orchestratorEngineLog.Printf("Performing strict mode validation (strict=%v)", c.strictMode)
	if err := c.validateStrictMode(result.Frontmatter, networkPermissions); err != nil {
		orchestratorEngineLog.Printf("Strict mode validation failed: %v", err)
		// Restore strict mode before returning error
		c.strictMode = initialStrictMode
		return nil, err
	}

	// Restore the initial strict mode state after validation
	// This ensures strict mode doesn't leak to other workflows being compiled
	c.strictMode = initialStrictMode

	// Override with command line AI engine setting if provided
	if c.engineOverride != "" {
		originalEngineSetting := engineSetting
		if originalEngineSetting != "" && originalEngineSetting != c.engineOverride {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Command line --engine %s overrides markdown file engine: %s", c.engineOverride, originalEngineSetting)))
			c.IncrementWarningCount()
		}
		engineSetting = c.engineOverride
	}

	// Process imports from frontmatter first (before @include directives)
	orchestratorEngineLog.Printf("Processing imports from frontmatter")
	importCache := c.getSharedImportCache()
	// Pass the full file content for accurate line/column error reporting
	importsResult, err := parser.ProcessImportsFromFrontmatterWithSource(result.Frontmatter, markdownDir, importCache, cleanPath, string(content))
	if err != nil {
		orchestratorEngineLog.Printf("Import processing failed: %v", err)
		return nil, err // Error is already formatted with source location
	}

	// Merge network permissions from imports with top-level network permissions
	if importsResult.MergedNetwork != "" {
		orchestratorEngineLog.Printf("Merging network permissions from imports")
		networkPermissions, err = c.MergeNetworkPermissions(networkPermissions, importsResult.MergedNetwork)
		if err != nil {
			orchestratorEngineLog.Printf("Network permissions merge failed: %v", err)
			return nil, fmt.Errorf("failed to merge network permissions: %w", err)
		}
	}

	// Validate permissions from imports against top-level permissions
	// Extract top-level permissions first
	topLevelPermissions := c.extractPermissions(result.Frontmatter)
	if importsResult.MergedPermissions != "" {
		orchestratorEngineLog.Printf("Validating included permissions")
		if err := c.ValidateIncludedPermissions(topLevelPermissions, importsResult.MergedPermissions); err != nil {
			orchestratorEngineLog.Printf("Included permissions validation failed: %v", err)
			return nil, fmt.Errorf("permission validation failed: %w", err)
		}
	}

	// Process @include directives to extract engine configurations and check for conflicts
	orchestratorEngineLog.Printf("Expanding includes for engine configurations")
	includedEngines, err := parser.ExpandIncludesForEngines(result.Markdown, markdownDir)
	if err != nil {
		orchestratorEngineLog.Printf("Failed to expand includes for engines: %v", err)
		return nil, fmt.Errorf("failed to expand includes for engines: %w", err)
	}

	// Combine imported engines with included engines
	allEngines := append(importsResult.MergedEngines, includedEngines...)

	// Validate that only one engine field exists across all files
	orchestratorEngineLog.Printf("Validating single engine specification")
	finalEngineSetting, err := c.validateSingleEngineSpecification(engineSetting, allEngines)
	if err != nil {
		orchestratorEngineLog.Printf("Engine specification validation failed: %v", err)
		return nil, err
	}
	if finalEngineSetting != "" {
		engineSetting = finalEngineSetting
	}

	// If engineConfig is nil (engine was in an included file), extract it from the included engine JSON
	if engineConfig == nil && len(allEngines) > 0 {
		orchestratorEngineLog.Printf("Extracting engine config from included file")
		extractedConfig, err := c.extractEngineConfigFromJSON(allEngines[0])
		if err != nil {
			orchestratorEngineLog.Printf("Failed to extract engine config: %v", err)
			return nil, fmt.Errorf("failed to extract engine config from included file: %w", err)
		}
		engineConfig = extractedConfig
	}

	// Apply the default AI engine setting if not specified
	if engineSetting == "" {
		defaultEngine := c.engineRegistry.GetDefaultEngine()
		engineSetting = defaultEngine.GetID()
		log.Printf("No 'engine:' setting found, defaulting to: %s", engineSetting)
		// Create a default EngineConfig with the default engine ID if not already set
		if engineConfig == nil {
			engineConfig = &EngineConfig{ID: engineSetting}
		} else if engineConfig.ID == "" {
			engineConfig.ID = engineSetting
		}
	}

	// Validate the engine setting
	orchestratorEngineLog.Printf("Validating engine setting: %s", engineSetting)
	if err := c.validateEngine(engineSetting); err != nil {
		orchestratorEngineLog.Printf("Engine validation failed: %v", err)
		return nil, err
	}

	// Get the agentic engine instance
	agenticEngine, err := c.getAgenticEngine(engineSetting)
	if err != nil {
		orchestratorEngineLog.Printf("Failed to get agentic engine: %v", err)
		return nil, err
	}

	log.Printf("AI engine: %s (%s)", agenticEngine.GetDisplayName(), engineSetting)
	if agenticEngine.IsExperimental() && c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Using experimental engine: %s", agenticEngine.GetDisplayName())))
		c.IncrementWarningCount()
	}

	// Enable firewall by default for copilot engine when network restrictions are present
	// (unless SRT sandbox is configured, since AWF and SRT are mutually exclusive)
	enableFirewallByDefaultForCopilot(engineSetting, networkPermissions, sandboxConfig)

	// Enable firewall by default for claude engine when network restrictions are present
	enableFirewallByDefaultForClaude(engineSetting, networkPermissions, sandboxConfig)

	// Re-evaluate strict mode for firewall and network validation
	// (it was restored after validateStrictMode but we need it again)
	initialStrictModeForFirewall := c.strictMode
	if !c.strictMode {
		// CLI flag not set, check frontmatter
		if strictValue, exists := result.Frontmatter["strict"]; exists {
			// Frontmatter explicitly sets strict mode
			if strictBool, ok := strictValue.(bool); ok {
				c.strictMode = strictBool
			}
		} else {
			// Neither CLI nor frontmatter set - use schema default (true)
			c.strictMode = true
		}
	}

	// Validate firewall is enabled in strict mode for copilot with network restrictions
	orchestratorEngineLog.Printf("Validating strict firewall (strict=%v)", c.strictMode)
	if err := c.validateStrictFirewall(engineSetting, networkPermissions, sandboxConfig); err != nil {
		orchestratorEngineLog.Printf("Strict firewall validation failed: %v", err)
		c.strictMode = initialStrictModeForFirewall
		return nil, err
	}

	// Check if the engine supports network restrictions when they are defined
	if err := c.checkNetworkSupport(agenticEngine, networkPermissions); err != nil {
		orchestratorEngineLog.Printf("Network support check failed: %v", err)
		// Restore strict mode before returning error
		c.strictMode = initialStrictModeForFirewall
		return nil, err
	}

	// Validate that imported custom engine steps don't use agentic engine secrets
	orchestratorEngineLog.Printf("Validating imported steps for agentic secrets (strict=%v)", c.strictMode)
	if err := c.validateImportedStepsNoAgenticSecrets(engineConfig, engineSetting); err != nil {
		orchestratorEngineLog.Printf("Imported steps validation failed: %v", err)
		// Restore strict mode before returning error
		c.strictMode = initialStrictModeForFirewall
		return nil, err
	}

	// Restore the strict mode state after network check
	c.strictMode = initialStrictModeForFirewall

	return &engineSetupResult{
		engineSetting:      engineSetting,
		engineConfig:       engineConfig,
		agenticEngine:      agenticEngine,
		networkPermissions: networkPermissions,
		sandboxConfig:      sandboxConfig,
		importsResult:      importsResult,
	}, nil
}
