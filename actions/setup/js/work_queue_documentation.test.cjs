import { describe, expect, it } from "vitest";

const fs = require("node:fs");
const path = require("node:path");
const { DEFAULT_LIMITS } = require("./work_queue_limits.cjs");
const { validatePolicy } = require("./work_queue_policy.cjs");
const { buildAWPolicy } = require("./work_queue_settings.cjs");
const repositoryRoot = path.resolve(__dirname, "../../..");
const deploymentPath = "docs/src/content/docs/guides/deploy-work-queue.md";
const referencePath = "docs/src/content/docs/reference/work-queue.md";
const specificationPath = "docs/src/content/docs/specs/work-queue-specification.md";
const instructionsPath = ".github/aw/work-queue.md";

function readRepositoryFile(file) {
  return fs.readFileSync(path.join(repositoryRoot, file), "utf8");
}

describe("work-queue deployment documentation", () => {
  it("omits removed storage selectors from the generated frontmatter reference", () => {
    const source = readRepositoryFile("docs/src/content/docs/reference/frontmatter-full.md");
    const queue = source.slice(source.indexOf("  # Read the immutable version-3 activation snapshot."), source.indexOf("  # Cache memory MCP configuration"));
    expect(queue).toMatch(/^  work-queue:\s*$/m);
    expect(queue).toContain("work-queue: null");
    expect(queue).toContain("worker: true");
    expect(queue).not.toMatch(/\bstorage:/);
  });

  it("resolves documented scheduling without identity placeholders", () => {
    const source = readRepositoryFile(deploymentPath);
    const examples = [...source.matchAll(/```json(?: [^\n]*)?\n([\s\S]*?)\n```/g)];
    expect(examples).toHaveLength(2);
    const config = JSON.parse(examples[0][1]);
    const policy = buildAWPolicy({ repository: "github/gh-aw", ref: "a".repeat(40), workflows: ["eslint-refiner"], settings: config.work_queue });

    expect(validatePolicy(policy)).toBe(policy);
    expect(policy.accounting_weights).toEqual({ "": 1 });
    expect(policy.producers).toEqual({});
    expect(policy.authorization).toBe("aw");
    expect(policy.limits).toEqual({ ...DEFAULT_LIMITS, pending_nodes: 30 });
    expect(policy.pools.default).toMatchObject({ logical_limit: 3, native_limit: 3, retry: { max_attempts: 3, backoff_ms: 30000 } });
    expect(policy.pools.default.profiles["eslint-refiner"]).not.toHaveProperty("principal");
    const projection = JSON.parse(
      examples[1][1]
        .replaceAll("REPLACE_WITH_NATIVE_PRINCIPAL_ID", "12")
        .replaceAll("REPLACE_WITH_40_OR_64_HEX_COMMIT_SHA", "a".repeat(40))
        .replaceAll("REPLACE_WITH_NUMERIC_REPOSITORY_ID", "9876")
        .replaceAll("REPLACE_WITH_NUMERIC_ISSUE_ID", "9007199254740993")
        .replaceAll("REPLACE_WITH_ISSUE_NUMBER", "42")
    );
    expect(projection.projectors[0].completion_policy).toBe("keep-open");
    expect(projection.projectors[0].backing_issues[0]).toEqual({ kind: "issue", host: "github.com", repository: "github/gh-aw", repository_id: "9876", resource_id: "9007199254740993", number: "42" });
  });

  it("separates published docs and specifications from bounded agent instructions", () => {
    for (const file of [deploymentPath, referencePath, specificationPath]) {
      expect(readRepositoryFile(file)).toMatch(/^---\ntitle: [^\n]+\ndescription: [^\n]+\n/);
    }
    expect(fs.existsSync(path.join(repositoryRoot, "specs/work-queue/priority-and-fairness.md"))).toBe(false);
    expect(fs.existsSync(path.join(repositoryRoot, "specs/work-queue/transactions.tsp"))).toBe(true);

    const instructions = readRepositoryFile(instructionsPath);
    expect(instructions.trim().split(/\s+/).length).toBeLessThanOrEqual(800);
    expect(instructions).not.toContain("```json");
    expect(instructions).toContain("issue-backed WorkQueueOps");
    expect(instructions).toContain("`uninitialized` plus null SHA is empty");
    expect(instructions).toContain("Existing policyless ledgers are failures");
    expect(readRepositoryFile(referencePath)).toContain("Work queues can be Git-backed or issue-backed");
    for (const file of [deploymentPath, referencePath, specificationPath]) {
      expect(instructions).toContain(file.replace(/^docs\//, "../../docs/"));
    }
    for (const invariant of ["originally single-Claim", "max_claims", "max_dispatches", "Standalone seeding is unsupported", "Repository access rules are GitHub's responsibility", "Result only after independently verified delivery"]) {
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

  it("treats genuine factory queue absence as empty without bypassing Policy", () => {
    const dispatcher = readRepositoryFile(".github/workflows/eslint-factory-dispatcher.md");
    expect(dispatcher).toContain('work-queue work_queue_read \'{"pool":"default","limit":32}\'');
    expect(dispatcher).toContain("Check `queue_state`, not just `total`");
    expect(dispatcher).toContain("Treat it as an empty backlog; the first trusted producer submission");
    expect(dispatcher).toContain("Do not dispatch until that submission has been admitted");
    expect(dispatcher).toContain("existing policyless ledger");
    expect(dispatcher).toContain('work-queue work_queue_dispatch_next \'{"pool":"default","max_claims":3,"max_dispatches":3}\'');
    expect(dispatcher).toContain('`status: "staged"`');
    expect(dispatcher).toContain("an empty snapshot or a prediction of no eligible Work can be stale");
    expect(dispatcher).toContain("Do not call ordinary `dispatch_workflow`");

    const factory = readRepositoryFile("docs/src/content/docs/patterns/linter-factory.md");
    expect(factory).toContain("go build -o ./gh-aw ./cmd/gh-aw");
    expect(factory).toContain("./gh-aw work-queue --repo github/gh-aw stats --json");
    expect(factory).toContain("--no-baseline --json");
    expect(factory).toContain("--ref REVIEWED_REF --json");
    expect(factory).toContain("does not send a workflow-dispatch request");
    expect(factory).toContain("A missing queue cannot exercise that lifecycle");
    expect(factory).toContain("The dispatcher is also the producer");
    expect(factory).toContain("does not itself admit any Work");
  });

  it("resolves queue documentation links after relocation", () => {
    const files = [
      deploymentPath,
      referencePath,
      specificationPath,
      instructionsPath,
      "specs/work-queue/README.md",
      "docs/adr/64955-git-backed-work-queue-coordination.md",
      "docs/src/content/docs/patterns/daily-report-portfolio.md",
      "docs/src/content/docs/patterns/linter-factory.md",
      "docs/src/content/docs/patterns/workqueue-ops.md",
    ];
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
