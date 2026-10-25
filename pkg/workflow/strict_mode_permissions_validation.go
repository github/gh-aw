// This file contains strict mode permissions, deprecated fields, and firewall validation functions.
//
// It enforces write permission restrictions, deprecated field checks, and firewall
// requirements for workflows compiled with the --strict flag.

package workflow

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
)

// validateStrictPermissions refuses write permissions in strict mode
func (c *Compiler) validateStrictPermissions(frontmatter map[string]any) error {
	permissionsValue, exists := frontmatter["permissions"]
	if !exists {
		// No permissions specified is fine
		strictModeValidationLog.Printf("No permissions specified, validation passed")
		return nil
	}

	// Parse permissions using the PermissionsParser
	perms := NewPermissionsParserFromValue(permissionsValue)

	// Check for write permissions on sensitive scopes
	writePermissions := []string{"contents", "issues", "pull-requests"}
	for _, scope := range writePermissions {
		if perms.IsAllowed(scope, "write") {
			strictModeValidationLog.Printf("Write permission validation failed: scope=%s", scope)
			return fmt.Errorf("strict mode: write permission '%s: write' is not allowed for security reasons. Use 'safe-outputs.create-issue', 'safe-outputs.create-pull-request', 'safe-outputs.add-comment', or 'safe-outputs.update-issue' to perform write operations safely. See: https://github.github.com/gh-aw/reference/safe-outputs/", scope)
		}
	}

	strictModeValidationLog.Printf("Permissions validation passed")
	return nil
}

// validateStrictDeprecatedFields refuses deprecated fields in strict mode
func (c *Compiler) validateStrictDeprecatedFields(frontmatter map[string]any) error {
	// Get the list of deprecated fields from the schema
	deprecatedFields, err := parser.GetMainWorkflowDeprecatedFields()
	if err != nil {
		strictModeValidationLog.Printf("Failed to get deprecated fields: %v", err)
		// Don't fail compilation if we can't load deprecated fields list
		return nil
	}

	// Check if any deprecated fields are present in the frontmatter
	foundDeprecated := parser.FindDeprecatedFieldsInFrontmatter(frontmatter, deprecatedFields)

	if len(foundDeprecated) > 0 {
		// Build error message with all deprecated fields
		var errorMessages []string
		for _, field := range foundDeprecated {
			message := fmt.Sprintf("Field '%s' is deprecated", field.Name)
			if field.Replacement != "" {
				message += fmt.Sprintf(". Use '%s' instead", field.Replacement)
			}
			errorMessages = append(errorMessages, message)
		}

		strictModeValidationLog.Printf("Deprecated fields found: %v", errorMessages)
		return fmt.Errorf("strict mode: deprecated fields are not allowed. %s", strings.Join(errorMessages, ". "))
	}

	strictModeValidationLog.Printf("No deprecated fields found")
	return nil
}

// validateStrictDisableXPIA refuses use of the disable-xpia-prompt feature flag in strict mode.
// Disabling XPIA (Cross-Prompt Injection Attack) protection removes the primary defense against
// prompt-injection attacks in production workflows.
func (c *Compiler) validateStrictDisableXPIA(frontmatter map[string]any) error {
	featuresValue, exists := frontmatter["features"]
	if !exists {
		return nil
	}
	featuresMap, ok := featuresValue.(map[string]any)
	if !ok {
		return nil
	}
	flagVal, exists := featuresMap["disable-xpia-prompt"]
	if !exists {
		return nil
	}
	// Only reject when the flag is explicitly enabled (true / non-empty string)
	enabled := false
	switch v := flagVal.(type) {
	case bool:
		enabled = v
	case string:
		enabled = v != ""
	}
	if !enabled {
		return nil
	}
	strictModeValidationLog.Printf("disable-xpia-prompt validation failed: feature flag enabled in strict mode")
	return errors.New("strict mode: 'disable-xpia-prompt: true' is not allowed because it removes XPIA (Cross-Prompt Injection Attack) protection from the workflow. This eliminates the primary defense against prompt-injection attacks. Remove the disable-xpia-prompt feature flag or set 'strict: false' to disable strict mode")
}

// validateStrictFirewall requires firewall to be enabled in strict mode for copilot and codex engines
// when network domains are provided (non-wildcard).
// In strict mode, ALL engines (regardless of LLM gateway support) disallow sandbox.agent: false.
// Ecosystem-owned domains that are not specified as ecosystem identifiers emit a warning suggesting
// the identifier form, but truly custom domains are allowed without error.
func (c *Compiler) validateStrictFirewall(engineID string, networkPermissions *NetworkPermissions, sandboxConfig *SandboxConfig) error {
	if !c.strictMode {
		strictModeValidationLog.Printf("Strict mode disabled, skipping firewall validation")
		return nil
	}

	// Check if sandbox.agent: false is set (explicitly disabled)
	sandboxAgentDisabled := sandboxConfig != nil && sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled

	// In strict mode, sandbox.agent: false is not allowed for any engine as it disables the agent sandbox firewall
	if sandboxAgentDisabled {
		strictModeValidationLog.Printf("sandbox.agent: false is set, refusing in strict mode")
		return errors.New("strict mode: 'sandbox.agent: false' is not allowed because it disables the agent sandbox firewall. This removes important security protections. Remove 'sandbox.agent: false' or set 'strict: false' to disable strict mode. See: https://github.github.com/gh-aw/reference/sandbox/")
	}

	// In strict mode, suggest using ecosystem identifiers for domains that belong to known ecosystems
	// This applies regardless of LLM gateway support
	// Both ecosystem domains and truly custom domains are allowed, but we warn about ecosystem domains
	if networkPermissions != nil && len(networkPermissions.Allowed) > 0 {
		suggestStrictNetworkEcosystems(engineID, networkPermissions)
	}

	return validateStrictEngineFirewall(engineID, networkPermissions, sandboxConfig)
}

func suggestStrictNetworkEcosystems(engineID string, networkPermissions *NetworkPermissions) {
	strictModeValidationLog.Printf("Validating network domains in strict mode for all engines")

	// Check if allowed domains contain only known ecosystem identifiers or truly custom domains
	// Track domains that belong to known ecosystems but are not specified as ecosystem identifiers
	type domainSuggestion struct {
		domain    string
		ecosystem string // empty if no ecosystem found, non-empty if domain belongs to known ecosystem
	}
	var ecosystemDomainsNotAsIdentifiers []domainSuggestion

	for _, domain := range networkPermissions.Allowed {
		// Skip wildcards (handled below)
		if domain == "*" {
			continue
		}
		// Check if this is a known ecosystem identifier using a direct map lookup
		// to avoid the allocation, copy, and sort that getEcosystemDomains incurs.
		if isKnownEcosystemIdentifier(domain) {
			// This is a known ecosystem identifier - allowed in strict mode
			strictModeValidationLog.Printf("Domain '%s' is a known ecosystem identifier", domain)
			continue
		}

		// Not an ecosystem identifier - check if it belongs to any ecosystem
		ecosystem := GetDomainEcosystem(domain)
		strictModeValidationLog.Printf("Domain '%s' ecosystem: '%s'", domain, ecosystem)

		if ecosystem != "" {
			// This domain belongs to a known ecosystem but was not specified as an ecosystem identifier
			// In strict mode, we suggest using ecosystem identifiers instead
			ecosystemDomainsNotAsIdentifiers = append(ecosystemDomainsNotAsIdentifiers, domainSuggestion{domain: domain, ecosystem: ecosystem})
		} else {
			// This is a truly custom domain (not part of any known ecosystem) - allowed in strict mode
			strictModeValidationLog.Printf("Domain '%s' is a truly custom domain, allowed in strict mode", domain)
		}
	}

	if len(ecosystemDomainsNotAsIdentifiers) > 0 {
		strictModeValidationLog.Printf("Engine '%s' has ecosystem domains not specified as identifiers in strict mode, emitting informational guidance", engineID)

		// Build informational message with ecosystem suggestions
		var suggestions []string
		for _, ds := range ecosystemDomainsNotAsIdentifiers {
			suggestions = append(suggestions, fmt.Sprintf("'%s' → '%s'", ds.domain, ds.ecosystem))
		}

		infoMsg := "recommend using ecosystem identifiers instead of individual domain names for better maintainability: " + strings.Join(suggestions, ", ")

		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(infoMsg))
	}
}

func validateStrictEngineFirewall(engineID string, networkPermissions *NetworkPermissions, sandboxConfig *SandboxConfig) error {
	// Only apply firewall validation to copilot and codex engines
	if engineID != string(constants.CopilotEngine) && engineID != string(constants.CodexEngine) {
		strictModeValidationLog.Printf("Engine '%s' does not support firewall, skipping firewall validation", engineID)
		return nil
	}

	// Skip firewall validation when agent sandbox is enabled (AWF/SRT)
	// The agent sandbox provides its own network isolation
	if isSandboxEnabled(sandboxConfig, networkPermissions) {
		strictModeValidationLog.Printf("Agent sandbox is enabled, skipping firewall validation")
		return nil
	}

	// If network permissions don't exist, that's fine (will default to "defaults")
	if networkPermissions == nil {
		strictModeValidationLog.Printf("No network permissions, skipping firewall validation")
		return nil
	}

	// Check if allowed contains "*" (unrestricted network access)
	// If it does, firewall is not required
	if slices.Contains(networkPermissions.Allowed, "*") {
		strictModeValidationLog.Printf("Wildcard '*' in allowed domains, skipping firewall validation")
		return nil
	}

	// At this point, we have network domains (or defaults) and copilot/codex engine
	// In strict mode, firewall MUST be enabled
	if networkPermissions.Firewall == nil || !networkPermissions.Firewall.Enabled {
		strictModeValidationLog.Printf("Firewall validation failed: firewall not enabled in strict mode")
		return fmt.Errorf("strict mode: firewall must be enabled for %s engine with network restrictions. The firewall should be enabled by default, but if you've explicitly disabled it with 'sandbox.agent: false', this is not allowed in strict mode for security reasons. See: https://github.github.com/gh-aw/reference/network/", engineID)
	}

	strictModeValidationLog.Printf("Firewall validation passed")
	return nil
}
