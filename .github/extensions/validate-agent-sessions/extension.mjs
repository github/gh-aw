import { defineWorkflow, joinSession } from "@github/copilot-sdk/extension";

const { default: validateAgentSessions } = await import("../../../scripts/validate-agent-sessions.mjs");

await joinSession({
  workflows: [
    defineWorkflow({
      meta: {
        name: "validate-agent-sessions",
        description: "Download and replay 50 existing agentic CI sessions, check the canonical/unified format, and persist source-local diagnostics without dispatching CI.",
        phases: [{ title: "Select runs" }, { title: "Download and normalize" }, { title: "Report problems" }],
        argsSchema: {
          type: "object",
          required: ["repo", "repoPath", "outputRoot"],
          properties: {
            repo: { type: "string" },
            repoPath: { type: "string" },
            outputRoot: { type: "string" },
            reuseRoot: { type: "string" },
            count: { type: "integer" },
            days: { type: "integer" },
          },
        },
      },
      run: validateAgentSessions,
    }),
  ],
});
