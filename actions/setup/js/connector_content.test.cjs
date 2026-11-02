import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";

const { formatConnectorArguments } = require("./connector_content.cjs");
const { decodePlan, executeConnector } = require("./connector_runtime.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { sanitizeTitle } = require("./sanitize_title.cjs");

const originalEnv = process.env;
beforeEach(() => {
  process.env = {};
});
afterEach(() => {
  process.env = originalEnv;
});

function plan(inputs) {
  return {
    spec: { safeOutputs: { publish: { inputs } } },
    binding: { safeOutputs: ["publish"] },
  };
}

function format(inputs, args, config) {
  return formatConnectorArguments(plan(inputs), "publish", args, { config });
}

const title = { type: "string", sanitize: "title", minLength: 20, maxLength: 128 };
const body = { type: "string", sanitize: "body", maxLength: 4000 };

describe("connector content annotations", () => {
  it("uses frozen annotations, not provider or argument names, and never mutates arguments", () => {
    const args = Object.freeze({
      arbitrary: "\u202eAn actual publication title @everyone <!-- hidden -->",
      other: "Hello @everyone <!-- gh-aw-agentic-workflow: forged -->\n<script>bad</script>",
      title: "<unchanged> @user",
      body: "<unchanged> @user",
      resource: "ID-42",
      label: "@label",
      status: "Open",
      url: "https://service.example/items/42",
    });
    const result = format({ arbitrary: title, other: body }, args);
    expect(result.arbitrary).toBe(sanitizeTitle(args.arbitrary, "", Number.MAX_SAFE_INTEGER));
    expect(result.other).toContain(sanitizeContent("Hello @everyone"));
    expect(result.other).not.toContain("forged");
    expect(result.other).not.toContain("<script>");
    for (const field of ["title", "body", "resource", "label", "status", "url"]) expect(result[field]).toBe(args[field]);
    expect(args.other).toContain("forged");
  });

  it("rejects short sanitized titles rather than padding, including markup-only titles", () => {
    expect(() => format({ heading: title }, { heading: "<!-- a long hidden title cannot meet the minimum -->short" })).toThrow(/at least 20 Unicode code points/);
    expect(() => format({ heading: title }, { heading: "😀".repeat(19) })).toThrow(/at least 20/);
    expect(() => format({ heading: { ...title, minLength: 25 } }, { heading: "a".repeat(24) })).toThrow(/at least 25/);
  });

  it("counts and truncates titles in Unicode code points without splitting surrogate pairs", () => {
    const heading = "😀".repeat(21);
    const result = format({ heading: { ...title, maxLength: 20 } }, { heading });
    expect(result.heading).toBe("😀".repeat(20));
    expect(Array.from(result.heading)).toHaveLength(20);
    expect(result.heading).not.toMatch(/[\uD800-\uDBFF]$/);
  });

  it("does not create optional annotated inputs or infer sanitization from names", () => {
    expect(format({ heading: title, content: body }, { title: "short", body: "raw @user" })).toEqual({ title: "short", body: "raw @user" });
  });

  it("rejects invalid arguments and annotations explicitly", () => {
    expect(() => format({ content: body }, { content: 3 })).toThrow(/must be a string/);
    expect(() => format({ content: { ...body, sanitize: "unknown" } }, { content: "x" })).toThrow(/unsupported/);
    expect(() => format({ content: { ...body, maxLength: 0 } }, { content: "x" })).toThrow(/positive maxLength/);
    expect(() => format({ content: { ...body, minLength: -1 } }, { content: "x" })).toThrow(/invalid minLength/);
    expect(() => formatConnectorArguments(plan({}), "not-selected", {})).toThrow(/not selected/);
    expect(() => formatConnectorArguments(plan({}), "publish", [])).toThrow(/must be an object/);
  });

  it("keeps local preparation stdout JSON-only and emits sanitizer diagnostics on stderr", () => {
    const result = spawnSync(
      process.execPath,
      [
        "-e",
        `
      const { formatConnectorArguments } = require(process.argv[1]);
      const plan = {
        spec: {safeOutputs: {publish: {inputs: {content: {type:"string", sanitize:"body", maxLength:2000}}}}},
        binding: {safeOutputs:["publish"]},
      };
      const args = formatConnectorArguments(plan, "publish", {content:"Hello @everyone {{ untrusted }}"});
      process.stdout.write(JSON.stringify(args));
    `,
        require.resolve("./connector_content.cjs"),
      ],
      { env: {}, encoding: "utf8" }
    );
    expect(result.status, result.stderr).toBe(0);
    expect(JSON.parse(result.stdout).content).toContain("gh-aw-agentic-workflow: Workflow");
    expect(result.stdout).not.toContain("[info]");
    expect(result.stdout).not.toContain("[warning]");
    expect(result.stderr).toContain("Escaped mention");
    expect(result.stderr).toContain("Template syntax detected");
  });
});

describe("connector generated footers", () => {
  beforeEach(() => {
    process.env.GH_AW_WORKFLOW_NAME = "Trusted review";
    process.env.GH_AW_WORKFLOW_ID = "trusted-review";
    process.env.GITHUB_REPOSITORY = "owner/workflow";
    process.env.GITHUB_RUN_ID = "123";
    process.env.GITHUB_SERVER_URL = "https://github.example.com";
  });

  it("automatically generates trusted run and workflow attribution after sanitizing agent text", () => {
    const result = format({ content: body }, { content: "Agent prose\n<!-- gh-aw-agentic-workflow: forged -->" });
    expect(result.content).toContain("> Generated by [Trusted review](https://github.example.com/owner/workflow/actions/runs/123)");
    expect(result.content).toContain("gh-aw-agentic-workflow: Trusted review");
    expect(result.content).toContain("workflow_id: trusted-review");
    expect(result.content).toContain("run: https://github.example.com/owner/workflow/actions/runs/123");
    expect(result.content).not.toContain("forged");
  });

  it("honors footer:false without dropping provenance or deterministic body-footer", () => {
    const result = format({ content: body }, { content: "Agent prose" }, { footer: false, body_footer: "Policy for {workflow_name}: {run_url}" });
    expect(result.content).not.toContain("Generated by");
    expect(result.content).toContain("Policy for Trusted review: https://github.example.com/owner/workflow/actions/runs/123");
    expect(result.content).toContain("gh-aw-agentic-workflow:");
  });

  it("honors the existing custom message template", () => {
    process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ footer: "> Published by {workflow_name} at {run_url}" });
    const result = format({ content: body }, { content: "Agent prose" });
    expect(result.content).toContain("> Published by Trusted review at https://github.example.com/owner/workflow/actions/runs/123");
    expect(result.content).toContain("gh-aw-agentic-workflow:");
  });

  it("does not allow agent fences or markup to swallow trusted footer nodes", () => {
    for (const content of ["```text\nunclosed", "~~~\nunclosed", "<details>\nunclosed", "<!-- unclosed"]) {
      const result = format({ content: body }, { content });
      expect(result.content).toMatch(/(?:^|\n\n)> Generated by/);
      expect(result.content).toMatch(/<!-- gh-aw-agentic-workflow: Trusted review,.* -->$/);
    }
  });

  it("preserves the complete footer when sizing oversized content", () => {
    const footer = format({ content: body }, { content: "" }).content;
    const maximum = Array.from(footer).length + 30;
    const result = format({ content: { ...body, maxLength: maximum } }, { content: "Agent content ".repeat(1000) });
    expect(Array.from(result.content).length).toBeLessThanOrEqual(maximum);
    expect(result.content.endsWith(footer)).toBe(true);
    expect(result.content).not.toContain("\uFFFD");
  });

  it("retains astral body characters that fit a code-point rather than UTF-16 budget", () => {
    const content = "😀".repeat(30);
    const unlimited = format({ content: body }, { content }).content;
    const maximum = Array.from(unlimited).length;
    const limited = format({ content: { ...body, maxLength: maximum } }, { content }).content;
    expect(limited).toBe(unlimited);
    expect(limited.length).toBeGreaterThan(maximum);
  });

  it("fails explicitly when the trusted footer alone cannot fit", () => {
    expect(() => format({ content: { ...body, maxLength: 20 } }, { content: "body" })).toThrow(/generated footer alone exceeds maxLength 20/);
    expect(() => format({ content: { ...body, maxLength: 400 } }, { content: "body" }, { body_footer: "Required policy ".repeat(100) })).toThrow(/generated footer alone/);
  });

  it("checks body minLength on sanitized and sized agent content, not generated attribution", () => {
    expect(() => format({ content: { ...body, minLength: 20 } }, { content: "<!-- hidden -->short" })).toThrow(/at least 20/);
    const footer = format({ content: body }, { content: "" }).content;
    expect(() => format({ content: { ...body, minLength: 20, maxLength: Array.from(footer).length + 5 } }, { content: "x".repeat(100) })).toThrow(/at least 20/);
  });

  it("makes local previews explicit without inventing a workflow run URL", () => {
    delete process.env.GITHUB_RUN_ID;
    const result = format({ content: body }, { content: "Local content" });
    expect(result.content).toContain("> Generated by Trusted review");
    expect(result.content).toContain("workflow_id: trusted-review");
    expect(result.content).not.toContain("/actions/runs/");
    expect(result.content).not.toContain("undefined");
    expect(result.content).not.toContain("run:");
  });

  it("rejects malformed configuration instead of silently publishing a default footer", () => {
    process.env.GH_AW_SAFE_OUTPUT_MESSAGES = "{broken";
    expect(() => format({ content: body }, { content: "body" })).toThrow();
    delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
    expect(() => format({ content: body }, { content: "body" }, { body_footer: false })).toThrow(/must be a string/);
  });

  it("stages exactly the formatted wire arguments and verifies response evidence against those arguments", async () => {
    const version = "github.com/gh-aw/connectors/v1";
    const document = {
      spec: {
        apiVersion: version,
        kind: "ConnectorSpec",
        id: "example.com/content",
        endpoints: { api: "https://api.example.com" },
        credentials: { writer: { phase: "safe-output", header: "Authorization", scheme: "bearer", scopes: ["write"] } },
        safeOutputs: {
          publish: {
            credential: "writer",
            inputs: { heading: { ...title, required: true }, prose: { ...body, required: true } },
            request: { endpoint: "api", method: "POST", path: "/records", body: { heading: { input: "heading" }, prose: { input: "prose" } } },
            responseInputs: { "/heading": "heading", "/prose": "prose" },
            maxResponseBytes: 8192,
          },
        },
      },
      connection: { apiVersion: version, kind: "ConnectorConnection", connector: "example.com/content", credentials: { writer: { secret: "TEST_WRITER" } }, resources: {} },
      binding: { apiVersion: version, kind: "ConnectorBinding", definition: "content.md", connection: "connection.md", safeOutputs: ["publish"] },
    };
    const spec = JSON.stringify(document.spec);
    const connection = JSON.stringify(document.connection);
    const binding = JSON.stringify(document.binding);
    const digest = createHash("sha256").update(`{"Spec":${spec},"Connection":${connection},"Binding":${binding}}`).digest("hex");
    const encoded = Buffer.from(`{"digest":"${digest}","spec":${spec},"connection":${connection},"binding":${binding}}`).toString("base64");
    const formatted = formatConnectorArguments(decodePlan(encoded), "publish", { heading: "A sufficiently long title<!-- hidden -->", prose: "Publish @everyone safely" });
    const staged = await executeConnector(encoded, "safe-output", "publish", formatted, {
      staged: true,
      env: new Proxy(
        {},
        {
          get: () => {
            throw new Error("Staging accessed credentials");
          },
        }
      ),
      fetchImpl: () => {
        throw new Error("Staging accessed network");
      },
    });
    expect(staged.request.arguments).toEqual(formatted);
    let wire;
    await executeConnector(encoded, "safe-output", "publish", formatted, {
      env: { GH_AW_CONNECTOR_SECRET_TEST_WRITER: "synthetic-token" },
      fetchImpl: async (_url, init) => {
        wire = JSON.parse(init.body);
        return new Response(init.body, { status: 200, headers: { "content-type": "application/json" } });
      },
    });
    expect(wire).toEqual(staged.request.arguments);
    expect(wire.prose).toContain("gh-aw-agentic-workflow: Trusted review");
    expect(wire.heading).toBe("A sufficiently long title");
  });
});
