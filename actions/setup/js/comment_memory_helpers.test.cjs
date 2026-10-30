import fs from "fs";
import path from "path";
import { describe, it, expect, vi } from "vitest";
import { buildCodeFenceOpener, extractCommentMemoryEntries, isSafeMemoryId, listCommentMemoryFiles, stripCommentMemoryCodeFence } from "./comment_memory_helpers.cjs";
import { renderReport } from "./report_ast.cjs";

describe("comment_memory_helpers", () => {
  it("extracts a rendered memory tree without truncating embedded fences or marker examples", () => {
    const content = "before\n``````\ninside\n````````\n<!-- gh-aw-execution: literal -->\nafter";
    const body = renderReport({ type: "report", title: "Memory", children: [{ type: "code_block", language: "gh-aw-comment-memory:notes", fenceLength: 6, value: content, agentTainted: true }] }).body;
    expect(extractCommentMemoryEntries(body)).toEqual([{ memoryId: "notes", content }]);
  });

  it("builds code-fence opener with memory id", () => {
    expect(buildCodeFenceOpener("default")).toBe("``````gh-aw-comment-memory:default");
    expect(buildCodeFenceOpener("session-1")).toBe("``````gh-aw-comment-memory:session-1");
  });

  it("extracts managed memory entries from new code-fence format", () => {
    const entries = extractCommentMemoryEntries("``````gh-aw-comment-memory:default\nhello\n``````\n");
    expect(entries).toEqual([{ memoryId: "default", content: "hello" }]);
  });

  it("extracts adjacent memory blocks with different fence lengths without consuming the next opener", () => {
    const body = "``````gh-aw-comment-memory:one\nfirst\n``````\n```````gh-aw-comment-memory:two\nsecond\n```````\n``````gh-aw-comment-memory:three\nthird\n``````\n";
    expect(extractCommentMemoryEntries(body)).toEqual([
      { memoryId: "one", content: "first" },
      { memoryId: "two", content: "second" },
      { memoryId: "three", content: "third" },
    ]);
  });

  it("extracts multiple entries from new code-fence format", () => {
    const body = "``````gh-aw-comment-memory:notes\nfirst note\n``````\n\n``````gh-aw-comment-memory:session\nsecond note\n``````\n";
    const entries = extractCommentMemoryEntries(body);
    expect(entries).toEqual([
      { memoryId: "notes", content: "first note" },
      { memoryId: "session", content: "second note" },
    ]);
  });

  it("extracts managed memory entries from legacy xml format (backward compat)", () => {
    const entries = extractCommentMemoryEntries('<gh-aw-comment-memory id="default">\n``````\nhello\n``````\n</gh-aw-comment-memory>');
    expect(entries).toEqual([{ memoryId: "default", content: "hello" }]);
  });

  it("supports legacy memory entries without code fence markers", () => {
    const entries = extractCommentMemoryEntries('<gh-aw-comment-memory id="default">\nhello\n</gh-aw-comment-memory>');
    expect(entries).toEqual([{ memoryId: "default", content: "hello" }]);
  });

  it("prefers new code-fence format over legacy xml for same memory id", () => {
    const body = '``````gh-aw-comment-memory:default\nnew content\n``````\n<gh-aw-comment-memory id="default">\nold content\n</gh-aw-comment-memory>';
    const entries = extractCommentMemoryEntries(body);
    expect(entries).toEqual([{ memoryId: "default", content: "new content" }]);
  });

  it("rejects unsafe memory IDs in new code-fence format", () => {
    const warning = vi.fn();
    const entries = extractCommentMemoryEntries("``````gh-aw-comment-memory:../bad\nhello\n``````\n", warning);
    expect(entries).toEqual([]);
  });

  it("keeps fenced text unchanged when trailing content exists after closing fence", () => {
    const content = "``````\nhello\n``````\ntrailing";
    expect(stripCommentMemoryCodeFence(content)).toBe(content);
  });

  it("keeps fenced text unchanged when closing fence is missing", () => {
    const content = "``````\nhello";
    expect(stripCommentMemoryCodeFence(content)).toBe(content);
  });

  it("keeps malformed fenced text unchanged", () => {
    const content = "``````hello\n``````";
    expect(stripCommentMemoryCodeFence(content)).toBe(content);
  });

  it("strips valid fenced text with extra newlines before content", () => {
    const content = "``````\n\nhello\n``````";
    expect(stripCommentMemoryCodeFence(content)).toBe("hello");
  });

  it("strips valid fenced text when content contains six-backtick lines", () => {
    const content = "``````\nline 1\n``````\nline 2\n``````";
    expect(stripCommentMemoryCodeFence(content)).toBe("line 1\n``````\nline 2");
  });

  it("keeps fenced text unchanged when closing fence has no leading newline", () => {
    const content = "``````\nhello``````";
    expect(stripCommentMemoryCodeFence(content)).toBe(content);
  });

  it("rejects unsafe memory IDs in legacy xml format", () => {
    const warning = vi.fn();
    const entries = extractCommentMemoryEntries('<gh-aw-comment-memory id="../bad">\nhello\n</gh-aw-comment-memory>', warning);
    expect(entries).toEqual([]);
    expect(warning).toHaveBeenCalled();
    expect(isSafeMemoryId("../bad")).toBe(false);
  });

  it("allows memory IDs up to 128 characters", () => {
    const maxLengthId = "a".repeat(128);
    const tooLongId = "b".repeat(129);
    expect(isSafeMemoryId(maxLengthId)).toBe(true);
    expect(isSafeMemoryId(tooLongId)).toBe(false);
  });

  it("wraps directory read failures with the memory directory path", () => {
    const memoryDir = fs.mkdtempSync(path.join("/tmp", "comment-memory-"));
    const originalReaddirSync = fs.readdirSync;
    const readdirSpy = vi.spyOn(fs, "readdirSync").mockImplementation((dirPath, options) => {
      if (dirPath === memoryDir) {
        throw new Error("EACCES");
      }
      return originalReaddirSync.call(fs, dirPath, options);
    });

    try {
      expect(() => listCommentMemoryFiles(memoryDir)).toThrow(`Failed to read comment-memory directory ${memoryDir}: EACCES`);
    } finally {
      readdirSpy.mockRestore();
      fs.rmSync(memoryDir, { recursive: true, force: true });
    }
  });
});
