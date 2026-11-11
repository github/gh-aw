//go:build !integration

package workflow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const azureFullWriteGrants = `      work-item-types: [Task, Bug]
      link-types: [System.LinkTypes.Related]
      repository-id: 55555555-5555-4555-8555-555555555555
      branch: feature/approved
`

func azureFullWritePlan(t *testing.T, grants string, operations ...string) *connectors.Plan {
	t.Helper()
	var selection strings.Builder
	selection.WriteString("safe-outputs:\n")
	for _, operation := range operations {
		selection.WriteString("  azure-devops-" + operation + ":\n")
	}
	_, data, _, err := parseAzureWorkItemParity(t, azureFullWriteGrants+grants, selection.String())
	require.NoError(t, err)
	plan := data.Connectors["azure-devops"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	frozen, err := connectors.DecodePlan(encoded)
	require.NoError(t, err)
	return frozen
}

func TestAzureFullWriteParityLiteralClears(t *testing.T) {
	for _, test := range []struct{ operation, field, patchOp string }{
		{"clear-work-item-description", "System.Description", "add"},
		{"clear-work-item-repro-steps", "Microsoft.VSTS.TCM.ReproSteps", "add"},
		{"clear-work-item-tags", "System.Tags", "replace"},
	} {
		t.Run(test.operation, func(t *testing.T) {
			plan := azureFullWritePlan(t, "", test.operation)
			secrets, calls := 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
				calls++
				assert.Equal(t, []map[string]any{{"op": test.patchOp, "path": "/fields/" + test.field, "value": ""}},
					azureWorkItemParityPatch(t, request), "clearing is literal empty text, not sanitized footer text")
				return `{"id":9876,"fields":{"System.TeamProject":"Approved Project"}}`
			})
			intent, err := plan.PrepareSafeOutput(test.operation, azureDevOpsConsumerArgs())
			require.NoError(t, err)
			assert.Zero(t, calls)
			assert.Zero(t, secrets)
			_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
			args := azureDevOpsConsumerArgs()
			args["body"] = "not accepted by a clearing operation"
			_, err = plan.PrepareSafeOutput(test.operation, args)
			require.Error(t, err)
		})
	}
}

const azureFullWriteStaticPatch = `      create-additional-patch:
        - op: add
          path: /fields/Custom.CostCenter
          value: {literal: CC-42}
        - op: add
          path: /fields/System.AreaPath
          value: {literal: 'Approved Project\Backend'}
        - op: add
          path: /fields/System.IterationPath
          value: {literal: 'Approved Project\Sprint 1'}
        - op: add
          path: /fields/System.AssignedTo
          value: {literal: approved@example.invalid}
        - op: add
          path: /fields/System.Tags
          value: {literal: 'triaged; backend'}
        - op: add
          path: /relations/-
          value:
            literal:
              rel: ArtifactLink
              url: vstfs:///Git/Ref/33333333-3333-4333-8333-333333333333%2F55555555-5555-4555-8555-555555555555%2FGBfeature/approved
              attributes: {name: Branch}
`

func TestAzureFullWriteParityAtomicRichCreation(t *testing.T) {
	for _, creator := range []string{"create-work-item", "create-work-item-with-description", "create-bug-with-repro-steps"} {
		t.Run(creator, func(t *testing.T) {
			plan := azureFullWritePlan(t, azureFullWriteStaticPatch, creator)
			args := azureWorkItemReceiptCreateArgs(creator)
			if creator == "create-work-item-with-description" {
				args["work-item-type"] = "Task"
			}
			secrets, calls := 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
				calls++
				assert.Equal(t, http.MethodPost, request.Method)
				patch := azureWorkItemParityPatch(t, request)
				fields := map[string]any{}
				for _, item := range patch {
					fields[item["path"].(string)] = item["value"]
				}
				assert.Equal(t, "CC-42", fields["/fields/Custom.CostCenter"])
				assert.Equal(t, `Approved Project\Backend`, fields["/fields/System.AreaPath"])
				assert.Equal(t, `Approved Project\Sprint 1`, fields["/fields/System.IterationPath"])
				assert.Equal(t, "approved@example.invalid", fields["/fields/System.AssignedTo"])
				assert.Equal(t, "triaged; backend", fields["/fields/System.Tags"])
				assert.Equal(t, map[string]any{
					"rel": "ArtifactLink", "url": "vstfs:///Git/Ref/33333333-3333-4333-8333-333333333333%2F55555555-5555-4555-8555-555555555555%2FGBfeature/approved",
					"attributes": map[string]any{"name": "Branch"},
				}, fields["/relations/-"])
				kind := "Task"
				if creator == "create-bug-with-repro-steps" {
					kind = "Bug"
				}
				return `{"id":9001,"fields":{"System.TeamProject":"Approved Project","System.WorkItemType":"` + kind + `"}}`
			})
			_, err := plan.Execute(t.Context(), runtime, "safe-output", creator, args)
			require.NoError(t, err)
			assert.Equal(t, 1, calls, "all author-owned rich fields are part of the one create request")
			args["patch"] = []any{map[string]any{"op": "add", "path": "/fields/Custom.Injected", "value": "no"}}
			_, err = plan.PrepareSafeOutput(creator, args)
			require.Error(t, err, "agents cannot submit custom field paths or patch arrays")
		})
	}
	for _, field := range []string{"/fields/System.Title", "/fields/system.title", "/fields/System.Description"} {
		t.Run("collision/"+field, func(t *testing.T) {
			grants := "      create-additional-patch:\n        - op: add\n          path: " + field + "\n          value: {literal: override}\n"
			_, _, _, err := parseAzureWorkItemParity(t, azureFullWriteGrants+grants,
				"safe-outputs:\n  azure-devops-create-work-item-with-description:\n")
			require.Error(t, err)
		})
	}
}

func TestAzureFullWriteParityNumericLinkPreflights(t *testing.T) {
	for _, operation := range []string{"link-work-item-by-id", "link-work-item-by-id-with-comment"} {
		t.Run(operation, func(t *testing.T) {
			plan := azureFullWritePlan(t, "", operation)
			args := azureDevOpsConsumerArgs()
			args["target-work-item"], args["link-type"] = "9877", "System.LinkTypes.Related"
			if strings.HasSuffix(operation, "with-comment") {
				args["comment"] = "A literal link comment: ${{ data }} $(not-code)."
			}
			for _, failure := range []string{"", "wrong-source-id", "wrong-target-id", "wrong-source-project", "wrong-target-project", "missing-target"} {
				t.Run(failure, func(t *testing.T) {
					calls, secrets := 0, 0
					runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
						calls++
						id, project := path.Base(request.URL.Path), "Approved Project"
						if request.Method == http.MethodGet {
							assert.Equal(t, "System.TeamProject", request.URL.Query().Get("fields"))
							if failure == "missing-target" && id == "9877" {
								return `{}`
							}
							if (failure == "wrong-source-id" && id == "9876") || (failure == "wrong-target-id" && id == "9877") {
								id = "9877"
								if failure == "wrong-target-id" {
									id = "9876"
								}
							}
							if (failure == "wrong-source-project" && id == "9876") || (failure == "wrong-target-project" && id == "9877") {
								project = "Wrong Project"
							}
						} else {
							assert.Empty(t, failure, "no PATCH is permitted after failed evidence")
							value := map[string]any{
								"rel": "System.LinkTypes.Related",
								"url": "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9877",
							}
							if comment, ok := args["comment"]; ok {
								value["attributes"] = map[string]any{"comment": comment}
							}
							assert.Equal(t, []map[string]any{{"op": "add", "path": "/relations/-", "value": value}}, azureWorkItemParityPatch(t, request))
						}
						return fmt.Sprintf(`{"id":%s,"fields":{"System.TeamProject":%q}}`, id, project)
					})
					intent, err := plan.PrepareSafeOutput(operation, args)
					require.NoError(t, err)
					assert.Zero(t, calls, "staging performs no preflight")
					assert.Zero(t, secrets)
					_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
					if failure == "" {
						require.NoError(t, err)
						assert.Equal(t, 3, calls)
					} else {
						require.Error(t, err)
						assert.LessOrEqual(t, calls, 2)
					}
				})
			}
			for _, invalid := range []map[string]any{
				{"target-work-item": "9999"}, {"target-work-item": 9877},
				{"target-work-item": "#aw_unbound"}, {"link-type": "ArtifactLink"},
				{"target-url": "https://evil.invalid"}, {"attributes": map[string]any{"x": "y"}},
			} {
				bad := map[string]any{}
				maps.Copy(bad, args)
				maps.Copy(bad, invalid)
				_, err := plan.PrepareSafeOutput(operation, bad)
				require.Error(t, err)
			}
		})
	}
}

func TestAzureFullWriteParityBothLinkIDsUseAuthenticatedReceipts(t *testing.T) {
	plan := azureFullWritePlan(t, "", "create-work-item", "link-work-item-by-id")
	host := connectors.CreatedResourceHost{Run: "azure-both-link-ids", Key: []byte(strings.Repeat("x", 32))}
	secrets, calls := 0, 0
	nextID := 9000
	runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
		calls++
		id := path.Base(request.URL.Path)
		if request.Method == http.MethodPost {
			nextID++
			id = strconv.Itoa(nextID)
		}
		if request.Method == http.MethodPatch {
			assert.Equal(t, "9001", id)
			patch := azureWorkItemParityPatch(t, request)
			assert.Equal(t, "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9002",
				patch[0]["value"].(map[string]any)["url"])
		}
		return fmt.Sprintf(`{"id":%s,"fields":{"System.TeamProject":"Approved Project"}}`, id)
	})
	for _, temporaryID := range []string{"#aw_source", "#aw_target"} {
		args := azureWorkItemReceiptCreateArgs("create-work-item")
		args["temporary_id"] = temporaryID
		result, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-work-item", args)
		require.NoError(t, err)
		host.Receipts = append(host.Receipts, result.CreatedResource)
	}
	args := azureDevOpsConsumerArgs()
	args["work-item"], args["target-work-item"], args["link-type"] = "#aw_source", "#aw_target", "System.LinkTypes.Related"
	_, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "link-work-item-by-id", args)
	require.NoError(t, err)
	assert.Equal(t, 5, calls, "two creates then two membership reads and one relation append")
	for _, untrusted := range []connectors.CreatedResourceHost{
		{Run: host.Run, Key: host.Key},
		{Run: "other-run", Key: host.Key, Receipts: host.Receipts},
		{Run: host.Run, Key: []byte(strings.Repeat("z", 32)), Receipts: host.Receipts},
		{Run: host.Run, Key: host.Key, Receipts: host.Receipts[:1]},
	} {
		before := secrets
		_, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, untrusted, "link-work-item-by-id", args)
		require.Error(t, err)
		assert.Equal(t, before, secrets)
		assert.Equal(t, 5, calls)
	}
	encoded, err := plan.Encode()
	require.NoError(t, err)
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
(async () => {
  let calls = 0;
  const env = {
    GH_AW_CONNECTOR_BINDING_RUN:fixture.run,
    GH_AW_CONNECTOR_BINDING_KEY:fixture.key,
    GH_AW_CONNECTOR_BINDING_RECEIPTS:JSON.stringify(fixture.receipts),
    GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"fixture-write-token",
  };
  const fetchImpl = async (target,init) => {
    calls++;
    const url = new URL(target);
    const id = Number(url.pathname.split("/").pop());
    if (init.method === "PATCH") {
      assert.equal(id,9001);
      assert.equal(JSON.parse(init.body)[0].value.url,
        "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9002");
    } else assert.equal(init.method,"GET");
    return Response.json({id,fields:{"System.TeamProject":"Approved Project"}});
  };
  const noSecrets = new Proxy({}, {get(){throw Error("staging resolved credentials");}});
  await executeConnector(fixture.plan,"safe-output","link-work-item-by-id",fixture.args,{staged:true,env:noSecrets,fetchImpl});
  assert.equal(calls,0);
  await executeConnector(fixture.plan,"safe-output","link-work-item-by-id",fixture.args,{env,fetchImpl});
  assert.equal(calls,3);
  await assert.rejects(executeConnector(fixture.plan,"safe-output","link-work-item-by-id",fixture.args,{
    env:{...env,GH_AW_CONNECTOR_BINDING_RECEIPTS:JSON.stringify(fixture.receipts.slice(0,1))},fetchImpl,
  }));
  assert.equal(calls,3);
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{
		"plan": encoded, "args": args, "run": host.Run,
		"key": base64.StdEncoding.EncodeToString(host.Key), "receipts": host.Receipts,
	})
}

func TestAzureFullWriteParityReviewedBranchArtifact(t *testing.T) {
	plan := azureFullWritePlan(t, "", "link-work-item-to-branch")
	args := azureDevOpsConsumerArgs()
	args["repository-id"], args["branch"] = "55555555-5555-4555-8555-555555555555", "feature/approved"
	for _, failure := range []string{"", "repository-id", "repository-project", "ref-name", "ref-count"} {
		t.Run(failure, func(t *testing.T) {
			calls, secrets := 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
				calls++
				if strings.HasSuffix(request.URL.Path, "/refs") {
					assert.Equal(t, "heads/feature/approved", request.URL.Query().Get("filter"))
					assert.Equal(t, "2", request.URL.Query().Get("$top"))
					name, count := "refs/heads/feature/approved", 1
					if failure == "ref-name" {
						name += "-other"
					}
					if failure == "ref-count" {
						count = 2
					}
					return fmt.Sprintf(`{"count":%d,"value":[{"name":%q}]}`, count, name)
				}
				if strings.Contains(request.URL.Path, "/git/") {
					id, project := args["repository-id"].(string), args["project"].(string)
					if failure == "repository-id" {
						id = "66666666-6666-4666-8666-666666666666"
					}
					if failure == "repository-project" {
						project = "66666666-6666-4666-8666-666666666666"
					}
					return fmt.Sprintf(`{"id":%q,"project":{"id":%q}}`, id, project)
				}
				if request.Method == http.MethodPatch {
					assert.Empty(t, failure, "failed repository/ref evidence blocks mutation")
					assert.Equal(t, []map[string]any{{
						"op": "add", "path": "/relations/-", "value": map[string]any{
							"rel": "ArtifactLink", "url": "vstfs:///Git/Ref/33333333-3333-4333-8333-333333333333%2F55555555-5555-4555-8555-555555555555%2FGBfeature/approved",
							"attributes": map[string]any{"name": "Branch"},
						},
					}}, azureWorkItemParityPatch(t, request))
				}
				return `{"id":9876,"fields":{"System.TeamProject":"Approved Project"}}`
			})
			_, err := plan.Execute(t.Context(), runtime, "safe-output", "link-work-item-to-branch", args)
			if failure == "" {
				require.NoError(t, err)
				assert.Equal(t, 4, calls)
			} else {
				require.Error(t, err)
				assert.LessOrEqual(t, calls, 3)
			}
		})
	}
	encoded, err := plan.Encode()
	require.NoError(t, err)
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
(async () => {
  for (const mismatch of [false,true]) {
    let calls = 0;
    const fetchImpl = async (target,init) => {
      calls++;
      const url = new URL(target);
      if (url.pathname.endsWith("/refs")) {
        assert.equal(url.searchParams.get("filter"),"heads/feature/approved");
        assert.equal(url.searchParams.get("$top"),"2");
        return Response.json({count:1,value:[{name:mismatch ? "refs/heads/feature/unapproved" : "refs/heads/feature/approved"}]});
      }
      if (url.pathname.includes("/git/")) return Response.json({
        id:fixture.args["repository-id"],project:{id:fixture.args.project},
      });
      if (init.method === "PATCH") {
        assert.equal(mismatch,false);
        assert.deepEqual(JSON.parse(init.body),[{
          op:"add",path:"/relations/-",value:{
            rel:"ArtifactLink",
            url:"vstfs:///Git/Ref/33333333-3333-4333-8333-333333333333%2F55555555-5555-4555-8555-555555555555%2FGBfeature/approved",
            attributes:{name:"Branch"},
          },
        }]);
      }
      return Response.json({id:9876,fields:{"System.TeamProject":"Approved Project"}});
    };
    const noSecrets = new Proxy({}, {get(){throw Error("staging resolved credentials");}});
    await executeConnector(fixture.plan,"safe-output","link-work-item-to-branch",fixture.args,{staged:true,env:noSecrets,fetchImpl});
    assert.equal(calls,0);
    const execute = () => executeConnector(fixture.plan,"safe-output","link-work-item-to-branch",fixture.args,{
      env:{GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"fixture-write-token"},fetchImpl,
    });
    if (mismatch) await assert.rejects(execute());
    else await execute();
    assert.equal(calls,mismatch ? 3 : 4);
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "args": args})
}

func TestAzureFullWriteParityBranchRefMembership(t *testing.T) {
	grants := strings.Replace(azureFullWriteGrants, "branch: feature/approved", "branch: main", 1)
	_, data, _, err := parseAzureWorkItemParity(t, grants,
		"safe-outputs:\n  azure-devops-link-work-item-to-branch:\n")
	require.NoError(t, err)
	plan := data.Connectors["azure-devops"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	args := azureDevOpsConsumerArgs()
	args["repository-id"], args["branch"] = "55555555-5555-4555-8555-555555555555", "main"
	cases := []struct {
		Name     string `json:"name"`
		Response string `json:"response"`
		Allowed  bool   `json:"allowed"`
	}{
		{"exact", `{"count":1,"value":[{"name":"refs/heads/main"}]}`, true},
		{"exact-with-sibling", `{"count":2,"value":[{"name":"refs/heads/main"},{"name":"refs/heads/main-fix"}]}`, true},
		{"exact-after-sibling", `{"count":2,"value":[{"name":"refs/heads/main-fix"},{"name":"refs/heads/main"}]}`, true},
		{"empty", `{"count":0,"value":[]}`, false},
		{"prefix-only", `{"count":1,"value":[{"name":"refs/heads/main-fix"}]}`, false},
		{"truncated-without-exact", `{"count":2,"value":[{"name":"refs/heads/main-fix"},{"name":"refs/heads/main-other"}]}`, false},
		{"short-array", `{"count":2,"value":[{"name":"refs/heads/main"}]}`, false},
		{"long-array", `{"count":1,"value":[{"name":"refs/heads/main"},{"name":"refs/heads/main-fix"}]}`, false},
		{"over-limit", `{"count":3,"value":[{"name":"refs/heads/main"},{"name":"refs/heads/main-fix"},{"name":"refs/heads/main-other"}]}`, false},
		{"missing-count", `{"value":[{"name":"refs/heads/main"}]}`, false},
		{"missing-array", `{"count":1}`, false},
		{"malformed-array", `{"count":1,"value":{"name":"refs/heads/main"}}`, false},
		{"wrong-ref", `{"count":1,"value":[{"name":"refs/heads/Main"}]}`, false},
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			reads, writes, secrets := 0, 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
				if request.Method == http.MethodPatch {
					writes++
					assert.True(t, test.Allowed, "failed ref evidence must block the mutation")
					patch := azureWorkItemParityPatch(t, request)
					assert.Equal(t, "vstfs:///Git/Ref/33333333-3333-4333-8333-333333333333%2F55555555-5555-4555-8555-555555555555%2FGBmain",
						patch[0]["value"].(map[string]any)["url"])
				} else {
					reads++
					if strings.HasSuffix(request.URL.Path, "/refs") {
						assert.Equal(t, "heads/main", request.URL.Query().Get("filter"))
						assert.Equal(t, "2", request.URL.Query().Get("$top"))
						return test.Response
					}
					if strings.Contains(request.URL.Path, "/git/") {
						return `{"id":"55555555-5555-4555-8555-555555555555","project":{"id":"33333333-3333-4333-8333-333333333333"}}`
					}
				}
				return `{"id":9876,"fields":{"System.TeamProject":"Approved Project"}}`
			})
			_, err := plan.Execute(t.Context(), runtime, "safe-output", "link-work-item-to-branch", args)
			if test.Allowed {
				require.NoError(t, err)
				assert.Equal(t, 1, writes)
			} else {
				require.Error(t, err)
				assert.Zero(t, writes)
			}
			assert.Equal(t, 3, reads, "one bounded refs page, without retries or pagination")
		})
	}
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
(async () => {
  for (const test of fixture.cases) {
    let reads=0,writes=0;
    const fetchImpl = async (target,init) => {
      const url = new URL(target);
      if (init.method === "PATCH") {
        writes++;
        assert.equal(test.allowed,true,test.name);
        assert.equal(JSON.parse(init.body)[0].value.url,
          "vstfs:///Git/Ref/33333333-3333-4333-8333-333333333333%2F55555555-5555-4555-8555-555555555555%2FGBmain");
      } else {
        reads++;
        if (url.pathname.endsWith("/refs")) {
          assert.equal(url.searchParams.get("filter"),"heads/main");
          assert.equal(url.searchParams.get("$top"),"2");
          return new Response(test.response,{status:200});
        }
        if (url.pathname.includes("/git/")) return Response.json({
          id:fixture.args["repository-id"],project:{id:fixture.args.project},
        });
      }
      return Response.json({id:9876,fields:{"System.TeamProject":"Approved Project"}});
    };
    const execute = () => executeConnector(fixture.plan,"safe-output","link-work-item-to-branch",fixture.args,{
      env:{GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"fixture-write-token"},fetchImpl,
    });
    if (test.allowed) await execute();
    else await assert.rejects(execute(),undefined,test.name);
    assert.equal(reads,3,test.name);
    assert.equal(writes,test.allowed?1:0,test.name);
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "args": args, "cases": cases})
}

const azureFullWriteNarrowingChecks = `      write-checks:
        - inputs:
            organization:
              type: string
              required: true
              maxLength: 64
              pattern: ^[A-Za-z0-9][A-Za-z0-9-]*$
            project:
              type: string
              required: true
              maxLength: 36
              pattern: ^[0-9a-f-]{36}$
            work-item:
              type: string
              required: true
              maxLength: 16
              pattern: ^[1-9][0-9]{0,9}$
          resources:
            organization: organizations
            project: projects
            work-item: work-items
          request:
            endpoint: api
            method: GET
            path: /{organization}/{project}/_apis/wit/workitems/{work-item}
            pathInputs:
              organization: organization
              project: project
              work-item: work-item
            query:
              api-version: {literal: "7.1"}
              fields: {literal: "System.TeamProject,System.Title,System.Tags,System.AreaPath"}
          maxResponseBytes: 131072
          responseRequired: [/id, /fields/System.TeamProject]
          responseResourceInputs:
            /id: work-item
          responseResources:
            /id: work-items
            /fields/System.TeamProject: project-names
          responseTypes:
            /id: integer
          responseWhen:
            all:
              - {path: /fields/System.Title, op: prefix, values: ['Agent: ']}
              - {path: /fields/System.Tags, op: member-prefix, values: [agent-], separator: ';'}
              - {path: /fields/System.AreaPath, op: path-prefix, values: ['Approved Project\Backend']}
`

func TestAzureFullWriteParityAuthorPrefixPoliciesOnlyNarrow(t *testing.T) {
	plan := azureFullWritePlan(t, azureFullWriteNarrowingChecks, "clear-work-item-description")
	for _, failure := range []string{"", "title", "tags", "area-boundary", "id"} {
		t.Run(failure, func(t *testing.T) {
			secrets, calls := 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
				calls++
				if request.Method == http.MethodPatch {
					assert.Empty(t, failure)
					return `{"id":9876,"fields":{"System.TeamProject":"Approved Project"}}`
				}
				fields := map[string]string{
					"System.TeamProject": "Approved Project", "System.Title": "Agent: approved work item",
					"System.Tags": "triaged; agent-reviewed", "System.AreaPath": `Approved Project\Backend\API`,
				}
				id := 9876
				switch failure {
				case "title":
					fields["System.Title"] = "Not an agent title"
				case "tags":
					fields["System.Tags"] = "agentless; triaged"
				case "area-boundary":
					fields["System.AreaPath"] = `Approved Project\BackendOther`
				case "id":
					id = 9877
				}
				body, err := json.Marshal(map[string]any{"id": id, "fields": fields})
				require.NoError(t, err)
				return string(body)
			})
			_, err := plan.Execute(t.Context(), runtime, "safe-output", "clear-work-item-description", azureDevOpsConsumerArgs())
			if failure == "" {
				require.NoError(t, err)
				assert.Equal(t, 2, calls)
			} else {
				require.Error(t, err)
				assert.Equal(t, 1, calls, "a policy mismatch cannot issue the mutation")
			}
		})
	}
	args := azureDevOpsConsumerArgs()
	args["work-item"] = "9999"
	_, err := plan.PrepareSafeOutput("clear-work-item-description", args)
	require.Error(t, err, "matching prefixes never replace exact work-item resource grants")
	encoded, err := plan.Encode()
	require.NoError(t, err)
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
(async () => {
  for (const mismatch of [false,true]) {
    let calls = 0;
    const fetchImpl = async (target,init) => {
      calls++;
      if (init.method === "PATCH") assert.equal(mismatch,false);
      return Response.json({id:9876,fields:{
        "System.TeamProject":"Approved Project","System.Title":"Agent: approved",
        "System.Tags": mismatch ? "not-agent-reviewed" : "triaged; agent-reviewed",
        "System.AreaPath":"Approved Project\\Backend\\API",
      }});
    };
    const noSecrets = new Proxy({}, {get(){throw Error("staging resolved credentials");}});
    await executeConnector(fixture.plan,"safe-output","clear-work-item-description",fixture.args,{staged:true,env:noSecrets,fetchImpl});
    assert.equal(calls,0);
    const execute = () => executeConnector(fixture.plan,"safe-output","clear-work-item-description",fixture.args,{
      env:{GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"fixture-write-token"},fetchImpl,
    });
    if (mismatch) await assert.rejects(execute());
    else await execute();
    assert.equal(calls,mismatch ? 1 : 2);
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "args": azureDevOpsConsumerArgs()})
}

func TestAzureFullWriteParityBundledNodeClearsAndStaticCreate(t *testing.T) {
	operations := []string{"clear-work-item-description", "clear-work-item-repro-steps", "clear-work-item-tags", "create-work-item"}
	plan := azureFullWritePlan(t, azureFullWriteStaticPatch, operations...)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	create := azureWorkItemReceiptCreateArgs("create-work-item")
	delete(create, "temporary_id")
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
(async () => {
  for (const [operation,field,op] of [
    ["clear-work-item-description","System.Description","add"],
    ["clear-work-item-repro-steps","Microsoft.VSTS.TCM.ReproSteps","add"],
    ["clear-work-item-tags","System.Tags","replace"],
    ["create-work-item",null,null],
  ]) {
    let calls = 0;
    const args = field ? fixture.args : fixture.create;
    const fetchImpl = async (target,init) => {
      calls++;
      const patch = JSON.parse(init.body);
      if (field) assert.deepEqual(patch,[{op,path:"/fields/"+field,value:""}]);
      else {
        assert.equal(init.method,"POST");
        assert.equal(patch.length,7);
        const fields = Object.fromEntries(patch.map(item => [item.path,item.value]));
        assert.equal(fields["/fields/Custom.CostCenter"],"CC-42");
        assert.equal(fields["/fields/System.AssignedTo"],"approved@example.invalid");
        assert.equal(fields["/fields/System.Tags"],"triaged; backend");
        assert.equal(fields["/relations/-"].rel,"ArtifactLink");
      }
      return Response.json({id:field ? 9876 : 9001,fields:{"System.TeamProject":"Approved Project"}});
    };
    const noSecrets = new Proxy({}, {get(){throw Error("staging resolved credentials");}});
    await executeConnector(fixture.plan,"safe-output",operation,args,{staged:true,env:noSecrets,fetchImpl});
    assert.equal(calls,0);
    await executeConnector(fixture.plan,"safe-output",operation,args,{
      env:{GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"fixture-write-token"},fetchImpl,
    });
    assert.equal(calls,1);
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "args": azureDevOpsConsumerArgs(), "create": create})
}

func TestAzureFullWriteParityDocumentationCoversNewEffects(t *testing.T) {
	docs, _ := loadAzureDevOpsDocumentation(t)
	seen := map[string]bool{}
	for _, effect := range docs.Effects.Items {
		seen[strings.TrimPrefix(effect.ID, "azure-devops-")] = true
	}
	for _, operation := range []string{
		"clear-work-item-description", "clear-work-item-repro-steps", "clear-work-item-tags",
		"link-work-item-by-id", "link-work-item-by-id-with-comment", "link-work-item-to-branch",
	} {
		assert.True(t, seen[operation], "new capabilities require corresponding XML contracts")
	}
	plan := azureFullWritePlan(t, "", "clear-work-item-description")
	encoded, err := plan.Encode()
	require.NoError(t, err)
	var document map[string]any
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &document))
	assert.NotContains(t, string(raw), "AZURE_DEVOPS_EXT_PAT")
	assert.NotContains(t, string(raw), "SYSTEM_ACCESSTOKEN")
}

func TestAzureFullWriteParityStructuralSchema(t *testing.T) {
	schemaData, err := connectors.Schema("ConnectorSpec")
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(schemaData, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	for _, test := range []struct{ name, grants, selection string }{
		{
			"literal-structured-and-prefixed-values", azureFullWriteStaticPatch,
			"safe-outputs:\n  azure-devops-create-work-item:\n  azure-devops-clear-work-item-description:\n  azure-devops-link-work-item-by-id-with-comment:\n  azure-devops-link-work-item-to-branch:\n",
		},
		{
			"authored-preflight-predicates", azureFullWriteNarrowingChecks,
			"safe-outputs:\n  azure-devops-clear-work-item-description:\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, _, path, err := parseAzureWorkItemParity(t, azureFullWriteGrants+test.grants, test.selection)
			require.NoError(t, err)
			compiler.SetSkipValidation(false)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err, "authored object-array imports must also pass the workflow structural schema")
			encoded, err := data.Connectors["azure-devops"].SafeOutputs.Encode()
			require.NoError(t, err)
			raw, err := base64.StdEncoding.DecodeString(encoded)
			require.NoError(t, err)
			var snapshot map[string]any
			require.NoError(t, json.Unmarshal(raw, &snapshot))
			require.NoError(t, resolved.Validate(snapshot["spec"]), "empty literal strings, structured values and prefixed inputs must pass the generated ConnectorSpec schema")
		})
	}
}
