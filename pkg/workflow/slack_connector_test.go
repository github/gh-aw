//go:build !integration

package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
)

func slackConnectorCompiler(t *testing.T) (*Compiler, string) {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{
		"pkg/connectors/testdata/providers/slack/spec.yaml",
		"pkg/connectors/testdata/providers/slack/connection.yaml",
	} {
		data, err := os.ReadFile(filepath.Join("../..", path))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	compiler.gitRoot = root
	compiler.SetActionMode(ActionModeDev)
	compiler.version = "v0.0.0-1-g683ddf9617"
	compiler.forceStaged = true
	return compiler, root
}

func TestSlackConnectorWorkflowCompilation(t *testing.T) {
	compiler, root := slackConnectorCompiler(t)
	source, err := os.ReadFile("../connectors/testdata/providers/slack/legacy/workflow.md")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "slack-proof.md")
	data, err := compiler.ParseWorkflowString(string(source), path)
	if err != nil {
		t.Fatal(err)
	}
	service := data.Connectors["slack"]
	if service == nil || service.Agent == nil || service.Context == nil || service.SafeOutputs == nil {
		t.Fatal("Slack service must lower all three selected executable parts")
	}
	output, err := compiler.CompileToYAML(data, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"connectors invoke context", "SLACK_CONTEXT_TOKEN", "SLACK_READ_TOKEN", "SLACK_WRITE_TOKEN", "connector_5_slack_reply_in_thread", "untrusted third-party data"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("compiled workflow missing %q", expected)
		}
	}
	for phase, plan := range map[string]*connectors.Plan{"agent": service.Agent, "context": service.Context, "safe-output": service.SafeOutputs} {
		expected := map[string][]string{"agent": {"SLACK_READ_TOKEN"}, "context": {"SLACK_CONTEXT_TOKEN"}, "safe-output": {"SLACK_WRITE_TOKEN"}}[phase]
		actual, _ := json.Marshal(plan.Secrets())
		want, _ := json.Marshal(expected)
		if string(actual) != string(want) {
			t.Fatalf("%s received wrong phase secrets: %s", phase, actual)
		}
	}
}

func TestSlackConnectorWorkflowIngressRejected(t *testing.T) {
	compiler, root := slackConnectorCompiler(t)
	source, err := os.ReadFile("../connectors/testdata/providers/slack/legacy/workflow-trigger-rejected.md")
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.ParseWorkflowString(string(source), filepath.Join(root, "slack-ingress.md"))
	if !errors.Is(err, connectors.ErrIngressUnavailable) || !strings.Contains(err.Error(), "connectors.slack: on:") {
		t.Fatalf("Slack ingress must fail closed with precise service diagnostic: %v", err)
	}
}
