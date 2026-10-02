import { afterEach, beforeEach, describe, expect, it } from "vitest";

const fs = require("fs");
const os = require("os");
const path = require("path");
const substitutePlaceholders = require("./substitute_placeholders.cjs");

describe("substitutePlaceholders", () => {
  /** @type {string} */
  let tempDir;
  /** @type {string} */
  let testFile;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "substitute-test-"));
    testFile = path.join(tempDir, "test.txt");
  });

  afterEach(() => {
    if (fs.existsSync(testFile)) fs.unlinkSync(testFile);
    if (fs.existsSync(tempDir)) fs.rmdirSync(tempDir);
  });

  it("should substitute a single placeholder", async () => {
    fs.writeFileSync(testFile, "Hello __NAME__!", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { NAME: "World" } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Hello World!");
  });

  it("should substitute multiple placeholders", async () => {
    fs.writeFileSync(testFile, "Repository: __REPO__\nActor: __ACTOR__\nBranch: __BRANCH__", "utf8");
    await substitutePlaceholders({
      file: testFile,
      substitutions: { REPO: "test/repo", ACTOR: "testuser", BRANCH: "main" },
    });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Repository: test/repo\nActor: testuser\nBranch: main");
  });

  it("should handle special characters safely", async () => {
    fs.writeFileSync(testFile, "Command: __CMD__", "utf8");
    await substitutePlaceholders({
      file: testFile,
      substitutions: { CMD: "$(malicious) `backdoor` ${VAR} | pipe" },
    });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Command: $(malicious) `backdoor` ${VAR} | pipe");
  });

  it("should handle placeholders appearing multiple times", async () => {
    fs.writeFileSync(testFile, "__NAME__ is great. I love __NAME__!", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { NAME: "Testing" } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Testing is great. I love Testing!");
  });

  it("should leave unmatched placeholders unchanged", async () => {
    fs.writeFileSync(testFile, "__FOO__ and __BAR__", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { FOO: "foo" } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("foo and __BAR__");
  });

  it("should handle empty values", async () => {
    fs.writeFileSync(testFile, "Value: __VAL__", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { VAL: "" } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Value: ");
  });

  it("should throw error if file parameter is missing", async () => {
    // @ts-expect-error - testing missing file param
    await expect(substitutePlaceholders({ substitutions: { NAME: "test" } })).rejects.toThrow("ERR_VALIDATION: file parameter is required");
  });

  it("should throw error if substitutions parameter is missing", async () => {
    // @ts-expect-error - testing missing substitutions param
    await expect(substitutePlaceholders({ file: testFile })).rejects.toThrow("ERR_VALIDATION: substitutions parameter must be an object");
  });

  it("should throw error if file does not exist", async () => {
    await expect(substitutePlaceholders({ file: "/nonexistent/file.txt", substitutions: { NAME: "test" } })).rejects.toThrow("ERR_SYSTEM: Failed to read file");
  });

  it("should handle undefined values as empty strings", async () => {
    fs.writeFileSync(testFile, "Value: __VAL__", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { VAL: undefined } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Value: ");
  });

  it("should handle null values as empty strings", async () => {
    fs.writeFileSync(testFile, "Value: __VAL__", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { VAL: null } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Value: ");
  });

  it("should handle mixed undefined and defined values", async () => {
    fs.writeFileSync(testFile, "Repo: __REPO__\nComment: __COMMENT__\nIssue: __ISSUE__", "utf8");
    await substitutePlaceholders({
      file: testFile,
      substitutions: { REPO: "test/repo", COMMENT: undefined, ISSUE: null },
    });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Repo: test/repo\nComment: \nIssue: ");
  });

  it("preserves legacy prompt substitution when split files are absent", async () => {
    const promptPath = path.join(tempDir, "prompt.txt");
    fs.writeFileSync(promptPath, "__ACTOR__");
    await substitutePlaceholders({ file: promptPath, substitutions: { ACTOR: "developer" } });
    expect(fs.readFileSync(promptPath, "utf8")).toBe("developer");
    fs.unlinkSync(promptPath);
  });
});
