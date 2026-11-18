package workflow

import (
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/constants"
)

var awfConfigWritePattern = regexp.MustCompile(`printf '%s\\n' "(\{(?:[^"\\]|\\.)*)" > "(?:\$\{RUNNER_TEMP\}/gh-aw|\$\{GH_AW_SETUP_ROOT\})/awf-config\.json"`)
var shellEscapePattern = regexp.MustCompile(`\\(.)`)

func workflowRuntimePath(path string) string {
	return runtimeRootExpression + strings.TrimPrefix(path, constants.TmpGhAwDir)
}

func workflowRuntimeShellPath(path string) string {
	return strings.ReplaceAll(workflowRuntimePath(path), runtimeRootExpression, "${GH_AW_RUNTIME_ROOT}")
}

func workflowSetupShellPath(path string) string {
	path = strings.ReplaceAll(path, constants.GhAwRootDirShell, "${GH_AW_SETUP_ROOT}")
	return strings.ReplaceAll(path, constants.TmpGhAwDir, "${GH_AW_SETUP_ROOT}")
}

func agentExecutionEnv(t *testing.T, lock string) map[string]string {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string            `yaml:"run"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(lock), &workflow))
	for _, step := range workflow.Jobs["agent"].Steps {
		if strings.Contains(step.Run, `awf --config "${GH_AW_SETUP_ROOT}/awf-config.json"`) ||
			strings.Contains(step.Run, `awf --config "${RUNNER_TEMP}/gh-aw/awf-config.json"`) {
			return step.Env
		}
	}
	t.Fatal("agent job has no AWF execution step")
	return nil
}
