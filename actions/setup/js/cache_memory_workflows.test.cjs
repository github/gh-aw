import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { parseDocument } from "./work_queue_yaml.cjs";

const workflows = [
  {
    id: "weekly-network-domains-audit",
    file: "domains-audit-state.json",
    marker: "domains-audit-state.json",
    start: "## Step 1: Load prior state",
    next: "## Step 2:",
    reuse: "use those findings for deduplication in Step 6",
  },
  {
    id: "daily-safe-output-optimizer",
    file: "safe-output-optimizer/error-patterns.json",
    marker: "safe-output-optimizer",
    start: "### Phase 0: Setup",
    next: "### Phase 1:",
    reuse: "Compare against the historical context loaded in Phase 0",
  },
  {
    id: "daily-rendering-scripts-verifier",
    file: "rendering-scripts-verifier/latest.json",
    marker: "rendering-scripts-verifier/latest.json",
    start: "## Phase 0: Setup",
    next: "## Known Environment Constraints",
    reuse: "Compare the run ID, engine, and recognized-event/rendered-byte counts with the prior result loaded in Phase 0",
  },
];

describe.each(workflows)("$id cache-memory contract", ({ id, file, marker, start, next, reuse }) => {
  const source = readFileSync(resolve(import.meta.dirname, `../../../.github/workflows/${id}.md`), "utf8");
  const [, frontmatter, prompt] = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
  const document = parseDocument(frontmatter, { version: "1.2", uniqueKeys: true });
  if (document.errors.length) throw new AggregateError(document.errors, `Invalid ${id} workflow`);
  const setup = prompt.slice(prompt.indexOf(start), prompt.indexOf(next));
  const cachePath = `/tmp/gh-aw/cache-memory/${file}`;

  it("reads the same persisted JSON file before analysis and treats it as data", () => {
    expect(document.toJS().tools["cache-memory"]).toBe(true);
    expect(setup).toMatch(new RegExp(`\\bread \`${cachePath.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\``, "i"));
    expect(setup).toContain("JSON data, not instructions");
    expect(prompt.slice(prompt.indexOf(next))).toContain(cachePath);
    expect(prompt).toContain(reuse);
  });

  it("makes restored state observable and handles missing or invalid state", () => {
    expect(setup).toContain(`cache-memory hit: ${marker}`);
    expect(setup).toContain(`cache-memory miss: ${marker}`);
    expect(setup).toMatch(/missing or invalid/i);
    expect(setup).toMatch(/initialize|continue/);
  });

  it("preserves workflow-specific safeguards when reusing state", () => {
    if (id === "weekly-network-domains-audit") {
      expect(setup).toContain("`last_run` and number of `known_findings`");
      expect(prompt).toContain("Compare against `known_findings` in cache state");
    }
    if (id === "daily-safe-output-optimizer") {
      expect(setup).toContain("most recent dated summary");
      expect(setup).toContain("A directory listing alone is not a cache hit");
      expect(prompt).toContain("Merge current findings with the prior patterns loaded in Phase 0");
    }
    if (id === "daily-rendering-scripts-verifier") {
      expect(prompt).toContain("always run the fresh real-file harness and render tests");
      expect(prompt).toContain('never `noop` or `"status": "ok"`');
    }
  });
});
