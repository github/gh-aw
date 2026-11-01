// @ts-check
const { testTemplateSubstitution, testValueState } = require("./fuzz_template_substitution_harness.cjs");
const fs = require("fs");

describe("fuzz_template_substitution_harness", () => {
  describe("cleanup", () => {
    it.each([false, true])("should remove the prompt and literal sidecar after error=%s", async fail => {
      const mkdtemp = vi.spyOn(fs, "mkdtempSync");
      try {
        const variables = fail
          ? Object.defineProperty({}, "VALUE", {
              enumerable: true,
              get() {
                throw new Error("interpolation failed");
              },
            })
          : { VALUE: "value" };
        const result = await testTemplateSubstitution("__TEST__ ${VALUE}", { TEST: "literal" }, variables);
        expect(mkdtemp).toHaveBeenCalledTimes(1);
        const directory = mkdtemp.mock.results[0].value;
        expect(fs.existsSync(directory)).toBe(false);
        if (fail) {
          expect(result.error).toBe("interpolation failed");
        } else {
          expect(result.error).toBeNull();
          expect(result.result).toBe("literal value");
        }
      } finally {
        mkdtemp.mockRestore();
      }
    });
  });

  describe("testValueState", () => {
    it("should handle undefined values correctly", async () => {
      const result = await testValueState(undefined);
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe(""); // undefined -> ""
      expect(result.isTruthyResult).toBe(false); // empty string is falsy
      expect(result.templateRemoved).toBe(true); // block should be removed
    });

    it("should handle null values correctly", async () => {
      const result = await testValueState(null);
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe(""); // null -> ""
      expect(result.isTruthyResult).toBe(false); // empty string is falsy
      expect(result.templateRemoved).toBe(true); // block should be removed
    });

    it("should handle empty string values correctly", async () => {
      const result = await testValueState("");
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe("");
      expect(result.isTruthyResult).toBe(false);
      expect(result.templateRemoved).toBe(true);
    });

    it("should handle '0' string values correctly", async () => {
      const result = await testValueState("0");
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe("0");
      expect(result.isTruthyResult).toBe(false); // "0" is falsy
      expect(result.templateRemoved).toBe(true);
    });

    it("should handle 'false' string values correctly", async () => {
      const result = await testValueState("false");
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe("false");
      expect(result.isTruthyResult).toBe(false); // "false" is falsy
      expect(result.templateRemoved).toBe(true);
    });

    it("should handle 'null' string values correctly", async () => {
      const result = await testValueState("null");
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe("null");
      expect(result.isTruthyResult).toBe(false); // "null" is falsy
      expect(result.templateRemoved).toBe(true);
    });

    it("should handle 'undefined' string values correctly", async () => {
      const result = await testValueState("undefined");
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe("undefined");
      expect(result.isTruthyResult).toBe(false); // "undefined" is falsy
      expect(result.templateRemoved).toBe(true);
    });

    it("should handle truthy values correctly", async () => {
      const result = await testValueState("some-value");
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe("some-value");
      expect(result.isTruthyResult).toBe(true);
      expect(result.templateRemoved).toBe(false); // block should be kept
    });

    it("should handle numeric string values correctly", async () => {
      const result = await testValueState("123");
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe("123");
      expect(result.isTruthyResult).toBe(true);
      expect(result.templateRemoved).toBe(false);
    });

    it("should handle whitespace values correctly", async () => {
      const result = await testValueState("   ");
      expect(result.error).toBeNull();
      expect(result.substitutedValue).toBe("   ");
      expect(result.isTruthyResult).toBe(false); // whitespace trims to empty
      expect(result.templateRemoved).toBe(true);
    });
  });

  describe("testTemplateSubstitution - full pipeline", () => {
    it("should handle simple placeholder substitution and template rendering", async () => {
      const template = `{{#if __NAME__}}\nHello __NAME__!\n{{/if}}`;
      const result = await testTemplateSubstitution(template, { NAME: "World" }, {});

      expect(result.error).toBeNull();
      expect(result.stages.afterSubstitution).toBe(`{{#if World}}\nHello World!\n{{/if}}`);
      expect(result.result).toBe("Hello World!\n");
    });

    it("should remove template blocks when placeholder is undefined", async () => {
      const template = `Before\n{{#if __VALUE__}}\nContent: __VALUE__\n{{/if}}\nAfter`;
      const result = await testTemplateSubstitution(template, { VALUE: undefined }, {});

      expect(result.error).toBeNull();
      expect(result.stages.afterSubstitution).toContain("{{#if }}");
      // Template removes the block and cleans up newlines
      expect(result.result).toBe("BeforeAfter");
    });

    it("should handle multiple placeholders with mixed states", async () => {
      const template = `{{#if __A__}}\nA: __A__\n{{/if}}\n{{#if __B__}}\nB: __B__\n{{/if}}\n{{#if __C__}}\nC: __C__\n{{/if}}`;
      const result = await testTemplateSubstitution(template, { A: "value-a", B: undefined, C: null }, {});

      expect(result.error).toBeNull();
      expect(result.result).toContain("A: value-a");
      expect(result.result).not.toContain("B:");
      expect(result.result).not.toContain("C:");
    });

    it("should handle variable interpolation after substitution", async () => {
      const template = `{{#if __ENABLED__}}\nRepo: \${REPO_VAR}\n{{/if}}`;
      const result = await testTemplateSubstitution(template, { ENABLED: "true" }, { REPO_VAR: "test/repo" });

      expect(result.error).toBeNull();
      expect(result.stages.afterInterpolation).toContain("Repo: test/repo");
      expect(result.result).toBe("Repo: test/repo\n");
    });

    it("should handle nested conditionals", async () => {
      const template = `{{#if __OUTER__}}\nOuter: __OUTER__\n{{#if __INNER__}}\nInner: __INNER__\n{{/if}}\n{{/if}}`;
      const result = await testTemplateSubstitution(template, { OUTER: "yes", INNER: "also-yes" }, {});

      expect(result.error).toBeNull();
      expect(result.result).toContain("Outer: yes");
      expect(result.result).toContain("Inner: also-yes");
    });

    it("should handle nested conditionals removal (outer is falsy)", async () => {
      const template = `Start\n{{#if __OUTER__}}\nOuter\n{{#if __INNER__}}\nInner\n{{/if}}\n{{/if}}\nEnd`;
      const result = await testTemplateSubstitution(template, { OUTER: undefined, INNER: "value" }, {});

      expect(result.error).toBeNull();
      // The outer conditional is false, so nested content should be removed
      expect(result.result).toContain("Start");
      expect(result.result).toContain("End");
      expect(result.result).not.toContain("Outer");
      expect(result.result).not.toContain("Inner");
    });

    it("should handle inline conditionals", async () => {
      const template = `Value: {{#if __X__}}__X__{{/if}}`;
      const result = await testTemplateSubstitution(template, { X: "123" }, {});

      expect(result.error).toBeNull();
      expect(result.result).toBe("Value: 123");
    });

    it("should remove inline conditionals when falsy", async () => {
      const template = `Value: {{#if __X__}}__X__{{/if}}`;
      const result = await testTemplateSubstitution(template, { X: null }, {});

      expect(result.error).toBeNull();
      expect(result.result).toBe("Value: ");
    });

    it("should clean up excessive blank lines", async () => {
      const template = `Line1\n{{#if __A__}}\nRemoved1\n{{/if}}\n{{#if __B__}}\nRemoved2\n{{/if}}\n{{#if __C__}}\nRemoved3\n{{/if}}\nLine2`;
      const result = await testTemplateSubstitution(template, { A: undefined, B: null, C: "" }, {});

      expect(result.error).toBeNull();
      // Should not have more than 2 consecutive newlines
      expect(result.result).not.toMatch(/\n{3,}/);
      expect(result.result).toContain("Line1");
      expect(result.result).toContain("Line2");
    });

    it("should handle complex GitHub context template", async () => {
      const template = `<github-context>
{{#if __GH_AW_GITHUB_ACTOR__}}
- **actor**: __GH_AW_GITHUB_ACTOR__
{{/if}}
{{#if __GH_AW_GITHUB_REPOSITORY__}}
- **repository**: __GH_AW_GITHUB_REPOSITORY__
{{/if}}
{{#if __GH_AW_GITHUB_EVENT_ISSUE_NUMBER__}}
- **issue**: #__GH_AW_GITHUB_EVENT_ISSUE_NUMBER__
{{/if}}
{{#if __GH_AW_GITHUB_EVENT_COMMENT_ID__}}
- **comment**: __GH_AW_GITHUB_EVENT_COMMENT_ID__
{{/if}}
</github-context>`;

      const result = await testTemplateSubstitution(
        template,
        {
          GH_AW_GITHUB_ACTOR: "testuser",
          GH_AW_GITHUB_REPOSITORY: "test/repo",
          GH_AW_GITHUB_EVENT_ISSUE_NUMBER: "42",
          GH_AW_GITHUB_EVENT_COMMENT_ID: undefined, // Not triggered by comment
        },
        {}
      );

      expect(result.error).toBeNull();
      expect(result.result).toContain("- **actor**: testuser");
      expect(result.result).toContain("- **repository**: test/repo");
      expect(result.result).toContain("- **issue**: #42");
      expect(result.result).not.toContain("comment"); // Should be removed
    });

    it("should handle all GitHub context values undefined", async () => {
      const template = `<github-context>
{{#if __GH_AW_GITHUB_ACTOR__}}
- **actor**: __GH_AW_GITHUB_ACTOR__
{{/if}}
{{#if __GH_AW_GITHUB_REPOSITORY__}}
- **repository**: __GH_AW_GITHUB_REPOSITORY__
{{/if}}
</github-context>`;

      const result = await testTemplateSubstitution(
        template,
        {
          GH_AW_GITHUB_ACTOR: undefined,
          GH_AW_GITHUB_REPOSITORY: null,
        },
        {}
      );

      expect(result.error).toBeNull();
      expect(result.result).toContain("<github-context>");
      expect(result.result).toContain("</github-context>");
      expect(result.result).not.toContain("actor");
      expect(result.result).not.toContain("repository");
    });

    it("should handle special characters in values", async () => {
      const template = `{{#if __VALUE__}}\nValue: __VALUE__\n{{/if}}`;
      const result = await testTemplateSubstitution(template, { VALUE: "test@example.com & <special>" }, {});

      expect(result.error).toBeNull();
      expect(result.result).toContain("test@example.com & <special>");
    });

    it("should handle combined substitution and interpolation", async () => {
      const template = `{{#if __ENABLED__}}\nRepo: \${REPO}\nBranch: __BRANCH__\n{{/if}}`;
      const result = await testTemplateSubstitution(template, { ENABLED: "1", BRANCH: "main" }, { REPO: "owner/repo" });

      expect(result.error).toBeNull();
      expect(result.result).toContain("Repo: owner/repo");
      expect(result.result).toContain("Branch: main");
    });
  });

  describe("edge cases and error handling", () => {
    it("should handle empty template", async () => {
      const result = await testTemplateSubstitution("", {}, {});
      expect(result.error).toBeNull();
      expect(result.result).toBe("");
    });

    it("should handle template with no placeholders", async () => {
      const template = "Just plain text\nNo placeholders here";
      const result = await testTemplateSubstitution(template, {}, {});
      expect(result.error).toBeNull();
      expect(result.result).toBe(template);
    });

    it("should handle template with no conditionals", async () => {
      const template = "Value: __X__";
      const result = await testTemplateSubstitution(template, { X: "test" }, {});
      expect(result.error).toBeNull();
      expect(result.result).toBe("Value: test");
    });

    it("should handle malformed conditionals gracefully", async () => {
      const template = "{{#if __X__}}\nNo closing tag";
      const result = await testTemplateSubstitution(template, { X: "value" }, {});
      // Should not crash, but output may not be fully processed
      expect(result.error).toBeNull();
    });
  });
});
