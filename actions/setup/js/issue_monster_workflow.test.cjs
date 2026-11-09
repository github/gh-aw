import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { parseDocument } from "./work_queue_yaml.cjs";

function readWorkflow(id) {
  const source = readFileSync(resolve(import.meta.dirname, `../../../.github/workflows/${id}.md`), "utf8");
  const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
  if (!match) throw new Error(`Could not find YAML frontmatter in ${id}.md`);
  const document = parseDocument(match[1], { version: "1.2", uniqueKeys: true });
  if (document.errors.length) throw new AggregateError(document.errors, `Invalid ${id} workflow`);
  return { config: document.toJS(), prompt: match[2] };
}

describe("Issue Monster scheduling contract", () => {
  const { config, prompt } = readWorkflow("issue-monster");

  it("checks for work every fifteen minutes and fills the bounded batch", () => {
    expect(config.on.schedule).toBe("every 15m");
    expect(prompt).toContain("every 15 minutes");
    expect(prompt).not.toContain("every 30 minutes");
    expect(prompt).toContain("Fill all three slots when suitable independent issues are available");
    expect(prompt).toContain("continue down the prioritized list rather than stopping early");
    expect(config["safe-outputs"]["assign-to-agent"].max).toBe(3);
  });

  it("retains approval, concurrency, and no-op safeguards", () => {
    expect(config["safe-outputs"]["assign-to-agent"]["required-labels"]).toEqual(["cookie"]);
    expect(config.concurrency["cancel-in-progress"]).toBe(false);
    expect(config.on["skip-if-match"].max).toBe(5);
    expect(config.on["skip-if-match"].query).toContain("is:pr is:open author:app/copilot-swe-agent");
    expect(config.on["skip-if-check-failing"].include).toEqual(["build", "test", "lint-go", "lint-js"]);
    const search = config.on.steps.find(step => step.id === "search").with.script;
    expect(search).toContain("if (rateLimitDetected)");
    expect(search).toContain("copilot-retry-blocked");
    expect(search).toContain("label:cookie");
    expect(prompt).toContain("Do not force assignments just to reach the maximum");
    expect(prompt).toContain("No overlapping file changes expected");
  });

  it("receives domains-audit findings as cookie-approved queue items", () => {
    const { config: audit } = readWorkflow("weekly-network-domains-audit");
    expect(audit["safe-outputs"]["create-issue"].labels).toContain("cookie");
    expect(audit["safe-outputs"]["create-issue"].labels).toContain("security");
    const lock = parseDocument(readFileSync(resolve(import.meta.dirname, "../../../.github/workflows/weekly-network-domains-audit.lock.yml"), "utf8"), { version: "1.2", uniqueKeys: true });
    expect(lock.errors).toEqual([]);
    const handlerConfig = Object.values(lock.toJS().jobs)
      .flatMap(job => job.steps || [])
      .find(step => step.env?.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG)?.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
    expect(handlerConfig).toBeDefined();
    expect(JSON.parse(handlerConfig).create_issue.labels).toEqual(["cookie", "security"]);
  });
});
