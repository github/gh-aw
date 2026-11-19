//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWheelSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func pipEngineDefinition(t *testing.T, requirements string) *EngineDefinition {
	t.Helper()
	source := `
id: py-agent
display-name: Py Agent
version: "1.2.0"
behaviors:
  installation:
    package-manager: pip
    package-name: Py_Agent
    verify-command: python3 -c 'print(1)'
    requirements:
` + requirements + `
  execution:
    command-name: python3
    args: ["-I", "-c", "print(1)"]
`
	var def EngineDefinition
	require.NoError(t, yaml.Unmarshal([]byte(source), &def))
	return &def
}

func TestPipInstallationValidation(t *testing.T) {
	valid := `      - name: py-agent
        version: "1.2.0"
        sha256: ` + testWheelSHA256 + `
      - name: requests
        version: "2.32.3"
        sha256: ` + testWheelSHA256
	_, err := NewBehaviorDefinedEngine(pipEngineDefinition(t, valid))
	require.NoError(t, err)

	tests := []struct {
		name, requirements, errContains string
	}{
		{"missing engine package", `      - {name: requests, version: "2.32.3", sha256: ` + testWheelSHA256 + `}`, `must pin package "Py_Agent" at version 1.2.0`},
		{"engine version mismatch", `      - {name: py.agent, version: "1.1.0", sha256: ` + testWheelSHA256 + `}`, "but the engine version is 1.2.0"},
		{"range version", `      - {name: py-agent, version: ">=1.2.0", sha256: ` + testWheelSHA256 + `}`, "must be an exact version"},
		{"missing hash", `      - {name: py-agent, version: "1.2.0"}`, "sha256 must be 64 lowercase hex"},
		{"url name", `      - {name: "https://example.com/x.whl", version: "1.2.0", sha256: ` + testWheelSHA256 + `}`, "not a valid distribution name"},
		{"duplicate", `      - {name: py-agent, version: "1.2.0", sha256: ` + testWheelSHA256 + `}
      - {name: PY_AGENT, version: "1.2.0", sha256: ` + testWheelSHA256 + `}`, "duplicates package"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewBehaviorDefinedEngine(pipEngineDefinition(t, test.requirements))
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.errContains)
		})
	}
}

func TestPipInstallationWithoutRequirementsKeepsCustomSteps(t *testing.T) {
	def := pipEngineDefinition(t, `      []`)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	for _, step := range engine.GetInstallationSteps(&WorkflowData{}) {
		assert.NotContains(t, strings.Join(step, "\n"), "pip install")
	}
	assert.NotContains(t, strings.Join(engine.GetExecutionSteps(&WorkflowData{Name: "py"}, "/tmp/log")[0], "\n"), "GH_AW_PYTHON_PACKAGES")

	def = pipEngineDefinition(t, `      - {name: py-agent, version: "1.2.0", sha256: `+testWheelSHA256+`}`)
	def.Behaviors.Installation.PackageManager = "npm"
	_, err = NewBehaviorDefinedEngine(def)
	require.ErrorContains(t, err, "'requirements' requires package-manager 'pip'")
}

func TestPipInstallationRendering(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(pipEngineDefinition(t, `      - {name: py-agent, version: "1.2.0", sha256: `+testWheelSHA256+`}`))
	require.NoError(t, err)

	data := &WorkflowData{EngineConfig: &EngineConfig{Version: "1.3.0"}}
	var installation strings.Builder
	for _, step := range engine.GetInstallationSteps(data) {
		installation.WriteString(strings.Join(step, "\n") + "\n")
	}
	rendered := installation.String()
	assert.Contains(t, rendered, "            py-agent==1.2.0 --hash=sha256:"+testWheelSHA256)
	assert.Contains(t, rendered, "--only-binary=:all: --require-hashes")
	assert.NotContains(t, rendered, "--no-deps", "pip must resolve dependencies so an incomplete lock fails")
	assert.Contains(t, rendered, `GH_AW_PYTHON_PACKAGE_VERSION: "1.3.0"`, "engine.version overrides are verified against the lock at runtime")
	assert.Contains(t, rendered, "GH_AW_PYTHON_PACKAGES: /tmp/gh-aw/agent/py-agent-python/site-packages")
	assert.Contains(t, rendered, "Verify Py Agent installation")
	assert.Empty(t, engine.GetSecretValidationStep(data))

	execution := strings.Join(engine.GetExecutionSteps(&WorkflowData{Name: "py"}, "/tmp/log")[0], "\n")
	assert.Contains(t, execution, "GH_AW_PYTHON_PACKAGES: /tmp/gh-aw/agent/py-agent-python/site-packages")
	assert.NotContains(t, execution, "PYTHONPATH")
}

func TestSharedPythonEnginesUseHashLockedInstallation(t *testing.T) {
	for _, test := range []struct{ path, pkg string }{
		{"../../.github/workflows/shared/aider.md", "aider-chat"},
		{"../../.github/workflows/shared/langgraph.md", "langgraph"},
	} {
		t.Run(test.pkg, func(t *testing.T) {
			var frontmatter struct {
				Engine EngineDefinition `yaml:"engine"`
			}
			readConformanceFrontmatter(t, test.path, &frontmatter)
			def := &frontmatter.Engine
			require.NotNil(t, def.Behaviors.Installation)
			assert.Equal(t, "pip", def.Behaviors.Installation.PackageManager)
			assert.Equal(t, test.pkg, def.Behaviors.Installation.PackageName)
			engine, err := NewBehaviorDefinedEngine(def)
			require.NoError(t, err)
			assert.Contains(t, strings.Join(def.Behaviors.Execution.Args, "\n"), `sys.path.insert(0, os.environ["GH_AW_PYTHON_PACKAGES"])`)
			var installation strings.Builder
			for _, step := range engine.GetInstallationSteps(&WorkflowData{}) {
				installation.WriteString(strings.Join(step, "\n") + "\n")
			}
			assert.Contains(t, installation.String(), test.pkg+"=="+def.Version+" --hash=sha256:")
		})
	}
}
