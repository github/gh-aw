// @ts-check
/// <reference types="@actions/github-script" />

const { displayFileContent, displayDirectory, displayDirectories } = require("./display_file_helpers.cjs");

describe("display_file_helpers", () => {
  const fs = require("fs");
  const path = require("path");
  const os = require("os");

  describe("displayFileContent", () => {
    test("displays regular file with content", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const filePath = path.join(tmpDir, "test.txt");
      fs.writeFileSync(filePath, "Line 1\nLine 2\nLine 3");

      const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), warning: vi.fn() };
      global.core = mockCore;

      try {
        displayFileContent(filePath, "test.txt");

        // Check file info was displayed
        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("test.txt"));
        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("bytes"));

        // Check group was started with just the filename
        expect(mockCore.startGroup).toHaveBeenCalledWith("test.txt");

        // Check content was displayed
        expect(mockCore.info).toHaveBeenCalledWith("Line 1");
        expect(mockCore.info).toHaveBeenCalledWith("Line 2");
        expect(mockCore.info).toHaveBeenCalledWith("Line 3");

        // Check group was ended
        expect(mockCore.endGroup).toHaveBeenCalled();
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("displays empty file", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const filePath = path.join(tmpDir, "empty.txt");
      fs.writeFileSync(filePath, "");

      const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), warning: vi.fn() };
      global.core = mockCore;

      try {
        displayFileContent(filePath, "empty.txt");

        // Check file info was displayed
        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("empty.txt"));
        expect(mockCore.info).toHaveBeenCalledWith("    (empty file)");

        // Should not start group for empty file
        expect(mockCore.startGroup).not.toHaveBeenCalled();
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("displays directory indicator", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const subDir = path.join(tmpDir, "subdir");
      fs.mkdirSync(subDir);

      const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), warning: vi.fn() };
      global.core = mockCore;

      try {
        displayFileContent(subDir, "subdir");

        // Check directory indicator was displayed
        expect(mockCore.info).toHaveBeenCalledWith("  subdir/ (directory)");

        // Should not start group for directory
        expect(mockCore.startGroup).not.toHaveBeenCalled();
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("truncates large file at specified max bytes", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const filePath = path.join(tmpDir, "large.txt");
      const largeContent = "A".repeat(100 * 1024); // 100KB
      fs.writeFileSync(filePath, largeContent);

      const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), warning: vi.fn() };
      global.core = mockCore;

      try {
        displayFileContent(filePath, "large.txt", 50 * 1024); // 50KB max

        // Check truncation message was displayed
        const infoMessages = mockCore.info.mock.calls.map(call => call[0]).join("\n");
        expect(infoMessages).toContain("...");
        expect(infoMessages).toContain("truncated");
        expect(infoMessages).toContain("51200 bytes");
        expect(infoMessages).toContain("102400 total");
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("handles files too large to read (>1MB)", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const filePath = path.join(tmpDir, "huge.txt");

      try {
        // Create a file larger than 1MB by writing in chunks
        const fd = fs.openSync(filePath, "w");
        const chunkSize = 1024 * 100; // 100KB chunks
        const chunk = Buffer.alloc(chunkSize, "A");

        // Write 15 chunks = 1.5MB
        for (let i = 0; i < 15; i++) {
          fs.writeSync(fd, chunk);
        }
        fs.closeSync(fd);

        // Verify file size
        const stats = fs.statSync(filePath);
        expect(stats.size).toBeGreaterThan(1024 * 1024);

        const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), warning: vi.fn() };
        global.core = mockCore;

        displayFileContent(filePath, "huge.txt");

        // Check "too large" message was displayed
        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("file too large to display"));

        // Should not start group for too large file
        expect(mockCore.startGroup).not.toHaveBeenCalled();

        delete global.core;
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("handles file read errors", () => {
      const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), warning: vi.fn() };
      global.core = mockCore;

      try {
        displayFileContent("/nonexistent/file.txt", "file.txt");

        // Check warning was displayed
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not display file"));
      } finally {
        delete global.core;
      }
    });

    test("skips content display for unsupported file extensions", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const filePath = path.join(tmpDir, "test.md");
      fs.writeFileSync(filePath, "# Markdown content");

      const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), warning: vi.fn() };
      global.core = mockCore;

      try {
        displayFileContent(filePath, "test.md");

        // Check file info was displayed
        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("test.md"));
        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("bytes"));

        // Check message about not displaying content
        expect(mockCore.info).toHaveBeenCalledWith("    (content not displayed for .md files)");

        // Should not start group for unsupported file type
        expect(mockCore.startGroup).not.toHaveBeenCalled();

        // Content should not be displayed
        expect(mockCore.info).not.toHaveBeenCalledWith("# Markdown content");
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("displays content for .json files", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const filePath = path.join(tmpDir, "test.json");
      fs.writeFileSync(filePath, '{"key": "value"}');

      const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), warning: vi.fn() };
      global.core = mockCore;

      try {
        displayFileContent(filePath, "test.json");

        // Check group was started
        expect(mockCore.startGroup).toHaveBeenCalledWith("test.json");

        // Check content was displayed
        expect(mockCore.info).toHaveBeenCalledWith('{"key": "value"}');
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("displays content for .log files", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const filePath = path.join(tmpDir, "test.log");
      fs.writeFileSync(filePath, "Log entry 1\nLog entry 2");

      const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), warning: vi.fn() };
      global.core = mockCore;

      try {
        displayFileContent(filePath, "test.log");

        // Check group was started
        expect(mockCore.startGroup).toHaveBeenCalledWith("test.log");

        // Check content was displayed
        expect(mockCore.info).toHaveBeenCalledWith("Log entry 1");
        expect(mockCore.info).toHaveBeenCalledWith("Log entry 2");
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });
  });

  describe("displayDirectory", () => {
    test("displays all files in directory", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      fs.writeFileSync(path.join(tmpDir, "file1.txt"), "Content 1");
      fs.writeFileSync(path.join(tmpDir, "file2.txt"), "Content 2");

      const mockCore = {
        info: vi.fn(),
        startGroup: vi.fn(),
        endGroup: vi.fn(),
        warning: vi.fn(),
        notice: vi.fn(),
        error: vi.fn(),
      };
      global.core = mockCore;

      try {
        displayDirectory(tmpDir);

        // Check directory group was started
        expect(mockCore.startGroup).toHaveBeenCalledWith(expect.stringContaining(tmpDir));

        // Check both files were displayed
        const infoMessages = mockCore.info.mock.calls.map(call => call[0]).join("\n");
        expect(infoMessages).toContain("file1.txt");
        expect(infoMessages).toContain("file2.txt");

        // Check group was ended
        expect(mockCore.endGroup).toHaveBeenCalled();
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("handles non-existent directory", () => {
      const mockCore = {
        info: vi.fn(),
        startGroup: vi.fn(),
        endGroup: vi.fn(),
        warning: vi.fn(),
        notice: vi.fn(),
        error: vi.fn(),
      };
      global.core = mockCore;

      try {
        displayDirectory("/nonexistent/directory");

        // Check notice was displayed
        expect(mockCore.notice).toHaveBeenCalledWith(expect.stringContaining("Directory does not exist"));

        // Check group was still properly closed
        expect(mockCore.startGroup).toHaveBeenCalled();
        expect(mockCore.endGroup).toHaveBeenCalled();
      } finally {
        delete global.core;
      }
    });

    test("handles empty directory", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));

      const mockCore = {
        info: vi.fn(),
        startGroup: vi.fn(),
        endGroup: vi.fn(),
        warning: vi.fn(),
        notice: vi.fn(),
        error: vi.fn(),
      };
      global.core = mockCore;

      try {
        displayDirectory(tmpDir);

        // Check empty directory message was displayed
        expect(mockCore.info).toHaveBeenCalledWith("  (empty directory)");

        // Check group was properly closed
        expect(mockCore.startGroup).toHaveBeenCalled();
        expect(mockCore.endGroup).toHaveBeenCalled();
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });
  });

  describe("displayDirectories", () => {
    test("displays multiple directories", () => {
      const tmpDir1 = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const tmpDir2 = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      fs.writeFileSync(path.join(tmpDir1, "file1.txt"), "Content 1");
      fs.writeFileSync(path.join(tmpDir2, "file2.txt"), "Content 2");

      const mockCore = {
        info: vi.fn(),
        startGroup: vi.fn(),
        endGroup: vi.fn(),
        warning: vi.fn(),
        notice: vi.fn(),
        error: vi.fn(),
      };
      global.core = mockCore;

      try {
        displayDirectories([tmpDir1, tmpDir2]);

        // Check outer group was started
        expect(mockCore.startGroup).toHaveBeenCalledWith("=== Listing All Gateway-Related Files ===");

        // Check both directories were displayed
        const startGroupCalls = mockCore.startGroup.mock.calls.map(call => call[0]);
        expect(startGroupCalls.some(call => call.includes(tmpDir1))).toBe(true);
        expect(startGroupCalls.some(call => call.includes(tmpDir2))).toBe(true);
      } finally {
        delete global.core;
        fs.rmSync(tmpDir1, { recursive: true, force: true });
        fs.rmSync(tmpDir2, { recursive: true, force: true });
      }
    });

    test("respects custom maxBytes parameter", () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "display-test-"));
      const filePath = path.join(tmpDir, "test.txt");
      fs.writeFileSync(filePath, "A".repeat(10000)); // 10KB

      const mockCore = {
        info: vi.fn(),
        startGroup: vi.fn(),
        endGroup: vi.fn(),
        warning: vi.fn(),
        notice: vi.fn(),
        error: vi.fn(),
      };
      global.core = mockCore;

      try {
        displayDirectories([tmpDir], 5000); // 5KB max

        // Check truncation message appears
        const infoMessages = mockCore.info.mock.calls.map(call => call[0]).join("\n");
        expect(infoMessages).toContain("truncated");
        expect(infoMessages).toContain("5000 bytes");
      } finally {
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });
  });
});
