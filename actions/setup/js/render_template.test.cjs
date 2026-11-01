import { describe, it, expect, vi } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
const __filename = fileURLToPath(import.meta.url),
  __dirname = path.dirname(__filename),
  core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), setFailed: vi.fn(), summary: { addHeading: vi.fn().mockReturnThis(), addRaw: vi.fn().mockReturnThis(), write: vi.fn() } };
global.core = core;
const { isTruthy } = require("./is_truthy.cjs"),
  { selectBranch } = require("./template_branch.cjs"),
  { renderMarkdownTemplate } = require("./render_template.cjs");
describe("renderMarkdownTemplate", () => {
  (it("should keep content in truthy blocks", () => {
    const output = renderMarkdownTemplate("{{#if true}}\nHello\n{{/if}}");
    expect(output).toBe("Hello\n");
  }),
    it("should remove content in falsy blocks", () => {
      const output = renderMarkdownTemplate("{{#if false}}\nHello\n{{/if}}");
      expect(output).toBe("");
    }),
    it("should process multiple blocks", () => {
      const output = renderMarkdownTemplate("{{#if true}}\nKeep this\n{{/if}}\n{{#if false}}\nRemove this\n{{/if}}");
      expect(output).toBe("Keep this\n");
    }),
    it("should handle nested content", () => {
      const output = renderMarkdownTemplate("# Title\n\n{{#if true}}\n## Section 1\nThis should be kept.\n{{/if}}\n\n{{#if false}}\n## Section 2\nThis should be removed.\n{{/if}}\n\n## Section 3\nThis is always visible.");
      expect(output).toBe("# Title\n\n## Section 1\nThis should be kept.\n\n## Section 3\nThis is always visible.");
    }),
    it("should leave content without conditionals unchanged", () => {
      const input = "# Normal Markdown\n\nNo conditionals here.",
        output = renderMarkdownTemplate(input);
      expect(output).toBe(input);
    }),
    it("should handle conditionals with various expressions", () => {
      (expect(renderMarkdownTemplate("{{#if 1}}\nKeep\n{{/if}}")).toBe("Keep\n"),
        expect(renderMarkdownTemplate("{{#if 0}}\nRemove\n{{/if}}")).toBe(""),
        expect(renderMarkdownTemplate("{{#if null}}\nRemove\n{{/if}}")).toBe(""),
        expect(renderMarkdownTemplate("{{#if undefined}}\nRemove\n{{/if}}")).toBe(""));
    }),
    it("should preserve markdown formatting inside blocks", () => {
      const output = renderMarkdownTemplate("{{#if true}}\n## Header\n- List item 1\n- List item 2\n\n```javascript\nconst x = 1;\n```\n{{/if}}");
      expect(output).toBe("## Header\n- List item 1\n- List item 2\n\n```javascript\nconst x = 1;\n```\n");
    }),
    it("should handle whitespace in conditionals", () => {
      (expect(renderMarkdownTemplate("{{#if   true  }}\nKeep\n{{/if}}")).toBe("Keep\n"), expect(renderMarkdownTemplate("{{#if\ttrue\t}}\nKeep\n{{/if}}")).toBe("Keep\n"));
    }),
    it("should clean up multiple consecutive empty lines", () => {
      const output = renderMarkdownTemplate("# Title\n\n{{#if false}}\n## Hidden Section\nThis should be removed.\n{{/if}}\n\n## Visible Section\nThis is always visible.");
      expect(output).toBe("# Title\n\n## Visible Section\nThis is always visible.");
    }),
    it("should collapse multiple false blocks without excessive empty lines", () => {
      const output = renderMarkdownTemplate("Start\n\n{{#if false}}\nBlock 1\n{{/if}}\n\n{{#if false}}\nBlock 2\n{{/if}}\n\n{{#if false}}\nBlock 3\n{{/if}}\n\nEnd");
      (expect(output).not.toMatch(/\n{3,}/), expect(output).toContain("Start"), expect(output).toContain("End"));
    }),
    it("should not merge the surrounding lines when a single-newline-separated falsy block is removed", () => {
      const output = renderMarkdownTemplate("Line1\n{{#if false}}\nRemoved\n{{/if}}\nLine2\n");
      expect(output).toBe("Line1\nLine2\n");
    }),
    it("should preserve leading spaces with truthy block", () => {
      const output = renderMarkdownTemplate("  {{#if true}}\n  Content with leading spaces\n  {{/if}}");
      expect(output).toBe("  Content with leading spaces\n");
    }),
    it("should remove leading spaces when block is falsy", () => {
      const output = renderMarkdownTemplate("  {{#if false}}\n  Content that should be removed\n  {{/if}}");
      expect(output).toBe("");
    }),
    it("should handle mixed indentation levels", () => {
      const output = renderMarkdownTemplate("{{#if true}}\nNo indent\n{{/if}}\n  {{#if true}}\n  Two space indent\n  {{/if}}\n    {{#if true}}\n    Four space indent\n    {{/if}}");
      expect(output).toBe("No indent\n  Two space indent\n    Four space indent\n");
    }),
    it("should preserve indentation in content when using leading spaces", () => {
      const output = renderMarkdownTemplate("# Header\n\n  {{#if true}}\n  ## Indented subsection\n  This content has two leading spaces\n  {{/if}}\n\nNormal content");
      expect(output).toBe("# Header\n\n  ## Indented subsection\n  This content has two leading spaces\n\nNormal content");
    }),
    it("should handle tabs as leading characters", () => {
      const output = renderMarkdownTemplate("\t{{#if true}}\n\tContent with tab\n\t{{/if}}");
      expect(output).toBe("\tContent with tab\n");
    }),
    it("should handle realistic linter-formatted markdown", () => {
      const inputWithValue = "# Analysis\n\n  {{#if github.event.issue.number}}\n  ## Issue Analysis\n  \n  Analyzing issue #123\n  \n  - Check description\n  - Review labels\n  {{/if}}\n\nContinue with other tasks".replace(
          "github.event.issue.number",
          "123"
        ),
        output = renderMarkdownTemplate(inputWithValue);
      expect(output).toBe("# Analysis\n\n  ## Issue Analysis\n  \n  Analyzing issue #123\n  \n  - Check description\n  - Review labels\n\nContinue with other tasks");
    }),
    it("should preserve closing tag indentation", () => {
      const output = renderMarkdownTemplate("  {{#if true}}\n  Content\n  {{/if}}\nNext line");
      expect(output).toBe("  Content\nNext line");
    }));
  describe("fenced code blocks", () => {
    it("should preserve {{#if false}} markers inside a fenced code block (regression)", () => {
      const input = "```js\n{{#if false}}\nHidden\n{{/if}}\n```";
      const output = renderMarkdownTemplate(input);
      expect(output).toBe(input);
    });
    it("should preserve {{#if true}} markers inside a fenced code block", () => {
      const input = "```js\n{{#if true}}\nVisible\n{{/if}}\n```";
      const output = renderMarkdownTemplate(input);
      expect(output).toBe(input);
    });
    it("should process conditionals outside fenced blocks while preserving inside", () => {
      const input = "{{#if false}}\nRemove this\n{{/if}}\n```js\n{{#if false}}\nKeep this\n{{/if}}\n```";
      const output = renderMarkdownTemplate(input);
      expect(output).toBe("```js\n{{#if false}}\nKeep this\n{{/if}}\n```");
    });
    it("should preserve fence count (no fence markers lost or gained)", () => {
      const input = "```js\n{{#if false}}\nHidden\n{{/if}}\n```";
      const output = renderMarkdownTemplate(input);
      expect((output.match(/`{3,}/g) || []).length).toBe((input.match(/`{3,}/g) || []).length);
    });
    it("should not warn about fence count when a fenced code block inside a false conditional is removed", () => {
      core.warning.mockClear();
      const input = "{{#if false}}\n```js\ncode\n```\n{{/if}}\nOther content";
      renderMarkdownTemplate(input);
      expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("Fence count mismatch"));
    });
    it("should not warn about fence count when multiple fenced blocks inside a false conditional are removed", () => {
      core.warning.mockClear();
      const input = "{{#if false}}\n```js\ncode1\n```\n\n```py\ncode2\n```\n{{/if}}\nOther content";
      renderMarkdownTemplate(input);
      expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("Fence count mismatch"));
    });
    it("should not warn when kept block contains fenced code but removed block also contained fenced code", () => {
      core.warning.mockClear();
      const input = "{{#if false}}\n```js\nremoved\n```\n{{/if}}\n```py\nkept\n```";
      renderMarkdownTemplate(input);
      expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("Fence count mismatch"));
    });
    it("should preserve multiple fenced code blocks unchanged", () => {
      const input = "```js\ncode 1\n```\n\n```py\ncode 2\n```";
      const output = renderMarkdownTemplate(input);
      expect(output).toBe(input);
    });
    it("should handle fenced blocks with language tag and conditional outside", () => {
      const input = "{{#if true}}\nKeep\n{{/if}}\n```python\nprint('hello')\n```";
      const output = renderMarkdownTemplate(input);
      expect(output).toBe("Keep\n```python\nprint('hello')\n```");
    });
  });
  describe("elseif support", () => {
    it("should keep elseif branch when if is false and elseif is true", () => {
      const output = renderMarkdownTemplate("{{#if false}}\nBranch A\n{{#elseif true}}\nBranch B\n{{/if}}");
      expect(output).toContain("Branch B");
      expect(output).not.toContain("Branch A");
    });
    it("should keep if branch and skip elseif when if is true", () => {
      const output = renderMarkdownTemplate("{{#if true}}\nFirst\n{{#elseif true}}\nSecond\n{{/if}}");
      expect(output).toContain("First");
      expect(output).not.toContain("Second");
    });
    it("should fall through to else when no if/elseif matches", () => {
      const output = renderMarkdownTemplate("{{#if false}}\nA\n{{#elseif false}}\nB\n{{#else}}\nFallback\n{{/if}}");
      expect(output).toContain("Fallback");
      expect(output).not.toContain("Branch A");
      expect(output).not.toContain("Branch B");
    });
    it("should support else-if hyphen variant", () => {
      const output = renderMarkdownTemplate("{{#if false}}\nA\n{{#else-if true}}\nB\n{{/if}}");
      expect(output).toContain("B");
      expect(output).not.toContain("A");
    });
    it("should support else_if underscore variant", () => {
      const output = renderMarkdownTemplate("{{#if false}}\nA\n{{#else_if true}}\nB\n{{/if}}");
      expect(output).toContain("B");
      expect(output).not.toContain("A");
    });
    it("should support elseif without hash prefix", () => {
      const output = renderMarkdownTemplate("{{#if false}}\nA\n{{elseif true}}\nB\n{{/if}}");
      expect(output).toContain("B");
      expect(output).not.toContain("A");
    });
    it("should remove entire block when no branch matches", () => {
      const output = renderMarkdownTemplate("{{#if false}}\nA\n{{#elseif false}}\nB\n{{/if}}");
      expect(output).toBe("");
    });
    it("should select first matching branch among multiple elseif", () => {
      const output = renderMarkdownTemplate("{{#if false}}\nA\n{{#elseif false}}\nB\n{{#elseif true}}\nC\n{{#elseif true}}\nD\n{{/if}}");
      expect(output).toContain("C");
      expect(output).not.toContain("A");
      expect(output).not.toContain("B");
      expect(output).not.toContain("D");
    });
    it("should support equality condition in elseif", () => {
      // 'something' != "concise" and 'something' != "verbose" so else branch is selected
      const output = renderMarkdownTemplate('{{#if something == "concise"}}\nConcise\n{{#elseif something == "verbose"}}\nVerbose\n{{#else}}\nDefault\n{{/if}}');
      expect(output).toContain("Default");
      expect(output).not.toContain("Concise");
      expect(output).not.toContain("Verbose");
    });
  });
});
