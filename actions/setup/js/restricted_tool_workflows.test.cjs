import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { readDocumentMetadata } from "../../../scripts/markdown_metadata_test_adapter.cjs";

const require = createRequire(import.meta.url);
const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");
const root = resolve(import.meta.dirname, "../../..");
const readWorkflow = name => {
  const source = readFileSync(resolve(root, `.github/workflows/${name}.md`), "utf8");
  const [, frontmatter, prompt] = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
  const workflow = readDocumentMetadata(frontmatter, { version: "1.2", uniqueKeys: true }).value;
  const lock = readDocumentMetadata(readFileSync(resolve(root, `.github/workflows/${name}.lock.yml`), "utf8"), { version: "1.2", uniqueKeys: true }).value;
  const config = JSON.parse(lock.jobs.agent.steps.find(step => step.env?.GH_AW_COPILOT_SDK_TOOL_CONFIG).env.GH_AW_COPILOT_SDK_TOOL_CONFIG);
  const handler = buildCopilotSDKPermissionHandler(config.permissions, () => ({ kind: "approve-once" }));
  const permission = command => handler({ kind: "shell", fullCommandText: command, commands: [{ identifier: command }] }).kind;
  return { workflow, prompt, config, permission };
};

describe("restricted workflow operation contracts", () => {
  it("allows focused Formal validation without granting arbitrary Go or Git commands", () => {
    const { workflow, prompt, config, permission } = readWorkflow("daily-formal-spec-verifier");
    expect(workflow.tools.bash).toContain("go test:*");
    expect(workflow.tools.bash).toContain("gofmt -w:*");
    expect(config.permissions.allowedTools).toContain("read");
    const handler = buildCopilotSDKPermissionHandler(config.permissions, () => ({ kind: "approve-once" }));
    expect(handler({ kind: "read", path: "/tmp/gh-aw/agent/formal-spec-verifier/replace-label-spec/sany-example/evidence.json" }).kind).toBe("approve-once");
    expect(workflow.tools.bash).not.toContain("cat /tmp/gh-aw/agent/formal-spec-verifier/*");
    expect(prompt).toContain("Built-in file inspection on the exact `evidence.json` path printed by the helper");
    expect(prompt).toContain("not a shell glob over slug directories");
    expect(prompt).toMatch(/Raw logs\s+are not persisted/);
    for (const command of [
      "go test ./pkg/workflow -run '^TestReplaceLabel' -count=1",
      "gofmt -w pkg/workflow/replace_label_formal_test.go",
      'find specs -type f -name "*.md" | sort',
      'gh pr list --state open --search "replace-label formal" --json number,title',
      "cat /tmp/gh-aw/repo-memory/default/formal-spec-verifier/notes-index.json",
      "node .github/scripts/formal_spec_check.cjs sany specs/replace-label-spec.md Model.tla",
      "node .github/scripts/formal_spec_check.cjs tlc specs/replace-label-spec.md Model.tla Model.cfg",
      "node .github/scripts/formal_spec_check.cjs koka specs/replace-label-spec.md main.kk",
    ]) {
      expect(permission(command), command).toBe("approve-once");
    }
    for (const command of [
      "go version",
      "go run ./cmd/gh-aw",
      "git log --oneline -5 -- specs/replace-label-spec.md",
      "go test ./pkg/workflow; python3 -c 'print(1)'",
      "gofmt -w pkg/workflow/replace_label_formal_test.go && go version",
      "node -e 'console.log(1)'",
      "cd specs/formal/replace-label-spec",
      "timeout 2m /tmp/gh-aw/agent/koka/bin/koka main.kk",
    ]) {
      expect(permission(command), command).toBe("reject");
    }
  });

  it("allows only finite Delight help commands without discovery probes", () => {
    const { workflow, prompt, permission } = readWorkflow("delight");
    expect(workflow.tools.bash).not.toContain("./gh-aw * --help");
    for (const command of ["./gh-aw --help", "./gh-aw compile --help", "./gh-aw run --help"]) {
      expect(workflow.tools.bash).toContain(command);
      expect(prompt).toContain(command);
      expect(permission(command), command).toBe("approve-once");
    }
    for (const command of [
      "./gh-aw run daily-formal-spec-verifier",
      "./gh-aw compile --help && ./gh-aw run daily-formal-spec-verifier",
      "which gh-aw",
      "cd /tmp/gh-aw/repo-memory/default && cat improvements.json",
      "python3 -c \"import json;json.load(open('improvements.json'))\"",
    ]) {
      expect(permission(command), command).toBe("reject");
    }
    expect(permission("cat /tmp/gh-aw/repo-memory/default/improvements.json")).toBe("approve-once");
    expect(permission("cat /tmp/gh-aw/agent/previous-findings.json")).toBe("approve-once");
  });

  it.each(["daily-formal-spec-verifier", "delight"])("%s preserves limits and tells agents how to recover from the first denial", name => {
    const { workflow, prompt, config } = readWorkflow(name);
    expect(workflow["max-tool-denials"]).toBe(3);
    expect(workflow.strict).toBe(true);
    expect(config.capabilities.dynamicWorkflows).toBe(false);
    expect(config.permissions.allowedTools).not.toContain("shell");
    expect(config.permissions.allowedTools).not.toContain("workflow");
    expect(config.permissions.allowedTools).not.toContain("shell(git:*)");
    expect(config.permissions.allowedTools).not.toContain("shell(go:*)");
    expect(config.permissions.allowedTools).not.toContain("shell(python3)");
    expect(prompt).toContain("On the first tool denial");
    expect(prompt).toContain("one standalone command per call");
    expect(prompt).toContain("not a GitHub authentication error");
    expect(prompt).toContain("report_incomplete");
  });

  it("uses the actual restored memory directory throughout Delight setup and persistence", () => {
    const { workflow, prompt } = readWorkflow("delight");
    const setup = workflow["pre-agent-steps"].find(step => step.name === "Sample files and load memory").run;
    expect(setup).toContain("cat /tmp/gh-aw/repo-memory/default/previous-findings.json");
    expect(setup).toContain("cat /tmp/gh-aw/repo-memory/default/improvement-themes.json");
    expect(setup).not.toContain("cat memory/delight/");
    expect(prompt).not.toContain("cat > memory/delight/");
    expect(prompt).toContain("/tmp/gh-aw/repo-memory/default/findings-<YYYY-MM-DD>.json");
    expect(prompt).toContain("/tmp/gh-aw/repo-memory/default/improvements.json");
    expect(prompt).toContain("built-in edit tools");
    expect(prompt).toMatch(/Do not use `cd`, shell\s+redirection, or Python/);
  });
});
