import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import { randomUUID } from "crypto";

const core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), setFailed: vi.fn() };
global.core = core;
const { main } = require("./interpolate_prompt.cjs");
const { renderMarkdownTemplate } = require("./render_template.cjs");
const { applyToNonCodeRegions } = require("./sanitize_content_core.cjs");

describe("optional code-region transformation errors", () => {
  it.each(["omitted", "undefined"])("retains conservative fallback with %s codeFn", mode => {
    let calls = 0;
    const transform = vi.fn(text => {
      if (calls++ === 0) throw new Error("transform failed");
      return text.toUpperCase();
    });
    const result = mode === "omitted" ? applyToNonCodeRegions("prose `code`", transform) : applyToNonCodeRegions("prose `code`", transform, undefined);
    expect(result).toBe("PROSE `CODE`");
    expect(transform).toHaveBeenLastCalledWith("prose `code`", "", "");
  });

  it("propagates non-code transformation errors when a codeFn is supplied", () => {
    expect(() =>
      applyToNonCodeRegions(
        "prose `code`",
        () => {
          throw new Error("transform failed");
        },
        code => code
      )
    ).toThrow("transform failed");
  });

  it("propagates code transformation errors", () => {
    expect(() =>
      applyToNonCodeRegions(
        "`code`",
        text => text,
        () => {
          throw new Error("code transform failed");
        }
      )
    ).toThrow("code transform failed");
  });
});

describe("parsed prompt condition operands", () => {
  let directory, promptPath, originalEnv;
  beforeEach(() => {
    originalEnv = { ...process.env };
    directory = path.join(process.cwd(), `.prompt-condition-tests-${randomUUID()}`);
    fs.mkdirSync(directory);
    promptPath = path.join(directory, "prompt.txt");
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("GH_AW_EXPR_") || key.startsWith("GH_AW_EXPERIMENTS_")) delete process.env[key];
    }
    process.env.GH_AW_PROMPT = promptPath;
    process.env.GITHUB_WORKSPACE = directory;
    core.setFailed.mockClear();
  });
  afterEach(() => {
    fs.rmSync(directory, { recursive: true, force: true });
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) delete process.env[key];
    }
    Object.assign(process.env, originalEnv);
  });

  async function render(template) {
    fs.writeFileSync(promptPath, template);
    await main();
    expect(core.setFailed).not.toHaveBeenCalled();
    return fs.readFileSync(promptPath, "utf8");
  }

  it.each(["#elseif", "#else-if", "#else_if", "elseif", "else-if", "else_if"])("preserves quoted experiment-looking literals in %s conditions", async marker => {
    process.env.GH_AW_EXPR_TITLE = "experiments.style";
    process.env.GH_AW_EXPERIMENTS_STYLE = "concise";
    for (const quote of ['"', "'"]) {
      expect(await render(`{{#if false}}\nRejected\n{{${marker} \${GH_AW_EXPR_TITLE} == ${quote}experiments.style${quote}}}\nSelected\n{{#else}}\nFallback\n{{/if}}`)).toBe("Selected\n");
    }
  });

  it("substitutes unquoted experiment operands but not quoted operands in the same condition", async () => {
    process.env.GH_AW_EXPERIMENTS_STYLE = "experiments.style";
    expect(await render('{{#if experiments.style == "experiments.style"}}\nSelected\n{{#else}}\nFallback\n{{/if}}')).toBe("Selected\n");
  });

  it.each(["#if", "#elseif", "#else-if", "#else_if", "elseif", "else-if", "else_if"])("restores backtick literals only in parsed %s conditions", async marker => {
    process.env.GH_AW_EXPR_TITLE = "`bug`";
    const prefix = marker === "#if" ? "" : "{{#if false}}\nRejected\n";
    const example = "`{{#if false}}literal{{#else}}example{{/if}}`";
    expect(await render(`${prefix}{{${marker} \${GH_AW_EXPR_TITLE} == "\`bug\`"}}\nSelected ${example}\n{{#else}}\nFallback\n{{/if}}`)).toBe(`Selected ${example}\n`);
  });

  it("does not parse directive text restored from a quoted code operand", () => {
    expect(renderMarkdownTemplate('{{#if `{{#else}}` == "`{{#else}}`"}}\nSelected\n{{#else}}\nFallback\n{{/if}}')).toBe("Selected\n");
  });

  it("keeps untrusted directive text opaque while comparing backtick literals", async () => {
    process.env.GH_AW_EXPR_TITLE = "`bug`";
    process.env.GH_AW_EXPR_BODY = "{{#else}}\nInjected\n{{/if}}\n```\n";
    expect(await render('{{#if ${GH_AW_EXPR_TITLE} == "`bug`"}}\nSelected: ${GH_AW_EXPR_BODY}\n{{#else}}\nFallback\n{{/if}}\nAuthor rule')).toBe(`Selected: ${process.env.GH_AW_EXPR_BODY}\nAuthor rule`);
  });

  it.each(["concise", "step_by_step", "detailed"])("renders only the %s branch in the real Dependabot workflow", async variant => {
    const workflow = fs.readFileSync(path.resolve(__dirname, "../../..", ".github/workflows/dependabot-go-checker.md"), "utf8");
    const body = workflow
      .split(/^---\s*$/m)
      .slice(2)
      .join("---");
    process.env.GH_AW_EXPERIMENTS_PROMPT_STYLE = variant;
    const output = await render(body);
    expect(output).not.toMatch(/\{\{#(?:if|elseif|else|endif)\b/);
    expect(output.includes("## Example Issue Formats")).toBe(variant === "detailed");
    expect(output.includes("````markdown")).toBe(variant === "detailed");
    expect(output).toContain("direct dependencies");
  });
});
