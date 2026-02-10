import { describe, it, expect, vi } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
const __filename = fileURLToPath(import.meta.url),
  __dirname = path.dirname(__filename),
  core = { info: vi.fn(), setFailed: vi.fn() };
global.core = core;
const { isTruthy } = require("./is_truthy.cjs"),
  interpolatePromptScript = fs.readFileSync(path.join(__dirname, "interpolate_prompt.cjs"), "utf8"),
  renderMarkdownTemplateMatch = interpolatePromptScript.match(/function renderMarkdownTemplate\(markdown\)\s*{[\s\S]*?return result;[\s\S]*?}/);
if (!renderMarkdownTemplateMatch) throw new Error("Could not extract renderMarkdownTemplate function from interpolate_prompt.cjs");
const renderMarkdownTemplate = eval(`(${renderMarkdownTemplateMatch[0]})`);
describe("renderMarkdownTemplate - Additional Edge Cases", () => {
  (describe("inline conditionals (tags not on their own lines)", () => {
    (it("should handle inline conditional at start of line", () => {
      const output = renderMarkdownTemplate("{{#if true}}Keep{{/if}}");
      expect(output).toBe("Keep");
    }),
      it("should handle inline conditional in middle of line", () => {
        const output = renderMarkdownTemplate("Before {{#if true}}Middle{{/if}} After");
        expect(output).toBe("Before Middle After");
      }),
      it("should handle inline conditional with false condition", () => {
        const output = renderMarkdownTemplate("Before {{#if false}}Remove{{/if}} After");
        expect(output).toBe("Before  After");
      }),
      it("should handle multiple inline conditionals on same line", () => {
        const output = renderMarkdownTemplate("{{#if true}}A{{/if}} {{#if false}}B{{/if}} {{#if true}}C{{/if}}");
        expect(output).toBe("A  C");
      }));
  }),
    describe("block conditionals (tags on their own lines)", () => {
      (it("should handle block conditional with tags on own lines", () => {
        const output = renderMarkdownTemplate("{{#if true}}\nContent\n{{/if}}");
        expect(output).toBe("Content\n");
      }),
        it("should remove block conditional with false condition", () => {
          const output = renderMarkdownTemplate("{{#if false}}\nContent\n{{/if}}");
          expect(output).toBe("");
        }),
        it("should handle block conditional with indentation", () => {
          const output = renderMarkdownTemplate("  {{#if true}}\n  Content\n  {{/if}}");
          expect(output).toBe("  Content\n");
        }),
        it("should handle block conditional with tabs", () => {
          const output = renderMarkdownTemplate("\t{{#if true}}\n\tContent\n\t{{/if}}");
          expect(output).toBe("\tContent\n");
        }));
    }),
    describe("whitespace handling", () => {
      (it("should handle spaces before opening tag", () => {
        const output = renderMarkdownTemplate("   {{#if true}}Content{{/if}}");
        expect(output).toBe("   Content");
      }),
        it("should handle spaces after closing tag", () => {
          const output = renderMarkdownTemplate("{{#if true}}Content{{/if}}   ");
          expect(output).toBe("Content   ");
        }),
        it("should handle trailing spaces on tag lines", () => {
          const output = renderMarkdownTemplate("{{#if true}}   \nContent\n{{/if}}   ");
          expect(output.trim()).toBe("Content");
        }),
        it("should handle no newline after opening tag (inline)", () => {
          const output = renderMarkdownTemplate("{{#if true}}Content on same line{{/if}}");
          expect(output).toBe("Content on same line");
        }),
        it("should handle no newline before closing tag (inline)", () => {
          const output = renderMarkdownTemplate("{{#if true}}\nContent{{/if}}");
          expect(output).toBe("Content");
        }));
    }),
    describe("nested and complex structures", () => {
      (it("should handle conditional with markdown formatting", () => {
        const output = renderMarkdownTemplate("{{#if true}}\n## Header\n\n**Bold** and *italic* text\n\n- List item 1\n- List item 2\n{{/if}}");
        expect(output).toBe("## Header\n\n**Bold** and *italic* text\n\n- List item 1\n- List item 2\n");
      }),
        it("should handle conditional with code blocks", () => {
          const output = renderMarkdownTemplate("{{#if true}}\n```javascript\nconst x = 1;\n```\n{{/if}}");
          expect(output).toBe("```javascript\nconst x = 1;\n```\n");
        }),
        it("should NOT handle nested conditionals (not supported)", () => {
          const output = renderMarkdownTemplate("{{#if true}}\nOuter\n{{#if true}}\nInner\n{{/if}}\n{{/if}}");
          expect(output).toContain("Outer");
        }));
    }),
    describe("edge cases with empty lines", () => {
      (it("should clean up multiple consecutive blank lines", () => {
        const output = renderMarkdownTemplate("Start\n\n\n{{#if false}}\nContent\n{{/if}}\n\n\nEnd");
        (expect(output).not.toMatch(/\n{3,}/), expect(output).toContain("Start"), expect(output).toContain("End"));
      }),
        it("should preserve single blank line", () => {
          const output = renderMarkdownTemplate("Line 1\n\nLine 2");
          expect(output).toBe("Line 1\n\nLine 2");
        }),
        it("should clean up triple newlines in input", () => {
          const output = renderMarkdownTemplate("Line 1\n\n\nLine 2");
          expect(output).toBe("Line 1\n\nLine 2");
        }),
        it("should collapse triple blank line to double", () => {
          const output = renderMarkdownTemplate("Line 1\n\n\n\nLine 2");
          expect(output).toBe("Line 1\n\nLine 2");
        }));
    }),
    describe("mixed scenarios", () => {
      (it("should handle mix of inline and block conditionals", () => {
        const output = renderMarkdownTemplate("Start {{#if true}}inline{{/if}} text\n{{#if true}}\nBlock content\n{{/if}}\nEnd");
        expect(output).toBe("Start inline text\nBlock content\nEnd");
      }),
        it("should handle mix of true and false conditionals", () => {
          const output = renderMarkdownTemplate("{{#if true}}\nKeep this\n{{/if}}\n{{#if false}}\nRemove this\n{{/if}}\n{{#if true}}\nKeep this too\n{{/if}}");
          (expect(output).toContain("Keep this"), expect(output).toContain("Keep this too"), expect(output).not.toContain("Remove this"));
        }),
        it("should handle complex real-world example", () => {
          const result = renderMarkdownTemplate(
            "# Workflow Prompt\n\nSome intro text\n\n{{#if github.event.issue.number}}\n## Issue Information\n\nIssue #{github.event.issue.number}\nTitle: {github.event.issue.title}\n{{/if}}\n\n{{#if github.event.pull_request.number}}\n## Pull Request Information\n\nPR #{github.event.pull_request.number}\n{{/if}}\n\n## Instructions\n\nAlways visible instructions here.".replace(
              /github\.event\.\w+\.\w+/g,
              "true"
            )
          );
          (expect(result).toContain("## Issue Information"), expect(result).toContain("## Pull Request Information"), expect(result).toContain("## Instructions"));
        }));
    }),
    describe("boundary conditions", () => {
      (it("should handle empty string", () => {
        const output = renderMarkdownTemplate("");
        expect(output).toBe("");
      }),
        it("should handle string with only whitespace", () => {
          const output = renderMarkdownTemplate("   \n\n   ");
          expect(output).toBe("   \n\n   ");
        }),
        it("should handle unclosed conditional (malformed)", () => {
          const input = "{{#if true}} Content",
            output = renderMarkdownTemplate(input);
          expect(output).toBe(input);
        }),
        it("should handle closing tag without opening (malformed)", () => {
          const output = renderMarkdownTemplate("Content {{/if}}");
          expect(output).toBe("Content {{/if}}");
        }),
        it("should handle empty condition expression (treated as false)", () => {
          const output = renderMarkdownTemplate("{{#if }}Content{{/if}}");
          expect(output).toBe("");
        }),
        it("should handle condition with only whitespace", () => {
          const output = renderMarkdownTemplate("{{#if   }}Content{{/if}}");
          expect(output).toBe("");
        }));
    }),
    describe("special characters in content", () => {
      (it("should handle content with curly braces", () => {
        const output = renderMarkdownTemplate("{{#if true}}{} and {{}}{{/if}}");
        expect(output).toBe("{} and {{}}");
      }),
        it("should handle content with template-like strings", () => {
          const output = renderMarkdownTemplate("{{#if true}}This looks like {{template}} but isn't{{/if}}");
          expect(output).toBe("This looks like {{template}} but isn't");
        }),
        it("should handle content with dollar signs", () => {
          const output = renderMarkdownTemplate("{{#if true}}Price: $100{{/if}}");
          expect(output).toBe("Price: $100");
        }),
        it("should handle content with backslashes", () => {
          const output = renderMarkdownTemplate("{{#if true}}Path: C:\\\\Users\\\\test{{/if}}");
          expect(output).toBe("Path: C:\\\\Users\\\\test");
        }),
        it("should handle content with newlines and special chars", () => {
          const output = renderMarkdownTemplate("{{#if true}}\nLine 1 with $pecial ch@rs!\nLine 2 with {{braces}}\n{{/if}}");
          (expect(output).toContain("Line 1 with $pecial ch@rs!"), expect(output).toContain("Line 2 with {{braces}}"));
        }));
    }),
    describe("performance and edge cases", () => {
      (it("should handle large number of conditionals", () => {
        let input = "";
        for (let i = 0; i < 100; i++) input += `{{#if true}}Block ${i}{{/if}}\n`;
        const output = renderMarkdownTemplate(input);
        (expect(output).toContain("Block 0"), expect(output).toContain("Block 99"));
      }),
        it("should handle long content blocks", () => {
          const longContent = "x".repeat(1e4),
            output = renderMarkdownTemplate(`{{#if true}}\n${longContent}\n{{/if}}`);
          expect(output).toContain(longContent);
        }));
    }),
    describe("leading newline preservation", () => {
      (it("should preserve leading newline when condition is true", () => {
        const output = renderMarkdownTemplate("Line before\n\n{{#if true}}\nContent\n{{/if}}");
        expect(output).toBe("Line before\n\nContent\n");
      }),
        it("should remove leading newline when condition is false", () => {
          const output = renderMarkdownTemplate("Line before\n\n{{#if false}}\nContent\n{{/if}}\nLine after");
          (expect(output).toContain("Line before"), expect(output).toContain("Line after"), expect(output).not.toMatch(/\n{3,}/));
        }),
        it("should handle no leading newline with true condition", () => {
          const output = renderMarkdownTemplate("{{#if true}}\nContent\n{{/if}}");
          expect(output).toBe("Content\n");
        }),
        it("should handle no leading newline with false condition", () => {
          const output = renderMarkdownTemplate("{{#if false}}\nContent\n{{/if}}Line after");
          expect(output).toBe("Line after");
        }));
    }));
});
