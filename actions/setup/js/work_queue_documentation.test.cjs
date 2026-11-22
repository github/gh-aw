import { describe, expect, it } from "vitest";

const fs = require("node:fs");
const path = require("node:path");
const { DEFAULT_LIMITS } = require("./work_queue_limits.cjs");
const { validatePolicy } = require("./work_queue_policy.cjs");
const { buildAWPolicy } = require("./work_queue_settings.cjs");
const repositoryRoot = path.resolve(__dirname, "../../..");
const deploymentPath = "docs/src/content/docs/patterns/workqueue-ops.md";
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
    const example = source.match(/```json title="\.github\/workflows\/aw\.json"\n([\s\S]*?)\n```/);
    expect(example).not.toBeNull();
    const config = JSON.parse(example[1]);
    const policy = buildAWPolicy({ repository: "github/gh-aw", ref: "a".repeat(40), workflows: ["queue-worker"], settings: config.work_queue });

    expect(validatePolicy(policy)).toBe(policy);
    expect(policy.accounting_weights).toEqual({ "": 1 });
    expect(policy.producers).toEqual({});
    expect(policy.authorization).toBe("aw");
    expect(policy.limits).toEqual(DEFAULT_LIMITS);
    expect(policy.pools.default).toMatchObject({ logical_limit: 1, native_limit: 1, retry: { max_attempts: 3, backoff_ms: 30000 } });
    expect(policy.pools.default.profiles["queue-worker"]).not.toHaveProperty("principal");

    const reference = readRepositoryFile(referencePath);
    const scheduling = reference.match(/```json title="\.github\/workflows\/aw\.json"\n([\s\S]*?)\n```/);
    expect(scheduling).not.toBeNull();
    const factoryPolicy = buildAWPolicy({ repository: "github/gh-aw", ref: "a".repeat(40), workflows: ["eslint-refiner"], settings: JSON.parse(scheduling[1]).work_queue });
    expect(validatePolicy(factoryPolicy)).toBe(factoryPolicy);
    expect(factoryPolicy.limits).toEqual({ ...DEFAULT_LIMITS, pending_nodes: 30 });
    expect(factoryPolicy.pools.default).toMatchObject({ logical_limit: 3, native_limit: 3, retry: { max_attempts: 3, backoff_ms: 30000 } });
  });

  it("separates published docs and specifications from bounded agent instructions", () => {
    for (const file of [deploymentPath, referencePath, specificationPath]) {
      const frontmatter = readRepositoryFile(file).match(/^---\n([\s\S]*?)\n---/);
      expect(frontmatter).not.toBeNull();
      expect(frontmatter[1]).toMatch(/^title: [^\n]+$/m);
      expect(frontmatter[1]).toMatch(/^description: [^\n]+$/m);
    }
    expect(fs.existsSync(path.join(repositoryRoot, "specs/work-queue/priority-and-fairness.md"))).toBe(false);
    expect(fs.existsSync(path.join(repositoryRoot, "specs/work-queue/transactions.tsp"))).toBe(true);

    const instructions = readRepositoryFile(instructionsPath);
    expect(instructions.trim().split(/\s+/).length).toBeLessThanOrEqual(800);
    expect(instructions).not.toContain("```json");
    expect(instructions).toContain("issue-backed WorkQueueOps");
    expect(instructions).toContain("Absent branches are empty");
    expect(instructions).toContain("reject malformed/unsupported logs");
    expect(readRepositoryFile(referencePath)).toContain("Git-backed");
    for (const file of [deploymentPath, referencePath, specificationPath]) {
      const linked = [...instructions.matchAll(/\[[^\]\n]*\]\(([^)#]+)(?:#[^)]*)?\)/g)].map(([, link]) => path.resolve(repositoryRoot, path.dirname(instructionsPath), link));
      expect(linked).toContain(path.join(repositoryRoot, file));
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
    expect(dispatcher).toContain("shared/work-queue-staged-plan.md");
    expect(readRepositoryFile(".github/workflows/shared/work-queue-staged-plan.md")).toContain("Trusted processing verifies the exact batch");
    expect(dispatcher).toContain("existing policyless ledger");
    expect(dispatcher).toContain('{"pool":"default","max_claims":3,"max_dispatches":3}');
    expect(dispatcher).toContain('`status: "staged"`');
    expect(dispatcher).toContain("an empty snapshot or a prediction of no eligible Work can be stale");
    expect(dispatcher).toContain("Do not call ordinary `dispatch_workflow`");

    const factory = readRepositoryFile("docs/src/content/docs/gallery/linter-factory.md");
    expect(factory).toContain("gh aw work-queue --repo OWNER/REPO stats --json");
    expect(factory).toContain("--no-baseline --json");
    expect(factory).toContain("The first authorized submission atomically creates Policy and Work");
    expect(factory).toContain("dispatch alone cannot bootstrap an absent queue");
    expect(factory).toContain("A denied Git read is an error, not an empty queue");
    expect(factory).toContain("Check the staged plan and authoritative ledger");
    expect(factory).toContain("Result is recorded only after independent verification");
  });

  it("resolves queue documentation links after relocation", () => {
    const files = [
      deploymentPath,
      referencePath,
      specificationPath,
      instructionsPath,
      "specs/work-queue/README.md",
      "docs/adr/64955-git-backed-work-queue-coordination.md",
      "docs/src/content/docs/gallery/daily-report-portfolio.md",
      "docs/src/content/docs/gallery/linter-factory.md",
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
