import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { preparePiSubagents } = await import("./pi_subagent_config.cjs");
let dir;
let frontmatter;
const sdk = { parseFrontmatter: () => ({ frontmatter, body: "Read the requested file." }) };
const catalog = ["github-copilot/claude-haiku-4.5", "github-copilot/gpt-5.6-luna", "anthropic/claude-haiku-4-5"];

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-subagents-"));
  fs.mkdirSync(path.join(dir, "agents"));
  fs.writeFileSync(path.join(dir, "agents/reader.md"), "Fixture");
  frontmatter = { description: "Read files", model: "claude-haiku-4.5", tools: ["read"] };
  vi.stubEnv("GH_AW_PI_MODEL_ALIASES", JSON.stringify({ small: ["copilot/*haiku*", "anthropic/*haiku*"], recursive: ["small"] }));
});
afterEach(() => {
  vi.unstubAllEnvs();
  fs.rmSync(dir, { recursive: true, force: true });
});

const prepare = overrides => preparePiSubagents({ agentDir: dir, sdk, provider: "github-copilot", catalog, gateway: true, parentModel: "github-copilot/gpt-5.6-luna", ...overrides });

describe("Pi sub-agent model resolution", () => {
  it("derives name from the marker filename and qualifies bare models through the gateway", () => {
    expect(prepare()).toEqual([expect.objectContaining({ name: "reader", model: "aw-gateway/claude-haiku-4.5", tools: ["read"] })]);
    expect(JSON.parse(fs.readFileSync(path.join(dir, "subagents.json"), "utf8"))[0].declaredModel).toBe("claude-haiku-4.5");
  });

  it.each(["small", "recursive"])("resolves alias %s only on the parent's provider", model => {
    frontmatter.model = model;
    expect(prepare()[0].model).toBe("aw-gateway/claude-haiku-4.5");
  });

  it("inherits an unspecified model and supports native execution", () => {
    delete frontmatter.model;
    expect(prepare({ gateway: false })[0].model).toBe("github-copilot/gpt-5.6-luna");
  });

  it("translates model effort into Pi thinking settings", () => {
    frontmatter.model = "small?effort=none";
    expect(prepare()[0]).toMatchObject({ modelId: "claude-haiku-4.5", thinking: "off" });
  });

  it("refuses unresolved aliases instead of silently using the parent model", () => {
    frontmatter.model = "small";
    expect(() => prepare({ catalog: ["github-copilot/gpt-5.6-luna"] })).toThrow("did not resolve");
  });

  it("refuses other provider routes", () => {
    frontmatter.model = "anthropic/claude-haiku-4-5";
    expect(() => prepare()).toThrow("different provider");
  });

  it.each([{ model: 123 }, { tools: { read: true } }, { description: "" }])("rejects malformed frontmatter %j", invalid => {
    Object.assign(frontmatter, invalid);
    expect(() => prepare()).toThrow();
  });

  it("rejects symlinked agent files rather than reading outside the staging directory", () => {
    fs.unlinkSync(path.join(dir, "agents/reader.md"));
    fs.symlinkSync(path.join(dir, "outside.md"), path.join(dir, "agents/reader.md"));
    expect(prepare()).toEqual([]);
  });
});
