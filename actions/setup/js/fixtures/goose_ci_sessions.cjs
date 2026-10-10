// @ts-check

// Sanitized stream shapes from github/gh-aw smoke-goose agent/agent-stdio.log:
// success https://github.com/github/gh-aw/actions/runs/37865761051
// failed shell https://github.com/github/gh-aw/actions/runs/37266019863
// terminal error https://github.com/github/gh-aw/actions/runs/37264898160
// extension startup https://github.com/github/gh-aw/actions/runs/37248341114
// IDs, timestamps, text, arguments and accounting values are replaced.
const message = (id, role, content) => ({
  type: "message",
  message: {
    id,
    role,
    created: 1,
    content,
    metadata: { userVisible: true, agentVisible: true, ...(role === "assistant" ? { inference: { provider: "openai", requestedModel: "gpt-5.4" } } : {}) },
  },
});
const request = (id, name, arguments_, extension, index) => ({
  type: "toolRequest",
  id,
  toolCall: { status: "success", value: { name, arguments: arguments_ } },
  _meta: { "goose.toolCall.providerIndex": index, goose_extension: extension },
});
const response = (id, value) => ({ type: "toolResponse", id, toolResult: { status: "success", value } });
const complete = { type: "complete", total_tokens: 12, input_tokens: 10, output_tokens: 2, cache_read_input_tokens: 4, cache_write_input_tokens: 0, cost_usd: 0 };

const success = [
  message("sanitized-assistant-1", "assistant", [
    request("sanitized-github-call", "github__list_pull_requests", { owner: "sanitized", repo: "repo", perPage: 2 }, "github", 0),
    request("sanitized-shell-call", "shell", { command: "printf sanitized", timeout_secs: 30 }, "developer", 1),
  ]),
  message("sanitized-user-1", "user", [response("sanitized-github-call", { content: [{ type: "text", text: "[]" }] })]),
  message("sanitized-user-2", "user", [response("sanitized-shell-call", { resultType: "complete", content: [{ type: "text", text: "sanitized" }], structuredContent: { stdout: "sanitized", stderr: "", exit_code: 0 }, isError: false })]),
  message("sanitized-assistant-2", "assistant", [{ type: "text", text: "Done" }]),
  message("sanitized-assistant-2", "assistant", [{ type: "text", text: "." }]),
  complete,
];

const failedTool = [
  message("sanitized-assistant-1", "assistant", [request("sanitized-shell-call", "shell", { command: "false", timeout_secs: 30 }, "developer", 0)]),
  message("sanitized-user-1", "user", [
    response("sanitized-shell-call", {
      resultType: "complete",
      content: [{ type: "text", text: "sanitized failure\n\nCommand exited with code 1", annotations: { priority: 0 } }],
      structuredContent: { stdout: "sanitized failure", stderr: "", exit_code: 1 },
      isError: true,
    }),
  ]),
  complete,
];

const terminalFailure = [
  success[0],
  success[1],
  success[2],
  { type: "error", error: "Ran into this error: Authentication error: Status: 403 Forbidden. Response: Maximum AI credits exceeded.\n\nPlease retry if you think this is a transient or recoverable error." },
];

const startupFailure = "Warning: Failed to start extension 'github' (sanitized transport failure)\nWarning: Failed to start extension 'safeoutputs' (sanitized transport failure)";

module.exports = { success, failedTool, terminalFailure, startupFailure };
