//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestFindJavaScriptDependencies(t *testing.T) {
	tests := []struct {
		name         string
		mainContent  string
		sources      map[string]string
		basePath     string
		wantDeps     map[string]bool
		wantErr      bool
		errorMessage string
	}{
		{
			name: "simple single dependency",
			mainContent: `const { foo } = require("./helper.cjs");
console.log(foo());`,
			sources: map[string]string{
				"js/helper.cjs": `function foo() { return "bar"; }
module.exports = { foo };`,
			},
			basePath: "js",
			wantDeps: map[string]bool{
				"js/helper.cjs": true,
			},
			wantErr: false,
		},
		{
			name: "chained dependencies",
			mainContent: `const { a } = require("./module-a.cjs");
console.log(a);`,
			sources: map[string]string{
				"js/module-a.cjs": `const { b } = require("./module-b.cjs");
module.exports = { a: b };`,
				"js/module-b.cjs": `module.exports = { b: "value" };`,
			},
			basePath: "js",
			wantDeps: map[string]bool{
				"js/module-a.cjs": true,
				"js/module-b.cjs": true,
			},
			wantErr: false,
		},
		{
			name:        "circular dependencies handled",
			mainContent: `const { x } = require("./a.cjs");`,
			sources: map[string]string{
				"js/a.cjs": `const { y } = require("./b.cjs");
module.exports = { x: y };`,
				"js/b.cjs": `const { x } = require("./a.cjs");
module.exports = { y: "val" };`,
			},
			basePath: "js",
			wantDeps: map[string]bool{
				"js/a.cjs": true,
				"js/b.cjs": true,
			},
			wantErr: false,
		},
		{
			name: "no dependencies",
			mainContent: `console.log("no requires here");
const x = 42;`,
			sources:  map[string]string{},
			basePath: "js",
			wantDeps: map[string]bool{},
			wantErr:  false,
		},
		{
			name:         "missing dependency error",
			mainContent:  `const { missing } = require("./not-found.cjs");`,
			sources:      map[string]string{},
			basePath:     "js",
			wantDeps:     nil,
			wantErr:      true,
			errorMessage: "required file not found in sources",
		},
		{
			name: "multiple dependencies",
			mainContent: `const { a } = require("./a.cjs");
const { b } = require("./b.cjs");
const { c } = require("./c.cjs");`,
			sources: map[string]string{
				"js/a.cjs": `module.exports = { a: 1 };`,
				"js/b.cjs": `module.exports = { b: 2 };`,
				"js/c.cjs": `module.exports = { c: 3 };`,
			},
			basePath: "js",
			wantDeps: map[string]bool{
				"js/a.cjs": true,
				"js/b.cjs": true,
				"js/c.cjs": true,
			},
			wantErr: false,
		},
		{
			name: "multi-line destructuring",
			mainContent: `const {
  foo,
  bar,
  baz
} = require("./utils.cjs");`,
			sources: map[string]string{
				"js/utils.cjs": `module.exports = { foo: 1, bar: 2, baz: 3 };`,
			},
			basePath: "js",
			wantDeps: map[string]bool{
				"js/utils.cjs": true,
			},
			wantErr: false,
		},
		{
			name: "safe-outputs MCP server dependencies",
			mainContent: `const { createServer, registerTool, normalizeTool, start } = require("./mcp_server_core.cjs");
const { loadConfig } = require("./safe_outputs_config.cjs");
const { createAppendFunction } = require("./safe_outputs_append.cjs");
const { createHandlers } = require("./safe_outputs_handlers.cjs");`,
			sources: map[string]string{
				"js/mcp_server_core.cjs": `const { readBuffer } = require("./read_buffer.cjs");
module.exports = { createServer, registerTool, normalizeTool, start };`,
				"js/read_buffer.cjs":         `module.exports = { readBuffer };`,
				"js/safe_outputs_config.cjs": `module.exports = { loadConfig };`,
				"js/safe_outputs_append.cjs": `module.exports = { createAppendFunction };`,
				"js/safe_outputs_handlers.cjs": `const { normalize } = require("./normalize_branch_name.cjs");
module.exports = { createHandlers };`,
				"js/normalize_branch_name.cjs": `module.exports = { normalize };`,
			},
			basePath: "js",
			wantDeps: map[string]bool{
				"js/mcp_server_core.cjs":       true,
				"js/read_buffer.cjs":           true,
				"js/safe_outputs_config.cjs":   true,
				"js/safe_outputs_append.cjs":   true,
				"js/safe_outputs_handlers.cjs": true,
				"js/normalize_branch_name.cjs": true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDeps, err := FindJavaScriptDependencies(tt.mainContent, tt.sources, tt.basePath)

			if (err != nil) != tt.wantErr {
				t.Errorf("FindJavaScriptDependencies() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("FindJavaScriptDependencies() expected error containing %q but got no error", tt.errorMessage)
				} else if tt.errorMessage != "" && !strings.Contains(err.Error(), tt.errorMessage) {
					t.Errorf("FindJavaScriptDependencies() error = %q, expected to contain %q", err.Error(), tt.errorMessage)
				}
				return
			}

			// Check that all wanted dependencies are present
			for dep := range tt.wantDeps {
				if !gotDeps[dep] {
					t.Errorf("FindJavaScriptDependencies() missing expected dependency: %q", dep)
				}
			}

			// Check that no unexpected dependencies are present
			for dep := range gotDeps {
				if !tt.wantDeps[dep] {
					t.Errorf("FindJavaScriptDependencies() unexpected dependency: %q", dep)
				}
			}

			// Check count
			if len(gotDeps) != len(tt.wantDeps) {
				t.Errorf("FindJavaScriptDependencies() got %d dependencies, want %d", len(gotDeps), len(tt.wantDeps))
			}
		})
	}
}
