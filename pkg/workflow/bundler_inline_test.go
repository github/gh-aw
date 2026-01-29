//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestDeduplicateRequiresWithInlinedContent tests deduplication with comment markers
func TestDeduplicateRequiresWithInlinedContent(t *testing.T) {
	input := `// === Inlined from ./safe_outputs_mcp_server.cjs ===
const { execFile, execSync } = require("child_process");
const os = require("os");
// === Inlined from ./read_buffer.cjs ===
class ReadBuffer {
}
// === End of ./read_buffer.cjs ===
// === Inlined from ./mcp_server_core.cjs ===
const fs = require("fs");
const path = require("path");
function initLogFile(server) {
  if (!fs.existsSync(server.logDir)) {
    fs.mkdirSync(server.logDir, { recursive: true });
  }
}
// === End of ./mcp_server_core.cjs ===
// === End of ./safe_outputs_mcp_server.cjs ===
`

	output := deduplicateRequires(input)

	t.Logf("Input:\n%s", input)
	t.Logf("Output:\n%s", output)

	// Check that fs and path requires are present
	if !strings.Contains(output, `require("fs")`) {
		t.Error("fs require should be present in output")
	}

	if !strings.Contains(output, `require("path")`) {
		t.Error("path require should be present in output")
	}

	// Check that they come before fs.existsSync usage
	fsRequireIndex := strings.Index(output, `require("fs")`)
	fsUsageIndex := strings.Index(output, "fs.existsSync")
	pathRequireIndex := strings.Index(output, `require("path")`)

	if fsRequireIndex == -1 {
		t.Error("fs require not found")
	}
	if pathRequireIndex == -1 {
		t.Error("path require not found")
	}
	if fsUsageIndex != -1 && fsRequireIndex > fsUsageIndex {
		t.Errorf("fs require should come before fs.existsSync usage (require at %d, usage at %d)", fsRequireIndex, fsUsageIndex)
	}
}
