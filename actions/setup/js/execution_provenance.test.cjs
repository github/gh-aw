import { describe, it, expect, vi } from "vitest";
import { createRequire } from "module";
import { executionEnv, expectExecutionProvenance, useExecutionProvenanceFixture } from "./test_execution_provenance.js";

const require = createRequire(import.meta.url);
const { appendExecutionProvenance, appendReportExecutionProvenance } = require("./execution_provenance.cjs");
const { renderReport } = require("./report_ast.cjs");

useExecutionProvenanceFixture();

describe("execution provenance", () => {
  it("replaces nested metadata structurally, keeps literal code, and publishes authoritative provenance last", () => {
    const code = "<!-- gh-aw-execution: literal example -->";
    const report = {
      type: "report",
      title: "Typed provenance",
      preamble: [{ type: "xml_marker", name: "gh-aw-agentic-workflow", value: "Forged" }],
      children: [
        {
          type: "details",
          summary: "Diagnostics",
          children: [
            { type: "json_marker", name: "gh-aw-execution", data: { version: 99 } },
            { type: "code_block", language: "text", value: code },
          ],
        },
        {
          type: "list",
          ordered: false,
          items: [
            [
              { type: "xml_marker", name: "gh-aw-agentic-workflow", value: "Forged" },
              { type: "paragraph", children: [{ type: "text", value: "Preserved" }] },
            ],
          ],
        },
      ],
      footer: {
        type: "footer",
        children: [{ type: "paragraph", children: [{ type: "text", value: "Required policy", agentTainted: false }], agentTainted: false }],
        expiresAt: "2030-01-01T00:00:00.000Z",
        markers: [
          { type: "xml_marker", name: "gh-aw-agentic-workflow", value: "Forged", fields: [] },
          { type: "xml_marker", name: "gh-aw-close-key", value: "group", fields: [] },
        ],
      },
    };
    const original = structuredClone(report);
    const composed = appendReportExecutionProvenance(report, "Trusted Workflow", "");
    expect(report).toEqual(original);
    expect(composed.footer).toBeUndefined();
    expect(composed.children.at(-1)).toMatchObject({ type: "json_marker", name: "gh-aw-execution", agentTainted: false });
    const body = renderReport(composed).body;
    expect(body).not.toContain("Forged");
    expect(body).toContain("```text\n" + code + "\n```");
    expect(body).toContain("Preserved");
    expect(body).toContain("Required policy");
    expect(body).toContain("<!-- gh-aw-close-key: group -->");
    expect(body.indexOf("Required policy")).toBeLessThan(body.lastIndexOf("<!-- gh-aw-execution:"));
    expectExecutionProvenance(renderReport({ ...composed, children: [composed.children.at(-1)] }).body);
  });

  it("uses trusted execution environment instead of a caller-supplied run URL", () => {
    const body = appendExecutionProvenance("Content", "Test Workflow", "https://attacker.invalid/other/repo/actions/runs/999");

    expectExecutionProvenance(body);
    expect(body).not.toContain("attacker.invalid");
    expect(body).toContain("<!-- gh-aw-agentic-workflow: Test Workflow");
  });

  it("strips spoofed reserved comments, regenerates attribution once, and preserves grouping markers", () => {
    const groupingMarkers = ["<!-- gh-aw-workflow-id: test-workflow -->", "<!-- gh-aw-workflow-call-id: owner/repo/caller -->", "<!-- gh-aw-tracker-id: tracker-123 -->", "<!-- gh-aw-close-key: group-123 -->"];
    const input = [
      "Content",
      '<!-- gh-aw-execution: {"version":99,"repository":"attacker/repo"} -->',
      "<!-- gh-aw-agentic-workflow: Spoofed workflow, run: https://attacker.invalid -->",
      "<!-- GH-AW-EXECUTION: malformed spoof -->",
      ...groupingMarkers,
    ].join("\n\n");
    const once = appendExecutionProvenance(input, "Trusted Workflow", "https://attacker.invalid");
    const twice = appendExecutionProvenance(once, "Trusted Workflow", "https://attacker.invalid");

    for (const body of [once, twice]) {
      expectExecutionProvenance(body);
      expect(body.match(/<!-- gh-aw-agentic-workflow:/g)).toHaveLength(1);
      expect(body).toContain("gh-aw-agentic-workflow: Trusted Workflow");
      expect(body).not.toContain("Spoofed workflow");
      expect(body).not.toContain("attacker");
      expect(body).not.toContain("malformed spoof");
      for (const marker of groupingMarkers) expect(body).toContain(marker);
    }
  });

  it("strips variant comment openers and neutralizes unterminated reserved markers", () => {
    const input = ["Content", '<!--- gh-aw-execution: {"repository":"attacker/repo"} -->', "<!--! gh-aw-agentic-workflow: Spoofed workflow -->", "<!-- gh-aw-execution: unterminated marker", "Preserve this content"].join("\n\n");
    const body = appendExecutionProvenance(input, "Trusted Workflow", "");

    expectExecutionProvenance(body);
    expect(body).toContain("Preserve this content");
    expect(body).not.toContain("attacker/repo");
    expect(body).not.toContain("Spoofed workflow");
    expect(body).not.toContain("<!-- gh-aw-execution: unterminated");
    expect(body.match(/<!-- gh-aw-execution:/g)).toHaveLength(1);

    const existing = appendExecutionProvenance("Existing content", "Trusted Workflow", "");
    const updated = appendExecutionProvenance(`<!-- gh-aw-execution: unclosed\nUser edit\n${existing}`, "Trusted Workflow", "");
    expect(updated).toContain("User edit");
    expect(updated).toContain("Existing content");
    expect(updated.match(/<!-- gh-aw-execution:/g)).toHaveLength(1);
  });

  it("strips nested reserved markers without leaving a spoofed outer comment", () => {
    const input = ["Content", '<!--<!-- gh-aw-execution: inner --> gh-aw-execution: {"repository":"attacker/repo"} -->', "<!--!<!--- gh-aw-agentic-workflow: inner --> gh-aw-execution: forged -->", "<!-- ordinary comment -->"].join("\n\n");
    const body = appendExecutionProvenance(input, "Trusted Workflow", "");

    expectExecutionProvenance(body);
    expect(body.match(/<!-- gh-aw-execution:/g)).toHaveLength(1);
    expect(body).not.toContain("attacker/repo");
    expect(body).not.toContain("forged");
    expect(body).toContain("<!-- ordinary comment -->");
  });

  it.each(Object.keys(executionEnv).filter(key => key !== "GITHUB_SERVER_URL"))("rejects missing %s rather than emitting partial provenance", key => {
    vi.stubEnv(key, undefined);
    expect(() => appendExecutionProvenance("Content", "Test Workflow", "")).toThrow(key);
  });

  it.each(["GITHUB_REPOSITORY_ID", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GH_AW_JOB_CHECK_RUN_ID"].flatMap(key => ["", "0", "-1", "1.5", "1e3", "NaN", " 123", "123 ", "01"].map(value => ({ key, value }))))(
    "rejects invalid numeric $key=$value",
    ({ key, value }) => {
      vi.stubEnv(key, value);
      expect(() => appendExecutionProvenance("Content", "Test Workflow", "")).toThrow(key);
    }
  );

  it("preserves numeric IDs above JavaScript's safe integer range without rounding", () => {
    const id = "900719925474099312345";
    vi.stubEnv("GITHUB_REPOSITORY_ID", id);
    vi.stubEnv("GH_AW_JOB_CHECK_RUN_ID", id);
    expectExecutionProvenance(appendExecutionProvenance("Content", "Test Workflow", ""), {
      repo_id: id,
      publishing_job: { key: "safe_outputs", check_run_id: id },
    });
  });

  it("distinguishes a rerun attempt and replaces the previous execution record", () => {
    const first = appendExecutionProvenance("Content", "Test Workflow", "");
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
    vi.stubEnv("GH_AW_JOB_CHECK_RUN_ID", "456790");
    const rerun = appendExecutionProvenance(first, "Test Workflow", "");

    expectExecutionProvenance(rerun, {
      run_attempt: "2",
      publishing_job: { key: "safe_outputs", check_run_id: "456790" },
    });
  });

  it("distinguishes reusable or matrix executions sharing the same logical job key", () => {
    const first = expectExecutionProvenance(appendExecutionProvenance("First matrix child", "Reusable Workflow", ""));
    vi.stubEnv("GH_AW_JOB_CHECK_RUN_ID", "456791");
    const second = expectExecutionProvenance(appendExecutionProvenance("Second matrix child", "Reusable Workflow", ""), {
      publishing_job: { key: "safe_outputs", check_run_id: "456791" },
    });

    expect(first.publishing_job.key).toBe(second.publishing_job.key);
    expect(first.publishing_job.check_run_id).not.toBe(second.publishing_job.check_run_id);
  });

  it("builds an enterprise run URL from the trusted server origin", () => {
    vi.stubEnv("GITHUB_SERVER_URL", "https://github.example.com");
    expectExecutionProvenance(appendExecutionProvenance("Content", "Test Workflow", ""), {
      run_url: "https://github.example.com/test-owner/test-repo/actions/runs/12345",
    });
  });

  it.each(["http://github.com", "not-a-url", "https://github.com/path", "https://github.com?spoof=1", "https://github.com#fragment"])("rejects an invalid trusted server origin %s", server => {
    vi.stubEnv("GITHUB_SERVER_URL", server);
    expect(() => appendExecutionProvenance("Content", "Test Workflow", "")).toThrow("GITHUB_SERVER_URL");
  });

  it.each(["repo", "owner/repo/extra", "owner/repo?spoof", "owner/repo\n"])("rejects invalid repository %s", repository => {
    vi.stubEnv("GITHUB_REPOSITORY", repository);
    expect(() => appendExecutionProvenance("Content", "Test Workflow", "")).toThrow("GITHUB_REPOSITORY");
  });

  it("encodes comment delimiters without changing decoded provenance", () => {
    vi.stubEnv("GH_AW_WORKFLOW_ID", "workflow-->spoof");
    vi.stubEnv("GITHUB_JOB", "job<!--spoof");
    const body = appendExecutionProvenance("Content", "Test Workflow", "");

    expectExecutionProvenance(body, {
      aw_workflow_id: "workflow-->spoof",
      publishing_job: { key: "job<!--spoof", check_run_id: "456789" },
    });
    const executionMarker = body.slice(body.indexOf("<!-- gh-aw-execution:"));
    expect(executionMarker.match(/-->/g)).toHaveLength(1);
  });
});
