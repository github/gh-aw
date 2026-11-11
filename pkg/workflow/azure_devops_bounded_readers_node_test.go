//go:build !integration

package workflow

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAzureBoundedReadersBundledNode(t *testing.T) {
	plan := azureBoundedReaderPlan(t)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	var cases []map[string]any
	for _, fixture := range azureBoundedReaderFixtures(t) {
		result, _, _, err := azureBoundedReaderExecute(t, plan, fixture, fixture.Response)
		require.NoError(t, err)
		var expected any
		require.NoError(t, json.Unmarshal(result.Data, &expected))
		inputs, err := plan.Inputs("mcp", fixture.Operation)
		require.NoError(t, err)
		cases = append(cases, map[string]any{
			"fixture": fixture, "expected": expected, "inputs": inputs,
			"invalid": azureBoundedReaderInvalidResponses(t, fixture), "badArgs": azureBoundedReaderBadArguments(fixture),
		})
	}

	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const { decodePlan, executeConnector } = require(process.argv[1]);
const { plan, cases } = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const decoded = decodePlan(plan);
assert.deepEqual(Object.keys(decoded.spec.credentials).sort(), ["build-reader", "code-reader", "rest-reader"]);
assert.equal(decoded.spec.safeOutputs, undefined);
(async () => {
  for (const test of cases) {
    const f = test.fixture;
    let calls = 0, secrets = 0;
    let response = f.response;
    const env = new Proxy({}, {get(_target, name) {
      secrets++;
      assert.equal(name, "GH_AW_CONNECTOR_SECRET_" + f.secret);
      return "fixture-read-token";
    }});
    const options = {env, fetchImpl: async (target, init) => {
      calls++;
      const url = new URL(target);
      assert.equal(url.origin, "https://dev.azure.com");
      assert.equal(url.pathname, f.path);
      assert.deepEqual(Object.fromEntries(url.searchParams), f.query);
      assert.equal(init.method, "GET");
      assert.equal(init.redirect, "manual");
      assert.equal(init.headers.Authorization, "Bearer fixture-read-token");
      assert.equal(init.body, undefined);
      return Response.json(response);
    }};
    const execute = args => executeConnector(plan, "mcp", f.operation, args, options);
    const result = await execute(f.args);
    assert.equal(result.untrusted, true);
    assert.deepEqual(JSON.parse(JSON.stringify(result.data)), test.expected);
    assert.equal(calls, 1, "fixed reads never follow provider URLs or implicit pages");
    assert.equal(secrets, 1);
    for (const invalid of test.invalid) {
      response = invalid;
      const before = calls;
      await assert.rejects(execute(f.args), undefined, f.operation + " invalid response");
      assert.equal(calls, before + 1);
    }
    response = {...f.response, padding:"x".repeat(262145)};
    await assert.rejects(execute(f.args), undefined, f.operation + " byte limit");
    response = f.response;
    for (const name of Object.keys(test.inputs)) {
      for (const bad of [{not:"primitive"}, "x".repeat(2049), -1]) {
        const before = [calls, secrets];
        await assert.rejects(execute({...f.args, [name]:bad}));
        assert.deepEqual([calls, secrets], before, "invalid argument must not resolve secrets");
      }
    }
    const before = [calls, secrets];
    await assert.rejects(execute({...f.args, url:"https://evil.invalid"}));
    assert.deepEqual([calls, secrets], before);
    for (const args of test.badArgs) {
      await assert.rejects(execute(args));
      assert.deepEqual([calls, secrets], before, "unapproved exact grants must fail before credential access");
    }
    await assert.rejects(executeConnector(plan, "mcp", f.operation, f.args, {
      ...options, env:{
        GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"not-a-fallback",
        GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_READER:"not-a-fallback",
        AZURE_DEVOPS_EXT_PAT:"not-a-fallback", SYSTEM_ACCESSTOKEN:"not-a-fallback",
      },
    }), /credential resolution failed/);
    assert.equal(calls, before[0]);
    if (f.operation.startsWith("list-") || f.operation === "run-work-item-query") {
      const key = f.operation === "run-work-item-query" ? "workItems" : "value";
      response = {...f.response, [key]:[]};
      const empty = await execute(f.args);
      assert.equal(empty.untrusted, true);
      assert.ok(Object.values(empty.data).some(value => Array.isArray(value) && value.length === 0));
    }
    for (const status of [200,302,401,503]) {
      let failedCalls = 0;
      await assert.rejects(executeConnector(plan,"mcp",f.operation,f.args,{
        env,fetchImpl:async () => {
          failedCalls++;
          return new Response("raw text is not a JSON reader response",{
            status,headers:{"Content-Type":"text/plain",Location:"https://evil.invalid/redirect"},
          });
        },
      }));
      assert.equal(failedCalls,1,"raw text/status failures never follow redirects, retry or change credential");
    }
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "cases": cases})
}

func TestAzureBoundedReadersExplicitPagination(t *testing.T) {
	plan := azureBoundedReaderPlan(t)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	var cases []azureBoundedReaderFixture
	for _, fixture := range azureBoundedReaderFixtures(t) {
		fixture.Args = maps.Clone(fixture.Args)
		fixture.Query = maps.Clone(fixture.Query)
		switch fixture.Operation {
		case "list-pipeline-definitions", "list-pipeline-runs":
			fixture.Args["continuation-token"] = "explicit-page-2"
			fixture.Query["continuationToken"] = "explicit-page-2"
		case "list-pull-requests":
			fixture.Args["skip"] = 20
			fixture.Query["$skip"] = "20"
		default:
			continue
		}
		_, calls, _, err := azureBoundedReaderExecute(t, plan, fixture, fixture.Response)
		require.NoError(t, err)
		require.Equal(t, 1, calls)
		cases = append(cases, fixture)
	}
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const {plan,cases} = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
(async () => {
  for (const f of cases) {
    let calls = 0;
    await executeConnector(plan,"mcp",f.operation,f.args,{
      env:{["GH_AW_CONNECTOR_SECRET_" + f.secret]:"fixture-read-token"},
      fetchImpl:async (target) => {
        calls++;
        const url = new URL(target);
        assert.deepEqual(Object.fromEntries(url.searchParams),f.query);
        return Response.json(f.response,{headers:{
          "x-ms-continuationtoken":"never-auto-follow",
          "Link":"<https://evil.invalid/page>; rel=next",
        }});
      },
    });
    assert.equal(calls,1,"only the caller's explicit page is fetched");
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "cases": cases})
}
