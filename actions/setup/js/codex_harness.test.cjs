import { describe, it, expect, afterAll } from "vitest";
import { spawnSync as spawnProcessSync } from "child_process";
import { createRequire } from "module";
import fs from "fs";
import path from "path";
import os from "node:os";

const require = createRequire(import.meta.url);
const {
  resolveCodexPromptFileArgs,
  resolveCodexPromptInput,
  buildCodexResumeArgs,
  injectJsonFlag,
  isRateLimitError,
  isTokenPerMinuteRateLimitError,
  isAuthenticationFailedError,
  isMissingApiKeyError,
  isServerError,
  isInvalidModelError,
  isUnsupportedModelToolsError,
  isInvalidRequestError,
  extractInvalidRequestErrorCode,
  isReconnectExhaustedError,
  countPermissionDeniedIssues,
  hasNumerousPermissionDeniedIssues,
  extractDeniedCommands,
  buildMissingToolPermissionIssuePayload,
  buildCodexChildEnv,
  hasCodexAzureOIDCProxy,
  extractPortFromURL,
  extractOpenAIProxyBaseURLFromToml,
  getConfiguredProviderPortFromReflect,
  validateCodexOpenAIBaseURLFromReflect,
  configureCodexProviderFromReflect,
  hasNoopInSafeOutputs,
  resolveRetryConfig,
  resolveContextRebuildCircuitBreakerConfig,
  evaluateContextRebuildCircuitBreaker,
  evaluateContextRebuildCircuitBreakerForAttempt,
  readWorkingSetFromTokenUsage,
  TOKEN_USAGE_PATHS,
  DEFAULT_CONTEXT_REBUILD_FACTOR_LIMIT,
  DEFAULT_CONTEXT_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS,
  DEFAULT_CONTEXT_REBUILD_POLL_INTERVAL_MS,
  DEFAULT_CONTEXT_REBUILD_TERM_GRACE_MS,
  resolvePostResultWatchdogIdleTimeoutMs,
  createMCPCallWatchdog,
  resolveMCPServerToolTimeouts,
  DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS,
  MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS,
  MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS,
  removeCodexRoutingOverrides,
  applyCodexRoutingEffort,
  resolveCodexModelRouting,
  getCodexRoutingFailureCode,
  resolveCodexConfiguredModel,
} = require("./codex_harness.cjs");
const reflectFixtures = require("./fixtures/model_resolution_reflect.cjs");
const { detectNonRetryableHarnessGuard } = require("./harness_retry_guard.cjs");

const agentTempDir = fs.mkdtempSync(path.join(os.tmpdir(), "codex-harness-tests-"));
afterAll(() => fs.rmSync(agentTempDir, { recursive: true, force: true }));
const preloadPath = path.join(agentTempDir, "isolated-audit.cjs");
fs.writeFileSync(
  preloadPath,
  `
const path=require("path");
const context=require(${JSON.stringify(require.resolve("./ai_credits_context.cjs"))});
const original=context.parseMaxAICreditsExceededFromAuditLog;
context.parseMaxAICreditsExceededFromAuditLog=()=>original(path.join(path.dirname(process.env.GH_AW_AGENT_OUTPUT),"sandbox","firewall","audit","log.jsonl"));
`
);

function makeHarnessTempDir(name) {
  fs.mkdirSync(agentTempDir, { recursive: true });
  return fs.mkdtempSync(path.join(agentTempDir, name));
}

function spawnSync(command, args, options) {
  const env = { ...options.env, HOME: path.join(agentTempDir, "home"), AWF_REFLECT_ENABLED: options.env.AWF_REFLECT_ENABLED || "0" };
  if (!env.CODEX_HOME || env.CODEX_HOME === process.env.CODEX_HOME) env.CODEX_HOME = path.join(agentTempDir, "codex-home");
  if (!env.RUNNER_TEMP || env.RUNNER_TEMP === process.env.RUNNER_TEMP) env.RUNNER_TEMP = agentTempDir;
  if (!env.GH_AW_AGENT_OUTPUT || env.GH_AW_AGENT_OUTPUT === process.env.GH_AW_AGENT_OUTPUT) env.GH_AW_AGENT_OUTPUT = path.join(agentTempDir, "agent-output.json");
  env.GH_AW_CODEX_CONTEXT_REBUILD_CIRCUIT_BREAKER = env.GH_AW_CODEX_TOKEN_USAGE_PATH ? "true" : "false";
  env.NODE_OPTIONS = `${env.NODE_OPTIONS || ""} --require ${JSON.stringify(preloadPath)}`.trim();
  return spawnProcessSync(command, args, { ...options, env });
}

function runHarnessFixture(script, { prompt = "fix the bug", args = [], env = {}, reflectData, reflectOk = true, config } = {}) {
  const dir = makeHarnessTempDir("runtime-");
  const executable = path.join(dir, "codex-stub.cjs");
  const promptPath = path.join(dir, "prompt.txt");
  const callsPath = path.join(dir, "calls.jsonl");
  fs.writeFileSync(
    executable,
    `#!${process.execPath}\nconst fs=require("fs");let input="";process.stdin.setEncoding("utf8");process.stdin.on("data",s=>input+=s);process.stdin.on("end",()=>{fs.appendFileSync(process.env.CODEX_HARNESS_STUB_CALLS,JSON.stringify({args:process.argv.slice(2),stdin:input})+"\\n");${script}\n});`
  );
  fs.chmodSync(executable, 0o700);
  fs.writeFileSync(promptPath, prompt);
  const childEnv = {
    ...process.env,
    CODEX_API_KEY: "fake-key",
    AWF_REFLECT_ENABLED: "0",
    RUNNER_TEMP: dir,
    CODEX_HOME: path.join(dir, "codex-home"),
    GH_AW_CODEX_CONTEXT_REBUILD_CIRCUIT_BREAKER: "false",
    GH_AW_HARNESS_MAX_RETRIES: "1",
    GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
    GH_AW_LLM_PROVIDER_EXPLICIT: "0",
    GH_AW_SAFE_OUTPUTS: "",
    CODEX_HARNESS_STUB_CALLS: callsPath,
    ...env,
  };
  if (config) {
    fs.mkdirSync(childEnv.CODEX_HOME, { recursive: true });
    fs.writeFileSync(path.join(childEnv.CODEX_HOME, "config.toml"), config);
  }
  if (reflectData) {
    const reflectPreload = path.join(dir, "reflect-fixture.cjs");
    fs.writeFileSync(
      reflectPreload,
      `const fs=require("fs");const path=require("path");const reflect=require(${JSON.stringify(require.resolve("./awf_reflect.cjs"))});const data=${JSON.stringify(reflectData)};reflect.fetchAWFReflect=async()=>{fs.mkdirSync(path.dirname(reflect.AWF_REFLECT_OUTPUT_PATH),{recursive:true});fs.writeFileSync(reflect.AWF_REFLECT_OUTPUT_PATH,JSON.stringify(data));return {ok:${reflectOk},reflectData:data};};`
    );
    childEnv.NODE_OPTIONS = `${childEnv.NODE_OPTIONS || ""} --require=${JSON.stringify(reflectPreload)}`;
  }
  const result = spawnSync(process.execPath, ["codex_harness.cjs", executable, "exec", ...args, "--prompt-file", promptPath], {
    cwd: path.dirname(require.resolve("./codex_harness.cjs")),
    env: childEnv,
    encoding: "utf8",
    timeout: 15000,
    maxBuffer: 12 * 1024 * 1024,
  });
  const calls = fs.existsSync(callsPath)
    ? fs
        .readFileSync(callsPath, "utf8")
        .trim()
        .split("\n")
        .map(line => JSON.parse(line))
    : [];
  return { result, calls };
}

describe("codex_harness.cjs", () => {
  describe("Azure OIDC", () => {
    const env = { CODEX_API_KEY: "", OPENAI_API_KEY: "", GH_AW_CODEX_OIDC_AUTH: "azure", AWF_REFLECT_ENABLED: "1", GH_AW_LLM_PROVIDER: "openai" };
    const endpoint = { provider: "openai", configured: true, auth_type: "github-oidc/azure", port: 10000, models_url: "http://api-proxy:10000/v1/models", models: ["gpt-5.4"] };
    const config = 'model_provider = "openai-proxy"\n[model_providers.openai-proxy]\nbase_url = "http://172.30.0.30:10000"\nwire_api = "responses"\nrequires_openai_auth = false\n';

    it("runs without API keys through the configured Entra proxy", () => {
      const { result, calls } = runHarnessFixture(
        'const config=fs.readFileSync(process.env.CODEX_HOME+"/config.toml","utf8");if(process.env.CODEX_API_KEY||process.env.OPENAI_API_KEY||config.includes("env_key")||!config.includes("http://api-proxy:10000"))process.exitCode=1;',
        { env, reflectData: { endpoints: [endpoint] }, config }
      );
      expect(result.status).toBe(0);
      expect(calls).toHaveLength(1);
    });

    it.each([
      ["unconfigured", { ...endpoint, configured: false }],
      ["static key", { ...endpoint, auth_type: "static-key" }],
      ["other OIDC provider", { ...endpoint, auth_type: "github-oidc/gcp" }],
      ["other inference provider", { ...endpoint, provider: "github" }],
    ])("refuses to spawn for a %s proxy", (_name, invalidEndpoint) => {
      const { result, calls } = runHarnessFixture("", { env, reflectData: { endpoints: [invalidEndpoint] }, config });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("Azure OIDC requires a configured OpenAI AWF proxy");
      expect(calls).toHaveLength(0);
    });

    it("refuses to spawn when reflection is disabled", () => {
      const { result, calls } = runHarnessFixture("", { env: { ...env, AWF_REFLECT_ENABLED: "0" }, config });
      expect(result.status).toBe(1);
      expect(calls).toHaveLength(0);
    });

    it("does not trust persisted reflection after a failed fresh fetch", () => {
      const { result, calls } = runHarnessFixture("", { env, reflectData: { endpoints: [endpoint] }, reflectOk: false, config });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("Azure OIDC requires a configured OpenAI AWF proxy");
      expect(calls).toHaveLength(0);
    });

    it.each([undefined, '[history]\npersistence = "none"\n'])("refuses to spawn when Codex proxy configuration is missing (%s)", config => {
      const { result, calls } = runHarnessFixture("", { env, reflectData: { endpoints: [endpoint] }, config });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("unable to configure Codex for the Azure OIDC AWF proxy");
      expect(calls).toHaveLength(0);
    });

    it("keeps the API-key requirement for non-OIDC runs", () => {
      const { result, calls } = runHarnessFixture("", { env: { CODEX_API_KEY: "", OPENAI_API_KEY: "" } });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("no API key available");
      expect(calls).toHaveLength(0);
    });

    it("requires the compiler policy and firewall even with OIDC reflection", () => {
      expect(hasCodexAzureOIDCProxy({ endpoints: [endpoint] }, env)).toBe(true);
      expect(hasCodexAzureOIDCProxy({ endpoints: [endpoint] }, { ...env, GH_AW_CODEX_OIDC_AUTH: "" })).toBe(false);
      expect(hasCodexAzureOIDCProxy({ endpoints: [endpoint] }, { ...env, AWF_REFLECT_ENABLED: "0" })).toBe(false);
      expect(hasCodexAzureOIDCProxy({ endpoints: [endpoint] }, { ...env, GH_AW_LLM_PROVIDER: "github" })).toBe(false);
    });

    it("checks the endpoint that will actually be selected", () => {
      const { result, calls } = runHarnessFixture("", { env, reflectData: { endpoints: [{ ...endpoint, auth_type: "static-key" }, endpoint] }, config });
      expect(result.status).toBe(1);
      expect(calls).toHaveLength(0);
    });

    it("accepts a normalized OpenAI provider alias selected by the resolver", () => {
      const aliasedEndpoint = { ...endpoint, provider: " OPENAI " };
      const { result, calls } = runHarnessFixture("", { env, reflectData: { endpoints: [aliasedEndpoint] }, config });
      expect(result.status).toBe(0);
      expect(calls).toHaveLength(1);
    });
  });
  describe("shared model resolution (recorded /reflect from run 38945917145)", () => {
    const tmpDirs = [];
    afterAll(() => tmpDirs.forEach(dir => fs.rmSync(dir, { recursive: true, force: true })));
    const envFor = () => {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "codex-model-resolution-"));
      tmpDirs.push(dir);
      return { GH_AW_TMP_DIR: dir, GH_AW_AWF_CONFIG_PATH: path.join(dir, "missing-awf-config.json") };
    };
    const resolve = (model, reflectData, options = {}) => resolveCodexConfiguredModel({ model, provider: "copilot", reflectData, logger: () => {}, env: envFor(), ...options });
    const withAliases = (reflect, aliases) => ({ ...reflect, model_aliases: { ...reflect.model_aliases, ...aliases } });

    it("resolves a workflow-defined glob alias to the newest live match", async () => {
      const env = envFor();
      const reflect = withAliases(reflectFixtures.ddgReflect(), { luna: ["copilot/gpt-5.6*"] });
      expect(await resolve("copilot/luna", reflect, { env })).toBe("gpt-5.6-luna");
      expect(JSON.parse(fs.readFileSync(path.join(env.GH_AW_TMP_DIR, "agent/model-resolution.json"), "utf8"))).toEqual([
        { engine: "codex", role: "main", configured_model: "copilot/luna", resolved_model: "gpt-5.6-luna", provider: "copilot", status: "resolved", cause: null },
      ]);
      const { resolveConfiguredCopilotModel } = require("./resolve_model_alias.cjs");
      expect(resolveConfiguredCopilotModel({ configuredModel: "luna", aliasMap: reflect.model_aliases, reflectData: reflect })).toBe("gpt-5.6-luna");
    });

    it("passes an exact id through unchanged", async () => {
      expect(await resolve("copilot/gpt-5.6-sol", reflectFixtures.ddgReflect())).toBe("gpt-5.6-sol");
    });

    // gpt-5-mini is both a built-in alias and a live id in the recorded /reflect; the
    // "absent" case removes it from a copy of the recording.
    it("a live exact id wins over an alias with the same name", async () => {
      expect(await resolve("copilot/gpt-5-mini", reflectFixtures.ddgReflect())).toBe("gpt-5-mini");
      const env = envFor();
      expect(await resolve("gpt-5-mini", reflectFixtures.ddgReflect(), { env })).toBe("gpt-5-mini");
      expect(JSON.parse(fs.readFileSync(path.join(env.GH_AW_TMP_DIR, "agent/model-resolution.json"), "utf8"))).toEqual([
        { engine: "codex", role: "main", configured_model: "gpt-5-mini", resolved_model: "gpt-5-mini", provider: "copilot", status: "exact", cause: null },
      ]);
      expect(await resolve("gpt-5-mini", reflectFixtures.withoutCopilotModels(["gpt-5-mini"]))).toBe("gpt-5.4-mini");
    });

    it("keeps copilot/auto as the Copilot gateway sentinel", async () => {
      expect(await resolve("copilot/auto", reflectFixtures.ddgReflect())).toBe("auto");
    });

    it("workflow-defined alias overrides built-in", async () => {
      const reflect = reflectFixtures.withCopilotModels(["claude-opus-6"]);
      expect(await resolve("opus", reflect)).toBe("claude-opus-6");
      expect(await resolve("opus", withAliases(reflect, { opus: ["copilot/claude-opus-5*"] }))).toBe("claude-opus-5.5");
    });

    // Incomplete discovery is derived from the recorded file (models_fetch_complete: false,
    // Copilot models: null); no recorded run with this shape exists.
    it("incomplete discovery passes exact ids through and fails aliases after one refetch", async () => {
      expect(await resolve("copilot/gpt-5.6-sol", reflectFixtures.incompleteDiscovery())).toBe("gpt-5.6-sol");
      let refetches = 0;
      const refetchReflectData = async () => {
        refetches++;
        return reflectFixtures.incompleteDiscovery();
      };
      await expect(resolve("copilot/gpt-5.6", reflectFixtures.incompleteDiscovery(), { refetchReflectData })).rejects.toMatchObject({ name: "ModelAliasResolutionError", reason: "catalog_unavailable" });
      expect(refetches).toBe(1);
    });
  });

  describe("AWF model routing", () => {
    const reflectData = {
      endpoints: [{ provider: "github", configured: true, models: ["gpt-5.6-sol"] }],
      routing: {
        status: "selected",
        selection: { provider: "github", model: "github-copilot/gpt-5.6-sol", wire_model: "gpt-5.6-sol", effort: "xhigh", endpoint: "/responses" },
      },
    };

    it("applies the routed model and reasoning effort over fixed args", () => {
      const result = resolveCodexModelRouting(reflectData, ["exec", "--model", "fixed-model", "-c", 'model_reasoning_effort="low"', "--json", "-"]);
      expect(result.error).toBeNull();
      expect(result.model).toBe("gpt-5.6-sol");
      expect(result.args).toEqual(["exec", "-c", 'model_reasoning_effort="xhigh"', "--model", "gpt-5.6-sol", "--json", "-"]);
    });

    it.each([
      [null, "required model-routing selection"],
      [{ endpoints: reflectData.endpoints, routing: { status: "pending" } }, "pending"],
      [
        {
          ...reflectData,
          endpoints: [{ ...reflectData.endpoints[0], routing_models: [{ model_id: "gpt-5.6-sol", candidate_metadata_complete: true, supported_endpoints: ["/v1/messages"] }] }],
          routing: { ...reflectData.routing, selection: { ...reflectData.routing.selection, endpoint: "/v1/messages" } },
        },
        "advertises endpoints [/v1/messages]",
      ],
      [{ ...reflectData, endpoints: [{ provider: "github", configured: true, models: [] }] }, "unavailable Copilot wire model"],
    ])("fails closed when routing data is invalid: %s", (reflect, error) => {
      expect(resolveCodexModelRouting(reflect, ["exec", "--model", "fixed"]).error).toContain(error);
    });

    it("uses the Responses endpoint for a routed GPT model selected on Chat Completions", () => {
      const reflect = {
        ...reflectData,
        endpoints: [{ ...reflectData.endpoints[0], routing_models: [{ model_id: "gpt-5.6-sol", candidate_metadata_complete: true, supported_endpoints: ["/chat/completions", "/responses"] }] }],
        routing: { ...reflectData.routing, selection: { ...reflectData.routing.selection, endpoint: "/chat/completions" } },
      };
      const result = resolveCodexModelRouting(reflect, ["exec"]);
      expect(result.error).toBeNull();
      expect(result.selection).toMatchObject({ endpoint: "/responses", selected_endpoint: "/chat/completions" });
    });

    it("drops fixed model and reasoning-effort configuration", () => {
      const args = removeCodexRoutingOverrides(["exec", "-m", "fixed", "-c", 'model_reasoning_effort="low"', "--json"]);
      expect(args).toEqual(["exec", "--json"]);
      expect(applyCodexRoutingEffort(["exec", "--json"], "high")).toEqual(["exec", "-c", 'model_reasoning_effort="high"', "--json"]);
    });

    // Derived fixtures: the recorded /reflect from run 38945917145 (routing: null) with an
    // added selected routing block for gpt-5.6-luna, which advertises none..max there.
    const runRoutedHarness = effort => {
      const dir = makeHarnessTempDir("routed-effort-");
      const { result, calls } = runHarnessFixture("process.exit(0);", {
        args: ["--model", "fixed-model", "-c", 'model_reasoning_effort="low"'],
        env: { GH_AW_MODEL_ROUTING: "1", GH_AW_MODEL_AGENT_CODEX: "fixed-model", GH_AW_LLM_PROVIDER: "github", GH_AW_TMP_DIR: dir },
        reflectData: reflectFixtures.withRoutingSelection("gpt-5.6-luna", effort),
      });
      const outcomePath = path.join(dir, "agent", "awf-routing-outcome.json");
      const outcome = fs.existsSync(outcomePath) ? JSON.parse(fs.readFileSync(outcomePath, "utf8")) : null;
      return { result, calls, outcome };
    };

    it.each([
      ["routed_codex_max_starts_with_selected_effort", "max"],
      ["routed_codex_none_starts_with_selected_effort", "none"],
    ])("%s", (_name, effort) => {
      const direct = resolveCodexModelRouting(reflectFixtures.withRoutingSelection("gpt-5.6-luna", effort), ["exec", "--model", "fixed-model", "-c", 'model_reasoning_effort="low"', "--json", "-"]);
      expect(direct.error).toBeNull();
      expect(direct.args).toEqual(["exec", "-c", `model_reasoning_effort="${effort}"`, "--model", "gpt-5.6-luna", "--json", "-"]);

      const { result, calls, outcome } = runRoutedHarness(effort);
      expect(result.status, result.stderr).toBe(0);
      expect(result.stderr).not.toContain("unsupported_effort");
      expect(result.stderr).toContain(`inference routing: mode=awf-routed model=gpt-5.6-luna effort=${effort} endpoint=/responses`);
      expect(calls).toHaveLength(1);
      const efforts = calls[0].args.filter(arg => arg.startsWith("model_reasoning_effort="));
      expect(efforts).toEqual([`model_reasoning_effort="${effort}"`]);
      expect(calls[0].args).toContain("gpt-5.6-luna");
      expect(calls[0].args).not.toContain("fixed-model");
      expect(outcome).toMatchObject({ status: "selected", wire_model: "gpt-5.6-luna", endpoint: "/responses", effort, applied_effort: effort });
      expect(outcome).not.toHaveProperty("failure_code");
    });

    it.each([
      ["AWF /reflect returned unsupported reasoning effort ultra", "unsupported_effort"],
      ["AWF model routing selection has none compatible endpoints", "unsupported_endpoint"],
    ])("classifies Codex routing failure %s", (error, expectedCode) => {
      expect(getCodexRoutingFailureCode(error)).toBe(expectedCode);
    });

    it("routed_codex_out_of_contract_effort_is_rejected", () => {
      const direct = resolveCodexModelRouting(reflectFixtures.withRoutingSelection("gpt-5.6-luna", "ultra"), ["exec"]);
      expect(direct.error).toBe("AWF /reflect returned unsupported reasoning effort ultra");

      const { result, calls, outcome } = runRoutedHarness("ultra");
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain("unsupported reasoning effort ultra; refusing to start Codex");
      expect(calls).toHaveLength(0);
      expect(outcome).toMatchObject({ status: "rejected", failure_code: "unsupported_effort", detail: "AWF /reflect returned unsupported reasoning effort ultra" });
    });

    it("starts Codex with the routed model and effort from AWF reflection", () => {
      const reflect = {
        ...reflectData,
        endpoints: [{ provider: "github", configured: true, port: 10002, base_url: "http://api-proxy:10002", models: ["gpt-5.6-sol"] }],
      };
      const { result, calls } = runHarnessFixture("process.exit(0);", {
        args: ["--model", "fixed-model", "-c", 'model_reasoning_effort="low"'],
        env: { GH_AW_MODEL_ROUTING: "1", GH_AW_MODEL_AGENT_CODEX: "fixed-model", GH_AW_LLM_PROVIDER: "github" },
        reflectData: reflect,
      });
      expect(result.status, result.stderr).toBe(0);
      expect(calls).toHaveLength(1);
      expect(calls[0].args).toContain("--model");
      expect(calls[0].args).toContain("gpt-5.6-sol");
      expect(calls[0].args).toContain('model_reasoning_effort="xhigh"');
      expect(calls[0].args).not.toContain("fixed-model");
      expect(result.stderr).toContain("inference routing: mode=awf-routed model=gpt-5.6-sol effort=xhigh endpoint=/responses");
    });
  });

  describe("MCP call watchdog", () => {
    it("times out only an outstanding MCP call, not other output or completed calls", () => {
      let time = 0;
      const watchdog = createMCPCallWatchdog(120_000, () => time);
      watchdog.observe(JSON.stringify({ type: "item.started", item: { type: "mcp_tool_call", id: "1", name: "search_repositories" } }));
      time = 119_999;
      watchdog.observe(JSON.stringify({ type: "item.completed", item: { type: "agent_message", id: "other" } }));
      expect(watchdog.expired()).toBe(false);
      time = 120_000;
      expect(watchdog.expired()).toBe(true);
      watchdog.observe(JSON.stringify({ type: "item.completed", item: { type: "mcp_tool_call", id: "1" } }));
      expect(watchdog.expired()).toBe(false);
    });

    it("clears failed calls and ignores malformed events", () => {
      let time = 0;
      const watchdog = createMCPCallWatchdog(100, () => time);
      watchdog.observe("{");
      watchdog.observe(JSON.stringify({ type: "item.started", item: { type: "mcp_tool_call", id: "2" } }));
      watchdog.observe(JSON.stringify({ type: "item.failed", item: { type: "mcp_tool_call", id: "2" } }));
      time = 200;
      expect(watchdog.expired()).toBe(false);
    });

    it("uses per-server timeouts after the global timeout override", () => {
      const timeouts = resolveMCPServerToolTimeouts(
        {
          defaults: { mcp_servers: { github: { tool_timeout_sec: 60 }, search: { tool_timeout_sec: 30 } } },
          overrides: { mcp_servers: { github: { tool_timeout_sec: 180 } } },
        },
        90
      );
      expect(timeouts).toEqual({ github: 180, search: 90 });

      let time = 0;
      const watchdog = createMCPCallWatchdog(
        item => timeouts[item.server] * 1000 + 60_000,
        () => time
      );
      watchdog.observe(JSON.stringify({ type: "item.started", item: { type: "mcp_tool_call", id: "1", server: "github" } }));
      time = 239_999;
      expect(watchdog.expired()).toBe(false);
      time = 240_000;
      expect(watchdog.expiredTimeoutMs()).toBe(240_000);
    });

    it("clears an oversized completion from its bounded lifecycle prefix", () => {
      let time = 0;
      const watchdog = createMCPCallWatchdog(100, () => time);
      watchdog.observe(JSON.stringify({ type: "item.started", item: { type: "mcp_tool_call", id: "large" } }));
      time = 100;
      watchdog.observePrefix('{"type":"item.com\\u0070leted","item":{"\\u0069d":"large","type":"mcp_tool_call","server":"github","result":"');
      expect(watchdog.expired()).toBe(false);
    });
  });
  describe("native exec orchestration", () => {
    it("preserves a native argument-parse exit without retrying a deterministic startup error", () => {
      const { result, calls } = runHarnessFixture(`process.stderr.write("error: unexpected argument '--invalid' found\\n\\nUsage: codex exec [OPTIONS] [PROMPT]\\n");process.exit(2);`);
      expect(result.status).toBe(2);
      expect(calls).toHaveLength(1);
      expect(result.stderr).toContain("startup configuration error");
    });

    it("delivers a large leading-hyphen prompt through stdin, not argv or logs", () => {
      const prompt = "--private-task\n" + "x".repeat(2 * 1024 * 1024);
      const { result, calls } = runHarnessFixture("process.exit(0);", { prompt });
      expect(result.status).toBe(0);
      expect(calls[0].stdin).toBe(prompt);
      expect(calls[0].args).toEqual(["exec", "--json", "-"]);
      expect(result.stderr).not.toContain("--private-task");
    });

    it("resumes the exact observed thread without replaying the original instructions", () => {
      const id = "0199a213-81c0-7800-8aa1-bbab2a035a53";
      const { result, calls } = runHarnessFixture(
        `
const n=fs.readFileSync(process.env.CODEX_HARNESS_STUB_CALLS,"utf8").trim().split("\\n").length;
if(n===1){process.stdout.write(JSON.stringify({type:"thread.started",thread_id:"${id}"})+"\\n");process.stdout.write(JSON.stringify({type:"turn.failed",error:{message:"connection reset"}})+"\\n");process.exit(1);}
process.stdout.write(JSON.stringify({type:"turn.completed",usage:{input_tokens:1,output_tokens:1}})+"\\n");process.exit(0);`,
        { args: ["--sandbox", "workspace-write"] }
      );
      expect(result.status).toBe(0);
      expect(calls).toHaveLength(2);
      expect(calls[1].args).toEqual(["exec", "--json", "--sandbox", "workspace-write", "resume", id, "-"]);
      expect(calls[1].args).not.toContain("--last");
      expect(calls[1].stdin).toContain("Complete only unfinished work");
      expect(calls[1].stdin).not.toBe(calls[0].stdin);
    });

    it.each(["stdout", "stderr"])("stops after an invalid_request_body resume rejection on %s and reports the cause", stream => {
      const id = "0199a213-81c0-7800-8aa1-bbab2a035a53";
      const dir = makeHarnessTempDir("invalid-request-body-");
      const safeOutputsPath = path.join(dir, "safe-outputs.jsonl");
      const safeOutputsCLI = path.join(dir, "safeoutputs.cjs");
      fs.writeFileSync(
        safeOutputsCLI,
        `#!${process.execPath}
const fs = require("fs");
const args = process.argv.slice(2);
const item = { type: args[0] };
for (let i = 1; i < args.length; i += 2) item[args[i].slice(2)] = args[i + 1];
fs.appendFileSync(process.env.GH_AW_SAFE_OUTPUTS, JSON.stringify(item) + "\\n");
`
      );
      fs.chmodSync(safeOutputsCLI, 0o700);
      const { result, calls } = runHarnessFixture(
        `
process.stdout.write(JSON.stringify({type:"thread.started",thread_id:"${id}"})+"\\n");
const resumed = process.argv.includes("resume");
const message = resumed ? JSON.stringify({error:{message:"Invalid 'input[13].id': 'ctco_example'. Expected an ID that begins with 'fc'.",code:"invalid_request_body"}}) : "connection reset";
process.${stream}.write(JSON.stringify({type:"turn.failed",error:{message}})+"\\n");
process.exit(1);`,
        { env: { GH_AW_HARNESS_MAX_RETRIES: "3", GH_AW_SAFE_OUTPUTS: safeOutputsPath, GH_AW_SAFEOUTPUTS_CLI: safeOutputsCLI } }
      );
      expect(result.status).toBe(1);
      expect(calls).toHaveLength(2);
      expect(calls[1].args).toEqual(["exec", "--json", "resume", id, "-"]);
      expect(result.stderr).toContain("invalid_request_body (HTTP 400) — not retrying");
      const items = fs
        .readFileSync(safeOutputsPath, "utf8")
        .trim()
        .split("\n")
        .map(line => JSON.parse(line));
      expect(items).toHaveLength(1);
      expect(items[0]).toMatchObject({ type: "report_incomplete", reason: "infrastructure_error" });
      expect(items[0].details).toContain("request-body rejection (invalid_request_body)");
      expect(items[0].details).toContain("not retrying");
    });

    it("does not select a session when ephemeral mode prevents persistence", () => {
      expect(buildCodexResumeArgs(["exec", "--ephemeral", "-"], "0199a213-81c0-7800-8aa1-bbab2a035a53")).toBeNull();
      expect(buildCodexResumeArgs(["exec", "-"], "not-an-id")).toBeNull();
      expect(buildCodexResumeArgs(["exec", "resume", "--last", "-"], "0199a213-81c0-7800-8aa1-bbab2a035a53")).toBeNull();
    });

    it.each([false, true])("preserves actual compiled execution flags on resume (detection=%s)", detection => {
      const id = "0199a213-81c0-7800-8aa1-bbab2a035a53";
      const flags = [
        "--model",
        "gpt-5.3-codex",
        "-c",
        'web_search="disabled"',
        "-c",
        "features.shell_tool=false",
        "--dangerously-bypass-approvals-and-sandbox",
        "--skip-git-repo-check",
        ...(detection ? ["--output-schema", "schema file.json", "-o", "result file.json"] : []),
      ];
      expect(buildCodexResumeArgs(["exec", "--json", ...flags, "-"], id)).toEqual(["exec", "--json", ...flags, "resume", id, "-"]);
      const { result, calls } = runHarnessFixture(
        `
const n=fs.readFileSync(process.env.CODEX_HARNESS_STUB_CALLS,"utf8").trim().split("\\n").length;
if(n===1){process.stdout.write(JSON.stringify({type:"thread.started",thread_id:"${id}"})+"\\n");process.stderr.write("connection reset\\n");process.exit(1);}
process.exit(0);`,
        { args: flags }
      );
      expect(result.status).toBe(0);
      expect(calls).toHaveLength(2);
      expect(calls[1].args).toEqual(["exec", "--json", ...flags, "resume", id, "-"]);
    });

    it.each(["--model", "-m", "--model="])("normalizes the runtime prefix in explicit %s model flags", flag => {
      const args = flag.endsWith("=") ? [flag + "copilot/gpt-5"] : [flag, "copilot/gpt-5"];
      const { result, calls } = runHarnessFixture("process.exit(0);", { args, env: { GH_AW_LLM_PROVIDER: "github", GH_AW_MODEL_AGENT_CODEX: "copilot/gpt-5" } });
      expect(result.status).toBe(0);
      expect(calls[0].args.join(" ")).toContain("gpt-5");
      expect(calls[0].args.join(" ")).not.toContain("copilot/");
      expect(calls[0].args.filter(arg => arg === "--model" || arg === "-m" || arg.startsWith("--model="))).toHaveLength(1);
    });

    it.each(["auto", "copilot/auto"])("starts Codex with the %s gateway picker in environment and CLI model inputs", model => {
      for (const args of [[], ["--model", model], [`--model=${model}`]]) {
        const { result, calls } = runHarnessFixture("process.exit(0);", { args, env: { GH_AW_LLM_PROVIDER: "github", GH_AW_MODEL_AGENT_CODEX: model } });
        expect(result.status, result.stderr).toBe(0);
        expect(calls).toHaveLength(1);
        expect(calls[0].args.join(" ")).toMatch(/--model(?:=| )auto/);
        expect(calls[0].args.join(" ")).not.toContain("copilot/");
      }
    });

    it("fails startup rather than switching credentials for a conflicting runtime prefix", () => {
      const { result, calls } = runHarnessFixture("process.exit(0);", { env: { GH_AW_LLM_PROVIDER: "openai", GH_AW_MODEL_AGENT_CODEX: "copilot/gpt-5" } });
      expect(result.status).toBe(1);
      expect(calls).toHaveLength(0);
      expect(result.stderr).toContain("does not match configured provider");
    });

    it.each(["max_cache_misses_exceeded", "effective_tokens_limit_exceeded", "permission_denied_limit_exceeded", "model_policy_violation"])("stops on the terminal proxy guard %s", guard => {
      const { result, calls } = runHarnessFixture(`process.stdout.write(JSON.stringify({type:"turn.failed",error:{message:"403 ${guard}"}})+"\\n");process.exit(1);`);
      expect(result.status).toBe(1);
      expect(calls).toHaveLength(1);
      expect(result.stderr).toContain("terminal proxy guard");
    });

    it("recognizes native budget errors even when verbose output exceeds the collection bound", () => {
      const { result, calls } = runHarnessFixture(
        `process.stdout.write(JSON.stringify({type:"turn.failed",error:{message:"403 Maximum AI credits exceeded (301 / 300)."}})+"\\n");process.stdout.write("x".repeat(5*1024*1024)+"\\n",()=>process.exit(1));`
      );
      expect(result.status).toBe(0);
      expect(calls).toHaveLength(1);
      expect(result.stderr).toContain("trusted budget-abort evidence");
    });

    it("does not let guard names in file reads mask the observed credit rejection", () => {
      const events = [
        { type: "item.completed", item: { type: "command_execution", aggregated_output: "GH_AW_MAX_CACHE_MISSES_EXCEEDED: max_cache_misses_exceeded" } },
        { type: "error", message: "unexpected status 403 Forbidden: Maximum AI credits exceeded (1.028578 / 1)., url: http://api-proxy:10000/responses" },
        { type: "turn.failed", error: { message: "unexpected status 403 Forbidden: Maximum AI credits exceeded (1.028578 / 1)., url: http://api-proxy:10000/responses" } },
      ];
      const { result, calls } = runHarnessFixture(`for(const event of ${JSON.stringify(events)})process.stdout.write(JSON.stringify(event)+"\\n");process.exit(1);`);
      expect(result.status).toBe(0);
      expect(calls).toHaveLength(1);
      expect(result.stderr).toContain("AI credits budget enforced");
      expect(result.stderr).not.toContain("terminal proxy guard");
    });

    it("reports the soft-deadline reason when preempting an attempt that handles SIGTERM cleanly", () => {
      const tempDir = makeHarnessTempDir("soft-deadline-report-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const safeOutputsCLI = path.join(tempDir, "safeoutputs.cjs");
      fs.writeFileSync(
        safeOutputsCLI,
        `#!${process.execPath}
const fs = require("fs");
const args = process.argv.slice(2);
const item = { type: args[0] };
for (let i = 1; i < args.length; i += 2) item[args[i].slice(2)] = args[i + 1];
fs.appendFileSync(process.env.GH_AW_SAFE_OUTPUTS, JSON.stringify(item) + "\\n");
`
      );
      fs.chmodSync(safeOutputsCLI, 0o700);
      const { result, calls } = runHarnessFixture(`process.on("SIGTERM",()=>process.exit(0));setInterval(()=>{},1000);`, {
        env: {
          GH_AW_TIMEOUT_MINUTES: "0.04",
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_SAFEOUTPUTS_CLI: safeOutputsCLI,
        },
      });
      expect(result.status).toBe(1);
      expect(calls).toHaveLength(1);
      expect(result.stderr).toContain("soft execution deadline");
      const [item] = fs
        .readFileSync(safeOutputsPath, "utf8")
        .trim()
        .split("\n")
        .map(line => JSON.parse(line));
      expect(item).toMatchObject({ type: "report_incomplete", reason: "infrastructure_error" });
      expect(item.details).toMatch(/^Codex run stopped by a runtime guard\./);
      expect(item.details).toContain("Codex reached the soft execution deadline");
    });

    it("requires a path for stdin prompt delivery", () => {
      expect(() => resolveCodexPromptInput(["exec", "--prompt-file"])).toThrow("requires a readable file path");
    });

    it("repairs a provider section without corrupting the next TOML header", () => {
      const dir = makeHarnessTempDir("toml-repair-");
      const config = path.join(dir, "config.toml");
      const reflect = path.join(dir, "reflect.json");
      fs.writeFileSync(config, '[model_providers.openai-proxy]\nname = "proxy"\n[features]\nplugins = false\n');
      fs.writeFileSync(reflect, JSON.stringify({ endpoints: [{ provider: "openai", configured: true, port: 10000 }] }));
      expect(configureCodexProviderFromReflect({ codexConfigPath: config, reflectPath: reflect, provider: "openai" }).configured).toBe(true);
      const text = fs.readFileSync(config, "utf8");
      expect(text).toMatch(/\nbase_url = "http:\/\/api-proxy:10000"\n+\[features\]/);
    });
  });

  describe("resolveCodexPromptFileArgs", () => {
    it("replaces --prompt-file with the file's content as the last positional arg", () => {
      const promptFile = path.join(agentTempDir, `codex-harness-prompt-${Date.now()}.txt`);
      fs.writeFileSync(promptFile, "fix the bug", "utf8");
      try {
        const result = resolveCodexPromptFileArgs(["exec", "--dangerously-bypass-approvals-and-sandbox", "--prompt-file", promptFile]);
        expect(result).toEqual(["exec", "--dangerously-bypass-approvals-and-sandbox", "fix the bug"]);
      } finally {
        fs.rmSync(promptFile);
      }
    });

    it("appends prompt content as the last arg when only --prompt-file is provided", () => {
      const promptFile = path.join(agentTempDir, `codex-harness-prompt-${Date.now()}.txt`);
      fs.writeFileSync(promptFile, "my task", "utf8");
      try {
        const result = resolveCodexPromptFileArgs(["--prompt-file", promptFile]);
        expect(result).toEqual(["my task"]);
      } finally {
        fs.rmSync(promptFile);
      }
    });

    it("passes through args that have no --prompt-file", () => {
      const result = resolveCodexPromptFileArgs(["exec", "--dangerously-bypass-approvals-and-sandbox"]);
      expect(result).toEqual(["exec", "--dangerously-bypass-approvals-and-sandbox"]);
    });

    it("preserves args when --prompt-file is provided without a path", () => {
      const result = resolveCodexPromptFileArgs(["exec", "--prompt-file"]);
      // When no path follows --prompt-file, it is preserved as-is
      expect(result).toEqual(["exec", "--prompt-file"]);
    });

    it("throws when the prompt file does not exist", () => {
      const missingFile = path.join(agentTempDir, `codex-harness-missing-${Date.now()}.txt`);
      expect(() => resolveCodexPromptFileArgs(["--prompt-file", missingFile])).toThrow(`--prompt-file '${missingFile}' is not readable`);
    });

    it("throws when the prompt file cannot be read (directory)", () => {
      const dir = makeHarnessTempDir("codex-harness-dir-");
      try {
        expect(() => resolveCodexPromptFileArgs(["--prompt-file", dir])).toThrow(`--prompt-file '${dir}' is not readable`);
      } finally {
        fs.rmdirSync(dir);
      }
    });
  });

  describe("isRateLimitError", () => {
    it("returns true for rate_limit_exceeded error", () => {
      expect(isRateLimitError("Error: rate_limit_exceeded")).toBe(true);
    });

    describe("isTokenPerMinuteRateLimitError", () => {
      it("returns true for OpenAI TPM-limit wording", () => {
        expect(isTokenPerMinuteRateLimitError("Rate limit reached for gpt-4o-mini in organization org-xxx on tokens per min (TPM): Limit 200000, Used 166655, Requested 35398. Please try again in 615ms.")).toBe(true);
      });

      it("returns false for generic rate-limit wording", () => {
        expect(isTokenPerMinuteRateLimitError("rate_limit_exceeded")).toBe(false);
      });

      it("returns false for unrelated mention of tokens per min", () => {
        expect(isTokenPerMinuteRateLimitError("rate_limit_exceeded while printing docs about 'on tokens per min'")).toBe(false);
      });
    });

    it("returns true for 429 Too Many Requests", () => {
      expect(isRateLimitError("429 Too Many Requests")).toBe(true);
    });

    it("returns true for RateLimitError", () => {
      expect(isRateLimitError("RateLimitError: You exceeded your current quota")).toBe(true);
    });

    it("returns true for 'Rate limit reached for' human-readable message", () => {
      expect(isRateLimitError("Rate limit reached for gpt-4o-mini in organization org-xxx on tokens per min (TPM): " + "Limit 200000, Used 166655, Requested 35398. Please try again in 615ms.")).toBe(true);
    });

    it("returns false for unrelated errors", () => {
      expect(isRateLimitError("Error: ENOENT: no such file")).toBe(false);
      expect(isRateLimitError("Fatal: out of memory")).toBe(false);
      expect(isRateLimitError("")).toBe(false);
    });

    it("returns false for a 500 server error", () => {
      expect(isRateLimitError("500 Internal Server Error")).toBe(false);
    });
  });

  describe("isAuthenticationFailedError", () => {
    it("returns true for authentication failed with request id", () => {
      expect(isAuthenticationFailedError("Authentication failed (Request ID: C818:3ED713:19D401B:1C446B7:69D653CA)")).toBe(true);
    });

    it("returns false for non-authentication-failed output", () => {
      expect(isAuthenticationFailedError("No authentication information found")).toBe(false);
      expect(isAuthenticationFailedError("rate_limit_exceeded")).toBe(false);
    });
  });

  describe("isMissingApiKeyError", () => {
    it("returns true for missing OPENAI_API_KEY with backtick delimiters", () => {
      expect(isMissingApiKeyError("ERROR: Missing environment variable: `OPENAI_API_KEY`")).toBe(true);
    });

    it("returns true for missing CODEX_API_KEY with backtick delimiters", () => {
      expect(isMissingApiKeyError("ERROR: Missing environment variable: `CODEX_API_KEY`")).toBe(true);
    });

    it("returns true for missing OPENAI_API_KEY without backtick delimiters", () => {
      expect(isMissingApiKeyError("Missing environment variable: OPENAI_API_KEY")).toBe(true);
    });

    it("returns true when the error appears within a larger output block", () => {
      const output = "Starting codex...\nERROR: Missing environment variable: `OPENAI_API_KEY`\nExiting.";
      expect(isMissingApiKeyError(output)).toBe(true);
    });

    it("returns false for unrelated errors", () => {
      expect(isMissingApiKeyError("Authentication failed")).toBe(false);
      expect(isMissingApiKeyError("rate_limit_exceeded")).toBe(false);
      expect(isMissingApiKeyError("Missing environment variable: HOME")).toBe(false);
      expect(isMissingApiKeyError("")).toBe(false);
    });
  });

  describe("injectJsonFlag", () => {
    it("injects --json after exec when not already present", () => {
      expect(injectJsonFlag(["exec", "--dangerously-bypass-approvals-and-sandbox", "do the thing"])).toEqual(["exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "do the thing"]);
    });

    it("does not inject --json when already present", () => {
      expect(injectJsonFlag(["exec", "--json", "--skip-git-repo-check", "do the thing"])).toEqual(["exec", "--json", "--skip-git-repo-check", "do the thing"]);
    });

    it("does not inject --json for non-exec subcommands", () => {
      expect(injectJsonFlag(["resume", "--last", "fix it"])).toEqual(["resume", "--last", "fix it"]);
    });

    it("returns empty array unchanged", () => {
      expect(injectJsonFlag([])).toEqual([]);
    });
  });

  describe("buildCodexChildEnv", () => {
    it("preserves captured keys even when base environment is missing them", () => {
      const result = buildCodexChildEnv({ PATH: "/usr/bin" }, "codex-key", "openai-key");
      expect(result.CODEX_API_KEY).toBe("codex-key");
      expect(result.OPENAI_API_KEY).toBe("openai-key");
      expect(result.PATH).toBe("/usr/bin");
    });

    it("does not add unset keys", () => {
      const result = buildCodexChildEnv({ PATH: "/usr/bin" }, undefined, undefined);
      expect(result.CODEX_API_KEY).toBeUndefined();
      expect(result.OPENAI_API_KEY).toBeUndefined();
    });

    it("does not pass execution evidence metadata to the engine child", () => {
      const result = buildCodexChildEnv({ PATH: "/usr/bin", GH_AW_AWF_EXECUTION_COMPONENT: "agent", GH_AW_AWF_EXECUTION_EVIDENCE_FILE: "/tmp/evidence.json" }, undefined, undefined);

      expect(result).not.toHaveProperty("GH_AW_AWF_EXECUTION_COMPONENT");
      expect(result).not.toHaveProperty("GH_AW_AWF_EXECUTION_EVIDENCE_FILE");
    });
  });

  describe("context rebuild circuit breaker", () => {
    it("uses defaults when env overrides are absent or invalid", () => {
      const cfg = resolveContextRebuildCircuitBreakerConfig({
        GH_AW_CODEX_MAX_REBUILD_FACTOR: "nope",
        GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS: "-1",
        GH_AW_CODEX_REBUILD_GUARD_POLL_MS: "0",
        GH_AW_CODEX_REBUILD_GUARD_TERM_GRACE_MS: "0",
      });
      expect(cfg.enabled).toBe(true);
      expect(cfg.maxRebuildFactor).toBe(DEFAULT_CONTEXT_REBUILD_FACTOR_LIMIT);
      expect(cfg.minCumulativeInputTokens).toBe(DEFAULT_CONTEXT_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS);
      expect(cfg.pollIntervalMs).toBe(DEFAULT_CONTEXT_REBUILD_POLL_INTERVAL_MS);
      expect(cfg.termGraceMs).toBe(DEFAULT_CONTEXT_REBUILD_TERM_GRACE_MS);
    });

    it.each(["false", "0", "off", "no", " FALSE ", "OFF", "No"])("supports explicit disable via env value %s", value => {
      const cfg = resolveContextRebuildCircuitBreakerConfig({
        GH_AW_CODEX_CONTEXT_REBUILD_CIRCUIT_BREAKER: value,
      });
      expect(cfg.enabled).toBe(false);
    });

    it("allows a measured long run with a higher workload-specific threshold", () => {
      const workingSet = { rebuild_factor: 29.53, cumulative_input_tokens: 4650000 };
      expect(evaluateContextRebuildCircuitBreaker(workingSet, resolveContextRebuildCircuitBreakerConfig({})).terminate).toBe(true);
      expect(evaluateContextRebuildCircuitBreaker(workingSet, resolveContextRebuildCircuitBreakerConfig({ GH_AW_CODEX_MAX_REBUILD_FACTOR: "50" })).terminate).toBe(false);
    });

    it("trips only when both rebuild factor and cumulative input exceed thresholds", () => {
      const config = { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 };
      expect(
        evaluateContextRebuildCircuitBreaker(
          {
            measurement_state: "measured",
            rebuild_factor: 4.5,
            cumulative_input_tokens: 1400,
            peak_input_tokens: 311,
            rebuild_excess_tokens: 1089,
            invocations: 5,
          },
          config
        ).terminate
      ).toBe(true);
      expect(
        evaluateContextRebuildCircuitBreaker(
          {
            measurement_state: "measured",
            rebuild_factor: 4.5,
            cumulative_input_tokens: 999,
            peak_input_tokens: 222,
            rebuild_excess_tokens: 777,
            invocations: 4,
          },
          config
        ).terminate
      ).toBe(false);
    });

    it("falls back to the default cumulative floor for fractional overrides below one token", () => {
      const cfg = resolveContextRebuildCircuitBreakerConfig({
        GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS: "0.5",
      });
      expect(cfg.minCumulativeInputTokens).toBe(DEFAULT_CONTEXT_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS);
    });

    it("does not trip when rebuild_factor is just below the threshold", () => {
      const config = { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 };
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: 3.99, cumulative_input_tokens: 2000 }, config).terminate).toBe(false);
    });

    it("trips when rebuild_factor is exactly at the threshold", () => {
      const config = { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 };
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: 4, cumulative_input_tokens: 2000 }, config).terminate).toBe(true);
    });

    it("does not trip after a terminal safe-output was produced in the current attempt", () => {
      const dir = makeHarnessTempDir("codex-rebuild-safe-output-");
      const safeOutputsPath = path.join(dir, "safe-outputs.jsonl");
      fs.writeFileSync(safeOutputsPath, '{"type":"noop","message":"done"}\n', "utf8");
      const logs = [];
      try {
        const decision = evaluateContextRebuildCircuitBreakerForAttempt(
          { rebuild_factor: 4, cumulative_input_tokens: 2000 },
          { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 },
          { safeOutputsPath, safeOutputsByteOffset: 0, logger: msg => logs.push(msg) }
        );
        expect(decision.terminate).toBe(false);
        expect(logs.some(line => line.includes("allowing Codex to exit normally"))).toBe(true);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("does not trip after a report_incomplete safe-output was produced in the current attempt", () => {
      const dir = makeHarnessTempDir("codex-rebuild-report-incomplete-");
      const safeOutputsPath = path.join(dir, "safe-outputs.jsonl");
      fs.writeFileSync(safeOutputsPath, '{"type":"report_incomplete","reason":"blocked"}\n', "utf8");
      try {
        const decision = evaluateContextRebuildCircuitBreakerForAttempt({ rebuild_factor: 4, cumulative_input_tokens: 2000 }, { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 }, { safeOutputsPath, safeOutputsByteOffset: 0 });
        expect(decision.terminate).toBe(false);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("still trips when terminal safe-output predates the current attempt", () => {
      const dir = makeHarnessTempDir("codex-rebuild-stale-safe-output-");
      const safeOutputsPath = path.join(dir, "safe-outputs.jsonl");
      fs.writeFileSync(safeOutputsPath, '{"type":"noop","message":"old"}\n', "utf8");
      const byteOffset = fs.statSync(safeOutputsPath).size;
      try {
        const decision = evaluateContextRebuildCircuitBreakerForAttempt({ rebuild_factor: 4, cumulative_input_tokens: 2000 }, { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 }, { safeOutputsPath, safeOutputsByteOffset: byteOffset });
        expect(decision.terminate).toBe(true);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("does not trip for null, empty, or non-finite working sets", () => {
      const config = { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 };
      expect(evaluateContextRebuildCircuitBreaker(null, config).terminate).toBe(false);
      expect(evaluateContextRebuildCircuitBreaker({}, config).terminate).toBe(false);
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: Number.NaN, cumulative_input_tokens: 5000 }, config).terminate).toBe(false);
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: Number.POSITIVE_INFINITY, cumulative_input_tokens: 5000 }, config).terminate).toBe(false);
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: 9, cumulative_input_tokens: Number.NaN }, config).terminate).toBe(false);
    });

    it("accepts a rebuild factor threshold of exactly 1", () => {
      expect(resolveContextRebuildCircuitBreakerConfig({ GH_AW_CODEX_MAX_REBUILD_FACTOR: "1" }).maxRebuildFactor).toBe(1);
    });

    it("skips token-usage candidates whose measurements are unavailable", async () => {
      const dir = makeHarnessTempDir("codex-token-usage-");
      const malformed = path.join(dir, "malformed.jsonl");
      const valid = path.join(dir, "valid.jsonl");
      fs.writeFileSync(malformed, "not json\n{oops\n");
      fs.writeFileSync(valid, `${JSON.stringify({ input_tokens: 100 })}\n${JSON.stringify({ input_tokens: 900 })}\n`);
      try {
        const workingSet = await readWorkingSetFromTokenUsage([malformed, valid]);
        expect(workingSet).not.toBeNull();
        expect(workingSet.measurement_state).toBe("measured");
        expect(workingSet.cumulative_input_tokens).toBe(1000);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("prefers the most recently written token-usage candidate", async () => {
      const dir = makeHarnessTempDir("codex-token-usage-");
      const stale = path.join(dir, "stale.jsonl");
      const fresh = path.join(dir, "fresh.jsonl");
      fs.writeFileSync(stale, `${JSON.stringify({ input_tokens: 7 })}\n`);
      fs.writeFileSync(fresh, `${JSON.stringify({ input_tokens: 500 })}\n${JSON.stringify({ input_tokens: 500 })}\n`);
      const now = Date.now() / 1000;
      fs.utimesSync(stale, now - 600, now - 600);
      fs.utimesSync(fresh, now, now);
      try {
        // `stale` is listed first, but `fresh` has the newer mtime and must win.
        const workingSet = await readWorkingSetFromTokenUsage([stale, fresh]);
        expect(workingSet.cumulative_input_tokens).toBe(1000);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("returns null when no candidate yields usable measurements", async () => {
      const dir = makeHarnessTempDir("codex-token-usage-");
      const malformed = path.join(dir, "malformed.jsonl");
      fs.writeFileSync(malformed, "not json\n");
      try {
        expect(await readWorkingSetFromTokenUsage([malformed, path.join(dir, "missing.jsonl")])).toBeNull();
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });
  });

  describe("OpenAI base URL validation", () => {
    it("extracts port from URL", () => {
      expect(extractPortFromURL("http://172.30.0.30:10000")).toBe(10000);
      expect(extractPortFromURL("https://example.com")).toBeNull();
      expect(extractPortFromURL("not-a-url")).toBeNull();
    });

    describe("configureCodexProviderFromReflect", () => {
      it("configures OPENAI_BASE_URL from reflected github/copilot endpoint", () => {
        const tmpDir = makeHarnessTempDir("codex-provider-");
        const configPath = path.join(tmpDir, "config.toml");
        const reflectPath = path.join(tmpDir, "awf-reflect.json");
        fs.writeFileSync(configPath, `[model_providers.openai-proxy]\nbase_url = "http://172.30.0.30:10000"\n`, "utf8");
        fs.writeFileSync(reflectPath, JSON.stringify({ endpoints: [{ provider: "copilot", configured: true, port: 10002 }] }), "utf8");
        try {
          const result = configureCodexProviderFromReflect({
            codexConfigPath: configPath,
            reflectPath,
            provider: "github",
          });
          expect(result.configured).toBe(true);
          expect(result.env.OPENAI_BASE_URL).toBe("http://api-proxy:10002");
          expect(fs.readFileSync(configPath, "utf8")).toContain(`base_url = "http://api-proxy:10002"`);
        } finally {
          fs.rmSync(tmpDir, { recursive: true, force: true });
        }
      });
    });

    it("extracts openai-proxy base_url from TOML", () => {
      const toml = `
[history]
persistence = "none"
[model_providers.openai-proxy]
name = "OpenAI AWF proxy"
base_url = "http://172.30.0.30:10000"
env_key = "OPENAI_API_KEY"
`;
      expect(extractOpenAIProxyBaseURLFromToml(toml)).toBe("http://172.30.0.30:10000");
    });

    it("extracts configured provider port from reflect payload", () => {
      const reflect = {
        endpoints: [
          { provider: "anthropic", port: 10001, configured: true },
          { provider: "openai", port: 10000, configured: true },
          { provider: "copilot", port: 10002, configured: true },
        ],
      };
      expect(getConfiguredProviderPortFromReflect(reflect)).toBe(10000);
      expect(getConfiguredProviderPortFromReflect(reflect, "github")).toBe(10002);
    });

    it("returns null for malformed reflect endpoint ports", () => {
      const reflect = {
        endpoints: [{ provider: "openai", port: "not-a-number", configured: true }],
      };
      expect(getConfiguredProviderPortFromReflect(reflect)).toBeNull();
    });

    it("returns null when the selected provider is not configured", () => {
      const reflect = {
        endpoints: [{ provider: "anthropic", port: 10001, configured: true }],
      };
      expect(getConfiguredProviderPortFromReflect(reflect, "github")).toBeNull();
    });

    it("fails validation when config and reflect OpenAI ports mismatch", () => {
      const toml = `[model_providers.openai-proxy]\nbase_url = "http://172.30.0.30:10001"\n`;
      const reflect = JSON.stringify({
        endpoints: [
          { provider: "openai", port: 10000, configured: true },
          { provider: "anthropic", port: 10001, configured: true },
        ],
      });
      const files = {
        "/tmp/codex-config.toml": toml,
        "/tmp/awf-reflect.json": reflect,
      };
      const readFileSync = filePath => files[filePath];
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        readFileSync,
      });
      expect(result.ok).toBe(false);
      expect(result.reason).toContain("mismatch");
    });

    it("passes validation when ports match", () => {
      const toml = `[model_providers.openai-proxy]\nbase_url = "http://172.30.0.30:10000"\n`;
      const reflect = JSON.stringify({
        endpoints: [{ provider: "openai", port: 10000, configured: true }],
      });
      const files = {
        "/tmp/codex-config.toml": toml,
        "/tmp/awf-reflect.json": reflect,
      };
      const readFileSync = filePath => files[filePath];
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        readFileSync,
      });
      expect(result.ok).toBe(true);
    });

    it("validates against the selected GitHub provider", () => {
      const files = {
        "/tmp/codex-config.toml": `[model_providers.openai-proxy]\nbase_url = "http://api-proxy:10002"\n`,
        "/tmp/awf-reflect.json": JSON.stringify({ endpoints: [{ provider: "copilot", port: 10002, configured: true }] }),
      };
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        provider: "github",
        readFileSync: filePath => files[filePath],
      });
      expect(result.ok).toBe(true);
    });

    it("fails strictly when /reflect has configured endpoints but none for the selected provider", () => {
      const files = {
        "/tmp/codex-config.toml": `[model_providers.openai-proxy]\nbase_url = "http://172.30.0.30:10000"\n`,
        "/tmp/awf-reflect.json": JSON.stringify({ endpoints: [{ provider: "anthropic", port: 10001, configured: true }] }),
      };
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        provider: "github",
        readFileSync: filePath => files[filePath],
      });
      expect(result.ok).toBe(false);
      expect(result.reason).toContain("no configured endpoint for provider");
    });

    it("passes through when TOML lacks openai-proxy section", () => {
      const files = {
        "/tmp/codex-config.toml": `[history]\npersistence = "none"\n`,
        "/tmp/awf-reflect.json": JSON.stringify({ endpoints: [{ provider: "openai", port: 10000, configured: true }] }),
      };
      const readFileSync = filePath => files[filePath];
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        readFileSync,
      });
      expect(result.ok).toBe(true);
    });
  });

  describe("isServerError", () => {
    it("returns true for InternalServerError", () => {
      expect(isServerError("InternalServerError: The server had an error processing your request")).toBe(true);
    });

    describe("isInvalidModelError", () => {
      it("returns true for model-not-supported errors", () => {
        expect(isInvalidModelError("Execution failed: CAPIError: 400 The requested model is not supported.")).toBe(true);
      });

      it("returns true for invalid model name errors", () => {
        expect(isInvalidModelError("invalid model name 'claude-sonnet-999'")).toBe(true);
        expect(isInvalidModelError("model 'gpt-foo' not found")).toBe(true);
        expect(isInvalidModelError("model gpt-unknown is not available")).toBe(true);
        expect(isInvalidModelError("model 'claude-3-5-sonnet@20241022' not found")).toBe(true);
      });

      it("returns true for AIC api-proxy 404 standalone Model not found shape", () => {
        expect(isInvalidModelError("404 Not Found: Model not found")).toBe(true);
        expect(isInvalidModelError("ResponseError: 404 Not Found: Model not found")).toBe(true);
        expect(isInvalidModelError("Error: 404 Model not found")).toBe(true);
      });

      it("returns false for unrelated errors", () => {
        expect(isInvalidModelError("rate_limit_exceeded")).toBe(false);
        expect(isInvalidModelError("unknown model behavior detected")).toBe(false);
        expect(isInvalidModelError("ServiceUnavailableError")).toBe(false);
        expect(isInvalidModelError("")).toBe(false);
      });
    });

    it("returns true for ServiceUnavailableError", () => {
      expect(isServerError("ServiceUnavailableError: The server is temporarily unable to service your request")).toBe(true);
    });

    it("returns true for 500 Internal Server Error", () => {
      expect(isServerError("500 Internal Server Error")).toBe(true);
    });

    it("returns true for 503 Service Unavailable", () => {
      expect(isServerError("503 Service Unavailable")).toBe(true);
    });

    it("returns false for rate limit errors", () => {
      expect(isServerError("rate_limit_exceeded")).toBe(false);
      expect(isServerError("429 Too Many Requests")).toBe(false);
    });

    it("returns false for unrelated errors", () => {
      expect(isServerError("Error: ENOENT: no such file")).toBe(false);
      expect(isServerError("")).toBe(false);
    });
  });

  describe("isInvalidRequestError", () => {
    it("returns true for a provider invalid_request_error payload", () => {
      const output = String.raw`{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"Provider returned error\",\"code\":400,\"metadata\":{\"raw\":\"{\\\"type\\\": \\\"invalid_request_error\\\",\\n    \\\"param\\\": \\\"messages[4].content\\\",\\n    \\\"code\\\": \\\"empty_array\\\"}\"}}}"}}`;
      expect(isInvalidRequestError(output)).toBe(true);
    });

    it("returns true for an unescaped provider error field in a turn.failed event", () => {
      expect(isInvalidRequestError('{"type":"turn.failed","error":{"type":"invalid_request_error"}}')).toBe(true);
    });

    it("extracts the invalid request error code", () => {
      expect(extractInvalidRequestErrorCode('{"type":"turn.failed","error":{"type":"invalid_request_error"}}')).toBe("invalid_request_error");
      expect(extractInvalidRequestErrorCode('{"type":"turn.failed","error":{"code":"invalid_request_body"}}')).toBe("invalid_request_body");
      expect(extractInvalidRequestErrorCode('{"type":"item.completed","item":{"text":"invalid_request_body"}}')).toBeNull();
    });

    it("returns true for the observed nested invalid_request_body item-ID rejection", () => {
      const output = String.raw`{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"Invalid 'input[13].id': 'ctco_example'. Expected an ID that begins with 'fc'.\",\"code\":\"invalid_request_body\"}}\n"}}`;
      expect(isInvalidRequestError(output)).toBe(true);
      expect(isInvalidRequestError('{"type":"turn.failed","error":{"code":"invalid_request_body"}}')).toBe(true);
    });

    it("returns false for unrelated errors and tool transcript content", () => {
      expect(isInvalidRequestError("rate_limit_exceeded")).toBe(false);
      expect(isInvalidRequestError("500 Internal Server Error")).toBe(false);
      expect(isInvalidRequestError("GitHub API responded with 400 Bad Request")).toBe(false);
      expect(isInvalidRequestError('{"type":"item.completed","item":{"type":"tool_call_output","output":"invalid_request_error"}}')).toBe(false);
      expect(isInvalidRequestError('{"type":"item.completed","item":{"type":"agent_message","text":"invalid_request_body"}}')).toBe(false);
      expect(isInvalidRequestError('{"type":"item.completed","item":{"type":"tool_call_output","output":"invalid_request_body"}}')).toBe(false);
      expect(isInvalidRequestError("invalid_request_body")).toBe(false);
      expect(isInvalidRequestError("")).toBe(false);
    });
  });

  describe("isUnsupportedModelToolsError", () => {
    it("consumes JSONL lazily through corrupt lines without decoding records after a match", () => {
      const first = '{"type":"turn.failed","error":{"code":"unknown_parameter","param":"tools"}}';
      const output = `[INFO] startup\n{"broken"\r\nfalse\r\n${first}\n{"later":true}`;
      const parse = vi.spyOn(JSON, "parse");
      try {
        expect(isUnsupportedModelToolsError(output)).toBe(true);
        expect(parse.mock.calls.map(([text]) => text)).toEqual(['{"broken"', "false", first]);
      } finally {
        parse.mockRestore();
      }
    });

    it("returns true for the observed 'tools' unknown_parameter turn.failed event", () => {
      const output =
        '{"type":"thread.started","thread_id":"01a0545e-6060-7472-9d50-d4a643611434"}\n' +
        '{"type":"turn.started"}\n' +
        String.raw`{"type":"error","message":"{\n  \"error\": {\n    \"message\": \"Invalid value: 'custom'\",\n    \"type\": \"invalid_request_error\",\n    \"param\": \"tools\",\n    \"code\": \"unknown_parameter\"\n  }\n}"}` +
        "\n" +
        String.raw`{"type":"turn.failed","error":{"message":"{\n  \"error\": {\n    \"message\": \"Invalid value: 'custom'\",\n    \"type\": \"invalid_request_error\",\n    \"param\": \"tools\",\n    \"code\": \"unknown_parameter\"\n  }\n}"}}`;
      expect(isUnsupportedModelToolsError(output)).toBe(true);
    });

    it("returns true regardless of the order of param/code fields", () => {
      const output = '{"type":"turn.failed","error":{"code":"unknown_parameter","param":"tools"}}';
      expect(isUnsupportedModelToolsError(output)).toBe(true);
    });

    it("returns true for a provider metadata.raw envelope", () => {
      const output = String.raw`{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"Provider returned error\",\"code\":400,\"metadata\":{\"raw\":\"{\\\"type\\\": \\\"invalid_request_error\\\",\\n    \\\"param\\\": \\\"tools\\\",\\n    \\\"code\\\": \\\"unknown_parameter\\\"}\"}}}"}}`;
      expect(isUnsupportedModelToolsError(output)).toBe(true);
    });

    it("returns false for an unrelated invalid_request_error (e.g. empty message array)", () => {
      const output = String.raw`{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"Provider returned error\",\"code\":400,\"metadata\":{\"raw\":\"{\\\"type\\\": \\\"invalid_request_error\\\",\\n    \\\"param\\\": \\\"messages[4].content\\\",\\n    \\\"code\\\": \\\"empty_array\\\"}\"}}}"}}`;
      expect(isUnsupportedModelToolsError(output)).toBe(false);
    });

    it("returns false for unrelated errors and empty output", () => {
      expect(isUnsupportedModelToolsError("rate_limit_exceeded")).toBe(false);
      expect(isUnsupportedModelToolsError('{"type":"item.completed","item":{"type":"tool_call_output","output":"unknown_parameter tools"}}')).toBe(false);
      expect(isUnsupportedModelToolsError("")).toBe(false);
    });
  });

  describe("permission-denied classification helpers", () => {
    it("counts repeated permission-denied signals", () => {
      const output = "permission denied\npermissions denied\nEACCES: permission denied";
      expect(countPermissionDeniedIssues(output)).toBe(4);
    });

    it("detects numerous permission-denied issues at threshold", () => {
      const output = "permission denied\npermission denied\npermission denied";
      expect(hasNumerousPermissionDeniedIssues(output)).toBe(true);
    });

    it("does not classify sparse permission-denied output as numerous", () => {
      expect(hasNumerousPermissionDeniedIssues("permission denied")).toBe(false);
    });

    it("builds report_missing_tool payload for permission issues", () => {
      const payload = JSON.parse(buildMissingToolPermissionIssuePayload());
      expect(payload.type).toBe("report_missing_tool");
      expect(payload.reason).toContain("missing tool/permission issue");
      expect(payload.denied_commands).toEqual([]);
    });

    it("builds report_missing_tool payload with denied commands", () => {
      const payload = JSON.parse(buildMissingToolPermissionIssuePayload(["go version", "ls /usr/local/go"]));
      expect(payload.type).toBe("report_missing_tool");
      expect(payload.denied_commands).toEqual(["go version", "ls /usr/local/go"]);
    });
  });

  describe("extractDeniedCommands", () => {
    it("returns empty array for empty output", () => {
      expect(extractDeniedCommands("")).toEqual([]);
      expect(extractDeniedCommands(null)).toEqual([]);
    });

    it("extracts command from line with box-drawing pipe marker (│) before permission denied", () => {
      const output = ["\u2713 Some successful step", "\u2717 Check if go command works (shell)", "  \u2502 go version 2>&1", "  \u2514 Permission denied and could not request permission from user"].join("\n");
      expect(extractDeniedCommands(output)).toEqual(["go version 2>&1"]);
    });

    it("extracts command with plain pipe (|) before permission denied", () => {
      const output = ["| ls -la", "Permission denied"].join("\n");
      expect(extractDeniedCommands(output)).toEqual(["ls -la"]);
    });

    it("deduplicates repeated denied commands", () => {
      const output = ["  \u2502 go version", "  Permission denied", "  \u2502 go version", "  Permission denied", "  \u2502 go version", "  Permission denied"].join("\n");
      const result = extractDeniedCommands(output);
      expect(result).toEqual(["go version"]);
    });

    it("extracts multiple distinct denied commands", () => {
      const output = ["  \u2502 go version 2>&1", "  Permission denied", "  \u2502 ls /usr/local/go/bin/go", "  Permission denied", "  \u2502 which go", "  Permission denied"].join("\n");
      const result = extractDeniedCommands(output);
      expect(result).toContain("go version 2>&1");
      expect(result).toContain("ls /usr/local/go/bin/go");
      expect(result).toContain("which go");
    });

    it("returns empty array when no pipe markers are present before permission denied", () => {
      const output = "Some output\nPermission denied\nMore output";
      expect(extractDeniedCommands(output)).toEqual([]);
    });

    it("looks back up to 3 lines for command context", () => {
      const output = ["  \u2502 make test", "Running...", "Still running...", "  Permission denied"].join("\n");
      expect(extractDeniedCommands(output)).toEqual(["make test"]);
    });

    it("does not look back more than 3 lines", () => {
      const output = ["  \u2502 make test", "line2", "line3", "line4", "  Permission denied"].join("\n");
      expect(extractDeniedCommands(output)).toEqual([]);
    });

    it("does not capture suffix of a command containing an internal pipe", () => {
      // "find . -name '*.go' | sort" should not match by splitting on the internal |
      const output = ["  find . -name '*.go' | sort", "  Permission denied"].join("\n");
      expect(extractDeniedCommands(output)).toEqual([]);
    });
  });

  describe("retry policy: fresh run on partial execution", () => {
    const MAX_RETRIES = 3;

    it.each(["stdout", "stderr"])("does not retry the observed unknown model pricing rejection on %s", stream => {
      const error = {
        type: "unknown_model_ai_credits",
        message: 'Model "gpt-6.1-sol" has no AI credits pricing and no default pricing is configured.',
        model: "gpt-6.1-sol",
      };
      const events = [
        { type: "thread.started", thread_id: "01a0545e-6060-7472-9d50-d4a643611434" },
        { type: "error", message: JSON.stringify(error) },
        { type: "turn.failed", error: { message: JSON.stringify(error) } },
      ];
      const { result, calls } = runHarnessFixture(`for(const event of ${JSON.stringify(events)})process.${stream}.write(JSON.stringify(event)+"\\n");process.exitCode=1;`, { env: { GH_AW_HARNESS_MAX_RETRIES: "3" } });
      expect(calls).toHaveLength(1);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("unknown_model_ai_credits");
      expect(result.stderr).toContain("not retrying (terminal proxy guard)");
      expect(result.stderr).not.toContain("will retry");
    });

    /**
     * @param {{hasOutput: boolean, exitCode: number, output: string}} result
     * @param {number} attempt
     * @returns {boolean}
     */
    function shouldRetry(result, attempt) {
      if (result.exitCode === 0) return false;
      if (attempt === 0 && isAuthenticationFailedError(result.output)) return false;
      if (isMissingApiKeyError(result.output)) return false;
      if (hasNumerousPermissionDeniedIssues(result.output)) return false;
      const nonRetryableGuard = detectNonRetryableHarnessGuard(result.output);
      if (nonRetryableGuard.aiCreditsExceeded || nonRetryableGuard.awfAPIProxyBlockingRequests || nonRetryableGuard.goalAlreadyActive || nonRetryableGuard.maxRunsExceeded) return false;
      const isRateLimit = isRateLimitError(result.output);
      const isTokenPerMinuteRateLimit = isTokenPerMinuteRateLimitError(result.output);
      if (isTokenPerMinuteRateLimit) return false;
      if (isRateLimit && isReconnectExhaustedError(result.output)) return false;
      const isTransient = isRateLimit || isServerError(result.output);
      return attempt < MAX_RETRIES && (result.hasOutput || isTransient);
    }

    it("does not retry a provider invalid_request_error failure", () => {
      const tempDir = makeHarnessTempDir("codex-invalid-request-error-");
      const safeOutputsPath = path.join(tempDir, "safeoutputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
fs.appendFileSync(process.env.CODEX_HARNESS_STUB_CALLS, "called\\n");
process.stderr.write(JSON.stringify({ type: "turn.failed", error: { message: JSON.stringify({ error: { type: "invalid_request_error" } }) } }) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
        },
        encoding: "utf8",
        timeout: 10000,
      });

      expect(fs.readFileSync(callsPath, "utf8").trim().split("\n")).toHaveLength(1);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("isInvalidRequestError=true");
      expect(result.stderr).toContain("invalid_request_error (HTTP 400) — not retrying");
      expect(JSON.parse(fs.readFileSync(safeOutputsPath, "utf8"))).toEqual({
        type: "report_incomplete",
        reason: "infrastructure_error",
        details: expect.stringContaining("Codex request-body rejection (invalid_request_error)"),
      });
    });

    it("does not retry the observed unsupported-model 'tools' unknown_parameter failure", () => {
      const tempDir = makeHarnessTempDir("codex-unsupported-model-tools-");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
fs.appendFileSync(process.env.CODEX_HARNESS_STUB_CALLS, "called\\n");
process.stderr.write(JSON.stringify({ type: "thread.started", thread_id: "01a0545e-6060-7472-9d50-d4a643611434" }) + "\\n");
process.stderr.write(JSON.stringify({ type: "turn.started" }) + "\\n");
const errorMessage = JSON.stringify({ error: { message: "Invalid value: 'custom'", type: "invalid_request_error", param: "tools", code: "unknown_parameter" } });
process.stderr.write(JSON.stringify({ type: "error", message: errorMessage }) + "\\n");
process.stderr.write(JSON.stringify({ type: "turn.failed", error: { message: errorMessage } }) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        },
        encoding: "utf8",
        timeout: 10000,
      });

      expect(fs.readFileSync(callsPath, "utf8").trim().split("\n")).toHaveLength(1);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("isUnsupportedModelToolsError=true");
      expect(result.stderr).toContain("configured model does not support Codex's required tool-calling schema");
      expect(result.stderr).toContain("not retrying");
    });

    it("exits 0 when the AWF API proxy returns HTTP 403 max-AI-credits as an authentication failure", () => {
      // Same proxy signature as the claude_harness.test.cjs regression, replayed against codex.
      // CODEX_API_KEY is set so the `!isMissingApiKey` guard cannot suppress the budget path.
      const tempDir = makeHarnessTempDir("codex-ai-credits-proxy-403-");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
fs.appendFileSync(process.env.CODEX_HARNESS_STUB_CALLS, "called\\n");
process.stdout.write(JSON.stringify({ type: "error", error: "authentication_failed", message: "Failed to authenticate. API Error: 403 Maximum AI credits exceeded (302.111025 / 300)." }) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "0",
        },
        encoding: "utf8",
        timeout: 10000,
      });

      expect(fs.readFileSync(callsPath, "utf8").trim().split("\n")).toHaveLength(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("trusted budget-abort evidence");
      expect(result.stderr).toContain("AI credits budget enforced");
    });

    it("retries on rate limit error even without output", () => {
      const result = { exitCode: 1, hasOutput: false, output: "rate_limit_exceeded" };
      expect(shouldRetry(result, 0)).toBe(true);
    });

    it("retries on server error even without output", () => {
      const result = { exitCode: 1, hasOutput: false, output: "InternalServerError" };
      expect(shouldRetry(result, 0)).toBe(true);
    });

    it("retries on any other non-zero exit when session produced output", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Error: connection reset" };
      expect(shouldRetry(result, 0)).toBe(true);
    });

    it("does not retry when first attempt fails authentication", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Authentication failed (Request ID: ABC123)" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when missing API key is detected (any attempt)", () => {
      const result = { exitCode: 1, hasOutput: false, output: "ERROR: Missing environment variable: `OPENAI_API_KEY`" };
      expect(shouldRetry(result, 0)).toBe(false);
      expect(shouldRetry(result, 1)).toBe(false);
    });

    it("does not retry when no output was produced and no transient error", () => {
      const result = { exitCode: 1, hasOutput: false, output: "" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry after retries are exhausted", () => {
      const result = { exitCode: 1, hasOutput: true, output: "rate_limit_exceeded" };
      expect(shouldRetry(result, MAX_RETRIES)).toBe(false);
    });

    it("does not retry on success", () => {
      const result = { exitCode: 0, hasOutput: true, output: "Task complete" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when numerous permission-denied issues are present", () => {
      const result = { exitCode: 1, hasOutput: true, output: "permission denied\npermission denied\npermission denied" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when codex reports an existing active goal", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: "cannot create a new goal because this thread already has a goal; use update_goal only when the existing goal is complete",
      };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when maximum LLM invocations are exceeded", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: '{"error":{"type":"max_runs_exceeded","message":"Maximum LLM invocations exceeded (20 / 20).","invocation_count":20,"max_runs":20}}',
      };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry on token-per-minute rate limit wording", () => {
      const result = {
        exitCode: 1,
        hasOutput: false,
        output: '{"type":"error","message":"Rate limit reached for gpt-4o-mini in organization org-xxx on tokens per min (TPM): Limit 200000, Used 50000, Requested 35000. Please try again in 615ms."}',
      };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry on token-per-minute rate limit wording even with partial output", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: '{"type":"error","message":"Rate limit reached for gpt-4o-mini in organization org-xxx on tokens per min (TPM): Limit 200000, Used 50000, Requested 35000. Please try again in 615ms."}',
      };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when rate-limit reconnects are exhausted (non-TPM rate limit)", () => {
      // Simulates the real log format: multiple Reconnecting... lines appear in
      // the output as codex retries the stream. The final "5/5" line is what
      // triggers the exhausted-reconnect detection; intermediate lines (1/5, 2/5)
      // confirm that the function ignores non-final attempts.
      const output =
        '{"type":"error","message":"Reconnecting... 1/5 (stream disconnected before completion: RateLimitError)"}\n' +
        '{"type":"error","message":"Reconnecting... 2/5 (stream disconnected before completion: RateLimitError)"}\n' +
        '{"type":"error","message":"Reconnecting... 5/5 (stream disconnected before completion: RateLimitError)"}';
      const result = { exitCode: 1, hasOutput: true, output };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("retries when reconnects are exhausted but no rate-limit error is present", () => {
      const output =
        '{"type":"error","message":"Reconnecting... 1/5 (stream disconnected before completion: Connection timed out)"}\n' + '{"type":"error","message":"Reconnecting... 5/5 (stream disconnected before completion: Connection timed out)"}';
      const result = { exitCode: 1, hasOutput: true, output };
      expect(shouldRetry(result, 0)).toBe(true);
    });
  });

  describe("isReconnectExhaustedError", () => {
    it("returns true when output contains Reconnecting N/N pattern (same numbers)", () => {
      expect(isReconnectExhaustedError("Reconnecting... 5/5 (some error)")).toBe(true);
    });

    it("returns true for last reconnect embedded in JSON output", () => {
      const output = '{"type":"error","message":"Reconnecting... 5/5 (stream disconnected before completion: Rate limit reached for gpt-4o-mini...)"}';
      expect(isReconnectExhaustedError(output)).toBe(true);
    });

    it("returns false when reconnect attempt is not the last (different numbers)", () => {
      expect(isReconnectExhaustedError("Reconnecting... 1/5 (some error)")).toBe(false);
      expect(isReconnectExhaustedError("Reconnecting... 3/5 (some error)")).toBe(false);
    });

    it("returns false when output has no reconnect messages", () => {
      expect(isReconnectExhaustedError("rate_limit_exceeded")).toBe(false);
      expect(isReconnectExhaustedError("")).toBe(false);
    });

    it("returns true for multi-digit N/N", () => {
      expect(isReconnectExhaustedError("Reconnecting... 10/10 (error)")).toBe(true);
    });

    it("returns false for N/M where N !== M", () => {
      expect(isReconnectExhaustedError("Reconnecting... 10/15 (error)")).toBe(false);
    });
  });

  describe("noop pre-flight and retry guard", () => {
    it("skips the agent when a noop is already in safe-outputs before the run", () => {
      const tempDir = makeHarnessTempDir("codex-noop-preflight-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      fs.writeFileSync(safeOutputsPath, '{"type":"noop","message":"nothing to do"}\n', "utf8");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.exit(0);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: { ...process.env, CODEX_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath },
        encoding: "utf8",
        timeout: 10000,
      });
      // Agent stub should never have been invoked
      const stubCallCount = fs.existsSync(callsPath) ? fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length : 0;
      expect(stubCallCount).toBe(0);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("pre-flight: noop message found in safe-outputs");
    });

    it("does not retry after a failed run when a noop was written to safe-outputs", () => {
      const tempDir = makeHarnessTempDir("codex-noop-retry-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes a noop on the first call then fails; harness must not retry.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"noop",message:"nothing to do"}) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Only one attempt — no retries after noop detected
      expect(callCount).toBe(1);
      // Harness exits 0 because noop means the work is done
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("noop message found in safe-outputs — not retrying");
    });

    it("exits 0 without retrying when the LLM invocation cap is saturated but the expected safe-output was already produced", () => {
      const tempDir = makeHarnessTempDir("codex-invocation-cap-suppression-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes an expected safe-output then fails with the pooled invocation-cap error.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add_comment",body:"ADR reviewed"}) + "\\n");
process.stderr.write('{"error":{"type":"max_runs_exceeded","message":"Maximum LLM invocations exceeded (20 / 20)."}}\\n');
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: { ...process.env, CODEX_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath, CODEX_API_KEY: "fake-key-for-test" },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Only one attempt — invocation cap exhaustion is never retried
      expect(callCount).toBe(1);
      // Harness exits 0 because the core work (add_comment) already succeeded
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("invocation cap saturated but safe-outputs already contain expected output");
    });
  });

  describe("post-result watchdog suppression when terminal safe-output already produced", () => {
    it("exits 0 without retrying when a terminal safe-output was produced and the process hangs on exit", () => {
      const tempDir = makeHarnessTempDir("codex-watchdog-suppression-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes a terminal safe-output, then remains alive until SIGTERM.
      // This exercises the watchdog-fired path end-to-end.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add-labels",labels:["spam"]}) + "\\n");
process.stdout.write("Label applied. Continuing cleanup...\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Only one attempt — no retries when watchdog suppression applies
      expect(callCount).toBe(1);
      // Harness exits 0 because the terminal safe-output was already produced
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("post-result watchdog fired after terminal safe-output was emitted");
      expect(result.stderr).toContain("late-activity exit suppressed");
    });

    it("exits 0 without retrying when a noop safe-output was produced and the process hangs on exit", () => {
      const tempDir = makeHarnessTempDir("codex-watchdog-noop-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes a noop safe-output, then remains alive until SIGTERM.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"noop",message:"nothing to do"}) + "\\n");
process.stdout.write("Noop recorded. Waiting...\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      expect(callCount).toBe(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("post-result watchdog fired after terminal safe-output was emitted");
      expect(result.stderr).toContain("late-activity exit suppressed");
    });

    it("exits 0 without retrying when a report_incomplete safe-output was produced and the process hangs on exit", () => {
      const tempDir = makeHarnessTempDir("codex-watchdog-report-incomplete-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"report_incomplete",reason:"infrastructure_error"}) + "\\n");
process.stdout.write("report_incomplete recorded. Waiting...\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      expect(callCount).toBe(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("post-result watchdog fired after terminal safe-output was emitted");
      expect(result.stderr).toContain("late-activity exit suppressed");
    });

    it("still retries partial_execution when no terminal safe-output was produced (watchdog not armed)", () => {
      const tempDir = makeHarnessTempDir("codex-watchdog-no-output-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub produces output but exits 1 without writing any safe-output.
      // The watchdog cannot fire (no terminal safe-output to arm it), so this
      // should fall through to the normal partial-execution retry path.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("partial work done\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          // Override retry config to keep the test fast.
          GH_AW_HARNESS_MAX_RETRIES: "1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Should retry (2 attempts total with max_retries=1)
      expect(callCount).toBeGreaterThan(1);
      // Harness exits 1 because retries are exhausted with no output
      expect(result.status).toBe(1);
      expect(result.stderr).not.toContain("late-activity exit suppressed");
    });

    it("never replays an attempt that already staged task output", () => {
      const tempDir = makeHarnessTempDir("codex-watchdog-baseline-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Attempt 0: writes a terminal safe-output, produces output, then exits 1 (no hang).
      // Attempt 1+: produces output and exits 1 without writing anything new to safe-outputs.
      // The watchdog on attempt 1 must NOT arm from attempt 0's output.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
const calls = fs.existsSync(callsPath) ? fs.readFileSync(callsPath, "utf8").trim().split("\\n").filter(Boolean).length : 0;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
if (calls === 0) {
  // First attempt: write terminal output and exit immediately
  fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add-labels",labels:["spam"]}) + "\\n");
}
// All attempts: produce output (so harness classifies as partial execution and retries)
process.stdout.write("partial work done\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
          GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Preserve the staged output without replaying the original task.
      expect(callCount).toBe(1);
      expect(result.status).toBe(1);
      expect(
        fs
          .readFileSync(safeOutputsPath, "utf8")
          .trim()
          .split("\n")
          .map(line => JSON.parse(line))
          .filter(record => record.type === "add-labels")
      ).toHaveLength(1);
      expect(result.stderr).toContain("task output already staged");
      // The watchdog-suppression path must not have fired
      expect(result.stderr).not.toContain("late-activity exit suppressed");
    });
  });

  describe("context rebuild circuit breaker termination", () => {
    it("stops and fails the run when the guard fires even if the process exits 0 on SIGTERM", () => {
      const tempDir = makeHarnessTempDir("codex-rebuild-breaker-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const tokenUsagePath = path.join(tempDir, "token-usage.jsonl");
      const safeOutputsCLI = path.join(tempDir, "safeoutputs.cjs");
      fs.writeFileSync(
        safeOutputsCLI,
        `#!${process.execPath}
const fs = require("fs");
const args = process.argv.slice(2);
const item = { type: args[0] };
for (let i = 1; i < args.length; i += 2) item[args[i].slice(2)] = args[i + 1];
fs.appendFileSync(process.env.GH_AW_SAFE_OUTPUTS, JSON.stringify(item) + "\\n");
`
      );
      fs.chmodSync(safeOutputsCLI, 0o700);
      // Preserve any pre-existing token-usage log so the test never destroys real data,
      // while still always executing its assertions.
      const previousTokenUsage = fs.existsSync(tokenUsagePath) ? fs.readFileSync(tokenUsagePath) : null;
      fs.mkdirSync(path.dirname(tokenUsagePath), { recursive: true });
      fs.writeFileSync(tokenUsagePath, [100, 100, 100].map(t => JSON.stringify({ input_tokens: t })).join("\n") + "\n", "utf8");
      // Stub stays alive until SIGTERM, then exits cleanly (exit code 0). Without the
      // guard-fired normalization this would be misreported as a successful run.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("rebuilding context...\\n");
process.on("SIGTERM", () => process.exit(0));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do the work", "utf8");

      try {
        const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
          cwd: path.dirname(require.resolve("./codex_harness.cjs")),
          env: {
            ...process.env,
            CODEX_HARNESS_STUB_CALLS: callsPath,
            GH_AW_SAFE_OUTPUTS: safeOutputsPath,
            GH_AW_SAFEOUTPUTS_CLI: safeOutputsCLI,
            CODEX_API_KEY: "fake-key-for-test",
            GH_AW_HARNESS_MAX_RETRIES: "1",
            GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
            GH_AW_CODEX_MAX_REBUILD_FACTOR: "2",
            GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS: "10",
            GH_AW_CODEX_REBUILD_GUARD_POLL_MS: "1000",
            GH_AW_CODEX_REBUILD_GUARD_TERM_GRACE_MS: "250",
            GH_AW_CODEX_TOKEN_USAGE_PATH: tokenUsagePath,
          },
          encoding: "utf8",
          timeout: 20000,
        });
        const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
        // The circuit breaker stops the retry loop after the first attempt.
        expect(callCount).toBe(1);
        expect(result.status).toBe(1);
        expect(result.stderr).toContain("runtime guard requested termination");
        expect(result.stderr).toContain("normalizing exit code to 1");
        expect(result.stderr).toContain("not retrying (circuit breaker)");
        const items = fs
          .readFileSync(safeOutputsPath, "utf8")
          .trim()
          .split("\n")
          .map(line => JSON.parse(line));
        expect(items).toHaveLength(1);
        expect(items[0]).toMatchObject({ type: "report_incomplete", reason: "infrastructure_error" });
        expect(items[0].details).toMatch(/^Codex run stopped by a runtime guard\./);
        expect(items[0].details).toContain("context-rebuild circuit breaker tripped: rebuild_factor=3.00 cumulative_input_tokens=300 thresholds=2/10");
      } finally {
        if (previousTokenUsage === null) {
          fs.rmSync(tokenUsagePath, { force: true });
        } else {
          fs.writeFileSync(tokenUsagePath, previousTokenUsage);
        }
      }
    });

    it("allows the post-result watchdog to report success when the breaker threshold is crossed after terminal output", () => {
      const tempDir = makeHarnessTempDir("codex-rebuild-breaker-safe-output-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const tokenUsagePath = path.join(tempDir, "token-usage.jsonl");
      const previousTokenUsage = fs.existsSync(tokenUsagePath) ? fs.readFileSync(tokenUsagePath) : null;
      fs.mkdirSync(path.dirname(tokenUsagePath), { recursive: true });
      fs.writeFileSync(tokenUsagePath, [100, 100, 100].map(t => JSON.stringify({ input_tokens: t })).join("\n") + "\n", "utf8");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"noop",message:"Metrics collection complete"}) + "\\n");
process.stdout.write("terminal safe-output written; waiting for slow shutdown\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "collect metrics", "utf8");

      try {
        const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
          cwd: path.dirname(require.resolve("./codex_harness.cjs")),
          env: {
            ...process.env,
            CODEX_HARNESS_STUB_CALLS: callsPath,
            GH_AW_SAFE_OUTPUTS: safeOutputsPath,
            CODEX_API_KEY: "fake-key-for-test",
            GH_AW_HARNESS_MAX_RETRIES: "1",
            GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
            GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
            GH_AW_CODEX_MAX_REBUILD_FACTOR: "2",
            GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS: "10",
            GH_AW_CODEX_REBUILD_GUARD_POLL_MS: "1000",
            GH_AW_CODEX_REBUILD_GUARD_TERM_GRACE_MS: "250",
            GH_AW_CODEX_TOKEN_USAGE_PATH: tokenUsagePath,
          },
          encoding: "utf8",
          timeout: 20000,
        });
        const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
        expect(callCount).toBe(1);
        expect(result.status).toBe(0);
        expect(result.stderr).toContain("context-rebuild circuit breaker threshold exceeded after terminal safe-output was emitted");
        expect(result.stderr).toContain("post-result watchdog fired after terminal safe-output was emitted");
        expect(result.stderr).not.toContain("not retrying (circuit breaker)");
        expect(result.stderr).not.toContain("report_incomplete emitted via safeoutputs CLI");
      } finally {
        if (previousTokenUsage === null) {
          fs.rmSync(tokenUsagePath, { force: true });
        } else {
          fs.writeFileSync(tokenUsagePath, previousTokenUsage);
        }
      }
    });
  });

  describe("resolvePostResultWatchdogIdleTimeoutMs", () => {
    it("uses a 2-minute shared default", () => {
      expect(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS).toBe(120000);
    });

    it("returns the default when no env var is set", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({})).toBe(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS);
    });

    it("returns the configured value when GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS is a positive number", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "5000" })).toBe(5000);
    });

    it("clamps to MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS when configured value is too small", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "1" })).toBe(MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS);
    });

    it("clamps to MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS when configured value is too large", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: String(MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS + 1) })).toBe(MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS);
    });

    it("returns the default for non-numeric input", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "not-a-number" })).toBe(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS);
    });

    it("returns the default for zero", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "0" })).toBe(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS);
    });

    it("returns the default for negative values", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "-100" })).toBe(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS);
    });
  });

  describe("retry configuration", () => {
    it("uses the default retry settings when env vars are unset", () => {
      expect(resolveRetryConfig({})).toEqual({
        maxRetries: 3,
        initialDelayMs: 5000,
        backoffMultiplier: 2,
        maxDelayMs: 60000,
      });
    });

    it("accepts env overrides for retry settings", () => {
      expect(
        resolveRetryConfig({
          GH_AW_HARNESS_MAX_RETRIES: "6",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "10000",
          GH_AW_HARNESS_BACKOFF_MULTIPLIER: "3",
          GH_AW_HARNESS_MAX_DELAY_MS: "180000",
        })
      ).toEqual({
        maxRetries: 6,
        initialDelayMs: 10000,
        backoffMultiplier: 3,
        maxDelayMs: 180000,
      });
    });

    it("falls back to defaults for invalid env values and logs a warning", () => {
      const logs = /** @type {string[]} */ [];
      const retryConfig = resolveRetryConfig(
        {
          GH_AW_HARNESS_MAX_RETRIES: "-1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "not-a-number",
        },
        msg => logs.push(msg)
      );
      expect(retryConfig).toEqual({
        maxRetries: 3,
        initialDelayMs: 5000,
        backoffMultiplier: 2,
        maxDelayMs: 60000,
      });
      expect(logs.some(msg => msg.includes("GH_AW_HARNESS_MAX_RETRIES"))).toBe(true);
      expect(logs.some(msg => msg.includes("GH_AW_HARNESS_INITIAL_DELAY_MS"))).toBe(true);
    });

    it("clamps max delay to at least initial delay", () => {
      const logs = /** @type {string[]} */ [];
      const retryConfig = resolveRetryConfig(
        {
          GH_AW_HARNESS_INITIAL_DELAY_MS: "30000",
          GH_AW_HARNESS_MAX_DELAY_MS: "1000",
        },
        msg => logs.push(msg)
      );
      expect(retryConfig.maxDelayMs).toBe(30000);
      expect(logs.some(msg => msg.includes("clamping max delay"))).toBe(true);
    });
  });

  describe("AI credits budget enforcement exits 0", () => {
    /**
     * @param {string} tempDir
     * @returns {string}
     */
    function writeTrustedAICreditsExceededAudit(tempDir) {
      const auditDir = path.join(tempDir, "sandbox", "firewall", "audit");
      const tokenUsageDir = path.join(auditDir, "api-proxy-logs");
      fs.mkdirSync(auditDir, { recursive: true });
      fs.mkdirSync(tokenUsageDir, { recursive: true });
      fs.writeFileSync(path.join(auditDir, "log.jsonl"), `${JSON.stringify({ type: "response", status: 200, max_ai_credits: 300 })}\n`, "utf8");
      fs.writeFileSync(path.join(tokenUsageDir, "token-usage.jsonl"), `${JSON.stringify({ status: 200, ai_credits_total: 302.111025 })}\n`, "utf8");
      return path.join(tempDir, "agent-output.json");
    }

    it("exits 0 when the agent outputs max_ai_credits_exceeded and the CLI exits non-zero", () => {
      const tempDir = makeHarnessTempDir("codex-ai-credits-exceeded-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const agentOutputPath = writeTrustedAICreditsExceededAudit(tempDir);
      // Stub emits the AI-credits-exceeded marker on stdout (as the AWF firewall would)
      // then exits non-zero.  The harness must detect this, set lastExitCode=0, and exit 0.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: max_ai_credits_exceeded=true\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_AGENT_OUTPUT: agentOutputPath,
          CODEX_API_KEY: "fake-key-for-test",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Only one attempt — credit limit is non-retryable
      expect(callCount).toBe(1);
      // Harness exits 0: budget enforcement is intentional, not a job failure
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("AI credits budget exceeded");
      expect(result.stderr).toContain("AI credits budget enforced");
    });

    it("exits 0 when the agent outputs ai_credits_rate_limit_error and the CLI exits non-zero", () => {
      const tempDir = makeHarnessTempDir("codex-ai-credits-rate-limit-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const agentOutputPath = writeTrustedAICreditsExceededAudit(tempDir);
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: ai_credits_rate_limit_error=true\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_AGENT_OUTPUT: agentOutputPath,
          CODEX_API_KEY: "fake-key-for-test",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      expect(callCount).toBe(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("AI credits budget enforced");
    });

    it("keeps non-zero exit for auth failure even when AI-credit markers and trusted audit are present", () => {
      const tempDir = makeHarnessTempDir("codex-auth-failure-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const agentOutputPath = writeTrustedAICreditsExceededAudit(tempDir);
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: max_ai_credits_exceeded=true\\n");
process.stdout.write("Authentication failed (Request ID: 123)\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_AGENT_OUTPUT: agentOutputPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "0",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      // Harness exits 1: normal non-credit failures still fail the job
      expect(result.status).toBe(1);
      expect(result.stderr).not.toContain("AI credits budget enforced");
    });

    it("keeps non-zero exit when AI-credit marker appears without trusted firewall audit evidence", () => {
      const tempDir = makeHarnessTempDir("codex-ai-credits-untrusted-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: max_ai_credits_exceeded=true\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "0",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("without trusted firewall audit confirmation");
    });
  });
});

describe("Codex native sub-agent roles before spawn (github/gh-aw#70939)", () => {
  const runs = require("./fixtures/claude_codex_subagent_runs.cjs");

  /**
   * Stage agents as the activation artifact does and run the harness. The stub CLI snapshots
   * `$CODEX_HOME/agents` when it starts, so assertions see the state at spawn time.
   */
  function runWithAgents({ agents, reflectData = runs.arxivReflect(), env = {} }) {
    const dir = makeHarnessTempDir("codex-subagents-");
    const ostmp = path.join(dir, "ostmp");
    const recordsDir = path.join(dir, "records");
    const codexHome = path.join(dir, "codex-home");
    const staged = path.join(ostmp, "gh-aw", ".codex", "agents");
    fs.mkdirSync(staged, { recursive: true });
    fs.mkdirSync(codexHome, { recursive: true });
    for (const [name, content] of Object.entries(agents)) fs.writeFileSync(path.join(staged, name), content);
    const { result, calls } = runHarnessFixture(
      `const p=require("path");const d=p.join(process.env.CODEX_HOME,"agents");fs.writeFileSync(p.join(process.env.CODEX_HOME,"roles-at-spawn.json"),JSON.stringify(fs.existsSync(d)?Object.fromEntries(fs.readdirSync(d).filter(n=>n.endsWith(".toml")).sort().map(n=>[n,fs.readFileSync(p.join(d,n),"utf8")])):null));`,
      {
        reflectData,
        env: { TMPDIR: ostmp, CODEX_HOME: codexHome, AWF_REFLECT_ENABLED: "1", GH_AW_LLM_PROVIDER: "openai", GH_AW_TMP_DIR: recordsDir, GH_AW_AWF_CONFIG_PATH: path.join(dir, "missing-awf-config.json"), GH_AW_PHASE: "", ...env },
      }
    );
    const recordPath = path.join(recordsDir, "agent", "model-resolution.json");
    const records = fs.existsSync(recordPath) ? JSON.parse(fs.readFileSync(recordPath, "utf8")) : [];
    const snapshotPath = path.join(codexHome, "roles-at-spawn.json");
    const rolesAtSpawn = fs.existsSync(snapshotPath) ? JSON.parse(fs.readFileSync(snapshotPath, "utf8")) : undefined;
    const stagedAfter = Object.fromEntries(fs.readdirSync(staged).map(name => [name, fs.readFileSync(path.join(staged, name), "utf8")]));
    return { result, calls, records, rolesAtSpawn, stagedAfter, codexHome };
  }

  const recordedAgents = () => Object.fromEntries(fs.readdirSync(runs.arxivAgentsDir).map(name => [name, fs.readFileSync(path.join(runs.arxivAgentsDir, name), "utf8")]));
  const roleField = (toml, key) =>
    JSON.parse(
      toml
        .split("\n")
        .find(line => line.startsWith(`${key} = `))
        .slice(key.length + 3)
    );

  it("codex_declared_small_writes_native_toml_before_spawn", () => {
    // Fixture mutation: the recorded paper-screener with its selector changed to `small`.
    const agent = recordedAgents()["paper-screener.md"].replace("model: openai/gpt-5.4-mini", "model: small");
    const { result, calls, records, rolesAtSpawn } = runWithAgents({ agents: { "paper-screener.md": agent } });

    expect(result.status, result.stderr).toBe(0);
    expect(calls).toHaveLength(1);
    expect(Object.keys(rolesAtSpawn)).toEqual(["paper-screener.toml"]);
    const toml = rolesAtSpawn["paper-screener.toml"];
    expect(roleField(toml, "name")).toBe("paper-screener");
    expect(roleField(toml, "description")).toBe("Fast relevance screening of arXiv paper abstracts for GitHub Agentic Workflows");
    expect(roleField(toml, "developer_instructions")).toBe(agent.slice(agent.indexOf("\n---\n") + 5).trim());
    expect(roleField(toml, "model")).toBe("gpt-5-mini-2025-08-07");
    expect(records.filter(record => record.role !== "main")).toEqual([{ engine: "codex", role: "paper-screener", configured_model: "small", resolved_model: "gpt-5-mini-2025-08-07", provider: "openai", status: "resolved", cause: null }]);
    expect(result.stderr).not.toMatch(/not applied|not loaded by Codex/);
  });

  it("writes the four recorded arXiv roles with their exact ids and leaves the staged Markdown unchanged", () => {
    const { result, calls, records, rolesAtSpawn, stagedAfter } = runWithAgents({ agents: recordedAgents() });

    expect(result.status, result.stderr).toBe(0);
    expect(calls).toHaveLength(1);
    expect(Object.fromEntries(Object.entries(rolesAtSpawn).map(([name, toml]) => [name, roleField(toml, "model")]))).toEqual({
      "opportunity-extractor.toml": "gpt-5.6-sol",
      "paper-screener.toml": "gpt-5.4-mini",
      "preliminary-investigator.toml": "gpt-5.4-mini",
      "relevance-ranker.toml": "gpt-5.4-mini",
    });
    expect(records.filter(record => record.role !== "main").map(record => [record.role, record.resolved_model, record.status])).toEqual([
      ["opportunity-extractor", "gpt-5.6-sol", "exact"],
      ["paper-screener", "gpt-5.4-mini", "exact"],
      ["preliminary-investigator", "gpt-5.4-mini", "exact"],
      ["relevance-ranker", "gpt-5.4-mini", "exact"],
    ]);
    expect(stagedAfter).toEqual(recordedAgents());
  });

  it("codex_declared_dead_model_fails_before_spawn", () => {
    // Fixture mutation: the recorded catalogue (also served on refresh) with gpt-5.4-mini removed;
    // the main model gpt-5.6-sol stays available.
    const reflectData = runs.arxivReflect();
    reflectData.endpoints.forEach(endpoint => endpoint.models && (endpoint.models = endpoint.models.filter(model => model !== "gpt-5.4-mini")));
    const { result, calls, records, codexHome } = runWithAgents({ agents: { "paper-screener.md": recordedAgents()["paper-screener.md"] }, reflectData });

    expect(result.status).not.toBe(0);
    expect(calls).toEqual([]);
    expect(records).toContainEqual({ engine: "codex", role: "paper-screener", configured_model: "openai/gpt-5.4-mini", resolved_model: null, provider: "openai", status: "failed", cause: "model_unavailable" });
    expect(result.stderr).toContain("Refusing to start Codex with a different sub-agent model.");
    expect(fs.existsSync(path.join(codexHome, "agents"))).toBe(false);
  });

  it("codex_no_declared_agents_is_noop", () => {
    const { result, calls, records, rolesAtSpawn } = runWithAgents({ agents: {} });

    expect(result.status, result.stderr).toBe(0);
    expect(calls).toHaveLength(1);
    expect(rolesAtSpawn).toBeNull();
    expect(records.filter(record => record.role !== "main")).toEqual([]);
    expect(result.stderr).not.toContain("codex sub-agent");
  });

  it("fails closed before spawn for a workflow-declared sub-agent named main", () => {
    const { result, calls, records } = runWithAgents({ agents: { "main.md": recordedAgents()["paper-screener.md"] } });

    expect(result.status).toBe(1);
    expect(calls).toEqual([]);
    expect(result.stderr).toContain("the sub-agent name 'main' is reserved for the Codex main model's resolution record");
    expect(records.filter(record => record.engine === "codex" && record.role === "main").every(record => record.configured_model !== "openai/gpt-5.4-mini")).toBe(true);
  });

  it("claude_codex_detection_skips_subagent_resolution (Codex)", () => {
    const { result, calls, records, rolesAtSpawn, stagedAfter } = runWithAgents({ agents: recordedAgents(), env: { GH_AW_PHASE: "detection" } });

    expect(result.status, result.stderr).toBe(0);
    expect(calls).toHaveLength(1);
    expect(records.filter(record => record.role !== "main")).toEqual([]);
    expect(rolesAtSpawn).toBeNull();
    expect(stagedAfter).toEqual(recordedAgents());
  });
});
