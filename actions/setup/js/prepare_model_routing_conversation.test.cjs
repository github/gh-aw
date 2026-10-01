// @ts-check

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fs = require("fs");
const os = require("os");
const path = require("path");
const { extractRoutingTask, main } = require("./prepare_model_routing_conversation.cjs");

describe("extractRoutingTask", () => {
  it("returns the trimmed text after the leading system block", () => {
    expect(extractRoutingTask("<system>instructions</system>\n  Do the task. \n")).toEqual({
      text: "Do the task.",
      trimmed: true,
    });
  });

  it("falls back to the full prompt when there is no leading system block", () => {
    expect(extractRoutingTask("  Do the task.  ")).toEqual({ text: "Do the task.", trimmed: false });
  });

  it("falls back when the system block has no closing tag", () => {
    expect(extractRoutingTask("<system>instructions")).toEqual({ text: "<system>instructions", trimmed: false });
  });

  it("falls back when the remainder is empty or whitespace", () => {
    expect(extractRoutingTask("<system>instructions</system>")).toEqual({
      text: "<system>instructions</system>",
      trimmed: false,
    });
    expect(extractRoutingTask("<system>instructions</system> \n ")).toEqual({
      text: "<system>instructions</system>",
      trimmed: false,
    });
  });

  it("does not split on neutralized system tags in the task", () => {
    expect(extractRoutingTask("<system>instructions</system>Do (system) work")).toEqual({
      text: "Do (system) work",
      trimmed: true,
    });
  });
});

describe("prepare_model_routing_conversation.cjs main", () => {
  let tempDir;
  let promptPath;
  let destination;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "prepare-model-routing-"));
    promptPath = path.join(tempDir, "prompt.txt");
    destination = path.join(tempDir, "gh-aw", "routing-conversation.json");
    vi.stubEnv("GH_AW_ROUTING_PROMPT", promptPath);
    vi.stubEnv("GH_AW_ROUTING_CONVERSATION_FILE", destination);
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
    fs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("writes the user conversation with restrictive file permissions", () => {
    fs.writeFileSync(promptPath, "<system>instructions</system>Do the task.");
    const log = vi.spyOn(console, "log").mockImplementation(() => {});

    main();

    expect(JSON.parse(fs.readFileSync(destination, "utf8"))).toEqual([{ role: "user", parts: [{ text: "Do the task." }] }]);
    expect(fs.statSync(destination).mode & 0o777).toBe(0o600);
    expect(log).toHaveBeenCalledWith("model-routing conversation: 12 of 41 chars (task only)");
  });

  it("throws when the rendered prompt is empty", () => {
    fs.writeFileSync(promptPath, " \n\t");

    expect(() => main()).toThrow("Rendered workflow prompt is empty; cannot route this task");
    expect(fs.existsSync(destination)).toBe(false);
  });
});
