// This file provides schema validation for generated AWF configuration files.
// See awf_config.go for the config file types and awf_config_build.go for the
// construction of the config JSON that is validated here.

package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/syncutil"
)

var awfConfigSchemaLog = logger.New("workflow:awf_config_schema")

//go:embed schemas/awf-config.schema.json
var awfConfigSchema string

// Cached compiled AWF config schema to avoid recompiling on every validation.
var compiledAWFConfigSchemaLoader syncutil.OnceLoader[*jsonschema.Schema]

// getCompiledAWFConfigSchema returns the compiled AWF config schema, compiling once and caching.
func getCompiledAWFConfigSchema() (*jsonschema.Schema, error) {
	return compiledAWFConfigSchemaLoader.Get(func() (*jsonschema.Schema, error) {
		awfConfigSchemaLog.Print("Compiling AWF config schema (first time)")
		schemaURL := fmt.Sprintf("https://github.com/github/gh-aw-firewall/releases/download/%s/awf-config.schema.json", constants.DefaultFirewallVersion)
		schema, err := compileSchema(awfConfigSchema, schemaURL)
		if err == nil {
			awfConfigSchemaLog.Print("AWF config schema compiled successfully")
		}
		return schema, err
	})
}

// validateAWFConfigJSON validates the provided AWF config JSON string against the
// embedded AWF config schema. Returns nil if validation passes.
func validateAWFConfigJSON(configJSON string) error {
	schema, err := getCompiledAWFConfigSchema()
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal([]byte(configJSON), &doc); err != nil {
		return fmt.Errorf("invalid AWF config JSON: expected generated output to be valid JSON for schema validation; parse error: %w. This indicates a compiler bug; please report it", err)
	}
	normalizeTemplatableModelFallbackEnabled(doc)
	normalizeCloudHypervisorBundleReleaseTag(doc)
	if err := schema.Validate(doc); err != nil {
		return fmt.Errorf("invalid AWF config JSON: expected generated output to satisfy the embedded schema; review the referenced field path and fix that workflow/frontmatter value: %w", err)
	}
	return nil
}

// The bundle setup resolves the release tag before AWF consumes the config.
func normalizeCloudHypervisorBundleReleaseTag(doc any) {
	root, ok := doc.(map[string]any)
	if !ok {
		return
	}
	config, ok := root["cloudHypervisor"].(map[string]any)
	if !ok {
		return
	}
	if config["artifactReleaseTag"] == "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_RELEASE_TAG}" {
		config["artifactReleaseTag"] = string(constants.DefaultFirewallVersion)
	}
}

// normalizeTemplatableModelFallbackEnabled adjusts a generated AWF config document
// for compile-time schema validation by coercing modelFallback.enabled GitHub Actions
// expressions to a boolean placeholder. GitHub Actions resolves these expressions at
// runtime before AWF consumes the config.
func normalizeTemplatableModelFallbackEnabled(doc any) {
	root, ok := doc.(map[string]any)
	if !ok {
		return
	}
	apiProxy, ok := root["apiProxy"].(map[string]any)
	if !ok {
		return
	}
	modelFallback, ok := apiProxy["modelFallback"].(map[string]any)
	if !ok {
		return
	}
	enabled, ok := modelFallback["enabled"].(string)
	if !ok || !isExpression(enabled) {
		return
	}
	modelFallback["enabled"] = true
}

// buildAWFConfigSchemaURL returns the release-pinned JSON schema URL for the AWF config file.
// The URL is versioned so that schema validation tools always reference the exact schema
// that matches the AWF binary being used. When DefaultFirewallVersion is bumped the URL
// automatically tracks the new release.
//
// If firewallConfig carries an explicit version (e.g. sandbox.agent.version) that version
// is used; otherwise DefaultFirewallVersion is used.
func buildAWFConfigSchemaURL(firewallConfig *FirewallConfig) string {
	version := string(constants.DefaultFirewallVersion)
	if firewallConfig != nil && firewallConfig.Version != "" {
		version = firewallConfig.Version
	}
	// Special-case "latest": the GitHub Releases /latest/download/ shortcut serves
	// assets from the most recent release without requiring a tag in the path.
	if strings.EqualFold(version, "latest") {
		return "https://github.com/github/gh-aw-firewall/releases/latest/download/awf-config.schema.json"
	}
	// Ensure version has the 'v' prefix required by GitHub release tag URLs.
	version = semverutil.EnsureVPrefix(version)
	return fmt.Sprintf("https://github.com/github/gh-aw-firewall/releases/download/%s/awf-config.schema.json", version)
}
