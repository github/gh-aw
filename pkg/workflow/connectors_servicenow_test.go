//go:build !integration

package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"gopkg.in/yaml.v3"
)

func serviceNowCompilerFixture(t *testing.T, name string) (*Compiler, string, string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "pkg/connectors/testdata/providers/servicenow", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	compiler.gitRoot = root
	compiler.SetActionMode(ActionModeDev)
	compiler.version = "v0.0.0-1-gabcdef0"
	return compiler, path, string(content)
}

func TestServiceNowWorkflowCompilation(t *testing.T) {
	tests := []struct {
		file                  string
		tools, context, write bool
	}{
		{"workflow.md", true, true, true},
		{"workflow-tools-only.md", true, false, false},
		{"workflow-context-only.md", false, true, false},
		{"workflow-effects-only.md", false, false, true},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			compiler, path, content := serviceNowCompilerFixture(t, test.file)
			data, err := compiler.ParseWorkflowString(content, path)
			if err != nil {
				t.Fatal(err)
			}
			config := data.Connectors["servicenow"]
			if config == nil || (config.Agent != nil) != test.tools || (config.Context != nil) != test.context || (config.SafeOutputs != nil) != test.write {
				t.Fatal("omitted connector capabilities must remain disabled")
			}
			if test.tools && !slices.Equal(config.Agent.Secrets(), []string{"SERVICENOW_READ_TOKEN"}) {
				t.Fatal("read host must receive only the read credential")
			}
			if test.context && !slices.Equal(config.Context.Secrets(), []string{"SERVICENOW_CONTEXT_TOKEN"}) {
				t.Fatal("context host must receive only its credential")
			}
			if test.write && !slices.Equal(config.SafeOutputs.Secrets(), []string{"SERVICENOW_WRITE_TOKEN"}) {
				t.Fatal("effect host must receive only the write credential")
			}
			output, err := compiler.CompileToYAML(data, path)
			if err != nil {
				t.Fatal(err)
			}
			assertServiceNowCompiledJobs(t, output, test.tools, test.context, test.write)
		})
	}
}

func assertServiceNowCompiledJobs(t *testing.T, output string, tools, context, write bool) {
	t.Helper()
	var document map[string]any
	if err := yaml.Unmarshal([]byte(output), &document); err != nil {
		t.Fatal(err)
	}
	jobs, ok := document["jobs"].(map[string]any)
	if !ok {
		t.Fatal("compiled workflow has no jobs")
	}
	agent, err := json.Marshal(jobs["agent"])
	if err != nil {
		t.Fatal(err)
	}
	effects, err := json.Marshal(jobs["safe_outputs"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(agent), "${{ secrets.SERVICENOW_WRITE_TOKEN }}") {
		t.Fatal("agent job must not receive the write token")
	}
	for secret, selected := range map[string]bool{"SERVICENOW_READ_TOKEN": tools, "SERVICENOW_CONTEXT_TOKEN": context} {
		if strings.Contains(string(agent), "${{ secrets."+secret+" }}") != selected {
			t.Fatalf("agent credential selection does not match %s capability", secret)
		}
		if strings.Contains(string(effects), "${{ secrets."+secret+" }}") {
			t.Fatal("effect job must not receive read/context credentials")
		}
	}
	if strings.Contains(string(effects), "${{ secrets.SERVICENOW_WRITE_TOKEN }}") != write {
		t.Fatal("effect credential selection does not match declared capability")
	}
	if strings.Contains(string(agent), "connectors invoke context") != context {
		t.Fatal("context hydration must be emitted only when selected")
	}
}

func TestServiceNowWorkflowIngressRejected(t *testing.T) {
	compiler, path, content := serviceNowCompilerFixture(t, "workflow-ingress-unavailable.md")
	_, err := compiler.ParseWorkflowString(content, path)
	if !errors.Is(err, connectors.ErrIngressUnavailable) {
		t.Fatalf("connector subscription must fail closed pending approved ingress: %v", err)
	}
}

func TestServiceNowWorkflowInvalidSelections(t *testing.T) {
	tests := []struct {
		name, original, replacement string
	}{
		{"unknown-tool", "tools: [get-incident]", "tools: [get-any-record]"},
		{"write-as-read-tool", "tools: [get-incident]", "tools: [append-incident-work-note]"},
		{"wildcard-tool", "tools: [get-incident]", `tools: ["*"]`},
		{"legacy-scattered-selector", "connectors:\n  servicenow:", "tools:\n  connectors:\n    servicenow:"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiler, path, content := serviceNowCompilerFixture(t, "workflow-tools-only.md")
			content = strings.Replace(content, test.original, test.replacement, 1)
			if _, err := compiler.ParseWorkflowString(content, path); err == nil {
				t.Fatal("invalid provider selector accepted")
			}
		})
	}
	for _, replacement := range []string{"INC0009009", serviceNowOtherRecordID} {
		t.Run("context-record-"+replacement, func(t *testing.T) {
			compiler, path, content := serviceNowCompilerFixture(t, "workflow-context-only.md")
			content = strings.Replace(content, "fedcba9876543210fedcba9876543210", replacement, 1)
			if _, err := compiler.ParseWorkflowString(content, path); err == nil {
				t.Fatal("invalid context target accepted")
			}
		})
	}
}

const serviceNowOtherRecordID = "11111111111111111111111111111111"
