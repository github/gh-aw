package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/github/gh-aw/pkg/connectors"
)

type connectorDevelopmentPrepared struct {
	Arguments        map[string]any                `json:"arguments"`
	Intent           *connectors.SafeOutputRequest `json:"intent,omitempty"`
	ContentFormatted bool                          `json:"contentFormatted"`
}

const connectorDevelopmentNodeScript = `
const fs = require("node:fs");
const { join, dirname } = require("node:path");
const blocked = () => { throw new Error("offline conformance forbids credentials and requests"); };
const diagnostic = () => { process.stderr.write("Connector publication diagnostic\n"); };
global.core = {
  debug: diagnostic, info: diagnostic, notice: diagnostic, warning: diagnostic, error: diagnostic,
  setFailed: blocked, setSecret: blocked, getIDToken: blocked,
};
global.fetch = async () => blocked();
const runtime = require(process.argv[1]);
const input = JSON.parse(fs.readFileSync(0, "utf8"));
async function main() {
  const snapshot = runtime.decodePlan(input.plan);
  let args = input.arguments;
  let intent;
  let contentFormatted = false;
  if (input.capability === "safe-output") {
    const annotated = Object.values(snapshot.spec.safeOutputs[input.name].inputs || {}).some(value => value.sanitize);
    if (annotated !== input.formatContent) throw new Error("content annotation mismatch");
    if (annotated) {
      const { formatConnectorArguments } = require(join(dirname(process.argv[1]), "connector_content.cjs"));
      args = formatConnectorArguments(snapshot, input.name, args, { config: {} });
      contentFormatted = true;
    }
    const staged = await runtime.executeConnector(input.plan, input.capability, input.name, args,
      { staged: true, env: new Proxy(Object.create(null), { get: blocked, has: blocked, ownKeys: blocked }),
        fetchImpl: async () => blocked() });
    if (!staged?.staged || !staged.request) throw new Error("staging did not return an intent");
    intent = staged.request;
  }
  process.stdout.write(JSON.stringify({ arguments: args, intent, contentFormatted }));
}
main().catch(() => { process.exitCode = 1; });
`

// Only an explicitly trusted runtime path is loaded as code. No connector
// declaration, input, or credential is interpolated into the Node program.
func validateDevelopmentConnectorNode(ctx context.Context, path, encoded, capability, name string, args map[string]any, formatContent bool) (*connectorDevelopmentPrepared, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("resolve --node-runtime path")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("--node-runtime must name a trusted local connector_runtime.cjs regular file")
	}
	if formatContent {
		info, err := os.Stat(filepath.Join(filepath.Dir(path), "connector_content.cjs"))
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("annotated safe-output publication requires the matching sibling connector_content.cjs; supply --node-runtime from a complete trusted matching runtime directory, not an isolated copied runtime file")
		}
	}
	payload, err := json.Marshal(struct {
		Plan          string         `json:"plan"`
		Capability    string         `json:"capability"`
		Name          string         `json:"name"`
		Arguments     map[string]any `json:"arguments"`
		FormatContent bool           `json:"formatContent"`
	}{encoded, capability, name, args, formatContent})
	if err != nil {
		return nil, errors.New("encode Node connector conformance input")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "-e", connectorDevelopmentNodeScript, path)
	command.Env = []string{"NODE_OPTIONS=", "NODE_PATH="}
	command.Stdin = bytes.NewReader(payload)
	var output connectorDevelopmentNodeOutput
	command.Stdout = &output
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		if formatContent {
			return nil, errors.New("shared Node connector publication conformance failed; sanitized titles must contain at least 20 Unicode code points and satisfy their declared bounds, and body bounds must fit the generated footer; ensure Node.js is installed and --node-runtime names a complete matching trusted runtime directory including connector_regex.cjs, connector_content.cjs and its dependencies; no credentials or provider requests were used")
		}
		return nil, errors.New("shared Node connector conformance failed; ensure Node.js is installed and --node-runtime names the matching trusted connector_runtime.cjs with its connector_regex.cjs sibling; no credentials or provider requests were used")
	}
	return decodeDevelopmentConnectorPreparation(output.Bytes(), capability, formatContent)
}

func decodeDevelopmentConnectorPreparation(output []byte, capability string, formatContent bool) (*connectorDevelopmentPrepared, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var prepared *connectorDevelopmentPrepared
	if err := decoder.Decode(&prepared); err != nil || prepared == nil {
		return nil, errors.New("shared Node connector conformance must return bounded JSON-only prepared arguments, without diagnostics or content on stdout")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("shared Node connector conformance returned extra output instead of one prepared JSON object")
	}
	if prepared.Arguments == nil || prepared.ContentFormatted != formatContent ||
		(capability == "safe-output" && prepared.Intent == nil) {
		return nil, errors.New("shared Node connector conformance did not return the required prepared arguments and staged intent")
	}
	return prepared, nil
}

type connectorDevelopmentNodeOutput struct {
	bytes.Buffer
}

func (output *connectorDevelopmentNodeOutput) Write(data []byte) (int, error) {
	if output.Len()+len(data) > 2*connectors.MaxDocumentBytes+4096 {
		return 0, errors.New("prepared connector output exceeds its byte bound")
	}
	return output.Buffer.Write(data)
}
