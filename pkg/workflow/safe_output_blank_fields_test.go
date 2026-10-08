//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSafeOutputBlankOptionalFieldConformance(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for safe-output conformance tests")
	}
	config, err := GetValidationConfigJSONWithDataSchema(nil, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	jsDir, err := filepath.Abs("../../actions/setup/js")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "-e", blankOptionalFieldConformance)
	cmd.Dir = jsDir
	cmd.Env = append(os.Environ(), "GH_AW_VALIDATION_CONFIG="+config)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Blank optional field conformance failed: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
}

const blankOptionalFieldConformance = `
const assert = require("node:assert/strict");
const tools = require("./safe_outputs_tools.json");
const { normalizeSafeOutputToolArguments } = require("./safe_outputs_mcp_arguments.cjs");
const { validateField, validateItem, loadValidationConfig } = require("./safe_output_type_validator.cjs");
const { validateArgumentsAgainstSchema, validateStringMinLengths } = require("./mcp_scripts_validation.cjs");
const { createHandlers } = require("./safe_outputs_handlers.cjs");
const config = loadValidationConfig();
let checked = 0;
for (const [type, { fields }] of Object.entries(config)) {
  const tool = tools.find(tool => tool.name === type);
  for (const [field, rule] of Object.entries(fields)) {
    if (rule.required) continue;
    for (const blank of ["", " \t\n"]) {
      const collector = validateField(blank, field, rule, type, 1);
      // Config-only fields include legacy aliases and handler-internal metadata.
      if (!tool?.inputSchema.properties[field] || tool.inputSchema.required?.includes(field)) {
        assert.equal(collector.isValid, true, type + "." + field);
        continue;
      }
      if (tool.inputSchema.anyOf) continue;
      const args = normalizeSafeOutputToolArguments(type, { [field]: blank }, undefined, tool.inputSchema);
      const mcpAccepted = !validateArgumentsAgainstSchema(args, tool.inputSchema) &&
        validateStringMinLengths(args, tool.inputSchema).length === 0;
      assert.equal(mcpAccepted, collector.isValid, type + "." + field + ": MCP/collector disagree");
      checked++;
    }
  }
}
assert.ok(checked > 200, "conformance test must cover the full optional-field catalog");

const pr = {
  type: "create_pull_request", title: "Fix optional fields", body: "Regression reproduction", branch: "fix-optional-fields",
  stack_position: "", stack_root: "", temporary_id: "", dependencies: [], labels: []
};
const collected = validateItem(JSON.parse(JSON.stringify(pr)), pr.type, 1);
assert.equal(collected.isValid, true, collected.error);
assert.equal(Object.hasOwn(collected.normalizedItem, "stack_position"), false);
assert.equal(Object.hasOwn(collected.normalizedItem, "stack_root"), false);
assert.equal(Object.hasOwn(collected.normalizedItem, "temporary_id"), false);
assert.deepEqual(collected.normalizedItem.dependencies, []);
assert.deepEqual(collected.normalizedItem.labels, []);
const normalized = normalizeSafeOutputToolArguments(pr.type, pr, undefined, tools.find(t => t.name === pr.type).inputSchema);
assert.deepEqual(validateItem(normalized, pr.type, 1), collected);
const clear = validateItem({ type: "update_issue", body: "" }, "update_issue", 1);
assert.equal(clear.isValid, true, clear.error);
assert.equal(clear.normalizedItem.body, "");
const blankUpdate = validateItem({ type: "update_issue", milestone: "" }, "update_issue", 1);
assert.equal(blankUpdate.isValid, false, "blank fields must not satisfy requiresOneOf");
const updateTool = tools.find(tool => tool.name === "update_issue");
const recorded = [];
const handlers = createHandlers({ debug() {} }, entry => recorded.push(entry), { update_issue: { target: "*" } });
for (const field of ["status", "labels", "assignees", "milestone"]) {
  for (const blank of ["", " \t\n"]) {
    const raw = { issue_number: 42, [field]: blank };
    const normalized = normalizeSafeOutputToolArguments("update_issue", raw, undefined, updateTool.inputSchema);
    assert.deepEqual(normalized, { issue_number: 42 });
    assert.equal(validateArgumentsAgainstSchema(normalized, updateTool.inputSchema), null);
    assert.throws(() => handlers.updateIssueHandler(normalized), {
      code: -32602,
      message: "ERR_VALIDATION: update_issue requires at least one of: status, title, body, labels, assignees, milestone fields"
    });
    assert.deepEqual(validateItem({ type: "update_issue", ...raw }, "update_issue", 1), {
      isValid: false,
      error: "Line 1: update_issue requires at least one of: 'status', 'title', 'body', 'labels', 'assignees', 'milestone' fields"
    });
    assert.equal(recorded.length, 0, "blank-only updates must not be recorded");
  }
}
for (const milestone of [null, 1, "1"]) {
  const normalized = normalizeSafeOutputToolArguments("update_issue", { issue_number: 42, milestone }, undefined, updateTool.inputSchema);
  assert.deepEqual(normalized, { issue_number: 42, milestone });
  assert.equal(validateArgumentsAgainstSchema(normalized, updateTool.inputSchema), null);
  const result = handlers.updateIssueHandler(normalized);
  assert.equal(result.isError, undefined);
  assert.deepEqual(recorded.at(-1), { type: "update_issue", issue_number: 42, milestone });
  const entry = JSON.parse(JSON.stringify(recorded.at(-1)));
  assert.deepEqual(validateItem(entry, entry.type, 1), {
    isValid: true,
    normalizedItem: { type: "update_issue", issue_number: 42, milestone: milestone === null ? null : 1 }
  });
}
const clearedIssueType = validateItem({ type: "set_issue_type", issue_type: "" }, "set_issue_type", 1);
assert.equal(clearedIssueType.isValid, true, clearedIssueType.error);
assert.equal(clearedIssueType.normalizedItem.issue_type, "");
const missingIssueType = validateItem({ type: "set_issue_type" }, "set_issue_type", 1);
assert.equal(missingIssueType.isValid, false, "set_issue_type still requires an explicit issue_type value");
console.log("Checked " + checked + " optional-field blank cases and collector regressions");
`
