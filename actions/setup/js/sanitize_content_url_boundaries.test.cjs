import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { sanitizeContentCore, sanitizeUrlProtocols, sanitizeUrlDomains, clearRedactedDomains } = require("./sanitize_content_core.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");

beforeEach(() => {
  vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
  vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "github.com");
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", "allowed-only");
  vi.stubEnv("GITHUB_SERVER_URL", "");
  vi.stubEnv("GITHUB_API_URL", "");
});

afterEach(() => {
  clearRedactedDomains();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

const urls = [
  ...["data:image/png;base64,AAAA==", "javascript:alert(1)", "vbscript:msgbox(1)", "about:blank", "mailto:test@example.invalid", "tel:123456", "magnet:?xt=fixture", "DaTa%253Aimage/png;base64,AAAA==", "file:///image.png"].map(url => [
    url,
    "(redacted)",
  ]),
  ...["http://", "ftp://", "https://", "//", "\\\\", "www."].map(prefix => [`${prefix}outside.invalid/image.png`, prefix === "www." ? "(www[.]outside.invalid/redacted)" : "(outside.invalid/redacted)"]),
];

const sanitizers = [
  ["core", text => sanitizeContentCore(text)],
  ["default", text => sanitizeContent(text)],
  ["selective mentions", text => sanitizeContent(text, { allowedAliases: ["synthetic"] })],
  ["deferred mentions", text => sanitizeContent(text, { deferMentions: true })],
];

describe.each(["allowed-only", "allowed-or-code-region"])("URL tag boundaries under %s", policy => {
  describe.each(sanitizers)("%s sanitization", (_name, sanitize) => {
    it.each(urls)("keeps %s redaction inside the original image", (url, redacted) => {
      vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", policy);
      for (const quote of ['"', "'", ""]) {
        for (const tail of [' text "onerror=alert(1)>', '\n\nSteps: run "and check\nonerror=alert(1)>']) {
          const tag = `<img src=${quote}${redacted}${quote}>`;
          const input = `<img src=${quote}${url}${quote} onload="alert(2)" style="display:none">`;
          const output = sanitize(input + tail);
          expect(output).toBe(tag + tail);
          expect(output.match(/<img\b[^>]*>/g)).toEqual([tag]);
          expect(sanitize(output)).toBe(output);
        }
      }
    });

    it("preserves allowed image URLs and layout attributes", () => {
      vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", policy);
      const input = '<img src="https://github.com/image.png" width="12" height="8" alt="image">';
      expect(sanitize(input)).toBe(input);
    });

    it("retains the configured code-region URL policy", () => {
      vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", policy);
      const input = "```text\ndata:image/png;base64,AAAA==\n```";
      expect(sanitize(input)).toBe(policy === "allowed-only" ? "```text\n(redacted)\n```" : input);
    });
  });
});

describe("URL matcher structural delimiters", () => {
  it("preserves the tag boundary in the reported minimal reproduction", () => {
    expect(sanitizeContentCore('<img src="data:image/png;base64,AAAA=="> text "onerror=alert(1)>')).toBe('<img src="(redacted)"> text "onerror=alert(1)>');
  });

  it.each(["javascript", "vbscript", "about"])("redacts %s when a quote or angle bracket immediately follows the scheme", scheme => {
    for (const delimiter of ['"', "'", "<", ">"]) {
      expect(sanitizeUrlProtocols(`${scheme}:${delimiter}payload${delimiter}`)).toBe(`(redacted)${delimiter}payload${delimiter}`);
    }
  });

  it("does not redact blocked-scheme prose when a space follows the colon", () => {
    expect(sanitizeUrlProtocols("about: blank")).toBe("about: blank");
  });

  it.each(urls)("does not consume delimiters after %s", (url, redacted) => {
    const filter = /^(?:https:|[/\\]|www\.)/.test(url) ? text => sanitizeUrlDomains(text, ["github.com"]) : sanitizeUrlProtocols;
    for (const delimiter of ['"', "'", ">", "<"]) {
      expect(filter(`${url}${delimiter}tail`)).toBe(`${redacted}${delimiter}tail`);
    }
  });

  it.each(urls)("does not consume an adjacent tag after %s", (url, redacted) => {
    expect(sanitizeContent(`<img src=${url}><b>next</b>`)).toBe(`<img src=${redacted}><b>next</b>`);
  });

  it("does not treat a backslash as escaping an HTML attribute quote", () => {
    expect(sanitizeUrlDomains('www.outside.invalid/path\\"tail', [])).toBe('(www[.]outside.invalid/redacted)"tail');
  });

  it("keeps a bare www attribute candidate inside its HTML tag", () => {
    const input = '<img src="safe" width=10 www.outside.invalid>text@outside.invalid/path';
    const expected = '<img src="safe" width=10 (www[.]outside.invalid/redacted)>text@outside.invalid/path';
    expect(sanitizeUrlDomains(input, ["github.com"])).toBe(expected);
    expect(sanitizeContent(input)).toBe(expected);
  });

  it.each([
    ['"www.github.com"@outside.invalid/path', '"(outside.invalid/redacted)'],
    ['command=www.github.com"@outside.invalid/path', "command=(outside.invalid/redacted)"],
  ])("validates the full www authority in prose: %s", (input, expected) => {
    expect(sanitizeUrlDomains(input, ["github.com"])).toBe(expected);
  });

  it.each([
    ['<img alt="www.github.com"@evil.example/x">', '<img alt="(evil.example/redacted)">'],
    ['<b www.github.com"@evil.invalid/x', "<b (evil.invalid/redacted)"],
    ['<b www.github.com"@-evil.invalid/x', "<b (evil.invalid/redacted)"],
    ["a <b www.github.com'@_evil.invalid/x", "a <b (evil.invalid/redacted)"],
  ])("validates the full www authority inside tag-like input: %s", (input, expected) => {
    expect(sanitizeUrlDomains(input, ["github.com"])).toBe(expected);
  });

  it("recovers from an unterminated HTML quote before filtering later prose", () => {
    const input = "<b don't\n www.outside.invalid/path>tail";
    expect(sanitizeUrlDomains(input, ["github.com"])).toBe("<b don't\n (www[.]outside.invalid/redacted)>tail");
  });

  it("bounds scanner state for an excessively long unterminated tag", () => {
    const prefix = `<b "${"x".repeat(4092)}`;
    expect(sanitizeUrlDomains(`${prefix} www.outside.invalid/path>tail`, ["github.com"])).toBe(`${prefix} (www[.]outside.invalid/redacted)>tail`);
  });

  it.each(["", " ", "\n"])("does not consume an image boundary when userinfo-like prose follows an attribute with %j spacing", spacing => {
    for (const [url, expected] of [
      ["www.github.com", "www.github.com"],
      ["www.outside.invalid", "(www[.]outside.invalid/redacted)"],
    ]) {
      const tail = '>text@outside.invalid/path "onerror=alert(1)>';
      expect(sanitizeContent(`<img src =${spacing}${url}${tail}`)).toBe(`<img src =${spacing}${expected}${tail}`);
    }
  });
});
