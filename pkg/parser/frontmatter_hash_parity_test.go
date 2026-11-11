//go:build !integration

package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nestedHashParityInput struct {
	WorkflowPath string            `json:"workflowPath"`
	Files        map[string]string `json:"files"`
}

type nestedHashParityResult struct {
	Frontmatter string   `json:"frontmatter"`
	Body        string   `json:"body"`
	ReadPaths   []string `json:"readPaths"`
}

func TestNestedImportHashGoJavaScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err, "Node.js is required to enforce Go/JavaScript hash parity")
	modulePath := filepath.Join(findRepoRoot(t), "actions", "setup", "js", "frontmatter_hash_pure.cjs")
	var fixture struct {
		Workflow struct {
			Path        string `json:"path"`
			Frontmatter string `json:"frontmatter"`
			Body        string `json:"body"`
		} `json:"workflow"`
		Imports []struct {
			Path        string `json:"path"`
			Frontmatter string `json:"frontmatter"`
			Body        string `json:"body"`
		} `json:"imports"`
	}
	data, err := os.ReadFile("testdata/nested_import_hash.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &fixture))
	files := map[string]string{
		fixture.Workflow.Path: "---\n" + fixture.Workflow.Frontmatter + "\n---\n" + fixture.Workflow.Body,
	}
	for _, file := range fixture.Imports {
		files[file.Path] = "---\n" + file.Frontmatter + "\n---\n" + file.Body
	}
	scenarios := map[string]map[string]string{"baseline": files}
	inlined := maps.Clone(files)
	inlined[fixture.Workflow.Path] = strings.Replace(inlined[fixture.Workflow.Path], "engine: copilot", "engine: copilot\ninlined-imports: true", 1)
	scenarios["inlined imports"] = inlined
	crlf := make(map[string]string, len(files))
	for path, content := range files {
		crlf[path] = strings.ReplaceAll(content, "\n", "\r\n")
	}
	scenarios["CRLF"] = crlf
	for _, file := range fixture.Imports {
		config := maps.Clone(files)
		config[file.Path] = "---\n" + file.Frontmatter + "\ndescription: changed\n---\n" + file.Body
		scenarios["config/"+file.Path] = config
		body := maps.Clone(files)
		body[file.Path] += "\nChanged ${{ vars.NESTED_CHANGE }}"
		scenarios["body/"+file.Path] = body
	}
	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			for _, baseDir := range []string{
				".github/workflows",
				filepath.Join(t.TempDir(), ".github", "workflows"),
				filepath.Join(".github", ".github", "workflows"),
				filepath.Join(t.TempDir(), ".github", ".github", "workflows"),
			} {
				input := nestedHashParityInput{WorkflowPath: filepath.Join(baseDir, fixture.Workflow.Path), Files: make(map[string]string)}
				for path, content := range scenario {
					input.Files[filepath.Join(baseDir, path)] = content
				}
				goResult := computeNestedHashParityGo(t, input)
				for _, mode := range []string{"local", "github-api"} {
					jsResult := computeNestedHashParityJS(t, node, modulePath, mode, input)
					assert.Equal(t, goResult, jsResult, "mode=%s, baseDir=%s", mode, baseDir)
				}
			}
		})
	}
}

func computeNestedHashParityGo(t *testing.T, input nestedHashParityInput) nestedHashParityResult {
	t.Helper()
	reads := make(map[string]bool)
	reader := func(path string) ([]byte, error) {
		content, ok := input.Files[path]
		if !ok {
			return nil, fmt.Errorf("unexpected parity fixture read: %s", path)
		}
		reads[path] = true
		return []byte(content), nil
	}
	frontmatter, err := ComputeFrontmatterHashFromFileWithReader(input.WorkflowPath, nil, reader)
	require.NoError(t, err)
	raw, err := reader(input.WorkflowPath)
	require.NoError(t, err)
	text, markdown, err := extractFrontmatterAndBodyText(string(raw))
	require.NoError(t, err)
	body, err := ComputeBodyHashFromParsedContent(markdown, text, filepath.Dir(input.WorkflowPath), reader)
	require.NoError(t, err)
	paths := make([]string, 0, len(reads))
	for path := range reads {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	require.Len(t, paths, len(input.Files), "both hash walkers must read every nested fixture file")
	return nestedHashParityResult{Frontmatter: frontmatter, Body: body, ReadPaths: paths}
}

func computeNestedHashParityJS(t *testing.T, node, modulePath, mode string, input nestedHashParityInput) nestedHashParityResult {
	t.Helper()
	const script = `
const fs = require("fs");
const { computeFrontmatterHash, computeBodyHash, createGitHubFileReader } = require(process.argv[1]);
const input = JSON.parse(fs.readFileSync(0, "utf8"));
const reads = new Set();
async function read(filePath) {
  if (!Object.hasOwn(input.files, filePath)) throw new Error("Unexpected parity fixture read: " + filePath);
  reads.add(filePath);
  return input.files[filePath];
}
const mode = process.argv[2];
const fileReader = mode === "github-api"
  ? createGitHubFileReader({ rest: { repos: { getContent: async ({ path }) => ({
      data: { encoding: "base64", content: Buffer.from(await read(path)).toString("base64") }
    }) } } }, "fixture-owner", "fixture-repo", "fixture-revision")
  : read;
(async () => {
  const frontmatter = await computeFrontmatterHash(input.workflowPath, { fileReader, readerMode: mode });
  const body = await computeBodyHash(input.workflowPath, { fileReader });
  process.stdout.write(JSON.stringify({ frontmatter, body, readPaths: [...reads].sort() }));
})().catch(error => { console.error(error); process.exitCode = 1; });
`
	data, err := json.Marshal(input)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", script, modulePath, mode)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "JavaScript parity runner failed: %s", stderr.String())
	var result nestedHashParityResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result), "invalid JavaScript parity result")
	return result
}
