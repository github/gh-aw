package lint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nativeGraderJavaScriptOptions(t testing.TB) *taint.JavaScriptOptions {
	t.Helper()
	dir, err := filepath.Abs("../taint/javascript")
	require.NoError(t, err)
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "@typescript", "native-preview", "package.json")); err != nil {
		t.Skip("native grader lint requires npm ci --prefix pkg/workflow/taint/javascript")
	}
	return &taint.JavaScriptOptions{Parser: &taint.TypeScriptParser{ModuleDirectory: dir}}
}

func graderFileIOInput(t *testing.T, source string, options *taint.JavaScriptOptions) Input {
	t.Helper()
	document, err := aw.Parse("---\ngraders:\n  quality:\n    script: |\n      " + strings.ReplaceAll(source, "\n", "\n      ") + "\n---\nPrompt\n")
	require.NoError(t, err)
	return Input{Document: document, JavaScript: options}
}

func TestNativeGraderFileIO(t *testing.T) {
	options := nativeGraderJavaScriptOptions(t)
	tests := []struct {
		name, source string
		unsafe       bool
	}{
		{"commonjs read", `return require("node:fs").readFileSync("evidence.json", "utf8");`, true},
		{"commonjs write", `require("fs").writeFileSync("result", "1"); return 1;`, true},
		{"destructured promises", `const {readFile: read} = require("fs/promises"); return read("evidence");`, true},
		{"node promises", `const io = require("node:fs/promises"); return io.stat("evidence");`, true},
		{"unused import", `import {readFile as read} from "node:fs/promises"; return 1;`, true},
		{"namespace import", `import * as io from "fs"; return io.readdirSync(".").length;`, true},
		{"dynamic import", `return import("node:fs").then(io => io.readFileSync("evidence"));`, true},
		{"dynamic module", `const name = config.module; return require(name);`, true},
		{"relative module", `return require("./hidden-file-access.cjs");`, true},
		{"indirect loader", `const load = require; return load("fs");`, true},
		{"escaped loader identifier", `return requ\u0069re("fs").readFileSync("evidence");`, true},
		{"process builtin loader", `return process.getBuiltinModule("fs").readFileSync("evidence");`, true},
		{"process loader alias", `const load = process.getBuiltinModule; return load("node:fs");`, true},
		{"native binding", `return process.binding("fs");`, true},
		{"module require", `return module.require("node:fs").existsSync("evidence");`, true},
		{"shell escape", `require("node:child_process").execSync("cat evidence"); return 1;`, true},
		{"ambient filesystem", `return fs.promises.readFile("evidence");`, true},
		{"computed property", "return fs[`readFileSync`]('evidence');", true},
		{"computed host property", `return globalThis["process"]["getBuiltinModule"]("fs");`, true},
		{"deno", `return Deno.readTextFileSync("evidence");`, true},
		{"deno alias", `const io = Deno; return io.readTextFileSync("evidence");`, true},
		{"process alias", `const host = process; return host.getBuiltinModule("fs");`, true},
		{"bun read", `return Bun.file("evidence").text();`, true},
		{"bun write", `return Bun.write("result", "1");`, true},
		{"browser reader", `return new FileReader();`, true},
		{"browser directory", `return navigator.storage.getDirectory();`, true},
		{"browser picker", `return showOpenFilePicker();`, true},
		{"browser legacy filesystem", `return requestFileSystem(0, 1, () => {});`, true},
		{"unused function", `function unused() { return require("fs").readFileSync("evidence"); } return 1;`, true},
		{"parameter default", `function metric(x = require("fs")) { return 1; } return metric();`, true},
		{"catch and finally", `try { return 1; } catch { fs.readFileSync("evidence"); } finally { fs.unlinkSync("evidence"); }`, true},
		{"re-export", `export {readFile} from "node:fs";`, true},
		{"in memory", `return session.events.length;`, false},
		{"helpers", `const {stats} = helpers.createSessionLookups(session); return stats.toolCalls;`, false},
		{"file metadata", `return session.events.filter(e => e.data?.path === "evidence.json").length;`, false},
		{"file wording", `// require("fs").readFileSync("evidence")\nreturn {value: 1, message: "fs.readFileSync is not used"};`, false},
		{"object property names", `const data = {require: 1, fs: 2, readFile: 3}; return data.fs;`, false},
		{"in-memory read function", `function readFile(value) { return value; } return readFile(1);`, false},
		{"local filesystem-shaped data", `const fs = {readFileSync: () => 1}; return fs.readFileSync();`, false},
		{"local loader-shaped function", `function require(value) { return value; } return require(1);`, false},
		{"local host-shaped function", `const process = value => value; return process(1);`, false},
		{"parameter is data", `function count({fs}) { return fs.length; } return count({fs: session.events});`, false},
		{"nested local does not hide global", `function count(fs) { return fs.length; } return fs.readFileSync("evidence");`, true},
		{"other branch local does not hide global", `if (config.test) { const fs = {length: 1}; } return fs.readFileSync("evidence");`, true},
		{"method name is not local binding", `const data = { process() { return process.getBuiltinModule("fs"); } }; return data.process();`, true},
		{"computed method uses outer scope", `const data = { [fs.readFileSync("evidence")](fs) { return fs; } }; return 1;`, true},
		{"destructured property key", `const {fs: count} = {fs: 1}; return count;`, false},
	}
	sources := make([]string, 0, len(tests))
	for _, tt := range tests {
		sources = append(sources, tt.source)
	}
	results, err := options.Parser.(taint.JavaScriptBatchParser).ParseBatch(t.Context(), sources)
	require.NoError(t, err)
	for _, result := range results {
		require.NoError(t, result.Err)
	}
	runner, err := Default().Select("grader-file-io")
	require.NoError(t, err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := graderFileIOInput(t, tt.source, options)
			diagnostics, err := runner.Run(t.Context(), input)
			require.NoError(t, err)
			if !tt.unsafe {
				assert.Empty(t, diagnostics)
				return
			}
			require.NotEmpty(t, diagnostics)
			for _, diagnostic := range diagnostics {
				assert.Equal(t, Error, diagnostic.Severity)
				assert.Equal(t, "graders.quality.script", diagnostic.Path)
				assert.Positive(t, diagnostic.Line)
				assert.NotContains(t, diagnostic.Message, tt.source)
			}
		})
	}
}

func TestOperationalValueUsesJavaScriptFileIORule(t *testing.T) {
	options := nativeGraderJavaScriptOptions(t)
	document, err := aw.Parse(`---
graders:
  operational-value:
    script: return require("node:fs").readFileSync("evidence");
---
Prompt
`)
	require.NoError(t, err)
	runner, err := Default().Select("grader-file-io")
	require.NoError(t, err)
	diagnostics, err := runner.Run(t.Context(), Input{Document: document, JavaScript: options})
	require.NoError(t, err)
	require.NotEmpty(t, diagnostics)
	assert.Equal(t, "graders.operational-value.script", diagnostics[0].Path)
}

type graderJavaScriptParserFunc func(context.Context, string) (*taint.JavaScriptNode, error)

func (f graderJavaScriptParserFunc) Parse(ctx context.Context, source string) (*taint.JavaScriptNode, error) {
	return f(ctx, source)
}

func TestGraderFileIOUnavailableAndInvalid(t *testing.T) {
	runner, err := Default().Select("grader-file-io")
	require.NoError(t, err)
	input := graderFileIOInput(t, "return 1;", nil)
	diagnostics, err := runner.Run(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, Warning, diagnostics[0].Severity)
	assert.Contains(t, diagnostics[0].Message, "--javascript-module")
	for _, parser := range []graderJavaScriptParserFunc{
		func(context.Context, string) (*taint.JavaScriptNode, error) { return nil, errors.New("PRIVATE_SOURCE") },
		func(context.Context, string) (*taint.JavaScriptNode, error) { return nil, nil },
		func(context.Context, string) (*taint.JavaScriptNode, error) {
			return &taint.JavaScriptNode{Kind: "invalid"}, nil
		},
	} {
		input.JavaScript = &taint.JavaScriptOptions{Parser: parser}
		_, err := runner.Run(t.Context(), input)
		require.ErrorContains(t, err, "graders.quality.script")
		assert.NotContains(t, err.Error(), "PRIVATE_SOURCE")
	}
	document, err := aw.Parse("---\ngraders:\n  quality:\n    enabled: false\n    script: [invalid]\n---\n")
	require.NoError(t, err)
	_, err = runner.Run(t.Context(), Input{Document: document})
	require.ErrorContains(t, err, "requires a JavaScript string")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = runner.Run(ctx, input)
	require.ErrorIs(t, err, context.Canceled)
}

func TestGraderFileIOOnlyExecutableGraderScripts(t *testing.T) {
	document, err := aw.Parse(`---
graders:
  retries: true
  tool-failure-count:
    enabled: true
  quality:
    script: return session.events.length;
mcp-scripts:
  allowed:
    script: require("fs").readFileSync("evidence");
---
graders:
  example:
    script: require("fs");
`)
	require.NoError(t, err)
	calls := 0
	parser := graderJavaScriptParserFunc(func(_ context.Context, source string) (*taint.JavaScriptNode, error) {
		calls++
		assert.Equal(t, "return session.events.length;", source)
		return &taint.JavaScriptNode{Kind: "SourceFile"}, nil
	})
	runner, err := Default().Select("grader-file-io")
	require.NoError(t, err)
	diagnostics, err := runner.Run(t.Context(), Input{Document: document, JavaScript: &taint.JavaScriptOptions{Parser: parser}})
	require.NoError(t, err)
	assert.Empty(t, diagnostics)
	assert.Equal(t, 1, calls)
}

func TestGraderFileIOASTBoundsAndUnknownSyntax(t *testing.T) {
	root := &taint.JavaScriptNode{Kind: "SourceFile"}
	hidden := &taint.JavaScriptNode{Kind: "Identifier", Text: "require", Line: 7}
	root.Other = []*taint.JavaScriptNode{{Kind: "FutureSyntax", Other: []*taint.JavaScriptNode{hidden}}}
	found, err := graderFileIOAST(t.Context(), root, "graders.custom.script")
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, 7, found[0].Line)
	root.Other = []*taint.JavaScriptNode{root}
	_, err = graderFileIOAST(t.Context(), root, "graders.custom.script")
	require.ErrorContains(t, err, "analysis limits")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = graderFileIOAST(ctx, root, "graders.custom.script")
	require.ErrorIs(t, err, context.Canceled)
}

func TestGraderFileIOBatchRetainsFindings(t *testing.T) {
	options := nativeGraderJavaScriptOptions(t)
	document, err := aw.Parse(`---
graders:
  unsafe:
    enabled: false
    script: |
      const value = 1;
      return require("node:fs").readFileSync("PRIVATE_PATH");
  malformed:
    script: return (PRIVATE_SOURCE;
---
Prompt
`)
	require.NoError(t, err)
	runner, err := Default().Select("grader-file-io")
	require.NoError(t, err)
	findings, err := runner.Run(t.Context(), Input{Document: document, JavaScript: options})
	require.ErrorContains(t, err, "graders.malformed.script")
	assert.NotContains(t, err.Error(), "PRIVATE_SOURCE")
	require.Len(t, findings, 1)
	assert.Equal(t, "graders.unsafe.script", findings[0].Path)
	assert.Equal(t, 2, findings[0].Line)
}
