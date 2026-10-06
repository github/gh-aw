import { readFile } from "node:fs/promises";

export const smokeWorkflow = {
  meta: {
    name: "smoke-copilot-dynamic-workflow",
    description: "Verify packaged support files, a durable step, and one Copilot subagent. args: { marker: string }.",
    phases: [{ title: "Package" }, { title: "Subagent" }, { title: "Verify" }],
    argsSchema: {
      type: "object",
      required: ["marker"],
      properties: { marker: { type: "string" } },
    },
  },
  run: async ctx => {
    if (typeof ctx.args?.marker !== "string" || ctx.args.marker !== "GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK") {
      throw new Error("Expected marker GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK");
    }

    ctx.phase("Package");
    const fixture = await ctx.step("package-v1", async () => {
      const contents = await readFile(new URL("./support/.fixture.json", import.meta.url), "utf8");
      const data = JSON.parse(contents);
      if (data.marker !== ctx.args.marker || data.version !== 1) {
        throw new Error("Packaged dynamic workflow fixture does not match");
      }
      return data;
    });
    ctx.log("Loaded packaged module and nested hidden support file");

    ctx.phase("Subagent");
    const response = await ctx.agent(`Return a JSON object with the single property marker equal to ${JSON.stringify(fixture.marker)}. Do not use tools.`, {
      label: "smoke-marker",
      schema: {
        type: "object",
        required: ["marker"],
        properties: { marker: { type: "string", const: fixture.marker } },
      },
    });

    ctx.phase("Verify");
    if (response === null || response?.marker !== fixture.marker) {
      throw new Error("Dynamic workflow subagent did not return the exact marker");
    }
    const result = await ctx.step("result-v1", () => ({
      status: "PASS",
      marker: response.marker,
      packageVersion: fixture.version,
      supportFile: "support/.fixture.json",
      subagents: 1,
    }));
    ctx.log(`PASS: ${result.marker}`);
    return result;
  },
};
