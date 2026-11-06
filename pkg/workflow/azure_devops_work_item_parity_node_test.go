//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAzureWorkItemParityBundledNodeEffects(t *testing.T) {
	plan := azureWorkItemParityPlan(t)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	var cases []map[string]any
	for _, test := range []struct {
		operation, input, value, field, patchOp string
	}{
		{"set-work-item-description", "body", "Only the **description** changes.", "System.Description", "add"},
		{"set-work-item-repro-steps", "body", "Only the **repro steps** change.", "Microsoft.VSTS.TCM.ReproSteps", "add"},
		{"set-work-item-state", "state", "Closed", "System.State", "add"},
		{"replace-work-item-tags", "tags", "triaged; backend", "System.Tags", "replace"},
		{"set-work-item-area-path", "area-path", `Approved Project\Backend`, "System.AreaPath", "add"},
		{"set-work-item-iteration-path", "iteration-path", `Approved Project\Sprint 1`, "System.IterationPath", "add"},
		{"create-work-item-with-description", "work-item-type", "User Story", "System.Description", "add"},
		{"create-bug-with-repro-steps", "work-item-type", "Bug", "Microsoft.VSTS.TCM.ReproSteps", "add"},
		{"link-approved-work-items", "link-type", "System.LinkTypes.Hierarchy-Reverse", "", "add"},
	} {
		args := azureDevOpsConsumerArgs()
		args[test.input] = test.value
		if test.input == "work-item-type" {
			delete(args, "work-item")
			args["title"], args["body"] = "Create a carefully scoped Azure work item", "Only this **body** is submitted."
		}
		if test.input == "link-type" {
			args["target-url"] = "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9878"
		}
		intent, err := plan.PrepareSafeOutput(test.operation, args)
		require.NoError(t, err)
		cases = append(cases, map[string]any{
			"operation": test.operation, "input": test.input, "field": test.field,
			"patchOp": test.patchOp, "args": args, "request": intent,
		})
	}
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const { decodePlan, executeConnector } = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const plan = decodePlan(fixture.plan);
assert.deepEqual(Object.keys(plan.spec.credentials), ["writer"]);
(async () => {
  for (const test of fixture.cases) {
    let calls = 0;
    const fetchImpl = async (target, init) => {
      calls++;
      const url = new URL(target);
      assert.equal(url.origin, "https://dev.azure.com");
      assert.equal(url.searchParams.get("api-version"), "7.1");
      assert.equal(init.redirect, "manual");
      assert.equal(init.headers["Content-Type"], "application/json-patch+json");
      let expected, id = 9876;
      if (test.input === "work-item-type") {
        id = 9001;
        assert.equal(init.method, "POST");
        assert.equal(decodeURIComponent(url.pathname),
          "/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workitems/$" + test.args["work-item-type"]);
        expected = [
          {op:"add",path:"/fields/System.Title",value:test.args.title},
          {op:"add",path:"/fields/" + test.field,value:test.args.body},
          {op:"add",path:"/multilineFieldsFormat/" + test.field,value:"Markdown"},
        ];
      } else {
        assert.equal(init.method, "PATCH");
        assert.equal(url.pathname, "/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workitems/9876");
        expected = test.input === "link-type" ?
          [{op:"add",path:"/relations/-",value:{rel:test.args["link-type"],url:test.args["target-url"]}}] :
          [{op:test.patchOp,path:"/fields/" + test.field,value:test.args[test.input]}];
        if (test.input === "body")
          expected.push({op:"add",path:"/multilineFieldsFormat/" + test.field,value:"Markdown"});
      }
      assert.deepEqual(JSON.parse(init.body), expected);
      return Response.json({id,fields:{
        "System.TeamProject":"Approved Project",
        "System.WorkItemType":test.args["work-item-type"],
      },private:"never projected"});
    };
    const noSecrets = new Proxy({}, {get() {throw Error("staging read a credential");}});
    const staged = await executeConnector(fixture.plan, "safe-output", test.operation, test.args, {
      staged:true,env:noSecrets,fetchImpl,
    });
    assert.deepEqual(JSON.parse(JSON.stringify(staged)), {staged:true,request:test.request});
    assert.equal(calls, 0);
    const result = await executeConnector(fixture.plan, "safe-output", test.operation, test.args, {
      env:{GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"fixture-write-token"},fetchImpl,
    });
    assert.equal(result.untrusted, true);
    assert.deepEqual(JSON.parse(JSON.stringify(result.data)), {"work-item":test.input === "work-item-type" ? 9001 : 9876});
    assert.equal(calls, 1, "no read-rewrite or automatic retry");
    const bad = {...test.args, unexpected:"never accepted"};
    await assert.rejects(executeConnector(fixture.plan, "safe-output", test.operation, bad, {
      staged:true,env:noSecrets,fetchImpl,
    }));
    assert.equal(calls, 1);
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "cases": cases})
}

func TestAzureWorkItemParityBundledNodeBodyRead(t *testing.T) {
	_, data, _, err := parseAzureWorkItemParity(t, azureWorkItemParityGrants,
		"tools:\n  azure-devops: [get-work-item-description, get-bug-repro-steps]\n")
	require.NoError(t, err)
	encoded, err := data.Connectors["azure-devops"].Agent.Encode()
	require.NoError(t, err)
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {decodePlan,executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
assert.deepEqual(Object.keys(decodePlan(fixture.plan).spec.credentials), ["rest-reader"]);
(async () => {
  for (const [operation, field] of [
    ["get-work-item-description","System.Description"],
    ["get-bug-repro-steps","Microsoft.VSTS.TCM.ReproSteps"],
  ]) {
    let calls = 0;
    const options = {
      env:{GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_REST_READER:"fixture-read-token"},
      fetchImpl:async (target, init) => {
        calls++;
        const url = new URL(target);
        assert.equal(url.origin, "https://dev.azure.com");
        assert.equal(init.method, "GET");
        assert.equal(url.searchParams.get("fields"), "System.TeamProject,System.Title,System.State," + field);
        return Response.json({id:9876,fields:{
          "System.TeamProject":"Approved Project",
          "System.Title":"Untrusted title",
          "System.State":"Active",
          [field]:"Untrusted body",
          "Custom.Secret":"never projected",
        },url:"https://evil.invalid/must-not-follow"});
      },
    };
    const result = await executeConnector(fixture.plan, "mcp", operation, fixture.args, options);
    assert.equal(result.untrusted,true);
    assert.deepEqual(JSON.parse(JSON.stringify(result.data)), {"work-item":9876,title:"Untrusted title",state:"Active",body:"Untrusted body"});
    assert.equal(calls,1);
    await assert.rejects(executeConnector(fixture.plan, "mcp", operation, fixture.args, {
      ...options,env:{AZURE_DEVOPS_EXT_PAT:"not-a-fallback",SYSTEM_ACCESSTOKEN:"not-a-fallback"},
    }), /credential resolution failed/);
    assert.equal(calls,1);
    await assert.rejects(executeConnector(fixture.plan, "mcp", operation, {...fixture.args,"work-item":"9877"}, {
      ...options,fetchImpl:async () => Response.json({
        id:9876,fields:{"System.TeamProject":"Approved Project","System.Title":"x","System.State":"Active",[field]:"x"},
      }),
    }));
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "args": azureDevOpsConsumerArgs()})
}
