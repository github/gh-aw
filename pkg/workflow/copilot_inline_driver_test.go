//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractEngineConfig_InlineDriverSource(t *testing.T) {
	c := NewCompiler()

	_, config, _ := c.ExtractEngineConfig(map[string]any{
		"engine": map[string]any{
			"id": "copilot",
			"driver": map[string]any{
				"python": "print('hello')",
			},
		},
	})

	require.NotNil(t, config)
	require.NotNil(t, config.InlineDriver)
	assert.Equal(t, inlineCopilotSDKDriverWrapperPath, config.Driver)
	assert.Equal(t, "python", config.InlineDriver.Runtime)
	assert.Equal(t, "print('hello')", config.InlineDriver.Source)
	assert.True(t, config.CopilotSDK, "inline driver should enable copilot-sdk mode")
}

func TestExtractEngineConfig_InlineDriverEmptySourcePreserved(t *testing.T) {
	// An empty inline source must not be silently dropped; it must be extracted
	// so that validateInlineEngineDriver can reject it with a clear error.
	c := NewCompiler()

	_, config, _ := c.ExtractEngineConfig(map[string]any{
		"engine": map[string]any{
			"id": "copilot",
			"driver": map[string]any{
				"python": "",
			},
		},
	})

	require.NotNil(t, config)
	require.NotNil(t, config.InlineDriver, "empty source should still produce an InlineDriver so validation can catch it")
	assert.Equal(t, "python", config.InlineDriver.Runtime)
	assert.Empty(t, config.InlineDriver.Source)
}

func TestValidateEngineDriver_EmptyInlineSourceIsRejected(t *testing.T) {
	err := NewCompiler().validateEngineDriver(&WorkflowData{
		EngineConfig: &EngineConfig{
			ID: "copilot",
			InlineDriver: &InlineEngineDriver{
				Runtime: "python",
				Source:  "",
			},
			Driver: inlineCopilotSDKDriverWrapperPath,
		},
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "must not be empty")
}

func TestValidateEngineDriver_InlineSourceRejectsNonCopilot(t *testing.T) {
	err := NewCompiler().validateEngineDriver(&WorkflowData{
		EngineConfig: &EngineConfig{
			ID: "claude",
			InlineDriver: &InlineEngineDriver{
				Runtime: "node",
				Source:  "console.log('hi')",
			},
			Driver: inlineCopilotSDKDriverWrapperPath,
		},
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "only supported for the copilot engine")
}

func TestCopilotEngineInstallationWithInlineDriver(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name       string
		runtime    string
		source     string
		expectPath string
		expectRun  string
	}{
		{
			name:       "node",
			runtime:    "node",
			source:     "console.log('hello')",
			expectPath: inlineCopilotSDKDriverNodePath,
			expectRun:  copilotSDKNpmCICommand,
		},
		{
			name:       "python",
			runtime:    "python",
			source:     "print('hello')",
			expectPath: inlineCopilotSDKDriverPythonPath,
			expectRun:  "python3 -m pip install --disable-pip-version-check --target",
		},
		{
			name:       "go",
			runtime:    "go",
			source:     "package main",
			expectPath: inlineCopilotSDKDriverGoPath,
			expectRun:  "go get github.com/github/copilot-sdk/go@v",
		},
		{
			name:       "java",
			runtime:    "java",
			source:     "class Main {}",
			expectPath: inlineCopilotSDKDriverJavaPath,
			expectRun:  "mvn -q dependency:build-classpath",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := engine.GetInstallationSteps(&WorkflowData{
				EngineConfig: &EngineConfig{
					ID:           "copilot",
					CopilotSDK:   true,
					InlineDriver: &InlineEngineDriver{Runtime: tt.runtime, Source: tt.source},
					Driver:       inlineCopilotSDKDriverWrapperPath,
				},
			})

			allSteps := flattenStepText(steps)
			assert.Contains(t, allSteps, "Write Inline Copilot SDK Driver")
			assert.Contains(t, allSteps, tt.expectPath)
			assert.Contains(t, allSteps, inlineCopilotSDKDriverWrapperPath)
			assert.Contains(t, allSteps, tt.expectRun)
		})
	}
}

func TestCopilotEngineExecutionStepsWithInlineDriver(t *testing.T) {
	engine := NewCopilotEngine()
	steps := engine.GetExecutionSteps(&WorkflowData{
		Name: "inline-driver-test",
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			CopilotSDK:   true,
			InlineDriver: &InlineEngineDriver{Runtime: "python", Source: "print('hello')"},
			Driver:       inlineCopilotSDKDriverWrapperPath,
		},
	}, "/tmp/gh-aw/test.log")

	require.Len(t, steps, 1)
	stepContent := strings.Join(steps[0], "\n")
	assert.Contains(t, stepContent, `${GITHUB_WORKSPACE}/`+inlineCopilotSDKDriverWrapperPath)
	assert.Contains(t, stepContent, "PYTHONPATH: ${{ github.workspace }}/.gh-aw/copilot-sdk/python")
	assert.NotContains(t, stepContent, "copilot_sdk_driver.cjs")
}

func TestDetectRuntimeRequirements_InlineDriver(t *testing.T) {
	for _, runtimeID := range []string{"node", "python", "go", "java"} {
		t.Run(runtimeID, func(t *testing.T) {
			requirements := DetectRuntimeRequirements(&WorkflowData{
				EngineConfig: &EngineConfig{
					ID:           "copilot",
					InlineDriver: &InlineEngineDriver{Runtime: runtimeID, Source: "inline"},
				},
			})

			found := false
			for _, req := range requirements {
				if req.Runtime != nil && req.Runtime.ID == runtimeID {
					found = true
					break
				}
			}
			assert.True(t, found, "expected runtime requirement for %s", runtimeID)
		})
	}
}

func TestInlineJavaInstallStep_BootstrapsMaven(t *testing.T) {
	// The Java inline install step must bootstrap Maven when it is not pre-installed,
	// rather than unconditionally invoking mvn and failing on self-hosted runners.
	engine := NewCopilotEngine()

	steps := engine.GetInstallationSteps(&WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			CopilotSDK:   true,
			InlineDriver: &InlineEngineDriver{Runtime: "java", Source: "class Main {}"},
			Driver:       inlineCopilotSDKDriverWrapperPath,
		},
	})

	allSteps := flattenStepText(steps)
	assert.Contains(t, allSteps, "command -v mvn", "step must check for existing mvn before bootstrapping")
	assert.Contains(t, allSteps, "repo.maven.apache.org", "bootstrap must download from Maven Central")
	assert.Contains(t, allSteps, inlineMavenVersion, "bootstrap must use pinned Maven version")
	assert.Contains(t, allSteps, "mvn -q dependency:build-classpath", "step must run classpath resolution")
}

func TestInlineGoDriverWriteStep_UsesEffectiveGoVersion(t *testing.T) {
	// The generated go.mod must reflect an explicitly pinned runtimes.go.version
	// rather than always emitting the repository-default Go version.
	step := buildInlineCopilotSDKDriverWriteStep(&WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			CopilotSDK:   true,
			InlineDriver: &InlineEngineDriver{Runtime: "go", Source: "package main"},
			Driver:       inlineCopilotSDKDriverWrapperPath,
		},
		ParsedFrontmatter: &FrontmatterConfig{
			RuntimesTyped: &RuntimesConfig{
				Go: &RuntimeConfig{Version: "1.22"},
			},
		},
	})

	content := strings.Join(step, "\n")
	assert.Contains(t, content, "go 1.22", "go.mod should use the explicitly pinned Go version")
	assert.NotContains(t, content, "go 1.26", "go.mod must not use the default version when an explicit pin is set")
}

func flattenStepText(steps []GitHubActionStep) string {
	var parts []string
	for _, step := range steps {
		parts = append(parts, strings.Join(step, "\n"))
	}
	return strings.Join(parts, "\n")
}

func TestValidateEngineDriver_MultipleRuntimeKeysRejected(t *testing.T) {
	// A driver map containing more than one runtime key should be rejected with a clear error,
	// not silently pick the first key.
	err := NewCompiler().validateEngineDriver(&WorkflowData{
		EngineConfig: &EngineConfig{
			ID: "copilot",
			InlineDriver: &InlineEngineDriver{
				Runtime:         "node",
				Source:          "console.log('hi')",
				MultipleRuntime: true,
			},
			Driver: inlineCopilotSDKDriverWrapperPath,
		},
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "exactly one runtime key")
}

func TestExtractEngineConfig_MultipleRuntimeKeys_SetsMultipleRuntimeFlag(t *testing.T) {
	// When the driver map has both node and python keys, MultipleRuntime must be true
	// so validation can produce a clear error instead of silently picking node.
	c := NewCompiler()

	_, config, _ := c.ExtractEngineConfig(map[string]any{
		"engine": map[string]any{
			"id": "copilot",
			"driver": map[string]any{
				"node":   "console.log('hi')",
				"python": "print('hi')",
			},
		},
	})

	require.NotNil(t, config)
	require.NotNil(t, config.InlineDriver)
	assert.True(t, config.InlineDriver.MultipleRuntime, "MultipleRuntime must be set when more than one runtime key is present")
}

func TestInlineGoInstallStep_CompilesBinary(t *testing.T) {
	// The Go inline install step must compile a binary during install so the wrapper
	// can exec it directly without recompiling on every agent invocation.
	engine := NewCopilotEngine()

	steps := engine.GetInstallationSteps(&WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			CopilotSDK:   true,
			InlineDriver: &InlineEngineDriver{Runtime: "go", Source: "package main\nfunc main(){}"},
			Driver:       inlineCopilotSDKDriverWrapperPath,
		},
	})

	allSteps := flattenStepText(steps)
	assert.Contains(t, allSteps, "go build", "install step must compile the driver to a binary")
	assert.Contains(t, allSteps, "inline-driver-bin", "install step must produce the binary used by the wrapper")
	assert.NotContains(t, allSteps, "go run", "install step must not use go run")
}

func TestInlineGoWrapperScript_ExecsBinaryNotGoRun(t *testing.T) {
	// The Go wrapper must exec the pre-compiled binary rather than using go run,
	// which would recompile on every agent invocation.
	d := &InlineEngineDriver{Runtime: "go", Source: "package main\nfunc main(){}"}
	script := d.wrapperScript()

	assert.Contains(t, script, inlineCopilotSDKDriverGoBinPath, "wrapper must reference the compiled binary path")
	assert.NotContains(t, script, "go run", "wrapper must not use go run")
}

func TestInlineJavaWrapperScript_RequiresClasspath(t *testing.T) {
	// The Java wrapper must require classpath.txt and fail loudly when absent,
	// rather than silently falling back to a plain `java` invocation that would
	// produce a cryptic JVM error.
	d := &InlineEngineDriver{Runtime: "java", Source: "public class Main {}"}
	script := d.wrapperScript()

	assert.Contains(t, script, inlineCopilotSDKDriverJavaClassPath, "wrapper must read classpath.txt")
	assert.NotContains(t, script, "if [ -f", "wrapper must not conditionally check for classpath.txt existence")
	// The wrapper uses cat which will fail with a clear error if the file is absent.
	assert.Contains(t, script, "cat \"", "wrapper must use cat to read classpath.txt, failing clearly if absent")
}

func TestHeredocWrite_TrailingNewlineDoesNotProduceBlankLine(t *testing.T) {
	// A source that ends with "\n" must not produce an extra blank line between
	// the last content line and the heredoc delimiter in the generated step.
	wd := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			CopilotSDK:   true,
			InlineDriver: &InlineEngineDriver{Runtime: "python", Source: "print('hello')\n"},
			Driver:       inlineCopilotSDKDriverWrapperPath,
		},
	}
	step := buildInlineCopilotSDKDriverWriteStep(wd)
	content := strings.Join(step, "\n")

	// Find the heredoc block for the Python source and verify the delimiter
	// immediately follows the last content line with no intervening blank line.
	lines := strings.Split(content, "\n")
	heredocFound := false
	for i, line := range lines {
		if strings.Contains(line, "cat > ") && strings.Contains(line, inlineCopilotSDKDriverPythonPath) {
			heredocFound = true
			// Find the delimiter name (last token on the cat line, strip heredoc single-quotes)
			fields := strings.Fields(line)
			delimiter := strings.Trim(fields[len(fields)-1], "'")
			// Scan forward: immediately after the last non-blank content line
			// we expect the delimiter, not a blank line.
			delimiterFound := false
			for j := i + 1; j < len(lines); j++ {
				trimmed := strings.TrimSpace(lines[j])
				if trimmed == delimiter {
					delimiterFound = true
					break
				}
				if trimmed == "" {
					t.Errorf("blank line at position %d found before heredoc delimiter %q; source trailing newline was not trimmed", j, delimiter)
					break
				}
			}
			assert.True(t, delimiterFound, "heredoc delimiter %q must be present after content lines", delimiter)
			break
		}
	}
	assert.True(t, heredocFound, "expected a heredoc block writing to %s in the generated step", inlineCopilotSDKDriverPythonPath)
}
