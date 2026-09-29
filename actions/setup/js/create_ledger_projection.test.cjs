// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { DatabaseSync } from "node:sqlite";
import { Ledger } from "./ledger_store.cjs";
import { createProjection } from "./create_ledger_projection.cjs";

test("creates a read-only SQLite projection from canonical ledger shards", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-projection-"));
  const sourceDir = path.join(root, "source");
  const databasePath = path.join(root, "projection", "ledger.db");
  const config = {
    name: "findings",
    schema: { type: "object", required: ["subject"], properties: { subject: { type: "string" } }, additionalProperties: false },
    max_record_kb: 32,
    max_segment_kb: 100,
    max_patch_kb: 10,
  };
  fs.mkdirSync(sourceDir, { recursive: true });
  const ledger = new Ledger({ memoryDir: sourceDir });
  try {
    ledger.append("finding", { subject: "A finding" });
    ledger.close();
    createProjection({ sourceDir, databasePath, config });

    const database = new DatabaseSync(databasePath, { readOnly: true });
    try {
      const row = database.prepare("SELECT payload FROM records").get();
      assert.deepEqual(JSON.parse(row.payload), { subject: "A finding" });
    } finally {
      database.close();
    }
    assert.equal(fs.statSync(databasePath).mode & 0o777, 0o444);
  } finally {
    ledger.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});
