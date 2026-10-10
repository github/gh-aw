"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const { readLedgerFile } = require("./native_probe.cjs");

function ledgerFixture(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-native-ledger-test-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const file = path.join(directory, "ledger.jsonl");
  fs.writeFileSync(file, "fixture");
  return file;
}

test("ledger reads retain short reads and split UTF-8 characters", t => {
  const file = ledgerFixture(t);
  fs.writeFileSync(file, "ledger \u00e9\n");
  const readSync = fs.readSync;
  t.mock.method(fs, "readSync", (fd, buffer, offset, length, position) => readSync(fd, buffer, offset, Math.min(length, 1), position));
  assert.equal(readLedgerFile(file), "ledger \u00e9\n");
});

test("ledger growth after fstat is bounded to 80 MiB plus one byte and closes the descriptor", t => {
  const file = ledgerFixture(t);
  let total = 0;
  let descriptor;
  t.mock.method(fs, "readSync", (fd, buffer, offset, length, position) => {
    descriptor = fd;
    assert.equal(position, total);
    total += length;
    return length;
  });
  assert.throws(() => readLedgerFile(file), /resource_limit: adapter input exceeds 80 MiB/);
  assert.equal(total, 80 * 1024 * 1024 + 1);
  assert.throws(() => fs.fstatSync(descriptor), { code: "EBADF" });
});

test("ledger reads accept the exact 80 MiB boundary", t => {
  const file = ledgerFixture(t);
  const fd = fs.openSync(file, "r+");
  fs.ftruncateSync(fd, 80 * 1024 * 1024);
  fs.closeSync(fd);
  assert.equal(Buffer.byteLength(readLedgerFile(file)), 80 * 1024 * 1024);
});
