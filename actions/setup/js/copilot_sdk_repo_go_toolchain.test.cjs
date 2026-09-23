// @ts-check

import { describe, it, expect } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { GO_SOURCE_EXTENSION, isGoSourceFile, goFormatWriteArgs, goFormatCheckTreeArgs, goVetArgs, goBuildArgs, goTestArgs } = require("./copilot_sdk_repo_go_toolchain.cjs");

describe("copilot_sdk_repo_go_toolchain", () => {
  describe("GO_SOURCE_EXTENSION", () => {
    it("is the .go extension", () => {
      expect(GO_SOURCE_EXTENSION).toBe(".go");
    });
  });

  describe("isGoSourceFile", () => {
    it("accepts files ending in .go", () => {
      expect(isGoSourceFile("main.go")).toBe(true);
      expect(isGoSourceFile("pkg/workflow/engine.go")).toBe(true);
    });

    it("rejects non-Go files", () => {
      expect(isGoSourceFile("main.js")).toBe(false);
      expect(isGoSourceFile("README.md")).toBe(false);
      expect(isGoSourceFile("go")).toBe(false);
      expect(isGoSourceFile("main.go.bak")).toBe(false);
    });

    it("rejects the empty string", () => {
      expect(isGoSourceFile("")).toBe(false);
    });

    it("accepts a bare extension match at the start of the filename", () => {
      expect(isGoSourceFile(".go")).toBe(true);
    });
  });

  describe("goFormatWriteArgs", () => {
    it("wraps absolute paths with -w -l", () => {
      expect(goFormatWriteArgs(["/repo/main.go"])).toEqual(["-w", "-l", "/repo/main.go"]);
    });

    it("preserves the order and count of multiple paths", () => {
      expect(goFormatWriteArgs(["/repo/a.go", "/repo/b.go", "/repo/c.go"])).toEqual(["-w", "-l", "/repo/a.go", "/repo/b.go", "/repo/c.go"]);
    });

    it("returns just the flags for an empty file list", () => {
      expect(goFormatWriteArgs([])).toEqual(["-w", "-l"]);
    });

    it("does not mutate the input array", () => {
      const input = Object.freeze(["/repo/a.go"]);
      expect(() => goFormatWriteArgs(input)).not.toThrow();
    });
  });

  describe("goFormatCheckTreeArgs", () => {
    it("returns the fixed -l . argv with no caller-controlled input", () => {
      expect(goFormatCheckTreeArgs()).toEqual(["-l", "."]);
    });

    it("returns a fresh array on each call", () => {
      const first = goFormatCheckTreeArgs();
      const second = goFormatCheckTreeArgs();
      expect(first).not.toBe(second);
      expect(first).toEqual(second);
    });
  });

  describe("goVetArgs", () => {
    it("returns the fixed vet ./... argv", () => {
      expect(goVetArgs()).toEqual(["vet", "./..."]);
    });
  });

  describe("goBuildArgs", () => {
    it("embeds only the output path with the fixed build argv", () => {
      expect(goBuildArgs("/tmp/output")).toEqual(["build", "-o", "/tmp/output", "./..."]);
    });

    it("does not alter the output path", () => {
      expect(goBuildArgs("relative/path")).toEqual(["build", "-o", "relative/path", "./..."]);
    });
  });

  describe("goTestArgs", () => {
    it("returns the full-suite argv when full is true", () => {
      expect(goTestArgs(true)).toEqual(["test", "-count=1", "./..."]);
    });

    it("returns the compile-only argv when full is false", () => {
      expect(goTestArgs(false)).toEqual(["test", "-run", "^$", "./..."]);
    });
  });
});
