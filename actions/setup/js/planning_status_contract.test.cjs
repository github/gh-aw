import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { getOctokit } from "@actions/github";
import Ajv from "ajv";

const { validateField, validateItem, resetValidationConfigCache } = require("./safe_output_type_validator.cjs");
const { normalizeSafeOutputToolArguments } = require("./safe_outputs_mcp_arguments.cjs");
const { loadHandlers } = require("./safe_output_handler_manager.cjs");
const { main } = require("./set_issue_status.cjs");
const tools = require("./safe_outputs_tools.json");

beforeEach(() => {
  vi.stubGlobal("core", Object.fromEntries(["info", "debug", "warning", "error"].map(key => [key, vi.fn()])));
  vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, payload: { issue: { number: 42 } }, eventName: "issues" });
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "false");
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  resetValidationConfigCache();
});

describe("native planning status collector and MCP contracts", () => {
  it.each(["Todo", 2, " Backlog "])("collects valid native selectors %j", status => {
    const result = validateField(status, "status", { required: true, stringOrInteger: true }, "set_issue_status", 1);
    expect(result).toEqual({ isValid: true, normalizedValue: typeof status === "string" ? status.trim() : status });
  });

  it.each([undefined, null, "", " ", 0, -1, 2.5, true, {}, Number.MAX_SAFE_INTEGER + 1])("rejects malformed required selectors %j", status => {
    expect(validateField(status, "status", { required: true, stringOrInteger: true }, "set_issue_status", 1).isValid).toBe(false);
  });

  it("allows omission but not explicit null for optional native selector", () => {
    expect(validateField(undefined, "planning_status", { stringOrInteger: true }, "create_issue", 1)).toEqual({ isValid: true });
    expect(validateField(null, "planning_status", { stringOrInteger: true }, "create_issue", 1).isValid).toBe(false);
  });

  it("does not turn an explicit blank initial planning status into omission", () => {
    const schema = tools.find(tool => tool.name === "create_issue").inputSchema;
    expect(normalizeSafeOutputToolArguments("create_issue", { planning_status: "" }, undefined, schema)).toEqual({ planning_status: "" });
  });

  it("keeps native status distinct from update_issue legacy status validation", () => {
    vi.stubEnv(
      "GH_AW_VALIDATION_CONFIG",
      JSON.stringify({
        set_issue_status: { fields: { status: { required: true, stringOrInteger: true }, issue_number: { issueNumberOrTemporaryId: true }, duplicate_of: { optionalPositiveInteger: true } } },
        create_issue: { fields: { planning_status: { stringOrInteger: true } } },
        update_issue: { fields: { status: { type: "string", enum: ["open", "closed"] } } },
      })
    );
    resetValidationConfigCache();
    expect(validateItem({ type: "set_issue_status", status: 2, issue_number: "aw_new" }, "set_issue_status", 1)).toMatchObject({ isValid: true, normalizedItem: { status: 2, issue_number: "#aw_new" } });
    expect(validateItem({ type: "set_issue_status" }, "set_issue_status", 1).isValid).toBe(false);
    expect(validateItem({ type: "create_issue", planning_status: null }, "create_issue", 1).isValid).toBe(false);
    expect(validateItem({ type: "update_issue", status: "OPEN" }, "update_issue", 1)).toMatchObject({ isValid: true, normalizedItem: { status: "open" } });
    expect(validateItem({ type: "update_issue", status: "Todo" }, "update_issue", 1).isValid).toBe(false);
  });

  it.each(["Todo", 2])("registers native tool and initial creation schemas for %j", status => {
    const ajv = new Ajv({ strict: false });
    const update = tools.find(tool => tool.name === "set_issue_status");
    const create = tools.find(tool => tool.name === "create_issue");
    expect(ajv.compile(update.inputSchema)({ status, duplicate_of: 9 })).toBe(true);
    expect(ajv.compile(create.inputSchema)({ title: "Test", body: "A complete issue description.", planning_status: status })).toBe(true);
    expect(normalizeSafeOutputToolArguments("set_issue_status", { issueNumber: 42, status, duplicateOf: 9 }, undefined, update.inputSchema)).toEqual({ issue_number: 42, status, duplicate_of: 9 });
    expect(normalizeSafeOutputToolArguments("create_issue", { title: "Test", body: "Test", planningStatus: status }, undefined, create.inputSchema)).toEqual({ title: "Test", body: "Test", planning_status: status });
  });

  it.each(["", " ", null, 0, -1, 2.5, true, {}, Number.MAX_SAFE_INTEGER + 1])("rejects malformed MCP native selectors %j", status => {
    const ajv = new Ajv({ strict: false });
    expect(ajv.compile(tools.find(tool => tool.name === "set_issue_status").inputSchema)({ status })).toBe(false);
    expect(ajv.compile(tools.find(tool => tool.name === "create_issue").inputSchema)({ title: "Test", body: "A complete issue description.", planning_status: status })).toBe(false);
  });
});

describe("native Octokit and safe-output routing", () => {
  it("applies canonical native status with real transport serialization", async () => {
    const backlog = { id: 1, name: "Backlog", category: "backlog" };
    const duplicate = { id: 7, name: "Duplicate", category: "duplicate" };
    let closed = false;
    const fetch = vi.fn(async (url, options) => {
      expect(options.headers["x-github-api-version"] || options.headers["X-GitHub-Api-Version"]).toBe("2026-03-10");
      const pathname = new URL(String(url)).pathname;
      if (pathname !== "/graphql" && pathname !== "/repos/owner/repo/planning-statuses") expect(options.redirect).toBe("error");
      let data;
      if (pathname === "/graphql") {
        expect(JSON.parse(options.body).query).toContain("duplicateOf");
        return new Response(JSON.stringify({ data: { repository: { issue: { number: 42, duplicateOf: closed ? { fullDatabaseId: "900", number: 9, repository: { nameWithOwner: "owner/repo" } } : null } } } }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      if (pathname === "/repos/owner/repo/planning-statuses") {
        expect(options.method).toBe("GET");
        data = [backlog, duplicate];
      } else if (pathname === "/repos/owner/repo") {
        data = { id: 1000, full_name: "owner/repo" };
      } else if (pathname === "/repos/owner/repo/issues/9") {
        data = { id: 900, number: 9, state: "open", repository_url: "https://api.github.com/repos/owner/repo", html_url: "https://github.com/owner/repo/issues/9" };
      } else {
        expect(pathname).toBe("/repos/owner/repo/issues/42");
        data = { id: 420, number: 42, state: "open", state_reason: null, status: backlog, html_url: "https://github.com/owner/repo/issues/42", repository_url: "https://api.github.com/repos/owner/repo" };
        if (options.method === "PATCH") {
          closed = true;
          expect(JSON.parse(options.body)).toEqual({ status: "Duplicate", duplicate_issue_id: 900 });
          data = { ...data, status: duplicate, state: "closed", state_reason: "duplicate" };
        }
      }
      return new Response(JSON.stringify(data), { status: 200, headers: { "content-type": "application/json" } });
    });
    const source = getOctokit("fixture-token", { request: { fetch }, headers: { "X-GitHub-Api-Version": "2026-03-10" } });
    const run = async client => {
      vi.stubGlobal("github", client);
      const handler = await main({ allow_close: true });
      expect(await handler({ status: 7, duplicate_of: 9 }, {})).toMatchObject({ success: true, after_state: { planning_status: duplicate } });
    };
    await run(source);
    expect(fetch.mock.calls.filter(([, options]) => options.method === "PATCH")).toHaveLength(1);
  });

  it("loads the registered native handler through the manager", async () => {
    const todo = { id: 2, name: "Todo", category: "planned" };
    vi.stubGlobal("github", {
      rest: { issues: { get: vi.fn().mockResolvedValue({ data: { number: 42, state: "open", status: null, repository_url: "https://api.github.com/repos/owner/repo", html_url: "https://github.com/owner/repo/issues/42" } }) } },
      request: vi.fn(async route => ({ data: route.startsWith("GET ") ? [todo] : { number: 42, state: "open", status: todo } })),
    });
    const handlers = await loadHandlers({ set_issue_status: { max: 5 } });
    expect(handlers.has("set_issue_status")).toBe(true);
    expect(await handlers.get("set_issue_status")({ type: "set_issue_status", status: "Todo" }, {})).toMatchObject({ success: true, issue_number: 42 });
  });
});
