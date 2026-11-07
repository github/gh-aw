import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noPathStartsWithoutSeparatorRule } from "./no-path-startswith-without-separator";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("no-path-startswith-without-separator", () => {
  it("uses the correct docs URL", () => {
    expect(noPathStartsWithoutSeparatorRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-path-startswith-without-separator");
  });

  it("validates path containment checks", () => {
    ruleTester.run("no-path-startswith-without-separator", noPathStartsWithoutSeparatorRule, {
      valid: [
        `const root = path.resolve(base) + path.sep; if (!dest.startsWith(root)) {}`,
        `const root = path.resolve(base); if (dest.startsWith(root + path.sep)) {}`,
        "const root = path.resolve(base); if (dest.startsWith(`${root}${path.sep}`)) {}",
        `if (dest.startsWith(path.join(base, path.sep))) {}`,
        `if (dest.startsWith(path.normalize("/work/repo/"))) {}`,
        `const root = path.join(base, path.sep); dest.startsWith(root);`,
        `const root = path.normalize("/work/repo/"); dest.startsWith(root);`,
        `if (path.resolve(target).startsWith("/tmp/gh-aw/")) {}`,
        `function f(root) { return file.startsWith(root); }`,
        `const prefix = "github/gh-aw/"; s.startsWith(prefix);`,
        `let root = path.resolve(a); root = root + path.sep; dest.startsWith(root);`,
      ],
      invalid: [
        { code: `const root = path.resolve(base); if (!dest.startsWith(root)) {}`, errors: [{ messageId: "noBarePathPrefix" }] },
        { code: `if (dest.startsWith(path.resolve(base))) {}`, errors: [{ messageId: "noBarePathPrefix" }] },
        { code: `const root = path.join(a, "b"); dest.startsWith(root);`, errors: [{ messageId: "noBarePathPrefix" }] },
        { code: `const root = path.normalize(a); dest.startsWith(root);`, errors: [{ messageId: "noBarePathPrefix" }] },
        { code: `if (dest.startsWith(path.join(base, "child"))) {}`, errors: [{ messageId: "noBarePathPrefix" }] },
        { code: `const root = path.normalize("/work/repo"); dest.startsWith(root);`, errors: [{ messageId: "noBarePathPrefix" }] },
      ],
    });
  });
});
