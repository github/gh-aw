package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

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
