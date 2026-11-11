//go:build !integration

package workflow

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func azureWorkItemReceiptPlan(t *testing.T, creator string) *connectors.Plan {
	t.Helper()
	grants := azureWorkItemParityGrants + "      work-item-creator: " + creator + "\n"
	selection := azureWorkItemParitySelection + strings.TrimPrefix(azureDevOpsWriteSelection, "safe-outputs:\n") +
		"  azure-devops-link-created-work-item:\n"
	_, data, _, err := parseAzureWorkItemParity(t, grants, selection)
	require.NoError(t, err)
	return data.Connectors["azure-devops"].SafeOutputs
}

func azureWorkItemReceiptCreateArgs(creator string) map[string]any {
	args := azureDevOpsConsumerArgs()
	delete(args, "work-item")
	args["title"], args["temporary_id"] = "Create a scoped work item with an authenticated receipt", "#aw_created12345"
	if creator != "create-work-item" {
		args["work-item-type"], args["body"] = "User Story", "Description for the approved Azure work item."
		if creator == "create-bug-with-repro-steps" {
			args["work-item-type"] = "Bug"
		}
	}
	return args
}

func TestAzureWorkItemParityCreatedResourceChains(t *testing.T) {
	for _, creator := range []string{"create-work-item", "create-work-item-with-description", "create-bug-with-repro-steps"} {
		t.Run(creator, func(t *testing.T) {
			plan := azureWorkItemReceiptPlan(t, creator)
			host := connectors.CreatedResourceHost{Run: "azure-receipt-fixture", Key: []byte(strings.Repeat("x", 32))}
			calls, secrets := 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
				calls++
				if request.Method != http.MethodPost || strings.HasSuffix(request.URL.Path, "/comments") {
					assert.Contains(t, request.URL.Path, "/9001")
				}
				if strings.HasSuffix(request.URL.Path, "/comments") {
					return `{"workItemId":9001}`
				}
				return `{"id":9001,"fields":{"System.TeamProject":"Approved Project","System.WorkItemType":"` +
					map[string]string{"create-work-item": "Task", "create-work-item-with-description": "User Story", "create-bug-with-repro-steps": "Bug"}[creator] + `"}}`
			})
			result, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, creator, azureWorkItemReceiptCreateArgs(creator))
			require.NoError(t, err)
			require.NotEmpty(t, result.CreatedResource)
			assert.JSONEq(t, `{"work-item":9001}`, string(result.Data))
			host.Receipts = []string{result.CreatedResource}
			for _, followup := range []struct {
				operation, input, value string
			}{
				{"update-work-item", "title", "Update only the title of the new Azure work item"},
				{"comment-on-work-item", "text", "Comment only on the newly created Azure work item."},
				{"assign-work-item", "assignee", "approved@example.invalid"},
				{"set-work-item-description", "body", "Only the new item's description."},
				{"set-work-item-repro-steps", "body", "Only the new item's reproduction steps."},
				{"set-work-item-state", "state", "Closed"},
				{"replace-work-item-tags", "tags", "triaged; backend"},
				{"set-work-item-area-path", "area-path", `Approved Project\Backend`},
				{"set-work-item-iteration-path", "iteration-path", `Approved Project\Sprint 1`},
				{"link-created-work-item", "", ""},
			} {
				t.Run(followup.operation, func(t *testing.T) {
					args := azureDevOpsConsumerArgs()
					args["work-item"] = "#aw_created12345"
					if followup.input != "" {
						args[followup.input] = followup.value
					}
					before := calls
					staged, err := plan.PrepareSafeOutputIntent(followup.operation, args)
					require.NoError(t, err, "staging supports the full bounded temporary-ID shape")
					assert.NotEmpty(t, staged)
					assert.Equal(t, before, calls)
					_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, followup.operation, args)
					require.NoError(t, err)
					assert.Equal(t, before+1, calls)
					unbound := connectors.CreatedResourceHost{Run: host.Run, Key: host.Key}
					beforeSecrets := secrets
					_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, unbound, followup.operation, args)
					require.Error(t, err)
					assert.Equal(t, beforeSecrets, secrets)
					assert.Equal(t, before+1, calls)
				})
			}
		})
	}
}

func TestAzureWorkItemParityNumericReceiptRejections(t *testing.T) {
	plan := azureWorkItemReceiptPlan(t, "create-work-item")
	for _, token := range []string{"0", "-1", "9876.0", "9.876e3", `"9001"`, "9007199254740992", "null"} {
		t.Run(token, func(t *testing.T) {
			host := connectors.CreatedResourceHost{Run: "azure-invalid-receipt", Key: []byte(strings.Repeat("x", 32))}
			secrets, calls := 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(*http.Request) string {
				calls++
				return `{"id":` + token + `,"fields":{"System.TeamProject":"Approved Project"}}`
			})
			result, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-work-item", azureWorkItemReceiptCreateArgs("create-work-item"))
			require.Error(t, err)
			if result != nil {
				assert.Empty(t, result.CreatedResource)
			}
			assert.Equal(t, 1, calls)
		})
	}
}

func TestAzureWorkItemParityReceiptProducerIsAuthorBound(t *testing.T) {
	for _, creator := range []string{"comment-on-work-item", "unknown", "${{ github.event.issue.title }}"} {
		grants := azureWorkItemParityGrants + "      work-item-creator: " + creator + "\n"
		_, _, _, err := parseAzureWorkItemParity(t, grants, azureWorkItemParitySelection)
		require.Error(t, err)
	}
	plan := azureWorkItemReceiptPlan(t, "create-work-item")
	encoded, err := plan.Encode()
	require.NoError(t, err)
	decoded, err := connectors.DecodePlan(encoded)
	require.NoError(t, err)
	assert.Equal(t, plan.Digest, decoded.Digest)
}

func TestAzureWorkItemParityBundledNodeReceiptChains(t *testing.T) {
	for _, creator := range []string{"create-work-item", "create-work-item-with-description", "create-bug-with-repro-steps"} {
		t.Run(creator, func(t *testing.T) {
			plan := azureWorkItemReceiptPlan(t, creator)
			host := connectors.CreatedResourceHost{Run: "azure-cross-host-receipt", Key: []byte(strings.Repeat("x", 32))}
			secrets := 0
			kind := map[string]string{"create-work-item": "Task", "create-work-item-with-description": "User Story", "create-bug-with-repro-steps": "Bug"}[creator]
			runtime := azureWorkItemParityRuntime(t, &secrets, func(*http.Request) string {
				return `{"id":9001,"fields":{"System.TeamProject":"Approved Project","System.WorkItemType":"` + kind + `"}}`
			})
			result, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, creator, azureWorkItemReceiptCreateArgs(creator))
			require.NoError(t, err)
			encoded, err := plan.Encode()
			require.NoError(t, err)
			args := azureDevOpsConsumerArgs()
			args["work-item"], args["state"] = "#aw_created12345", "Closed"
			runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
(async () => {
  let calls = 0;
  const env = {
    GH_AW_CONNECTOR_BINDING_RUN:fixture.run,
    GH_AW_CONNECTOR_BINDING_KEY:fixture.key,
    GH_AW_CONNECTOR_BINDING_RECEIPTS:JSON.stringify([fixture.receipt]),
    GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"fixture-write-token",
  };
  const fetchImpl = async (target, init) => {
    calls++;
    const url = new URL(target);
    if (init.method === "PATCH") {
      assert.equal(url.pathname, "/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workitems/9001");
      assert.deepEqual(JSON.parse(init.body), [{op:"add",path:"/fields/System.State",value:"Closed"}]);
    }
    return Response.json({id:9001,fields:{"System.TeamProject":"Approved Project","System.WorkItemType":fixture.kind}});
  };
  const result = await executeConnector(fixture.plan, "safe-output", "set-work-item-state", fixture.args, {env,fetchImpl});
  assert.equal(result.data["work-item"], 9001);
  assert.equal(calls,1,"Node accepts Go's authenticated normalized numeric receipt");
  await assert.rejects(executeConnector(fixture.plan, "safe-output", "set-work-item-state", fixture.args, {
    env:{...env,GH_AW_CONNECTOR_BINDING_RECEIPTS:"[]"},fetchImpl,
  }));
  assert.equal(calls,1);
  const created = await executeConnector(fixture.plan, "safe-output", fixture.creator, fixture.createArgs, {
    env:{...env,GH_AW_CONNECTOR_BINDING_RECEIPTS:"[]"},fetchImpl,
  });
  assert.ok(created.createdResource);
  const payload = JSON.parse(Buffer.from(created.createdResource.split(".")[0],"base64url").toString("utf8"));
  assert.equal(payload.Value,"9001","integer receipt IDs normalize to canonical string authority");
  assert.equal(payload.Producer,fixture.creator);
  await executeConnector(fixture.plan, "safe-output", "set-work-item-state", fixture.args, {
    env:{...env,GH_AW_CONNECTOR_BINDING_RECEIPTS:JSON.stringify([created.createdResource])},fetchImpl,
  });
  assert.equal(calls,3);
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{
				"plan": encoded, "run": host.Run, "key": base64.StdEncoding.EncodeToString(host.Key),
				"receipt": result.CreatedResource, "args": args, "creator": creator,
				"createArgs": azureWorkItemReceiptCreateArgs(creator), "kind": kind,
			})
		})
	}
}
