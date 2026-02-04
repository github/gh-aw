import { describe, it, expect } from "vitest";

describe("markdown_unfencing.cjs", () => {
  let unfenceMarkdown;

  beforeEach(async () => {
    // Import the module
    const module = await import("./markdown_unfencing.cjs");
    unfenceMarkdown = module.unfenceMarkdown;
  });
  it("should unfence basic markdown fence with backticks", () => {
    const input = "```markdown\nThis is the content\n```";
    const expected = "This is the content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with md language tag", () => {
    const input = "```md\nThis is the content\n```";
    const expected = "This is the content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with tildes", () => {
    const input = "~~~markdown\nThis is the content\n~~~";
    const expected = "This is the content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with md and tildes", () => {
    const input = "~~~md\nThis is the content\n~~~";
    const expected = "This is the content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with no language tag", () => {
    const input = "```\nThis is the content\n```";
    const expected = "This is the content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with multiline content", () => {
    const input = "```markdown\nLine 1\nLine 2\nLine 3\n```";
    const expected = "Line 1\nLine 2\nLine 3";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with nested code blocks", () => {
    const input = '```markdown\nHere is some code:\n```javascript\nconsole.log("hello");\n```\n```';
    const expected = 'Here is some code:\n```javascript\nconsole.log("hello");\n```';
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with leading and trailing whitespace", () => {
    const input = "   ```markdown\nContent here\n```   ";
    const expected = "Content here";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should be case insensitive for MARKDOWN tag", () => {
    const input = "```MARKDOWN\nContent\n```";
    const expected = "Content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should be case insensitive for MD tag", () => {
    const input = "```MD\nContent\n```";
    const expected = "Content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should not unfence different language fences", () => {
    const input = '```javascript\nconsole.log("test");\n```';
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should not unfence when no closing fence", () => {
    const input = "```markdown\nThis has no closing fence";
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should not unfence with mismatched fence types", () => {
    const input = "```markdown\nContent\n~~~";
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should not unfence with content before opening fence", () => {
    const input = "Some text before\n```markdown\nContent\n```";
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should not unfence with content after closing fence", () => {
    const input = "```markdown\nContent\n```\nSome text after";
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should handle empty string", () => {
    expect(unfenceMarkdown("")).toBe("");
  });

  it("should handle only whitespace", () => {
    const input = "   \n\t\t\t\n\t\t\t";
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should handle single line", () => {
    const input = "```markdown";
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should unfence markdown fence with empty content", () => {
    const input = "```markdown\n```";
    const expected = "";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with only whitespace content", () => {
    const input = "```markdown\n   \n```";
    const expected = "";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with complex nested structures", () => {
    const input = '```markdown\n# Heading\n\nSome text with **bold** and *italic*.\n\n```python\ndef hello():\n    print("world")\n```\n\nMore text here.\n```';
    const expected = '# Heading\n\nSome text with **bold** and *italic*.\n\n```python\ndef hello():\n    print("world")\n```\n\nMore text here.';
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with special characters", () => {
    const input = "```markdown\nContent with ${{ github.actor }} and @mentions\n```";
    const expected = "Content with ${{ github.actor }} and @mentions";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence longer backtick fence", () => {
    const input = "````markdown\nContent\n````";
    const expected = "Content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence longer tilde fence", () => {
    const input = "~~~~markdown\nContent\n~~~~";
    const expected = "Content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should unfence markdown fence with extra spaces in language tag", () => {
    const input = "```  markdown  \nContent\n```";
    const expected = "Content";
    expect(unfenceMarkdown(input)).toBe(expected);
  });

  it("should preserve normal markdown with headers", () => {
    const input = "# Title\n\nSome content here.\n\n## Subtitle\n\nMore content.";
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should preserve markdown with multiple code blocks", () => {
    const input = "Some text\n\n```javascript\ncode1();\n```\n\nMore text\n\n```python\ncode2()\n```";
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should preserve markdown with inline code", () => {
    const input = "Use `code` for inline code snippets.";
    expect(unfenceMarkdown(input)).toBe(input);
  });

  it("should handle null input", () => {
    expect(unfenceMarkdown(null)).toBe(null);
  });

  it("should handle undefined input", () => {
    expect(unfenceMarkdown(undefined)).toBe(undefined);
  });

  it("should handle non-string input", () => {
    expect(unfenceMarkdown(123)).toBe(123);
    expect(unfenceMarkdown({})).toStrictEqual({});
  });
});
