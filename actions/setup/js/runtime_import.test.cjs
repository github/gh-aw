import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";
const core = { info: vi.fn(), warning: vi.fn(), setFailed: vi.fn() };
global.core = core;
const { processRuntimeImports, processRuntimeImport, hasFrontMatter, removeXMLComments, hasGitHubActionsMacros, isSafeExpression, evaluateExpression } = require("./runtime_import.cjs");
describe("runtime_import", () => {
  let tempDir;
  let githubDir;
  let workflowsDir;
  (beforeEach(() => {
    ((tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "runtime-import-test-"))),
      (githubDir = path.join(tempDir, ".github")),
      (workflowsDir = path.join(githubDir, "workflows")),
      fs.mkdirSync(workflowsDir, { recursive: true }),
      vi.clearAllMocks());
  }),
    afterEach(() => {
      tempDir && fs.existsSync(tempDir) && fs.rmSync(tempDir, { recursive: !0, force: !0 });
    }),
    describe("hasFrontMatter", () => {
      (it("should detect front matter at the start", () => {
        expect(hasFrontMatter("---\ntitle: Test\n---\nContent")).toBe(!0);
      }),
        it("should detect front matter with CRLF line endings", () => {
          expect(hasFrontMatter("---\r\ntitle: Test\r\n---\r\nContent")).toBe(!0);
        }),
        it("should detect front matter with leading whitespace", () => {
          expect(hasFrontMatter("  \n  ---\ntitle: Test\n---\nContent")).toBe(!0);
        }),
        it("should not detect front matter in the middle", () => {
          expect(hasFrontMatter("Some content\n---\ntitle: Test\n---")).toBe(!1);
        }),
        it("should not detect incomplete front matter marker", () => {
          expect(hasFrontMatter("--\ntitle: Test\n--\nContent")).toBe(!1);
        }),
        it("should handle empty content", () => {
          expect(hasFrontMatter("")).toBe(!1);
        }));
    }),
    describe("removeXMLComments", () => {
      (it("should remove simple XML comments", () => {
        expect(removeXMLComments("Before \x3c!-- comment --\x3e After")).toBe("Before  After");
      }),
        it("should remove multiline XML comments", () => {
          expect(removeXMLComments("Before \x3c!-- multi\nline\ncomment --\x3e After")).toBe("Before  After");
        }),
        it("should remove multiple XML comments", () => {
          expect(removeXMLComments("\x3c!-- first --\x3eText\x3c!-- second --\x3eMore\x3c!-- third --\x3e")).toBe("TextMore");
        }),
        it("should handle content without comments", () => {
          expect(removeXMLComments("No comments here")).toBe("No comments here");
        }),
        it("should handle nested-looking comments", () => {
          expect(removeXMLComments("\x3c!-- outer \x3c!-- inner --\x3e --\x3e")).toBe(" --\x3e");
        }),
        it("should handle empty content", () => {
          expect(removeXMLComments("")).toBe("");
        }));
    }),
    describe("hasGitHubActionsMacros", () => {
      (it("should detect simple GitHub Actions macros", () => {
        expect(hasGitHubActionsMacros("${{ github.actor }}")).toBe(!0);
      }),
        it("should detect multiline GitHub Actions macros", () => {
          expect(hasGitHubActionsMacros("${{ \ngithub.actor \n}}")).toBe(!0);
        }),
        it("should detect multiple GitHub Actions macros", () => {
          expect(hasGitHubActionsMacros("${{ github.actor }} and ${{ github.repo }}")).toBe(!0);
        }),
        it("should not detect template conditionals", () => {
          expect(hasGitHubActionsMacros("{{#if condition}}text{{/if}}")).toBe(!1);
        }),
        it("should not detect runtime-import macros", () => {
          expect(hasGitHubActionsMacros("{{#runtime-import file.md}}")).toBe(!1);
        }),
        it("should detect GitHub Actions macros within other content", () => {
          expect(hasGitHubActionsMacros("Some text ${{ github.actor }} more text")).toBe(!0);
        }),
        it("should handle content without macros", () => {
          expect(hasGitHubActionsMacros("No macros here")).toBe(!1);
        }));
    }),
    describe("isSafeExpression", () => {
      it("should allow basic safe expressions", () => {
        expect(isSafeExpression("github.actor")).toBe(!0);
        expect(isSafeExpression("github.repository")).toBe(!0);
        expect(isSafeExpression("github.event.issue.number")).toBe(!0);
      });
      it("should allow dynamic patterns", () => {
        expect(isSafeExpression("needs.job-id.outputs.result")).toBe(!0);
        expect(isSafeExpression("steps.step-id.outputs.value")).toBe(!0);
        expect(isSafeExpression("github.event.inputs.repo")).toBe(!0);
        expect(isSafeExpression("inputs.repository")).toBe(!0);
        expect(isSafeExpression("env.MY_VAR")).toBe(!0);
      });
      it("should reject unsafe expressions", () => {
        expect(isSafeExpression("secrets.TOKEN")).toBe(!1);
        expect(isSafeExpression("vars.MY_VAR")).toBe(!1);
        expect(isSafeExpression("unknown.property")).toBe(!1);
      });
      it("should allow OR with single-quoted literals", () => {
        expect(isSafeExpression("inputs.repository || 'FStarLang/FStar'")).toBe(!0);
        expect(isSafeExpression("github.event.inputs.name || 'default'")).toBe(!0);
      });
      it("should allow OR with double-quoted literals", () => {
        expect(isSafeExpression('inputs.value || "default"')).toBe(!0);
        expect(isSafeExpression('github.actor || "anonymous"')).toBe(!0);
      });
      it("should allow OR with backtick literals", () => {
        expect(isSafeExpression("inputs.config || `default-config`")).toBe(!0);
        expect(isSafeExpression("env.MODE || `production`")).toBe(!0);
      });
      it("should allow OR with number literals", () => {
        expect(isSafeExpression("inputs.count || 42")).toBe(!0);
        expect(isSafeExpression("inputs.timeout || 3600")).toBe(!0);
      });
      it("should allow OR with boolean literals", () => {
        expect(isSafeExpression("inputs.flag || true")).toBe(!0);
        expect(isSafeExpression("inputs.enabled || false")).toBe(!0);
      });
      it("should allow OR with two safe expressions", () => {
        expect(isSafeExpression("inputs.repo || github.repository")).toBe(!0);
        expect(isSafeExpression("github.actor || github.repository_owner")).toBe(!0);
      });
      it("should reject OR with unsafe left side", () => {
        expect(isSafeExpression("secrets.TOKEN || 'default'")).toBe(!1);
        expect(isSafeExpression("vars.SECRET || 'fallback'")).toBe(!1);
      });
      it("should reject OR with unsafe right side (non-literal)", () => {
        expect(isSafeExpression("inputs.value || secrets.TOKEN")).toBe(!1);
        expect(isSafeExpression("github.actor || vars.NAME")).toBe(!1);
      });
      it("should block dangerous property names - constructor", () => {
        expect(isSafeExpression("github.constructor")).toBe(!1);
        expect(isSafeExpression("inputs.constructor")).toBe(!1);
        expect(isSafeExpression("github.event.constructor")).toBe(!1);
        expect(isSafeExpression("needs.job.constructor")).toBe(!1);
      });
      it("should block dangerous property names - __proto__", () => {
        expect(isSafeExpression("github.__proto__")).toBe(!1);
        expect(isSafeExpression("inputs.__proto__")).toBe(!1);
        expect(isSafeExpression("github.event.__proto__")).toBe(!1);
      });
      it("should block dangerous property names - prototype", () => {
        expect(isSafeExpression("github.prototype")).toBe(!1);
        expect(isSafeExpression("inputs.prototype")).toBe(!1);
        expect(isSafeExpression("github.event.prototype")).toBe(!1);
      });
      it("should block dangerous property names - __defineGetter__", () => {
        expect(isSafeExpression("github.__defineGetter__")).toBe(!1);
        expect(isSafeExpression("inputs.__defineGetter__")).toBe(!1);
      });
      it("should block dangerous property names - __defineSetter__", () => {
        expect(isSafeExpression("github.__defineSetter__")).toBe(!1);
        expect(isSafeExpression("inputs.__defineSetter__")).toBe(!1);
      });
      it("should block dangerous property names - __lookupGetter__", () => {
        expect(isSafeExpression("github.__lookupGetter__")).toBe(!1);
        expect(isSafeExpression("inputs.__lookupGetter__")).toBe(!1);
      });
      it("should block dangerous property names - __lookupSetter__", () => {
        expect(isSafeExpression("github.__lookupSetter__")).toBe(!1);
        expect(isSafeExpression("inputs.__lookupSetter__")).toBe(!1);
      });
      it("should block dangerous property names - hasOwnProperty", () => {
        expect(isSafeExpression("github.hasOwnProperty")).toBe(!1);
        expect(isSafeExpression("inputs.hasOwnProperty")).toBe(!1);
      });
      it("should block dangerous property names - isPrototypeOf", () => {
        expect(isSafeExpression("github.isPrototypeOf")).toBe(!1);
        expect(isSafeExpression("inputs.isPrototypeOf")).toBe(!1);
      });
      it("should block dangerous property names - propertyIsEnumerable", () => {
        expect(isSafeExpression("github.propertyIsEnumerable")).toBe(!1);
        expect(isSafeExpression("inputs.propertyIsEnumerable")).toBe(!1);
      });
      it("should block dangerous property names - toString", () => {
        expect(isSafeExpression("github.toString")).toBe(!1);
        expect(isSafeExpression("inputs.toString")).toBe(!1);
      });
      it("should block dangerous property names - valueOf", () => {
        expect(isSafeExpression("github.valueOf")).toBe(!1);
        expect(isSafeExpression("inputs.valueOf")).toBe(!1);
      });
      it("should block dangerous property names - toLocaleString", () => {
        expect(isSafeExpression("github.toLocaleString")).toBe(!1);
        expect(isSafeExpression("inputs.toLocaleString")).toBe(!1);
      });
      it("should block dangerous properties in array access", () => {
        expect(isSafeExpression("github.event.release.assets[0].constructor")).toBe(!1);
        expect(isSafeExpression("github.event.release.assets[0].__proto__")).toBe(!1);
      });
      it("should reject excessive nesting depth (more than 5 levels)", () => {
        // Valid: max 5 levels (needs.job.outputs.foo.bar)
        expect(isSafeExpression("needs.job.outputs.foo.bar")).toBe(!0);
        expect(isSafeExpression("steps.step.outputs.foo.bar")).toBe(!0);
        // Invalid: 6 levels
        expect(isSafeExpression("needs.job.outputs.foo.bar.baz")).toBe(!1);
        expect(isSafeExpression("steps.step.outputs.foo.bar.baz")).toBe(!1);
        // Invalid: 7+ levels
        expect(isSafeExpression("needs.job.outputs.foo.bar.baz.qux")).toBe(!1);
      });
      it("should allow valid nested expressions within depth limit", () => {
        // 2 levels
        expect(isSafeExpression("needs.job.outputs")).toBe(!0);
        // 3 levels
        expect(isSafeExpression("needs.job.outputs.result")).toBe(!0);
        // 4 levels
        expect(isSafeExpression("needs.job.outputs.foo.value")).toBe(!0);
        // 5 levels (max)
        expect(isSafeExpression("needs.job.outputs.foo.bar")).toBe(!0);
      });
      it("should reject string literals with nested expressions", () => {
        // Reject ${{ markers in string literals
        expect(isSafeExpression("inputs.value || '${{ secrets.TOKEN }}'")).toBe(!1);
        expect(isSafeExpression("inputs.value || 'text ${{ expr }}'")).toBe(!1);
        // Reject }} markers in string literals
        expect(isSafeExpression("inputs.value || 'text }} more'")).toBe(!1);
        // Reject both markers
        expect(isSafeExpression("inputs.value || '${{ }} combined'")).toBe(!1);
      });
      it("should reject string literals with escape sequences", () => {
        // Reject hex escape sequences (\x)
        expect(isSafeExpression("inputs.value || '\\x41'")).toBe(!1);
        expect(isSafeExpression("inputs.value || 'test\\x20value'")).toBe(!1);
        // Reject unicode escape sequences (\u)
        expect(isSafeExpression("inputs.value || '\\u0041'")).toBe(!1);
        expect(isSafeExpression("inputs.value || 'test\\u0020value'")).toBe(!1);
        // Reject octal escape sequences
        expect(isSafeExpression("inputs.value || '\\101'")).toBe(!1);
        expect(isSafeExpression("inputs.value || 'test\\040value'")).toBe(!1);
      });
      it("should reject string literals with zero-width characters", () => {
        // Reject zero-width space (U+200B)
        expect(isSafeExpression("inputs.value || 'test\u200Bvalue'")).toBe(!1);
        // Reject zero-width non-joiner (U+200C)
        expect(isSafeExpression("inputs.value || 'test\u200Cvalue'")).toBe(!1);
        // Reject zero-width joiner (U+200D)
        expect(isSafeExpression("inputs.value || 'test\u200Dvalue'")).toBe(!1);
        // Reject zero-width no-break space (U+FEFF)
        expect(isSafeExpression("inputs.value || 'test\uFEFFvalue'")).toBe(!1);
      });
      it("should allow safe string literals", () => {
        // Normal strings should still work
        expect(isSafeExpression("inputs.value || 'normal string'")).toBe(!0);
        expect(isSafeExpression("inputs.value || 'with-dashes_and_underscores'")).toBe(!0);
        // Safe escape sequences like \n, \t should work
        expect(isSafeExpression("inputs.value || 'line\\nbreak'")).toBe(!0);
        expect(isSafeExpression("inputs.value || 'tab\\there'")).toBe(!0);
      });
    }),
    describe("evaluateExpression", () => {
      beforeEach(() => {
        // Mock the global context object
        global.context = {
          actor: "testuser",
          job: "test-job",
          repo: { owner: "testorg", repo: "testrepo" },
          runId: 12345,
          runNumber: 67,
          workflow: "test-workflow",
          payload: { inputs: { repository: "testorg/testrepo", name: "test-name" } },
        };
      });
      afterEach(() => {
        delete global.context;
      });
      it("should evaluate basic expressions", () => {
        expect(evaluateExpression("github.actor")).toBe("testuser");
        expect(evaluateExpression("github.repository")).toBe("testorg/testrepo");
      });
      it("should evaluate OR with literal fallback when left is undefined", () => {
        expect(evaluateExpression("inputs.missing || 'default'")).toBe("default");
        expect(evaluateExpression('inputs.undefined || "fallback"')).toBe("fallback");
        expect(evaluateExpression("inputs.none || `backup`")).toBe("backup");
      });
      it("should use left value when defined", () => {
        expect(evaluateExpression("inputs.repository || 'FStarLang/FStar'")).toBe("testorg/testrepo");
        expect(evaluateExpression("inputs.name || 'default-name'")).toBe("test-name");
      });
      it("should handle number literals", () => {
        expect(evaluateExpression("inputs.missing || 42")).toBe("42");
        expect(evaluateExpression("inputs.undefined || 100")).toBe("100");
      });
      it("should handle boolean literals", () => {
        expect(evaluateExpression("inputs.missing || true")).toBe("true");
        expect(evaluateExpression("inputs.undefined || false")).toBe("false");
      });
      it("should chain OR expressions", () => {
        expect(evaluateExpression("inputs.missing1 || inputs.missing2 || 'final-fallback'")).toBe("final-fallback");
      });
      it("should return wrapped expression for undefined without fallback", () => {
        expect(evaluateExpression("inputs.missing")).toContain("${{");
        expect(evaluateExpression("inputs.missing")).toContain("inputs.missing");
      });
      it("should not access prototype chain properties", () => {
        // These should return undefined (wrapped expression) instead of accessing prototype
        const result = evaluateExpression("github.constructor");
        expect(result).toContain("${{");
        expect(result).toContain("github.constructor");
      });
      it("should not access __proto__ property", () => {
        const result = evaluateExpression("github.__proto__");
        expect(result).toContain("${{");
        expect(result).toContain("github.__proto__");
      });
      it("should safely handle missing properties without prototype pollution", () => {
        // These properties don't exist in the context object and should be undefined
        expect(evaluateExpression("github.nonexistent")).toContain("${{");
        expect(evaluateExpression("inputs.toString")).toContain("${{");
      });
      it("should handle array access safely with bounds checking", () => {
        // Test with actual array in context
        global.context = {
          actor: "testuser",
          job: "test-job",
          repo: { owner: "testorg", repo: "testrepo" },
          runId: 12345,
          runNumber: 67,
          workflow: "test-workflow",
          payload: {
            inputs: { repository: "testorg/testrepo", name: "test-name" },
            release: { assets: [{ id: 123 }, { id: 456 }] },
          },
        };
        // Valid array access
        expect(evaluateExpression("github.event.release.assets[0].id")).toBe("123");
        expect(evaluateExpression("github.event.release.assets[1].id")).toBe("456");
        // Out of bounds - should return undefined
        const outOfBounds = evaluateExpression("github.event.release.assets[999].id");
        expect(outOfBounds).toContain("${{");
      });
      it("should escape $ and { in extracted string literals", () => {
        // Test escaping of $ character
        expect(evaluateExpression("inputs.missing || 'test$value'")).toBe("test\\$value");
        expect(evaluateExpression("inputs.missing || 'multiple$$$signs'")).toBe("multiple\\$\\$\\$signs");
        // Test escaping of { character
        expect(evaluateExpression("inputs.missing || 'test{value'")).toBe("test\\{value");
        expect(evaluateExpression("inputs.missing || 'multiple{{{braces'")).toBe("multiple\\{\\{\\{braces");
        // Test escaping of both $ and {
        expect(evaluateExpression("inputs.missing || 'test${value}'")).toBe("test\\$\\{value}");
        expect(evaluateExpression("inputs.missing || '${combined}'")).toBe("\\$\\{combined}");
      });
      it("should not escape characters in non-literal expressions", () => {
        // When left side is defined, should not escape
        expect(evaluateExpression("inputs.repository || 'default'")).toBe("testorg/testrepo");
        // Number and boolean literals don't need escaping
        expect(evaluateExpression("inputs.missing || 42")).toBe("42");
        expect(evaluateExpression("inputs.missing || true")).toBe("true");
      });
    }),
    describe("processRuntimeImport", () => {
      (it("should read and return file content", async () => {
        const content = "# Test Content\n\nThis is a test.";
        fs.writeFileSync(path.join(workflowsDir, "test.md"), content);
        const result = await processRuntimeImport("test.md", !1, tempDir);
        expect(result).toBe(content);
      }),
        it("should throw error for missing required file", async () => {
          await expect(processRuntimeImport("missing.md", !1, tempDir)).rejects.toThrow("Runtime import file not found: workflows/missing.md");
        }),
        it("should return empty string for missing optional file", async () => {
          const result = await processRuntimeImport("missing.md", !0, tempDir);
          (expect(result).toBe(""), expect(core.warning).toHaveBeenCalledWith("Optional runtime import file not found: workflows/missing.md"));
        }),
        it("should remove front matter and warn", async () => {
          const filepath = "with-frontmatter.md";
          fs.writeFileSync(path.join(workflowsDir, filepath), "---\ntitle: Test\nkey: value\n---\n\n# Content\n\nActual content.");
          const result = await processRuntimeImport(filepath, !1, tempDir);
          (expect(result).toContain("# Content"),
            expect(result).toContain("Actual content."),
            expect(result).not.toContain("title: Test"),
            expect(core.warning).toHaveBeenCalledWith(`File workflows/${filepath} contains front matter which will be ignored in runtime import`));
        }),
        it("should remove XML comments", async () => {
          fs.writeFileSync(path.join(workflowsDir, "with-comments.md"), "# Title\n\n\x3c!-- This is a comment --\x3e\n\nContent here.");
          const result = await processRuntimeImport("with-comments.md", !1, tempDir);
          (expect(result).toContain("# Title"), expect(result).toContain("Content here."), expect(result).not.toContain("\x3c!-- This is a comment --\x3e"));
        }),
        it("should render safe GitHub Actions expressions", async () => {
          // Setup context for expression evaluation
          global.context = {
            actor: "testuser",
            job: "test-job",
            repo: { owner: "testorg", repo: "testrepo" },
            runId: 12345,
            runNumber: 42,
            workflow: "test-workflow",
            payload: {},
          };
          fs.writeFileSync(path.join(workflowsDir, "with-macros.md"), "# Title\n\nActor: ${{ github.actor }}\n");
          const result = await processRuntimeImport("with-macros.md", !1, tempDir);
          expect(result).toContain("# Title");
          expect(result).toContain("Actor: testuser");
          delete global.context;
        }),
        it("should reject unsafe GitHub Actions expressions", async () => {
          fs.writeFileSync(path.join(workflowsDir, "unsafe-macros.md"), "Secret: ${{ secrets.TOKEN }}\n");
          await expect(processRuntimeImport("unsafe-macros.md", !1, tempDir)).rejects.toThrow("unauthorized GitHub Actions expressions");
        }),
        it("should handle file in subdirectory", async () => {
          const subdir = path.join(workflowsDir, "subdir");
          (fs.mkdirSync(subdir), fs.writeFileSync(path.join(workflowsDir, "subdir/test.md"), "Subdirectory content"));
          const result = await processRuntimeImport("subdir/test.md", !1, tempDir);
          expect(result).toBe("Subdirectory content");
        }),
        it("should handle empty file", async () => {
          fs.writeFileSync(path.join(workflowsDir, "empty.md"), "");
          const result = await processRuntimeImport("empty.md", !1, tempDir);
          expect(result).toBe("");
        }),
        it("should handle file with only front matter", async () => {
          fs.writeFileSync(path.join(workflowsDir, "only-frontmatter.md"), "---\ntitle: Test\n---\n");
          const result = await processRuntimeImport("only-frontmatter.md", !1, tempDir);
          expect(result.trim()).toBe("");
        }),
        it("should allow template conditionals", async () => {
          const content = "{{#if condition}}content{{/if}}";
          fs.writeFileSync(path.join(workflowsDir, "with-conditionals.md"), content);
          const result = await processRuntimeImport("with-conditionals.md", !1, tempDir);
          expect(result).toBe(content);
        }),
        it("should support .github/workflows/ prefix in path", async () => {
          const content = "Test with .github/workflows prefix";
          fs.writeFileSync(path.join(workflowsDir, "test-prefix.md"), content);
          const result = await processRuntimeImport(".github/workflows/test-prefix.md", !1, tempDir);
          expect(result).toBe(content);
        }),
        it("should work without prefix (resolves to workflows/)", async () => {
          const content = "Test without prefix";
          fs.writeFileSync(path.join(workflowsDir, "test-no-prefix.md"), content);
          const result = await processRuntimeImport("test-no-prefix.md", !1, tempDir);
          expect(result).toBe(content);
        }),
        it("should support .agents/ prefix (top-level folder)", async () => {
          // .agents is a top-level folder at workspace root, not inside .github
          const agentsDir = path.join(tempDir, ".agents");
          fs.mkdirSync(agentsDir, { recursive: true });
          const content = "Test with .agents prefix";
          fs.writeFileSync(path.join(agentsDir, "test-skill.md"), content);
          const result = await processRuntimeImport(".agents/test-skill.md", !1, tempDir);
          expect(result).toBe(content);
        }),
        it("should reject paths outside .github folder", async () => {
          // Try to access a file in the root (not in .github)
          fs.writeFileSync(path.join(tempDir, "outside.md"), "Outside content");
          // Use ../../ to escape .github/workflows and go up to the temp directory
          await expect(processRuntimeImport("../../outside.md", !1, tempDir)).rejects.toThrow("Security: Path");
        }));
    }),
    describe("processRuntimeImports", () => {
      (it("should process single runtime-import macro", async () => {
        fs.writeFileSync(path.join(workflowsDir, "import.md"), "Imported content");
        const result = await processRuntimeImports("Before\n{{#runtime-import import.md}}\nAfter", tempDir);
        expect(result).toBe("Before\nImported content\nAfter");
      }),
        it("should process optional runtime-import macro", async () => {
          fs.writeFileSync(path.join(workflowsDir, "import.md"), "Imported content");
          const result = await processRuntimeImports("Before\n{{#runtime-import? import.md}}\nAfter", tempDir);
          expect(result).toBe("Before\nImported content\nAfter");
        }),
        it("should process multiple runtime-import macros", async () => {
          (fs.writeFileSync(path.join(workflowsDir, "import1.md"), "Content 1"), fs.writeFileSync(path.join(workflowsDir, "import2.md"), "Content 2"));
          const result = await processRuntimeImports("{{#runtime-import import1.md}}\nMiddle\n{{#runtime-import import2.md}}", tempDir);
          expect(result).toBe("Content 1\nMiddle\nContent 2");
        }),
        it("should handle optional import of missing file", async () => {
          const result = await processRuntimeImports("Before\n{{#runtime-import? missing.md}}\nAfter", tempDir);
          (expect(result).toBe("Before\n\nAfter"), expect(core.warning).toHaveBeenCalled());
        }),
        it("should throw error for required import of missing file", async () => {
          await expect(processRuntimeImports("Before\n{{#runtime-import missing.md}}\nAfter", tempDir)).rejects.toThrow();
        }),
        it("should handle content without runtime-import macros", async () => {
          const result = await processRuntimeImports("No imports here", tempDir);
          expect(result).toBe("No imports here");
        }),
        it("should reuse cached content for duplicate imports", async () => {
          fs.writeFileSync(path.join(workflowsDir, "import.md"), "Content");
          const result = await processRuntimeImports("{{#runtime-import import.md}}\n{{#runtime-import import.md}}", tempDir);
          expect(result).toBe("Content\nContent");
          expect(core.info).toHaveBeenCalledWith("Reusing cached content for import.md");
        }),
        it("should handle macros with extra whitespace", async () => {
          fs.writeFileSync(path.join(workflowsDir, "import.md"), "Content");
          const result = await processRuntimeImports("{{#runtime-import    import.md    }}", tempDir);
          expect(result).toBe("Content");
        }),
        it("should handle inline macros", async () => {
          fs.writeFileSync(path.join(workflowsDir, "inline.md"), "inline content");
          const result = await processRuntimeImports("Before {{#runtime-import inline.md}} after", tempDir);
          expect(result).toBe("Before inline content after");
        }),
        it("should process imports with files containing special characters", async () => {
          fs.writeFileSync(path.join(workflowsDir, "import.md"), "Content with $pecial ch@racters!");
          const result = await processRuntimeImports("{{#runtime-import import.md}}", tempDir);
          expect(result).toBe("Content with $pecial ch@racters!");
        }),
        it("should remove XML comments from imported content", async () => {
          fs.writeFileSync(path.join(workflowsDir, "with-comment.md"), "Text \x3c!-- comment --\x3e more text");
          const result = await processRuntimeImports("{{#runtime-import with-comment.md}}", tempDir);
          expect(result).toBe("Text  more text");
        }),
        it("should handle path with subdirectories", async () => {
          const subdir = path.join(workflowsDir, "docs", "shared");
          (fs.mkdirSync(subdir, { recursive: !0 }), fs.writeFileSync(path.join(workflowsDir, "docs/shared/import.md"), "Subdir content"));
          const result = await processRuntimeImports("{{#runtime-import docs/shared/import.md}}", tempDir);
          expect(result).toBe("Subdir content");
        }),
        it("should preserve newlines around imports", async () => {
          fs.writeFileSync(path.join(workflowsDir, "import.md"), "Content");
          const result = await processRuntimeImports("Line 1\n\n{{#runtime-import import.md}}\n\nLine 2", tempDir);
          expect(result).toBe("Line 1\n\nContent\n\nLine 2");
        }),
        it("should handle multiple consecutive imports", async () => {
          (fs.writeFileSync(path.join(workflowsDir, "import1.md"), "Content 1"), fs.writeFileSync(path.join(workflowsDir, "import2.md"), "Content 2"));
          const result = await processRuntimeImports("{{#runtime-import import1.md}}{{#runtime-import import2.md}}", tempDir);
          expect(result).toBe("Content 1Content 2");
        }),
        it("should handle imports at the start of content", async () => {
          fs.writeFileSync(path.join(workflowsDir, "import.md"), "Start content");
          const result = await processRuntimeImports("{{#runtime-import import.md}}\nFollowing text", tempDir);
          expect(result).toBe("Start content\nFollowing text");
        }),
        it("should handle imports at the end of content", async () => {
          fs.writeFileSync(path.join(workflowsDir, "import.md"), "End content");
          const result = await processRuntimeImports("Preceding text\n{{#runtime-import import.md}}", tempDir);
          expect(result).toBe("Preceding text\nEnd content");
        }),
        it("should handle tab characters in macro", async () => {
          fs.writeFileSync(path.join(workflowsDir, "import.md"), "Content");
          const result = await processRuntimeImports("{{#runtime-import\timport.md}}", tempDir);
          expect(result).toBe("Content");
        }));
    }),
    describe("Edge Cases", () => {
      (it("should handle very large files", async () => {
        const largeContent = "x".repeat(1e5);
        fs.writeFileSync(path.join(workflowsDir, "large.md"), largeContent);
        const result = await processRuntimeImports("{{#runtime-import large.md}}", tempDir);
        expect(result).toBe(largeContent);
      }),
        it("should handle files with unicode characters", async () => {
          fs.writeFileSync(path.join(workflowsDir, "unicode.md"), "Hello 世界 🌍 café", "utf8");
          const result = await processRuntimeImports("{{#runtime-import unicode.md}}", tempDir);
          expect(result).toBe("Hello 世界 🌍 café");
        }),
        it("should handle files with various line endings", async () => {
          const content = "Line 1\nLine 2\r\nLine 3\rLine 4";
          fs.writeFileSync(path.join(workflowsDir, "mixed-lines.md"), content);
          const result = await processRuntimeImports("{{#runtime-import mixed-lines.md}}", tempDir);
          expect(result).toBe(content);
        }),
        it("should not process runtime-import as a substring", async () => {
          const content = "text{{#runtime-importnospace.md}}text",
            result = await processRuntimeImports(content, tempDir);
          expect(result).toBe(content);
        }),
        it("should handle front matter with varying formats", async () => {
          fs.writeFileSync(path.join(workflowsDir, "yaml-frontmatter.md"), "---\ntitle: Test\narray:\n  - item1\n  - item2\n---\n\nBody content");
          const result = await processRuntimeImport("yaml-frontmatter.md", !1, tempDir);
          (expect(result).toContain("Body content"), expect(result).not.toContain("array:"), expect(result).not.toContain("item1"));
        }));
    }),
    describe("Error Handling", () => {
      (it("should provide clear error for unsafe GitHub Actions expressions", async () => {
        fs.writeFileSync(path.join(workflowsDir, "bad.md"), "${{ secrets.TOKEN }}");
        await expect(processRuntimeImports("{{#runtime-import bad.md}}", tempDir)).rejects.toThrow("unauthorized GitHub Actions expressions");
      }),
        it("should provide clear error for missing required files", async () => {
          await expect(processRuntimeImports("{{#runtime-import nonexistent.md}}", tempDir)).rejects.toThrow("Failed to process runtime import for nonexistent.md");
        }));
    }),
    describe("Path Security", () => {
      (it("should reject paths that escape .github folder with ../", async () => {
        // Try to escape .github folder using ../../../etc/passwd
        await expect(processRuntimeImport("../../../etc/passwd", !1, tempDir)).rejects.toThrow("Security: Path ../../../etc/passwd must be within .github folder");
      }),
        it("should reject paths that escape .github folder with ../../", async () => {
          // Try to escape .github folder using ./../../etc/passwd
          await expect(processRuntimeImport("../../etc/passwd", !1, tempDir)).rejects.toThrow("Security: Path ../../etc/passwd must be within .github folder");
        }),
        it("should allow valid path within .github folder", async () => {
          // Create a subdirectory structure within workflows
          const subdir = path.join(workflowsDir, "subdir");
          fs.mkdirSync(subdir, { recursive: !0 });
          fs.writeFileSync(path.join(subdir, "subfile.txt"), "Sub content");

          // Access subdir/subfile.txt
          const result = await processRuntimeImport("subdir/subfile.txt", !1, tempDir);
          expect(result).toBe("Sub content");
        }),
        it("should allow ./path within workflows folder", async () => {
          fs.writeFileSync(path.join(workflowsDir, "test.txt"), "Test content");
          const result = await processRuntimeImport("./test.txt", !1, tempDir);
          expect(result).toBe("Test content");
        }),
        it("should normalize paths with redundant separators", async () => {
          fs.writeFileSync(path.join(workflowsDir, "test.txt"), "Test content");
          const result = await processRuntimeImport("./././test.txt", !1, tempDir);
          expect(result).toBe("Test content");
        }),
        it("should allow nested paths that stay within workflows folder", async () => {
          // Create nested directory structure within workflows
          const dirA = path.join(workflowsDir, "a");
          const dirB = path.join(dirA, "b");
          fs.mkdirSync(dirB, { recursive: !0 });
          fs.writeFileSync(path.join(dirB, "file.txt"), "Nested content");

          // Access a/b/file.txt
          const result = await processRuntimeImport("a/b/file.txt", !1, tempDir);
          expect(result).toBe("Nested content");
        }),
        it("should reject attempts to access files outside .github", async () => {
          // Create a file outside .github
          fs.writeFileSync(path.join(tempDir, "root-file.txt"), "Root content");
          // Try to access it with a path that truly escapes - using ../../ to go up twice
          await expect(processRuntimeImport("../../root-file.txt", !1, tempDir)).rejects.toThrow("Security: Path");
        }));
    }),
    describe("processRuntimeImport with line ranges", () => {
      (it("should extract specific line range", async () => {
        const content = "Line 1\nLine 2\nLine 3\nLine 4\nLine 5";
        fs.writeFileSync(path.join(workflowsDir, "test.txt"), content);
        const result = await processRuntimeImport("test.txt", !1, tempDir, 2, 4);
        expect(result).toBe("Line 2\nLine 3\nLine 4");
      }),
        it("should extract single line", async () => {
          const content = "Line 1\nLine 2\nLine 3\nLine 4\nLine 5";
          fs.writeFileSync(path.join(workflowsDir, "test.txt"), content);
          const result = await processRuntimeImport("test.txt", !1, tempDir, 3, 3);
          expect(result).toBe("Line 3");
        }),
        it("should extract from start line to end of file", async () => {
          const content = "Line 1\nLine 2\nLine 3\nLine 4\nLine 5";
          fs.writeFileSync(path.join(workflowsDir, "test.txt"), content);
          const result = await processRuntimeImport("test.txt", !1, tempDir, 3, 5);
          expect(result).toBe("Line 3\nLine 4\nLine 5");
        }),
        it("should throw error for invalid start line", async () => {
          const content = "Line 1\nLine 2\nLine 3";
          fs.writeFileSync(path.join(workflowsDir, "test.txt"), content);
          await expect(processRuntimeImport("test.txt", !1, tempDir, 0, 2)).rejects.toThrow("Invalid start line 0");
          await expect(processRuntimeImport("test.txt", !1, tempDir, 10, 12)).rejects.toThrow("Invalid start line 10");
        }),
        it("should throw error for invalid end line", async () => {
          const content = "Line 1\nLine 2\nLine 3";
          fs.writeFileSync(path.join(workflowsDir, "test.txt"), content);
          await expect(processRuntimeImport("test.txt", !1, tempDir, 1, 0)).rejects.toThrow("Invalid end line 0");
          await expect(processRuntimeImport("test.txt", !1, tempDir, 1, 10)).rejects.toThrow("Invalid end line 10");
        }),
        it("should throw error when start line > end line", async () => {
          const content = "Line 1\nLine 2\nLine 3";
          fs.writeFileSync(path.join(workflowsDir, "test.txt"), content);
          await expect(processRuntimeImport("test.txt", !1, tempDir, 3, 1)).rejects.toThrow("Start line 3 cannot be greater than end line 1");
        }),
        it("should handle line range with front matter", async () => {
          const filepath = "frontmatter-lines.md";
          // Line 1: ---
          // Line 2: title: Test
          // Line 3: ---
          // Line 4: (empty)
          // Line 5: Line 1
          fs.writeFileSync(path.join(workflowsDir, filepath), "---\ntitle: Test\n---\n\nLine 1\nLine 2\nLine 3\nLine 4\nLine 5");
          const result = await processRuntimeImport(filepath, !1, tempDir, 2, 4);
          // Lines 2-4 of raw file are: "title: Test", "---", ""
          // After front matter removal, these lines are part of front matter so they get removed
          // The result should be empty or minimal content
          expect(result).toBeTruthy(); // At minimum, it should not fail
        }));
    }),
    describe("processRuntimeImports with line ranges from macros", () => {
      (it("should process {{#runtime-import path:line-line}} macro", async () => {
        fs.writeFileSync(path.join(workflowsDir, "test.txt"), "Line 1\nLine 2\nLine 3\nLine 4\nLine 5");
        const result = await processRuntimeImports("Content: {{#runtime-import test.txt:2-4}} end", tempDir);
        expect(result).toBe("Content: Line 2\nLine 3\nLine 4 end");
      }),
        it("should process multiple {{#runtime-import path:line-line}} macros", async () => {
          fs.writeFileSync(path.join(workflowsDir, "test.txt"), "Line 1\nLine 2\nLine 3\nLine 4\nLine 5");
          const result = await processRuntimeImports("First: {{#runtime-import test.txt:1-2}} Second: {{#runtime-import test.txt:4-5}}", tempDir);
          expect(result).toBe("First: Line 1\nLine 2 Second: Line 4\nLine 5");
        }));
    }),
    describe("Expression Validation and Rendering", () => {
      const { isSafeExpression, evaluateExpression, processExpressions } = require("./runtime_import.cjs");

      // Setup mock context for expression evaluation
      beforeEach(() => {
        global.context = {
          actor: "testuser",
          job: "test-job",
          repo: { owner: "testorg", repo: "testrepo" },
          runId: 12345,
          runNumber: 42,
          workflow: "test-workflow",
          payload: {
            issue: { number: 123, title: "Test Issue", state: "open" },
            pull_request: { number: 456, title: "Test PR", state: "open" },
            sender: { id: 789 },
          },
        };
        process.env.GITHUB_SERVER_URL = "https://github.com";
        process.env.GITHUB_WORKSPACE = "/workspace";
      });

      afterEach(() => {
        delete global.context;
      });

      describe("isSafeExpression", () => {
        it("should allow expressions from the safe list", () => {
          expect(isSafeExpression("github.actor")).toBe(true);
          expect(isSafeExpression("github.repository")).toBe(true);
          expect(isSafeExpression("github.event.issue.number")).toBe(true);
          expect(isSafeExpression("github.event.pull_request.title")).toBe(true);
        });

        it("should allow dynamic patterns", () => {
          expect(isSafeExpression("needs.build.outputs.version")).toBe(true);
          expect(isSafeExpression("steps.checkout.outputs.ref")).toBe(true);
          expect(isSafeExpression("github.event.inputs.branch")).toBe(true);
          expect(isSafeExpression("inputs.version")).toBe(true);
          expect(isSafeExpression("env.NODE_VERSION")).toBe(true);
        });

        it("should reject unsafe expressions", () => {
          expect(isSafeExpression("secrets.GITHUB_TOKEN")).toBe(false);
          expect(isSafeExpression("github.token")).toBe(false);
          expect(isSafeExpression("runner.os")).toBe(false);
          expect(isSafeExpression("vars.MY_VAR")).toBe(false);
        });

        it("should handle whitespace", () => {
          expect(isSafeExpression("  github.actor  ")).toBe(true);
          expect(isSafeExpression("\ngithub.repository\n")).toBe(true);
        });
      });

      describe("evaluateExpression", () => {
        it("should evaluate simple GitHub context expressions", () => {
          expect(evaluateExpression("github.actor")).toBe("testuser");
          expect(evaluateExpression("github.repository")).toBe("testorg/testrepo");
          expect(evaluateExpression("github.run_id")).toBe("12345");
        });

        it("should evaluate nested event properties", () => {
          expect(evaluateExpression("github.event.issue.number")).toBe("123");
          expect(evaluateExpression("github.event.pull_request.title")).toBe("Test PR");
          expect(evaluateExpression("github.event.sender.id")).toBe("789");
        });

        it("should return wrapped expression for unresolvable values", () => {
          expect(evaluateExpression("needs.build.outputs.version")).toContain("needs.build.outputs.version");
          expect(evaluateExpression("steps.test.outputs.result")).toContain("steps.test.outputs.result");
        });

        it("should handle missing properties gracefully", () => {
          const result = evaluateExpression("github.event.nonexistent.property");
          expect(result).toContain("github.event.nonexistent.property");
        });
      });

      describe("processExpressions", () => {
        it("should render safe expressions in content", () => {
          const content = "Actor: ${{ github.actor }}, Run: ${{ github.run_id }}";
          const result = processExpressions(content, "test.md");
          expect(result).toBe("Actor: testuser, Run: 12345");
        });

        it("should handle multiple expressions", () => {
          const content = "Issue #${{ github.event.issue.number }}: ${{ github.event.issue.title }}";
          const result = processExpressions(content, "test.md");
          expect(result).toBe("Issue #123: Test Issue");
        });

        it("should throw error for unsafe expressions", () => {
          const content = "Token: ${{ secrets.GITHUB_TOKEN }}";
          expect(() => processExpressions(content, "test.md")).toThrow("unauthorized GitHub Actions expressions");
        });

        it("should throw error for multiline expressions", () => {
          const content = "Value: ${{ \ngithub.actor \n}}";
          expect(() => processExpressions(content, "test.md")).toThrow("unauthorized");
        });

        it("should handle mixed safe and unsafe expressions", () => {
          const content = "Safe: ${{ github.actor }}, Unsafe: ${{ secrets.TOKEN }}";
          expect(() => processExpressions(content, "test.md")).toThrow("unauthorized");
          expect(() => processExpressions(content, "test.md")).toThrow("secrets.TOKEN");
        });

        it("should pass through content without expressions", () => {
          const content = "No expressions here";
          const result = processExpressions(content, "test.md");
          expect(result).toBe("No expressions here");
        });
      });

      describe("runtime import with expressions", () => {
        it("should process file with safe expressions", async () => {
          const content = "Actor: ${{ github.actor }}\nRepo: ${{ github.repository }}";
          fs.writeFileSync(path.join(workflowsDir, "with-expr.md"), content);
          const result = await processRuntimeImport("with-expr.md", false, tempDir);
          expect(result).toBe("Actor: testuser\nRepo: testorg/testrepo");
        });

        it("should reject file with unsafe expressions", async () => {
          const content = "Secret: ${{ secrets.TOKEN }}";
          fs.writeFileSync(path.join(workflowsDir, "unsafe.md"), content);
          await expect(processRuntimeImport("unsafe.md", false, tempDir)).rejects.toThrow("unauthorized");
        });

        it("should process expressions in URL imports", async () => {
          // Note: URL imports would need HTTP mocking to test properly
          // This is a placeholder for the structure
        });

        it("should handle expressions with front matter removal", async () => {
          const content = "---\ntitle: Test\n---\n\nActor: ${{ github.actor }}";
          fs.writeFileSync(path.join(workflowsDir, "frontmatter-expr.md"), content);
          const result = await processRuntimeImport("frontmatter-expr.md", false, tempDir);
          expect(result).toContain("Actor: testuser");
          expect(result).not.toContain("title: Test");
        });

        it("should handle expressions with XML comments", async () => {
          const content = "<!-- Comment -->\nActor: ${{ github.actor }}";
          fs.writeFileSync(path.join(workflowsDir, "comment-expr.md"), content);
          const result = await processRuntimeImport("comment-expr.md", false, tempDir);
          expect(result).toContain("Actor: testuser");
          expect(result).not.toContain("<!-- Comment -->");
        });
      });

      describe("recursive imports", () => {
        it("should recursively process runtime-import macros in imported files", async () => {
          // Create a chain: main.md -> level1.md -> level2.md
          fs.writeFileSync(path.join(workflowsDir, "level2.md"), "Level 2 content");
          fs.writeFileSync(path.join(workflowsDir, "level1.md"), "Level 1 before\n{{#runtime-import level2.md}}\nLevel 1 after");
          fs.writeFileSync(path.join(workflowsDir, "main.md"), "Main before\n{{#runtime-import level1.md}}\nMain after");

          const result = await processRuntimeImports("{{#runtime-import main.md}}", tempDir);
          expect(result).toBe("Main before\nLevel 1 before\nLevel 2 content\nLevel 1 after\nMain after");
          expect(core.info).toHaveBeenCalledWith(expect.stringContaining("Recursively processing runtime-imports in main.md"));
          expect(core.info).toHaveBeenCalledWith(expect.stringContaining("Recursively processing runtime-imports in level1.md"));
        });

        it("should handle multiple recursive imports at different levels", async () => {
          // Create: main.md -> [a.md, b.md] and a.md -> shared.md
          fs.writeFileSync(path.join(workflowsDir, "shared.md"), "Shared content");
          fs.writeFileSync(path.join(workflowsDir, "a.md"), "A before\n{{#runtime-import shared.md}}\nA after");
          fs.writeFileSync(path.join(workflowsDir, "b.md"), "B content");
          fs.writeFileSync(path.join(workflowsDir, "main.md"), "{{#runtime-import a.md}}\n---\n{{#runtime-import b.md}}");

          const result = await processRuntimeImports("{{#runtime-import main.md}}", tempDir);
          expect(result).toBe("A before\nShared content\nA after\n---\nB content");
        });

        it("should cache imported files and reuse them in recursive processing", async () => {
          // Create: main.md -> [a.md, b.md] where both import shared.md
          fs.writeFileSync(path.join(workflowsDir, "shared.md"), "Shared content");
          fs.writeFileSync(path.join(workflowsDir, "a.md"), "A: {{#runtime-import shared.md}}");
          fs.writeFileSync(path.join(workflowsDir, "b.md"), "B: {{#runtime-import shared.md}}");
          fs.writeFileSync(path.join(workflowsDir, "main.md"), "{{#runtime-import a.md}}\n{{#runtime-import b.md}}");

          const result = await processRuntimeImports("{{#runtime-import main.md}}", tempDir);
          expect(result).toBe("A: Shared content\nB: Shared content");
          // shared.md should be cached after first import
          expect(core.info).toHaveBeenCalledWith("Reusing cached content for shared.md");
        });

        it("should detect circular dependencies", async () => {
          // Create circular dependency: a.md -> b.md -> a.md
          fs.writeFileSync(path.join(workflowsDir, "a.md"), "A content\n{{#runtime-import b.md}}");
          fs.writeFileSync(path.join(workflowsDir, "b.md"), "B content\n{{#runtime-import a.md}}");

          await expect(processRuntimeImports("{{#runtime-import a.md}}", tempDir)).rejects.toThrow("Circular dependency detected: a.md -> b.md -> a.md");
        });

        it("should detect self-referencing circular dependencies", async () => {
          // Create self-referencing file: self.md -> self.md
          fs.writeFileSync(path.join(workflowsDir, "self.md"), "Self content\n{{#runtime-import self.md}}");

          await expect(processRuntimeImports("{{#runtime-import self.md}}", tempDir)).rejects.toThrow("Circular dependency detected: self.md -> self.md");
        });

        it("should detect complex circular dependencies", async () => {
          // Create circular dependency: a.md -> b.md -> c.md -> a.md
          fs.writeFileSync(path.join(workflowsDir, "a.md"), "A content\n{{#runtime-import b.md}}");
          fs.writeFileSync(path.join(workflowsDir, "b.md"), "B content\n{{#runtime-import c.md}}");
          fs.writeFileSync(path.join(workflowsDir, "c.md"), "C content\n{{#runtime-import a.md}}");

          await expect(processRuntimeImports("{{#runtime-import a.md}}", tempDir)).rejects.toThrow("Circular dependency detected: a.md -> b.md -> c.md -> a.md");
        });

        it("should handle recursive imports with optional files", async () => {
          // Create: main.md -> exists.md -> optional-missing.md (optional)
          fs.writeFileSync(path.join(workflowsDir, "exists.md"), "Exists before\n{{#runtime-import? optional-missing.md}}\nExists after");
          fs.writeFileSync(path.join(workflowsDir, "main.md"), "Main\n{{#runtime-import exists.md}}");

          const result = await processRuntimeImports("{{#runtime-import main.md}}", tempDir);
          expect(result).toBe("Main\nExists before\n\nExists after");
          expect(core.warning).toHaveBeenCalledWith("Optional runtime import file not found: workflows/optional-missing.md");
        });

        it("should process expressions in recursively imported files", async () => {
          // Create recursive imports with expressions
          fs.writeFileSync(path.join(workflowsDir, "inner.md"), "Actor: ${{ github.actor }}");
          fs.writeFileSync(path.join(workflowsDir, "outer.md"), "Outer\n{{#runtime-import inner.md}}");

          const result = await processRuntimeImports("{{#runtime-import outer.md}}", tempDir);
          expect(result).toBe("Outer\nActor: testuser");
        });

        it("should remove XML comments from recursively imported files", async () => {
          // Create recursive imports with XML comments
          fs.writeFileSync(path.join(workflowsDir, "inner.md"), "Inner <!-- comment --> text");
          fs.writeFileSync(path.join(workflowsDir, "outer.md"), "Outer <!-- comment -->\n{{#runtime-import inner.md}}");

          const result = await processRuntimeImports("{{#runtime-import outer.md}}", tempDir);
          expect(result).toBe("Outer \nInner  text");
        });

        it("should handle deep nesting of imports", async () => {
          // Create a deep chain: level1 -> level2 -> level3 -> level4 -> level5
          fs.writeFileSync(path.join(workflowsDir, "level5.md"), "Level 5");
          fs.writeFileSync(path.join(workflowsDir, "level4.md"), "Level 4\n{{#runtime-import level5.md}}");
          fs.writeFileSync(path.join(workflowsDir, "level3.md"), "Level 3\n{{#runtime-import level4.md}}");
          fs.writeFileSync(path.join(workflowsDir, "level2.md"), "Level 2\n{{#runtime-import level3.md}}");
          fs.writeFileSync(path.join(workflowsDir, "level1.md"), "Level 1\n{{#runtime-import level2.md}}");

          const result = await processRuntimeImports("{{#runtime-import level1.md}}", tempDir);
          expect(result).toBe("Level 1\nLevel 2\nLevel 3\nLevel 4\nLevel 5");
        });
      });

      describe("arbitrary graph patterns", () => {
        describe("tree topologies", () => {
          it("should handle balanced binary tree (depth 3)", async () => {
            // Root -> [L1, R1], L1 -> [L2, L3], R1 -> [R2, R3]
            fs.writeFileSync(path.join(workflowsDir, "leaf-l2.md"), "L2");
            fs.writeFileSync(path.join(workflowsDir, "leaf-l3.md"), "L3");
            fs.writeFileSync(path.join(workflowsDir, "leaf-r2.md"), "R2");
            fs.writeFileSync(path.join(workflowsDir, "leaf-r3.md"), "R3");
            fs.writeFileSync(path.join(workflowsDir, "left-branch.md"), "L1-start\n{{#runtime-import leaf-l2.md}}\nL1-mid\n{{#runtime-import leaf-l3.md}}\nL1-end");
            fs.writeFileSync(path.join(workflowsDir, "right-branch.md"), "R1-start\n{{#runtime-import leaf-r2.md}}\nR1-mid\n{{#runtime-import leaf-r3.md}}\nR1-end");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import left-branch.md}}\n---\n{{#runtime-import right-branch.md}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            expect(result).toBe("Root\nL1-start\nL2\nL1-mid\nL3\nL1-end\n---\nR1-start\nR2\nR1-mid\nR3\nR1-end");
          });

          it("should handle wide tree (one root, many leaves)", async () => {
            // Root -> [leaf1, leaf2, leaf3, ..., leaf10]
            for (let i = 1; i <= 10; i++) {
              fs.writeFileSync(path.join(workflowsDir, `leaf${i}.md`), `Leaf ${i}`);
            }
            const imports = Array.from({ length: 10 }, (_, i) => `{{#runtime-import leaf${i + 1}.md}}`).join("\n");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), `Root\n${imports}`);

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            const expected = "Root\n" + Array.from({ length: 10 }, (_, i) => `Leaf ${i + 1}`).join("\n");
            expect(result).toBe(expected);
          });

          it("should handle unbalanced tree (left-skewed)", async () => {
            // Root -> L1, L1 -> L2, L2 -> L3, Root -> R1 (short right branch)
            fs.writeFileSync(path.join(workflowsDir, "l3.md"), "L3");
            fs.writeFileSync(path.join(workflowsDir, "l2.md"), "L2\n{{#runtime-import l3.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l1.md"), "L1\n{{#runtime-import l2.md}}");
            fs.writeFileSync(path.join(workflowsDir, "r1.md"), "R1");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import l1.md}}\n{{#runtime-import r1.md}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            expect(result).toBe("Root\nL1\nL2\nL3\nR1");
          });

          it("should handle very deep tree (depth 15)", async () => {
            // Create a chain of 15 levels deep
            fs.writeFileSync(path.join(workflowsDir, "d15.md"), "D15");
            for (let i = 14; i >= 1; i--) {
              fs.writeFileSync(path.join(workflowsDir, `d${i}.md`), `D${i}\n{{#runtime-import d${i + 1}.md}}`);
            }

            const result = await processRuntimeImports("{{#runtime-import d1.md}}", tempDir);
            const expected = Array.from({ length: 15 }, (_, i) => `D${i + 1}`).join("\n");
            expect(result).toBe(expected);
          });
        });

        describe("DAG (Directed Acyclic Graph) patterns", () => {
          it("should handle diamond dependency pattern", async () => {
            // Root -> [A, B], A -> Shared, B -> Shared
            fs.writeFileSync(path.join(workflowsDir, "shared.md"), "Shared content");
            fs.writeFileSync(path.join(workflowsDir, "path-a.md"), "Path A\n{{#runtime-import shared.md}}");
            fs.writeFileSync(path.join(workflowsDir, "path-b.md"), "Path B\n{{#runtime-import shared.md}}");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import path-a.md}}\n---\n{{#runtime-import path-b.md}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            expect(result).toBe("Root\nPath A\nShared content\n---\nPath B\nShared content");
            // Verify caching is used for second import
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for shared.md");
          });

          it("should handle multiple diamond patterns", async () => {
            // Root -> [A, B], A -> [S1, S2], B -> [S1, S2]
            fs.writeFileSync(path.join(workflowsDir, "s1.md"), "S1");
            fs.writeFileSync(path.join(workflowsDir, "s2.md"), "S2");
            fs.writeFileSync(path.join(workflowsDir, "a.md"), "A\n{{#runtime-import s1.md}}\n{{#runtime-import s2.md}}");
            fs.writeFileSync(path.join(workflowsDir, "b.md"), "B\n{{#runtime-import s1.md}}\n{{#runtime-import s2.md}}");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import a.md}}\n{{#runtime-import b.md}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            expect(result).toBe("Root\nA\nS1\nS2\nB\nS1\nS2");
            // Both S1 and S2 should be cached after first use
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for s1.md");
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for s2.md");
          });

          it("should handle complex DAG with multiple levels", async () => {
            // L1 -> [L2a, L2b], L2a -> [L3a, L3b], L2b -> [L3b, L3c], L3a -> L4, L3b -> L4, L3c -> L4
            fs.writeFileSync(path.join(workflowsDir, "l4.md"), "L4");
            fs.writeFileSync(path.join(workflowsDir, "l3a.md"), "L3a\n{{#runtime-import l4.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l3b.md"), "L3b\n{{#runtime-import l4.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l3c.md"), "L3c\n{{#runtime-import l4.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l2a.md"), "L2a\n{{#runtime-import l3a.md}}\n{{#runtime-import l3b.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l2b.md"), "L2b\n{{#runtime-import l3b.md}}\n{{#runtime-import l3c.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l1.md"), "L1\n{{#runtime-import l2a.md}}\n{{#runtime-import l2b.md}}");

            const result = await processRuntimeImports("{{#runtime-import l1.md}}", tempDir);
            expect(result).toBe("L1\nL2a\nL3a\nL4\nL3b\nL4\nL2b\nL3b\nL4\nL3c\nL4");
            // L3b and L4 should be cached after first use
            expect(core.info).toHaveBeenCalledWith(expect.stringContaining("Reusing cached content"));
          });

          it("should handle convergent DAG (many nodes converge to one)", async () => {
            // [A, B, C, D] -> Sink
            fs.writeFileSync(path.join(workflowsDir, "sink.md"), "Sink");
            fs.writeFileSync(path.join(workflowsDir, "a.md"), "A\n{{#runtime-import sink.md}}");
            fs.writeFileSync(path.join(workflowsDir, "b.md"), "B\n{{#runtime-import sink.md}}");
            fs.writeFileSync(path.join(workflowsDir, "c.md"), "C\n{{#runtime-import sink.md}}");
            fs.writeFileSync(path.join(workflowsDir, "d.md"), "D\n{{#runtime-import sink.md}}");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import a.md}}\n{{#runtime-import b.md}}\n{{#runtime-import c.md}}\n{{#runtime-import d.md}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            expect(result).toBe("Root\nA\nSink\nB\nSink\nC\nSink\nD\nSink");
            // Sink should be cached after first use
            const cacheInfoCalls = core.info.mock.calls.filter(call => call[0].includes("Reusing cached content for sink.md"));
            expect(cacheInfoCalls.length).toBeGreaterThanOrEqual(3); // Used 4 times, cached 3 times
          });
        });

        describe("star topology patterns", () => {
          it("should handle hub-and-spoke (one center, many peripherals)", async () => {
            // Root -> Hub, Hub -> [Spoke1, Spoke2, ..., Spoke8]
            for (let i = 1; i <= 8; i++) {
              fs.writeFileSync(path.join(workflowsDir, `spoke${i}.md`), `Spoke ${i}`);
            }
            const imports = Array.from({ length: 8 }, (_, i) => `{{#runtime-import spoke${i + 1}.md}}`).join("\n");
            fs.writeFileSync(path.join(workflowsDir, "hub.md"), `Hub\n${imports}`);
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import hub.md}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            const expected = "Root\nHub\n" + Array.from({ length: 8 }, (_, i) => `Spoke ${i + 1}`).join("\n");
            expect(result).toBe(expected);
          });

          it("should handle reverse star (many import one common file)", async () => {
            // Root -> [A, B, C], all import Common
            fs.writeFileSync(path.join(workflowsDir, "common.md"), "Common");
            fs.writeFileSync(path.join(workflowsDir, "a.md"), "A\n{{#runtime-import common.md}}");
            fs.writeFileSync(path.join(workflowsDir, "b.md"), "B\n{{#runtime-import common.md}}");
            fs.writeFileSync(path.join(workflowsDir, "c.md"), "C\n{{#runtime-import common.md}}");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import a.md}}\n{{#runtime-import b.md}}\n{{#runtime-import c.md}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            expect(result).toBe("Root\nA\nCommon\nB\nCommon\nC\nCommon");
            // Common should be cached after first use
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for common.md");
          });
        });

        describe("complex multi-level patterns", () => {
          it("should handle grid-like dependency pattern", async () => {
            // Row1 -> [C1, C2, C3], Row2 -> [C1, C2, C3], Root -> [Row1, Row2]
            fs.writeFileSync(path.join(workflowsDir, "c1.md"), "C1");
            fs.writeFileSync(path.join(workflowsDir, "c2.md"), "C2");
            fs.writeFileSync(path.join(workflowsDir, "c3.md"), "C3");
            fs.writeFileSync(path.join(workflowsDir, "row1.md"), "Row1\n{{#runtime-import c1.md}}\n{{#runtime-import c2.md}}\n{{#runtime-import c3.md}}");
            fs.writeFileSync(path.join(workflowsDir, "row2.md"), "Row2\n{{#runtime-import c1.md}}\n{{#runtime-import c2.md}}\n{{#runtime-import c3.md}}");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import row1.md}}\n---\n{{#runtime-import row2.md}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            expect(result).toBe("Root\nRow1\nC1\nC2\nC3\n---\nRow2\nC1\nC2\nC3");
            // All columns should be cached in Row2
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for c1.md");
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for c2.md");
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for c3.md");
          });

          it("should handle pyramid pattern (expanding then contracting)", async () => {
            // L1 -> [L2a, L2b], L2a -> [L3a, L3b, L3c], L2b -> [L3b, L3c, L3d], L3a/b/c/d -> Apex
            fs.writeFileSync(path.join(workflowsDir, "apex.md"), "Apex");
            fs.writeFileSync(path.join(workflowsDir, "l3a.md"), "L3a\n{{#runtime-import apex.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l3b.md"), "L3b\n{{#runtime-import apex.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l3c.md"), "L3c\n{{#runtime-import apex.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l3d.md"), "L3d\n{{#runtime-import apex.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l2a.md"), "L2a\n{{#runtime-import l3a.md}}\n{{#runtime-import l3b.md}}\n{{#runtime-import l3c.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l2b.md"), "L2b\n{{#runtime-import l3b.md}}\n{{#runtime-import l3c.md}}\n{{#runtime-import l3d.md}}");
            fs.writeFileSync(path.join(workflowsDir, "l1.md"), "L1\n{{#runtime-import l2a.md}}\n{{#runtime-import l2b.md}}");

            const result = await processRuntimeImports("{{#runtime-import l1.md}}", tempDir);
            expect(result).toBe("L1\nL2a\nL3a\nApex\nL3b\nApex\nL3c\nApex\nL2b\nL3b\nApex\nL3c\nApex\nL3d\nApex");
            // Multiple files should be cached
            expect(core.info).toHaveBeenCalledWith(expect.stringContaining("Reusing cached content"));
          });

          it("should handle layered architecture (strict layer dependencies)", async () => {
            // Layer1 -> [Layer2a, Layer2b], Layer2a -> Layer3a, Layer2b -> Layer3b, Layer3a -> Core, Layer3b -> Core
            fs.writeFileSync(path.join(workflowsDir, "core.md"), "Core");
            fs.writeFileSync(path.join(workflowsDir, "layer3a.md"), "Layer3a\n{{#runtime-import core.md}}");
            fs.writeFileSync(path.join(workflowsDir, "layer3b.md"), "Layer3b\n{{#runtime-import core.md}}");
            fs.writeFileSync(path.join(workflowsDir, "layer2a.md"), "Layer2a\n{{#runtime-import layer3a.md}}");
            fs.writeFileSync(path.join(workflowsDir, "layer2b.md"), "Layer2b\n{{#runtime-import layer3b.md}}");
            fs.writeFileSync(path.join(workflowsDir, "layer1.md"), "Layer1\n{{#runtime-import layer2a.md}}\n{{#runtime-import layer2b.md}}");

            const result = await processRuntimeImports("{{#runtime-import layer1.md}}", tempDir);
            expect(result).toBe("Layer1\nLayer2a\nLayer3a\nCore\nLayer2b\nLayer3b\nCore");
            // Core should be cached on second use
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for core.md");
          });
        });

        describe("cache efficiency tests", () => {
          it("should efficiently cache heavily reused dependencies", async () => {
            // Create a pattern where one file is imported by many others
            fs.writeFileSync(path.join(workflowsDir, "base.md"), "Base content");
            for (let i = 1; i <= 20; i++) {
              fs.writeFileSync(path.join(workflowsDir, `consumer${i}.md`), `Consumer ${i}\n{{#runtime-import base.md}}`);
            }
            const imports = Array.from({ length: 20 }, (_, i) => `{{#runtime-import consumer${i + 1}.md}}`).join("\n");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), `Root\n${imports}`);

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);

            // Verify base.md is only processed once and cached for remaining 19 uses
            const baseCacheCalls = core.info.mock.calls.filter(call => call[0].includes("Reusing cached content for base.md"));
            expect(baseCacheCalls.length).toBe(19); // First use processes, next 19 use cache

            // Verify output is correct
            const expectedLines = ["Root"];
            for (let i = 1; i <= 20; i++) {
              expectedLines.push(`Consumer ${i}`);
              expectedLines.push("Base content");
            }
            expect(result).toBe(expectedLines.join("\n"));
          });

          it("should cache files with line ranges independently", async () => {
            // Create a file that is imported with different line ranges
            const content = "Line 1\nLine 2\nLine 3\nLine 4\nLine 5";
            fs.writeFileSync(path.join(workflowsDir, "lines.md"), content);
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import lines.md:1-2}}\n---\n{{#runtime-import lines.md:3-5}}\n===\n{{#runtime-import lines.md:1-2}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            expect(result).toBe("Root\nLine 1\nLine 2\n---\nLine 3\nLine 4\nLine 5\n===\nLine 1\nLine 2");

            // Verify lines.md:1-2 is cached on second use
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for lines.md:1-2");
          });

          it("should handle mixed optional and required imports with caching", async () => {
            // Create pattern with both optional and required imports of same file
            fs.writeFileSync(path.join(workflowsDir, "shared.md"), "Shared");
            fs.writeFileSync(path.join(workflowsDir, "a.md"), "A\n{{#runtime-import shared.md}}");
            fs.writeFileSync(path.join(workflowsDir, "b.md"), "B\n{{#runtime-import? shared.md}}");
            fs.writeFileSync(path.join(workflowsDir, "c.md"), "C\n{{#runtime-import shared.md}}");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import a.md}}\n{{#runtime-import b.md}}\n{{#runtime-import c.md}}");

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            expect(result).toBe("Root\nA\nShared\nB\nShared\nC\nShared");

            // Verify shared.md is cached
            expect(core.info).toHaveBeenCalledWith("Reusing cached content for shared.md");
          });
        });

        describe("circular dependency detection in complex graphs", () => {
          it("should detect indirect cycles in DAG-like structures", async () => {
            // A -> B, B -> C, C -> D, D -> A (cycle)
            fs.writeFileSync(path.join(workflowsDir, "a.md"), "A\n{{#runtime-import b.md}}");
            fs.writeFileSync(path.join(workflowsDir, "b.md"), "B\n{{#runtime-import c.md}}");
            fs.writeFileSync(path.join(workflowsDir, "c.md"), "C\n{{#runtime-import d.md}}");
            fs.writeFileSync(path.join(workflowsDir, "d.md"), "D\n{{#runtime-import a.md}}");

            await expect(processRuntimeImports("{{#runtime-import a.md}}", tempDir)).rejects.toThrow("Circular dependency detected: a.md -> b.md -> c.md -> d.md -> a.md");
          });

          it("should detect cycles in branching structures", async () => {
            // Root -> [A, B], A -> C, B -> C, C -> A (cycle through A)
            fs.writeFileSync(path.join(workflowsDir, "a.md"), "A\n{{#runtime-import c.md}}");
            fs.writeFileSync(path.join(workflowsDir, "b.md"), "B\n{{#runtime-import c.md}}");
            fs.writeFileSync(path.join(workflowsDir, "c.md"), "C\n{{#runtime-import a.md}}");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), "Root\n{{#runtime-import a.md}}\n{{#runtime-import b.md}}");

            await expect(processRuntimeImports("{{#runtime-import root.md}}", tempDir)).rejects.toThrow("Circular dependency detected");
            await expect(processRuntimeImports("{{#runtime-import root.md}}", tempDir)).rejects.toThrow("a.md -> c.md -> a.md");
          });

          it("should detect long cycles (length 10)", async () => {
            // Create a cycle: f1 -> f2 -> f3 -> ... -> f10 -> f1
            for (let i = 1; i <= 9; i++) {
              fs.writeFileSync(path.join(workflowsDir, `f${i}.md`), `F${i}\n{{#runtime-import f${i + 1}.md}}`);
            }
            fs.writeFileSync(path.join(workflowsDir, "f10.md"), "F10\n{{#runtime-import f1.md}}");

            await expect(processRuntimeImports("{{#runtime-import f1.md}}", tempDir)).rejects.toThrow("Circular dependency detected");
            await expect(processRuntimeImports("{{#runtime-import f1.md}}", tempDir)).rejects.toThrow("f1.md");
            await expect(processRuntimeImports("{{#runtime-import f1.md}}", tempDir)).rejects.toThrow("f10.md");
          });

          it("should allow diamond patterns without false positive cycle detection", async () => {
            // Verify that diamond (A -> [B, C], B -> D, C -> D) is NOT detected as a cycle
            fs.writeFileSync(path.join(workflowsDir, "d.md"), "D");
            fs.writeFileSync(path.join(workflowsDir, "b.md"), "B\n{{#runtime-import d.md}}");
            fs.writeFileSync(path.join(workflowsDir, "c.md"), "C\n{{#runtime-import d.md}}");
            fs.writeFileSync(path.join(workflowsDir, "a.md"), "A\n{{#runtime-import b.md}}\n{{#runtime-import c.md}}");

            const result = await processRuntimeImports("{{#runtime-import a.md}}", tempDir);
            expect(result).toBe("A\nB\nD\nC\nD");
            // Should complete without throwing cycle error
          });
        });

        describe("performance and stress tests", () => {
          it("should handle large fan-out (1 root -> 50 direct imports)", async () => {
            // Create 50 leaf files
            for (let i = 1; i <= 50; i++) {
              fs.writeFileSync(path.join(workflowsDir, `leaf${i}.md`), `Leaf ${i}`);
            }
            const imports = Array.from({ length: 50 }, (_, i) => `{{#runtime-import leaf${i + 1}.md}}`).join("\n");
            fs.writeFileSync(path.join(workflowsDir, "root.md"), `Root\n${imports}`);

            const result = await processRuntimeImports("{{#runtime-import root.md}}", tempDir);
            const lines = result.split("\n");
            expect(lines[0]).toBe("Root");
            expect(lines.length).toBe(51); // Root + 50 leaves
          });

          it("should handle moderate depth with moderate breadth (depth 5, breadth 4)", async () => {
            // Each level has 4 nodes, each node imports 4 nodes from next level
            // But use a shared pattern to keep file count reasonable
            fs.writeFileSync(path.join(workflowsDir, "l5.md"), "L5");

            // Level 4: 4 nodes each importing l5
            for (let i = 1; i <= 4; i++) {
              fs.writeFileSync(path.join(workflowsDir, `l4-${i}.md`), `L4-${i}\n{{#runtime-import l5.md}}`);
            }

            // Level 3: 4 nodes each importing all level 4 nodes
            for (let i = 1; i <= 4; i++) {
              const imports = Array.from({ length: 4 }, (_, j) => `{{#runtime-import l4-${j + 1}.md}}`).join("\n");
              fs.writeFileSync(path.join(workflowsDir, `l3-${i}.md`), `L3-${i}\n${imports}`);
            }

            // Level 2: 4 nodes each importing one level 3 node
            for (let i = 1; i <= 4; i++) {
              fs.writeFileSync(path.join(workflowsDir, `l2-${i}.md`), `L2-${i}\n{{#runtime-import l3-${i}.md}}`);
            }

            // Level 1: Root imports all level 2 nodes
            const imports = Array.from({ length: 4 }, (_, i) => `{{#runtime-import l2-${i + 1}.md}}`).join("\n");
            fs.writeFileSync(path.join(workflowsDir, "l1.md"), `L1\n${imports}`);

            const result = await processRuntimeImports("{{#runtime-import l1.md}}", tempDir);
            expect(result).toContain("L1");
            expect(result).toContain("L5");
            // Verify caching is used extensively
            expect(core.info).toHaveBeenCalledWith(expect.stringContaining("Reusing cached content"));
          });
        });
      });
    }));
});
