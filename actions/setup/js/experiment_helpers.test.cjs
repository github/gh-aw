import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import fs from "fs";

const { readExperimentAssignments, EXPERIMENT_ASSIGNMENTS_PATH } = await import("./experiment_helpers.cjs");

describe("readExperimentAssignments", () => {
  let readFileSpy;
  const savedStateDir = process.env.GH_AW_EXPERIMENT_STATE_DIR;

  beforeEach(() => {
    delete process.env.GH_AW_EXPERIMENT_STATE_DIR;
    readFileSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    readFileSpy.mockRestore();
    delete process.env.GH_AW_EXPERIMENTS_PROMPT_STYLE;
    delete process.env.GH_AW_EXPERIMENTS_REASONING_DEPTH;
    if (savedStateDir !== undefined) {
      process.env.GH_AW_EXPERIMENT_STATE_DIR = savedStateDir;
    } else {
      delete process.env.GH_AW_EXPERIMENT_STATE_DIR;
    }
  });

  it("returns null when the assignments file does not exist", () => {
    expect(readExperimentAssignments()).toBeNull();
  });

  it("returns null when the assignments file contains invalid JSON", () => {
    readFileSpy.mockReturnValue("not-valid-json");
    expect(readExperimentAssignments()).toBeNull();
  });

  it("returns null when the assignments file contains a non-object value", () => {
    readFileSpy.mockReturnValue(JSON.stringify(["A", "B"]));
    expect(readExperimentAssignments()).toBeNull();
  });

  it("returns the parsed assignments object when the file is valid", () => {
    readFileSpy.mockImplementation(filePath => {
      if (filePath === EXPERIMENT_ASSIGNMENTS_PATH) {
        return JSON.stringify({ caveman: "yes", style: "detailed" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readExperimentAssignments()).toEqual({ caveman: "yes", style: "detailed" });
  });

  it("reads from GH_AW_EXPERIMENT_STATE_DIR/assignments.json when env var is set", () => {
    process.env.GH_AW_EXPERIMENT_STATE_DIR = "/custom/experiments";
    readFileSpy.mockImplementation(filePath => {
      if (filePath === "/custom/experiments/assignments.json") {
        return JSON.stringify({ feature: "on" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readExperimentAssignments()).toEqual({ feature: "on" });
  });

  it("falls back to EXPERIMENT_ASSIGNMENTS_PATH when GH_AW_EXPERIMENT_STATE_DIR is not set", () => {
    readFileSpy.mockImplementation(filePath => {
      if (filePath === EXPERIMENT_ASSIGNMENTS_PATH) {
        return JSON.stringify({ mode: "fast" });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });
    expect(readExperimentAssignments()).toEqual({ mode: "fast" });
  });

  it("falls back to GH_AW_EXPERIMENTS_* environment variables when assignments file is unavailable", () => {
    process.env.GH_AW_EXPERIMENTS_PROMPT_STYLE = "concise";
    process.env.GH_AW_EXPERIMENTS_REASONING_DEPTH = "deep";
    expect(readExperimentAssignments()).toEqual({ prompt_style: "concise", reasoning_depth: "deep" });
  });

  it("never treats the secret broker endpoint as an experiment assignment", () => {
    vi.stubEnv("GH_AW_EXPERIMENTS_BROKER_URL", "https://synthetic-broker.example.test/secret");
    expect(readExperimentAssignments()).toBeNull();
    process.env.GH_AW_EXPERIMENTS_PROMPT_STYLE = "concise";
    expect(readExperimentAssignments()).toEqual({ prompt_style: "concise" });
  });
});
