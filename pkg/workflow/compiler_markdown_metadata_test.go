package workflow

import (
	"crypto/sha256"
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

type activationMetadataDocument struct {
	Jobs map[string]struct {
		Env   map[string]string `yaml:"env"`
		Steps []struct {
			ID  string            `yaml:"id"`
			Env map[string]string `yaml:"env"`
			Run string            `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func compileActivationMetadataWorkflow(t *testing.T, content string) (string, *WorkflowData, map[string]string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "agents", "repo.agent.md"), []byte("---\nmodel: repo-model\ntools: [Edit]\n---\nBody"), 0o600))
	source := filepath.Join(root, ".github", "workflows", "workflow.md")
	require.NoError(t, os.WriteFile(source, []byte(content), 0o600))
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowFile(source)
	require.NoError(t, err)
	generated, err := compiler.CompileToYAML(data, source)
	require.NoError(t, err)
	var document activationMetadataDocument
	require.NoError(t, yaml.Unmarshal([]byte(generated), &document))
	activation := document.Jobs["activation"]
	require.NotContains(t, activation.Env, "GH_AW_INFO_MARKDOWN_METADATA", "only the run info step reads the catalog")
	for _, step := range activation.Steps {
		if step.ID == "generate_aw_info" {
			return generated, data, step.Env
		}
	}
	require.FailNow(t, "generate_aw_info step missing")
	return "", nil, nil
}

func TestCompilerEmbedsActivationMarkdownMetadata(t *testing.T) {
	content := "---\non:\n  workflow_call:\nstrict: false\nengine: copilot\n---\n# Work\n\n" +
		"## agent: reader\n---\nmodel: ${{ inputs.model }}\ntools: [Read, \"it's-café\"]\n---\nRead files.\n## end agent: reader\n\n" +
		"## agent: writer\n---\ndescription: Writes files\n---\nWrite files.\n"
	generated, data, env := compileActivationMetadataWorkflow(t, content)
	embedded := env["GH_AW_INFO_MARKDOWN_METADATA"]
	require.NotEmpty(t, embedded)
	require.NotContains(t, embedded, "${{", "the embedded env value must not contain a live expression")
	require.Contains(t, embedded, `\u0024{{ inputs.model }}`, "authored expressions must stay inert")
	require.Contains(t, embedded, `it\u0027s-caf\u00e9`)
	var catalog GHAWMarkdownMetadata
	require.NoError(t, json.Unmarshal([]byte(embedded), &catalog))
	require.Equal(t, 2, catalog.Version)
	require.NotEmpty(t, catalog.Records)
	for _, record := range catalog.Records {
		require.Empty(t, record.References, "embedded records must be self-contained")
		require.NotContains(t, []string{"{}", "null"}, string(record.Value), "empty records are implied by absence")
		require.NotContains(t, string(record.Value), "repo-model", "repository agents are recovered from the checkout")
	}
	var value struct{ Model string }
	require.NoError(t, json.Unmarshal(catalog.Records[0].Value, &value))
	require.Equal(t, "${{ inputs.model }}", value.Model)
	require.NotEmpty(t, data.CompiledPromptMarkdown)
	for _, prompt := range data.CompiledPromptMarkdown {
		for _, block := range parser.ExtractMarkdownMetadataBlocks(prompt) {
			if strings.Contains(block.Source, "Writes files") {
				continue
			}
			digest := sha256.Sum256([]byte(block.Source))
			require.Contains(t, embedded, hex.EncodeToString(digest[:]), "include post-substitution prompt metadata")
		}
	}
	require.NotContains(t, generated, "model: ${{ inputs.model }}")
}

func TestCompilerOmitsEmptyActivationMarkdownMetadata(t *testing.T) {
	generated, _, _ := compileActivationMetadataWorkflow(t, "---\non: workflow_dispatch\nstrict: false\nengine: copilot\n---\n# Work\n\n## agent: writer\n---\ndescription: Writes files\n---\nWrite files.\n")
	require.NotContains(t, generated, "GH_AW_INFO_MARKDOWN_METADATA")
	require.NotContains(t, generated, "activation metadata chunk")
}

func TestCompilerEmbedsLargeActivationMarkdownMetadata(t *testing.T) {
	var content strings.Builder
	content.WriteString("---\non: workflow_dispatch\nstrict: false\nengine: copilot\n---\n# Work\n\n")
	for i := range 200 {
		fmt.Fprintf(&content, "## agent: reader-%d\n---\nmodel: model-%d-%s\ntools: [Read]\n---\nRead files.\n## end agent: reader-%d\n\n", i, i, strings.Repeat("é", 300), i)
	}
	generated, _, env := compileActivationMetadataWorkflow(t, content.String())
	require.NoError(t, NewCompiler().validateExpressionSizes(generated))
	require.NotContains(t, env, "GH_AW_INFO_MARKDOWN_METADATA")
	require.Equal(t, "${{ runner.temp }}/gh-aw-markdown-metadata.json", env["GH_AW_INFO_MARKDOWN_METADATA_FILE"])
	var document activationMetadataDocument
	require.NoError(t, yaml.Unmarshal([]byte(generated), &document))
	runnerTemp := t.TempDir()
	chunks := 0
	for _, step := range document.Jobs["activation"].Steps {
		if !strings.HasPrefix(step.Run, "printf '%s' '") {
			if chunks == 0 {
				continue
			}
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
	payload, err := os.ReadFile(filepath.Join(runnerTemp, "gh-aw-markdown-metadata.json"))
	require.NoError(t, err)
	require.Greater(t, len(payload), 128*1024)
	var catalog GHAWMarkdownMetadata
	require.NoError(t, json.Unmarshal(payload, &catalog))
	require.Len(t, catalog.Records, 200)
	var value struct{ Model string }
	require.NoError(t, json.Unmarshal(catalog.Records[0].Value, &value))
	require.Contains(t, value.Model, strings.Repeat("é", 300))
}

func TestInertJSON(t *testing.T) {
	input := []byte(`{"a":"${{ x }}'é😀\u003c"}`)
	encoded := inertJSON(input)
	require.Equal(t, `{"a":"\u0024{{ x }}\u0027\u00e9\ud83d\ude00\u003c"}`, encoded) //nolint:testifylint // Exact escapes keep Actions and shell expressions inert.
	var want, got any
	require.NoError(t, json.Unmarshal(input, &want))
	require.NoError(t, json.Unmarshal([]byte(encoded), &got))
	require.Equal(t, want, got)
}

func TestCompilerActivationMarkdownMetadataErrors(t *testing.T) {
	compiler := NewCompiler()
	catalog := &GHAWMarkdownMetadata{Version: 2, Records: []GHAWMarkdownMetadataRecord{}}
	require.NoError(t, compiler.embedActivationMarkdownMetadata(catalog), "an empty catalog embeds nothing")
	catalog.Records = append(catalog.Records, GHAWMarkdownMetadataRecord{SHA256: strings.Repeat("a", 64), Kind: "artifact", Value: json.RawMessage(`{"model":"small"}`)})
	require.ErrorContains(t, compiler.embedActivationMarkdownMetadata(catalog), "activation job missing")
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "activation"}))
	require.ErrorContains(t, compiler.embedActivationMarkdownMetadata(catalog), "run info step missing")
	catalog.Records[0].Value = json.RawMessage(`{"model":"` + strings.Repeat("x", maxActivationMetadataBytes) + `"}`)
	require.ErrorContains(t, compiler.embedActivationMarkdownMetadata(catalog), "4194304-byte limit")
	catalog.Records[0].Value = json.RawMessage(`{"model":"` + strings.Repeat("$", maxActivationMetadataBytes/2) + `"}`)
	require.ErrorContains(t, compiler.embedActivationMarkdownMetadata(catalog), "4194304-byte limit", "the limit applies to the escaped payload the runtime reads")
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
