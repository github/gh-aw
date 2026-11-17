package workflow

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestCompilerEmbedsActivationMarkdownMetadata(t *testing.T) {
	source := filepath.Join(t.TempDir(), "workflow.md")
	content := "---\non:\n  workflow_call:\nstrict: false\nengine: copilot\n---\n# Work\n\n" +
		"## agent: reader\n---\nmodel: ${{ inputs.model }}\ntools: [Read]\n---\nRead files.\n"
	require.NoError(t, os.WriteFile(source, []byte(content), 0o600))
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowFile(source)
	require.NoError(t, err)
	generated, err := compiler.CompileToYAML(data, source)
	require.NoError(t, err)
	var document struct {
		Jobs map[string]struct {
			Env map[string]string `yaml:"env"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(generated), &document))
	embedded := document.Jobs["activation"].Env["GH_AW_INFO_MARKDOWN_METADATA"]
	require.NotEmpty(t, embedded)
	decoded, err := base64.StdEncoding.DecodeString(embedded)
	require.NoError(t, err)
	require.Contains(t, generated, "      # Activation metadata preview: "+activationMetadataPreview(decoded)+"\n      GH_AW_INFO_MARKDOWN_METADATA: "+embedded)
	require.Contains(t, string(decoded), "${{ inputs.model }}", "authored expressions must survive as inert data")
	var catalog GHAWMarkdownMetadata
	require.NoError(t, json.Unmarshal(decoded, &catalog))
	require.Equal(t, 2, catalog.Version)
	require.NotEmpty(t, catalog.Records)
	for _, record := range catalog.Records {
		require.Empty(t, record.References, "embedded records must be self-contained")
	}
	require.NotEmpty(t, data.CompiledPromptMarkdown)
	for _, prompt := range data.CompiledPromptMarkdown {
		for _, block := range parser.ExtractMarkdownMetadataBlocks(prompt) {
			digest := sha256.Sum256([]byte(block.Source))
			require.Contains(t, string(decoded), hex.EncodeToString(digest[:]), "include post-substitution prompt metadata")
		}
	}
	require.Contains(t, string(decoded), `"Read"`)
	require.NotContains(t, generated, "model: ${{ inputs.model }}")
}

func TestCompilerEmbedsLargeActivationMarkdownMetadata(t *testing.T) {
	source := filepath.Join(t.TempDir(), "workflow.md")
	var content strings.Builder
	content.WriteString("---\non: workflow_dispatch\nstrict: false\nengine: copilot\n---\n# Work\n\n")
	for i := range 200 {
		fmt.Fprintf(&content, "## agent: reader-%d\n---\nmodel: model-%d-%s\ntools: [Read]\n---\nRead files.\n## end agent: reader-%d\n\n", i, i, strings.Repeat("x", 600), i)
	}
	require.NoError(t, os.WriteFile(source, []byte(content.String()), 0o600))
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowFile(source)
	require.NoError(t, err)
	generated, err := compiler.CompileToYAML(data, source)
	require.NoError(t, err)
	require.NoError(t, compiler.validateExpressionSizes(generated))
	var document struct {
		Jobs map[string]struct {
			Env   map[string]string `yaml:"env"`
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(generated), &document))
	activation := document.Jobs["activation"]
	require.NotContains(t, activation.Env, "GH_AW_INFO_MARKDOWN_METADATA")
	require.Equal(t, "${{ runner.temp }}/gh-aw-markdown-metadata.b64", activation.Env["GH_AW_INFO_MARKDOWN_METADATA_FILE"])
	runnerTemp := t.TempDir()
	chunks := 0
	for _, step := range activation.Steps {
		run := step.Run
		if chunks == 0 {
			_, run, _ = strings.Cut(run, "\n")
		}
		if !strings.HasPrefix(run, "printf '%s' '") {
			break
		}
		require.Less(t, len(step.Run), 21000)
		command := exec.Command("bash", "-e", "-c", step.Run)
		command.Env = append(os.Environ(), "RUNNER_TEMP="+runnerTemp)
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
		chunks++
	}
	require.Greater(t, chunks, 1)
	payload, err := os.ReadFile(filepath.Join(runnerTemp, "gh-aw-markdown-metadata.b64"))
	require.NoError(t, err)
	require.Greater(t, len(payload), 128*1024)
	decoded, err := base64.StdEncoding.DecodeString(string(payload))
	require.NoError(t, err)
	require.Contains(t, generated, "          # Activation metadata preview: "+activationMetadataPreview(decoded)+"\n          printf '%s' '")
	var catalog GHAWMarkdownMetadata
	require.NoError(t, json.Unmarshal(decoded, &catalog))
	require.Len(t, catalog.Records, 200)
}

func TestActivationMetadataPreview(t *testing.T) {
	require.Equal(t, `short`, activationMetadataPreview([]byte("short")))
	require.Equal(t, `\u0024{{ inputs.model }}\n\u2028\u0085`, activationMetadataPreview([]byte("${{ inputs.model }}\n\u2028\u0085")))
	require.Equal(t, strings.Repeat("x", 41)+`é...`, activationMetadataPreview([]byte(strings.Repeat("x", 41)+"éz")))
}

func TestCompilerActivationMarkdownMetadataErrors(t *testing.T) {
	compiler := NewCompiler()
	catalog := &GHAWMarkdownMetadata{Version: 2, Records: []GHAWMarkdownMetadataRecord{}}
	require.ErrorContains(t, compiler.embedActivationMarkdownMetadata(catalog), "activation job missing")
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "activation"}))
	catalog.Records = append(catalog.Records, GHAWMarkdownMetadataRecord{
		SHA256: strings.Repeat("a", 64),
		Kind:   "artifact",
		Value:  json.RawMessage(`{"model":"` + strings.Repeat("x", maxActivationMetadataBytes) + `"}`),
	})
	require.ErrorContains(t, compiler.embedActivationMarkdownMetadata(catalog), "4194304-byte limit")
}

func TestCompilerMarkdownMetadataReusesCatalogWithoutMutation(t *testing.T) {
	record := GHAWMarkdownMetadataRecord{
		SHA256: strings.Repeat("a", 64),
		Kind:   "artifact",
		Value:  json.RawMessage(`{"model":"small"}`),
	}
	catalog := &GHAWMarkdownMetadata{Version: 2, Records: []GHAWMarkdownMetadataRecord{record}}
	data := &WorkflowData{compiledMarkdownMetadata: catalog}
	compiler := NewCompiler()
	var header strings.Builder
	header.WriteString("# gh-aw-metadata: {\"agent_model\":\"small\"}\n")
	require.NoError(t, compiler.generateManifestHeader(&header, data, nil, nil))
	require.Contains(t, header.String(), `"metadata_refs"`)
	require.Equal(t, []GHAWMarkdownMetadataRecord{record}, catalog.Records, "manifest references must not leak into activation metadata")
}

func TestCompilerMarkdownMetadataManifest(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "agents"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "skills", "reader"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "guidance", "reader"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "agents", "reader.agent.md"), []byte("---\r\nmodel: small\r\ndescription: >-\r\n  Reads files\r\n---\r\nBody"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "skills", "reader", "SKILL.md"), []byte("---\ndescription: Reads skill files\n---\nBody"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "agents", "import.txt"), []byte("---\n{description: Imported text, tools: [Read]}\n---\nBody"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "guidance", "reader", "SKILL.md"), []byte("---\ndescription: Custom directory\n---\nBody"), 0o644))
	c := &Compiler{markdownPath: filepath.Join(root, ".github", "workflows", "main.md")}
	data := &WorkflowData{
		RawMarkdown:            "## agent: `inline`\n---\nmodel: large\ntools: [Read]\n---\nBody",
		CompiledPromptMarkdown: []string{"## skill: `dynamic`\n---\ndescription: __GH_AW_EXPR_TEST__\n---\nBody"},
		MainWorkflowMarkdown:   "{{#runtime-import .github/agents/import.txt}}",
		Skills:                 []string{"guidance"},
	}

	manifest := c.collectMarkdownMetadata(data)
	require.Equal(t, 2, manifest.Version)
	values := map[string]any{}
	for _, record := range manifest.Records {
		require.Empty(t, record.Error)
		var value any
		require.NoError(t, json.Unmarshal(record.Value, &value))
		values[record.SHA256] = value
	}
	hash := func(source string) string {
		value := sha256.Sum256([]byte(source))
		return hex.EncodeToString(value[:])
	}
	require.Equal(t, map[string]any{"model": "small"}, values[hash("model: small\ndescription: >-\n  Reads files")])
	require.NotContains(t, values, hash("description: Reads skill files"))
	require.Equal(t, map[string]any{"model": "large", "tools": []any{"Read"}}, values[hash("model: large\ntools: [Read]")])
	require.NotContains(t, values, hash("description: __GH_AW_EXPR_TEST__"))
	require.Equal(t, map[string]any{"tools": []any{"Read"}}, values[hash("{description: Imported text, tools: [Read]}")])
	require.NotContains(t, values, hash("description: Custom directory"))
}

func TestCompilerMarkdownMetadataDelimiterForms(t *testing.T) {
	for _, fence := range []string{"\uFEFF---", "  ---\t", "\uFEFF \t--- "} {
		t.Run(fence, func(t *testing.T) {
			records := map[string]GHAWMarkdownMetadataRecord{}
			scanMarkdownMetadata(records, fence+"\r\nmodel: small\r\ntools: [Read]\r\n \t--- \r\nBody", "artifact")
			require.Len(t, records, 1)
			for _, record := range records {
				require.Empty(t, record.Error)
				require.JSONEq(t, `{"model":"small","tools":["Read"]}`, string(record.Value))
			}
		})
	}
	records := map[string]GHAWMarkdownMetadataRecord{}
	scanMarkdownMetadata(records, "---\n---\nBody\n---\nmodel: forged\n---", "artifact")
	require.Len(t, records, 1)
	for _, record := range records {
		require.JSONEq(t, `{}`, string(record.Value))
	}
	records = map[string]GHAWMarkdownMetadataRecord{}
	scanMarkdownMetadata(records, "## agent: reader\n---\nmodel: small", "workflow")
	require.Len(t, records, 1)
	for _, record := range records {
		require.Equal(t, "artifact", record.Kind)
		require.Equal(t, "unterminated frontmatter", record.Error)
	}
}

func TestCompilerMarkdownMetadataAcrossPromptChunks(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github"), 0o755))
	c := NewCompiler()
	c.markdownPath = filepath.Join(root, ".github", "workflows", "main.md")
	data := &WorkflowData{
		Name:                 "metadata",
		WorkflowID:           "metadata",
		InlinedImports:       true,
		MainWorkflowMarkdown: "## agent: `reader`\n---\ndescription: |-\n" + strings.Repeat("  Read repository files carefully.\n", 800) + "  ${{ github.event.issue.body }}\nmodel: small\ntools: [Read]\n---\nBody\n## end agent: `reader`",
	}
	var output strings.Builder
	require.NoError(t, c.generatePrompt(&output, data, false, nil))
	require.Len(t, data.CompiledPromptMarkdown, 1)
	require.Greater(t, len(data.CompiledPromptMarkdown[0]), 21000)
	blocks := parser.ExtractMarkdownMetadataBlocks(data.CompiledPromptMarkdown[0])
	require.Len(t, blocks, 1)
	require.Contains(t, blocks[0].Source, "__GH_AW_")
	digest := sha256.Sum256([]byte(blocks[0].Source))
	found := false
	for _, record := range c.collectMarkdownMetadata(data).Records {
		var value map[string]any
		if record.Kind != "artifact" || json.Unmarshal(record.Value, &value) != nil || value["model"] != "small" {
			continue
		}
		if record.SHA256 == hex.EncodeToString(digest[:]) {
			require.Empty(t, record.Error)
			require.Equal(t, []any{"Read"}, value["tools"])
			require.NotContains(t, value, "description")
			found = true
		}
	}
	require.True(t, found, "rewritten metadata must remain complete across prompt chunk boundaries")
}

func TestCompilerMarkdownMetadataScopesWorkflowConfiguration(t *testing.T) {
	root := t.TempDir()
	workflows := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflows, 0o755))
	body := "## agent: `reader`\n---\ndescription: Reads files\nmodel: small\n---\nBody\n## end agent: `reader`\n"
	frontmatter := "engine:\n  id: copilot\n  model: primary\nsteps:\n  - run: " + strings.Repeat("irrelevant ", 3000)
	raw := "---\n" + frontmatter + "\n---\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "main.md"), []byte(raw), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "unrelated.md"), []byte("---\nmodel: unrelated\n---\n"+strings.ReplaceAll(body, "small", "unrelated")), 0o644))
	imported := "---\nmodels:\n  local: [openai/model]\nsteps:\n  - run: " + strings.Repeat("irrelevant ", 3000) + "\n---\n"
	c := &Compiler{markdownPath: filepath.Join(workflows, "main.md")}
	data := &WorkflowData{
		FrontmatterYAML: frontmatter, RawMarkdown: body, MainWorkflowMarkdown: body,
		CompiledPromptMarkdown:    []string{"{{#runtime-import .github/workflows/main.md}}"},
		ImportedMetadataDocuments: map[string]string{filepath.Join(root, ".github", "aw", "nested", "models.md"): imported},
	}
	result := c.collectMarkdownMetadata(data)
	require.Len(t, result.Records, 1)
	artifacts := 0
	for _, record := range result.Records {
		require.Empty(t, record.Error)
		require.NotContains(t, string(record.Value), "irrelevant")
		require.NotContains(t, string(record.Value), "unrelated")
		if record.Kind == "artifact" {
			artifacts++
			require.JSONEq(t, `{"model":"small"}`, string(record.Value))
		}
	}
	require.Equal(t, 1, artifacts, "workflow frontmatter and unrelated workflows must not become artifact records")
}

func TestCompilerMarkdownMetadataRepresentativeWorkflowScope(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	file := filepath.Join(root, ".github", "workflows", "code-simplifier.md")
	c := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	c.SetWorkflowIdentifier(filepath.Base(file))
	data, err := c.ParseWorkflowFile(file)
	require.NoError(t, err)
	c.markdownPath = file
	result := c.collectMarkdownMetadata(data)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	digest := sha256.Sum256([]byte(strings.ReplaceAll(data.FrontmatterYAML, "\r\n", "\n")))
	for _, record := range result.Records {
		require.NotEqual(t, hex.EncodeToString(digest[:]), record.SHA256, "workflow configuration must not be embedded in the lock")
		if record.Kind == "artifact" {
			require.NotContains(t, string(record.Value), "Prepare recent-change dataset", "full workflow configurations must not be duplicated as artifacts")
		}
	}
	lock, err := os.ReadFile(strings.TrimSuffix(file, ".md") + ".lock.yml")
	require.NoError(t, err)
	for _, line := range strings.SplitN(string(lock), "\n", 4)[:3] {
		if strings.HasPrefix(line, "# gh-aw-manifest: ") {
			require.NotContains(t, line, `"model_pins"`)
			t.Logf("code-simplifier metadata: %d records, %d bytes", len(result.Records), len(encoded))
			break
		}
	}
}
