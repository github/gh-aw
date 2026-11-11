//go:build !integration

package workflow

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func TestAzureDevOpsNumericTargetsSkipAreaLookup(t *testing.T) {
	_, data, _ := parseAzureDevOpsConsumer(t, "safe-outputs:\n  azure-devops-comment-on-work-item:\n")
	plan := data.Connectors["azure-devops"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	var cases []map[string]any
	for _, id := range []string{"9876", "9877"} {
		t.Run(id, func(t *testing.T) {
			args := azureDevOpsConsumerArgs()
			args["work-item"], args["text"] = id, "Numeric-string target handoff"
			calls, secrets := 0, 0
			request, err := plan.PrepareSafeOutput("comment-on-work-item", args)
			require.NoError(t, err)
			runtime := connectors.Runtime{
				Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://dev.azure.com"},
				ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
					secrets++
					require.Equal(t, "safe-output", phase)
					require.Equal(t, "APPROVED_AZURE_WRITER", name)
					return "fixture-token", nil
				},
				Client: &http.Client{Transport: connectorSmokeTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					require.Equal(t, http.MethodPost, r.Method, "exact IDs must not trigger an area-path GET")
					require.Equal(t, "/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/"+id+"/comments", r.URL.Path)
					require.Equal(t, "7.1-preview.4", r.URL.Query().Get("api-version"))
					require.Equal(t, "markdown", r.URL.Query().Get("format"))
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"text":"Numeric-string target handoff"}`, string(body))
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"workItemId":` + id + `}`)), Request: r}, nil
				})},
			}
			require.Zero(t, calls)
			require.Zero(t, secrets, "staged preparation cannot resolve a credential")
			_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *request)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, 1, secrets)
			cases = append(cases, map[string]any{"id": id, "args": args, "request": request})
		})
	}
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
(async () => {
  for (const test of fixture.cases) {
    let calls = 0;
    const fetchImpl = async (target,init) => {
      calls++;
      assert.equal(init.method,"POST","exact numeric-string targets must not fetch area paths");
      const url = new URL(target);
      assert.equal(url.pathname,"/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/"+test.id+"/comments");
      assert.equal(url.searchParams.get("format"),"markdown");
      assert.deepEqual(JSON.parse(init.body),{text:"Numeric-string target handoff"});
      return Response.json({workItemId:Number(test.id)});
    };
    const noSecrets = new Proxy({}, {get(){throw Error("staging resolved credentials");}});
    const staged = await executeConnector(fixture.plan,"safe-output","comment-on-work-item",test.args,{staged:true,env:noSecrets,fetchImpl});
    assert.deepEqual(JSON.parse(JSON.stringify(staged)),{staged:true,request:test.request});
    assert.equal(calls,0);
    const result = await executeConnector(fixture.plan,"safe-output","comment-on-work-item",test.args,{
      env:{GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"fixture-token"},fetchImpl,
    });
    assert.deepEqual(JSON.parse(JSON.stringify(result.data)),{"work-item":Number(test.id)});
    assert.equal(calls,1);
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "cases": cases})
}

func TestAzureDevOpsMalformedNumericTargetsFailBeforeCredentials(t *testing.T) {
	for _, target := range []string{"", " ", " 9876 ", "09876", "0", "-1", "1.5", "1e3", "9007199254740993"} {
		t.Run(strconv.Quote(target), func(t *testing.T) {
			source := strings.Replace(azureDevOpsConsumerImport, `work-item: "9876"`, "work-item: "+strconv.Quote(target), 1)
			_, data, _, err := parseConnectorConsumer(t, "azure-target.md",
				awConnectorHeader+source+"safe-outputs:\n  azure-devops-link-work-items:\n---\nUse exact approved targets.\n")
			if err != nil {
				require.ErrorContains(t, err, "resource", "empty grants must reject during compilation")
				return
			}
			plan := data.Connectors["azure-devops"].SafeOutputs
			args := azureDevOpsConsumerArgs()
			args["work-item"] = target
			_, err = plan.PrepareSafeOutput("link-work-items", args)
			require.Error(t, err, "an authored grant is not proof of a valid numeric ID")
			_, err = plan.Execute(t.Context(), connectors.Runtime{
				Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://dev.azure.com"},
				ResolveSecret: func(context.Context, string, string) (string, error) {
					t.Fatal("invalid target resolved a credential")
					return "", nil
				},
				Client: &http.Client{Transport: connectorSmokeTransport(func(*http.Request) (*http.Response, error) {
					t.Fatal("invalid target sent a provider request")
					return nil, nil
				})},
			}, "safe-output", "link-work-items", args)
			require.Error(t, err)
			encoded, err := plan.Encode()
			require.NoError(t, err)
			runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
(async () => {
  for (const staged of [false,true]) {
    const noSecrets = new Proxy({}, {get(){throw Error("invalid target resolved credentials");}});
    await assert.rejects(executeConnector(fixture.plan,"safe-output","link-work-items",fixture.args,{
      staged,env:noSecrets,fetchImpl:async () => {throw Error("invalid target reached provider");},
    }), /argument.*(pattern|bound)/i);
  }
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"plan": encoded, "args": args})
		})
	}
}

func TestAzureDevOpsNumericTargetTypesRemainExact(t *testing.T) {
	_, data, _ := parseAzureDevOpsConsumer(t, "safe-outputs:\n  azure-devops-comment-on-work-item:\n")
	plan := data.Connectors["azure-devops"].SafeOutputs
	for _, target := range []any{9876, nil, []string{"9876"}, "*", "9999", " 9876 ", ""} {
		t.Run(fmt.Sprint(target), func(t *testing.T) {
			args := azureDevOpsConsumerArgs()
			args["work-item"], args["text"] = target, "Exact target boundary"
			_, err := plan.PrepareSafeOutput("comment-on-work-item", args)
			require.Error(t, err, "native normalization must not widen exact connector grants")
		})
	}
}
