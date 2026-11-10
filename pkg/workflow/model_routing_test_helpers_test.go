package workflow

import (
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

var awfConfigWritePattern = regexp.MustCompile(`printf '%s\\n' "(\{(?:[^"\\]|\\.)*)" > "\$\{RUNNER_TEMP\}/gh-aw/awf-config\.json"`)
var shellEscapePattern = regexp.MustCompile(`\\(.)`)

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
		if strings.Contains(step.Run, `awf --config "${RUNNER_TEMP}/gh-aw/awf-config.json"`) {
			return step.Env
		}
	}
	t.Fatal("agent job has no AWF execution step")
	return nil
}
