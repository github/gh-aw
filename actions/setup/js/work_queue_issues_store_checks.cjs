"use strict";

const assert = require("node:assert/strict");
const { readIssues, applyAndPublishIssues, serializeRecord, record } = require("./work_queue_issues_store.cjs");
const { readWorkQueueLog, publishWorkQueueRequest } = require("./work_queue_store.cjs");
const { newRequest } = require("./work_queue_replay.cjs");
const { administrator, context } = require("./work_queue_test_helpers.cjs");

function registerTests({ describe, it }) {
  describe("unsupported Issues queue backend", () => {
    it("rejects every Issues storage entry point without reading/writing or reinterpreting old records", () => {
      const githubClient = new Proxy(
        {},
        {
          get: () => {
            throw new Error("remote API must not be called");
          },
        }
      );
      for (const operation of [readIssues, applyAndPublishIssues, serializeRecord, record]) assert.throws(() => operation({ githubClient, secret: "ignored", intents: [] }), /unsupported_backend/);
    });
    it("rejects explicit and environment-selected Issues storage before all Git/API operations", async () => {
      const request = newRequest("control", "control", administrator, { operations: [{ kind: "Control", control: "grants_paused", value: true, reason: "maintenance" }] });
      const githubClient = new Proxy(
        {},
        {
          get: () => {
            throw new Error("remote API must not be called");
          },
        }
      );
      await assert.rejects(readWorkQueueLog({ githubClient, owner: "owner", repo: "repo", storage: "issues" }), /unsupported_backend/);
      await assert.rejects(publishWorkQueueRequest({ githubClient, owner: "owner", repo: "repo", request, context: context(administrator), storage: "issues" }), /unsupported_backend/);
      const previous = process.env.GH_AW_WORK_QUEUE_STORAGE;
      process.env.GH_AW_WORK_QUEUE_STORAGE = "issues";
      try {
        await assert.rejects(readWorkQueueLog({ githubClient, owner: "owner", repo: "repo" }), /unsupported_backend/);
      } finally {
        if (previous === undefined) delete process.env.GH_AW_WORK_QUEUE_STORAGE;
        else process.env.GH_AW_WORK_QUEUE_STORAGE = previous;
      }
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
