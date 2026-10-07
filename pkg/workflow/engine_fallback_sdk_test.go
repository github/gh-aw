//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEngineFallbackSDKRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for harness runtime tests")
	}
	dir := t.TempDir()
	jsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	preload := filepath.Join(dir, "preload.cjs")
	require.NoError(t, os.WriteFile(preload, []byte(`
const fs = require("fs");
const root = process.env.TEST_JS_DIR;
const reflect = require(root + "/awf_reflect.cjs");
reflect.fetchAWFReflect = async () => ({ok: true, reflectData: {endpoints: [
  {provider:"github-copilot", configured:true, port:10002, models:["gpt-5.4"]},
  {provider:"anthropic", configured:true, port:10003, models:["claude-sonnet-4.6"]}
]}});
reflect.waitForProviderListenerReady = async () => ({ok:true});
const sidecar = require(root + "/copilot_sdk_sidecar.cjs");
sidecar.startCopilotSDKServer = async ({env}) => {
  fs.appendFileSync(process.env.TEST_SIDECAR_LOG, JSON.stringify({event:"start",model:env.COPILOT_MODEL,type:env.COPILOT_PROVIDER_TYPE,url:env.COPILOT_PROVIDER_BASE_URL}) + "\n");
  return {};
};
sidecar.stopCopilotSDKServer = async () => {fs.appendFileSync(process.env.TEST_SIDECAR_LOG, '{"event":"stop"}\n');};
`), 0o600))
	stub := filepath.Join(dir, "driver.cjs")
	require.NoError(t, os.WriteFile(stub, []byte(`
const fs = require("fs");
const cfg = JSON.parse(process.env.GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON);
fs.appendFileSync(process.env.TEST_DRIVER_LOG, JSON.stringify({model:cfg.model,provider:cfg.models.find(m=>m.id===cfg.model).provider}) + "\n");
if (cfg.model === "claude-sonnet-4.6") console.log("completed");
else {console.log(JSON.stringify({type:"session.error",data:{status:503}})); process.exitCode=1;}
`), 0o600))
	infoPath := filepath.Join(dir, "aw_info.json")
	require.NoError(t, os.WriteFile(infoPath, []byte(`{"model":"gpt-5.4"}`), 0o600))
	sidecarLog := filepath.Join(dir, "sidecar.jsonl")
	driverLog := filepath.Join(dir, "driver.jsonl")
	cmd := exec.Command(node, "--require", preload, filepath.Join(jsDir, "copilot_harness.cjs"), node, stub, "copilot")
	cmd.Env = append(os.Environ(), "GH_AW_TMP_DIR="+dir, "GITHUB_WORKSPACE="+dir, "GH_AW_ENGINE_CWD="+dir,
		"GH_AW_FALLBACK_MODELS=[\"anthropic/claude-sonnet-4.6\"]", "GH_AW_HARNESS_MAX_RETRIES=0",
		"COPILOT_MODEL=gpt-5.4", "COPILOT_SDK_URI=localhost:19999", "GH_AW_LLM_PROVIDER=github",
		"AWF_REFLECT_ENABLED=1", "GH_AW_PHASE=agent", "GH_AW_SAFE_OUTPUTS=", "GITHUB_ENV=",
		"TEST_JS_DIR="+jsDir, "TEST_SIDECAR_LOG="+sidecarLog, "TEST_DRIVER_LOG="+driverLog)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	drivers, err := os.ReadFile(driverLog)
	require.NoError(t, err)
	require.JSONEq(t, `[{"model":"gpt-5.4","provider":"github-copilot"},{"model":"claude-sonnet-4.6","provider":"anthropic"}]`,
		"["+strings.ReplaceAll(strings.TrimSpace(string(drivers)), "\n", ",")+"]")
	sidecars, err := os.ReadFile(sidecarLog)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(sidecars)), "\n")
	require.Len(t, lines, 4, string(output))
	require.Contains(t, lines[0], `"url":"http://api-proxy:10002"`)
	require.Contains(t, lines[1], `"event":"stop"`)
	require.Contains(t, lines[2], `"url":"http://api-proxy:10003"`)
	require.Contains(t, lines[2], `"type":"anthropic"`)
	require.Contains(t, lines[3], `"event":"stop"`)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	require.Contains(t, string(info), `"model": "anthropic/claude-sonnet-4.6"`)
}
