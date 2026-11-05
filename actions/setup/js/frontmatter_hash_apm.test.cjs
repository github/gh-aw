// @ts-check
import { describe, it, expect } from "vitest";
const path = require("path");
const { computeFrontmatterHash } = require("./frontmatter_hash_pure.cjs");

describe("APM manifest hash", () => {
  it.each(["apm: apm.yml", 'apm: "apm.yml" # comment', "apm: 'apm.yml'", '"apm": apm.yml'])("hashes the manifest and lockfile for %s", async spelling => {
    const workflow = "/repo/.github/workflows/review.md";
    const manifest = path.join(path.dirname(workflow), "apm.yml");
    const lock = path.join(path.dirname(workflow), "apm.lock.yaml");
    const files = new Map([
      [workflow, `---\nengine: copilot\n${spelling}\n---\nReview code.\n`],
      [manifest, "name: review\nversion: 1.0.0\n"],
      [lock, "lockfile_version: '1'\ndependencies: []\n"],
    ]);
    const fileReader = async file => {
      const content = files.get(file);
      if (content === undefined) throw new Error(`Missing ${file}`);
      return content;
    };
    const first = await computeFrontmatterHash(workflow, { fileReader });
    files.set(lock, `${files.get(lock)}# branch head changed\n`);
    const movedLock = await computeFrontmatterHash(workflow, { fileReader });
    expect(movedLock).not.toBe(first);
    files.set(manifest, `${files.get(manifest)}# manifest changed\n`);
    expect(await computeFrontmatterHash(workflow, { fileReader })).not.toBe(movedLock);
    files.delete(lock);
    await expect(computeFrontmatterHash(workflow, { fileReader })).rejects.toThrow("Missing");
  });

  it("normalizes CRLF and resolves root-relative traversal through the file reader", async () => {
    const workflow = "/repo/.github/workflows/review.md";
    const files = new Map([
      [workflow, "---\nengine: copilot\napm: ../../apm.yml\n---\n"],
      ["/repo/apm.yml", "name: review\nversion: 1.0.0\n"],
      ["/repo/apm.lock.yaml", "lockfile_version: '1'\ndependencies: []\n"],
    ]);
    const fileReader = async file => {
      const content = files.get(file);
      if (content === undefined) throw new Error(`Missing ${file}`);
      return content;
    };
    const first = await computeFrontmatterHash(workflow, { fileReader });
    for (const [name, content] of files) files.set(name, content.replaceAll("\n", "\r\n"));
    expect(await computeFrontmatterHash(workflow, { fileReader })).toBe(first);
  });
});
