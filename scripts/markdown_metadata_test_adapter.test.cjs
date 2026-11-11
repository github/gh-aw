"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const { withCompilerFixtures } = require("./markdown_metadata_test_adapter.cjs");
const metadata = require("../actions/setup/js/markdown_metadata.cjs");

function fixture(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-fixture-adapter-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const file = path.join(directory, "agent.md");
  const content = "---\nmodel: fixture-reader\n---\nRead files.";
  fs.writeFileSync(file, content);
  const main = t.mock.fn();
  return { directory, file, content, main, collect: withCompilerFixtures({ main }).main };
}

test("collects Markdown files, nested directory arguments, and embedded strings", t => {
  const { directory, file, content, main, collect } = fixture(t);
  const nested = path.join(directory, "nested");
  fs.mkdirSync(nested);
  fs.writeFileSync(path.join(nested, "nested.md"), "---\nmodel: nested-reader\n---\nRead.");
  for (const excluded of ["node_modules", ".git"]) {
    fs.mkdirSync(path.join(directory, excluded));
    fs.writeFileSync(path.join(directory, excluded, "invalid.md"), "---\nmodel: [invalid\n---");
  }
  fs.writeFileSync(path.join(directory, "ignored.txt"), "---\nmodel: [invalid\n---");
  collect(file, { directory }, ["---\nmodel: embedded-reader\n---\nRead."]);
  assert.equal(main.mock.callCount(), 1);
  for (const [document, model] of [
    [content, "fixture-reader"],
    ["---\nmodel: nested-reader\n---\nRead.", "nested-reader"],
    ["---\nmodel: embedded-reader\n---\nRead.", "embedded-reader"],
  ]) {
    assert.equal(metadata.readMarkdownMetadata("agent", document).value.model, model);
  }
});

test("skips existing file and directory symlinks and missing paths", t => {
  const { directory, file, main, collect } = fixture(t);
  const link = path.join(directory, "link.md");
  const directoryLink = path.join(directory, "directory-link");
  fs.symlinkSync(file, link);
  fs.symlinkSync(directory, directoryLink, "dir");
  const open = t.mock.method(fs, "openSync");
  collect(link, directoryLink, path.join(directory, "missing.md"));
  assert.equal(open.mock.callCount(), 0);
  assert.equal(main.mock.callCount(), 1);
});

test("rejects a leaf replaced with a symlink between inspection and open", t => {
  const { directory, file, main, collect } = fixture(t);
  const target = path.join(directory, "target.md");
  fs.writeFileSync(target, "---\nmodel: forbidden-reader\n---");
  const openSync = fs.openSync;
  t.mock.method(fs, "openSync", (filename, ...args) => {
    if (filename === file) {
      fs.unlinkSync(file);
      fs.symlinkSync(target, file);
    }
    return openSync(filename, ...args);
  });
  assert.throws(() => collect(file), { code: "ELOOP" });
  assert.equal(main.mock.callCount(), 0);
});

test("reads the opened descriptor even when the pathname is replaced", t => {
  const { directory, file, content, collect } = fixture(t);
  const target = path.join(directory, "target.md");
  fs.writeFileSync(target, "---\nmodel: forbidden-reader\n---");
  const fstatSync = fs.fstatSync;
  t.mock.method(fs, "fstatSync", descriptor => {
    fs.unlinkSync(file);
    fs.symlinkSync(target, file);
    return fstatSync(descriptor);
  });
  const close = t.mock.method(fs, "closeSync");
  collect(file);
  assert.equal(metadata.readMarkdownMetadata("agent", content).value.model, "fixture-reader");
  assert.equal(close.mock.callCount(), 1);
});

test("skips a leaf replaced with a directory and closes its descriptor", t => {
  const { file, main, collect } = fixture(t);
  const openSync = fs.openSync;
  t.mock.method(fs, "openSync", (filename, ...args) => {
    if (filename === file) {
      fs.unlinkSync(file);
      fs.mkdirSync(file);
    }
    return openSync(filename, ...args);
  });
  const read = t.mock.method(fs, "readFileSync");
  const close = t.mock.method(fs, "closeSync");
  collect(file);
  assert.equal(read.mock.callCount(), 0);
  assert.equal(close.mock.callCount(), 1);
  assert.equal(main.mock.callCount(), 1);
});

test("propagates read failures and closes the opened descriptor", t => {
  const { file, main, collect } = fixture(t);
  const failure = new Error("fixture read failed");
  t.mock.method(fs, "readFileSync", () => {
    throw failure;
  });
  const close = t.mock.method(fs, "closeSync");
  assert.throws(
    () => collect(file),
    error => error === failure
  );
  assert.equal(close.mock.callCount(), 1);
  assert.equal(main.mock.callCount(), 0);
});
