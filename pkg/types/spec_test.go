//go:build !integration

package types_test

import (
	"encoding/json"
	"testing"

	"github.com/github/gh-aw/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpec_Types_BaseMCPServerConfig validates that BaseMCPServerConfig has all documented
// fields and can be used for the server modes described in the types package README.md.
func TestSpec_Types_BaseMCPServerConfig(t *testing.T) {
	tests := []struct {
		name   string
		cfg    types.BaseMCPServerConfig
		checks func(t *testing.T, cfg types.BaseMCPServerConfig)
	}{
		{
			name: "stdio MCP server from spec example",
			cfg: types.BaseMCPServerConfig{
				Type:    "stdio",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
				Env: map[string]string{
					"ALLOWED_PATHS": "/workspace",
				},
			},
			checks: func(t *testing.T, cfg types.BaseMCPServerConfig) {
				assert.Equal(t, "stdio", cfg.Type, "Type field must hold the server type")
				assert.Equal(t, "npx", cfg.Command, "Command field must hold the executable")
				assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-filesystem"}, cfg.Args, "Args field must hold the arguments")
				assert.Equal(t, "/workspace", cfg.Env["ALLOWED_PATHS"], "Env field must hold environment variables")
			},
		},
		{
			name: "HTTP MCP server with OIDC auth from spec example",
			cfg: types.BaseMCPServerConfig{
				Type: "http",
				URL:  "https://my-mcp-server.example.com",
				Auth: &types.MCPAuthConfig{
					Type:     "github-oidc",
					Audience: "https://my-mcp-server.example.com",
				},
			},
			checks: func(t *testing.T, cfg types.BaseMCPServerConfig) {
				assert.Equal(t, "http", cfg.Type, "Type field must be 'http' for HTTP mode")
				assert.Equal(t, "https://my-mcp-server.example.com", cfg.URL, "URL field must hold the HTTP endpoint")
				require.NotNil(t, cfg.Auth, "Auth field must be non-nil for authenticated HTTP servers")
				assert.Equal(t, "github-oidc", cfg.Auth.Type, "Auth.Type must match the documented auth type")
				assert.Equal(t, "https://my-mcp-server.example.com", cfg.Auth.Audience, "Auth.Audience must match the server URL")
			},
		},
		{
			name: "container MCP server with mounts",
			cfg: types.BaseMCPServerConfig{
				Type:           "container",
				Container:      "my-mcp-image:latest",
				Entrypoint:     "/usr/bin/server",
				EntrypointArgs: []string{"--port", "8080"},
				Mounts:         []string{"/host/path:/container/path:ro"},
			},
			checks: func(t *testing.T, cfg types.BaseMCPServerConfig) {
				assert.Equal(t, "my-mcp-image:latest", cfg.Container, "Container field must hold the image")
				assert.Equal(t, "/usr/bin/server", cfg.Entrypoint, "Entrypoint field must hold the override entrypoint")
				assert.Equal(t, []string{"--port", "8080"}, cfg.EntrypointArgs, "EntrypointArgs must hold the entrypoint arguments")
				assert.Equal(t, []string{"/host/path:/container/path:ro"}, cfg.Mounts, "Mounts must hold volume mounts in source:dest:mode format")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.checks(t, tt.cfg)
		})
	}
}

// TestSpec_Types_BaseMCPServerConfig_JSONRoundTrip validates that BaseMCPServerConfig fields
// use both json and yaml struct tags as documented in the Design Notes section of the README.
// Spec: "All struct fields use both json and yaml struct tags so they can be round-tripped
// through both serialization formats."
func TestSpec_Types_BaseMCPServerConfig_JSONRoundTrip(t *testing.T) {
	original := types.BaseMCPServerConfig{
		Type:    "stdio",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
		Env:     map[string]string{"ALLOWED_PATHS": "/workspace"},
		Version: "1.2.3",
		Headers: map[string]string{"X-Custom": "header"},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err, "BaseMCPServerConfig must serialize to JSON without error")

	var decoded types.BaseMCPServerConfig
	require.NoError(t, json.Unmarshal(data, &decoded), "BaseMCPServerConfig must deserialize from JSON without error")

	assert.Equal(t, original.Type, decoded.Type, "Type must round-trip through JSON")
	assert.Equal(t, original.Command, decoded.Command, "Command must round-trip through JSON")
	assert.Equal(t, original.Args, decoded.Args, "Args must round-trip through JSON")
	assert.Equal(t, original.Env, decoded.Env, "Env must round-trip through JSON")
	assert.Equal(t, original.Version, decoded.Version, "Version must round-trip through JSON")
	assert.Equal(t, original.Headers, decoded.Headers, "Headers must round-trip through JSON")
}

// TestSpec_Types_MCPAuthConfig validates the MCPAuthConfig type documented in the README.
// Spec: "Authentication configuration for HTTP MCP servers. When configured, the MCP gateway
// dynamically acquires tokens and injects them as Authorization headers on each outgoing request."
func TestSpec_Types_MCPAuthConfig(t *testing.T) {
	tests := []struct {
		name     string
		auth     types.MCPAuthConfig
		wantType string
		wantAud  string
	}{
		{
			name: "github-oidc auth from spec example",
			auth: types.MCPAuthConfig{
				Type:     "github-oidc",
				Audience: "https://my-service.example.com",
			},
			wantType: "github-oidc",
			wantAud:  "https://my-service.example.com",
		},
		{
			name: "auth without explicit audience",
			auth: types.MCPAuthConfig{
				Type: "github-oidc",
			},
			wantType: "github-oidc",
			wantAud:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantType, tt.auth.Type, "Type field must match — spec says only 'github-oidc' is currently supported")
			assert.Equal(t, tt.wantAud, tt.auth.Audience, "Audience field must match — defaults to server URL if omitted")
		})
	}

	// Spec: "Type is the authentication type; currently only 'github-oidc' is supported."
	data, err := json.Marshal(types.MCPAuthConfig{Type: "github-oidc", Audience: "https://aud.example.com"})
	require.NoError(t, err, "MCPAuthConfig must serialize to JSON")
	assert.Contains(t, string(data), `"type":"github-oidc"`, "JSON must use documented field name 'type'")
	assert.Contains(t, string(data), `"audience"`, "JSON must use documented field name 'audience'")
}

// TestSpec_Types_TokenWeights validates the TokenWeights type documented in the README.
// Spec: "Defines custom model cost information for AI Credits computation stored in aw_info.json."
func TestSpec_Types_TokenWeights(t *testing.T) {
	weights := types.TokenWeights{
		Multipliers: map[string]float64{
			"gpt-4o": 2.5,
		},
		TokenClassWeights: &types.TokenClassWeights{
			Input:  1.0,
			Output: 3.0,
		},
	}

	assert.InDelta(t, 2.5, weights.Multipliers["gpt-4o"], 1e-9, "Multipliers must map model names to cost multipliers")
	require.NotNil(t, weights.TokenClassWeights, "TokenClassWeights must be settable")
	assert.InDelta(t, 1.0, weights.TokenClassWeights.Input, 1e-9, "TokenClassWeights.Input must hold the input token weight")
	assert.InDelta(t, 3.0, weights.TokenClassWeights.Output, 1e-9, "TokenClassWeights.Output must hold the output token weight")
}

// TestSpec_Types_TokenClassWeights validates the TokenClassWeights type documented in the README.
// Spec: "Per-token-class weights for AI Credits computation. Each field corresponds to
// one token class; a zero value means 'use the default weight'."
func TestSpec_Types_TokenClassWeights(t *testing.T) {
	// Spec documents these token classes:
	//   Input       → standard input tokens
	//   CachedInput → cache-hit input tokens
	//   Output      → generated output tokens
	//   Reasoning   → internal reasoning tokens
	//   CacheWrite  → cache-write tokens
	w := types.TokenClassWeights{
		Input:       1.0,
		CachedInput: 0.1,
		Output:      3.0,
		Reasoning:   2.0,
		CacheWrite:  1.5,
	}

	assert.InDelta(t, 1.0, w.Input, 1e-9, "Input must hold the standard input token weight")
	assert.InDelta(t, 0.1, w.CachedInput, 1e-9, "CachedInput must hold the cache-hit input token weight")
	assert.InDelta(t, 3.0, w.Output, 1e-9, "Output must hold the generated output token weight")
	assert.InDelta(t, 2.0, w.Reasoning, 1e-9, "Reasoning must hold the internal reasoning token weight")
	assert.InDelta(t, 1.5, w.CacheWrite, 1e-9, "CacheWrite must hold the cache-write token weight")

	// Spec: "a zero value means 'use the default weight'"
	zero := types.TokenClassWeights{}
	assert.InDelta(t, 0.0, zero.Input, 1e-9, "zero value of Input must be 0 (use default)")
	assert.InDelta(t, 0.0, zero.CachedInput, 1e-9, "zero value of CachedInput must be 0 (use default)")

	// Verify JSON field names from struct tags (underscores for stable wire compatibility)
	data, err := json.Marshal(w)
	require.NoError(t, err, "TokenClassWeights must serialize to JSON")
	assert.Contains(t, string(data), `"cached_input"`, "JSON must use underscore field name 'cached_input'")
	assert.Contains(t, string(data), `"cache_write"`, "JSON must use underscore field name 'cache_write'")
}

// TestSpec_Types_ZeroValueSafety validates that all types have sensible zero values
// and no required-but-unset-field panics.
// Spec (Design Notes): "BaseMCPServerConfig is designed to be embedded."
func TestSpec_Types_ZeroValueSafety(t *testing.T) {
	// Zero value of BaseMCPServerConfig must be usable without panicking.
	var cfg types.BaseMCPServerConfig
	assert.Empty(t, cfg.Type, "zero value Type must be empty string")
	assert.Nil(t, cfg.Auth, "zero value Auth must be nil")
	assert.Nil(t, cfg.Args, "zero value Args must be nil")

	// Zero value of TokenWeights must be usable.
	var tw types.TokenWeights
	assert.Nil(t, tw.TokenClassWeights, "zero value TokenClassWeights pointer must be nil (no overrides)")
	assert.Nil(t, tw.Multipliers, "zero value Multipliers must be nil (no overrides)")
}

// TestSpec_Types_InputDefinition validates the InputDefinition type documented in the README.
// Spec: "Defines an input parameter for workflows, safe-jobs, and imported workflows. The
// structure follows the workflow_dispatch input schema from GitHub Actions."
func TestSpec_Types_InputDefinition(t *testing.T) {
	// Spec example: a choice input.
	input := types.InputDefinition{
		Description: "The environment to deploy to",
		Required:    true,
		Default:     "staging",
		Type:        "choice",
		Options:     []string{"staging", "production"},
	}

	assert.Equal(t, "The environment to deploy to", input.Description, "Description field must hold the human-readable description")
	assert.True(t, input.Required, "Required field must hold whether the input is required")
	assert.Equal(t, "staging", input.Default, "Default field must hold the default value")
	assert.Equal(t, "choice", input.Type, "Type field must hold the documented input type")
	assert.Equal(t, []string{"staging", "production"}, input.Options, "Options field must hold the valid choice options")

	// Spec: "Required ... defaults to false"; zero value must be a usable input.
	var zero types.InputDefinition
	assert.False(t, zero.Required, "zero value Required must default to false as documented")
	assert.Nil(t, zero.Default, "zero value Default must be nil")
	assert.Nil(t, zero.Options, "zero value Options must be nil")

	// Design Notes: "All struct fields use both json and yaml struct tags." Verify documented
	// JSON field names round-trip.
	data, err := json.Marshal(input)
	require.NoError(t, err, "InputDefinition must serialize to JSON")
	assert.Contains(t, string(data), `"description"`, "JSON must use documented field name 'description'")
	assert.Contains(t, string(data), `"type"`, "JSON must use documented field name 'type'")
	assert.Contains(t, string(data), `"options"`, "JSON must use documented field name 'options'")
}

// TestSpec_PublicAPI_GetDefaultAsString validates the documented behavior of the
// InputDefinition.GetDefaultAsString method as described in the types package README.md.
// Spec: "Returns the Default field as a string, regardless of its underlying type. Handles
// string, bool, int, int64, and float64 inputs. Integer-valued float64 defaults (e.g. 1.0)
// are formatted without a decimal point. Returns \"\" when Default is nil."
func TestSpec_PublicAPI_GetDefaultAsString(t *testing.T) {
	tests := []struct {
		name     string
		def      any
		expected string
	}{
		// Spec: "Returns \"\" when Default is nil."
		{name: "nil default returns empty string", def: nil, expected: ""},
		// Spec: handles string inputs.
		{name: "string default returned as-is", def: "staging", expected: "staging"},
		// Spec: handles bool inputs.
		{name: "bool true default", def: true, expected: "true"},
		{name: "bool false default", def: false, expected: "false"},
		// Spec: handles int inputs.
		{name: "int default", def: 42, expected: "42"},
		// Spec: handles int64 inputs.
		{name: "int64 default", def: int64(9000000000), expected: "9000000000"},
		// Spec: "Integer-valued float64 defaults (e.g. 1.0) are formatted without a decimal point."
		{name: "integer-valued float64 has no decimal point", def: 1.0, expected: "1"},
		// Spec: non-integer float64 retains its fractional value.
		{name: "fractional float64 default", def: 2.5, expected: "2.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := types.InputDefinition{Default: tt.def}
			assert.Equal(t, tt.expected, input.GetDefaultAsString(),
				"GetDefaultAsString() with Default=%v (%T) should return %q as documented in spec", tt.def, tt.def, tt.expected)
		})
	}
}
