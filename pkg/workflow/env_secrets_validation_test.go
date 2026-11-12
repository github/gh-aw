//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileImportedEngineSupportedEnvVarKeysStrict(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		topLevelEnv string
		strict      bool
		wantSection string
		awfVersion  string
		wantError   string
		envValue    string
		importedEnv string
	}{
		{name: "declared key", key: "ANTHROPIC_API_KEY", strict: true},
		{name: "undeclared key", key: "OTHER_API_KEY", strict: true, wantSection: "engine.env"},
		{name: "top-level secret", key: "ANTHROPIC_API_KEY", topLevelEnv: "env:\n  TOP_SECRET: ${{ secrets.TOP_SECRET }}\n", strict: true, wantSection: "env"},
		{name: "non-strict top-level warning once", key: "ANTHROPIC_API_KEY", topLevelEnv: "env:\n  TOP_SECRET: ${{ secrets.TOP_SECRET }}\n"},
		{name: "non-strict engine warning once", key: "OTHER_API_KEY"},
		{name: "imported top-level credential strict", key: "ANTHROPIC_API_KEY", strict: true, importedEnv: "env:\n  IMPORTED_TOKEN: ${{ github.token }}\n", wantSection: "env"},
		{name: "imported top-level credential non-strict warning once", key: "ANTHROPIC_API_KEY", importedEnv: "env:\n  IMPORTED_TOKEN: ${{ github.token }}\n"},
		{name: "older firewall", key: "ANTHROPIC_API_KEY", strict: true, awfVersion: "v0.25.0", wantError: "requires AWF v0.25.3 or newer"},
		{name: "minimum firewall", key: "ANTHROPIC_API_KEY", strict: true, awfVersion: "v0.25.3"},
		{name: "latest firewall", key: "ANTHROPIC_API_KEY", strict: true, awfVersion: "latest"},
		{name: "unversioned firewall", key: "ANTHROPIC_API_KEY", strict: true, awfVersion: "custom", wantError: "requires AWF v0.25.3 or newer"},
		{name: "non-strict older firewall", key: "ANTHROPIC_API_KEY", awfVersion: "v0.25.0"},
		{name: "older firewall without secrets", key: "ANTHROPIC_API_KEY", strict: true, awfVersion: "v0.25.0", envValue: "plain-value"},
		{name: "declared key with github token", key: "ANTHROPIC_API_KEY", strict: true, envValue: "${{ github['token'] }}"},
		{name: "github token with older firewall", key: "ANTHROPIC_API_KEY", strict: true, awfVersion: "v0.25.0", envValue: "${{ github['token'] }}", wantError: "requires AWF v0.25.3 or newer"},
	}
	for _, api := range []string{"file", "string"} {
		for _, tt := range tests {
			t.Run(api+"/"+tt.name, func(t *testing.T) {
				dir := t.TempDir()
				sharedDir := filepath.Join(dir, "shared")
				require.NoError(t, os.MkdirAll(sharedDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "engine.md"), []byte(fmt.Sprintf(`---
engine:
  id: my-engine
  display-name: My Engine
  behaviors:
    secret-strategy: universal-llm-consumer
    supported-env-var-keys: [ANTHROPIC_API_KEY]
    execution:
      command-name: my-engine
      step-name: Execute My Engine
%s---
`, tt.importedEnv)), 0600))
				path := filepath.Join(dir, "repro.md")
				sandbox := ""
				if tt.awfVersion != "" {
					sandbox = fmt.Sprintf("sandbox:\n  agent:\n    version: %s\n", tt.awfVersion)
				}
				envValue := tt.envValue
				if envValue == "" {
					envValue = "${{ secrets.MY_ANTHROPIC_KEY }}"
				}
				content := fmt.Sprintf(`---
on: workflow_dispatch
permissions:
  contents: read
strict: %t
imports: [shared/engine.md]
engine:
  id: my-engine
  model: anthropic/claude-sonnet-4
  env:
    %s: %s
%s%s---
Say hello.
`, tt.strict, tt.key, envValue, tt.topLevelEnv, sandbox)
				require.NoError(t, os.WriteFile(path, []byte(content), 0600))
				var compileErr error
				var yamlContent string
				stderr := captureStderr(func() {
					compiler := NewCompiler()
					if api == "file" {
						compileErr = compiler.CompileWorkflow(path)
						return
					}
					var data *WorkflowData
					data, compileErr = compiler.ParseWorkflowString(content, path)
					if compileErr == nil && tt.wantSection == "" && tt.wantError == "" {
						yamlContent, compileErr = compiler.CompileToYAML(data, path)
					}
				})
				if tt.wantSection != "" {
					require.ErrorContains(t, compileErr, "strict mode: secrets detected in '"+tt.wantSection+"' section")
					require.NoFileExists(t, filepath.Join(dir, "repro.lock.yml"))
					return
				}
				if tt.wantError != "" {
					require.ErrorContains(t, compileErr, tt.wantError)
					return
				}
				require.NoError(t, compileErr)
				if api == "file" {
					lock, err := os.ReadFile(filepath.Join(dir, "repro.lock.yml"))
					require.NoError(t, err)
					yamlContent = string(lock)
				}
				if tt.awfVersion == "v0.25.0" {
					assert.NotContains(t, yamlContent, "--exclude-env ")
				} else {
					assert.Contains(t, yamlContent, "--exclude-env "+tt.key)
				}
				assert.Contains(t, yamlContent, envValue)
				wantTopWarnings, wantEngineWarnings := 0, 0
				if tt.topLevelEnv != "" || tt.importedEnv != "" {
					wantTopWarnings = 1
				}
				if tt.key == "OTHER_API_KEY" {
					wantEngineWarnings = 1
				}
				assert.Equal(t, wantTopWarnings, strings.Count(stderr, "secrets detected in 'env' section"))
				assert.Equal(t, wantEngineWarnings, strings.Count(stderr, "secrets detected in 'engine.env' section"))
			})
		}
	}
}

func TestCompileEngineFallbackTopLevelModelEnvSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fallback.md")
	content := `---
on: workflow_dispatch
permissions:
  contents: read
strict: true
model: anthropic/claude-sonnet-4
engine:
  id: copilot
  fallback-models: [secondary]
  env:
    ANTHROPIC_API_KEY: ${{ secrets.MY_ANTHROPIC_KEY }}
---
Say hello.
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	t.Run("file", func(t *testing.T) {
		require.NoError(t, NewCompiler().CompileWorkflow(path))
	})
	t.Run("string", func(t *testing.T) {
		_, err := NewCompiler().ParseWorkflowString(content, path)
		require.NoError(t, err)
	})
}

func TestCompileEnvCredentialReferences(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		engineEnv    bool
		key          string
		nonStrict    bool
		awfVersion   string
		wantSection  string
		wantError    string
		preserveLock bool
	}{
		{name: "dot token", value: "${{ github.token }}", wantSection: "env"},
		{name: "github wildcard", value: "${{ github.* }}", wantSection: "env"},
		{name: "serialized github wildcard", value: "${{ toJSON(github.*) }}", wantSection: "env"},
		{name: "github wildcard preserves lock", value: "${{ github.* }}", wantSection: "env", preserveLock: true},
		{name: "serialized github wildcard preserves lock", value: "${{ toJSON(github.*) }}", wantSection: "env", preserveLock: true},
		{name: "secrets wildcard", value: "${{ secrets.* }}", wantSection: "env"},
		{name: "secrets wildcard property", value: "${{ secrets.*.value }}", wantSection: "env"},
		{name: "single quoted token", value: "${{ github['token'] }}", wantSection: "env"},
		{name: "whole github context", value: "${{ github }}", wantSection: "env"},
		{name: "computed github access", value: "${{ github[vars.KEY] }}", wantSection: "env"},
		{name: "whole secrets context", value: "${{ secrets }}", wantSection: "env"},
		{name: "computed secrets access", value: "${{ secrets[vars.KEY] }}", wantSection: "env"},
		{name: "double quoted token", value: `${{ github["token"] }}`, wantError: "invalid expression in 'env' section"},
		{name: "case insensitive token", value: "${{ GitHub [ 'ToKeN' ] }}", wantSection: "env"},
		{name: "fallback token", value: "${{ vars.VALUE || github['token'] }}", wantSection: "env"},
		{name: "formatted token", value: "${{ format('{0}', github['token']) }}", wantSection: "env"},
		{name: "embedded token", value: "prefix-${{ github['token'] }}", wantSection: "env"},
		{name: "secret control", value: "${{ secrets['GITHUB_TOKEN'] }}", wantSection: "env"},
		{name: "metadata control", value: "${{ github['repository'] }}"},
		{name: "github event wildcard control", value: "${{ toJSON(github.event.*) }}"},
		{name: "vars control", value: "${{ vars.VALUE }}"},
		{name: "vars wildcard control", value: "${{ toJSON(vars.*) }}"},
		{name: "quoted wildcard control", value: "${{ 'github.*' }}"},
		{name: "quoted token control", value: "${{ 'github.token' }}"},
		{name: "quoted bracket token control", value: `${{ 'github["token"]' }}`},
		{name: "plain token control", value: "github['token']"},
		{name: "non-strict whole github context", value: "${{ github }}", nonStrict: true, wantSection: "env"},
		{name: "non-strict github wildcard", value: "${{ github.* }}", nonStrict: true, wantSection: "env"},
		{name: "non-strict serialized github wildcard", value: "${{ toJSON(github.*) }}", nonStrict: true, wantSection: "env"},
		{name: "non-strict single quoted token", value: "${{ github['token'] }}", nonStrict: true, wantSection: "env"},
		{name: "non-strict double quoted token", value: `${{ github["token"] }}`, nonStrict: true, wantError: "invalid expression in 'env' section"},
		{name: "unsupported engine key", value: "${{ github['token'] }}", engineEnv: true, wantSection: "engine.env"},
		{name: "engine github wildcard", value: "${{ github.* }}", engineEnv: true, wantSection: "engine.env"},
		{name: "engine serialized github wildcard", value: "${{ toJSON(github.*) }}", engineEnv: true, wantSection: "engine.env"},
		{name: "engine github wildcard preserves lock", value: "${{ github.* }}", engineEnv: true, wantSection: "engine.env", preserveLock: true},
		{name: "engine serialized github wildcard preserves lock", value: "${{ toJSON(github.*) }}", engineEnv: true, wantSection: "engine.env", preserveLock: true},
		{name: "non-strict engine github wildcard", value: "${{ github.* }}", engineEnv: true, nonStrict: true, wantSection: "engine.env"},
		{name: "non-strict engine serialized github wildcard", value: "${{ toJSON(github.*) }}", engineEnv: true, nonStrict: true, wantSection: "engine.env"},
		{name: "unsupported engine double quoted token", value: `${{ github["token"] }}`, engineEnv: true, wantError: "invalid expression in 'engine.env' section"},
		{name: "supported engine key", value: "${{ github['token'] }}", engineEnv: true, key: "COPILOT_GITHUB_TOKEN"},
		{name: "non-core Copilot provider key", value: "${{ github['token'] }}", engineEnv: true, key: "COPILOT_PROVIDER_API_KEY"},
		{name: "supported engine double quoted token", value: `${{ github["token"] }}`, engineEnv: true, key: "COPILOT_GITHUB_TOKEN", wantError: "invalid expression in 'engine.env' section"},
		{name: "supported engine key older firewall", value: "${{ github['token'] }}", engineEnv: true, key: "COPILOT_GITHUB_TOKEN", awfVersion: "v0.25.0", wantError: "requires AWF v0.25.3 or newer"},
		{name: "supported engine dot token older firewall", value: "${{ github.token }}", engineEnv: true, key: "COPILOT_GITHUB_TOKEN", awfVersion: "v0.25.0", wantError: "requires AWF v0.25.3 or newer"},
		{name: "empty expression", value: "${{ }}", wantError: "invalid expression in 'env' section"},
		{name: "unclosed expression", value: "${{ github['token']", wantError: "invalid expression in 'env' section"},
	}
	for _, api := range []string{"file", "string"} {
		for _, tt := range tests {
			t.Run(api+"/"+tt.name, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "credentials.md")
				key := tt.key
				if key == "" {
					key = "MY_VALUE"
				}
				env := fmt.Sprintf("env:\n  %s: %s\n", key, tt.value)
				engine := "engine: copilot\n"
				if tt.engineEnv {
					engine = fmt.Sprintf("engine:\n  id: copilot\n  env:\n    %s: %s\n", key, tt.value)
					env = ""
				}
				sandbox := ""
				if tt.awfVersion != "" {
					sandbox = fmt.Sprintf("sandbox:\n  agent:\n    version: %s\n", tt.awfVersion)
				}
				content := fmt.Sprintf("---\non: workflow_dispatch\npermissions:\n  contents: read\nstrict: %t\n%s%s%s---\nSay hello.\n", !tt.nonStrict, engine, env, sandbox)
				require.NoError(t, os.WriteFile(path, []byte(content), 0600))
				lockPath := filepath.Join(dir, "credentials.lock.yml")
				const existingLock = "existing lock file\n"
				if tt.preserveLock {
					require.NoError(t, os.WriteFile(lockPath, []byte(existingLock), 0600))
				}
				var compileErr error
				var yamlContent string
				stderr := captureStderr(func() {
					compiler := NewCompiler()
					if api == "file" {
						compileErr = compiler.CompileWorkflow(path)
						return
					}
					var data *WorkflowData
					data, compileErr = compiler.ParseWorkflowString(content, path)
					if compileErr == nil {
						yamlContent, compileErr = compiler.CompileToYAML(data, path)
					}
				})
				if tt.wantSection != "" && !tt.nonStrict {
					require.ErrorContains(t, compileErr, "strict mode: secrets detected in '"+tt.wantSection+"' section")
					if tt.preserveLock {
						lock, err := os.ReadFile(lockPath)
						require.NoError(t, err)
						assert.Equal(t, existingLock, string(lock))
					} else {
						require.NoFileExists(t, lockPath)
					}
					return
				}
				if tt.wantError != "" {
					require.ErrorContains(t, compileErr, tt.wantError)
					assert.NotContains(t, compileErr.Error(), "invalid expression in env variable")
					require.NoFileExists(t, filepath.Join(dir, "credentials.lock.yml"))
					return
				}
				require.NoError(t, compileErr)
				if api == "file" {
					lock, err := os.ReadFile(filepath.Join(dir, "credentials.lock.yml"))
					require.NoError(t, err)
					yamlContent = string(lock)
				}
				assert.Contains(t, yamlContent, tt.value)
				if tt.nonStrict {
					assert.Contains(t, stderr, "secrets detected in '"+tt.wantSection+"' section")
				}
				if tt.engineEnv {
					assert.Contains(t, yamlContent, "--exclude-env "+key)
				}
			})
		}
	}
}

func TestCredentialExpressionsFromMapSerializationException(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "whole secrets context", value: "${{ secrets }}", want: true},
		{name: "unresolved github access", value: "${{ github[vars.KEY] }}", want: true},
		{name: "secrets serialization is handled separately", value: "${{ toJSON(secrets) }}"},
		{name: "credential beside serialized secrets", value: "${{ format('{0}{1}', toJSON(secrets), github.token) }}", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := credentialExpressionsFromMap(map[string]string{"VALUE": tt.value})
			require.NoError(t, err)
			assert.Equal(t, tt.want, len(got) > 0)
		})
	}
}

func TestValidateEnvSecrets(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		strictMode  bool
		expectError bool
		errorMsg    string
	}{
		{
			name: "no env section is allowed",
			frontmatter: map[string]any{
				"on": "push",
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "env with no secrets is allowed",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"NODE_ENV":    "production",
					"API_URL":     "https://api.example.com",
					"ENABLE_LOGS": "true",
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "env with plain values in non-strict mode is allowed",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"NODE_ENV": "production",
					"API_URL":  "https://api.example.com",
				},
			},
			strictMode:  false,
			expectError: false,
		},
		{
			name: "env with single secret in strict mode fails",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"API_KEY": "${{ secrets.API_KEY }}",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container. Found: ${{ secrets.API_KEY }}",
		},
		{
			name: "env with multiple secrets in strict mode fails",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"API_KEY":    "${{ secrets.API_KEY }}",
					"DB_PASS":    "${{ secrets.DATABASE_PASSWORD }}",
					"AUTH_TOKEN": "${{ secrets.AUTH_TOKEN }}",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container",
		},
		{
			name: "env with secret with fallback in strict mode fails",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"API_KEY": "${{ secrets.API_KEY || 'default' }}",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container",
		},
		{
			name: "env with secret embedded in string in strict mode fails",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"AUTH_HEADER": "Bearer ${{ secrets.TOKEN }}",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container. Found: ${{ secrets.TOKEN }}",
		},
		{
			name: "env with mixed secrets and plain values in strict mode fails",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"NODE_ENV": "production",
					"API_KEY":  "${{ secrets.API_KEY }}",
					"API_URL":  "https://api.example.com",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container. Found: ${{ secrets.API_KEY }}",
		},
		{
			name: "env with env variables (not secrets) is allowed",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"NODE_ENV":    "production",
					"WORKSPACE":   "${{ github.workspace }}",
					"RUNNER_TEMP": "${{ runner.temp }}",
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "empty env section is allowed",
			frontmatter: map[string]any{
				"on":  "push",
				"env": map[string]any{},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "env section with non-string values is allowed (edge case)",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"PORT":         8080,
					"ENABLE_DEBUG": true,
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "env with sub-expression: github.workflow && secrets.TOKEN fails in strict mode",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"TOKEN": "${{ github.workflow && secrets.TOKEN }}",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container",
		},
		{
			name: "env with sub-expression: secrets in OR with env fails in strict mode",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"DB_PASS": "${{ secrets.DB_PASSWORD || env.DEFAULT_PASS }}",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container",
		},
		{
			name: "env with sub-expression: secrets in parentheses fails in strict mode",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"AUTH": "${{ (github.actor || secrets.HIDDEN_KEY) }}",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container",
		},
		{
			name: "env with sub-expression: NOT operator with secrets fails in strict mode",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"CHECK": "${{ !secrets.PRIVATE_KEY && github.workflow }}",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container",
		},
		{
			name: "env with multiple sub-expressions fails in strict mode",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"SIMPLE":    "${{ secrets.SIMPLE_KEY }}",
					"SUB_EXPR1": "${{ github.workflow && secrets.TOKEN }}",
					"SUB_EXPR2": "${{ (github.actor || secrets.HIDDEN) }}",
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = tt.strictMode

			err := compiler.validateEnvSecrets(tt.frontmatter)

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}
		})
	}
}

func TestValidateEnvSecretsNonStrictMode(t *testing.T) {
	// Note: This test cannot capture the warning output directly as it goes to stderr
	// The warning behavior is tested by ensuring no error is returned in non-strict mode
	tests := []struct {
		name        string
		frontmatter map[string]any
	}{
		{
			name: "env with single secret in non-strict mode emits warning",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"API_KEY": "${{ secrets.API_KEY }}",
				},
			},
		},
		{
			name: "env with multiple secrets in non-strict mode emits warning",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"API_KEY": "${{ secrets.API_KEY }}",
					"DB_PASS": "${{ secrets.DATABASE_PASSWORD }}",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = false

			// In non-strict mode, this should not return an error (only emit a warning)
			err := compiler.validateEnvSecrets(tt.frontmatter)
			assert.NoError(t, err, "Non-strict mode should not return an error, only emit a warning")
		})
	}
}

func TestValidateEnvSecretsIntegration(t *testing.T) {
	// Test that validateEnvSecrets is properly integrated with the compiler
	tests := []struct {
		name        string
		frontmatter map[string]any
		strictMode  bool
		expectError bool
	}{
		{
			name: "compilation with env secrets in strict mode should fail",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "copilot",
				"env": map[string]any{
					"API_KEY": "${{ secrets.API_KEY }}",
				},
			},
			strictMode:  true,
			expectError: true,
		},
		{
			name: "compilation with env secrets in non-strict mode should succeed with warning",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "copilot",
				"strict": false,
				"env": map[string]any{
					"API_KEY": "${{ secrets.API_KEY }}",
				},
			},
			strictMode:  false,
			expectError: false,
		},
		{
			name: "compilation without env secrets in strict mode should succeed",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "copilot",
				"env": map[string]any{
					"NODE_ENV": "production",
				},
			},
			strictMode:  true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = tt.strictMode

			err := compiler.validateEnvSecrets(tt.frontmatter)

			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}
		})
	}
}

func TestValidateEnvSecretsErrorMessage(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = true

	frontmatter := map[string]any{
		"on": "push",
		"env": map[string]any{
			"API_KEY": "${{ secrets.API_KEY }}",
		},
	}

	err := compiler.validateEnvSecrets(frontmatter)
	require.Error(t, err, "Expected an error")

	errorMsg := err.Error()
	assert.Contains(t, errorMsg, "strict mode", "Error should mention strict mode")
	assert.Contains(t, errorMsg, "secrets detected in 'env' section", "Error should explain the issue")
	assert.Contains(t, errorMsg, "leaked to the agent container", "Error should explain the security risk")
	assert.Contains(t, errorMsg, "${{ secrets.API_KEY }}", "Error should list the specific secret found")
	assert.Contains(t, errorMsg, "Use engine-specific secret configuration", "Error should provide guidance")
}

func TestExtractSecretsFromEnv(t *testing.T) {
	// Test the underlying secret extraction logic
	tests := []struct {
		name           string
		envMap         map[string]string
		expectedCount  int
		expectedSecret string
	}{
		{
			name: "single secret",
			envMap: map[string]string{
				"API_KEY": "${{ secrets.API_KEY }}",
			},
			expectedCount:  1,
			expectedSecret: "${{ secrets.API_KEY }}",
		},
		{
			name: "multiple secrets",
			envMap: map[string]string{
				"API_KEY": "${{ secrets.API_KEY }}",
				"DB_PASS": "${{ secrets.DB_PASSWORD }}",
			},
			expectedCount: 2,
		},
		{
			name: "secret with fallback",
			envMap: map[string]string{
				"API_KEY": "${{ secrets.API_KEY || 'default' }}",
			},
			expectedCount: 1,
		},
		{
			name: "embedded secret",
			envMap: map[string]string{
				"AUTH_HEADER": "Bearer ${{ secrets.TOKEN }}",
			},
			expectedCount:  1,
			expectedSecret: "${{ secrets.TOKEN }}",
		},
		{
			name: "no secrets",
			envMap: map[string]string{
				"NODE_ENV": "production",
				"API_URL":  "https://api.example.com",
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secrets := ExtractSecretsFromMap(tt.envMap)
			assert.Len(t, secrets, tt.expectedCount, "Secret count mismatch")

			if tt.expectedSecret != "" {
				found := false
				for _, secret := range secrets {
					if secret == tt.expectedSecret {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected secret %s not found in extracted secrets", tt.expectedSecret)
			}
		})
	}
}

func TestValidateEnvSecretsMultipleSecretsErrorMessage(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = true

	frontmatter := map[string]any{
		"on": "push",
		"env": map[string]any{
			"API_KEY":    "${{ secrets.API_KEY }}",
			"DB_PASS":    "${{ secrets.DB_PASSWORD }}",
			"AUTH_TOKEN": "${{ secrets.AUTH_TOKEN }}",
		},
	}

	err := compiler.validateEnvSecrets(frontmatter)
	require.Error(t, err, "Expected an error")

	errorMsg := err.Error()
	// Should contain all three secrets or at least indicate multiple secrets
	secretCount := strings.Count(errorMsg, "${{ secrets.")
	assert.GreaterOrEqual(t, secretCount, 1, "Error should mention at least one secret")
}

func TestValidateEngineEnvSecrets(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		strictMode  bool
		expectError bool
		errorMsg    string
	}{
		{
			name: "engine string format has no env section to validate",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "copilot",
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "engine object without env is allowed",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "engine.env with no secrets is allowed",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"NODE_ENV": "production",
						"API_URL":  "https://api.example.com",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "engine.env overriding COPILOT_GITHUB_TOKEN with custom secret is allowed in strict mode",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						// Override the engine's own token with an org-specific secret – allowed.
						"COPILOT_GITHUB_TOKEN": "${{ secrets.MY_ORG_COPILOT_TOKEN }}",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "engine.env with non-engine secret alongside engine var override still fails",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						// Override for engine var – allowed.
						"COPILOT_GITHUB_TOKEN": "${{ secrets.MY_ORG_COPILOT_TOKEN }}",
						// Non-engine var with a secret – should still fail.
						"SOME_OTHER_KEY": "${{ secrets.SOME_OTHER_SECRET }}",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'engine.env' section",
		},
		{
			name: "engine.env with single secret in strict mode fails",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"API_KEY": "${{ secrets.API_KEY }}",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    fmt.Sprintf("are excluded from the agent sandbox via awf --exclude-env (requires AWF %s+) and are not accessible to the agent when that version is in use. Found: ${{ secrets.API_KEY }}", constants.AWFExcludeEnvMinVersion),
		},
		{
			name: "engine.env with multiple secrets in strict mode fails",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"API_KEY": "${{ secrets.API_KEY }}",
						"DB_PASS": "${{ secrets.DATABASE_PASSWORD }}",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    fmt.Sprintf("are excluded from the agent sandbox via awf --exclude-env (requires AWF %s+)", constants.AWFExcludeEnvMinVersion),
		},
		{
			name: "engine.env with secret embedded in string in strict mode fails",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"AUTH_HEADER": "Bearer ${{ secrets.TOKEN }}",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    fmt.Sprintf("are excluded from the agent sandbox via awf --exclude-env (requires AWF %s+) and are not accessible to the agent when that version is in use. Found: ${{ secrets.TOKEN }}", constants.AWFExcludeEnvMinVersion),
		},
		{
			name: "engine.env with secret in non-strict mode emits warning (no error)",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"API_KEY": "${{ secrets.API_KEY }}",
					},
				},
			},
			strictMode:  false,
			expectError: false,
		},
		{
			name: "both env and engine.env with secrets in strict mode fails on env first",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"TOP_KEY": "${{ secrets.TOP_SECRET }}",
				},
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"API_KEY": "${{ secrets.API_KEY }}",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'env' section will be leaked to the agent container",
		},
		// BYOK (Bring Your Own Key) provider variable tests
		{
			name: "engine.env with COPILOT_PROVIDER_BASE_URL secret is allowed in strict mode (BYOK)",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"COPILOT_PROVIDER_BASE_URL": "${{ secrets.PROVIDER_BASE_URL }}",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "engine.env with COPILOT_PROVIDER_API_KEY secret is allowed in strict mode (BYOK)",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"COPILOT_PROVIDER_API_KEY": "${{ secrets.PROVIDER_API_KEY }}",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "engine.env with COPILOT_PROVIDER_BEARER_TOKEN secret is allowed in strict mode (BYOK)",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"COPILOT_PROVIDER_BEARER_TOKEN": "${{ secrets.PROVIDER_BEARER_TOKEN }}",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "engine.env with full BYOK config (URL + API key + model) is allowed in strict mode",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"COPILOT_PROVIDER_BASE_URL": "${{ secrets.PROVIDER_BASE_URL }}",
						"COPILOT_PROVIDER_API_KEY":  "${{ secrets.PROVIDER_API_KEY }}",
						"COPILOT_MODEL":             "claude-sonnet-4",
						"COPILOT_PROVIDER_TYPE":     "anthropic",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "engine.env with COPILOT_PROVIDER_WIRE_API secret is allowed in strict mode (BYOK)",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"COPILOT_PROVIDER_WIRE_API": "${{ secrets.PROVIDER_WIRE_API }}",
					},
				},
			},
			strictMode:  true,
			expectError: false,
		},
		{
			name: "engine.env with BYOK vars and an unrelated secret still fails in strict mode",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "copilot",
					"env": map[string]any{
						"COPILOT_PROVIDER_BASE_URL": "${{ secrets.PROVIDER_BASE_URL }}",
						"COPILOT_PROVIDER_API_KEY":  "${{ secrets.PROVIDER_API_KEY }}",
						"UNRELATED_SECRET":          "${{ secrets.UNRELATED }}",
					},
				},
			},
			strictMode:  true,
			expectError: true,
			errorMsg:    "strict mode: secrets detected in 'engine.env' section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = tt.strictMode

			err := compiler.validateEnvSecrets(tt.frontmatter)

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}
		})
	}
}
