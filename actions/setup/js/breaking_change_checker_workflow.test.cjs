import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";
import { readDocumentMetadata } from "../../../scripts/markdown_metadata_test_adapter.cjs";

const root = resolve(import.meta.dirname, "../../..");
const source = readFileSync(resolve(root, ".github/workflows/breaking-change-checker.md"), "utf8");
const [, frontmatter, prompt] = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
const workflow = readDocumentMetadata(frontmatter, { version: "1.2", uniqueKeys: true }).value;
const lock = readDocumentMetadata(readFileSync(resolve(root, ".github/workflows/breaking-change-checker.lock.yml"), "utf8"), {
  version: "1.2",
  uniqueKeys: true,
}).value;
const preparation = workflow.steps.find(step => step.name === "Prepare breaking review context");
const sha = "4d73748ff2d044cc0fd9cb05ed13b51bb475cc72";

async function prepare({ createdAt = "2026-11-10T14:53:28Z", headSha = sha, contextSha = sha, branch = "main", failure } = {}) {
  const writes = [];
  const calls = [];
  await runInNewContext(
    `(async () => { ${preparation.with.script}\n})()`,
    {
      require: name => {
        expect(name).toBe("node:fs");
        return {
          mkdirSync: (path, options) => {
            expect(path).toBe("/tmp/gh-aw/agent");
            expect(options).toEqual({ recursive: true });
          },
          writeFileSync: (path, content) => writes.push({ path, value: JSON.parse(content) }),
        };
      },
      context: { repo: { owner: "github", repo: "gh-aw" }, runId: 42838691761, sha: contextSha, payload: { repository: { default_branch: branch } } },
      github: {
        rest: {
          actions: {
            getWorkflowRun: async args => {
              calls.push(args);
              if (failure) throw failure;
              return { data: { created_at: createdAt, head_sha: headSha } };
            },
          },
        },
      },
    },
    { timeout: 1000 }
  );
  return { writes, calls };
}

describe("Breaking Change Checker coverage contract", () => {
  it("pins the interval to run creation and the run SHA, not invocation time or a moving branch", async () => {
    const { writes, calls } = await prepare();
    expect(calls).toEqual([{ owner: "github", repo: "gh-aw", run_id: 42838691761 }]);
    expect(writes).toEqual([
      {
        path: "/tmp/gh-aw/agent/breaking-review-context.json",
        value: {
          repository: "github/gh-aw",
          sha,
          base_branch: "main",
          start: "2026-11-09T14:53:28.000Z",
          end: "2026-11-10T14:53:28.000Z",
          interval: "start-inclusive,end-exclusive",
        },
      },
    ]);
  });

  it.each([{ createdAt: "not-a-date" }, { headSha: "0".repeat(40) }, { contextSha: "main" }, { branch: "" }])("rejects unpinned or invalid review context: %j", async options => {
    await expect(prepare(options)).rejects.toThrow("Cannot establish a pinned breaking-change review context");
  });

  it("does not turn metadata API failures into a success-shaped context", async () => {
    await expect(prepare({ failure: new Error("API unavailable") })).rejects.toThrow("API unavailable");
  });

  it("keeps repository reads narrow and does not depend on Git history or agent writes", () => {
    expect(workflow.permissions["pull-requests"]).toBe("read");
    expect(workflow.tools.github).toEqual({
      mode: "local",
      toolsets: ["repos", "pull_requests"],
      "allowed-repos": ["github/gh-aw"],
      "min-integrity": "approved",
      allowed: ["list_commits", "get_commit", "get_file_contents", "search_pull_requests", "pull_request_read"],
    });
    expect(workflow.tools.bash).toEqual(["cat:*", "jq:*"]);
    expect(workflow.tools.edit).toBe(false);
    const steps = lock.jobs.agent.steps;
    expect(steps.findIndex(step => step.name === preparation.name)).toBeLessThan(steps.findIndex(step => step.name === "Execute GitHub Copilot CLI"));
    expect(steps.find(step => step.name === preparation.name).uses).toMatch(/^actions\/github-script@[0-9a-f]{40}$/);
    expect(steps.find(step => step.name === preparation.name).with.script.trim()).toBe(preparation.with.script.trim());
    const config = JSON.parse(steps.find(step => step.env?.GH_AW_COPILOT_SDK_TOOL_CONFIG).env.GH_AW_COPILOT_SDK_TOOL_CONFIG);
    expect(config.capabilities.edit).toBe(false);
    expect(config.permissions.allowedTools).not.toContain("write");
    expect(config.permissions.allowedTools.some(tool => /^shell\(git(?:[ :)]|$)/.test(tool))).toBe(false);
    expect(config.permissions.allowedTools).not.toContain("shell");
    expect(steps.some(step => step.env?.GITHUB_READ_ONLY === "1") || JSON.stringify(steps).includes('\\"GITHUB_READ_ONLY\\": \\"1\\"')).toBe(true);
  });

  it("requires complete manifests, bounded recovery and honest partial-report semantics", () => {
    for (const requirement of [
      "sha` from context",
      "Follow every page until exhausted",
      "committer",
      "search_pull_requests",
      "merged_at",
      "split the time interval",
      "20,480-byte",
      "native `view` bounded line ranges",
      "immutable parent/head SHAs",
      "stats-only response is not a",
      "call `report_incomplete` as well",
      "no finding is confirmed, call `report_incomplete`",
      "Only after the ledger establishes complete",
    ]) {
      expect(prompt).toContain(requirement);
    }
    expect(workflow["safe-outputs"].messages["run-success"]).not.toContain("has reviewed all changes");
    expect(workflow.evals.find(item => item.id === "analysis-completed").question).toContain("Answer NO when coverage is partial");
  });
});
