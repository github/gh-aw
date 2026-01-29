//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestCollectScriptFiles(t *testing.T) {
	// Create mock sources with dependencies
	sources := map[string]string{
		"main.cjs": `
const { helper } = require('./helper.cjs');
const { util } = require('./utils/util.cjs');
helper();
util();
`,
		"helper.cjs": `
const { shared } = require('./shared.cjs');
function helper() {
	shared();
	console.log("helper");
}
module.exports = { helper };
`,
		"shared.cjs": `
function shared() {
	console.log("shared");
}
module.exports = { shared };
`,
		"utils/util.cjs": `
function util() {
	console.log("util");
}
module.exports = { util };
`,
	}

	result, err := CollectScriptFiles("main", sources["main.cjs"], sources)
	if err != nil {
		t.Fatalf("CollectScriptFiles failed: %v", err)
	}

	// Should collect all 4 files
	if len(result.Files) != 4 {
		t.Errorf("Expected 4 files, got %d", len(result.Files))
		for _, f := range result.Files {
			t.Logf("  - %s", f.Path)
		}
	}

	// Check that main script path is set
	if result.MainScriptPath != "main.cjs" {
		t.Errorf("Expected MainScriptPath to be 'main.cjs', got '%s'", result.MainScriptPath)
	}

	// Check total size is > 0
	if result.TotalSize == 0 {
		t.Error("Expected TotalSize > 0")
	}
}

func TestCollectScriptFiles_MissingDependency(t *testing.T) {
	sources := map[string]string{
		"main.cjs": `
const { missing } = require('./missing.cjs');
missing();
`,
	}

	_, err := CollectScriptFiles("main", sources["main.cjs"], sources)
	if err == nil {
		t.Fatal("Expected error for missing dependency, got nil")
	}
	if !strings.Contains(err.Error(), "missing.cjs") {
		t.Errorf("Expected error to mention 'missing.cjs', got: %v", err)
	}
}

func TestCollectScriptFiles_CircularDependency(t *testing.T) {
	// Circular dependencies should be handled (file only processed once)
	sources := map[string]string{
		"a.cjs": `
const { b } = require('./b.cjs');
module.exports = { a: () => b() };
`,
		"b.cjs": `
const { a } = require('./a.cjs');
module.exports = { b: () => console.log("b") };
`,
	}

	result, err := CollectScriptFiles("a", sources["a.cjs"], sources)
	if err != nil {
		t.Fatalf("CollectScriptFiles failed with circular dependency: %v", err)
	}

	// Should collect both files without infinite loop
	if len(result.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(result.Files))
	}
}

func TestGenerateWriteScriptsStep(t *testing.T) {
	files := []ScriptFile{
		{
			Path:    "test.cjs",
			Content: "console.log('hello');",
			Hash:    "abc12345",
		},
	}

	steps := GenerateWriteScriptsStep(files)
	if len(steps) == 0 {
		t.Fatal("Expected steps to be generated")
	}

	// Check that the step includes the mkdir command
	stepsStr := strings.Join(steps, "")
	if !strings.Contains(stepsStr, "mkdir -p /opt/gh-aw/actions") {
		t.Error("Expected mkdir command for actions directory")
	}

	// Check that the file is written
	if !strings.Contains(stepsStr, "cat > /opt/gh-aw/actions/test.cjs") {
		t.Error("Expected cat command for writing file")
	}

	// Check that content is included
	if !strings.Contains(stepsStr, "console.log") {
		t.Error("Expected file content to be included")
	}
}

func TestGenerateRequireScript(t *testing.T) {
	script := GenerateRequireScript("create_issue.cjs")

	if !strings.Contains(script, "/opt/gh-aw/actions/create_issue.cjs") {
		t.Errorf("Expected script to require from /opt/gh-aw/actions/, got: %s", script)
	}

	if !strings.Contains(script, "require(") {
		t.Error("Expected script to contain require()")
	}

	// Should be wrapped in async IIFE to support top-level await
	if !strings.Contains(script, "(async () =>") {
		t.Error("Should be wrapped in async IIFE to support top-level await")
	}

	// Should have the closing IIFE parentheses
	if !strings.Contains(script, ")()") {
		t.Error("Should have IIFE invocation")
	}
}

func TestRewriteScriptForFileMode(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		currentPath string
		wantContain string
	}{
		{
			name:        "simple relative require",
			content:     "const { helper } = require('./helper.cjs');",
			currentPath: "main.cjs",
			wantContain: "/opt/gh-aw/actions/helper.cjs",
		},
		{
			name:        "nested relative require",
			content:     "const { util } = require('./utils/util.cjs');",
			currentPath: "main.cjs",
			wantContain: "/opt/gh-aw/actions/utils/util.cjs",
		},
		{
			name:        "parent directory require",
			content:     "const { shared } = require('../shared.cjs');",
			currentPath: "utils/util.cjs",
			wantContain: "/opt/gh-aw/actions/shared.cjs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RewriteScriptForFileMode(tt.content, tt.currentPath)
			if !strings.Contains(result, tt.wantContain) {
				t.Errorf("Expected result to contain %q, got: %s", tt.wantContain, result)
			}
		})
	}
}

func TestPrepareFilesForFileMode(t *testing.T) {
	files := []ScriptFile{
		{
			Path:    "main.cjs",
			Content: "const { helper } = require('./helper.cjs'); helper();",
			Hash:    "abc123",
		},
		{
			Path:    "helper.cjs",
			Content: "module.exports = { helper: () => {} };",
			Hash:    "def456",
		},
	}

	prepared := PrepareFilesForFileMode(files)
	if len(prepared) != 2 {
		t.Fatalf("Expected 2 prepared files, got %d", len(prepared))
	}

	// Check that require paths are rewritten
	mainFile := prepared[0]
	if !strings.Contains(mainFile.Content, "/opt/gh-aw/actions/helper.cjs") {
		t.Errorf("Expected main file to have rewritten require path, got: %s", mainFile.Content)
	}

	// Check that hash is updated
	if mainFile.Hash == files[0].Hash {
		t.Error("Expected hash to be updated after rewriting")
	}
}

func TestCollectAllJobScriptFiles(t *testing.T) {
	// This test uses the actual script registry
	// Skip if registry is empty (shouldn't happen in normal runs)
	if !DefaultScriptRegistry.Has("create_issue") {
		t.Skip("Script registry not populated")
	}

	scriptNames := []string{"create_issue", "add_comment"}
	sources := GetJavaScriptSources()

	result, err := CollectAllJobScriptFiles(scriptNames, sources)
	if err != nil {
		t.Fatalf("CollectAllJobScriptFiles failed: %v", err)
	}

	// Should collect at least the 2 main scripts plus shared dependencies
	if len(result.Files) < 2 {
		t.Errorf("Expected at least 2 files, got %d", len(result.Files))
	}

	// Check that helpers are deduplicated (shared files should appear only once)
	pathCounts := make(map[string]int)
	for _, f := range result.Files {
		pathCounts[f.Path]++
		if pathCounts[f.Path] > 1 {
			t.Errorf("File %s appears multiple times", f.Path)
		}
	}
}
