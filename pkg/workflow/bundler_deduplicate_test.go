//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestDeduplicateRequiresPreservesIndentation tests that deduplicateRequires
// preserves the indentation level of requires
func TestDeduplicateRequiresPreservesIndentation(t *testing.T) {
	input := `async function main() {
  const fs = require("fs");
  
  if (fs.existsSync("/tmp/test.txt")) {
    console.log("exists");
  }
}

const path = require("path");
console.log(path.basename("/tmp/file.txt"));
`

	output := deduplicateRequires(input)

	t.Logf("Input:\n%s", input)
	t.Logf("Output:\n%s", output)

	// Check that fs require is at indent 2
	if !strings.Contains(output, "  const fs = require(\"fs\");") {
		t.Error("fs require should have 2 spaces of indentation")
	}

	// Check that path require is at indent 0
	if !strings.Contains(output, "const path = require(\"path\");") {
		t.Error("path require should have 0 spaces of indentation")

		// Check if it was incorrectly indented
		if strings.Contains(output, "  const path = require(\"path\");") {
			t.Error("path require was incorrectly indented with 2 spaces")
		}
	}
}
