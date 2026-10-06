export const meta = {
  name: "smoke-claude-dynamic",
  description: "Verify a packaged Claude dynamic workflow and its hidden fixture",
  phases: [{ title: "Read packaged fixture" }],
};

if (!args || typeof args.runId !== "string" || !/^\d+$/.test(args.runId)) {
  throw new Error("Pass the GitHub Actions run ID as args.runId");
}

phase("Read packaged fixture");
const result = await agent(
  `Read .claude/workflows/references/smoke-claude-dynamic/.context using the Read tool.
Return its trimmed contents as token and "${args.runId}" as runId.
Do not edit any files, run another agent, or infer the token without reading the file.`,
  {
    schema: {
      type: "object",
      additionalProperties: false,
      required: ["token", "runId"],
      properties: {
        token: { type: "string" },
        runId: { type: "string" },
      },
    },
  }
);

if (!result || result.token !== "gh-aw-claude-dynamic-fixture-v1" || result.runId !== args.runId) {
  throw new Error("The dynamic workflow agent did not read the packaged fixture");
}

return {
  status: "PASS",
  workflow: "smoke-claude-dynamic",
  token: result.token,
  runId: result.runId,
};
