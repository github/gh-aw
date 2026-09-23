//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// secretsReferencePattern matches a direct reference to a repository/organization secret,
// such as "secrets.MY_TOKEN" inside a ${{ }} expression. The leading character class avoids
// matching identifiers that merely end in "secrets", such as "steps.validate-secrets.outputs".
var secretsReferencePattern = regexp.MustCompile(`(^|[^-A-Za-z0-9_.])secrets\.[A-Za-z_][A-Za-z0-9_]*`)

// secretOutputViolation describes a workflow output whose value references a secret.
type secretOutputViolation struct {
	// Location identifies where the output is declared, e.g. "jobs.agent.outputs" or
	// "on.workflow_call.outputs".
	Location string
	// Name is the output key.
	Name string
	// Value is the offending output expression.
	Value string
}

func (v secretOutputViolation) String() string {
	return fmt.Sprintf("%s.%s = %s", v.Location, v.Name, v.Value)
}

// findSecretReferencingOutputs parses a compiled GitHub Actions workflow and returns every
// declared output whose value references secrets directly. It inspects the actual YAML
// "outputs:" maps (job outputs and on.workflow_call outputs) rather than relying on
// line-proximity text matching, so it produces no false positives from unrelated nearby lines.
func findSecretReferencingOutputs(lockContent []byte) ([]secretOutputViolation, error) {
	var workflow map[string]any
	if err := yaml.Unmarshal(lockContent, &workflow); err != nil {
		return nil, err
	}

	var violations []secretOutputViolation

	collect := func(location string, outputs any) {
		outputsMap, ok := outputs.(map[string]any)
		if !ok {
			return
		}
		names := make([]string, 0, len(outputsMap))
		for name := range outputsMap {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, value := range outputExpressions(outputsMap[name]) {
				if secretsReferencePattern.MatchString(value) {
					violations = append(violations, secretOutputViolation{
						Location: location,
						Name:     name,
						Value:    value,
					})
				}
			}
		}
	}

	// on.workflow_call.outputs: each output is a map with a "value" expression.
	if on, ok := workflow["on"].(map[string]any); ok {
		if workflowCall, ok := on["workflow_call"].(map[string]any); ok {
			collect("on.workflow_call.outputs", workflowCall["outputs"])
		}
	}

	// jobs.<job>.outputs: each output maps directly to an expression.
	if jobs, ok := workflow["jobs"].(map[string]any); ok {
		jobNames := make([]string, 0, len(jobs))
		for jobName := range jobs {
			jobNames = append(jobNames, jobName)
		}
		sort.Strings(jobNames)
		for _, jobName := range jobNames {
			job, ok := jobs[jobName].(map[string]any)
			if !ok {
				continue
			}
			collect(fmt.Sprintf("jobs.%s.outputs", jobName), job["outputs"])
		}
	}

	return violations, nil
}

// outputExpressions normalizes an output declaration into the list of expression strings it
// contains. Job outputs are plain strings, while workflow_call outputs are maps holding a
// "value" key.
func outputExpressions(output any) []string {
	switch value := output.(type) {
	case string:
		return []string{value}
	case map[string]any:
		if inner, ok := value["value"]; ok {
			return outputExpressions(inner)
		}
	}
	return nil
}

// TestCompiledLockFiles_NoSecretsInOutputs is a deterministic guard against secrets leaking
// through workflow outputs, which are persisted in the workflow run and readable by any job
// that consumes them. It replaces the ad hoc proximity grep previously used to spot-check this.
func TestCompiledLockFiles_NoSecretsInOutputs(t *testing.T) {
	lockFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	require.NoError(t, err, "should glob .lock.yml workflow files")
	require.NotEmpty(t, lockFiles, "should find at least one compiled .lock.yml file")

	for _, lockFile := range lockFiles {
		lockContent, err := os.ReadFile(lockFile)
		require.NoError(t, err, "should read %s", lockFile)

		violations, err := findSecretReferencingOutputs(lockContent)
		require.NoError(t, err, "should parse %s as YAML", lockFile)

		for _, violation := range violations {
			assert.Fail(t, "secret referenced in workflow output",
				"%s declares an output that exposes a secret: %s", filepath.Base(lockFile), violation)
		}
	}
}

// TestFindSecretReferencingOutputs verifies the detector reports direct secret references in
// job and workflow_call outputs, and does not report unrelated nearby usage of secrets.
func TestFindSecretReferencingOutputs(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "clean workflow with secrets used only in env",
			yaml: `
"on":
  workflow_call:
    outputs:
      created_issue_number:
        value: ${{ jobs.safe_outputs.outputs.created_issue_number }}
jobs:
  agent:
    outputs:
      output: ${{ steps.collect.outputs.output }}
    steps:
      - name: Run
        env:
          TOKEN: ${{ secrets.MY_TOKEN }}
        run: echo hello
`,
			expected: nil,
		},
		{
			name: "job output referencing a secret",
			yaml: `
jobs:
  agent:
    outputs:
      token: ${{ secrets.MY_TOKEN }}
    steps:
      - run: echo hello
`,
			expected: []string{"jobs.agent.outputs.token = ${{ secrets.MY_TOKEN }}"},
		},
		{
			name: "workflow_call output referencing a secret",
			yaml: `
"on":
  workflow_call:
    outputs:
      token:
        description: leaky
        value: ${{ secrets.MY_TOKEN }}
jobs:
  agent:
    steps:
      - run: echo hello
`,
			expected: []string{"on.workflow_call.outputs.token = ${{ secrets.MY_TOKEN }}"},
		},
		{
			name: "output name mentioning secrets without referencing one",
			yaml: `
jobs:
  agent:
    outputs:
      secrets_count: ${{ steps.scan.outputs.secrets_count }}
    steps:
      - run: echo hello
`,
			expected: nil,
		},
		{
			name: "step id ending in secrets is not a secret reference",
			yaml: `
jobs:
  activation:
    outputs:
      validation_result: ${{ steps.validate-secrets.outputs.verification_result }}
    steps:
      - run: echo hello
`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations, err := findSecretReferencingOutputs([]byte(tt.yaml))
			require.NoError(t, err)

			var actual []string
			for _, violation := range violations {
				actual = append(actual, violation.String())
			}
			assert.Equal(t, tt.expected, actual)
		})
	}
}
