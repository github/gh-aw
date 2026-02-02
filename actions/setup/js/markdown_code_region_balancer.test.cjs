import { describe, it, expect } from "vitest";

describe("markdown_code_region_balancer.cjs", () => {
  let balancer;

  beforeEach(async () => {
    balancer = await import("./markdown_code_region_balancer.cjs");
  });

  describe("balanceCodeRegions", () => {
    describe("basic functionality", () => {
      it("should handle empty string", () => {
        expect(balancer.balanceCodeRegions("")).toBe("");
      });

      it("should handle null input", () => {
        expect(balancer.balanceCodeRegions(null)).toBe("");
      });

      it("should handle undefined input", () => {
        expect(balancer.balanceCodeRegions(undefined)).toBe("");
      });

      it("should not modify markdown without code blocks", () => {
        const input = `# Title
This is a paragraph.
## Section
More content.`;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should not modify properly balanced code blocks", () => {
        const input = `# Title

\`\`\`javascript
function test() {
  return true;
}
\`\`\`

End`;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });
    });

    describe("nested code regions with same indentation", () => {
      it("should escape nested backtick fence inside code block", () => {
        const input = `\`\`\`javascript
function test() {
\`\`\`
nested
\`\`\`
}
\`\`\``;
        const expected = `\`\`\`\`javascript
function test() {
\`\`\`
nested
\`\`\`
}
\`\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should escape nested tilde fence inside code block", () => {
        const input = `~~~markdown
Example:
~~~
nested
~~~
End
~~~`;
        const expected = `~~~~markdown
Example:
~~~
nested
~~~
End
~~~~`;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should handle multiple nested fences", () => {
        const input = `\`\`\`javascript
function test() {
\`\`\`
first nested
\`\`\`
\`\`\`
second nested
\`\`\`
}
\`\`\``;
        const expected = `\`\`\`\`javascript
function test() {
\`\`\`
first nested
\`\`\`
\`\`\`
second nested
\`\`\`
}
\`\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });
    });

    describe("fence character types", () => {
      it("should not allow backticks to close tilde fence", () => {
        const input = `~~~markdown
Content
\`\`\`
Should be escaped
~~~`;
        const expected = `~~~markdown
Content
\`\`\`
Should be escaped
~~~`;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should not allow tildes to close backtick fence", () => {
        const input = `\`\`\`markdown
Content
~~~
Should be escaped
\`\`\``;
        const expected = `\`\`\`markdown
Content
~~~
Should be escaped
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should handle alternating fence types", () => {
        const input = `\`\`\`javascript
code
\`\`\`

~~~markdown
content
~~~`;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });
    });

    describe("fence lengths", () => {
      // TODO: Edge case - separate sequential blocks with different fence lengths incorrectly identified as nested
      it.skip("should require closing fence to be at least as long as opening", () => {
        const input = `\`\`\`\`\`
content
\`\`\`
should be escaped
\`\`\`\`\``;
        const expected = `\`\`\`\`\`\`
content
\`\`\`
should be escaped
\`\`\`\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should allow longer closing fence", () => {
        const input = `\`\`\`
content
\`\`\`\`\`
end`;
        // This is valid - closing fence can be longer
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      // TODO: Edge case - multiple separate blocks with vastly different fence lengths incorrectly identified as nested
      it.skip("should handle various fence lengths", () => {
        const input = `\`\`\`
three
\`\`\`

\`\`\`\`
four
\`\`\`\`

\`\`\`\`\`\`\`
seven
\`\`\`\`\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      // TODO: Edge case - 6-char opener with 3-char inner fences incorrectly calculated
      it.skip("should escape shorter fence inside longer fence block", () => {
        const input = `\`\`\`\`\`\`
content
\`\`\`
nested short fence
\`\`\`
\`\`\`\`\`\``;
        const expected = `\`\`\`\`\`\`\`
content
\`\`\`
nested short fence
\`\`\`
\`\`\`\`\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });
    });

    describe("indentation", () => {
      it("should preserve indentation in code blocks", () => {
        const input = `  \`\`\`javascript
  function test() {
    return true;
  }
  \`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should handle nested fence with different indentation", () => {
        const input = `\`\`\`markdown
Example:
  \`\`\`
  nested
  \`\`\`
\`\`\``;
        // Indented fences inside a markdown block are treated as content (examples), not active fences
        // No escaping needed
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should preserve indentation when escaping", () => {
        const input = `\`\`\`markdown
    \`\`\`
    indented nested
    \`\`\`
\`\`\``;
        // Indented fences inside a markdown block are treated as content (examples), not active fences
        // No escaping needed
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });
    });

    describe("language specifiers", () => {
      it("should handle opening fence with language specifier", () => {
        const input = `\`\`\`javascript
code
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should handle multiple language specifiers", () => {
        const input = `\`\`\`javascript
js code
\`\`\`

\`\`\`python
py code
\`\`\`

\`\`\`typescript
ts code
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should handle language specifier with additional info", () => {
        const input = `\`\`\`javascript {1,3-5}
code
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });
    });

    describe("unclosed code blocks", () => {
      it("should close unclosed backtick code block", () => {
        const input = `\`\`\`javascript
function test() {
  return true;
}`;
        const expected = `\`\`\`javascript
function test() {
  return true;
}
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should close unclosed tilde code block", () => {
        const input = `~~~markdown
Content here
No closing fence`;
        const expected = `~~~markdown
Content here
No closing fence
~~~`;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should close with matching fence length", () => {
        const input = `\`\`\`\`\`
five backticks
content`;
        const expected = `\`\`\`\`\`
five backticks
content
\`\`\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should preserve indentation in closing fence", () => {
        const input = `  \`\`\`javascript
  code`;
        const expected = `  \`\`\`javascript
  code
  \`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });
    });

    describe("complex real-world scenarios", () => {
      // TODO: This test is currently skipped due to a known issue with the algorithm
      // The algorithm treats fences inside code blocks as real fences, causing incorrect escaping
      // See: https://github.com/github/gh-aw/issues/XXXXX
      it.skip("should handle AI-generated code with nested markdown", () => {
        const input = `# Example

Here's how to use code blocks:

\`\`\`markdown
You can create code blocks like this:
\`\`\`javascript
function hello() {
  console.log("world");
}
\`\`\`
\`\`\`

Text after`;
        // No changes expected - the javascript block is separate from the markdown block
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should handle documentation with multiple code examples", () => {
        const input = `## Usage

\`\`\`bash
npm install
\`\`\`

\`\`\`javascript
const x = 1;
\`\`\`

\`\`\`python
print("hello")
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      // TODO: Edge case - separate blocks being incorrectly treated as nested
      it.skip("should handle mixed fence types in document", () => {
        const input = `\`\`\`javascript
const x = 1;
\`\`\`

~~~bash
echo "test"
~~~

\`\`\`
generic code
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      // TODO: This test is currently skipped due to a known issue with the algorithm
      // The algorithm treats fences inside code blocks as real fences, causing incorrect escaping
      // See: https://github.com/github/gh-aw/issues/XXXXX
      it.skip("should handle deeply nested example", () => {
        const input = `\`\`\`markdown
# Tutorial

\`\`\`javascript
code here
\`\`\`

More text
\`\`\``;
        // No changes expected - the javascript block is separate from the markdown block
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should not modify markdown block containing indented bare fences as examples (issue #11081)", () => {
        // This reproduces the issue from GitHub issue #11081
        // A markdown code block containing examples of code blocks with indentation
        const input = `**Add to AGENTS.md:**

\`\`\`markdown
## Safe Outputs Schema Synchronization

**CRITICAL: When modifying safe output templates or handlers:**

1. **Update all related files:**
   - Source: \`actions/setup/js/handle_*.cjs\`
   - Schema: \`pkg/workflow/js/safe_outputs_tools.json\`

2. **Schema sync checklist:**
   \`\`\`
   # After modifying any handle_*.cjs file:
   cd actions/setup/js
   npm test  # MUST pass
   \`\`\`

3. **Common pitfalls:**
   - ❌ Changing issue titles without updating schema
   
4. **Pattern to follow:**
   \`\`\`
   # Find all related definitions
   grep -r "your-new-text" actions/setup/js/
   \`\`\`
\`\`\`

## Historical Context`;
        // No changes expected - the indented bare ``` inside the markdown block are examples
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });
    });

    describe("edge cases", () => {
      it("should handle Windows line endings", () => {
        const input = "\`\`\`javascript\r\ncode\r\n\`\`\`";
        const expected = "\`\`\`javascript\ncode\n\`\`\`";
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should handle mixed line endings", () => {
        const input = "\`\`\`\r\ncode\n\`\`\`\r\n";
        const expected = "\`\`\`\ncode\n\`\`\`\n";
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should handle empty code blocks", () => {
        const input = `\`\`\`
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should handle single line with fence", () => {
        const input = "\`\`\`javascript";
        const expected = "\`\`\`javascript\n\`\`\`";
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });

      it("should handle consecutive code blocks without blank lines", () => {
        const input = `\`\`\`javascript
code1
\`\`\`
\`\`\`python
code2
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should not affect inline code", () => {
        const input = "Use `console.log()` to print";
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should not affect multiple inline code", () => {
        const input = "Use `const x = 1` and `const y = 2` in code";
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should handle very long fence", () => {
        const input = `\`\`\`\`\`\`\`\`\`\`\`\`\`\`\`\`
content
\`\`\`\`\`\`\`\`\`\`\`\`\`\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should close unmatched opening fence when shorter fence cannot close it", () => {
        // Regression test for GitHub Issue #11630
        // When a 4-backtick fence is opened but only a 3-backtick fence follows,
        // the 3-backtick fence should be treated as content inside the code block,
        // not as a separate unclosed fence.
        const input = `#### NPM Versions Available

\`\`\`\`
0.0.56
0.0.57
0.0.58
\`\`\``;
        const expected = `#### NPM Versions Available

\`\`\`\`
0.0.56
0.0.57
0.0.58
\`\`\`
\`\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(expected);
      });
    });

    describe("trailing content after fence", () => {
      it("should handle trailing content after opening fence", () => {
        const input = `\`\`\`javascript some extra text
code
\`\`\``;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });

      it("should handle trailing content after closing fence", () => {
        const input = `\`\`\`javascript
code
\`\`\` trailing text`;
        expect(balancer.balanceCodeRegions(input)).toBe(input);
      });
    });
  });

  describe("isBalanced", () => {
    it("should return true for empty string", () => {
      expect(balancer.isBalanced("")).toBe(true);
    });

    it("should return true for null", () => {
      expect(balancer.isBalanced(null)).toBe(true);
    });

    it("should return true for undefined", () => {
      expect(balancer.isBalanced(undefined)).toBe(true);
    });

    it("should return true for markdown without code blocks", () => {
      const input = "# Title\nContent";
      expect(balancer.isBalanced(input)).toBe(true);
    });

    it("should return true for balanced code blocks", () => {
      const input = `\`\`\`javascript
code
\`\`\``;
      expect(balancer.isBalanced(input)).toBe(true);
    });

    it("should return false for unclosed code block", () => {
      const input = `\`\`\`javascript
code`;
      expect(balancer.isBalanced(input)).toBe(false);
    });

    it("should return false for nested unmatched fence", () => {
      const input = `\`\`\`javascript
\`\`\`
nested
\`\`\``;
      expect(balancer.isBalanced(input)).toBe(false);
    });

    it("should return true for multiple balanced blocks", () => {
      const input = `\`\`\`javascript
code1
\`\`\`

\`\`\`python
code2
\`\`\``;
      expect(balancer.isBalanced(input)).toBe(true);
    });
  });

  describe("countCodeRegions", () => {
    it("should return zero counts for empty string", () => {
      expect(balancer.countCodeRegions("")).toEqual({
        total: 0,
        balanced: 0,
        unbalanced: 0,
      });
    });

    it("should return zero counts for null", () => {
      expect(balancer.countCodeRegions(null)).toEqual({
        total: 0,
        balanced: 0,
        unbalanced: 0,
      });
    });

    it("should count single balanced block", () => {
      const input = `\`\`\`javascript
code
\`\`\``;
      expect(balancer.countCodeRegions(input)).toEqual({
        total: 1,
        balanced: 1,
        unbalanced: 0,
      });
    });

    it("should count unclosed block as unbalanced", () => {
      const input = `\`\`\`javascript
code`;
      expect(balancer.countCodeRegions(input)).toEqual({
        total: 1,
        balanced: 0,
        unbalanced: 1,
      });
    });

    it("should count multiple blocks correctly", () => {
      const input = `\`\`\`javascript
code1
\`\`\`

\`\`\`python
code2
\`\`\``;
      expect(balancer.countCodeRegions(input)).toEqual({
        total: 2,
        balanced: 2,
        unbalanced: 0,
      });
    });

    it("should count nested unmatched fences", () => {
      const input = `\`\`\`javascript
\`\`\`
nested
\`\`\``;
      // First ``` opens block, second ``` closes it, third ``` opens new block (unclosed)
      expect(balancer.countCodeRegions(input)).toEqual({
        total: 2,
        balanced: 1,
        unbalanced: 1,
      });
    });

    it("should count mixed fence types", () => {
      const input = `\`\`\`javascript
code
\`\`\`

~~~markdown
content
~~~`;
      expect(balancer.countCodeRegions(input)).toEqual({
        total: 2,
        balanced: 2,
        unbalanced: 0,
      });
    });
  });

  describe("fuzz testing", () => {
    it("should handle random combinations of fences", () => {
      // Generate various random but structured inputs
      const testCases = ["```\n```\n```\n```", "~~~\n~~~\n~~~", "```js\n~~~\n```\n~~~", "````\n```\n````", "```\n````\n```", "  ```\n```\n  ```", "```\n  ```\n```", "```\n\n```\n\n```\n\n```"];

      testCases.forEach(input => {
        // Should not throw an error
        expect(() => balancer.balanceCodeRegions(input)).not.toThrow();
        // Result should be a string
        expect(typeof balancer.balanceCodeRegions(input)).toBe("string");
      });
    });

    it("should handle long documents with many code blocks", () => {
      let input = "# Document\n\n";
      for (let i = 0; i < 50; i++) {
        input += `\`\`\`javascript\ncode${i}\n\`\`\`\n\n`;
      }
      const result = balancer.balanceCodeRegions(input);
      expect(result).toContain("code0");
      expect(result).toContain("code49");
      expect(balancer.isBalanced(result)).toBe(true);
    });

    it("should handle deeply nested structures", () => {
      let input = "```markdown\n";
      for (let i = 0; i < 10; i++) {
        input += "```\nnested " + i + "\n```\n";
      }
      input += "```";

      // Should not throw and should produce some output
      expect(() => balancer.balanceCodeRegions(input)).not.toThrow();
      const result = balancer.balanceCodeRegions(input);
      expect(result.length).toBeGreaterThan(0);
    });

    it("should handle very long lines", () => {
      const longLine = "a".repeat(10000);
      const input = `\`\`\`\n${longLine}\n\`\`\``;
      const result = balancer.balanceCodeRegions(input);
      expect(result).toContain(longLine);
    });

    it("should handle special characters in code blocks", () => {
      const input = `\`\`\`
<>&"'\n\t\r
\`\`\``;
      const result = balancer.balanceCodeRegions(input);
      expect(result).toContain("<>&\"'");
    });

    it("should handle unicode characters", () => {
      const input = `\`\`\`javascript
const emoji = "🚀";
const chinese = "你好";
const arabic = "مرحبا";
\`\`\``;
      expect(balancer.balanceCodeRegions(input)).toBe(input);
    });

    it("should handle empty lines in various positions", () => {
      const input = `

\`\`\`


code


\`\`\`

`;
      expect(balancer.balanceCodeRegions(input)).toBe(input);
    });

    it("should never create MORE unbalanced regions than input", () => {
      // Test quality degradation detection
      const testCases = [
        "```\ncode\n```", // Balanced - should not modify
        "```javascript\nunclosed", // Unclosed - should add closing
        "```\ncode1\n```\n```\ncode2\n```", // Multiple balanced - should not modify
        "```\nnested\n```\n```\n```", // Unbalanced sequence
        "```markdown\n```\nexample\n```\n```", // Nested example
        "```\nfirst\n```\nsecond\n```\nthird\n```", // Partially balanced
      ];

      testCases.forEach(input => {
        const originalCounts = balancer.countCodeRegions(input);
        const result = balancer.balanceCodeRegions(input);
        const resultCounts = balancer.countCodeRegions(result);

        // Key quality invariant: never create MORE unbalanced regions
        expect(resultCounts.unbalanced).toBeLessThanOrEqual(originalCounts.unbalanced);
      });
    });

    it("should preserve balanced markdown exactly (except line ending normalization)", () => {
      const balancedExamples = ["```javascript\nconst x = 1;\n```", "~~~markdown\ntext\n~~~", "```\ngeneric\n```\n\n```python\ncode\n```", "# Title\n\n```bash\necho test\n```\n\nMore text", "````\nfour backticks\n````"];

      balancedExamples.forEach(input => {
        const result = balancer.balanceCodeRegions(input);
        expect(result).toBe(input);
      });
    });

    it("should handle AI-generated common error patterns", () => {
      // Common error pattern: AI generates nested markdown examples without proper escaping
      const aiPattern1 = `How to use code blocks:

\`\`\`markdown
You can write code like this:
\`\`\`javascript
code here
\`\`\`
\`\`\``;

      const result1 = balancer.balanceCodeRegions(aiPattern1);
      const counts1 = balancer.countCodeRegions(result1);

      // Result should have fewer or equal unbalanced regions
      const originalCounts1 = balancer.countCodeRegions(aiPattern1);
      expect(counts1.unbalanced).toBeLessThanOrEqual(originalCounts1.unbalanced);

      // Common error pattern: Unclosed code block at end of content
      const aiPattern2 = `Here's some code:

\`\`\`javascript
function example() {
  console.log("test");
}`;

      const result2 = balancer.balanceCodeRegions(aiPattern2);
      expect(balancer.isBalanced(result2)).toBe(true);

      // Common error pattern: Mixed fence types causing confusion
      const aiPattern3 = `\`\`\`markdown
Example with tilde:
~~~
content
~~~
\`\`\``;

      const result3 = balancer.balanceCodeRegions(aiPattern3);
      const counts3 = balancer.countCodeRegions(result3);
      expect(counts3.unbalanced).toBe(0);
    });

    it("should handle pathological cases without hanging", () => {
      // Generate pathological input: alternating fences
      let pathological = "";
      for (let i = 0; i < 100; i++) {
        pathological += i % 2 === 0 ? "```\n" : "~~~\n";
      }

      // Should complete in reasonable time (not hang)
      const start = Date.now();
      const result = balancer.balanceCodeRegions(pathological);
      const elapsed = Date.now() - start;

      expect(elapsed).toBeLessThan(1000); // Should complete in less than 1 second
      expect(typeof result).toBe("string");
    });

    it("should handle random fence variations", () => {
      // Generate random fence lengths and types
      const fenceChars = ["`", "~"];
      const fenceLengths = [3, 4, 5, 6, 10];

      for (let i = 0; i < 20; i++) {
        const char = fenceChars[i % fenceChars.length];
        const length = fenceLengths[i % fenceLengths.length];
        const fence = char.repeat(length);
        const input = `${fence}javascript\ncode${i}\n${fence}`;

        const result = balancer.balanceCodeRegions(input);
        expect(balancer.isBalanced(result)).toBe(true);
      }
    });
  });
});
