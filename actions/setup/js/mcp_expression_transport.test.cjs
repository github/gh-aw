import { describe, expect, it } from "vitest";
import { resolveMCPConfigExpressions } from "./start_mcp_gateway.cjs";

describe("MCP configuration expression transport", () => {
  const expressionA = `GH_AW_MCP_EXPR_${"A".repeat(64)}`;
  const expressionB = `GH_AW_MCP_EXPR_${"B".repeat(64)}`;

  it("resolves parsed URLs, headers and arguments without corrupting JSON bytes", () => {
    const runtimeValue = '"\\\n\r\t`$(touch marker)` ' + `\${${expressionB}}`;
    const config = {
      http: {
        url: `http://localhost:\${${expressionA}}/mcp`,
        headers: { "X-Value": `prefix \${${expressionB}}` },
      },
      stdio: { args: ["--ref", `\${${expressionB}}`], env: { LITERAL: "${OTHER}" } },
    };
    const resolved = resolveMCPConfigExpressions(config, {
      [expressionA]: "8765",
      [expressionB]: runtimeValue,
    });
    expect(resolved.http.url).toBe("http://localhost:8765/mcp");
    expect(resolved.http.headers["X-Value"]).toBe(`prefix ${runtimeValue}`);
    expect(resolved.stdio.args[1]).toBe(runtimeValue);
    expect(resolved.stdio.env.LITERAL).toBe("${OTHER}");
    expect(JSON.parse(JSON.stringify(resolved))).toEqual(resolved);
    expect(config.stdio.args[1]).toBe(`\${${expressionB}}`);
  });

  it("resolves normalized legacy shell references without expanding inserted values again", () => {
    const runtimeValue = '"\\\n`$(literal)` ${GH_AW_MCP_ENV_OTHER}';
    const resolved = resolveMCPConfigExpressions(
      {
        url: "http://host.docker.internal:${GH_AW_MCP_ENV_GH_AW_MCP_SCRIPTS_PORT}",
        args: ["${GH_AW_MCP_ENV_GITHUB_WORKSPACE}", "${GH_AW_MCP_ENV_VALUE}"],
        env: { VALUE: "${GH_AW_MCP_ENV_VALUE}", MISSING: "${GH_AW_MCP_ENV_MISSING}" },
        headers: { "X-Key": "${GH_AW_MCP_SCRIPTS_API_KEY}" },
      },
      { GH_AW_MCP_SCRIPTS_PORT: "8765", GITHUB_WORKSPACE: "/workspace", VALUE: runtimeValue, OTHER: "must not expand" }
    );
    expect(resolved.url).toBe("http://host.docker.internal:8765");
    expect(new URL(resolved.url).port).toBe("8765");
    expect(resolved.args).toEqual(["/workspace", runtimeValue]);
    expect(resolved.env).toEqual({ VALUE: runtimeValue, MISSING: "" });
    expect(resolved.headers["X-Key"]).toBe("${GH_AW_MCP_SCRIPTS_API_KEY}");
    expect(JSON.parse(JSON.stringify(resolved))).toEqual(resolved);
  });

  it("rejects missing compiler bindings without revealing runtime values", () => {
    expect(() => resolveMCPConfigExpressions(`\${${expressionA}}`, {})).toThrow(`MCP configuration environment variable ${expressionA} is not set`);
  });
});
