import { describe, expect, it } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

describe("shared-runner chroot cleanup boundary", () => {
  for (const relativePath of ["../post.js", "../clean.sh", "../sh/install_copilot_cli.sh", "../sh/install_awf_binary.sh"]) {
    it(`${relativePath} never sweeps another invocation's chroots`, () => {
      const script = fs.readFileSync(path.join(__dirname, relativePath), "utf8");

      expect(script).not.toMatch(/\bfind\s+\/tmp\b/);
      expect(script).not.toContain('"find", "/tmp"');
      expect(script).not.toContain("-exec rm -rf");
      expect(script).not.toContain("awf-*-chroot-home");
      expect(script).not.toContain("awf-chroot-*");
    });
  }

  for (const relativePath of ["../post.js", "../clean.sh"]) {
    it(`${relativePath} warns instead of reclaiming untracked chroots`, () => {
      const script = fs.readFileSync(path.join(__dirname, relativePath), "utf8");

      expect(script).toContain("untracked AWF chroot directories are not reclaimed");
      expect(script).toContain("shared-runner ownership cannot be established");
    });
  }
});
