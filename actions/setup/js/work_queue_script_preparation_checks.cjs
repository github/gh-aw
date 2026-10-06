"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { prepareAdapterContext } = require("./work_queue_prepare_claim_adapter.cjs");
const { main } = require("./work_queue_prepare_claim_script.cjs");

function registerTests({ describe, it }) {
  describe("credential-free Claim script preparation", () => {
    it("runs the compiler-produced script against exact isolated inputs and only exports declared data", async () => {
      const root = path.resolve(".queue-validation-cache", `queue-script-${randomUUID()}`);
      const keys = ["RUNNER_TEMP", "GH_AW_CLAIM_INPUT", "GH_AW_CLAIM_OUTPUT", "GH_AW_CLAIM_SCRIPT_FILENAME"];
      const previous = keys.map(key => process.env[key]);
      const assignment = {
        version: 3,
        dispatch_id: "d",
        request_id: "r",
        commit_id: "c",
        policy_epoch: "e",
        pool: "p",
        worker_profile: "w",
        claims: ["one", "two"].map(handle => ({ handle, claim_id: `c:${handle}`, work_id: `w:${handle}`, work: {}, result_refs: [] })),
      };
      const message = { type: "custom", claim_handle: "one", number: 42, content: "original" };
      try {
        const directory = path.join(root, "gh-aw", "actions");
        fs.mkdirSync(directory, { recursive: true });
        const scriptFilename = "safe_output_script_custom.cjs";
        fs.writeFileSync(path.join(directory, scriptFilename), 'module.exports = { main: async () => async item => ({ number: item.number, content: item.content + " prepared" }) };');
        const prepared = await prepareAdapterContext({
          assignment,
          adapter: { mode: "script", "effect-type": "update_issue", "target-repo": "owner/repo", "field-map": { item_number: "number", body: "content" } },
          type: "custom",
          index: 0,
          artifactRoot: root,
          messages: [message, { ...message, claim_handle: "two" }],
          authorize: async request => ({ authorized: true, claim_handle: request.claim_handle }),
        });
        process.env.RUNNER_TEMP = root;
        process.env.GH_AW_CLAIM_INPUT = prepared.inputFile;
        process.env.GH_AW_CLAIM_OUTPUT = prepared.outputFile;
        process.env.GH_AW_CLAIM_SCRIPT_FILENAME = scriptFilename;
        await main();
        const output = JSON.parse(fs.readFileSync(prepared.outputFile, "utf8"));
        assert.deepEqual(output, { version: 3, claim_handle: "one", type: "custom", messages: [{ input: message, payload: { number: 42, content: "original prepared" } }] });
        assert.equal(JSON.parse(fs.readFileSync(prepared.assignmentFile, "utf8")).claims.length, 2);
        delete require.cache[path.join(directory, scriptFilename)];
      } finally {
        keys.forEach((key, index) => (previous[index] === undefined ? delete process.env[key] : (process.env[key] = previous[index])));
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
