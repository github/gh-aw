//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceNowBundledNodeRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required to exercise the shipped connector host")
	}
	compiler, path := serviceNowImportCompiler(t, serviceNowImportGrants,
		"tools:\n  servicenow: [get-incident, get-requested-item]\nsafe-outputs:\n  staged: true\n  servicenow-append-incident-work-note:\n  servicenow-append-requested-item-work-note:\n")
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	config := data.Connectors["servicenow"]
	agent, err := config.Agent.Encode()
	require.NoError(t, err)
	effects, err := config.SafeOutputs.Encode()
	require.NoError(t, err)
	setup, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	runner := t.TempDir()
	assets := filepath.Join(runner, "gh-aw", "actions")
	require.NoError(t, os.MkdirAll(assets, 0700))
	for _, name := range []string{"connector_runtime.cjs", "connector_regex.cjs", "error_codes.cjs"} {
		content, err := os.ReadFile(filepath.Join(setup, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(assets, name), content, 0600))
	}
	scripts := map[string]string{}
	expected := map[string]any{}
	for _, target := range []struct{ name, record string }{
		{"incident", serviceNowImportIncident},
		{"requested-item", serviceNowImportItem},
	} {
		operation := "append-" + target.name + "-work-note"
		scripts[operation] = data.SafeOutputs.Scripts["servicenow-"+operation].Script
		intent, err := config.SafeOutputs.PrepareSafeOutput(operation, map[string]any{
			"record": target.record, "note": "Synthetic review\n${{ secrets.NOT_A_SECRET }} $(not-code)",
		})
		require.NoError(t, err)
		expected[operation] = map[string]any{"staged": true, "request": intent}
	}
	input, err := json.Marshal(map[string]any{
		"agent": agent, "effects": effects, "setup": setup, "runner": runner,
		"scripts": scripts, "expected": expected,
	})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", serviceNowNodeDriver, filepath.Join(assets, "connector_runtime.cjs"))
	command.Stdin = strings.NewReader(string(input))
	command.Env = []string{}
	var diagnostics strings.Builder
	command.Stderr = &diagnostics
	output, err := command.Output()
	require.NoError(t, err, "%s\n%s", output, diagnostics.String())
	require.JSONEq(t, `{"tables":2,"generatedStagedEffects":2}`, string(output))
}

const serviceNowNodeDriver = `
const assert = require("node:assert/strict");
const { join } = require("node:path");
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const { executeConnector } = require(process.argv[1]);
const { decodePlan } = require(join(input.setup, "connector_runtime.cjs"));
const { formatConnectorArguments } = require(join(input.setup, "connector_content.cjs"));
const note = "Synthetic review\n" + "$" + "{{ secrets.NOT_A_SECRET }} $(not-code)";
const secretFree = new Proxy({}, { get() { throw new Error("credential access forbidden"); } });
const jsonData = value => JSON.parse(JSON.stringify(value));
globalThis.fetch = () => { throw new Error("live requests forbidden"); };
const reply = (body, status = 200) => new Response(body, { status, headers: { "content-type": "application/json" } });
async function main() {
  for (const [table, name, record, other] of [
    ["incident", "incident", "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"],
    ["sc_req_item", "requested-item", "fedcba9876543210fedcba9876543210", "0123456789abcdef0123456789abcdef"],
  ]) {
    const operation = "append-" + name + "-work-note";
    const args = { record, note };
    const metadata = JSON.stringify({ result: { sys_id: record, number: "SYNTHETIC1", state: "2", sys_mod_count: "17", description: "private-fixture" } });
    const receipt = JSON.stringify({ result: { sys_id: record } });
    let calls = 0;
    const fetchImpl = async (target, init) => {
      calls++;
      const url = new URL(target);
      assert.equal(url.origin, "https://example.service-now.com");
      assert.equal(url.pathname, "/api/now/table/" + table + "/" + record);
      assert.equal([...url.searchParams].length, 3);
      assert.equal(url.searchParams.get("sysparm_display_value"), "false");
      assert.equal(url.searchParams.get("sysparm_exclude_reference_link"), "true");
      assert.equal(init.redirect, "manual");
      if (init.method === "GET") {
        assert.equal(init.headers.Authorization, "Bearer synthetic-reader");
        assert.equal(url.searchParams.get("sysparm_fields"), "sys_id,number,state,sys_mod_count");
        return reply(metadata);
      }
      assert.equal(init.method, "PATCH");
      assert.equal(init.headers.Authorization, "Bearer synthetic-writer");
      assert.equal(init.headers["Content-Type"], "application/json");
      assert.equal(url.searchParams.get("sysparm_fields"), "sys_id");
      assert.deepEqual(JSON.parse(init.body), { work_notes: note });
      return reply(receipt);
    };
    const read = await executeConnector(input.agent, "mcp", "get-" + name, { record }, {
      env: { GH_AW_CONNECTOR_SECRET_APPROVED_SERVICENOW_READER: "synthetic-reader" }, fetchImpl
    });
    assert.equal(read.untrusted, true);
    assert.deepEqual(jsonData(read.data), { "sys-id": record, number: "SYNTHETIC1", state: "2", revision: "17" });
    const stage = await executeConnector(input.effects, "safe-output", operation, args, { staged: true, env: secretFree, fetchImpl });
    assert.deepEqual(jsonData(stage), input.expected[operation]);
    assert.equal(calls, 1);
    for (const invalid of ["", " \t\r\n\f", "x".repeat(4001), "\u00e9".repeat(4001), "\u{1f600}".repeat(4001)]) {
      await assert.rejects(() => executeConnector(input.effects, "safe-output", operation, { record, note: invalid }, { staged: true, env: secretFree, fetchImpl }));
    }
    for (const valid of ["x".repeat(4000), "\u00e9".repeat(4000), "\u{1f600}".repeat(4000), "First\nSecond", "\v", "\u00a0"]) {
      const staged = await executeConnector(input.effects, "safe-output", operation, { record, note: valid }, { staged: true, env: secretFree, fetchImpl });
      assert.equal(staged.request.arguments.note, valid);
    }
    for (const invalid of [other, record.toUpperCase(), "INC0009009", record.slice(0, -1) + "\n"]) {
      await assert.rejects(() => executeConnector(input.agent, "mcp", "get-" + name, { record: invalid }, { env: secretFree, fetchImpl }));
    }
    await assert.rejects(() => executeConnector(input.agent, "mcp", "get-" + name, { record, table: "sys_user" }, { env: secretFree, fetchImpl }));
    assert.equal(calls, 1);
    const write = await executeConnector(input.effects, "safe-output", operation, args, {
      env: { GH_AW_CONNECTOR_SECRET_APPROVED_SERVICENOW_WRITER: "synthetic-writer" }, fetchImpl
    });
    assert.equal(write.untrusted, true);
    assert.deepEqual(jsonData(write.data), { "sys-id": record });
    assert.equal(calls, 2);
    for (const [plan, capability, op, arguments_, limit, body, env] of [
      [input.agent, "mcp", "get-" + name, { record }, 8192, metadata, { GH_AW_CONNECTOR_SECRET_APPROVED_SERVICENOW_READER: "synthetic-reader" }],
      [input.effects, "safe-output", operation, args, 1024, receipt, { GH_AW_CONNECTOR_SECRET_APPROVED_SERVICENOW_WRITER: "synthetic-writer" }],
    ]) {
      for (const size of [limit, limit + 1]) {
        const padded = body + " ".repeat(size - Buffer.byteLength(body));
        const execute = () => executeConnector(plan, capability, op, arguments_, { env, fetchImpl: async () => reply(padded) });
        if (size === limit) await execute();
        else await assert.rejects(execute);
      }
      for (const [bad, status] of [
        ['{"error":{"message":"private-fixture"}}', 200],
        [body.replace(record, other), 200],
        [body.replace('"' + record + '"', "17"), 200],
        ["{}", 200],
        ["", 204],
        ['{"error":{"message":"private-fixture"}}', 403],
        ['{"error":{"message":"private-fixture"}}', 429],
        ['{"error":{"message":"private-fixture"}}', 500],
        ["", 307],
      ]) {
        let failures = 0;
        await assert.rejects(() => executeConnector(plan, capability, op, arguments_, {
          env, fetchImpl: async () => { failures++; return status === 204 ? new Response(null, { status }) : reply(bad, status); }
        }), error => !error.message.includes("private-fixture"));
        assert.equal(failures, 1, "failed requests must not be retried");
      }
    }
    for (const body of [
      metadata.replace(',"state":"2"', ""),
      metadata.replace('"state":"2"', '"state":""'),
      metadata.replace('"state":"2"', '"state":{"private":"must-not-project"}'),
      metadata.replace('"sys_mod_count":"17"', '"sys_mod_count":17'),
    ]) {
      await assert.rejects(() => executeConnector(input.agent, "mcp", "get-" + name, { record }, {
        env: { GH_AW_CONNECTOR_SECRET_APPROVED_SERVICENOW_READER: "synthetic-reader" }, fetchImpl: async () => reply(body)
      }));
    }
    const adaptedRequire = module => module.startsWith("./") ? require(join(input.setup, module)) : require(module);
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
    const stagedProcess = {
      execPath: process.execPath,
      env: new Proxy({ RUNNER_TEMP: input.runner }, {
        get(env, key) { assert.equal(key, "RUNNER_TEMP", "staged host must not read writer credentials"); return env[key]; }
      })
    };
    const generated = new Function("item", "require", "process", "temporaryIdMap", input.scripts[operation]);
    const staged = generated({ type: "servicenow_" + operation.replaceAll("-", "_"), ...args }, adaptedRequire, stagedProcess, new Map());
    const expected = input.expected[operation];
    expected.request.arguments = formatConnectorArguments(decodePlan(input.effects), operation, args);
    assert.deepEqual(staged, expected, "the generated effect must stage formatted wire arguments, not the raw Go compatibility intent");
  }
  process.stdout.write(JSON.stringify({ tables: 2, generatedStagedEffects: 2 }));
}
main().catch(error => { console.error(error); process.exitCode = 1; });
`
