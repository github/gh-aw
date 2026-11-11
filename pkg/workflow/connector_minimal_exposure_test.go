//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorMinimalReadCommentExposure(t *testing.T) {
	for _, provider := range []string{"linear", "jira", "azure-devops"} {
		t.Run(provider, func(t *testing.T) {
			var data *WorkflowData
			var readers, secrets []string
			switch provider {
			case "linear":
				_, parsed, _, err := parseLinearConnectorWorkflow(t, linearConnectorImport, "tools:\n  linear: [get-issue]\nsafe-outputs:\n  linear-add-comment:\n")
				require.NoError(t, err)
				data, readers, secrets = parsed, []string{"get-issue"}, []string{"LINEAR_READ_API_KEY"}
			case "jira":
				imports := strings.Replace(jiraEmbeddedImport, "      queries: [project = APP ORDER BY created DESC]\n", "", 1)
				_, parsed, _, err := parseJiraMigrationWorkflow(t, imports+"tools:\n  jira:\n    personal: [getJiraIssue]\nsafe-outputs:\n  jira-add-comment:\n")
				require.NoError(t, err)
				data, readers, secrets = parsed, []string{"personal__getJiraIssue"}, []string{"JIRA_MCP_PERSONAL_BASIC_AUTH"}
			case "azure-devops":
				_, parsed, _ := parseAzureDevOpsConsumer(t, "tools:\n  azure-devops: [get-work-item-description]\nsafe-outputs:\n  azure-devops-comment-on-work-item:\n")
				data, readers, secrets = parsed, []string{"get_work_item_description"}, []string{"AZURE_DEVOPS_READ_TOKEN"}
			}
			connector := data.Connectors[provider]
			require.Equal(t, readers, connector.Agent.ToolNames())
			require.Equal(t, secrets, connector.Agent.Secrets())
			effect := "add-comment"
			if provider == "azure-devops" {
				effect = "comment-on-work-item"
			}
			require.Equal(t, []string{effect}, connector.SafeOutputs.SafeOutputNames())
			require.Nil(t, connector.Context)
			require.False(t, usesSafeOutputsArtifactStaging(data.SafeOutputs), "unselected file capabilities must not enable their artifact channel")
			encoded, err := connector.Agent.Encode()
			require.NoError(t, err)
			checkMinimalConnectorAdvertisement(t, encoded, readers)
		})
	}
}

func checkMinimalConnectorAdvertisement(t *testing.T, encoded string, names []string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"plan": encoded, "names": names})
	require.NoError(t, err)
	module, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "-e", `
const assert = require("node:assert/strict");
const {Readable,Writable} = require("node:stream");
const input = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
let output = "";
const sink = new Writable({write(chunk,encoding,next){output += chunk.toString();next();}});
require(process.argv[1]).serve(input.plan,
  Readable.from([JSON.stringify({jsonrpc:"2.0",id:1,method:"tools/list"})+"\n"]),sink,
  {env:new Proxy({}, {get(){throw Error("advertisement read a credential");}}),
   fetchImpl(){throw Error("advertisement made a provider request");}}
).then(()=>{
  const result = JSON.parse(output.trim()).result;
  assert.deepEqual(result.tools.map(tool=>tool.name).sort(),input.names.slice().sort());
}).catch(error=>{console.error(error);process.exitCode=1;});
`, module)
	command.Stdin = bytes.NewReader(payload)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
