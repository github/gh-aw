// @ts-check
import { describe, it, expect, vi } from "vitest";
global.core = { debug: vi.fn(), info: vi.fn(), warning: vi.fn(), setFailed: vi.fn() };

const { selectBranch, renderMarkdownTemplate } = require("./fuzz_template_branch_harness.cjs");

describe("fuzz_template_branch_harness", () => {
  describe("selectBranch — seed corpus", () => {
    it("returns if-branch when condition is truthy (no elseif)", () => {
      expect(selectBranch("true", "content\n")).toBe("content\n");
    });

    it("returns null when condition is falsy (no elseif, no else)", () => {
      expect(selectBranch("false", "content\n")).toBeNull();
    });

    it("returns elseif branch when if is false and elseif is true", () => {
      const body = "Branch A\n{{#elseif true}}\nBranch B\n";
      expect(selectBranch("false", body)).toContain("Branch B");
    });

    it("returns else branch when all conditions are false", () => {
      const body = "A\n{{#elseif false}}\nB\n{{#else}}\nFallback\n";
      expect(selectBranch("false", body)).toContain("Fallback");
    });

    it("returns first matching elseif among many", () => {
      const body = "A\n{{#elseif false}}\nB\n{{#elseif true}}\nC\n{{#elseif true}}\nD\n";
      const result = selectBranch("false", body);
      expect(result).toContain("C");
      expect(result).not.toContain("D");
    });

    it("handles else-if hyphen variant", () => {
      const body = "A\n{{#else-if true}}\nB\n";
      expect(selectBranch("false", body)).toContain("B");
    });

    it("handles else_if underscore variant", () => {
      const body = "A\n{{#else_if true}}\nB\n";
      expect(selectBranch("false", body)).toContain("B");
    });

    it("handles elseif without hash", () => {
      const body = "A\n{{elseif true}}\nB\n";
      expect(selectBranch("false", body)).toContain("B");
    });

    it("returns null for fully-false chain with no else", () => {
      const body = "A\n{{#elseif false}}\nB\n{{#elseif false}}\nC\n";
      expect(selectBranch("false", body)).toBeNull();
    });

    it("handles equality condition in elseif (experiment-style)", () => {
      // concise == "concise" is truthy
      const body = 'A\n{{#elseif concise == "concise"}}\nB\n{{#else}}\nC\n';
      const result = selectBranch("false", body);
      expect(result).toContain("B");
      expect(result).not.toContain("C");
    });

    it("does not crash on empty body", () => {
      expect(() => selectBranch("true", "")).not.toThrow();
    });

    it("does not crash on empty condition", () => {
      expect(() => selectBranch("", "content")).not.toThrow();
    });

    it("does not crash on deeply nested elseif chain", () => {
      let body = "";
      for (let i = 0; i < 50; i++) {
        body += `Branch ${i}\n{{#elseif false}}\n`;
      }
      body += `Last\n`;
      expect(() => selectBranch("false", body)).not.toThrow();
    });
  });

  describe("renderMarkdownTemplate — elseif integration", () => {
    it("renders elseif branch when if is false", () => {
      const md = "{{#if false}}\nA\n{{#elseif true}}\nB\n{{/if}}";
      expect(renderMarkdownTemplate(md)).toContain("B");
    });

    it("renders else fallback when all elseif branches are false", () => {
      const md = "{{#if false}}\nA\n{{#elseif false}}\nB\n{{#else}}\nC\n{{/if}}";
      expect(renderMarkdownTemplate(md)).toContain("C");
    });

    it("does not crash on arbitrary whitespace in elseif condition", () => {
      const md = "{{#if false}}\nA\n{{#elseif   true   }}\nB\n{{/if}}";
      expect(() => renderMarkdownTemplate(md)).not.toThrow();
    });

    it("does not crash on empty string", () => {
      expect(() => renderMarkdownTemplate("")).not.toThrow();
    });

    it("does not crash on malformed elseif (no closing }}", () => {
      expect(() => renderMarkdownTemplate("{{#if false}}\nA\n{{#elseif true\nB\n{{/if}}")).not.toThrow();
    });
  });
});
