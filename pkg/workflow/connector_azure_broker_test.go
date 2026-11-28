//go:build !integration

package workflow

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func connectorAzureSource(t *testing.T) string {
	t.Helper()
	doc, err := parser.ExtractFrontmatterFromContent("---\n" + awConnectorSource(t) + "---\n")
	require.NoError(t, err)
	tracker := doc.Frontmatter["connectors"].(map[string]any)["tracker"].(map[string]any)
	creds := tracker["credentials"].(map[string]any)
	for _, name := range []string{"reader", "writer"} {
		credential := creds[name].(map[string]any)
		delete(credential, "secret")
		credential["azure-oidc"] = map[string]string{
			"azure-client-id": "${{ vars." + name + "_ID }}",
			"azure-tenant-id": "12345678-1234-1234-1234-123456789012",
			"azure-scope":     "https://api.example.com/.default",
		}
	}
	tracker["mcp-servers"] = map[string]any{"remote": map[string]any{
		"url": "https://api.example.com/mcp", "credential": "reader", "readOnly": true,
		"tools": map[string]any{"native_read": map[string]any{"inputs": map[string]any{}}}, "maxResponseBytes": 65536,
	}}
	source, err := yaml.Marshal(doc.Frontmatter)
	require.NoError(t, err)
	return string(source)
}

func TestConnectorAzureBrokerPhaseIsolation(t *testing.T) {
	const read = "tools:\n  tracker: [get-issue]\n"
	const remote = "tools:\n  tracker: {remote: [native_read]}\n"
	const write = "safe-outputs:\n  environment: connector-publish\n  tracker-add-comment:\n"
	for _, tc := range []struct {
		name, selection string
		reader, writer  bool
	}{
		{"import-only", "", false, false},
		{"reader", read, true, false},
		{"remote", remote, true, false},
		{"writer", write, false, true},
		{"combined", read + write, true, true},
		{"staged", read + "safe-outputs:\n  tracker-add-comment:\n    staged: true\n", true, false},
		{"conditional", "safe-outputs:\n  environment: connector-publish\n  tracker-add-comment:\n    staged: ${{ inputs.stage }}\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+connectorAzureSource(t)+tc.selection+"---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			output, err := c.CompileToYAML(data, root+"/proof.md")
			require.NoError(t, err)
			var workflow struct {
				Jobs map[string]struct {
					Permissions map[string]string
					Environment string
					Steps       []map[string]any
				}
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &workflow))
			total := 0
			for jobID, job := range workflow.Jobs {
				require.Equal(t, tc.writer && jobID == "safe_outputs", job.Environment == "connector-publish", jobID)
				for _, step := range job.Steps {
					run, _ := step["run"].(string)
					if !strings.HasPrefix(run, "node \"$RUNNER_TEMP/gh-aw/actions/connector_azure_oidc.cjs\" start") {
						continue
					}
					total++
					require.Equal(t, "write", job.Permissions["id-token"])
					writer := step["name"] == "Start connector Azure OIDC broker tracker/writer"
					if writer {
						require.Equal(t, "safe_outputs", jobID)
						require.Equal(t, tc.name == "conditional", step["if"] != nil)
					} else {
						require.Equal(t, "agent", jobID)
					}
				}
				usesFederation := jobID == "agent" && tc.reader || jobID == "safe_outputs" && tc.writer
				if jobID != "activation" && jobID != "conclusion" && !usesFederation {
					require.NotEqual(t, "write", job.Permissions["id-token"], jobID)
				}
			}
			want := 0
			if tc.reader {
				want++
			}
			if tc.writer {
				want++
			}
			require.Equal(t, want, total)
			require.Equal(t, want, strings.Count(output, "Stop connector Azure OIDC broker "))
			require.Equal(t, want, strings.Count(output, "GH_AW_CONNECTOR_BROKER_TIMEOUT_MINUTES:"))
			for _, name := range []string{"reader", "writer"} {
				secret := connectorAppSecret("tracker", name)
				require.NotContains(t, output, "${{ secrets."+secret)
				require.NotContains(t, output, "steps."+connectorAppStepID(secret)+".outputs.token")
			}
			agent, err := json.Marshal(workflow.Jobs["agent"].Steps)
			require.NoError(t, err)
			require.NotContains(t, string(agent), "vars.writer_ID")
			require.Equal(t, tc.reader, strings.Contains(string(agent), connectorAzureMount(connectorAppSecret("tracker", "reader"))))
			require.NotContains(t, string(agent), connectorAzureMount(connectorAppSecret("tracker", "writer")))
			require.Equal(t, tc.writer, strings.Contains(output, "vars.writer_ID"))
			require.Equal(t, tc.reader, strings.Contains(string(agent), "privateHostSocketDirs"))
		})
	}
}

func TestConnectorAzureReaderPrivateSocketConfig(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("private connector sockets require Linux")
	}
	for _, engine := range []string{"copilot", "claude", "codex", "gemini", "pi"} {
		t.Run(engine, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			source := strings.Replace(awConnectorHeader, "engine: copilot", "engine: "+engine, 1)
			data, err := c.ParseWorkflowString(source+connectorAzureSource(t)+"tools:\n  tracker: [get-issue]\n---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			output, err := c.CompileToYAML(data, root+"/proof.md")
			require.NoError(t, err)
			var workflow struct {
				Jobs map[string]struct {
					Steps []struct {
						ID  string
						Run string
						Env map[string]string
					}
				}
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &workflow))
			var run string
			for _, step := range workflow.Jobs["agent"].Steps {
				if step.ID == "agentic_execution" {
					run = step.Run
					require.Equal(t, "${{ "+connectorAzureDirectory(connectorAppSecret("tracker", "reader"))+" }}", step.Env[connectorAzurePrivateDirEnv(0)])
				}
			}
			require.Contains(t, run, "privateHostSocketDirs")
			require.Contains(t, run, "--exclude-env "+connectorAzurePrivateDirEnv(0))
			require.Contains(t, run, "/usr/local/bin/awf")
			installed := false
			for _, step := range workflow.Jobs["agent"].Steps {
				if strings.Contains(step.Run, "install_awf_binary.sh") {
					installed = true
					require.NotContains(t, step.Run, "--rootless")
				}
			}
			require.True(t, installed)
			for line := range strings.SplitSeq(run, "\n") {
				if !strings.HasPrefix(line, "node -e ") || !strings.Contains(line, "privateHostSocketDirs") {
					continue
				}
				temp := t.TempDir()
				require.NoError(t, os.Mkdir(filepath.Join(temp, "gh-aw"), 0o700))
				configPath := filepath.Join(temp, "gh-aw", "awf-config.json")
				require.NoError(t, os.WriteFile(configPath, []byte(`{"filesystem":{"allowWrite":["/workspace"]}}`), 0o600))
				directory := filepath.Join(temp, `dir "with $special"`)
				require.NoError(t, os.Mkdir(directory, 0o700))
				listener, err := net.Listen("unix", filepath.Join(directory, "token.sock"))
				require.NoError(t, err)
				defer listener.Close()
				cmd := exec.Command("bash", "-c", line)
				cmd.Env = append(os.Environ(), "RUNNER_TEMP="+temp, connectorAzurePrivateDirEnv(0)+"="+directory)
				require.NoError(t, cmd.Run())
				content, err := os.ReadFile(configPath)
				require.NoError(t, err)
				var config struct {
					Filesystem struct {
						AllowWrite            []string
						PrivateHostSocketDirs []string
					}
				}
				require.NoError(t, json.Unmarshal(content, &config))
				require.Equal(t, []string{directory}, config.Filesystem.PrivateHostSocketDirs)
				require.Equal(t, []string{"/workspace"}, config.Filesystem.AllowWrite)
				cmd = exec.Command("bash", "-c", line)
				cmd.Env = append(os.Environ(), "RUNNER_TEMP="+temp, connectorAzurePrivateDirEnv(0)+"=")
				require.Error(t, cmd.Run())
				require.NoError(t, listener.Close())
				cmd = exec.Command("bash", "-c", line)
				cmd.Env = append(os.Environ(), "RUNNER_TEMP="+temp, connectorAzurePrivateDirEnv(0)+"="+directory)
				require.Error(t, cmd.Run())
				require.NoError(t, os.WriteFile(filepath.Join(directory, "token.sock"), nil, 0o600))
				cmd = exec.Command("bash", "-c", line)
				cmd.Env = append(os.Environ(), "RUNNER_TEMP="+temp, connectorAzurePrivateDirEnv(0)+"="+directory)
				require.Error(t, cmd.Run())
				cmd = exec.Command("bash", "-c", line)
				cmd.Env = append(os.Environ(), "RUNNER_TEMP="+temp, connectorAzurePrivateDirEnv(0)+"=relative")
				require.Error(t, cmd.Run())
				return
			}
			t.Fatal("missing private socket config update")
		})
	}
}

func TestConnectorAzureReaderMultiplePrivateSockets(t *testing.T) {
	var frontmatter map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(connectorAzureSource(t)), &frontmatter))
	connectors := frontmatter["connectors"].(map[string]any)
	connectors["other"] = connectors["tracker"]
	source, err := yaml.Marshal(frontmatter)
	require.NoError(t, err)
	c, root := awConnectorCompiler(t)
	data, err := c.ParseWorkflowString(awConnectorHeader+string(source)+"tools:\n  tracker: [get-issue]\n  other: [get-issue]\n---\nReview.\n", root+"/proof.md")
	require.NoError(t, err)
	output, err := c.CompileToYAML(data, root+"/proof.md")
	require.NoError(t, err)
	for i, alias := range []string{"other", "tracker"} {
		require.Contains(t, output, strings.TrimSpace(formatYAMLEnv("", connectorAzurePrivateDirEnv(i), "${{ "+connectorAzureDirectory(connectorAppSecret(alias, "reader"))+" }}")))
		require.Contains(t, output, "--exclude-env "+connectorAzurePrivateDirEnv(i))
	}
}

func TestConnectorAzureBrokerRejectsUnsafeRoutes(t *testing.T) {
	src := connectorAzureSource(t)
	const selection = "tools:\n  tracker: [get-issue]\nsafe-outputs:\n  tracker-add-comment:\n"
	for _, tc := range []struct{ name, replace, with, extra, err string }{
		{"client-expression", "vars.reader_ID", "github.actor", "", "UUIDs or standalone vars"},
		{"invalid-tenant", "12345678-1234-1234-1234-123456789012", "../common", "", "UUIDs or standalone vars"},
		{"invalid-scope", "https://api.example.com/.default", "dynamic", "", "single literal"},
		{"same-client", "vars.writer_ID", "vars.reader_ID", "", "separate client"},
		{"non-bearer", "scheme: bearer", "scheme: raw", "", "Authorization/bearer"},
		{"disabled", "", "", "sandbox:\n  agent: false\n", "sandbox"},
		{"host-access", "", "", "sandbox:\n  agent:\n    runtime: docker-sudo-iptables\n", "sandbox"},
		{"arc-dind", "", "", "runner:\n  topology: arc-dind\n", "privileged local Docker"},
		{"sibling-mount", "", "", "mcp-servers:\n  sibling:\n    type: stdio\n    container: node:22-alpine\n    mounts: ['${RUNNER_TEMP}:/mount:ro']\n", "sibling"},
		{"gateway-mount", "", "", "sandbox:\n  mcp:\n    mounts: ['${RUNNER_TEMP}:/mount:ro']\n", "trusted MCP gateway"},
		{"container", "", "", "container: ubuntu:latest\n", "custom host access"},
		{"services", "", "", "services:\n  redis:\n    image: redis:latest\n", "custom host access"},
		{"setup-steps", "", "", "jobs:\n  agent:\n    setup-steps:\n      - run: echo setup\n", "authored runner steps"},
		{"job-pre-steps", "", "", "jobs:\n  agent:\n    pre-steps:\n      - run: echo setup\n", "authored runner steps"},
		{"secret-masking", "", "", "secret-masking:\n  steps:\n    - run: echo setup\n", "authored runner steps"},
		{"mcp-scripts", "", "", "mcp-scripts:\n  example:\n    description: example\n    script: return 1;\n", "mcp-scripts"},
		{"old-awf", "", "", "sandbox:\n  agent:\n    version: v0.29.20\n", "private socket isolation"},
		{"latest-awf", "", "", "sandbox:\n  agent:\n    version: latest\n", "pinned AWF release"},
		{"prerelease-awf", "", "", "sandbox:\n  agent:\n    version: v0.30.0-rc.1\n", "pinned AWF release"},
		{"workflow-env", "", "", "env:\n  NODE_OPTIONS: --require=/tmp/hook.js\n", "workflow env"},
		{"engine-env", "engine: copilot", "engine:\n  id: copilot\n  env:\n    NODE_OPTIONS: --require=/tmp/hook.js", "", "engine.env"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			source := awConnectorHeader + src
			if tc.replace != "" {
				source = strings.ReplaceAll(source, tc.replace, tc.with)
			}

			_, err := c.ParseWorkflowString(source+tc.extra+selection+"---\nReview.\n", root+"/proof.md")
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestConnectorAzureBrokerRequiresAgentSandbox(t *testing.T) {
	for _, tc := range []struct {
		name, sandbox, selection string
		wantError                bool
	}{
		{"reader-disabled", "sandbox:\n  agent: false\n", "tools:\n  tracker: [get-issue]\n", true},
		{"remote-disabled", "sandbox:\n  agent: false\n", "tools:\n  tracker: {remote: [native_read]}\n", true},
		{"writer-disabled", "sandbox:\n  agent: false\n", "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n", true},
		{"conditional-disabled", "sandbox:\n  agent: false\n", "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n    staged: ${{ inputs.stage }}\n", true},
		{"staged-disabled", "sandbox:\n  agent: false\n", "safe-outputs:\n  threat-detection: false\n  tracker-add-comment:\n    staged: true\n", false},
		{"import-disabled", "sandbox:\n  agent: false\n", "", false},
		{"writer-default", "", "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n", false},
		{"writer-enabled", "sandbox:\n  agent: awf\n", "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n", false},
		{"reader-minimum-awf", "sandbox:\n  agent:\n    version: v0.29.21\n", "tools:\n  tracker: [get-issue]\n", false},
		{"reader-rootless-cli", "runner:\n  installation: rootless\n", "tools:\n  tracker: [get-issue]\n", false},
		{"import-arc-dind", "runner:\n  topology: arc-dind\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+"strict: false\nfeatures:\n  dangerously-disable-sandbox-agent: true\n"+connectorAzureSource(t)+tc.sandbox+tc.selection+"---\nReview.\n", root+"/proof.md")
			if tc.wantError {
				require.ErrorContains(t, err, "connector Azure OIDC brokers require an enabled sandbox.agent")
				return
			}
			require.NoError(t, err)
			_, err = c.CompileToYAML(data, root+"/proof.md")
			require.NoError(t, err)
		})
	}
}

func TestConnectorAzureWriterRequiresExclusiveEnvironment(t *testing.T) {
	const write = "safe-outputs:\n  tracker-add-comment:\n"
	for _, tc := range []struct{ name, selection, message string }{
		{"missing", write, "dedicated literal"},
		{"dynamic", write + "  environment: ${{ inputs.stage }}\n", "dedicated literal"},
		{"shared", "environment: Publish\n" + write + "  environment: publish\n", "must not share"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+connectorAzureSource(t)+tc.selection+"---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			_, err = c.CompileToYAML(data, root+"/proof.md")
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestConnectorAzureWriterRejectsOtherJobEnvironments(t *testing.T) {
	const write = "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n"
	for _, tc := range []struct {
		name, job, want string
	}{
		{"same name", "environment: publish", "must not share"},
		{"case insensitive", "environment: PUBLISH", "must not share"},
		{"object name", "environment:\n      name: publish", "must not share"},
		{"dynamic name", "environment: ${{ inputs.environment }}", "dynamically select"},
		{"distinct name", "environment: test", ""},
		{"no environment", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			jobs := "jobs:\n  other:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			if tc.job != "" {
				jobs += "    " + tc.job + "\n"
			}
			data, err := c.ParseWorkflowString(awConnectorHeader+connectorAzureSource(t)+jobs+write+"---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			_, err = c.CompileToYAML(data, root+"/proof.md")
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "job other")
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}

func TestConnectorAzureWriterIdentityOnlyForLiveWrites(t *testing.T) {
	const read = "tools:\n  tracker: [get-issue]\n"
	for _, tc := range []struct {
		name, selection string
		forceStaged     bool
		wantError       bool
	}{
		{"live", "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n", false, true},
		{"staged", "safe-outputs:\n  tracker-add-comment:\n    staged: true\n", false, false},
		{"forced", "safe-outputs:\n  tracker-add-comment:\n", true, false},
		{"conditional", "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n    staged: ${{ inputs.stage }}\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			c.forceStaged = tc.forceStaged
			source := strings.ReplaceAll(connectorAzureSource(t), "vars.writer_ID", "vars.reader_ID")
			_, err := c.ParseWorkflowString(awConnectorHeader+source+read+tc.selection+"---\nReview.\n", root+"/proof.md")
			if tc.wantError {
				require.ErrorContains(t, err, "separate client")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConnectorAzureBrokerRejectsWindows(t *testing.T) {
	for _, tc := range []struct{ name, selection, phase string }{
		{"reader", "runs-on: windows-latest\ntools:\n  tracker: [get-issue]\n", "agent"},
		{"writer", "safe-outputs:\n  runs-on: windows-latest\n  environment: publish\n  tracker-add-comment:\n", "safe-output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+connectorAzureSource(t)+tc.selection+"---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			_, err = c.CompileToYAML(data, root+"/proof.md")
			require.ErrorContains(t, err, "connector Azure OIDC "+tc.phase+" requires a non-Windows runner")
		})
	}
}

func TestConnectorAzureWriterRejectsNativeInferenceIdentity(t *testing.T) {
	c, root := awConnectorCompiler(t)
	selection := "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n"
	data, err := c.ParseWorkflowString(awConnectorHeader+connectorAzureSource(t)+selection+"---\nReview.\n", root+"/proof.md")
	require.NoError(t, err)
	data.EngineConfig.Auth = &EngineAuthConfig{
		Type: "github-oidc", Provider: "azure", AzureClientID: "${{ vars.writer_ID }}",
	}
	require.ErrorContains(t, c.validateConnectorAzureBoundary(data), "separate client")
	data.EngineConfig.Auth.AzureClientID = "${{ vars.other_ID }}"
	require.NoError(t, c.validateConnectorAzureBoundary(data))
}
