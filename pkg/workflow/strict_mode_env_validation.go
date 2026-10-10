// This file contains strict mode environment secrets validation functions.
//
// It validates that secrets are not exposed through the env section of workflows
// compiled with the --strict flag, and emits warnings in non-strict mode.

package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
)

// validateEnvSecrets detects secrets in the top-level env section and the engine.env section,
// raising an error in strict mode or a warning in non-strict mode. Secrets in env will be
// leaked to the agent container.
//
// For engine.env, env vars whose key matches a known agentic engine env var (returned by the
// engine's GetRequiredSecretNames) are allowed to carry secrets – this enables users to
// override the engine's default secret with an org-specific one, e.g.
//
//	COPILOT_GITHUB_TOKEN: ${{ secrets.MY_ORG_COPILOT_TOKEN }}
//
// No other engine.env var is allowed to have secrets.
func (c *Compiler) validateEnvSecrets(frontmatter map[string]any) error {
	return c.validateEnvSecretsWithModels(frontmatter, BuiltinModelAliases())
}

func (c *Compiler) validateEarlyEnvSecrets(frontmatter map[string]any) error {
	_, config, _ := c.ExtractEngineConfig(frontmatter)
	if config != nil && len(config.FallbackModels) > 0 {
		// Provider aliases are not available until imports have been merged.
		return c.validateEnvSecretsSection(frontmatter, "env", nil)
	}
	return c.validateEnvSecrets(frontmatter)
}

func (c *Compiler) validateEnvSecretsWithModels(frontmatter map[string]any, models map[string][]string) error {
	// Check top-level env section (no allowed overrides here)
	if err := c.validateEnvSecretsSection(frontmatter, "env", nil); err != nil {
		return err
	}

	// Check engine.env section when engine is in object format
	if engineValue, exists := frontmatter["engine"]; exists {
		if engineObj, ok := engineValue.(map[string]any); ok {
			// Determine which env var keys may carry secrets: those that the engine itself
			// requires (e.g. COPILOT_GITHUB_TOKEN for the copilot engine).
			// The second return value is *EngineConfig (not an error); we only need the engine ID.
			engineSetting, engineConfig, model := c.ExtractEngineConfig(frontmatter)
			allowedEnvVarKeys := c.getEngineBaseEnvVarKeys(engineSetting)
			if engineConfig != nil && len(engineConfig.FallbackModels) > 0 {
				data := &WorkflowData{EngineConfig: engineConfig, Model: model, ModelMappings: models}
				for _, name := range fallbackProviderSecretNames(data) {
					allowedEnvVarKeys[name] = struct{}{}
				}
			}

			if err := c.validateEnvSecretsSection(engineObj, "engine.env", allowedEnvVarKeys); err != nil {
				return err
			}
		}
	}

	return nil
}

// getEngineBaseEnvVarKeys returns the set of env var key names that the named engine
// supports as defined in the AWF specification. These keys are allowed to carry secrets
// in engine.env overrides.
func (c *Compiler) getEngineBaseEnvVarKeys(engineID string) map[string]struct {
} {
	if engineID == "" {
		return nil
	}
	engine, err := c.engineRegistry.GetEngine(engineID)
	if err != nil {
		strictModeValidationLog.Printf("Could not look up engine '%s' for env-key allowlist: %v", engineID, err)
		return nil
	}
	keys := make(map[string]struct {
	})
	for _, name := range engine.GetSupportedEnvVarKeys() {
		keys[name] = struct {
		}{}
	}

	// Also include secrets declared in the AuthDefinition for inline engine definitions.
	// This allows workflows to pass auth secrets through engine.env without triggering the
	// "secret in env" strict-mode check.
	if def := c.engineCatalog.Get(engineID); def != nil && def.Provider.Auth != nil {
		for _, name := range def.Provider.Auth.RequiredSecretNames() {
			strictModeValidationLog.Printf("Adding auth-definition secret key to allowlist: %s", name)
			keys[name] = struct {
			}{}
		}
	}

	return keys
}

// validateEnvSecretsSection checks a single config map's "env" key for secrets.
// sectionName is used in log and error messages (e.g. "env" or "engine.env").
// allowedEnvVarKeys is an optional set of env var key names whose secret values are
// permitted (used for engine.env to allow overriding engine env vars).
func (c *Compiler) validateEnvSecretsSection(config map[string]any, sectionName string, allowedEnvVarKeys map[string]struct {
}) error {
	envValue, exists := config["env"]
	if !exists {
		strictModeValidationLog.Printf("No %s section found, validation passed", sectionName)
		return nil
	}

	// Check if env is a map[string]any
	envMap, ok := envValue.(map[string]any)
	if !ok {
		strictModeValidationLog.Printf("%s section is not a map, skipping validation", sectionName)
		return nil
	}

	// Convert to map[string]string for secret extraction, skipping keys whose secrets
	// are explicitly allowed (e.g. engine env var overrides in engine.env).
	envStrings := make(map[string]string)
	for key, value := range envMap {
		if allowedEnvVarKeys != nil && setutil.Contains(allowedEnvVarKeys, key) {
			strictModeValidationLog.Printf("Skipping allowed engine env var key in %s: %s", sectionName, key)
			continue
		}
		if strValue, ok := value.(string); ok {
			envStrings[key] = strValue
		}
	}

	// Extract secrets from env values
	secrets := ExtractSecretsFromMap(envStrings)
	if len(secrets) == 0 {
		strictModeValidationLog.Printf("No secrets found in %s section", sectionName)
		return nil
	}

	// Build list of secret references found
	var secretRefs []string
	for _, secretExpr := range secrets {
		secretRefs = append(secretRefs, secretExpr)
	}

	strictModeValidationLog.Printf("Found %d secret(s) in %s section: %v", len(secrets), sectionName, secretRefs)

	// In strict mode, this is an error
	if c.strictMode {
		if sectionName == "engine.env" {
			// engine.env secrets are excluded from the agent sandbox via awf --exclude-env
			// (requires AWF v0.25.3+), so they are not leaked, but strict mode still requires
			// engine-specific configuration.
			return fmt.Errorf("strict mode: secrets detected in 'engine.env' section are excluded from the agent sandbox via awf --exclude-env (requires AWF %s+) and are not accessible to the agent when that version is in use. Found: %s. Use engine-specific secret configuration instead. See: https://github.github.com/gh-aw/reference/engines/", constants.AWFExcludeEnvMinVersion, strings.Join(secretRefs, ", "))
		}
		return fmt.Errorf("strict mode: secrets detected in '%s' section will be leaked to the agent container. Found: %s. Use engine-specific secret configuration instead. See: https://github.github.com/gh-aw/reference/engines/", sectionName, strings.Join(secretRefs, ", "))
	}

	// In non-strict mode, emit a warning
	var warningMsg string
	if sectionName == "engine.env" {
		// engine.env secrets are excluded from the agent sandbox via awf --exclude-env
		// (requires AWF v0.25.3+). On older AWF versions this protection is not applied and
		// the values will reach the agent container.
		warningMsg = fmt.Sprintf("Warning: secrets detected in 'engine.env' section will be excluded from the agent sandbox via awf --exclude-env (requires AWF %s+); on older AWF versions the agent process will see these values. Found: %s. Consider using engine-specific secret configuration instead.", constants.AWFExcludeEnvMinVersion, strings.Join(secretRefs, ", "))
	} else {
		warningMsg = fmt.Sprintf("Warning: secrets detected in '%s' section will be leaked to the agent container. Found: %s. Consider using engine-specific secret configuration instead.", sectionName, strings.Join(secretRefs, ", "))
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(warningMsg))
	c.IncrementWarningCount()

	return nil
}
