import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";
import { fileURLToPath } from "url";
const { ERR_CONFIG } = require("./error_codes.cjs");
const __filename = fileURLToPath(import.meta.url),
  __dirname = path.dirname(__filename),
  core = { info: vi.fn(), warning: vi.fn(), setFailed: vi.fn() };
global.core = core;
const { isTruthy } = require("./is_truthy.cjs"),
  { selectBranch } = require("./template_branch.cjs"),
  interpolatePromptScript = fs.readFileSync(path.join(__dirname, "interpolate_prompt.cjs"), "utf8"),
  interpolateVariablesMatch = interpolatePromptScript.match(/function interpolateVariables\(content, variables\)\s*{[\s\S]*?return result;[\s\S]*?}/);
if (!interpolateVariablesMatch) throw new Error("Could not extract interpolateVariables function from interpolate_prompt.cjs");
const interpolateVariables = eval(`(${interpolateVariablesMatch[0]})`),
  { renderMarkdownTemplate } = require("./render_template.cjs");
describe("interpolate_prompt", () => {
  (describe("interpolateVariables", () => {
    (it("should interpolate single variable", () => {
      const result = interpolateVariables("Repository: ${GH_AW_EXPR_TEST123}", { GH_AW_EXPR_TEST123: "github/test-repo" });
      expect(result).toBe("Repository: github/test-repo");
    }),
      it("should interpolate multiple variables", () => {
        const result = interpolateVariables("Repo: ${GH_AW_EXPR_REPO}, Actor: ${GH_AW_EXPR_ACTOR}, Issue: ${GH_AW_EXPR_ISSUE}", { GH_AW_EXPR_REPO: "github/test-repo", GH_AW_EXPR_ACTOR: "testuser", GH_AW_EXPR_ISSUE: "123" });
        expect(result).toBe("Repo: github/test-repo, Actor: testuser, Issue: 123");
      }),
      it("should handle multiline content", () => {
        const result = interpolateVariables("# Test Workflow\n\nRepository: ${GH_AW_EXPR_REPO}\nActor: ${GH_AW_EXPR_ACTOR}\n\nSome other content here.", { GH_AW_EXPR_REPO: "github/test-repo", GH_AW_EXPR_ACTOR: "testuser" });
        (expect(result).toContain("Repository: github/test-repo"), expect(result).toContain("Actor: testuser"));
      }),
      it("should handle empty variable values", () => {
        const result = interpolateVariables("Value: ${GH_AW_EXPR_EMPTY}", { GH_AW_EXPR_EMPTY: "" });
        expect(result).toBe("Value: ");
      }),
      it("should replace all occurrences of the same variable", () => {
        const result = interpolateVariables("Repo: ${GH_AW_EXPR_REPO}, Same repo: ${GH_AW_EXPR_REPO}", { GH_AW_EXPR_REPO: "github/test-repo" });
        expect(result).toBe("Repo: github/test-repo, Same repo: github/test-repo");
      }),
      it("should not modify content without variables", () => {
        const result = interpolateVariables("No variables here", {});
        expect(result).toBe("No variables here");
      }),
      it("should handle content with literal dollar signs", () => {
        const result = interpolateVariables("Price: $100, Repo: ${GH_AW_EXPR_REPO}", { GH_AW_EXPR_REPO: "github/test-repo" });
        expect(result).toBe("Price: $100, Repo: github/test-repo");
      }),
      it("should not corrupt output when value contains $$ (special replacement pattern)", () => {
        const result = interpolateVariables("Value: ${GH_AW_EXPR_BODY}", { GH_AW_EXPR_BODY: "cost is $$100" });
        expect(result).toBe("Value: cost is $$100");
      }),
      it("should not corrupt output when value contains $& (matched substring pattern)", () => {
        const result = interpolateVariables("Value: ${GH_AW_EXPR_BODY}", { GH_AW_EXPR_BODY: "see $& for details" });
        expect(result).toBe("Value: see $& for details");
      }),
      it("should not corrupt output when value contains $` (before-match pattern)", () => {
        const result = interpolateVariables("Value: ${GH_AW_EXPR_BODY}", { GH_AW_EXPR_BODY: "use $`cmd` to run" });
        expect(result).toBe("Value: use $`cmd` to run");
      }),
      it("should not corrupt output when value contains $' (after-match pattern)", () => {
        const result = interpolateVariables("Value: ${GH_AW_EXPR_BODY}", { GH_AW_EXPR_BODY: "it's $'quoted'" });
        expect(result).toBe("Value: it's $'quoted'");
      }),
      it("should not corrupt output when value contains $1 (capture group pattern)", () => {
        const result = interpolateVariables("Value: ${GH_AW_EXPR_BODY}", { GH_AW_EXPR_BODY: "group $1 matched" });
        expect(result).toBe("Value: group $1 matched");
      }));
  }),
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
        it("should keep true branch of {{#else}} block when condition is truthy", () => {
          const output = renderMarkdownTemplate("{{#if true}}\nTrue branch\n{{#else}}\nFalse branch\n{{#endif}}");
          expect(output).toContain("True branch");
          expect(output).not.toContain("False branch");
          expect(output).not.toContain("{{#else}}");
        }),
        it("should keep false branch of {{#else}} block when condition is falsy", () => {
          const output = renderMarkdownTemplate("{{#if false}}\nTrue branch\n{{#else}}\nFalse branch\n{{#endif}}");
          expect(output).toContain("False branch");
          expect(output).not.toContain("True branch");
          expect(output).not.toContain("{{#else}}");
        }),
        it("should handle {{#else}} with GitHub Actions style equality condition matching", () => {
          // Simulates what happens after experiment substitution: concise == "concise"
          const conciseOutput = renderMarkdownTemplate('{{#if concise == "concise"}}\nConcise content\n{{#else}}\nVerbose content\n{{#endif}}');
          expect(conciseOutput).toContain("Concise content");
          expect(conciseOutput).not.toContain("Verbose content");
          const verboseOutput = renderMarkdownTemplate('{{#if verbose == "concise"}}\nConcise content\n{{#else}}\nVerbose content\n{{#endif}}');
          expect(verboseOutput).toContain("Verbose content");
          expect(verboseOutput).not.toContain("Concise content");
        }),
        it("should support {{/if}} as alternate closing tag", () => {
          const output = renderMarkdownTemplate("{{#if true}}\nKeep\n{{/if}}");
          expect(output).toContain("Keep");
        }),
        it("should keep first elseif branch when if is false and elseif is true", () => {
          const output = renderMarkdownTemplate("{{#if false}}\nBranch A\n{{#elseif true}}\nBranch B\n{{#endif}}");
          expect(output).toContain("Branch B");
          expect(output).not.toContain("Branch A");
          expect(output).not.toContain("{{#elseif}}");
        }),
        it("should skip all elseif branches when none match and use else", () => {
          const output = renderMarkdownTemplate("{{#if false}}\nA\n{{#elseif false}}\nB\n{{#else}}\nFallback\n{{#endif}}");
          expect(output).toContain("Fallback");
          expect(output).not.toContain("Branch A");
          expect(output).not.toContain("Branch B");
        }),
        it("should keep the if branch and skip elseif when if is true", () => {
          const output = renderMarkdownTemplate("{{#if true}}\nFirst\n{{#elseif true}}\nSecond\n{{#endif}}");
          expect(output).toContain("First");
          expect(output).not.toContain("Second");
        }),
        it("should support else-if hyphen syntax variant", () => {
          const output = renderMarkdownTemplate("{{#if false}}\nA\n{{#else-if true}}\nB\n{{#endif}}");
          expect(output).toContain("B");
          expect(output).not.toContain("A");
        }),
        it("should support else_if underscore syntax variant", () => {
          const output = renderMarkdownTemplate("{{#if false}}\nA\n{{#else_if true}}\nB\n{{#endif}}");
          expect(output).toContain("B");
          expect(output).not.toContain("A");
        }),
        it("should support elseif without hash prefix", () => {
          const output = renderMarkdownTemplate("{{#if false}}\nA\n{{elseif true}}\nB\n{{#endif}}");
          expect(output).toContain("B");
          expect(output).not.toContain("A");
        }),
        it("should handle multiple elseif branches selecting the correct one", () => {
          const output = renderMarkdownTemplate("{{#if false}}\nA\n{{#elseif false}}\nB\n{{#elseif true}}\nC\n{{#elseif true}}\nD\n{{#endif}}");
          expect(output).toContain("C");
          expect(output).not.toContain("A");
          expect(output).not.toContain("B");
          expect(output).not.toContain("D");
        }),
        it("should support equality condition in elseif", () => {
          // 'something' != "concise" and 'something' != "verbose" so else branch is selected
          const output = renderMarkdownTemplate('{{#if something == "concise"}}\nConcise\n{{#elseif something == "verbose"}}\nVerbose\n{{#else}}\nDefault\n{{#endif}}');
          expect(output).toContain("Default");
          expect(output).not.toContain("Concise");
          expect(output).not.toContain("Verbose");
        }),
        it("should not warn about fence count when a fenced code block inside a false conditional is removed", () => {
          core.warning.mockClear();
          const input = "{{#if false}}\n```js\ncode\n```\n{{/if}}\nOther content";
          renderMarkdownTemplate(input);
          expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("Fence count mismatch"));
        }),
        it("should not warn about fence count when multiple fenced blocks inside a false conditional are removed", () => {
          core.warning.mockClear();
          const input = "{{#if false}}\n```js\ncode1\n```\n\n```py\ncode2\n```\n{{/if}}\nOther content";
          renderMarkdownTemplate(input);
          expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("Fence count mismatch"));
        }),
        it("should not warn when kept block contains fenced code but removed block also contained fenced code", () => {
          core.warning.mockClear();
          const input = "{{#if false}}\n```js\nremoved\n```\n{{/if}}\n```py\nkept\n```";
          renderMarkdownTemplate(input);
          expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("Fence count mismatch"));
        }));
    }),
    describe("combined interpolation and template rendering", () => {
      (it("should interpolate variables and then render templates", () => {
        let result = interpolateVariables("Repo: ${GH_AW_EXPR_REPO}\n{{#if true}}\nShow this\n{{/if}}", { GH_AW_EXPR_REPO: "github/test-repo" });
        (expect(result).toBe("Repo: github/test-repo\n{{#if true}}\nShow this\n{{/if}}"), (result = renderMarkdownTemplate(result)), expect(result).toBe("Repo: github/test-repo\nShow this\n"));
      }),
        it("should handle template conditionals that depend on interpolated values", () => {
          let result = interpolateVariables("${GH_AW_EXPR_CONDITION}\n{{#if ${GH_AW_EXPR_CONDITION}}}\nShow this\n{{/if}}", { GH_AW_EXPR_CONDITION: "true" });
          (expect(result).toBe("true\n{{#if true}}\nShow this\n{{/if}}"), (result = renderMarkdownTemplate(result)), expect(result).toBe("true\nShow this\n"));
        }),
        it("should render github context prompt with aw_context fallbacks", () => {
          const githubContextTemplate = fs.readFileSync(path.join(__dirname, "../../../pkg/workflow/prompts/github_context_prompt.md"), "utf8");
          const issueExpr = "github.event.issue.number || (github.aw.context.item_type == 'issue' && github.aw.context.item_number)";
          const discussionExpr = "github.event.discussion.number || (github.aw.context.item_type == 'discussion' && github.aw.context.item_number)";
          const pullRequestExpr = "github.event.pull_request.number || (github.aw.context.item_type == 'pull_request' && github.aw.context.item_number)";
          const commentExpr = "github.event.comment.id || github.aw.context.comment_id";
          const renderWithValues = (conditionValues, expressionValues) => {
            const withEvaluatedConditions = githubContextTemplate.replace(/{{#if\s+([^}]+)}}/g, (_, conditionExpr) => `{{#if ${conditionValues[conditionExpr.trim()] || ""}}}`);
            const withEvaluatedExpressions = withEvaluatedConditions.replace(/\${{\s*(.*?)\s*}}/g, (_, expression) => expressionValues[expression.trim()] || "");
            return renderMarkdownTemplate(withEvaluatedExpressions);
          };
          const workflowDispatchValues = {
            "github.actor": "octocat",
            "github.repository": "github/gh-aw",
            "github.workspace": "/home/runner/work/gh-aw/gh-aw",
            [issueExpr]: "456",
            [discussionExpr]: "",
            [pullRequestExpr]: "",
            [commentExpr]: "999",
            "github.run_id": "111",
          };
          const workflowDispatchRendered = renderWithValues(workflowDispatchValues, workflowDispatchValues);
          expect(workflowDispatchRendered).toContain("- **issue-number**: #456");
          expect(workflowDispatchRendered).toContain("- **comment-id**: 999");
          expect(workflowDispatchRendered).not.toContain("discussion-number");
          expect(workflowDispatchRendered).not.toContain("pull-request-number");
          const repositoryDispatchValues = {
            "github.actor": "octocat",
            "github.repository": "github/gh-aw",
            "github.workspace": "/home/runner/work/gh-aw/gh-aw",
            [issueExpr]: "",
            [discussionExpr]: "",
            [pullRequestExpr]: "789",
            [commentExpr]: "31415",
            "github.run_id": "222",
          };
          const repositoryDispatchRendered = renderWithValues(repositoryDispatchValues, repositoryDispatchValues);
          expect(repositoryDispatchRendered).toContain("- **pull-request-number**: #789");
          expect(repositoryDispatchRendered).toContain("- **comment-id**: 31415");
          expect(repositoryDispatchRendered).not.toContain("issue-number");
          expect(repositoryDispatchRendered).not.toContain("discussion-number");
        }));
    }),
    describe("main function integration", () => {
      const { main } = require("./interpolate_prompt.cjs");
      let tmpDir, promptPath, originalEnv;
      /**
       * Apply the STEP 2.5 experiment condition substitution logic from main().
       * Reads GH_AW_EXPERIMENTS_* from process.env and substitutes experiments.name
       * references inside {{#if}} conditions, using a replacer function to prevent
       * special $ replacement patterns from corrupting the output.
       * @param {string} content
       * @returns {string}
       */
      function applyExperimentSubstitution(content) {
        for (const [key, value] of Object.entries(process.env)) {
          if (key.startsWith("GH_AW_EXPERIMENTS_")) {
            const experimentName = key.substring("GH_AW_EXPERIMENTS_".length).toLowerCase();
            const exprForm = `experiments.${experimentName}`;
            const conditionPattern = new RegExp(`(\\{\\{#if[^}]*?)${exprForm.replace(".", "\\.")}`, "gi");
            if (conditionPattern.test(content)) {
              conditionPattern.lastIndex = 0;
              content = content.replace(conditionPattern, (_, prefix) => prefix + (value || ""));
            }
          }
        }
        return content;
      }
      (beforeEach(() => {
        ((originalEnv = { ...process.env }),
          (tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "interpolate-test-"))),
          (promptPath = path.join(tmpDir, "prompt.txt")),
          (process.env.GH_AW_PROMPT = promptPath),
          core.info.mockClear(),
          core.setFailed.mockClear());
      }),
        afterEach(() => {
          (tmpDir && fs.existsSync(tmpDir) && fs.rmSync(tmpDir, { recursive: !0, force: !0 }),
            Object.keys(process.env).forEach(k => {
              if (!(k in originalEnv)) delete process.env[k];
            }),
            Object.assign(process.env, originalEnv));
        }),
        it("should fail when GH_AW_PROMPT is not set", () => {
          delete process.env.GH_AW_PROMPT;
          const mainMatch = interpolatePromptScript.match(/async function main\(\)\s*{[\s\S]*?^}/m);
          if (!mainMatch) throw new Error("Could not extract main function");
          const main = eval(`(${mainMatch[0]})`);
          (main(), expect(core.setFailed).toHaveBeenCalledWith(`${ERR_CONFIG}: GH_AW_PROMPT environment variable is not set`));
        }),
        it("should fail when split prompt files do not match the combined prompt", async () => {
          fs.writeFileSync(path.join(tmpDir, "system.txt"), "System");
          fs.writeFileSync(path.join(tmpDir, "user.txt"), "User");
          fs.writeFileSync(promptPath, "Different");

          await main();

          expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("Split prompt files do not match the combined prompt"));
          expect(fs.readFileSync(promptPath, "utf8")).toBe("Different");
        }),
        it("should fail when template rendering removes the split boundary", async () => {
          const system = "{{#if false}}\nHidden\n";
          const user = "{{/if}}\nVisible\n";
          fs.writeFileSync(path.join(tmpDir, "system.txt"), system);
          fs.writeFileSync(path.join(tmpDir, "user.txt"), user);
          fs.writeFileSync(promptPath, system + user);

          await main();

          expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("Prompt split boundary was lost during processing"));
          expect(fs.readFileSync(promptPath, "utf8")).toBe(system + user);
        }),
        it("should not corrupt condition when experiment value contains $1 (STEP 2.5)", () => {
          // Regression: GH_AW_EXPERIMENTS_* value containing $1 must not be interpreted as a
          // capture-group reference when substituting experiments.name inside {{#if}} conditions.
          process.env.GH_AW_EXPERIMENTS_STYLE = "$1bold";
          const content = applyExperimentSubstitution("{{#if experiments.style}}\nSelected\n{{#else}}\nNot selected\n{{#endif}}");
          const result = renderMarkdownTemplate(content);
          expect(result).toContain("Selected");
          expect(result).not.toContain("Not selected");
        }),
        it("should not corrupt condition when experiment value contains $& (STEP 2.5)", () => {
          // Regression: GH_AW_EXPERIMENTS_* value containing $& must not be interpreted as the
          // matched-substring pattern when substituting inside {{#if}} conditions.
          process.env.GH_AW_EXPERIMENTS_STYLE = "$&matched";
          const content = applyExperimentSubstitution("{{#if experiments.style}}\nSelected\n{{#else}}\nNot selected\n{{#endif}}");
          const result = renderMarkdownTemplate(content);
          expect(result).toContain("Selected");
          expect(result).not.toContain("Not selected");
        }));
    }),
    describe("prompt-template.txt and prompt-import-tree.json artifacts", () => {
      const { main } = require("./interpolate_prompt.cjs");
      let tmpDir, promptPath, originalEnv;
      (beforeEach(() => {
        ((originalEnv = { ...process.env }),
          (tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "interpolate-artifact-test-"))),
          (promptPath = path.join(tmpDir, "prompt.txt")),
          (process.env.GH_AW_PROMPT = promptPath),
          (process.env.GITHUB_WORKSPACE = tmpDir),
          core.info.mockClear(),
          core.setFailed.mockClear());
      }),
        afterEach(() => {
          (tmpDir && fs.existsSync(tmpDir) && fs.rmSync(tmpDir, { recursive: !0, force: !0 }),
            Object.keys(process.env).forEach(k => {
              if (!(k in originalEnv)) delete process.env[k];
            }),
            Object.assign(process.env, originalEnv));
        }),
        it("should write prompt-template.txt with the original content before interpolation", async () => {
          const templateContent = "# My Workflow\n\nProcess: ${GH_AW_EXPR_ISSUE}\n\nDo the work.";
          fs.writeFileSync(promptPath, templateContent, "utf8");
          process.env.GH_AW_EXPR_ISSUE = "123";

          await main();

          const templatePath = path.join(tmpDir, "prompt-template.txt");
          expect(fs.existsSync(templatePath)).toBe(true);
          // Template file should contain the original (non-interpolated) content
          expect(fs.readFileSync(templatePath, "utf8")).toBe(templateContent);
          // The prompt.txt should have the interpolated value
          expect(fs.readFileSync(promptPath, "utf8")).toContain("Process: 123");
        }),
        it("keeps split prompts synchronized through imports, rendering, and placeholder substitution", async () => {
          const systemPath = path.join(tmpDir, "system.txt");
          const userPath = path.join(tmpDir, "user.txt");
          const system = "<system>\nRepo: ${GH_AW_EXPR_REPO}\n__ACTOR__\n</system>\n";
          const user = "Task:\n{{#runtime-import .github/workflows/helper.md}}\n{{#if false}}\nHidden\n{{/if}}\n__ACTOR__\n";
          fs.mkdirSync(path.join(tmpDir, ".github", "workflows"), { recursive: true });
          fs.writeFileSync(path.join(tmpDir, ".github", "workflows", "helper.md"), "Imported task");
          fs.writeFileSync(systemPath, system);
          fs.writeFileSync(userPath, user);
          fs.writeFileSync(promptPath, system + user);
          process.env.GH_AW_EXPR_REPO = "github/gh-aw";

          await main();
          expect(core.setFailed).not.toHaveBeenCalled();
          expect(fs.readFileSync(path.join(tmpDir, "prompt-template.txt"), "utf8")).toBe(system + user);
          const importTree = JSON.parse(fs.readFileSync(path.join(tmpDir, "prompt-import-tree.json"), "utf8"));
          expect(importTree.template).toBe(system + user);
          expect(importTree.template).not.toContain("\x00GH_AW_PROMPT_SPLIT_");
          const substitutePlaceholders = require("./substitute_placeholders.cjs");
          await substitutePlaceholders({ file: promptPath, substitutions: { ACTOR: "developer" } });

          const finalSystem = fs.readFileSync(systemPath, "utf8");
          const finalUser = fs.readFileSync(userPath, "utf8");
          expect(finalSystem).toBe("<system>\nRepo: github/gh-aw\ndeveloper\n</system>\n");
          expect(finalUser).toContain("Imported task");
          expect(finalUser).not.toContain("runtime-import");
          expect(finalUser).not.toContain("Hidden");
          expect(finalUser).toContain("developer");
          expect(fs.readFileSync(promptPath, "utf8")).toBe(finalSystem + finalUser);
        }),
        it("extracts inline agent and skill headings at the start of the split user prompt", async () => {
          const systemPath = path.join(tmpDir, "system.txt");
          const userPath = path.join(tmpDir, "user.txt");
          const system = "<system>\nRules\n</system>";
          const user = "## agent: `helper`\nAgent body\n## end agent: `helper`\n## skill: `tool`\nSkill body\n## end skill: `tool`\n{{#if false}}\nHidden\n{{/if}}\nTask\n";
          fs.writeFileSync(systemPath, system);
          fs.writeFileSync(userPath, user);
          fs.writeFileSync(promptPath, system + user);
          const realWriteFileSync = fs.writeFileSync;
          const writeSpy = vi.spyOn(fs, "writeFileSync").mockImplementation((file, ...args) => {
            if (String(file).startsWith(tmpDir)) realWriteFileSync(file, ...args);
          });

          await main();

          expect(core.setFailed).not.toHaveBeenCalled();
          const writtenPaths = writeSpy.mock.calls.map(call => String(call[0]));
          writeSpy.mockRestore();
          expect(writtenPaths.some(p => p.includes("helper") && !p.startsWith(tmpDir))).toBe(true);
          expect(writtenPaths.some(p => p.includes("tool") && !p.startsWith(tmpDir))).toBe(true);
          const finalSystem = fs.readFileSync(systemPath, "utf8");
          const finalUser = fs.readFileSync(userPath, "utf8");
          expect(finalSystem).toBe(system);
          expect(finalUser).not.toContain("Agent body");
          expect(finalUser).not.toContain("Skill body");
          expect(finalUser).not.toContain("Hidden");
          expect(finalUser).toContain("Task");
          expect(fs.readFileSync(promptPath, "utf8")).toBe(finalSystem + finalUser);
        }),
        it("should write prompt-import-tree.json with version and empty children when no imports", async () => {
          const templateContent = "Hello World";
          fs.writeFileSync(promptPath, templateContent, "utf8");

          await main();

          const treePath = path.join(tmpDir, "prompt-import-tree.json");
          expect(fs.existsSync(treePath)).toBe(true);
          const tree = JSON.parse(fs.readFileSync(treePath, "utf8"));
          expect(tree.version).toBe(1);
          expect(tree.template).toBe(templateContent);
          expect(tree.children).toEqual([]);
        }),
        it("should populate import tree children with file import provenance", async () => {
          const githubWorkflowsDir = path.join(tmpDir, ".github", "workflows");
          fs.mkdirSync(githubWorkflowsDir, { recursive: true });
          fs.writeFileSync(path.join(githubWorkflowsDir, "helper.md"), "Helper content");

          const templateContent = "Before\n{{#runtime-import .github/workflows/helper.md}}\nAfter";
          fs.writeFileSync(promptPath, templateContent, "utf8");

          await main();

          const treePath = path.join(tmpDir, "prompt-import-tree.json");
          const tree = JSON.parse(fs.readFileSync(treePath, "utf8"));

          expect(tree.version).toBe(1);
          expect(tree.template).toBe(templateContent);
          expect(tree.children).toHaveLength(1);
          expect(tree.children[0].macro).toBe("{{#runtime-import .github/workflows/helper.md}}");
          expect(tree.children[0].src).toContain("helper.md");
          expect(tree.children[0].rawContent).toBe("Helper content");
          expect(tree.children[0].optional).toBe(false);
          expect(tree.children[0].children).toEqual([]);
        }),
        it("should capture nested import provenance in the tree", async () => {
          const githubWorkflowsDir = path.join(tmpDir, ".github", "workflows");
          fs.mkdirSync(githubWorkflowsDir, { recursive: true });
          fs.writeFileSync(path.join(githubWorkflowsDir, "leaf.md"), "Leaf content");
          fs.writeFileSync(path.join(githubWorkflowsDir, "parent.md"), "Parent\n{{#runtime-import .github/workflows/leaf.md}}");

          const templateContent = "{{#runtime-import .github/workflows/parent.md}}";
          fs.writeFileSync(promptPath, templateContent, "utf8");

          await main();

          const treePath = path.join(tmpDir, "prompt-import-tree.json");
          const tree = JSON.parse(fs.readFileSync(treePath, "utf8"));

          expect(tree.children).toHaveLength(1);
          const parentNode = tree.children[0];
          expect(parentNode.rawContent).toBe("Parent\n{{#runtime-import .github/workflows/leaf.md}}");
          expect(parentNode.children).toHaveLength(1);
          expect(parentNode.children[0].rawContent).toBe("Leaf content");
        }));
    }));
});
