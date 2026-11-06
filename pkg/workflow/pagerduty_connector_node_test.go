//go:build !integration

package workflow

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPagerDutyConnectorBundledNodeExecution(t *testing.T) {
	read, write := pagerDutyPlans(t)
	reader, err := read.Encode()
	require.NoError(t, err)
	writer, err := write.Encode()
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{
		"reader": reader, "writer": writer,
		"incident": json.RawMessage(pagerDutyFixture(t, "incident.json")),
		"note":     json.RawMessage(pagerDutyFixture(t, "note.json")),
		"readArgs": pagerDutyReadArgs(), "noteArgs": pagerDutyNoteArgs(),
	})
	require.NoError(t, err)
	node, err := exec.LookPath("node")
	require.NoError(t, err, "Node.js is required to verify the bundled execution host")
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", pagerDutyNodeChecks, runtime)
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.JSONEq(t, `{"reads":2,"writes":1,"staged":true}`, string(output))
}

const pagerDutyNodeChecks = `
const assert = require("node:assert/strict");
const { Readable, Writable } = require("node:stream");
const { executeConnector, serve } = require(process.argv[1]);
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
globalThis.fetch = () => { throw new Error("Live network is forbidden in provider tests"); };
const normalized = value => JSON.parse(JSON.stringify(value));
const response = (value, status = 200) => new Response(
  typeof value === "string" ? value : JSON.stringify(value),
  { status, headers: { "Content-Type": "application/json" } }
);
const noSecrets = new Proxy({}, { get() { throw new Error("Staging or rejected arguments accessed environment"); } });
const noFetch = () => { throw new Error("Staging or rejected arguments reached network"); };

(async () => {
  const stage = args => executeConnector(input.writer, "safe-output", "add-note", args,
    { staged: true, env: noSecrets, fetchImpl: noFetch });
  const staged = await stage(input.noteArgs);
  assert.equal(staged.staged, true);
  assert.equal(staged.request.operation, "add-note");
  assert.deepEqual(normalized(staged.request.arguments), input.noteArgs);
  for (const content of ["x".repeat(2000), "\u00e9".repeat(2000), "Untrusted $(not-code); <xml>& data"]) {
    assert.equal((await stage({ ...input.noteArgs, content })).request.arguments.content, content);
  }
  for (const args of [
    { ...input.noteArgs, "incident-id": "POTHER1" },
    { ...input.noteArgs, content: "x".repeat(2001) },
    { ...input.noteArgs, content: "\u00e9".repeat(2001) },
    { ...input.noteArgs, content: " \n\t" },
    { ...input.noteArgs, From: "agent@example.invalid" },
    { ...input.noteArgs, status: "acknowledged" },
  ]) await assert.rejects(() => stage(args));
  for (const field of ["incident-id", "service-id", "team-id"]) {
    await assert.rejects(() => executeConnector(input.reader, "mcp", "get-incident",
      { ...input.readArgs, [field]: "POTHER1" }, { env: noSecrets, fetchImpl: noFetch }));
  }
  await assert.rejects(() => executeConnector(input.reader, "safe-output", "add-note",
    input.noteArgs, { env: noSecrets, fetchImpl: noFetch }));

  let reads = 0, writes = 0;
  const credentials = phase => new Proxy({}, { get(_target, key) {
    if (!String(key).startsWith("GH_AW_CONNECTOR_SECRET_")) return undefined;
    assert.equal(key, "GH_AW_CONNECTOR_SECRET_PAGERDUTY_" +
      (phase === "agent" ? "READ" : "WRITE") + "_AUTHORIZATION");
    return "Token token=fixture-" + phase;
  } });
  const readFetch = async (url, init) => {
    reads++;
    assert.equal(url, "https://api.pagerduty.com/incidents/PINC001");
    assert.equal(init.method, "GET");
    assert.equal(init.redirect, "manual");
    const headers = new Headers(init.headers);
    assert.equal(headers.get("Authorization"), "Token token=fixture-agent");
    assert.equal(headers.get("Accept"), "application/vnd.pagerduty+json;version=2");
    assert.equal(headers.get("From"), null);
    return response(input.incident);
  };
  const result = await executeConnector(input.reader, "mcp", "get-incident", input.readArgs,
    { env: credentials("agent"), fetchImpl: readFetch });
  assert.equal(result.untrusted, true);
  assert.deepEqual(normalized(result.data), {
    "incident-id": "PINC001", "service-id": "PSVC001", title: "Synthetic integration incident",
    status: "triggered", urgency: "high"
  });
  const written = await executeConnector(input.writer, "safe-output", "add-note", input.noteArgs,
    { env: credentials("safe-output"), fetchImpl: async (url, init) => {
      writes++;
      assert.equal(url, "https://api.pagerduty.com/incidents/PINC001/notes");
      assert.equal(init.method, "POST");
      assert.equal(init.redirect, "manual");
      const headers = new Headers(init.headers);
      assert.equal(headers.get("Authorization"), "Token token=fixture-safe-output");
      assert.equal(headers.get("Accept"), "application/vnd.pagerduty+json;version=2");
      assert.equal(headers.get("From"), "incident-automation@example.invalid");
      assert.equal(headers.get("Content-Type"), "application/json");
      assert.deepEqual(JSON.parse(init.body), { note: { content: input.noteArgs.content } });
      return response(input.note, 201);
    } });
  assert.equal(written.untrusted, true);
  assert.deepEqual(normalized(written.data), { "note-id": "PNOTE01", "created-at": "2026-01-01T00:00:00Z" });

  let output = "";
  const messages = [
    { jsonrpc: "2.0", id: 1, method: "tools/list" },
    { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "pagerduty_get_incident", arguments: input.readArgs } },
    { jsonrpc: "2.0", id: 3, method: "tools/call", params: { name: "pagerduty_add_note", arguments: input.noteArgs } }
  ];
  await serve(input.reader, Readable.from([messages.map(value => JSON.stringify(value)).join("\n") + "\n"]),
    new Writable({ write(chunk, _encoding, done) { output += chunk.toString(); done(); } }),
    { env: credentials("agent"), fetchImpl: readFetch });
  const replies = output.trim().split("\n").map(line => JSON.parse(line));
  assert.deepEqual(replies[0].result.tools.map(tool => tool.name), ["pagerduty_get_incident"]);
  assert.equal(JSON.parse(replies[1].result.content[0].text).untrusted, true);
  assert.equal(replies[2].result.isError, true);
  assert.equal(reads, 2);
  assert.equal(writes, 1);

  for (const invalid of [
    { incident: { ...input.incident.incident, id: "POTHER1" } },
    { incident: { ...input.incident.incident, service: { id: "POTHER1", type: "service_reference" } } },
    { incident: { ...input.incident.incident, teams: [{ id: "POTHER1" }] } },
  ]) await assert.rejects(() => executeConnector(input.reader, "mcp", "get-incident", input.readArgs,
    { env: credentials("agent"), fetchImpl: async () => response(invalid) }));
  for (const [status, body] of [
    [401, {}], [403, {}], [429, {}], [500, {}], [302, {}], [201, ""],
    [201, "invalid JSON"], [201, "x".repeat(16385)],
    [201, { note: { ...input.note.note, content: "wrong content" } }],
    [201, { note: { content: input.noteArgs.content, created_at: "2026-01-01T00:00:00Z" } }]
  ]) {
    let calls = 0;
    await assert.rejects(() => executeConnector(input.writer, "safe-output", "add-note", input.noteArgs,
      { env: credentials("safe-output"), fetchImpl: async () => { calls++; return response(body, status); } }));
    assert.equal(calls, 1, "Failed writes must not retry or fall back");
  }
  let failures = 0;
  await assert.rejects(() => executeConnector(input.writer, "safe-output", "add-note", input.noteArgs,
    { env: credentials("safe-output"), fetchImpl: async () => { failures++; throw new Error("private diagnostic"); } }),
    error => !error.message.includes("private diagnostic"));
  assert.equal(failures, 1);
  process.stdout.write(JSON.stringify({ reads, writes, staged: true }));
})().catch(error => { console.error(error); process.exitCode = 1; });
`
