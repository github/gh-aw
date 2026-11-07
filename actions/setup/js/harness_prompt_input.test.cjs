import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const { resolveHarnessPromptFileArgs } = require("./harness_prompt_input.cjs");
const { resolveClaudePromptFileArgs } = require("./claude_harness.cjs");
const { resolveCodexPromptFileArgs, resolveCodexPromptInput } = require("./codex_harness.cjs");

let directory;
let promptPath;
let logger;

beforeEach(() => {
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "harness-prompt-input-"));
  promptPath = path.join(directory, "prompt with spaces.txt");
  logger = vi.fn();
});

afterEach(() => {
  fs.rmSync(directory, { recursive: true, force: true });
});

describe("resolveHarnessPromptFileArgs", () => {
  it("preserves argv order without modifying the input or logging prompt contents", () => {
    const prompt = "private instructions 世界\n";
    fs.writeFileSync(promptPath, prompt);
    const args = ["--print", "--prompt-file", promptPath, "--model", "model"];
    expect(resolveHarnessPromptFileArgs(args, { logger })).toEqual({ args: ["--print", "--model", "model"], prompt });
    expect(args).toEqual(["--print", "--prompt-file", promptPath, "--model", "model"]);
    expect(logger).toHaveBeenCalledWith(`resolved --prompt-file: path=${promptPath} size=${Buffer.byteLength(prompt)}B`);
    expect(logger.mock.calls.flat().join(" ")).not.toContain(prompt);
  });

  it("returns null only when no prompt file was supplied", () => {
    expect(resolveHarnessPromptFileArgs(["--print"], { logger })).toEqual({ args: ["--print"], prompt: null });
    expect(logger).not.toHaveBeenCalled();
  });

  it.each(["", "hello\0world", "世界\n".repeat(40000)])("preserves empty, binary and large text prompts", prompt => {
    fs.writeFileSync(promptPath, prompt);
    expect(resolveHarnessPromptFileArgs(["--prompt-file", promptPath], { logger })).toEqual({ args: [], prompt });
  });

  it("reads every supplied file and uses the last prompt, including an empty one", () => {
    const secondPath = path.join(directory, "second.txt");
    fs.writeFileSync(promptPath, "first");
    fs.writeFileSync(secondPath, "");
    expect(resolveHarnessPromptFileArgs(["--prompt-file", promptPath, "--print", "--prompt-file", secondPath], { logger })).toEqual({ args: ["--print"], prompt: "" });
    expect(logger).toHaveBeenCalledTimes(2);
  });

  it("rejects missing paths by default but preserves the legacy Codex option", () => {
    expect(() => resolveHarnessPromptFileArgs(["--prompt-file"], { logger })).toThrow("--prompt-file requires a readable file path");
    expect(resolveHarnessPromptFileArgs(["--print", "--prompt-file"], { logger, allowMissingPath: true })).toEqual({ args: ["--print", "--prompt-file"], prompt: null });
    expect(logger).toHaveBeenCalledWith("warning: --prompt-file provided without a path; leaving arguments unchanged");
  });

  it.each(["missing", "directory"])("rejects a %s file even in legacy mode and preserves the cause", kind => {
    const file = kind === "directory" ? directory : promptPath;
    let error;
    try {
      resolveHarnessPromptFileArgs(["--prompt-file", file], { logger, allowMissingPath: true });
    } catch (caught) {
      error = caught;
    }
    expect(error).toBeInstanceOf(Error);
    expect(error.message).toContain(`--prompt-file '${file}' is not readable`);
    expect(error.cause).toBeInstanceOf(Error);
  });
});

describe("engine prompt delivery contracts", () => {
  it.each(["", "instructions 世界\n"])("keeps Claude stdin and Codex positional/stdin delivery distinct", prompt => {
    fs.writeFileSync(promptPath, prompt);
    expect(resolveClaudePromptFileArgs(["--print", "--prompt-file", promptPath])).toEqual({ args: ["--print"], prompt });
    expect(resolveCodexPromptFileArgs(["exec", "--prompt-file", promptPath])).toEqual(["exec", prompt]);
    expect(resolveCodexPromptInput(["exec", "--prompt-file", promptPath])).toEqual({ args: ["exec", "-"], stdin: prompt });
  });

  it("keeps trailing-flag tolerance exclusive to the legacy Codex helper", () => {
    expect(resolveCodexPromptFileArgs(["exec", "--prompt-file"])).toEqual(["exec", "--prompt-file"]);
    expect(() => resolveCodexPromptInput(["exec", "--prompt-file"])).toThrow("requires a readable file path");
    expect(() => resolveClaudePromptFileArgs(["--print", "--prompt-file"])).toThrow("requires a readable file path");
  });
});
