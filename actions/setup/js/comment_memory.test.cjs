// @ts-check
import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
import { extractCommentMemoryEntries } from "./comment_memory_helpers.cjs";
import { parseReportMarkdown } from "./report_markdown.cjs";
import { expectExecutionProvenance, useExecutionProvenanceFixture } from "./test_execution_provenance.js";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

useExecutionProvenanceFixture();

describe("comment_memory", () => {
  let originalPromptsDir;

  beforeAll(() => {
    originalPromptsDir = process.env.GH_AW_PROMPTS_DIR;
    process.env.GH_AW_PROMPTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), "../md");
  });

  afterAll(() => {
    if (originalPromptsDir === undefined) {
      delete process.env.GH_AW_PROMPTS_DIR;
      return;
    }
    process.env.GH_AW_PROMPTS_DIR = originalPromptsDir;
  });

  it("sanitizes valid memory IDs", async () => {
    const module = await import("./comment_memory.cjs");
    expect(module.sanitizeMemoryID("Session_1")).toBe("Session_1");
    expect(module.sanitizeMemoryID("notes-2026")).toBe("notes-2026");
  });

  it("rejects invalid memory IDs", async () => {
    const module = await import("./comment_memory.cjs");
    expect(module.sanitizeMemoryID("bad id")).toBeNull();
    expect(module.sanitizeMemoryID("../oops")).toBeNull();
  });

  it("builds managed comment body with code-fence memory region", async () => {
    const module = await import("./comment_memory.cjs");
    const body = module.buildManagedMemoryBody("Hello world", "default", {
      includeFooter: false,
      runUrl: "https://example.com/run/1",
      workflowName: "Workflow",
      workflowSource: "",
      workflowSourceURL: "",
      historyUrl: undefined,
      triggeringIssueNumber: undefined,
      triggeringPRNumber: undefined,
    });
    expect(body).toContain("### Comment Memory");
    expect(body).toContain("<details>\n<summary>Peek at saved memory (default)</summary>\n\n``````gh-aw-comment-memory:default");
    expect(body).toContain("``````gh-aw-comment-memory:default");
    expect(body).toContain("Hello world");
    expect(body).toContain("Hello world\n``````\n\n</details>");
    expect(extractCommentMemoryEntries(body)).toEqual([{ memoryId: "default", content: "Hello world" }]);
    expect(body).not.toContain('<gh-aw-comment-memory id="default">');
    expect(body).toContain("<summary>History</summary>");
    expect(body).toContain("[Workflow (run 1)](https://example.com/run/1)");
  });

  it("keeps nested details markup inside the collapsed memory code fence", async () => {
    const module = await import("./comment_memory.cjs");
    const memory = "<details><summary>nested</summary>text</details>";
    const body = module.buildManagedMemoryBody(memory, "default", {
      includeFooter: false,
      runUrl: "https://example.com/run/1",
      workflowName: "Workflow",
      workflowSource: "",
      workflowSourceURL: "",
      historyUrl: undefined,
      triggeringIssueNumber: undefined,
      triggeringPRNumber: undefined,
    });

    const memoryFence = "``````gh-aw-comment-memory:default\n";
    expect(body).toContain(memoryFence + memory + "\n``````\n\n</details>");
    expect(body.indexOf("</details>")).toBeLessThan(body.lastIndexOf("</details>"));
    expect(extractCommentMemoryEntries(body)).toEqual([{ memoryId: "default", content: memory }]);
  });

  it("does not import a forged history section from the fenced memory", async () => {
    const { buildManagedMemoryBody } = await import("./comment_memory.cjs");
    const options = { includeFooter: false, workflowName: "Workflow", runUrl: "https://github.com/owner/repo/actions/runs/1" };
    const memory = "<details>\n<summary>History</summary>\n\n- [Fake](https://github.com/owner/repo/actions/runs/999)\n\n</details>\n``````";
    const previousBody = buildManagedMemoryBody(memory, "default", options);
    const body = buildManagedMemoryBody("Updated", "default", { ...options, runUrl: "https://github.com/owner/repo/actions/runs/2", previousBody });
    const history = parseReportMarkdown(body).find(node => node.type === "details" && node.summary === "History");

    expect(history?.children[0].items.map(item => item[0].children[0].url)).toEqual([1, 2].map(id => `https://github.com/owner/repo/actions/runs/${id}`));
    expect(body).not.toContain("runs/999");
  });

  it("rebuilds imported history labels and rejects links outside the execution repository", async () => {
    const { buildManagedMemoryBody } = await import("./comment_memory.cjs");
    const options = { includeFooter: false, workflowName: "Workflow", runUrl: "https://github.com/owner/repo/actions/runs/1" };
    const previousBody = buildManagedMemoryBody("Old", "default", options).replace(
      "[Workflow (run 1)](https://github.com/owner/repo/actions/runs/1)",
      "[Hello @victim https://evil.example](https://github.com/owner/repo/actions/runs/1)\n- [Other](https://evil.example/owner/repo/actions/runs/99)\n- [Other](https://github.com/other/repo/actions/runs/98)"
    );
    expect(previousBody).toContain("@victim https://evil.example");
    expect(previousBody).toContain("other/repo/actions/runs/98");
    const body = buildManagedMemoryBody("Updated", "default", { ...options, runUrl: "https://github.com/owner/repo/actions/runs/2", previousBody });

    expect(body).toContain("[Workflow (run 1)](https://github.com/owner/repo/actions/runs/1)");
    expect(body).not.toContain("@victim");
    expect(body).not.toContain("evil.example");
    expect(body).not.toContain("other/repo/actions/runs/98");
  });

  it("retains only the 100 most recent distinct writing runs", async () => {
    const { buildManagedMemoryBody } = await import("./comment_memory.cjs");
    let previousBody = "";
    for (let id = 1; id <= 101; id++) {
      previousBody = buildManagedMemoryBody("Memory", "default", {
        includeFooter: false,
        workflowName: "Workflow",
        runUrl: `https://github.com/owner/repo/actions/runs/${id}`,
        previousBody,
      });
    }
    const body = buildManagedMemoryBody("Updated", "default", {
      includeFooter: false,
      workflowName: "Workflow",
      runUrl: "https://github.com/owner/repo/actions/runs/2",
      previousBody,
    });
    const history = parseReportMarkdown(body).find(node => node.type === "details" && node.summary === "History");
    const urls = history?.children[0].items.map(item => item[0].children[0].url);

    expect(urls).toHaveLength(100);
    expect(urls[0]).toBe("https://github.com/owner/repo/actions/runs/3");
    expect(urls[99]).toBe("https://github.com/owner/repo/actions/runs/2");
    expect(body).not.toContain("/actions/runs/1)");
    expect(extractCommentMemoryEntries(body)).toEqual([{ memoryId: "default", content: "Updated" }]);
  });

  it.each(["", " \n ", "<!-- clear memory -->"])("retains an extractable tagged memory block when %j sanitizes to empty", async memory => {
    const module = await import("./comment_memory.cjs");
    const body = module.buildManagedMemoryBody(memory, "default", {
      includeFooter: false,
      runUrl: "https://example.com/run/1",
      workflowName: "Workflow",
      workflowSource: "",
      workflowSourceURL: "",
    });

    expect(body).toContain("<summary>Peek at saved memory (default)</summary>");
    expect(body).toContain("``````gh-aw-comment-memory:default\n\n``````");
    expect(extractCommentMemoryEntries(body)).toEqual([{ memoryId: "default", content: "" }]);
  });

  it("builds managed comment body with footer when enabled", async () => {
    const module = await import("./comment_memory.cjs");
    const body = module.buildManagedMemoryBody("Persist me", "session", {
      includeFooter: true,
      runUrl: "https://example.com/run/2",
      workflowName: "Workflow",
      workflowSource: "",
      workflowSourceURL: "",
      historyUrl: undefined,
      triggeringIssueNumber: 123,
      triggeringPRNumber: undefined,
    });

    expect(body).toContain("### Comment Memory");
    expect(body).toContain("``````gh-aw-comment-memory:session");
    expect(body).toContain("Persist me");
    expect(body).toContain("> [!NOTE]");
    expect(body).toContain("> Expand the saved memory block to view or edit the persistent context for this thread.");
    expect(body).toContain("Generated by");
    const codeFenceIdx = body.indexOf("``````gh-aw-comment-memory:session");
    const detailsEndIdx = body.indexOf("</details>");
    const noteIdx = body.indexOf("> [!NOTE]");
    const footerIdx = body.indexOf("Generated by");
    expect(extractCommentMemoryEntries(body)).toEqual([{ memoryId: "session", content: "Persist me" }]);
    expect(codeFenceIdx).toBeLessThan(noteIdx);
    expect(detailsEndIdx).toBeLessThan(noteIdx);
    expect(noteIdx).toBeLessThan(footerIdx);
  });

  it("appends a configured body footer independently of the generated footer", async () => {
    const module = await import("./comment_memory.cjs");
    const body = module.buildManagedMemoryBody("Persist me", "session", {
      includeFooter: false,
      bodyFooter: "Custom footer for {workflow_name}: {run_url}",
      runUrl: "https://example.com/run/2",
      workflowName: "Workflow",
      workflowSource: "",
      workflowSourceURL: "",
      historyUrl: undefined,
      triggeringIssueNumber: undefined,
      triggeringPRNumber: undefined,
    });
    expect(body).toContain("Custom footer for Workflow: https://example.com/run/2");
    expect(body).not.toContain("Generated by");
  });

  it("throws E001 for invalid memory ID in managed body builder", async () => {
    const module = await import("./comment_memory.cjs");
    expect(() =>
      module.buildManagedMemoryBody("Persist me", "bad id", {
        includeFooter: false,
        runUrl: "https://example.com/run/3",
        workflowName: "Workflow",
        workflowSource: "",
        workflowSourceURL: "",
        historyUrl: undefined,
        triggeringIssueNumber: undefined,
        triggeringPRNumber: undefined,
      })
    ).toThrow("E001:");
  });

  it("finds only managed comments with provenance marker", async () => {
    const module = await import("./comment_memory.cjs");
    const github = {
      rest: {
        issues: {
          listComments: async () => ({
            data: [
              { id: 1, body: "``````gh-aw-comment-memory:default\nold\n``````" },
              {
                id: 2,
                body: "``````gh-aw-comment-memory:default\nmanaged\n``````\n\n<!-- gh-aw-agentic-workflow: Workflow, run: https://example.com -->",
              },
            ],
          }),
        },
      },
    };

    const found = await module.findManagedComment(github, "octo", "repo", 123, "default");
    expect(found?.id).toBe(2);
  });

  it("finds legacy xml-format managed comments with provenance marker", async () => {
    const module = await import("./comment_memory.cjs");
    const github = {
      rest: {
        issues: {
          listComments: async () => ({
            data: [
              { id: 1, body: '<gh-aw-comment-memory id="default">\nold\n</gh-aw-comment-memory>' },
              {
                id: 2,
                body: '<gh-aw-comment-memory id="default">\nmanaged\n</gh-aw-comment-memory>\n\n<!-- gh-aw-agentic-workflow: Workflow, run: https://example.com -->',
              },
            ],
          }),
        },
      },
    };

    const found = await module.findManagedComment(github, "octo", "repo", 123, "default");
    expect(found?.id).toBe(2);
  });

  describe("handler integration", () => {
    let mockCore;
    let mockGithub;
    let mockContext;
    let originalGlobals;

    beforeEach(() => {
      originalGlobals = {
        core: global.core,
        github: global.github,
        context: global.context,
      };

      mockCore = {
        debug: () => {},
        info: () => {},
        warning: () => {},
        error: () => {},
        setOutput: () => {},
        setFailed: () => {},
      };

      mockGithub = {
        rest: {
          issues: {
            listComments: async () => ({ data: [] }),
            createComment: async () => ({
              data: { id: 99, html_url: "https://github.com/owner/repo/issues/1#issuecomment-99" },
            }),
            updateComment: async () => ({
              data: { id: 99, html_url: "https://github.com/owner/repo/issues/1#issuecomment-99" },
            }),
          },
        },
      };

      mockContext = {
        eventName: "pull_request",
        runId: 1,
        repo: { owner: "owner", repo: "repo" },
        payload: { pull_request: { number: 42 } },
        serverUrl: "https://github.com",
      };

      global.core = mockCore;
      global.github = mockGithub;
      global.context = mockContext;
    });

    afterEach(() => {
      global.core = originalGlobals.core;
      global.github = originalGlobals.github;
      global.context = originalGlobals.context;
    });

    it("should skip (not fail) when target is 'triggering' but running in workflow_dispatch context", async () => {
      const commentMemoryScript = fs.readFileSync(path.join(__dirname, "comment_memory.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      // workflow_dispatch has no triggering issue or PR
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {};

      const handler = await eval(`(async () => { ${commentMemoryScript}; return await main({ target: 'triggering' }); })()`);

      const result = await handler({ type: "comment_memory", body: "Agent memory" }, {});

      // Should be skipped, not failed — workflow_dispatch has no triggering issue/PR
      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("triggering");

      // Should have warned about the failed target resolution
      const targetWarning = warningCalls.find(msg => msg.includes("target resolution failed"));
      expect(targetWarning).toBeTruthy();
    });

    it("publishes execution provenance when creating a managed comment with footer disabled", async () => {
      mockGithub.rest.issues.createComment = vi.fn().mockResolvedValue({
        data: { id: 99, html_url: "https://github.com/owner/repo/issues/42#issuecomment-99" },
      });
      const handler = await (await import("./comment_memory.cjs")).main({ target: "triggering", footer: false });
      const result = await handler({ type: "comment_memory", body: "Agent memory" });

      expect(result.success).toBe(true);
      const body = mockGithub.rest.issues.createComment.mock.calls[0][0].body;
      expectExecutionProvenance(body);
      expect(body).not.toContain("> Generated by");
    });

    it("does not read or write comments in staged mode", async () => {
      mockGithub.rest.issues.listComments = vi.fn();
      mockGithub.rest.issues.createComment = vi.fn();
      mockGithub.rest.issues.updateComment = vi.fn();
      const handler = await (await import("./comment_memory.cjs")).main({ target: "triggering", staged: true });

      expect(await handler({ type: "comment_memory", body: "Preview" })).toEqual({ success: true, staged: true });
      expect(mockGithub.rest.issues.listComments).not.toHaveBeenCalled();
      expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
      expect(mockGithub.rest.issues.updateComment).not.toHaveBeenCalled();
    });

    it.each([false, true])("rejects oversized memory without overwriting an existing comment (footer=%s)", async footer => {
      const oldBody = "``````gh-aw-comment-memory:default\nOld memory\n``````\n\n<!-- gh-aw-agentic-workflow: Workflow -->";
      mockGithub.rest.issues.listComments = vi.fn().mockResolvedValue({ data: [{ id: 98, body: oldBody }] });
      mockGithub.rest.issues.updateComment = vi.fn();
      mockGithub.rest.issues.createComment = vi.fn();
      const handler = await (await import("./comment_memory.cjs")).main({ target: "triggering", footer });

      const result = await handler({ type: "comment_memory", body: "x".repeat(65536) });

      expect(result.success).toBe(false);
      expect(result.error).toContain("exceeds maximum length");
      expect(mockGithub.rest.issues.updateComment).not.toHaveBeenCalled();
      expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
      expect(extractCommentMemoryEntries(oldBody)).toEqual([{ memoryId: "default", content: "Old memory" }]);
    });

    it.each([false, true])("retains every writing run across updates, clears, and repeated writes (footer=%s)", async footer => {
      const savedComment = { id: 98, body: "", html_url: "https://github.com/owner/repo/issues/42#issuecomment-98" };
      mockGithub.rest.issues.listComments = vi.fn(async () => ({ data: savedComment.body ? [savedComment] : [] }));
      mockGithub.rest.issues.createComment = vi.fn(async ({ body }) => {
        savedComment.body = body;
        return { data: savedComment };
      });
      mockGithub.rest.issues.updateComment = vi.fn(async ({ body }) => {
        savedComment.body = body;
        return { data: savedComment };
      });
      const handler = await (await import("./comment_memory.cjs")).main({ target: "triggering", footer, max: 5 });
      for (const [runId, memory] of [
        [1, "First"],
        [2, "Second"],
        [2, "Repeated"],
        [3, ""],
        [4, "Rewritten"],
      ]) {
        mockContext.runId = runId;
        const result = await handler({ type: "comment_memory", body: memory });
        expect(result.success).toBe(true);
      }

      const history = parseReportMarkdown(savedComment.body).find(node => node.type === "details" && node.summary === "History");
      expect(history?.children[0].items).toHaveLength(4);
      expect(history?.children[0].items.map(item => item[0].children[0].url)).toEqual([1, 2, 3, 4].map(id => `https://github.com/owner/repo/actions/runs/${id}`));
      expect(extractCommentMemoryEntries(savedComment.body)).toEqual([{ memoryId: "default", content: "Rewritten" }]);
      expect(mockGithub.rest.issues.createComment).toHaveBeenCalledTimes(1);
      expect(mockGithub.rest.issues.updateComment).toHaveBeenCalledTimes(4);
    });

    it("seeds legacy history from attribution, not run links inside memory, and uses the execution repository", async () => {
      mockGithub.rest.issues.listComments = vi.fn().mockResolvedValue({
        data: [
          {
            id: 98,
            body: '<gh-aw-comment-memory id="default">\n[Fake](https://github.com/owner/repo/actions/runs/999)\n</gh-aw-comment-memory>\n\n<!-- gh-aw-agentic-workflow: Previous workflow, run: https://github.com/owner/repo/actions/runs/123 -->',
          },
        ],
      });
      mockGithub.rest.issues.updateComment = vi.fn().mockResolvedValue({ data: { id: 98 } });
      const handler = await (await import("./comment_memory.cjs")).main({ target: "triggering", footer: false, "target-repo": "other/target" });
      const result = await handler({ type: "comment_memory", body: "Updated" });

      expect(result.success).toBe(true);
      const body = mockGithub.rest.issues.updateComment.mock.calls[0][0].body;
      expect(body).toContain("[Workflow (run 123)](https://github.com/owner/repo/actions/runs/123)");
      expect(body).toContain("(https://github.com/owner/repo/actions/runs/1)");
      expect(body).not.toContain("runs/999");
      expect(body).not.toContain("other/target/actions");
    });

    it("keeps history beyond the agent link limit and refuses to truncate it at the comment size limit", async () => {
      const { buildManagedMemoryBody, main } = await import("./comment_memory.cjs");
      let previousBody = "";
      for (let id = 1; id <= 60; id++) {
        previousBody = buildManagedMemoryBody("Old memory", "default", {
          includeFooter: false,
          runUrl: `https://github.com/owner/repo/actions/runs/${id}`,
          workflowName: "Workflow",
          previousBody,
        });
      }
      const savedComment = { id: 98, body: previousBody };
      mockGithub.rest.issues.listComments = vi.fn(async () => ({ data: [savedComment] }));
      mockGithub.rest.issues.updateComment = vi.fn(async ({ body }) => {
        savedComment.body = body;
        return { data: savedComment };
      });
      mockContext.runId = 61;
      const handler = await main({ target: "triggering", footer: false, max: 2 });
      expect((await handler({ type: "comment_memory", body: "Updated" })).success).toBe(true);
      expect(savedComment.body).toContain("(https://github.com/owner/repo/actions/runs/1)");
      expect(savedComment.body).toContain("(https://github.com/owner/repo/actions/runs/61)");
      const bodyBeforeOverflow = savedComment.body;
      const overflow = await handler({ type: "comment_memory", body: "x".repeat(62000) });
      expect(overflow.success).toBe(false);
      expect(overflow.error).toContain("exceeds maximum length");
      expect(savedComment.body).toBe(bodyBeforeOverflow);
      expect(mockGithub.rest.issues.updateComment).toHaveBeenCalledTimes(1);
    });

    it.each([false, true])("keeps the managed comment identity across clearing and rewriting memory (footer=%s)", async footer => {
      const savedComment = {
        id: 98,
        body: "``````gh-aw-comment-memory:default\nOld memory\n``````\n\n<!-- gh-aw-agentic-workflow: Workflow -->",
        html_url: "https://github.com/owner/repo/issues/42#issuecomment-98",
      };
      mockGithub.rest.issues.listComments = vi.fn(async () => ({ data: [savedComment] }));
      mockGithub.rest.issues.updateComment = vi.fn(async ({ body }) => {
        savedComment.body = body;
        return { data: savedComment };
      });
      mockGithub.rest.issues.createComment = vi.fn();
      const handler = await (await import("./comment_memory.cjs")).main({ target: "triggering", footer, max: 2 });

      const cleared = await handler({ type: "comment_memory", body: "<!-- clear memory -->" });
      expect(cleared.success).toBe(true);
      expect(cleared.commentId).toBe(98);
      expect(extractCommentMemoryEntries(savedComment.body)).toEqual([{ memoryId: "default", content: "" }]);
      expectExecutionProvenance(savedComment.body);

      const rewritten = await handler({ type: "comment_memory", body: "New memory" });
      expect(rewritten.success).toBe(true);
      expect(rewritten.commentId).toBe(98);
      expect(extractCommentMemoryEntries(savedComment.body)).toEqual([{ memoryId: "default", content: "New memory" }]);
      expect(mockGithub.rest.issues.updateComment).toHaveBeenCalledTimes(2);
      expect(mockGithub.rest.issues.updateComment.mock.calls.map(([parameters]) => parameters.comment_id)).toEqual([98, 98]);
      expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
    });

    it("publishes execution provenance when updating a managed comment", async () => {
      mockGithub.rest.issues.updateComment = vi.fn().mockResolvedValue({
        data: { id: 98, html_url: "https://github.com/owner/repo/issues/42#issuecomment-98" },
      });
      mockGithub.rest.issues.listComments = vi.fn().mockResolvedValue({
        data: [
          {
            id: 98,
            body: "``````gh-aw-comment-memory:default\nOld memory\n``````\n\n<!-- gh-aw-agentic-workflow: Workflow, run: https://example.com -->",
          },
        ],
      });
      const handler = await (await import("./comment_memory.cjs")).main({ target: "triggering", footer: false });
      const result = await handler({ type: "comment_memory", body: "Updated memory" });

      expect(result.success).toBe(true);
      const body = mockGithub.rest.issues.updateComment.mock.calls[0][0].body;
      expectExecutionProvenance(body);
      expect(body).not.toContain("> Generated by");
    });
  });
});
