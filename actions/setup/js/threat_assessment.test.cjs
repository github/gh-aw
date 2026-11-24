import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { prepareAssessment } from "./threat_assessment.cjs";
import { analyzeScaffolding } from "./threat_scaffolding.cjs";
import { extractInputs } from "./threat_static.cjs";

describe("pinned static analysis", () => {
  it("aligns rendered regions to template boundaries without treating template words as runtime input", () => {
    expect(extractInputs("Task: {{task}}\nContext: {{context}}", "Task: fix the bug\nContext: src/main.go has an error").inputs).toEqual([
      { placeholder: "{{task}}", content: "fix the bug" },
      { placeholder: "{{context}}", content: "src/main.go has an error" },
    ]);
    expect(extractInputs("Before {{empty}} After", "Before  After")).toEqual({ inputs: [], indeterminate: true });
    expect(extractInputs("Static content", "Static content")).toEqual({ inputs: [], indeterminate: false });
  });

  it("trusts only a bounded leading system block and strips template scaffolding only for the whole-line host marker", () => {
    const marker = "[gh-aw framework system prompt block removed before analysis]";
    const template = '\n<SYSTEM role="framework">\n<github-context>\n</ system >\n\nWorkflow {{actor}}';
    const result = analyzeScaffolding(marker + "\nWorkflow bob", template);
    expect(result.section).toContain("Lines 2-4 of the prompt template");
    expect(result.section).toContain("`<github-context>`");
    expect(result.template).toBe(marker + "\nWorkflow {{actor}}");
    expect(analyzeScaffolding(marker + " extra\nbody", template).section).toBe("");
    expect(analyzeScaffolding("body\n" + template, "").section).toBe("");
    expect(analyzeScaffolding("<system>" + "é".repeat(65536) + "</system>", "").section).toBe("");
  });
});

describe("assessment preparation", () => {
  let dir;
  let log;
  function write(relative, value) {
    const file = path.join(dir, relative);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, value);
    return file;
  }
  const prepare = opts => prepareAssessment({ artifactsDir: dir, ...opts });
  beforeEach(() => {
    dir = fs.mkdtempSync(path.resolve(".threat-assessment-"));
    write("aw-prompts/prompt.txt", "Static content");
    write("aw-prompts/prompt-template.txt", "Static content");
    write("aw-prompts/prompt-import-tree.json", "{}");
    for (const name of ["GH_AW_DETECTION_CONTINUE_ON_ERROR", "GH_AW_DETECTION_CONTINUE_ON_WARNING", "HAS_PATCH", "CUSTOM_PROMPT"]) vi.stubEnv(name, "");
    log = vi.spyOn(process.stderr, "write").mockReturnValue(true);
  });
  afterEach(() => {
    fs.rmSync(dir, { force: true, recursive: true });
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it.each([
    ["no mutable channels", () => {}, false, false, false],
    ["non-empty agent output", () => write("agent_output.json", "{}"), false, true, false],
    ["top-level patch", () => write("aw-changes.patch", "diff"), false, true, true],
    ["empty but staged patch", () => write("aw-changes.patch", ""), false, true, true],
    ["nested patch ignored", () => write("other/aw-p.patch", "diff"), false, false, false],
    ["missing declared patch", () => vi.stubEnv("HAS_PATCH", "true"), false, true, true],
    ["comment memory", () => write("comment-memory/a.md", ""), true, true, false],
    ["refused memory", () => fs.symlinkSync("aw-prompts", path.join(dir, "comment-memory")), true, true, false],
    ["variable template", () => write("aw-prompts/prompt-template.txt", "Static {{item}}"), true, false, false],
    ["missing template", () => fs.unlinkSync(path.join(dir, "aw-prompts/prompt-template.txt")), true, false, false],
    ["template failed to align", () => write("aw-prompts/prompt-template.txt", "Unknown {{item}}"), true, false, false],
    ["activation context", () => write("aw_info.json", '{"actor":"bob"}'), true, false, false],
  ])("derives structural eligibility: %s", (_name, stage, injection, secret, patch) => {
    stage();
    expect(prepare().eligibility).toEqual({ prompt_injection: injection, secret_leak: secret, malicious_patch: patch });
  });

  it("renders trusted options and allowlisted metadata without re-expanding artifact values", () => {
    write("aw_info.json", '{"model":"{BUDGET}","context":{"run_id":9999999999999999999,"private":"redact-me"},"private":"redact-me"}');
    write("agent_output.json", "{}");
    const template = write("custom.md", "{WORKFLOW_NAME}|{WORKFLOW_DESCRIPTION}|{ACTIVATION_CONTEXT}");
    const custom = write("extra.md", "file overrides flag");
    vi.stubEnv("WORKFLOW_NAME", "unused");
    const result = prepare({ workflowName: "", workflowDescription: "$&", customPrompt: "unused", customPromptFile: custom, promptTemplate: template, timeoutMs: 90000, maxTurns: 0 });
    expect(result.prompt).toMatch(/^\|\$&\|\{/);
    expect(result.prompt).toContain('"model": "{BUDGET}"');
    expect(result.prompt).toContain('"run_id": "9999999999999999999"');
    expect(result.prompt).not.toContain("redact-me");
    expect(result.prompt).toContain("## Prompt Analysis (Trusted vs Untrusted Content)");
    expect(result.prompt).toContain("**wall-clock timeout 90s**, **agentic turn cap unlimited**");
    expect(result.prompt).toMatch(/## Additional Instructions\n\nfile overrides flag$/);
    expect(result.warnings).toEqual([]);
  });

  it("distinguishes unreadable from absent provenance and enforces the two policy switches independently", () => {
    write("agent_output.json", "[]");
    fs.unlinkSync(path.join(dir, "aw-prompts/prompt-template.txt"));
    fs.mkdirSync(path.join(dir, "aw-prompts/prompt-template.txt"));
    write("aw-prompts/prompt-import-tree.json", " ");
    vi.stubEnv("GH_AW_DETECTION_CONTINUE_ON_ERROR", "FALSE");
    const result = prepare();
    expect(result.warnings).toEqual([
      { field: "prompt_template", code: "ERR_VALIDATION", message: expect.stringContaining("could not be read") },
      { field: "prompt_import_tree", code: "ERR_VALIDATION", message: expect.stringContaining("Missing or empty") },
    ]);
    expect(result.prompt).toContain("### Uninspectable Prompt Analysis Inputs");
    expect(result.prompt).toContain("does NOT establish that no untrusted content reached");
    expect(result.eligibility.prompt_injection).toBe(true);
    vi.stubEnv("GH_AW_DETECTION_CONTINUE_ON_WARNING", "False");
    expect(() => prepare()).toThrow(/partially readable/);
    expect(log).toHaveBeenCalledWith(expect.stringMatching(/^::error::ERR_VALIDATION:.*Missing or empty/));
  });

  it("refuses empty input even in warn mode, and reports strict failures before throwing", () => {
    fs.unlinkSync(path.join(dir, "aw-prompts/prompt.txt"));
    write("agent_output.json", "{");
    expect(() => prepare()).toThrow(/empty input/);
    expect(log).toHaveBeenCalledWith(expect.stringMatching(/^::warning::.*not valid JSON/));
    write("aw-foo.bundle", "non-empty");
    vi.stubEnv("GH_AW_DETECTION_CONTINUE_ON_ERROR", "False");
    expect(() => prepare()).toThrow(/required detection inputs/i);
    expect(log).toHaveBeenCalledWith(expect.stringMatching(/^::error::.*Missing detection context/));
  });

  it("clips warnings by code point and neutralizes both workflow-command syntaxes in annotations", () => {
    fs.mkdirSync(path.join(dir, "comment-memory"));
    for (let i = 0; i < 60; i++) fs.symlinkSync("missing", path.join(dir, "comment-memory", `a%##[\r\n☃${"é".repeat(30)}${i}.md`));
    const result = prepare();
    const memory = result.warnings.find(w => w.field === "comment_memory");
    expect(Array.from(memory.message)).toHaveLength(2000);
    expect(memory.message).toMatch(/…$/);
    const annotation = log.mock.calls.find(([line]) => line.includes("Skipped"))[0];
    expect(annotation).toContain("%25##\\[%0D%0A");
    expect(annotation.trimEnd()).not.toMatch(/[\r\n]/);
  });
});
