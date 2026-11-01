package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNpmCooldownVersionCheckFailsClosedOnUnsupportedNpm(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
	}{
		{version: "11.9.0"},
		{version: "11.10.0", valid: true},
		{version: "12.0.0", valid: true},
		{version: "unknown"},
	}

	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			binDir := t.TempDir()
			npm := filepath.Join(binDir, "npm")
			require.NoError(t, os.WriteFile(npm, []byte("#!/bin/sh\nprintf '%s\\n' \"$NPM_VERSION_FIXTURE\"\n"), 0o755))
			lines := appendNpmCooldownVersionCheck(nil, 3)
			scriptLines := make([]string, len(lines))
			for i, line := range lines {
				scriptLines[i] = strings.TrimSpace(line)
			}
			cmd := exec.Command("bash", "-c", strings.Join(scriptLines, "\n"))
			cmd.Env = append(os.Environ(),
				"NPM_VERSION_FIXTURE="+test.version,
				"PATH="+binDir+":"+os.Getenv("PATH"),
			)
			err := cmd.Run()
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestNpmEngineInstallPathsRequireCooldownCapableNpm(t *testing.T) {
	hostSteps := GenerateNpmInstallSteps("example-engine", "1.2.3", "Install", "example", NPMInstallOptions{
		CooldownEnabled: true,
	})
	host := strings.Join([]string(hostSteps[0]), "\n")
	microVM := strings.Join(GenerateMicroVMNpmCLIInstallStep("example-engine", "1.2.3", "Install microVM", "example", false, true, 7), "\n")
	for name, step := range map[string]string{"host": host, "microVM": microVM} {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, step, "npm 11.10.0 or newer is required")
			assert.Contains(t, step, "NPM_CONFIG_MIN_RELEASE_AGE")
		})
	}

	days := 0
	disabledSteps := GenerateNpmInstallSteps("example-engine", "1.2.3", "Install", "example", NPMInstallOptions{
		CooldownDays: &days,
	})
	disabled := strings.Join([]string(disabledSteps[0]), "\n")
	assert.NotContains(t, disabled, "npm 11.10.0 or newer is required")
}

// TestResolveRuntimeCooldown_NilWorkflowData verifies nil WorkflowData defaults to
// cooldown enabled (true).
func TestResolveRuntimeCooldown_NilWorkflowData(t *testing.T) {
	assert.True(t, resolveRuntimeCooldown(nil, "node"))
}

// TestResolveRuntimeCooldown_TypedRuntimeConfig covers the RuntimesTyped path for
// every supported runtime ID, both when Cooldown is explicitly set and when it is nil.
func TestResolveRuntimeCooldown_TypedRuntimeConfig(t *testing.T) {
	tests := []struct {
		name      string
		runtimeID string
		configure func(rt *RuntimesConfig, cfg *RuntimeConfig)
	}{
		{"node", "node", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Node = cfg }},
		{"python", "python", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Python = cfg }},
		{"go", "go", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Go = cfg }},
		{"uv", "uv", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.UV = cfg }},
		{"bun", "bun", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Bun = cfg }},
		{"deno", "deno", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Deno = cfg }},
		{"dotnet", "dotnet", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Dotnet = cfg }},
		{"elixir", "elixir", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Elixir = cfg }},
		{"gh-aw", "gh-aw", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.GhAw = cfg }},
		{"haskell", "haskell", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Haskell = cfg }},
		{"java", "java", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Java = cfg }},
		{"ruby", "ruby", func(rt *RuntimesConfig, cfg *RuntimeConfig) { rt.Ruby = cfg }},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/cooldown-false", func(t *testing.T) {
			runtimeConfig := &RuntimeConfig{Cooldown: boolPtr(false)}
			runtimesTyped := &RuntimesConfig{}
			tt.configure(runtimesTyped, runtimeConfig)
			wd := &WorkflowData{
				ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: runtimesTyped},
			}
			assert.False(t, resolveRuntimeCooldown(wd, tt.runtimeID))
		})

		t.Run(tt.name+"/cooldown-true", func(t *testing.T) {
			runtimeConfig := &RuntimeConfig{Cooldown: boolPtr(true)}
			runtimesTyped := &RuntimesConfig{}
			tt.configure(runtimesTyped, runtimeConfig)
			wd := &WorkflowData{
				ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: runtimesTyped},
			}
			assert.True(t, resolveRuntimeCooldown(wd, tt.runtimeID))
		})

		t.Run(tt.name+"/cooldown-nil-falls-through", func(t *testing.T) {
			runtimeConfig := &RuntimeConfig{Cooldown: nil}
			runtimesTyped := &RuntimesConfig{}
			tt.configure(runtimesTyped, runtimeConfig)
			wd := &WorkflowData{
				ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: runtimesTyped},
				Runtimes:          map[string]any{},
			}
			// Cooldown nil on typed config => falls through to legacy Runtimes map,
			// which is empty here, so it should default to true.
			assert.True(t, resolveRuntimeCooldown(wd, tt.runtimeID))
		})
	}
}

// TestResolveRuntimeCooldown_UnknownRuntimeID verifies an unrecognized runtime ID
// leaves runtimeConfig nil in the typed switch and falls through to the legacy path.
func TestResolveRuntimeCooldown_UnknownRuntimeID(t *testing.T) {
	wd := &WorkflowData{
		ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: &RuntimesConfig{}},
		Runtimes:          map[string]any{},
	}
	assert.True(t, resolveRuntimeCooldown(wd, "unknown-runtime"))
}

// TestResolveRuntimeCooldown_NoParsedFrontmatter verifies the function falls
// through to the legacy Runtimes map when ParsedFrontmatter or RuntimesTyped is nil.
func TestResolveRuntimeCooldown_NoParsedFrontmatter(t *testing.T) {
	wd := &WorkflowData{
		Runtimes: map[string]any{},
	}
	assert.True(t, resolveRuntimeCooldown(wd, "node"))

	wd2 := &WorkflowData{
		ParsedFrontmatter: &FrontmatterConfig{RuntimesTyped: nil},
		Runtimes:          map[string]any{},
	}
	assert.True(t, resolveRuntimeCooldown(wd2, "node"))
}

// TestResolveRuntimeCooldown_LegacyRuntimesMap covers the legacy map[string]any
// path: missing runtime key, wrong type entry, missing cooldown key, non-bool
// cooldown value, and valid bool cooldown values.
func TestResolveRuntimeCooldown_LegacyRuntimesMap(t *testing.T) {
	t.Run("missing runtime key defaults true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("runtime entry not a map defaults true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": "not-a-map"}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("cooldown key missing defaults true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": map[string]any{}}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("cooldown value not a bool defaults true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": map[string]any{"cooldown": "false"}}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("cooldown explicitly false", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": map[string]any{"cooldown": false}}}
		assert.False(t, resolveRuntimeCooldown(wd, "node"))
	})

	t.Run("cooldown explicitly true", func(t *testing.T) {
		wd := &WorkflowData{Runtimes: map[string]any{"node": map[string]any{"cooldown": true}}}
		assert.True(t, resolveRuntimeCooldown(wd, "node"))
	})
}
