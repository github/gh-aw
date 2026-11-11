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

func TestConnectorPlainBodyPublication(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	_, data, _, err := parseLinearConnectorWorkflow(t, jiraParityImport,
		"safe-outputs:\n  jira-add-comment:\n  jira-update-issue-description:\n")
	require.NoError(t, err)
	jira, err := data.Connectors["jira"].SafeOutputs.Encode()
	require.NoError(t, err)
	zendesk, err := zendeskPlan(t, "safe-output").Encode()
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]string{"jira": jira, "zendesk": zendesk})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", `
const assert = require("node:assert/strict");
const {join, dirname} = require("node:path");
const {decodePlan, executeConnector} = require(process.argv[1]);
const {formatConnectorArguments} = require(join(dirname(process.argv[1]), "connector_content.cjs"));
const plans = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const literal = "Observed x < y && y > z.";
const credentials = {GH_AW_CONNECTOR_SECRET_JIRA_REST_BASIC_AUTH:"synthetic", GH_AW_CONNECTOR_SECRET_ZENDESK_WRITE_OAUTH_TOKEN:"synthetic"};
const secretFree = new Proxy({}, {get() { throw new Error("staging read a credential"); }});
const response = body => new Response(JSON.stringify(body), {status:200, headers:{"content-type":"application/json"}});
async function main() {
  for (const actions of [false, true]) {
    process.env.GH_AW_WORKFLOW_NAME = "Trusted review";
    process.env.GH_AW_WORKFLOW_ID = "trusted-review";
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = actions ? "123" : "";
    for (const footer of [true, false]) {
      for (const text of [literal, "x".repeat(4000), "😀".repeat(4000), "~~~text\none   two\n\n\nthree\n~~~"]) {
        const args = formatConnectorArguments(decodePlan(plans.zendesk), "add-internal-note",
          {subdomain:"example", ticket:"35436", text}, {config:{footer}});
        assert.ok(!args.text.includes("<!--"), "provider must not strip protected attribution");
        assert.ok(args.text.includes("gh-aw-agentic-workflow: Trusted review"));
        assert.ok(Array.from(args.text).length <= 4000);
        assert.ok(!/ {2,}|\n{2,}/.test(args.text), "prepare provider-normalized whitespace before delivery");
        const staged = await executeConnector(plans.zendesk, "safe-output", "add-internal-note", args,
          {staged:true, env:secretFree, fetchImpl:async () => { throw new Error("staging sent HTTP"); }});
        assert.deepEqual(JSON.parse(JSON.stringify(staged.request.arguments)), args);
        let calls = 0;
        const result = await executeConnector(plans.zendesk, "safe-output", "add-internal-note", args,
          {env:credentials, fetchImpl:async (_url, init) => {
            calls++;
            const sent = JSON.parse(init.body);
            assert.equal(sent.ticket.comment.public, false);
            assert.equal(sent.ticket.comment.body, args.text);
            const normalized = sent.ticket.comment.body.replace(/<!--[\s\S]*?-->/g, "").replace(/<\/?[A-Za-z][^>]*>/g, "")
              .replace(/ {2,}/g, " ").replace(/\n{2,}/g, "\n").trim();
            return response({ticket:{id:35436}, audit:{id:70001,ticket_id:35436,created_at:"2026-11-02T06:00:00Z",
              events:[{id:70002,type:"Comment",public:false,body:normalized}]}});
          }});
        assert.equal(calls, 1);
        assert.deepEqual(JSON.parse(JSON.stringify(result.data)), {"ticket-id":35436,"audit-id":70001,"comment-id":70002,public:false});
      }
      for (const [operation, field] of [["add-comment","text"], ["update-issue-description","description"]]) {
        const args = formatConnectorArguments(decodePlan(plans.jira), operation, {issue:"APP-1",[field]:literal}, {config:{footer}});
        assert.ok(args[field].includes(literal), "ADF must preserve literal comparisons and ampersands");
        assert.ok(!args[field].includes("&lt;") && !args[field].includes("&amp;"));
        let calls = 0;
        await executeConnector(plans.jira, "safe-output", operation, args, {env:credentials, fetchImpl:async (_url, init) => {
          calls++;
          const sent = JSON.parse(init.body);
          const adf = operation === "add-comment" ? sent.body : sent.fields.description;
          const text = adf.content[0].content.filter(node => node.type === "text").map(node => node.text).join("\n");
          assert.ok(text.includes(literal));
          assert.ok(text.includes("gh-aw-agentic-workflow: Trusted review"));
          return operation === "add-comment" ? response({id:"10001",body:adf}) : new Response(null,{status:204});
        }});
        assert.equal(calls,1);
      }
    }
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
`, runtime)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
