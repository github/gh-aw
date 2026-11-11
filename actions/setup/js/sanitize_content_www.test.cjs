import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import { HtmlRenderer, Parser } from "commonmark";

const require = createRequire(import.meta.url);
const { sanitizeContent } = require("./sanitize_content.cjs");
const { sanitizeUrlDomains, clearRedactedDomains, getRedactedDomains } = require("./sanitize_content_core.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderReport } = require("./report_ast.cjs");

beforeEach(() => {
  vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
  vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "github.com");
  vi.stubEnv("GITHUB_SERVER_URL", "");
  vi.stubEnv("GITHUB_API_URL", "");
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", "allowed-only");
});

afterEach(() => {
  clearRedactedDomains();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("scheme-less www URL filtering", () => {
  it.each([{}, { allowedAliases: ["synthetic"] }, { deferMentions: true }])("redacts the full URL through each sanitization path: %j", options => {
    const input = "See www.outside.invalid/leak?sample=synthetic#fragment";
    const output = sanitizeContent(input, options);
    expect(output).toBe("See (www[.]outside.invalid/redacted)");
    expect(sanitizeContent(output, options)).toBe(output);
    expect(getRedactedDomains()).toContain("www.outside.invalid");
  });

  it.each(["", " ", "\n", "(", "*", "_", "~"])("filters GFM autolink boundary %j", prefix => {
    expect(sanitizeUrlDomains(`${prefix}www.outside.invalid/path`, ["github.com"])).toBe(`${prefix}(www[.]outside.invalid/redacted)`);
  });

  it.each(["www.outside.invalid?sample=synthetic", "www.outside.invalid#fragment", "WWW.OutSide.Invalid:8080/path", "www.github.com.outside.invalid/path", "www.github.com%2eoutside.invalid/path", "www&#46;outside.invalid/path"])(
    "redacts unallowed query, fragment, port, and case variants: %s",
    input => {
      expect(sanitizeContent(input)).not.toContain(input);
      expect(sanitizeContent(input)).toContain("/redacted)");
    }
  );

  it.each([
    ["www.github.com/owner/repo", "github.com"],
    ["www.Example.Invalid:8080/path?q=1#part", "EXAMPLE.INVALID"],
    ["www.sub.example.invalid/path", "*.example.invalid"],
  ])("preserves allowed hostnames and their original spelling: %s", (input, allowed) => {
    expect(sanitizeUrlDomains(input, [allowed])).toBe(input);
  });

  it.each(["www.github.com@outside.invalid/path", "www.github.com:8080@outside.invalid/path", "www.github.com%40outside.invalid/path"])("does not trust an allowlisted hostname in userinfo: %s", input => {
    expect(sanitizeUrlDomains(input, ["github.com"])).not.toBe(input);
    expect(sanitizeUrlDomains(input, ["github.com"])).not.toContain("@");
  });

  it("strips userinfo even when the actual destination is allowed", () => {
    expect(sanitizeUrlDomains("www.synthetic.invalid@www.github.com/path", ["github.com"])).toBe("www.github.com/path");
    expect(sanitizeUrlDomains("www.outside.invalid,@www.github.com/path", ["github.com"])).toBe("www.github.com/path");
  });

  it("uses the rendered authority when userinfo contains backslashes", () => {
    const input = "www.github.com\\@%6Futside.invalid/path";
    expect(sanitizeUrlDomains(input, ["github.com"])).toBe("(outside.invalid/redacted)");
  });

  it("does not log attacker-controlled scheme-less hostnames", () => {
    const hostname = "tokenized-secret.outside.invalid";
    sanitizeUrlDomains(`www.${hostname}/path`, ["github.com"]);
    expect(core.info).toHaveBeenCalledWith("Redacted scheme-less www URL");
    expect(core.info.mock.calls.flat().join(" ")).not.toContain(hostname);
  });

  it.each([".", ",", "!", "?", ":", ";", ").", "**", "_", "~~"])("preserves trailing prose punctuation %j", suffix => {
    expect(sanitizeUrlDomains(`www.outside.invalid/a(b)${suffix}`, [])).toBe(`(www[.]outside.invalid/redacted)${suffix}`);
  });

  it("filters adjacent links separately", () => {
    expect(sanitizeContent("www.outside.invalid/a www.second.invalid/b")).toBe("(www[.]outside.invalid/redacted) (www[.]second.invalid/redacted)");
  });

  it.each(["//", "\\\\"])("does not mistake %s before emphasis for an allowed URL", prefix => {
    expect(sanitizeContent(`${prefix}*www.outside.invalid/path*`)).toBe(`${prefix}*(www[.]outside.invalid/redacted)*`);
  });

  it.each([
    "wordwww.outside.invalid/path",
    "https://github.com/_www.outside.invalid/path",
    "www.github.com/www.outside.invalid/path",
    "https://github.com/a(b)_www.outside.invalid/path",
    "[x](https://github.com/a(b)_www.outside.invalid/path)",
    "[x](https://github.com/a\\)_www.outside.invalid/path)",
  ])("does not rewrite non-autolinks or allowed URL paths: %s", input => {
    expect(sanitizeContent(input)).toBe(input);
  });

  it.each([
    ["//github.com/_www.outside.invalid/path", "//github.com/_(www[.]outside.invalid/redacted)"],
    ["\\\\github.com/_www.outside.invalid/path", "//github.com/_(www[.]outside.invalid/redacted)"],
    ["a//x_www.outside.invalid/path", "a//x_(www[.]outside.invalid/redacted)"],
    ["//github.com/*www.outside.invalid/path*", "//github.com/*(www[.]outside.invalid/redacted)*"],
    ["//github.com/~www.outside.invalid/path~", "//github.com/~(www[.]outside.invalid/redacted)~"],
  ])("scans www. after a protocol-relative or backslash run: %s", (input, expected) => {
    expect(sanitizeContent(input)).toBe(expected);
  });

  it.each([
    [">www.outside.invalid/path", ">(www[.]outside.invalid/redacted)"],
    ["[^1]:www.outside.invalid/path", "[^1]:(www[.]outside.invalid/redacted)"],
    ["|www.outside.invalid|keep |", "|(www[.]outside.invalid/redacted)|keep |"],
    ["|www.github.com|keep |", "|www.github.com|keep |"],
  ])("recognizes block, footnote, and table-cell boundaries: %s", (input, expected) => {
    expect(sanitizeUrlDomains(input, ["github.com"])).toBe(expected);
  });

  it.each(["www.github.com>@%6Futside.invalid/path", "www.github.com`@%6Futside.invalid/path"])("validates the full GFM authority including %s", input => {
    expect(sanitizeUrlDomains(input, ["github.com"])).toBe("(outside.invalid/redacted)");
  });

  it("preserves adjacent Markdown links around a filtered www. destination", () => {
    const input = "[docs](www.example.invalid/path)[source](https://github.com/source)";
    expect(sanitizeContent(input)).toBe("[docs]((www[.]example.invalid/redacted))[source](https://github.com/source)");
    expect(sanitizeContent("[www.example.invalid/path](https://github.com/source)")).toBe("[(www[.]example.invalid/redacted)](https://github.com/source)");
  });

  it("filters a www. autolink after emphasis in an explicit URL", () => {
    expect(sanitizeContent("https://github.com/x*www.outside.invalid/path*")).toBe("https://github.com/x*(www[.]outside.invalid/redacted)*");
    expect(sanitizeContent("[https://github.com/x]*www.outside.invalid/path*")).toBe("[https://github.com/x]*(www[.]outside.invalid/redacted)*");
  });

  it("scans many adjacent allowed links in one forward pass", () => {
    const input = Array.from({ length: 500 }, (_, index) => `[link${index}](https://github.com/${index})`).join("");
    expect(sanitizeContent(input)).toBe(input);
  });

  it.each(["https://github.com/a(b)", "//github.com/a(b)"])("checks autolinks immediately following an allowed Markdown destination: %s", destination => {
    const input = `[x](${destination})*www.outside.invalid/path*`;
    expect(sanitizeContent(input)).toBe(`[x](${destination})*(www[.]outside.invalid/redacted)*`);
  });

  it.each(["allowed-only", "allowed-or-code-region"])("respects code-region policy %s", policy => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", policy);
    const url = "www.outside.invalid/path";
    const marker = "(www[.]outside.invalid/redacted)";
    const input = `${url}\n\n\`${url}\`\n\n\`\`\`suggestion\n${url}\n\`\`\``;
    const code = policy === "allowed-or-code-region" ? url : marker;
    expect(sanitizeContent(input)).toBe(`${marker}\n\n\`${code}\`\n\n\`\`\`suggestion\n${code}\n\`\`\``);
  });

  it.each(["www.outside.invalid/path", "https://www.outside.invalid/path", "//www.outside.invalid/path"])("keeps all www redaction markers inert across Markdown parsing: %s", input => {
    const output = sanitizeContent(input);
    expect(output).toBe("(www[.]outside.invalid/redacted)");
    expect(new HtmlRenderer().render(new Parser().parse(output))).toBe("<p>(www[.]outside.invalid/redacted)</p>\n");
    const publication = renderReport({ type: "report", title: "Synthetic www regression", agentTainted: true, children: parseReportMarkdown(output, { agentTainted: true }) }).body;
    expect(new HtmlRenderer().render(new Parser().parse(publication))).toBe("<p>(www[.]outside.invalid/redacted)</p>\n");
    expect(sanitizeContent(output)).toBe(output);
  });
});
