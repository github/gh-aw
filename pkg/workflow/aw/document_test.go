package aw

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocumentRoundTrip(t *testing.T) {
	for _, content := range []string{
		"",
		"# Plain Markdown\n\n  Preserve indentation.  \n",
		"---\nengine: copilot\n---\n\n# Mission\n\n---\n\n```yaml\non: push\n```\n",
		"---\n---",
		"---\n# Shared instructions\n---\n\nPrompt.\n",
		"---\n \t\n---\n",
		"---\nredirect: owner/repo/workflow.md@main\n---",
		"---\nengine: copilot\non: daily\nsafe-outputs:\n  create-issue:\n    max: 1\n---\n\n# Mission\n\nDo work.  \n",
		" \t--- \r\n# Comment\r\non: [push, issues]\r\nengine: {id: copilot}\r\n--- \r\n\r\n# Mission  \r\n",
	} {
		t.Run(content, func(t *testing.T) {
			document, err := Parse(content)
			require.NoError(t, err)
			emitted, err := Emit(document)
			require.NoError(t, err)
			assert.Equal(t, content, emitted)
			expected, err := parser.ExtractFrontmatterFromContent(content)
			require.NoError(t, err)
			assert.Equal(t, expected, document.FrontmatterResult(), "compiler view must preserve parser behavior")
		})
	}
}

func TestDocumentEnvironmentDeployment(t *testing.T) {
	source := `---
environment: &environment
  name: protected-secrets
  deployment: false # secrets only
safe-outputs:
  environment: *environment
  steps:
    - run: echo public
jobs:
  build:
    environment: *environment
    steps:
      - run: echo public
---

# Prompt
`
	document, err := Parse(source)
	require.NoError(t, err)
	emitted, err := Emit(document)
	require.NoError(t, err)
	assert.Equal(t, source, emitted)
	blocks, err := ExecutionBlocks(context.Background(), document)
	require.NoError(t, err)
	require.Len(t, blocks, 3)
	for _, block := range blocks {
		assert.Equal(t, map[string]any{"name": "protected-secrets", "deployment": false}, block.Fields["environment"], block.Path)
	}

	view := document.FrontmatterResult()
	view.Frontmatter["environment"].(map[string]any)["deployment"] = true
	assert.Equal(t, false, document.Frontmatter["environment"].(map[string]any)["deployment"], "compiler view must not mutate the AST")

	document.Frontmatter["engine"] = "copilot"
	emitted, err = Emit(document)
	require.NoError(t, err)
	reparsed, err := Parse(emitted)
	require.NoError(t, err)
	assert.Equal(t, document.Frontmatter, reparsed.Frontmatter)
}

func TestDocumentRunsOnRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		source string
		value  any
	}{
		{"scalar", "ubuntu-latest", "ubuntu-latest"},
		{"single quoted scalar", "'ubuntu-latest'", "ubuntu-latest"},
		{"double quoted scalar", `"ubuntu-latest"`, "ubuntu-latest"},
		{"quoted special characters", "'runner: linux # large'", "runner: linux # large"},
		{"expression", "${{ matrix.runner }}", "${{ matrix.runner }}"},
		{"quoted expression", `"${{ inputs.runner }}"`, "${{ inputs.runner }}"},
		{"array expression", `${{ fromJSON('["self-hosted", "linux"]') }}`, `${{ fromJSON('["self-hosted", "linux"]') }}`},
		{"folded expression", ">-\n  ${{ inputs.runner ||\n      'ubuntu-latest' }}", "${{ inputs.runner ||\n    'ubuntu-latest' }}"},
		{"single label", "[self-hosted]", []any{"self-hosted"}},
		{"single block label", "\n  - self-hosted", []any{"self-hosted"}},
		{"flow labels", "[self-hosted, linux, x64, something]", []any{"self-hosted", "linux", "x64", "something"}},
		{"multiline flow labels", "[\n  self-hosted,\n  linux,\n]", []any{"self-hosted", "linux"}},
		{"block labels", "\n  - self-hosted\n  - linux\n  - x64", []any{"self-hosted", "linux", "x64"}},
		{"indentless labels", "\n- self-hosted\n- linux", []any{"self-hosted", "linux"}},
		{"group", "{group: my-runners}", map[string]any{"group": "my-runners"}},
		{"block group", "\n  group: my-runners", map[string]any{"group": "my-runners"}},
		{"label only", "{labels: linux}", map[string]any{"labels": "linux"}},
		{"labels only", "{labels: [self-hosted, linux]}", map[string]any{"labels": []any{"self-hosted", "linux"}}},
		{"group and label", "{group: my-runners, labels: linux}", map[string]any{"group": "my-runners", "labels": "linux"}},
		{"flow group and labels", "{group: my-runners, labels: [linux, x64]}", map[string]any{"group": "my-runners", "labels": []any{"linux", "x64"}}},
		{"group and labels", "\n  group: my-runners\n  labels:\n    - linux\n    - x64", map[string]any{"group": "my-runners", "labels": []any{"linux", "x64"}}},
		{"group and indentless labels", "\n  group: my-runners\n  labels:\n  - linux\n  - x64", map[string]any{"group": "my-runners", "labels": []any{"linux", "x64"}}},
		{"group expression", "{group: '${{ inputs.group }}'}", map[string]any{"group": "${{ inputs.group }}"}},
		{"labels expression", "{labels: '${{ matrix.labels }}'}", map[string]any{"labels": "${{ matrix.labels }}"}},
		{"group and label expressions", "{group: '${{ inputs.group }}', labels: [self-hosted, '${{ matrix.os }}']}", map[string]any{"group": "${{ inputs.group }}", "labels": []any{"self-hosted", "${{ matrix.os }}"}}},
		{"expression label", "[self-hosted, '${{ matrix.os }}']", []any{"self-hosted", "${{ matrix.os }}"}},
		{"anchored scalar", "&runner ubuntu-latest", "ubuntu-latest"},
		{"anchored labels", "&runners [self-hosted, linux]", []any{"self-hosted", "linux"}},
		{"anchored group", "&runners {group: my-runners, labels: [linux, x64]}", map[string]any{"group": "my-runners", "labels": []any{"linux", "x64"}}},
		{"aliased label", "[&label self-hosted, *label]", []any{"self-hosted", "self-hosted"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := "runs-on: " + tt.source + "\n"
			source := "---\nengine: copilot\n" + field +
				"jobs:\n  build:\n    " + strings.ReplaceAll(strings.TrimSuffix(field, "\n"), "\n", "\n    ") + "\n" +
				"safe-outputs:\n  jobs:\n    deploy:\n      " + strings.ReplaceAll(strings.TrimSuffix(field, "\n"), "\n", "\n      ") +
				"\n---\n\n# Prompt\n\nKeep these bytes.  \n"
			document, err := Parse(source)
			require.NoError(t, err)
			emitted, err := Emit(document)
			require.NoError(t, err)
			assert.Equal(t, source, emitted)
			blocks, err := ExecutionBlocks(context.Background(), document)
			require.NoError(t, err)
			require.Len(t, blocks, 3)
			for _, block := range blocks {
				assert.Equal(t, tt.value, block.Fields["runs-on"], block.Path)
			}

			document.Frontmatter["engine"] = "claude"
			emitted, err = Emit(document)
			require.NoError(t, err)
			reparsed, err := Parse(emitted)
			require.NoError(t, err)
			assert.Equal(t, document.Frontmatter, reparsed.Frontmatter, "rewriting frontmatter must preserve runner types and values")
			assert.Equal(t, document.Markdown, reparsed.Markdown)
		})
	}
}

func TestDocumentRunsOnEdits(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		source string
		value  any
	}{
		{"root scalar to list", "root", "ubuntu-latest", []any{"self-hosted", "linux"}},
		{"root block list to group mapping", "root", "\n  - self-hosted\n  - linux\n  - x64", map[string]any{"group": "my-runners"}},
		{"root mapping to scalar", "root", "{group: my-runners}", "ubuntu-latest"},
		{"custom job scalar to mapping", "custom", "ubuntu-latest", map[string]any{"group": "my-runners"}},
		{"custom job mapping to list", "custom", "{group: my-runners}", []any{"self-hosted", "linux"}},
		{"custom job nested label to labels", "custom", "{group: my-runners, labels: linux}", map[string]any{"group": "my-runners", "labels": []any{"linux", "x64"}}},
		{"safe-output job list to scalar", "safe-output", "[self-hosted, linux]", "ubuntu-latest"},
		{"safe-output job nested labels to label", "safe-output", "\n  group: my-runners\n  labels:\n    - linux\n    - x64", map[string]any{"group": "my-runners", "labels": "linux"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := "runs-on: " + tt.source + "\n"
			source := "---\nengine: copilot\n" + field +
				"jobs:\n  build:\n    " + strings.ReplaceAll(strings.TrimSuffix(field, "\n"), "\n", "\n    ") + "\n" +
				"safe-outputs:\n  jobs:\n    deploy:\n      " + strings.ReplaceAll(strings.TrimSuffix(field, "\n"), "\n", "\n      ") +
				"\n---\n\n# Prompt\n\nKeep these bytes.  \n"
			document, err := Parse(source)
			require.NoError(t, err)
			switch tt.field {
			case "root":
				document.Frontmatter["runs-on"] = tt.value
			case "custom":
				document.Frontmatter["jobs"].(map[string]any)["build"].(map[string]any)["runs-on"] = tt.value
			case "safe-output":
				document.Frontmatter["safe-outputs"].(map[string]any)["jobs"].(map[string]any)["deploy"].(map[string]any)["runs-on"] = tt.value
			default:
				t.Fatalf("unknown runner field %q", tt.field)
			}
			emitted, err := Emit(document)
			require.NoError(t, err)
			reparsed, err := Parse(emitted)
			require.NoError(t, err)
			assert.Equal(t, document.Frontmatter, reparsed.Frontmatter)
			assert.Equal(t, document.Markdown, reparsed.Markdown)
			again, err := Emit(reparsed)
			require.NoError(t, err)
			assert.Equal(t, emitted, again, "edited runner emission must be stable")
		})
	}
}

func TestDocumentRunsOnAliasRoundTrip(t *testing.T) {
	for _, runner := range []string{"ubuntu-latest", "[self-hosted, linux]", "{group: my-runners, labels: [linux, x64]}"} {
		t.Run(runner, func(t *testing.T) {
			source := "---\nengine: copilot\nruns-on: &runners " + runner +
				"\njobs:\n  build:\n    runs-on: *runners\n---\n\n# Prompt\n"
			document, err := Parse(source)
			require.NoError(t, err)
			emitted, err := Emit(document)
			require.NoError(t, err)
			assert.Equal(t, source, emitted)

			document.Frontmatter["runs-on"] = "windows-latest"
			emitted, err = Emit(document)
			require.NoError(t, err)
			reparsed, err := Parse(emitted)
			require.NoError(t, err)
			assert.Equal(t, document.Frontmatter, reparsed.Frontmatter, "editing an anchor must not change custom job runners")
			assert.Equal(t, document.Markdown, reparsed.Markdown)
		})
	}
}

func TestDocumentEditsPreserveUntouchedRegions(t *testing.T) {
	content := "---\r\n# Keep this header comment\r\nengine: copilot\r\n---\r\n\r\n# Prompt  \r\n"
	document, err := Parse(content)
	require.NoError(t, err)
	assert.Equal(t, "\r\n# Prompt  \r\n", document.Markdown)
	assert.Equal(t, 2, document.Location.FrontmatterStart)
	assert.Equal(t, 3, document.Location.FieldLines["engine"])
	document.Markdown = "\n# Edited prompt\n"
	emitted, err := Emit(document)
	require.NoError(t, err)
	assert.Equal(t, "---\r\n# Keep this header comment\r\nengine: copilot\r\n---\r\n\n# Edited prompt\n", emitted)
	document, err = Parse(content)
	require.NoError(t, err)
	document.Frontmatter["engine"] = map[string]any{"id": "copilot", "model": "gpt-5.4"}
	emitted, err = Emit(document)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(emitted, "\r\n# Prompt  \r\n"), "frontmatter edits must not change prompt bytes")
	reparsed, err := Parse(emitted)
	require.NoError(t, err)
	assert.Equal(t, document.Frontmatter, reparsed.Frontmatter)
	assert.Equal(t, document.Markdown, reparsed.Markdown)
}

func TestDocumentProgrammaticConstruction(t *testing.T) {
	document := &Document{
		Frontmatter: map[string]any{
			"on": "workflow_dispatch", "engine": "copilot",
			"tools":        map[string]any{"bash": []any{"echo"}},
			"safe-outputs": map[string]any{"create-issue": map[string]any{"max": 1}},
		},
		Markdown: "\n# Mission\n\nDo work.\n",
	}
	content, err := Emit(document)
	require.NoError(t, err)
	parsed, err := Parse(content)
	require.NoError(t, err)
	assert.Equal(t, document.Markdown, parsed.Markdown)
	assert.Equal(t, "copilot", parsed.Frontmatter["engine"])
	assert.Contains(t, parsed.Frontmatter, "safe-outputs")
	plain, err := Emit(&Document{Markdown: "# Plain\n"})
	require.NoError(t, err)
	assert.Equal(t, "# Plain\n", plain)
}

func TestDocumentParserViewIsIndependent(t *testing.T) {
	source := "---\nengine: copilot\ntools:\n  bash: [echo]\n---\n\n# Prompt\n"
	document, err := Parse(source)
	require.NoError(t, err)
	view := document.FrontmatterResult()
	view.Frontmatter["tools"].(map[string]any)["bash"].([]any)[0] = "changed"
	view.Frontmatter["engine"] = "changed"
	view.FieldLines["engine"] = 100
	view.FrontmatterLines[0] = "changed"
	after, err := Emit(document)
	require.NoError(t, err)
	assert.Equal(t, source, after)
	assert.Equal(t, "copilot", document.Frontmatter["engine"])
	assert.Equal(t, 2, document.Location.FieldLines["engine"])
}

func TestDocumentErrors(t *testing.T) {
	for _, content := range []string{"---\non: push", "---\non: [\n---\nPrompt"} {
		_, err := Parse(content)
		require.Error(t, err)
	}
	_, err := Emit(nil)
	require.ErrorContains(t, err, "cannot be nil")
	_, err = Emit(&Document{Frontmatter: map[string]any{"invalid": make(chan int)}})
	require.Error(t, err)
}

func TestRepositoryMarkdownDocuments(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", ".github", "workflows", "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			source, err := os.ReadFile(file)
			require.NoError(t, err)
			document, err := Parse(string(source))
			require.NoError(t, err)
			emitted, err := Emit(document)
			require.NoError(t, err)
			assert.Equal(t, string(source), emitted, "must preserve every AW field, comment, and prompt byte")
			expected, err := parser.ExtractFrontmatterFromContent(string(source))
			require.NoError(t, err)
			assert.Equal(t, expected, document.FrontmatterResult())
		})
	}
	t.Logf("Round-tripped all %d repository AW Markdown documents", len(files))
}
