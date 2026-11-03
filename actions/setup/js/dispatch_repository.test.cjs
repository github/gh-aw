// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

const { main } = require("./dispatch_repository.cjs");
const globals = /** @type {any} */ global;

describe("dispatch_repository", () => {
  /** @type {any} */
  let mockCore;
  /** @type {any} */
  let mockGithub;
  /** @type {any} */
  let mockContext;

  /** @type {any} */
  let dispatchEventCalls;

  /** @type {{ core: any, github: any, context: any, getOctokit: any, hadGetOctokit: boolean }} */
  let savedGlobals;

  beforeEach(() => {
    savedGlobals = {
      core: globals.core,
      github: globals.github,
      context: globals.context,
      getOctokit: globals.getOctokit,
      hadGetOctokit: Object.hasOwn(globals, "getOctokit"),
    };

    dispatchEventCalls = [];

    mockCore = {
      infos: /** @type {string[]} */ [],
      warnings: /** @type {string[]} */ [],
      errors: /** @type {string[]} */ [],
      info: /** @param {string} msg */ msg => mockCore.infos.push(msg),
      warning: /** @param {string} msg */ msg => mockCore.warnings.push(msg),
      error: /** @param {string} msg */ msg => mockCore.errors.push(msg),
    };

    mockGithub = {
      rest: {
        repos: {
          createDispatchEvent: /** @param {any} params */ async params => {
            dispatchEventCalls.push(params);
            return { data: {} };
          },
        },
      },
    };

    mockContext = {
      runId: 9999,
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };

    globals.core = mockCore;
    globals.github = mockGithub;
    globals.context = mockContext;
  });

  afterEach(() => {
    globals.core = savedGlobals.core;
    globals.github = savedGlobals.github;
    globals.context = savedGlobals.context;
    if (savedGlobals.hadGetOctokit) globals.getOctokit = savedGlobals.getOctokit;
    else delete globals.getOctokit;
    vi.restoreAllMocks();
  });

  describe("main factory", () => {
    it("should return a handler function", async () => {
      const handler = await main({ tools: {} });
      expect(typeof handler).toBe("function");
    });

    it("should return a handler function with no config", async () => {
      const handler = await main();
      expect(typeof handler).toBe("function");
    });

    it("should log initialization info", async () => {
      await main({ tools: { deploy: { event_type: "deploy", max: 2 } } });
      expect(mockCore.infos.some(/** @param {string} m */ m => m.includes("dispatch_repository handler initialized"))).toBe(true);
    });
  });

  describe("handleDispatchRepository", () => {
    it("should return error when tool_name is missing", async () => {
      const handler = await main({ tools: {} });
      const result = await handler({ tool_name: "" }, {});
      expect(result.success).toBe(false);
      expect(result.error).toMatch(/tool_name is required/);
    });

    it("should return error when tool_name is whitespace-only", async () => {
      const handler = await main({ tools: {} });
      const result = await handler({ tool_name: "   " }, {});
      expect(result.success).toBe(false);
      expect(result.error).toMatch(/tool_name is required/);
    });

    it("should return error when tool is not configured", async () => {
      const handler = await main({ tools: {} });
      const result = await handler({ tool_name: "unknown_tool" }, {});
      expect(result.success).toBe(false);
      expect(result.error).toMatch(/not configured/);
    });

    it("should return error when max count is reached", async () => {
      const handler = await main({
        tools: {
          deploy: { event_type: "deploy", repository: "test-owner/test-repo", max: 1 },
        },
      });

      // First dispatch succeeds
      const first = await handler({ tool_name: "deploy" }, {});
      expect(first.success).toBe(true);

      // Second dispatch hits the limit
      const second = await handler({ tool_name: "deploy" }, {});
      expect(second.success).toBe(false);
      expect(second.error).toMatch(/Max count/);
    });

    it("should return error when no target repository is configured", async () => {
      const handler = await main({
        tools: { deploy: { event_type: "deploy" } },
      });
      const result = await handler({ tool_name: "deploy" }, {});
      expect(result.success).toBe(false);
      expect(result.error).toMatch(/No target repository/);
    });

    it("should return error when repository slug is invalid", async () => {
      const handler = await main({
        tools: { deploy: { event_type: "deploy", repository: "not-valid-slug" } },
      });
      const result = await handler({ tool_name: "deploy" }, {});
      expect(result.success).toBe(false);
      expect(result.error).toMatch(/Invalid repository slug/);
    });

    it("should return error when event_type is not configured", async () => {
      const handler = await main({
        tools: { deploy: { repository: "test-owner/test-repo" } },
      });
      const result = await handler({ tool_name: "deploy" }, {});
      expect(result.success).toBe(false);
      expect(result.error).toMatch(/event_type is required/);
    });

    it("should dispatch successfully with valid config", async () => {
      const handler = await main({
        tools: {
          deploy: { event_type: "deploy", repository: "test-owner/other-repo", max: 2 },
        },
      });

      const result = await handler({ tool_name: "deploy" }, {});
      expect(result.success).toBe(true);
      expect(result.tool_name).toBe("deploy");
      expect(result.repository).toBe("test-owner/other-repo");
      expect(result.event_type).toBe("deploy");
      expect(dispatchEventCalls.length).toBe(1);
      expect(dispatchEventCalls[0].event_type).toBe("deploy");
    });

    it("should authenticate with the per-tool github-token override instead of the step-level client", async () => {
      const toolDispatchCalls = [];
      const toolGithubClient = {
        rest: {
          repos: {
            createDispatchEvent: async params => {
              toolDispatchCalls.push(params);
              return { data: {} };
            },
          },
        },
      };
      const getOctokit = vi.fn().mockReturnValue(toolGithubClient);
      globals.getOctokit = getOctokit;

      const handler = await main({
        tools: {
          deploy: { event_type: "deploy", repository: "test-owner/other-repo", max: 2, "github-token": "tool-scoped-token" },
        },
      });

      const result = await handler({ tool_name: "deploy" }, {});

      expect(result.success).toBe(true);
      expect(getOctokit).toHaveBeenCalledWith("tool-scoped-token", expect.anything());
      expect(toolDispatchCalls.length).toBe(1);
      expect(dispatchEventCalls.length).toBe(0);
    });

    it("should return a structured error when per-tool client creation fails", async () => {
      globals.getOctokit = vi.fn(() => {
        throw new Error("client setup failed");
      });

      const handler = await main({
        tools: {
          deploy: { event_type: "deploy", repository: "test-owner/other-repo", "github-token": "tool-scoped-token" },
        },
      });

      const result = await handler({ tool_name: "deploy" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/E099: Failed to dispatch/);
      expect(result.error).toContain("client setup failed");
      expect(dispatchEventCalls).toHaveLength(0);
    });

    it("should fail closed when a per-tool github-token resolves to an empty value", async () => {
      const getOctokit = vi.fn();
      globals.getOctokit = getOctokit;

      const handler = await main({
        tools: {
          deploy: { event_type: "deploy", repository: "test-owner/other-repo", "github-token": "" },
        },
      });

      const result = await handler({ tool_name: "deploy" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/E099: Failed to dispatch/);
      expect(result.error).toContain("resolved to an empty value");
      expect(getOctokit).not.toHaveBeenCalled();
      expect(dispatchEventCalls).toHaveLength(0);
    });

    it("should include message inputs in client_payload", async () => {
      const handler = await main({
        tools: {
          deploy: { event_type: "deploy", repository: "test-owner/test-repo", max: 5 },
        },
      });

      await handler({ tool_name: "deploy", inputs: { env: "production", version: "1.2.3" } }, {});

      expect(dispatchEventCalls.length).toBe(1);
      expect(dispatchEventCalls[0].client_payload.env).toBe("production");
      expect(dispatchEventCalls[0].client_payload.version).toBe("1.2.3");
    });

    it("should inject aw_context into client_payload", async () => {
      process.env.GITHUB_RUN_ID = "9999";
      process.env.GITHUB_RUN_ATTEMPT = "2";
      process.env.GITHUB_WORKFLOW_REF = "test-owner/test-repo/.github/workflows/test.lock.yml@refs/heads/main";
      mockContext.eventName = "issues";

      const handler = await main({
        tools: {
          deploy: { event_type: "deploy", repository: "test-owner/test-repo", max: 5 },
        },
      });

      await handler({ tool_name: "deploy" }, {});

      expect(dispatchEventCalls.length).toBe(1);
      expect(dispatchEventCalls[0].client_payload).toHaveProperty("aw_context");
      expect(dispatchEventCalls[0].client_payload.aw_context).toMatchObject({
        episode_id: "9999-2:test-owner/test-repo/.github/workflows/test.lock.yml@refs/heads/main",
        hop_id: "9999-2:test-owner/test-repo/.github/workflows/test.lock.yml@refs/heads/main",
        parent_hop_id: "",
        origin_event: "issues",
        root_repo: "test-owner/test-repo",
        root_workflow_id: "test-owner/test-repo/.github/workflows/test.lock.yml@refs/heads/main",
        root_run_id: "9999",
      });

      delete process.env.GITHUB_RUN_ID;
      delete process.env.GITHUB_RUN_ATTEMPT;
      delete process.env.GITHUB_WORKFLOW_REF;
    });

    it("should use message.repository over toolConfig.repository when both set", async () => {
      const handler = await main({
        tools: {
          deploy: { event_type: "deploy", repository: "test-owner/default-repo", max: 5 },
        },
      });

      const result = await handler({ tool_name: "deploy", repository: "test-owner/override-repo" }, {});

      expect(result.success).toBe(true);
      expect(result.repository).toBe("test-owner/override-repo");
      expect(dispatchEventCalls[0].owner).toBe("test-owner");
      expect(dispatchEventCalls[0].repo).toBe("override-repo");
    });

    it("should default to first allowed_repository when no target given", async () => {
      const handler = await main({
        tools: {
          deploy: { event_type: "deploy", allowed_repositories: ["test-owner/allowed-repo"], max: 5 },
        },
      });

      const result = await handler({ tool_name: "deploy" }, {});
      expect(result.success).toBe(true);
      expect(result.repository).toBe("test-owner/allowed-repo");
    });

    it("should return error on API failure", async () => {
      mockGithub.rest.repos.createDispatchEvent = async () => {
        throw new Error("API rate limit exceeded");
      };

      const handler = await main({
        tools: { deploy: { event_type: "deploy", repository: "test-owner/test-repo", max: 5 } },
      });

      const result = await handler({ tool_name: "deploy" }, {});
      expect(result.success).toBe(false);
      expect(result.error).toMatch(/Failed to dispatch/);
      expect(result.error).toMatch(/API rate limit exceeded/);
    });

    it("should return staged result when isStaged is true", async () => {
      const handler = await main({
        tools: { deploy: { event_type: "deploy", repository: "test-owner/test-repo", max: 5 } },
        staged: true,
      });

      const result = await handler({ tool_name: "deploy" }, {});
      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(dispatchEventCalls.length).toBe(0);
    });

    it.each([false, true])("forwards unique webhook context without previewing bodies, staged=%s", async staged => {
      const first = { delivery_id: "a", payload: { url: "https://api.github.com/orgs/org/projectsV2/1/items/1", type: ["edited"] } };
      mockContext.payload = { client_payload: { aw_context: { webhook: { event: "projects_v2_item", deliveries: [first, first] } } } };
      const handler = await main({ staged, tools: { deploy: { event_type: "deploy", repository: "test-owner/test-repo" } } });
      const result = await handler({ tool_name: "deploy" }, {});
      expect(result.success).toBe(true);
      expect(result.client_payload.aw_context.items).toHaveLength(1);
      expect(result.client_payload.aw_context.webhook.deliveries).toEqual([first]);
      expect(mockCore.infos.join("\n")).not.toContain("do-not-log-webhook");
    });

    it('should not treat string "false" in tool staged config as staged mode', async () => {
      const handler = await main({
        tools: { deploy: { event_type: "deploy", repository: "test-owner/test-repo", max: 5, staged: "false" } },
      });

      const result = await handler({ tool_name: "deploy" }, {});
      expect(result.success).toBe(true);
      expect(result.staged).toBeUndefined();
      expect(dispatchEventCalls.length).toBe(1);
    });

    it("should parse numeric max from string config", async () => {
      const handler = await main({
        tools: { deploy: { event_type: "deploy", repository: "test-owner/test-repo", max: "3" } },
      });

      // Should allow up to 3 dispatches
      for (let i = 0; i < 3; i++) {
        const result = await handler({ tool_name: "deploy" }, {});
        expect(result.success).toBe(true);
      }

      const overflow = await handler({ tool_name: "deploy" }, {});
      expect(overflow.success).toBe(false);
      expect(overflow.error).toMatch(/Max count of 3/);
    });
  });
});
