//go:build !integration

package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const attachmentUUID = "a5cedde4-2dd5-4fcf-befe-fd0977dd3433"
const attachmentURL = "https://dev.azure.com/contoso/_apis/wit/attachments/" + attachmentUUID + "?fileName=sample.png"

func attachmentParityPlan(t *testing.T) (*WorkflowData, *connectors.Plan) {
	t.Helper()
	_, data, _ := parseAzureDevOpsConsumer(t, "safe-outputs:\n  azure-devops-upload-attachment:\n  azure-devops-attach-file-to-work-item:\n")
	encoded, err := data.Connectors["azure-devops"].SafeOutputs.Encode()
	require.NoError(t, err)
	plan, err := connectors.DecodePlan(encoded)
	require.NoError(t, err)
	return data, plan
}

func stageAttachment(t *testing.T, root string, content []byte, extension string) string {
	t.Helper()
	sum := sha256.Sum256(content)
	name := hex.EncodeToString(sum[:]) + extension
	require.NoError(t, os.WriteFile(filepath.Join(root, name), content, 0600))
	return name
}

func attachmentArguments(file string) map[string]any {
	return map[string]any{"organization": "contoso", "project": "33333333-3333-4333-8333-333333333333",
		"file": file, "file-name": "sample.png", "temporary_id": "aw_attach"}
}

func TestAzureAttachmentParityUploadAndReceipt(t *testing.T) {
	data, plan := attachmentParityPlan(t)
	require.True(t, usesSafeOutputsArtifactStaging(data.SafeOutputs))
	require.Nil(t, data.SafeOutputs.UploadArtifact, "binary transport does not enable public upload_artifact")
	root := t.TempDir()
	content := []byte{0, 1, 2, 255, 42}
	name := stageAttachment(t, root, content, ".png")
	calls, secrets := 0, 0
	runtime := connectors.Runtime{Phase: "safe-output", AllowWrites: true, StagedFilesRoot: root, AllowedOrigins: []string{"https://dev.azure.com"},
		ResolveSecret: func(context.Context, string, string) (string, error) { secrets++; return "fixture", nil },
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			response := `{"id":"` + attachmentUUID + `","url":"` + attachmentURL + `"}`
			if request.Method == http.MethodPost {
				require.Equal(t, "application/octet-stream", request.Header.Get("Content-Type"))
				require.Equal(t, content, body)
				require.Equal(t, "sample.png", request.URL.Query().Get("fileName"))
			} else {
				require.Contains(t, string(body), `"rel":"AttachedFile"`)
				require.Contains(t, string(body), strings.Split(attachmentURL, "?")[0])
				response = `{"id":9876,"fields":{"System.TeamProject":"Approved Project"}}`
			}
			return &http.Response{StatusCode: http.StatusCreated, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
		})}}
	host := connectors.CreatedResourceHost{Run: "trusted-files", Key: bytes.Repeat([]byte("k"), 32)}
	result, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "upload-attachment", attachmentArguments(name))
	require.NoError(t, err)
	require.NotEmpty(t, result.CreatedResource)
	host.Receipts = []string{result.CreatedResource}
	args := azureDevOpsConsumerArgs()
	args["attachment-id"], args["comment"] = "#aw_attach", "Reviewed attachment"
	_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "attach-file-to-work-item", args)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, 2, secrets)
	args["attachment-id"] = attachmentUUID
	_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "attach-file-to-work-item", args)
	require.Error(t, err, "returned URL alone must not establish persisted attachment authority")
	require.Equal(t, 2, calls)
	require.Equal(t, 2, secrets)
	checkAttachmentNodeTransport(t, plan, root, name, content)
}

func checkAttachmentNodeTransport(t *testing.T, plan *connectors.Plan, root, name string, content []byte) {
	t.Helper()
	encoded, err := plan.Encode()
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{"plan": encoded, "args": attachmentArguments(name), "root": root, "bytes": base64.StdEncoding.EncodeToString(content)})
	require.NoError(t, err)
	module, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "-e", `
const assert = require("node:assert/strict");
const input = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
const runtime = require(process.argv[1]);
let reads = 0, calls = 0;
const env = {GH_AW_CONNECTOR_BINDING_RUN:"fixture",GH_AW_CONNECTOR_BINDING_KEY:Buffer.alloc(32,107).toString("base64"),GH_AW_CONNECTOR_BINDING_RECEIPTS:"[]",GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER:"fixture"};
(async () => {
  const stage = await runtime.executeConnector(input.plan,"safe-output","upload-attachment",input.args,{staged:true,env:new Proxy({}, {get(){throw Error("staging read a credential");}}),readStagedFile(){throw Error("staging read a file");}});
  assert.equal(stage.staged,true);
  const linux = process.platform === "linux";
  const result = await runtime.executeConnector(input.plan,"safe-output","upload-attachment",input.args,{env,stagedFilesRoot:linux?input.root:"/trusted/files",
    readStagedFile:linux?undefined:(path,max)=>{reads++;assert.equal(path,"/trusted/files/"+input.args.file);assert.equal(max,5242880);return Buffer.from(input.bytes,"base64");},
    fetchImpl:async(url,request)=>{calls++;assert.ok(Buffer.isBuffer(request.body));assert.deepEqual(request.body,Buffer.from(input.bytes,"base64"));assert.equal(request.headers["Content-Type"],"application/octet-stream");return new Response(JSON.stringify({id:"a5cedde4-2dd5-4fcf-befe-fd0977dd3433",url:"`+attachmentURL+`"}),{status:201});}});
  assert.equal(reads,linux?0:1);assert.equal(calls,1);assert.equal(typeof result.createdResource,"string");
})().catch(error=>{console.error(error);process.exitCode=1;});
`, module)
	command.Stdin = bytes.NewReader(payload)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestAzureAttachmentParityArtifactChannel(t *testing.T) {
	compiler, data, path := parseAzureDevOpsConsumer(t, "safe-outputs:\n  staged: true\n  azure-devops-upload-attachment:\n  azure-devops-attach-file-to-work-item:\n")
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	require.Contains(t, output, SafeOutputsUploadArtifactStagingArtifactName)
	require.Contains(t, output, "Upload upload-artifact staging")
	require.Contains(t, output, "upload-artifacts")
	require.NotContains(t, output, "${{ secrets.APPROVED_AZURE_WRITER }}")
	config, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	var tools map[string]any
	require.NoError(t, json.Unmarshal([]byte(config), &tools))
	creator := tools["azure_devops_upload_attachment"].(map[string]any)
	require.Equal(t, "temporary_id", creator["temporary_id_field"])
	policy := creator["staged_files"].(map[string]any)["file"].(map[string]any)
	require.InDelta(t, 5242880, policy["maxBytes"], 0)
}

func TestAzureAttachmentParityRejectsFilesBeforeCredentials(t *testing.T) {
	_, plan := attachmentParityPlan(t)
	root := t.TempDir()
	secrets, calls := 0, 0
	runtime := connectors.Runtime{Phase: "safe-output", AllowWrites: true, StagedFilesRoot: root, AllowedOrigins: []string{"https://dev.azure.com"},
		ResolveSecret: func(context.Context, string, string) (string, error) { secrets++; return "must-not-resolve", nil },
		Client:        &http.Client{Transport: connectorSmokeTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, io.EOF })}}
	for _, name := range []string{
		"../escape.png", "/etc/passwd", strings.Repeat("a", 64) + ".png",
		stageAttachment(t, root, []byte("##vso[task.setvariable variable=secret]"), ".txt"),
		stageAttachment(t, root, make([]byte, 5242881), ".bin"),
	} {
		_, err := plan.Execute(t.Context(), runtime, "safe-output", "upload-attachment", attachmentArguments(name))
		require.Error(t, err)
	}
	name := stageAttachment(t, root, []byte("unchanged"), ".png")
	require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("changed"), 0600))
	_, err := plan.Execute(t.Context(), runtime, "safe-output", "upload-attachment", attachmentArguments(name))
	require.Error(t, err)
	link := strings.Repeat("b", 64) + ".png"
	require.NoError(t, os.Symlink(filepath.Join(root, name), filepath.Join(root, link)))
	_, err = plan.Execute(t.Context(), runtime, "safe-output", "upload-attachment", attachmentArguments(link))
	require.Error(t, err)
	require.Zero(t, secrets)
	require.Zero(t, calls)
}
