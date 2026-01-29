//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestDeduplicateRequiresWithMixedIndentation tests what happens when requires have different indentation
func TestDeduplicateRequiresWithMixedIndentation(t *testing.T) {
	// This simulates the real scenario where some code has no indentation
	// but other inlined code has indentation
	input := `const { execFile } = require("child_process");
const os = require("os");

function someFunction() {
  const fs = require("fs");
  const path = require("path");
  
  fs.existsSync("/tmp");
  path.join("/tmp", "test");
}
`

	output := deduplicateRequires(input)

	t.Logf("Input:\n%s", input)
	t.Logf("Output:\n%s", output)

	// Count requires at each indentation level
	lines := strings.Split(output, "\n")
	indent0Requires := 0
	indent2Requires := 0

	for _, line := range lines {
		if strings.Contains(line, "require(") {
			// Count leading spaces
			spaces := len(line) - len(strings.TrimLeft(line, " "))
			switch spaces {
			case 0:
				indent0Requires++
				t.Logf("Indent 0: %s", line)
			case 2:
				indent2Requires++
				t.Logf("Indent 2: %s", line)
			}
		}
	}

	t.Logf("Requires at indent 0: %d", indent0Requires)
	t.Logf("Requires at indent 2: %d", indent2Requires)

	// fs and path should stay at indent 2 (inside the function scope)
	if indent2Requires != 2 {
		t.Errorf("Expected 2 requires at indent 2 (fs and path inside function), got %d", indent2Requires)
	}
}
