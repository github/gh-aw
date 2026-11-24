import { describe, expect, it } from "vitest";

const fs = require("node:fs");
const path = require("node:path");
const limits = require("./work_queue_limits.cjs");
const { ROLES } = require("./work_queue_policy.cjs");
const { CLAIM_CANCELLATION_REASONS, OP_FIELDS, OP_ROLES, PROTOCOL_VERSION, RELEASE_REASONS, REQUEST_KINDS, WORK_CANCELLATION_REASONS } = require("./work_queue_replay.cjs");
const repositoryRoot = path.resolve(__dirname, "../../..");
const deploymentPath = "docs/src/content/docs/patterns/workqueue-ops.md";
const referencePath = "docs/src/content/docs/reference/work-queue.md";
const specificationPath = "docs/src/content/docs/specs/work-queue-specification.md";
const instructionsPath = ".github/aw/work-queue.md";
const queueDocs = [deploymentPath, referencePath, specificationPath, instructionsPath];

// Protocol v3 concepts that v4 removed; current docs must not describe them.
const REMOVED_V3 =
  /policy epoch|policy_epoch|PoolInstall|DeliveryEvidence|DeliveryFailure|delivery barrier|keep_open|expires_at|retry_not_before|admission_paused|grants_paused|logical_limit|native_limit|class_weights|accounting_weights|backoff_seconds|work_queue_explain|sync-issues|transactions\.tsp|activation_pending|recovery_required|installed Policy/i;

function readRepositoryFile(file) {
  return fs.readFileSync(path.join(repositoryRoot, file), "utf8");
}

/** Cells of the Markdown table row whose first cell is `name` in backticks. */
function tableRow(source, name) {
  const line = source.split("\n").find(row => row.startsWith(`| \`${name}\` |`));
  expect(line, `table row for ${name}`).toBeDefined();
  return line.split("|").slice(1, -1);
}

function backticked(cell) {
  return [...cell.matchAll(/`([^`]+)`/g)].map(([, value]) => value);
}

describe("work-queue v4 documentation", () => {
  it("documents the version-4 work-queue tool in the generated frontmatter reference", () => {
    const source = readRepositoryFile("docs/src/content/docs/reference/frontmatter-full.md");
    const start = source.indexOf("  # Read the version-4 activation snapshot");
    expect(start).toBeGreaterThan(-1);
    const queue = source.slice(start, source.indexOf("  # Cache memory MCP configuration", start));
    expect(queue).toMatch(/^  work-queue:\s*$/m);
    expect(queue).toContain("work-queue: null");
    expect(queue).toContain("worker: true");
    expect(queue).not.toMatch(/\bstorage:/);
    expect(queue).not.toMatch(/version-3|installed/i);
  });

  it("keeps the specification tables in sync with the v4 replay contract", () => {
    const spec = readRepositoryFile(specificationPath);
    expect(PROTOCOL_VERSION).toBe(4);
    expect(spec).toContain('{"version":4,');

    for (const [kind, [required, optional]] of Object.entries(OP_FIELDS)) {
      const [, requiredCell, optionalCell] = tableRow(spec, kind);
      expect(backticked(requiredCell).sort(), `${kind} required fields`).toEqual([...required].sort());
      expect(backticked(optionalCell).sort(), `${kind} optional fields`).toEqual([...optional].sort());
    }
    expect(Object.keys(OP_FIELDS)).not.toEqual(expect.arrayContaining(["Policy", "PoolInstall", "Deployment", "Control", "Result", "DeliveryFailure"]));

    for (const [kind, { operations, parameters }] of Object.entries(REQUEST_KINDS)) {
      const [, parameterCell, operationCell] = tableRow(spec, kind);
      expect(backticked(operationCell).sort(), `${kind} operations`).toEqual([...operations].sort());
      expect(backticked(parameterCell), `${kind} parameters`).toEqual(expect.arrayContaining(parameters));
    }

    const reasons = { ClaimCancellation: CLAIM_CANCELLATION_REASONS, WorkCancellation: WORK_CANCELLATION_REASONS, Release: RELEASE_REASONS };
    const reasonSection = spec.slice(spec.indexOf("### 6.2 Reasons"), spec.indexOf("### 6.3 Roles"));
    for (const [kind, allowed] of Object.entries(reasons)) {
      expect(backticked(tableRow(reasonSection, kind)[1]).sort(), `${kind} reasons`).toEqual([...allowed].sort());
    }

    const roleSection = spec.slice(spec.indexOf("### 6.3 Roles"), spec.indexOf("## 7. Projection"));
    for (const [kind, roles] of Object.entries(OP_ROLES)) {
      const row = roleSection.split("\n").find(line => line.startsWith("| ") && backticked(line.split("|")[1]).includes(kind));
      expect(row, `role row for ${kind}`).toBeDefined();
      expect(
        backticked(row.split("|")[2])
          .filter(value => ROLES.includes(value))
          .sort(),
        `${kind} roles`
      ).toEqual([...roles].sort());
    }

    for (const name of ["PAYLOAD_BYTES", "OPERATIONS_PER_COMMIT", "GRAPH_NODES", "PREDECESSORS", "ASSIGNMENT_BYTES", "EFFECTS_BYTES", "LEDGER_BYTES", "CHECKPOINT_AFTER_LINES", "MAX_PENDING_WORK"]) {
      expect(tableRow(spec, name)[1].trim(), name).toBe(String(limits[name]));
    }
  });

  it("describes only the v4 time-free protocol", () => {
    for (const file of queueDocs) {
      const source = readRepositoryFile(file);
      const stale = source.split("\n").filter(line => REMOVED_V3.test(line));
      expect(stale, file).toEqual([]);
    }
    const reference = readRepositoryFile(referencePath);
    expect(reference).toContain("Git-backed");
    expect(reference).toContain("No decision reads a clock");
    expect(readRepositoryFile(specificationPath)).toContain("replay MUST NOT read it");
  });

  it("separates published docs and specifications from bounded agent instructions", () => {
    for (const file of [deploymentPath, referencePath, specificationPath]) {
      const frontmatter = readRepositoryFile(file).match(/^---\n([\s\S]*?)\n---/);
      expect(frontmatter).not.toBeNull();
      expect(frontmatter[1]).toMatch(/^title: [^\n]+$/m);
      expect(frontmatter[1]).toMatch(/^description: [^\n]+$/m);
    }
    expect(fs.existsSync(path.join(repositoryRoot, "specs/work-queue/WorkQueueV4.tla"))).toBe(true);
    expect(fs.existsSync(path.join(repositoryRoot, "specs/work-queue/transactions.tsp"))).toBe(false);

    const instructions = readRepositoryFile(instructionsPath);
    expect(instructions.trim().split(/\s+/).length).toBeLessThanOrEqual(800);
    expect(instructions).not.toContain("```json");
    for (const file of [deploymentPath, referencePath, specificationPath]) {
      const linked = [...instructions.matchAll(/\[[^\]\n]*\]\(([^)#]+)(?:#[^)]*)?\)/g)].map(([, link]) => path.resolve(repositoryRoot, path.dirname(instructionsPath), link));
      expect(linked).toContain(path.join(repositoryRoot, file));
    }
    for (const invariant of [
      "issue-backed WorkQueueOps",
      "Absent branches are empty",
      "reject malformed/unsupported logs",
      "max_claims",
      "max_dispatches",
      "Standalone seeding is unsupported",
      "Repository access rules are GitHub's responsibility",
      "`max_attempts`",
      "No clocks",
    ]) {
      expect(instructions).toContain(invariant);
    }
  });

  it("routes queue tasks through both installed and checked-in dispatchers", () => {
    for (const file of [".github/skills/agentic-workflows/SKILL.md", "pkg/cli/data/agentic_workflows_skill.md"]) {
      const dispatcher = readRepositoryFile(file);
      expect(dispatcher).toContain("Design, deploy, inspect or recover a Git-backed work queue: `.github/aw/work-queue.md`");
      expect(dispatcher).toContain("also load `.github/aw/work-queue.md` after the primary prompt");
    }
    for (const file of [".github/agents/agentic-workflows.md", "pkg/cli/data/agentic_workflows_agent.md"]) {
      const dispatcher = readRepositoryFile(file);
      expect(dispatcher).toContain("https://raw.githubusercontent.com/github/gh-aw/main/.github/aw/work-queue.md");
      expect(dispatcher).toContain("Also load it after the primary create/update/debug/upgrade prompt");
    }
    const fallback = JSON.parse(readRepositoryFile("pkg/cli/data/agentic_workflows_fallback_aw_files.json"));
    expect(fallback).toContain("work-queue.md");
    expect(readRepositoryFile(".github/aw/patterns.md")).toContain("### Git-backed Work Queue");
    const automation = readRepositoryFile(".github/aw/safe-outputs-automation.md");
    expect(automation).toContain("use `work_queue_dispatch_next`");
    expect(automation).not.toContain('pass `work_queue: {work_id: "<id>"}`');
  });

  it("treats genuine factory queue absence as an empty backlog", () => {
    const dispatcher = readRepositoryFile(".github/workflows/eslint-factory-dispatcher.md");
    expect(dispatcher).toContain('work-queue work_queue_read \'{"pool":"default","limit":32}\'');
    expect(dispatcher).toContain("Treat it as an empty backlog; the first trusted producer submission");
    expect(dispatcher).toContain("shared/work-queue-staged-plan.md");
    expect(readRepositoryFile(".github/workflows/shared/work-queue-staged-plan.md")).toContain("Trusted processing verifies the exact batch");
    expect(dispatcher).toContain('{"pool":"default","max_claims":3,"max_dispatches":3}');
    expect(dispatcher).toContain('`status: "staged"`');
    expect(dispatcher).toContain("an empty snapshot or a prediction of no eligible Work can be stale");
    expect(dispatcher).toContain("Do not call ordinary `dispatch_workflow`");

    const factory = readRepositoryFile("docs/src/content/docs/gallery/linter-factory.md");
    expect(factory).toContain("gh aw work-queue --repo OWNER/REPO stats --json");
    expect(factory.split("\n").filter(line => REMOVED_V3.test(line))).toEqual([]);
  });

  it("resolves queue documentation links after relocation", () => {
    const files = [...queueDocs, "specs/work-queue/README.md", "docs/src/content/docs/gallery/daily-report-portfolio.md", "docs/src/content/docs/gallery/linter-factory.md"];
    for (const file of files) {
      const source = readRepositoryFile(file);
      for (const [, link] of source.matchAll(/\[[^\]\n]*\]\(([^)\s]+)\)/g)) {
        const [destination, anchor] = link.split("#");
        let target;
        const repositoryURL = destination.match(/^https:\/\/github\.com\/github\/gh-aw\/(?:blob|tree)\/main\/(.+)$/);
        if (repositoryURL) target = path.join(repositoryRoot, repositoryURL[1]);
        else if (destination.startsWith("/gh-aw/")) target = path.resolve(repositoryRoot, "docs/src/content/docs", destination.slice("/gh-aw/".length));
        else if (/^[a-z]+:/.test(destination) || destination.startsWith("/")) continue;
        else target = destination ? path.resolve(repositoryRoot, path.dirname(file), destination) : path.join(repositoryRoot, file);

        if (destination.endsWith("/")) {
          const candidates = [`${target}.md`, `${target}.mdx`, path.join(target, "index.md"), path.join(target, "index.mdx")];
          target = candidates.find(candidate => fs.existsSync(candidate)) ?? target;
        }
        expect(fs.existsSync(target), `${file}: ${link}`).toBe(true);
        if (anchor && target.endsWith(".md")) {
          const headings = [...fs.readFileSync(target, "utf8").matchAll(/^#{1,6} (.+)$/gm)].map(([, heading]) =>
            heading
              .toLowerCase()
              .replace(/[^\p{L}\p{N}_\s-]/gu, "")
              .replace(/\s/g, "-")
          );
          expect(headings, `${file}: ${link}`).toContain(anchor);
        }
      }
    }
  });
});
