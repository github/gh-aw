import { describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { validBinaryPolicy, validateConnectorFile, readConnectorFile, stageConnectorFiles } from "./connector_file.cjs";

const bytes = Buffer.from([0, 1, 255, 42]);
const name = createHash("sha256").update(bytes).digest("hex") + ".png";
const policy = { input: "file", maxBytes: 4, extensions: [".png"], rejectContains: ["##vso["] };

describe("bounded connector staged files", () => {
  it("accepts only a bounded safe-output binary POST without another body", () => {
    const inputs = { file: { type: "string", required: true, maxLength: 1024 } };
    expect(validBinaryPolicy(policy, inputs, { method: "POST" }, "safe-output")).toBe(true);
    for (const invalid of [
      { ...policy, maxBytes: 0 },
      { ...policy, maxBytes: 10485761 },
      { ...policy, extensions: ["*"] },
      { ...policy, rejectContains: [""] },
    ]) {
      expect(validBinaryPolicy(invalid, inputs, { method: "POST" }, "safe-output")).toBe(false);
    }
    expect(validBinaryPolicy(policy, inputs, { method: "POST", body: { data: {} } }, "safe-output")).toBe(false);
    expect(validBinaryPolicy(policy, inputs, { method: "POST" }, "agent")).toBe(false);
  });

  it("verifies the exact byte threshold, digest, suffix and forbidden bytes", () => {
    expect(() => validateConnectorFile(name, bytes, policy)).not.toThrow();
    expect(() => validateConnectorFile(name, Buffer.concat([bytes, Buffer.from([5])]), policy)).toThrow();
    expect(() => validateConnectorFile(name, Buffer.from("bad"), policy)).toThrow();
    expect(() => validateConnectorFile(name.replace(".png", ".txt"), bytes, policy)).toThrow();
    const forbidden = Buffer.from("##vso[task.setvariable variable=x]");
    const forbiddenName = createHash("sha256").update(forbidden).digest("hex") + ".png";
    expect(() => validateConnectorFile(forbiddenName, forbidden, { ...policy, maxBytes: 100 })).toThrow("rejected byte");
  });

  it("pins reads to one content-addressed staging filename", () => {
    let reads = 0;
    const read = (path, max) => {
      reads++;
      expect(path).toBe("/stage/" + name);
      expect(max).toBe(4);
      return bytes;
    };
    expect(readConnectorFile(policy, { file: name }, "/stage", read)).toEqual(bytes);
    for (const file of ["/etc/passwd", "../escape", "missing.png"]) expect(() => readConnectorFile(policy, { file }, "/stage", read)).toThrow();
    expect(reads).toBe(1);
  });

  it("records the staged name only after the guarded copy succeeds", () => {
    const copied = [];
    const options = {
      root: "/stage",
      workspace: "/repo",
      rejectPath: () => null,
      read: (path, max) => {
        expect(path).toBe("/repo/image.png");
        expect(max).toBe(4);
        return bytes;
      },
      write: (path, content) => copied.push([path, content]),
    };
    const result = stageConnectorFiles({ file: "/repo/image.png", text: "kept" }, { file: policy }, options);
    expect(result).toEqual({ file: name, text: "kept" });
    expect(copied).toEqual([["/stage/" + name, bytes]]);
    expect(() =>
      stageConnectorFiles(
        { file: "/repo/image.png" },
        { file: policy },
        {
          ...options,
          write() {
            throw new Error("disk full");
          },
        }
      )
    ).toThrow("disk full");
  });

  it("rejects paths outside approved roots and sensitive paths before reading", () => {
    const options = {
      root: "/stage",
      workspace: "/repo",
      rejectPath: path => (path.includes(".git") ? "sensitive" : null),
      read() {
        throw new Error("must not read");
      },
    };
    for (const file of ["/etc/passwd", "/repo/../private", "/repo/.git/config"]) expect(() => stageConnectorFiles({ file }, { file: policy }, options)).toThrow("approved nonsensitive");
  });

  it.skipIf(process.platform !== "linux")("uses production descriptor-relative staging and rejects links on Linux", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "connector-files-"));
    try {
      const workspace = path.join(root, "workspace");
      const staging = path.join(root, "staging");
      fs.mkdirSync(workspace);
      fs.writeFileSync(path.join(workspace, "image.png"), bytes);
      const entry = stageConnectorFiles({ file: path.join(workspace, "image.png") }, { file: policy }, { root: staging, workspace, rejectPath: () => null });
      expect(entry.file).toBe(name);
      expect(readConnectorFile(policy, entry, staging)).toEqual(bytes);
      fs.symlinkSync(path.join(workspace, "image.png"), path.join(workspace, "link.png"));
      expect(() => stageConnectorFiles({ file: path.join(workspace, "link.png") }, { file: policy }, { root: staging, workspace, rejectPath: () => null })).toThrow();
      fs.linkSync(path.join(workspace, "image.png"), path.join(workspace, "hardlink.png"));
      expect(() => stageConnectorFiles({ file: path.join(workspace, "hardlink.png") }, { file: policy }, { root: staging, workspace, rejectPath: () => null })).toThrow();
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
