package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

func TestParseCommandParserParity(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"structures and scalar types", `---
if: "${{ github.event_name == 'workflow_dispatch' }}"
imports: []
on:
  workflow_dispatch:
    inputs:
      enabled:
        type: boolean
        default: false
        required: true
      count:
        type: number
        default: 42
      ratio:
        default: 1.25
      label:
        type: string
        default: "false"
      unset:
        default: null
      choice:
        type: choice
        options: [one, "2"]
unknown-field: preserved
---
# Body
{{#import missing-body.md}}
`},
		{"missing fields", "---\nname: Only a name\n---\nBody"},
		{"missing frontmatter", "# Markdown only\n"},
		{"empty content", ""},
		{"empty frontmatter", "---\n---\n"},
		{"null frontmatter", "---\nnull\n---\n"},
		{"CRLF delimiters", "---\r\nif: false\r\non: push\r\n---\r\nBody\r\n"},
		{"no trailing newline", "---\nimports: []\non: {}\n---"},
		{"malformed YAML", "---\non: [unclosed\n---\n"},
		{"duplicate keys", "---\nif: true\nif: false\n---\n"},
		{"unclosed frontmatter", "---\non: push\n"},
		{"non-mapping frontmatter", "---\n- item\n---\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, parseErr := parser.ExtractFrontmatterFromContent(tt.content)
			for _, source := range []string{"file", "stdin"} {
				t.Run(source, func(t *testing.T) {
					cmd := NewParseCommand()
					var stdout, stderr bytes.Buffer
					cmd.SetOut(&stdout)
					cmd.SetErr(&stderr)
					cmd.SilenceErrors = true
					if source == "file" {
						path := filepath.Join(t.TempDir(), "workflow.md")
						if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
							t.Fatal(err)
						}
						cmd.SetArgs([]string{path})
					} else {
						cmd.SetIn(strings.NewReader(tt.content))
						cmd.SetArgs([]string{"-"})
					}
					err := cmd.Execute()
					if parseErr != nil {
						if err == nil || err.Error() != parseErr.Error() {
							t.Fatalf("error = %v, parser error = %v", err, parseErr)
						}
						if stdout.Len() != 0 {
							t.Fatalf("unexpected JSON on failure: %s", &stdout)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if _, exists := parsed.Frontmatter["imports"]; exists {
						parsed.Frontmatter["resolved-imports"] = []string{}
					}
					expected, err := json.Marshal(parsed.Frontmatter)
					if err != nil {
						t.Fatal(err)
					}
					if stdout.String() != string(expected)+"\n" {
						t.Fatalf("JSON = %s, want %s", &stdout, expected)
					}
					if stderr.Len() != 0 {
						t.Fatalf("unexpected stderr: %s", &stderr)
					}
					if tt.name == "structures and scalar types" {
						var decoded map[string]any
						if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
							t.Fatal(err)
						}
						inputs := decoded["on"].(map[string]any)["workflow_dispatch"].(map[string]any)["inputs"].(map[string]any)
						for name, want := range map[string]any{
							"enabled": false, "count": float64(42), "ratio": 1.25, "label": "false", "unset": nil,
						} {
							if got := inputs[name].(map[string]any)["default"]; got != want {
								t.Errorf("%s default = %#v, want %#v", name, got, want)
							}
						}
					}
				})
			}
		})
	}
}

func TestParseCommandCommittedContent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	dir := t.TempDir()
	runGit := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
		return output
	}
	runGit("init", "--quiet")
	path := filepath.Join(dir, "workflow.md")
	content := "---\nif: false\nimports: []\non:\n  workflow_dispatch:\n    inputs:\n      count:\n        default: 7\n---\nCommitted body\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "workflow.md")
	runGit("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture")
	commit := strings.TrimSpace(string(runGit("rev-parse", "HEAD")))
	if err := os.WriteFile(path, []byte("---\nif: true\n---\nChanged body\n"), 0600); err != nil {
		t.Fatal(err)
	}
	committed := runGit("show", commit+":workflow.md")
	if string(committed) != content {
		t.Fatal("git show did not preserve the committed Markdown")
	}
	parsed, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Frontmatter["resolved-imports"] = []string{}
	expected, err := json.Marshal(parsed.Frontmatter)
	if err != nil {
		t.Fatal(err)
	}
	cmd := NewParseCommand()
	cmd.SetArgs([]string{"-"})
	cmd.SetIn(bytes.NewReader(committed))
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if output.String() != string(expected)+"\n" {
		t.Fatalf("JSON = %s, want committed frontmatter %s", &output, expected)
	}
}

func TestParseCommandResolvedImports(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"root.md":   "---\nname: root\nimports: [parent.md, leaf.md]\n---\n",
		"parent.md": "---\nimports: [leaf.md, deep.md]\n---\n",
		"leaf.md":   "---\nname: leaf\n---\n",
		"deep.md":   "---\nname: deep\n---\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{filepath.Join(dir, "root.md")},
		{"-", "--base-dir", dir},
	} {
		cmd := NewParseCommand()
		cmd.SetArgs(args)
		cmd.SetIn(strings.NewReader(files["root.md"]))
		var output bytes.Buffer
		cmd.SetOut(&output)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Name     string   `json:"name"`
			Imports  []string `json:"imports"`
			Resolved []string `json:"resolved-imports"`
		}
		if err := json.Unmarshal(output.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		resolved := strings.Join(got.Resolved, ",")
		if got.Name != "root" || strings.Join(got.Imports, ",") != "parent.md,leaf.md" ||
			(resolved != "leaf.md,deep.md,parent.md" && resolved != "deep.md,leaf.md,parent.md") {
			t.Fatalf("unexpected output for %v: %s", args, &output)
		}
	}
}

func TestParseCommandImportErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cycle.md"), []byte("---\nimports: [cycle.md]\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("---\nimports: [unclosed\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{
		"---\nimports: [missing.md]\n---\n",
		"---\nimports: [cycle.md]\n---\n",
		"---\nimports: [broken.md]\n---\n",
		"---\nimports: [owner/repo@main]\n---\n",
		"---\nimports: []\nresolved-imports: keep\n---\n",
	} {
		cmd := NewParseCommand()
		cmd.SetArgs([]string{"-", "--base-dir", dir})
		cmd.SetIn(strings.NewReader(content))
		cmd.SilenceErrors = true
		var output bytes.Buffer
		cmd.SetOut(&output)
		if err := cmd.Execute(); err == nil || output.Len() != 0 {
			t.Fatalf("expected error and no output for %q: error = %v, output = %s", content, err, &output)
		}
	}
}

func TestParseCommandErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"-", "extra"}, {filepath.Join(t.TempDir(), "missing.md")}} {
		cmd := NewParseCommand()
		cmd.SetArgs(args)
		cmd.SilenceErrors = true
		var output bytes.Buffer
		cmd.SetOut(&output)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected error for arguments %v", args)
		}
		if output.Len() != 0 {
			t.Fatalf("unexpected output for arguments %v: %s", args, &output)
		}
	}

	sentinel := errors.New("I/O failure")
	t.Run("stdin read", func(t *testing.T) {
		cmd := NewParseCommand()
		cmd.SetArgs([]string{"-"})
		cmd.SetIn(parseFailingIO{sentinel})
		cmd.SilenceErrors = true
		var output bytes.Buffer
		cmd.SetOut(&output)
		if err := cmd.Execute(); !errors.Is(err, sentinel) || output.Len() != 0 {
			t.Fatalf("error = %v, output = %s", err, &output)
		}
	})
	t.Run("stdout write", func(t *testing.T) {
		cmd := NewParseCommand()
		cmd.SetArgs([]string{"-"})
		cmd.SetIn(strings.NewReader("# Body"))
		cmd.SetOut(parseFailingIO{sentinel})
		cmd.SilenceErrors = true
		if err := cmd.Execute(); !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want I/O failure", err)
		}
	})
}

func TestParseCommandSkipsRootHook(t *testing.T) {
	root := &cobra.Command{
		Use: "gh-aw",
		PersistentPreRun: func(*cobra.Command, []string) {
			t.Fatal("parse must not run the project configuration hook")
		},
	}
	var stderr, stdout bytes.Buffer
	root.SetOut(&stderr)
	cmd := NewParseCommand()
	cmd.SetOut(&stdout)
	root.AddCommand(cmd)
	root.SetIn(strings.NewReader("---\nif: false\n---\n"))
	root.SetArgs([]string{"parse", "-"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "{\"if\":false}\n" || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

type parseFailingIO struct {
	err error
}

func (f parseFailingIO) Read([]byte) (int, error)  { return 0, f.err }
func (f parseFailingIO) Write([]byte) (int, error) { return 0, f.err }
