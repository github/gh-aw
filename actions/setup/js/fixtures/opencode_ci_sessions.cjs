// Sanitized excerpt from https://github.com/github/gh-aw/actions/runs/37248124399
// Agent artifact 11320350295: agent-stdio.log. The agent succeeded; detection failed.
// IDs, timestamps, outcomes and per-step accounting are native. Payloads are examples.
// This is not the full run: its two step reports must not imply 12 observed turns.
const sessionID = "ses_ef68102a3ffeTmwnJYDQFQwPZy";
const opencodeCiExcerpt = [
  {
    type: "tool_use",
    timestamp: 1791160690452,
    sessionID,
    part: {
      type: "tool",
      tool: "bash",
      callID: "call_0ClcuvHj9n34okF4LgTGXbrf",
      state: {
        status: "completed",
        input: { command: "make example" },
        output: "example: build failed\n",
        metadata: { output: "example: build failed\n", exit: 2, truncated: false },
        title: "make example",
        time: { start: 1791160689766, end: 1791160690449 },
      },
      id: "prt_1097f23db001SfI8lKwJLD8s7O",
      sessionID,
      messageID: "msg_1097f2138001g5dK7rQGVLv1Q4",
    },
  },
  {
    type: "tool_use",
    timestamp: 1791160693809,
    sessionID,
    part: {
      type: "tool",
      tool: "safeoutputs_add_comment",
      callID: "call_ebzApcryxtzXvEtFYKrI5XuM",
      state: {
        status: "error",
        input: { body: "Example comment." },
        error: '{"result":"error","error":"MCP rejected example"}',
        time: { start: 1791160693780, end: 1791160693806 },
      },
      id: "prt_1097f3360001rw4zddzf8u0mTN",
      sessionID,
      messageID: "msg_1097f30c8001BV9x3J4owGLpvv",
    },
  },
  {
    type: "step_finish",
    timestamp: 1791160694689,
    sessionID,
    part: {
      id: "prt_1097f379e001Gy4W0W3vZhZ04n",
      reason: "tool-calls",
      snapshot: "19b1896a62b927a9d2b9781911f07b7763fa7f22",
      messageID: "msg_1097f345b001oPJafm2aZQqQjp",
      sessionID,
      type: "step-finish",
      tokens: { total: 23076, input: 131, output: 33, reasoning: 0, cache: { write: 0, read: 22912 } },
      cost: 0,
    },
  },
  {
    type: "text",
    timestamp: 1791160697403,
    sessionID,
    part: {
      id: "prt_1097f41b8001mLg1LobPygUZVA",
      messageID: "msg_1097f37c1001nFS5d46JWLkGYH",
      sessionID,
      type: "text",
      text: "Example finished.\n",
      time: { start: 1791160697272, end: 1791160697400 },
    },
  },
  {
    type: "step_finish",
    timestamp: 1791160697428,
    sessionID,
    part: {
      id: "prt_1097f4251001OUgCgvKScXrd2h",
      reason: "stop",
      snapshot: "19b1896a62b927a9d2b9781911f07b7763fa7f22",
      messageID: "msg_1097f37c1001nFS5d46JWLkGYH",
      sessionID,
      type: "step-finish",
      tokens: { total: 23111, input: 23091, output: 20, reasoning: 0, cache: { write: 0, read: 0 } },
      cost: 0,
    },
  },
];

// Actual logfmt-only diagnostics from run 37865496743, agent artifact 11588512191.
// That artifact has no native JSON session events. Neither error is a terminal record.
const opencodeCiStreamErrors = [
  'timestamp=2026-10-09T00:37:13.776Z level=ERROR run=ab01f139 message="stream error" providerID=awf-proxy modelID=auto session.id=ses_ee1e84b56ffeap38OrBZh2U7qY small=false agent=build mode=primary error.error="AI_APICallError: Too Many Requests"',
  'timestamp=2026-10-09T00:37:19.590Z level=ERROR run=ab01f139 message="stream error" providerID=awf-proxy modelID=auto session.id=ses_ee1e84b56ffeap38OrBZh2U7qY small=true agent=title mode=primary error.error="AI_RetryError: Failed after 3 attempts. Last error: Too Many Requests"',
];

module.exports = { opencodeCiExcerpt, opencodeCiStreamErrors };
