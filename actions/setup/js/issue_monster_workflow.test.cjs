import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

function readWorkflow(id) {
  const source = readFileSync(resolve(import.meta.dirname, `../../../.github/workflows/${id}.md`), "utf8");
  const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
  if (!match) throw new Error(`Could not find YAML frontmatter in ${id}.md`);
  return { frontmatter: match[1], prompt: match[2] };
}

describe("Issue Monster scheduling contract", () => {
  const { frontmatter, prompt } = readWorkflow("issue-monster");

  it("checks for work every fifteen minutes and fills the bounded batch", () => {
    expect(frontmatter).toContain("schedule: every 15m");
    expect(prompt).toContain("every 15 minutes");
    expect(prompt).not.toContain("every 30 minutes");
    expect(prompt).toContain("Fill all three slots when suitable independent issues are available");
    expect(prompt).toContain("continue down the prioritized list rather than stopping early");
    expect(frontmatter).toContain("max: 3");
  });

  it("retains approval, concurrency, and no-op safeguards", () => {
    expect(frontmatter).toContain("required-labels: [cookie]");
    expect(frontmatter).toContain("cancel-in-progress: false");
    expect(frontmatter).toContain("max: 5");
    expect(frontmatter).toContain('query: "is:pr is:open author:app/copilot-swe-agent -label:broccoli"');
    expect(frontmatter).toContain("- build\n      - test\n      - lint-go\n      - lint-js");
    expect(frontmatter).toContain("id: search");
    expect(frontmatter).toContain("if (rateLimitDetected)");
    expect(frontmatter).toContain("copilot-retry-blocked");
    expect(frontmatter).toContain("label:cookie");
    expect(prompt).toContain("Do not force assignments just to reach the maximum");
    expect(prompt).toContain("No overlapping file changes expected");
  });

  it("receives domains-audit findings as cookie-approved queue items", () => {
    const { frontmatter } = readWorkflow("weekly-network-domains-audit");
    expect(frontmatter).toContain("labels: [cookie, security]");

    const lock = readFileSync(resolve(import.meta.dirname, "../../../.github/workflows/weekly-network-domains-audit.lock.yml"), "utf8");
    const configLine = lock.match(/^\s+GH_AW_SAFE_OUTPUTS_CONFIG:\s+("(?:\\.|[^"])*")$/m);
    expect(configLine).not.toBeNull();
    const safeOutputs = JSON.parse(JSON.parse(configLine[1]));
    expect(safeOutputs.create_issue.labels).toEqual(["cookie", "security"]);
  });
});
