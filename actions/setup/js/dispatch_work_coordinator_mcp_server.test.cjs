// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { createDispatchWorkCoordinatorServer } from "./dispatch_work_coordinator_mcp_server.cjs";

test("MCP server exposes only explicitly named Work operations", () => {
  const coordinator = {
    submit: async work => ({ work }),
    get: async workId => ({ workId }),
    list: async filter => ({ filter }),
    status: async () => ({ available: 0 }),
    claim: async workId => ({ workId }),
    claimNext: async filter => ({ filter }),
    cancel: async workId => ({ workId }),
  };
  const server = createDispatchWorkCoordinatorServer({ coordinator });
  assert.deepEqual(Object.keys(server.tools).sort(), ["dispatch_work_cancel", "dispatch_work_claim", "dispatch_work_claim_next", "dispatch_work_get", "dispatch_work_list", "dispatch_work_status", "dispatch_work_submit"]);
  for (const tool of Object.values(server.tools)) {
    assert.equal(tool.inputSchema.additionalProperties, false);
  }
  assert.equal(server.tools.dispatch_work_submit.inputSchema.properties.work.type, "object");
  assert.equal(server.tools.dispatch_work_claim.inputSchema.properties.work_id.type, "string");
  assert.throws(() => createDispatchWorkCoordinatorServer({ coordinator: undefined }), /required/);
});

test("MCP handlers use the coordinator and return safe validation errors", async () => {
  const coordinator = {
    async get(workId) {
      if (workId === "bad") throw new TypeError("Unknown Work item");
      return { work_id: workId };
    },
  };
  const server = createDispatchWorkCoordinatorServer({ coordinator });
  assert.deepEqual(await server.tools.dispatch_work_get.handler({ work_id: "work-a" }), {
    content: [{ type: "text", text: JSON.stringify({ work_id: "work-a" }) }],
  });
  assert.deepEqual(await server.tools.dispatch_work_get.handler({ work_id: "bad" }), {
    isError: true,
    content: [{ type: "text", text: "Invalid coordinator arguments or transaction-size limit exceeded." }],
  });
});
