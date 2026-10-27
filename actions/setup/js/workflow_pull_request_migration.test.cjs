import { describe, expect, it } from "vitest";
import fs from "node:fs";
import { parseDocument } from "./work_queue_yaml.cjs";

// Latest 100 merged PRs: these workflows produced closing issues with gh-aw provenance.
const migrated = [
  ["daily-safe-outputs-conformance", 71574, 71614],
  ["sergo", 71810, 71896],
  ["duplicate-code-detector", 71713, 71893],
  ["cli-version-checker", 72019, 72099],
  ["static-analysis-report", 71826, 71898],
  ["daily-model-inventory", 71514, 71620],
  ["lint-monster", 71532, 71889],
  ["daily-cli-performance", 71918, 71927],
  ["daily-safe-output-optimizer", 71952, 72095],
  ["daily-formal-spec-verifier", 71465, 71625],
  ["ab-testing-advisor", 71628, 71666],
];
const retained = ["semantic-function-refactor", "go-fan"];

describe("code-producing issue workflow migration", () => {
  it("converts at least eighty percent while retaining read-only and experiment-sensitive analysis", () => {
    expect(migrated.length / (migrated.length + retained.length)).toBeGreaterThanOrEqual(0.8);
  });

  it.each(migrated)("%s delivers protected implementation pull requests (issue #%i -> merged PR #%i)", (id, _issue, _pullRequest) => {
    const source = fs.readFileSync(new URL(`../../../.github/workflows/${id}.md`, import.meta.url), "utf8");
    const frontmatter = source.match(/^---\n([\s\S]*?)\n---/)[1];
    const document = parseDocument(frontmatter, { version: "1.2", uniqueKeys: true });
    expect(document.errors).toEqual([]);
    const config = document.toJS();
    expect(config.source).toBeUndefined();
    expect(config["safe-outputs"]["create-issue"]).toBeUndefined();
    expect(config["safe-outputs"]["create-pull-request"]["exclude-lock-yml"]).toBe(true);
    expect(config.imports).toContain("shared/lock-safe-pull-request.md");
    expect(JSON.stringify(config.imports)).not.toContain("shared/daily-issue-base.md");
    const lock = fs.readFileSync(new URL(`../../../.github/workflows/${id}.lock.yml`, import.meta.url), "utf8");
    expect(lock).toContain('\\"exclude_lock_yml\\":true');
  });
});
