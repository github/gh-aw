import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";

const parsers = {
  agy: content => require("./parse_agy_log.cjs").parseAgyLog(content),
  claude: content => require("./parse_claude_log.cjs").parseClaudeLog(content),
  codex: content => require("./parse_codex_log.cjs").parseCodexLog(content),
  copilot: content => require("./parse_copilot_log.cjs").parseCopilotLog(content),
  custom: content => require("./parse_custom_log.cjs").parseCustomLog(content),
  deepseek: content => require("./parse_deepseek_log.cjs").parseDeepSeekLog(content),
  gemini: content => require("./parse_gemini_log.cjs").parseGeminiLog(content),
  goose: content => require("./parse_goose_log.cjs").parseGooseLog(content),
  kiro: content => require("./parse_kiro_log.cjs").parseKiroLog(content),
  opencode: content => require("./parse_opencode_log.cjs").parseOpenCodeLog(content),
  pi: content => require("./parse_pi_log.cjs").parsePiLog(content),
  pydantic: content => require("./parse_pydantic_log.cjs").parsePydanticLog(content),
};

function verifyLog(engine, content) {
  if (typeof engine !== "string" || !Object.hasOwn(parsers, engine)) {
    throw new Error(`Unsupported engine: ${String(engine)}. Select a parser explicitly; do not infer success from static inspection.`);
  }
  expect(content.trim().length, "The downloaded agent log must not be empty").toBeGreaterThan(0);
  const result = parsers[engine](content);
  expect(result.logEntries.length, "The parser must recognize events from the downloaded log").toBeGreaterThan(0);
  expect(result.markdown.trim().length, "The parser must render nonempty Markdown").toBeGreaterThan(0);
  expect(global.core.error).not.toHaveBeenCalled();
  expect(global.core.setFailed).not.toHaveBeenCalled();
  return result;
}

beforeEach(() => {
  vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn() });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("rendering verifier harness regressions", () => {
  it("executes the Pi v3 parser from the checkout and treats log text as data", () => {
    const text = "All done. require('node:fs').writeFileSync('/untrusted-log-code', 'never execute')";
    const content = [
      { type: "session", version: 3, id: "verifier-regression" },
      { type: "agent_start" },
      { type: "turn_start" },
      { type: "turn_end", message: { role: "assistant", model: "test-model", content: [{ type: "text", text }], usage: { input: 10, output: 5 } } },
      { type: "agent_settled" },
      { type: "agent_end", messages: [] },
    ]
      .map(entry => JSON.stringify(entry))
      .join("\n");
    const result = verifyLog("pi", content);
    expect(result.markdown).toContain(text);
    expect(result.logEntries.some(entry => entry.type === "session.result")).toBe(true);
  });

  it.each(["", "plain text without structured events"])("rejects empty or unrecognized logs: %j", content => {
    expect(() => verifyLog("pi", content)).toThrow();
  });

  it.each(["unknown", "../parse_pi_log.cjs", "constructor", "__proto__", null])("rejects unsupported engine selectors: %j", engine => {
    expect(() => verifyLog(engine, "{}")).toThrow("Unsupported engine");
  });
});

describe("render template verification", () => {
  it.each([
    ["truthy block", "{{#if true}}\nHello\n{{/if}}", "Hello\n"],
    ["falsy block", "{{#if false}}\nHidden\n{{/if}}", ""],
    ["inline truthy", "Start {{#if true}}middle{{/if}} end", "Start middle end"],
    ["inline falsy", "Start {{#if false}}gone{{/if}} end", "Start  end"],
    ["surrounding lines", "Before\n{{#if false}}\nRemoved\n{{/if}}\nAfter", "Before\nAfter"],
  ])("%s", (_name, input, expected) => {
    const { renderMarkdownTemplate } = require("./render_template.cjs");
    expect(renderMarkdownTemplate(input)).toBe(expected);
    expect(global.core.error).not.toHaveBeenCalled();
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });
});

describe.skipIf(process.env.GH_AW_VERIFY_RENDERING !== "true")("downloaded agent output verification", () => {
  it("parses and renders the selected real log without executing its contents", () => {
    // The dedicated config requires real input; missing files must fail, not skip.
    const input = JSON.parse(fs.readFileSync(process.env.GH_AW_RENDERING_INPUT, "utf8"));
    expect(typeof input.logPath).toBe("string");
    const result = verifyLog(input.engine, fs.readFileSync(input.logPath, "utf8"));
    console.log(`Verified ${input.engine}: ${result.logEntries.length} events, ${Buffer.byteLength(result.markdown, "utf8")} rendered bytes, ${global.core.warning.mock.calls.length} warnings`);
  });
});
