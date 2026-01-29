//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestDeduplicateRequiresDuplicateModules tests that when multiple files require
// the same module with the same variable name, only one require statement is kept
func TestDeduplicateRequiresDuplicateModules(t *testing.T) {
	// Simulates what happens when multiple inlined files all require "fs"
	input := `const fs = require("fs");
const path = require("path");
// Inlined from file1.cjs
const fs = require("fs");
// Inlined from file2.cjs
const fs = require("fs");
const path = require("path");
// Inlined from file3.cjs
const fs = require("fs");

function useModules() {
  fs.existsSync("/tmp");
  path.join("/tmp", "test");
}
`

	output := deduplicateRequires(input)

	t.Logf("Input:\n%s", input)
	t.Logf("Output:\n%s", output)

	// Should have exactly 1 fs require
	fsCount := strings.Count(output, `const fs = require`)
	if fsCount != 1 {
		t.Errorf("Expected 1 fs require, got %d", fsCount)
	}

	// Should have exactly 1 path require
	pathCount := strings.Count(output, `const path = require`)
	if pathCount != 1 {
		t.Errorf("Expected 1 path require, got %d", pathCount)
	}

	// Both requires should come before their usage
	fsRequireIndex := strings.Index(output, `require("fs")`)
	fsUsageIndex := strings.Index(output, "fs.existsSync")
	pathRequireIndex := strings.Index(output, `require("path")`)
	pathUsageIndex := strings.Index(output, "path.join")

	if fsRequireIndex == -1 {
		t.Error("fs require not found")
	}
	if pathRequireIndex == -1 {
		t.Error("path require not found")
	}
	if fsUsageIndex != -1 && fsRequireIndex > fsUsageIndex {
		t.Errorf("fs require should come before fs.existsSync usage")
	}
	if pathUsageIndex != -1 && pathRequireIndex > pathUsageIndex {
		t.Errorf("path require should come before path.join usage")
	}
}
