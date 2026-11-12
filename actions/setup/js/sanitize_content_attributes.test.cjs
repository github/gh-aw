import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { convertXmlTags, sanitizeContentCore, clearRedactedDomains } = require("./sanitize_content_core.cjs");

beforeEach(() => {
  vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
});

afterEach(() => {
  clearRedactedDomains();
  vi.unstubAllGlobals();
});

describe("quote-aware HTML attribute sanitization", () => {
  it.each([
    ['<img src="safe"title="example">', '<img src="safe">'],
    ["<img src='safe'data-hidden='example'>", "<img src='safe'>"],
    ['<img src="safe"onerror=\'x\'style="display:none"data-note=hidden />', '<img src="safe" />'],
    ['<span class="one > two"title="hidden">ok</span>', '<span class="one > two">ok</span>'],
    ['<span class="safe > title=\'example\' data-note=value onerror=x" id="keep">ok</span>', '<span class="safe > title=\'example\' data-note=value onerror=x" id="keep">ok</span>'],
    ['<b title="unclosed>', '(b title="unclosed)'],
    ['<b title="unclosed', '<b title="unclosed'],
    ['<![CDATA[<b title="left > right">]]>', '(![CDATA[(b title="left > right")]])'],
  ])("converts %j consistently", (input, expected) => {
    expect(convertXmlTags(input)).toBe(expected);
    expect(sanitizeContentCore(input)).toBe(expected);
  });
});
