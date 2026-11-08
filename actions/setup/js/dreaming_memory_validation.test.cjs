import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { fileURLToPath } from "url";
import { runCustomMemoryValidation } from "./memory_custom_validation.cjs";

const workflowPath = fileURLToPath(new URL("../../../.github/workflows/weekly-git-simulator-dreaming.md", import.meta.url));
const workflow = fs.readFileSync(workflowPath, "utf8");
const scriptBlock = workflow.match(/^      script: \|\n((?:        .*(?:\n|$)|\n)+)/m);
if (!scriptBlock) throw new Error("Dreamer workflow must declare an inline memory validator");
const script = scriptBlock[1].replace(/^        /gm, "");

const review = {
  version: 1,
  source_hashes: { "git-sim-state.json": "a".repeat(64) },
  repository_files: { "pkg/workflow/repo_memory.go": "b".repeat(64) },
  reviewed_note_ids: ["persisted-note"],
  lesson_count: 1,
  signals: [],
};
const reviewJSON = JSON.stringify(review);
const files = {
  "dreaming-index.md": "index\n",
  "dreaming-lessons.md": "### Lesson 1\nSupported, scoped lesson.\n",
  "dreaming-review.json": reviewJSON,
};
const lessonHeading = "### Lesson 1\n";
const cases = [
  ["cold-start", {}, true],
  ["complete-view", files, true],
  ["partial-view", { "dreaming-index.md": "index" }, false],
  ["index-at-byte-limit", { ...files, "dreaming-index.md": "\u00e9".repeat(1024) }, true],
  ["index-over-byte-limit", { ...files, "dreaming-index.md": "\u00e9".repeat(1025) }, false],
  ["lessons-at-byte-limit", { ...files, "dreaming-lessons.md": lessonHeading + "x".repeat(12288 - Buffer.byteLength(lessonHeading)) }, true],
  ["lessons-over-byte-limit", { ...files, "dreaming-lessons.md": lessonHeading + "x".repeat(12289 - Buffer.byteLength(lessonHeading)) }, false],
  ["review-at-byte-limit", { ...files, "dreaming-review.json": reviewJSON + " ".repeat(16384 - Buffer.byteLength(reviewJSON)) }, true],
  ["review-over-byte-limit", { ...files, "dreaming-review.json": reviewJSON + " ".repeat(16385 - Buffer.byteLength(reviewJSON)) }, false],
  ["malformed-review", { ...files, "dreaming-review.json": "{" }, false],
  ["nonconsecutive-headings", { ...files, "dreaming-lessons.md": "### Lesson 2\n" }, false],
  [
    "ten-lessons",
    {
      ...files,
      "dreaming-lessons.md": Array.from({ length: 10 }, (_, index) => `### Lesson ${index + 1}\nScoped lesson.\n`).join(""),
      "dreaming-review.json": JSON.stringify({ ...review, lesson_count: 10 }),
    },
    true,
  ],
];
for (const [name, fields] of [
  ["too-many-lessons", { lesson_count: 11 }],
  ["count-mismatch", { lesson_count: 0 }],
  ["bad-hash", { source_hashes: { "git-sim-state.json": "not-a-hash" } }],
  ["unsafe-path", { repository_files: { "../secret": "a".repeat(64) } }],
  ["too-many-notes", { reviewed_note_ids: Array.from({ length: 21 }, (_, index) => String(index)) }],
  ["too-many-sources", { source_hashes: Object.fromEntries(Array.from({ length: 21 }, (_, index) => [String(index), "a".repeat(64)])) }],
]) {
  cases.push([name, { ...files, "dreaming-review.json": JSON.stringify({ ...review, ...fields }) }, false]);
}

describe.each(["baseline", "upload", "push"])("dreaming memory validator (%s)", phase => {
  let memoryDir;

  beforeEach(() => {
    memoryDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-dreaming-validation-test-"));
  });

  afterEach(() => {
    fs.rmSync(memoryDir, { recursive: true, force: true });
  });

  it.each(cases)("%s", (_name, contents, expected) => {
    for (const [name, text] of Object.entries(contents)) {
      fs.writeFileSync(path.join(memoryDir, name), text);
    }
    const result = runCustomMemoryValidation({
      script,
      memoryDir,
      kind: "repo",
      phase,
      timeoutSeconds: 5,
    });
    expect(result.ok, result.stderr).toBe(expected);
  });
});
