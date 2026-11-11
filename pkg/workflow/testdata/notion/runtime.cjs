const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { Readable, Writable } = require("node:stream");
const { decodePlan, executeConnector, serve } = require(process.argv[2]);
const { read, write, page, comment } = JSON.parse(readFileSync(0, "utf8"));
const pageID = "11111111-2222-4333-8444-555555555555";
const args = { "page-id": pageID, text: "Release reference reviewed." };
const plain = value => JSON.parse(JSON.stringify(value));
const secretReads = [];
const calls = [];
globalThis.fetch = () => {
  throw new Error("Real network is forbidden in Notion provider tests");
};
const env = new Proxy(
  {
    GH_AW_CONNECTOR_SECRET_NOTION_READ_TOKEN: "fixture-read-token",
    GH_AW_CONNECTOR_SECRET_NOTION_COMMENT_WRITE_TOKEN: "fixture-write-token",
  },
  {
    get(target, key) {
      if (String(key).startsWith("GH_AW_CONNECTOR_SECRET_")) secretReads.push(key);
      return target[key];
    },
  }
);
const noSecrets = new Proxy(
  {},
  {
    get() {
      throw new Error("Staging or rejected arguments must not access credentials");
    },
  }
);
const forbiddenFetch = () => {
  throw new Error("Staging or rejected arguments must not issue requests");
};
const response = (body, status = 200) =>
  new Response(typeof body === "string" ? body : JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "retry-after": "10" },
  });
function options(fixture, status = 200) {
  return {
    env,
    fetchImpl: async (url, init) => {
      calls.push({ url, init });
      assert.equal(init.headers["Notion-Version"], "2026-03-11");
      assert.equal(init.headers["Content-Type"], "application/json");
      assert.equal(init.redirect, "manual");
      assert.equal(init.credentials, "omit");
      return response(fixture, status);
    },
  };
}
async function main() {
  const readPlan = decodePlan(read);
  const writePlan = decodePlan(write);
  assert.deepEqual(plain(readPlan.connection.resources), { pages: [pageID] });
  assert.deepEqual(plain(writePlan.connection.resources), { pages: [pageID] });
  assert.deepEqual(Object.keys(readPlan.connection.credentials), ["reader"]);
  assert.deepEqual(Object.keys(writePlan.connection.credentials), ["writer"]);
  const metadata = await executeConnector(read, "mcp", "get-page", { "page-id": pageID }, options(page));
  assert.equal(metadata.untrusted, true);
  assert.deepEqual(plain(metadata.data), {
    "page-id": pageID,
    url: page.url,
    "last-edited-time": page.last_edited_time,
  });
  assert.equal(calls[0].url, "https://api.notion.com/v1/pages/" + pageID + "?filter_properties=title");
  assert.equal(calls[0].init.headers.Authorization, "Bearer fixture-read-token");
  const before = calls.length;
  const staged = await executeConnector(write, "safe-output", "add-comment", args, {
    staged: true,
    env: noSecrets,
    fetchImpl: forbiddenFetch,
  });
  assert.deepEqual(plain(staged.request.arguments), args);
  assert.equal(staged.staged, true);
  assert.equal(calls.length, before);
  await assert.rejects(executeConnector(read, "safe-output", "add-comment", args, { staged: true, env: noSecrets, fetchImpl: forbiddenFetch }));
  await assert.rejects(executeConnector(write, "mcp", "get-page", { "page-id": pageID }, { env: noSecrets, fetchImpl: forbiddenFetch }));
  for (const invalid of [
    {},
    { ...args, "page-id": "11111111-2222-4333-8444-555555555556" },
    { ...args, "page-id": "../comments" },
    { ...args, "page-id": pageID + "\n" },
    { ...args, text: "" },
    { ...args, text: " \t\r\n\v\f" },
    { ...args, text: "x".repeat(2001) },
    { ...args, text: 42 },
    { ...args, "discussion-id": "unapproved" },
  ]) {
    await assert.rejects(executeConnector(write, "safe-output", "add-comment", invalid, { staged: true, env: noSecrets, fetchImpl: forbiddenFetch }));
  }
  const result = await executeConnector(write, "safe-output", "add-comment", args, options(comment));
  assert.equal(result.untrusted, true);
  assert.deepEqual(plain(result.data), {
    "page-id": pageID,
    "comment-id": comment.id,
    "discussion-id": comment.discussion_id,
    "created-time": comment.created_time,
  });
  assert.equal(calls.at(-1).url, "https://api.notion.com/v1/comments");
  assert.equal(calls.at(-1).init.headers.Authorization, "Bearer fixture-write-token");
  assert.deepEqual(JSON.parse(calls.at(-1).init.body), {
    parent: { page_id: pageID },
    rich_text: [{ type: "text", text: { content: args.text } }],
  });
  assert.deepEqual(secretReads, ["GH_AW_CONNECTOR_SECRET_NOTION_READ_TOKEN", "GH_AW_CONNECTOR_SECRET_NOTION_COMMENT_WRITE_TOKEN"]);
  for (const status of [302, 400, 401, 403, 404, 429, 500, 503, 529]) {
    const count = calls.length;
    await assert.rejects(executeConnector(write, "safe-output", "add-comment", args, options({ object: "error" }, status)));
    assert.equal(calls.length, count + 1, "Failed comments must not retry");
  }
  for (const invalid of [
    { ...comment, object: "error" },
    { ...comment, id: "" },
    { ...comment, discussion_id: "" },
    { ...comment, parent: { type: "page_id", page_id: "11111111-2222-4333-8444-555555555556" } },
    { ...comment, rich_text: [{ text: { content: "different" } }] },
    "not-json",
    "",
    "x".repeat(65537),
  ]) {
    const count = calls.length;
    await assert.rejects(executeConnector(write, "safe-output", "add-comment", args, options(invalid)));
    assert.equal(calls.length, count + 1, "Missing response evidence must not retry a write");
  }
  for (const invalid of [
    { ...page, object: "block" },
    { ...page, in_trash: true },
    { ...page, id: "11111111-2222-4333-8444-555555555556" },
    { ...page, last_edited_time: 42 },
  ]) {
    await assert.rejects(executeConnector(read, "mcp", "get-page", { "page-id": pageID }, options(invalid)));
  }
  let output = "";
  await serve(
    read,
    Readable.from([JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list" }) + "\n"]),
    new Writable({
      write(chunk, _encoding, callback) {
        output += chunk;
        callback();
      },
    }),
    { env: noSecrets, fetchImpl: forbiddenFetch }
  );
  const listed = JSON.parse(output).result.tools;
  assert.deepEqual(
    listed.map(tool => tool.name),
    ["get_page"]
  );
  assert.equal(listed[0].inputSchema.additionalProperties, false);
  assert.deepEqual(listed[0].inputSchema.required, ["page-id"]);
  for (const text of ["x".repeat(2000), "\u{1f600}".repeat(2000)]) {
    const intent = await executeConnector(write, "safe-output", "add-comment", { ...args, text }, { staged: true, env: noSecrets, fetchImpl: forbiddenFetch });
    assert.equal(intent.request.arguments.text, text);
  }
  process.stdout.write("Notion frozen Go plans passed bundled Node wire, MCP, staging and rejection checks\n");
}
main().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
