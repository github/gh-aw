const fs = require("fs");
const path = require("path");
const vm = require("vm");
const { resolveProviderEndpointFromReflect } = require("./awf_reflect.cjs");

const definitionPath = path.join(__dirname, "../../../.github/workflows/shared/cursor.md");
const definition = fs.readFileSync(definitionPath, "utf8");
const harnessMatch = definition.match(/^    harness-script: \|\n([\s\S]*?)(?=^    log-parser:)/m);
if (!harnessMatch) throw new Error("Cursor definition has no harness-script");
const harnessSource = harnessMatch[1].replace(/^      /gm, "");
const checksum = "6e9f17247ffeb5f8f7e2246b4bcd6bb26cb2d5a9f9a4b0012c9a80d868ed25b4";
const reflectData = {
  endpoints: [{ provider: "openai", configured: true, base_url: "http://api-proxy:12005", port: 12005 }],
};

async function runHarness(options = {}) {
  const mockFs = {
    existsSync: vi.fn(() => true),
    mkdirSync: vi.fn(),
    mkdtempSync: vi.fn(() => "/tmp/cursor-harness-test"),
    readFileSync: vi.fn(file => (file.endsWith(".tar.gz") ? Buffer.from("archive") : "Test prompt")),
    rmSync: vi.fn(),
    writeFileSync: vi.fn(),
  };
  const spawnSync = vi.fn((command, args) => {
    if (command.endsWith("/cursor-agent") && args[0] !== "--version") return { status: options.exitStatus ?? 0 };
    return { status: 0 };
  });
  const fetchAWFReflect = vi.fn(async () => options.reflectResult ?? { ok: true, reflectData });
  const logs = [];
  const processMock = {
    argv: ["node", "cursor_harness.cjs", "cursor-agent", "-p", "--force"],
    arch: "x64",
    env: {
      AWF_REFLECT_ENABLED: "1",
      CURSOR_API_KEY: "awf-proxy",
      CURSOR_MODEL: "cursor/auto",
      GH_AW_PROMPT: "/tmp/prompt.txt",
      ...options.env,
    },
    stderr: { write: message => logs.push(message) },
    exitCode: undefined,
  };
  await vm.runInNewContext(harnessSource, {
    process: processMock,
    require: name => {
      if (name === "fs") return mockFs;
      if (name === "crypto") return { createHash: () => ({ update: () => ({ digest: () => checksum }) }) };
      if (name === "child_process") return { spawnSync };
      if (name === "./awf_reflect.cjs") return { fetchAWFReflect, resolveProviderEndpointFromReflect };
      return require(name);
    },
  });
  return { mockFs, spawnSync, fetchAWFReflect, logs, processMock };
}

describe("Cursor shared engine harness", () => {
  it("binds both native endpoints to the reflected gateway, overriding direct environment URLs", async () => {
    const result = await runHarness({
      env: { CURSOR_API_ENDPOINT: "https://direct.example", CURSOR_API_BASE_URL: "https://direct.example" },
    });

    const execution = result.spawnSync.mock.calls.find(([command, args]) => command.endsWith("/cursor-agent") && args[0] !== "--version");
    expect(execution).toBeDefined();
    expect(execution[1]).toEqual(["-p", "--force", "--model", "auto", "--auth-token", "awf-proxy", "--endpoint", "http://api-proxy:12005", "--agent-endpoint", "http://api-proxy:12005", "--http-version", "1.1", "Test prompt"]);
    expect(execution[2].env).toMatchObject({
      CURSOR_API_KEY: "awf-proxy",
      CURSOR_API_ENDPOINT: "http://api-proxy:12005",
      CURSOR_API_BASE_URL: "http://api-proxy:12005",
      CURSOR_CONFIG_DIR: "/tmp/cursor-harness-test/config",
    });
    expect(result.processMock.exitCode).toBeUndefined();
    expect(result.mockFs.rmSync).toHaveBeenCalledWith("/tmp/cursor-harness-test", { recursive: true, force: true });
    const [configPath, configJSON, configOptions] = result.mockFs.writeFileSync.mock.calls[0];
    expect(configPath).toBe("/tmp/cursor-harness-test/config/cli-config.json");
    expect(JSON.parse(configJSON).network.useHttp1ForAgent).toBe(true);
    expect(configOptions).toEqual({ mode: 0o600 });
  });

  it.each([
    { name: "disabled AWF", env: { AWF_REFLECT_ENABLED: "" }, error: "Cursor requires the AWF LLM gateway" },
    { name: "failed reflection", reflectResult: { ok: false, reason: "timeout" }, error: "/reflect: timeout" },
    { name: "missing response", reflectResult: { ok: true }, error: "/reflect: empty response" },
    { name: "wrong provider", reflectResult: { ok: true, reflectData: { endpoints: [{ provider: "copilot", configured: true, port: 10002 }] } }, error: "No configured OpenAI gateway endpoint" },
    { name: "unconfigured provider", reflectResult: { ok: true, reflectData: { endpoints: [{ provider: "openai", configured: false, port: 10000 }] } }, error: "No configured OpenAI gateway endpoint" },
    { name: "invalid endpoint", reflectResult: { ok: true, reflectData: { endpoints: [{ provider: "openai", configured: true, base_url: "not a URL" }] } }, error: "No configured OpenAI gateway endpoint" },
  ])("fails before downloading or running Cursor for $name", async testCase => {
    const result = await runHarness(testCase);
    expect(result.processMock.exitCode).toBe(1);
    expect(result.spawnSync).not.toHaveBeenCalled();
    expect(result.logs.join("")).toContain(testCase.error);
    expect(result.mockFs.rmSync).toHaveBeenCalledOnce();
  });

  it("surfaces a nonzero Cursor execution status", async () => {
    const result = await runHarness({ exitStatus: 7 });
    expect(result.processMock.exitCode).toBe(1);
    expect(result.logs.join("")).toContain("Cursor Agent execution failed with exit code 7");
  });
});

describe("Cursor runner-side gateway authentication", () => {
  const prepareMatch = definition.match(/^      prepare-script: \|\n([\s\S]*?)(?=^      model-env-var:)/m);
  if (!prepareMatch) throw new Error("Cursor definition has no prepare-script");
  const prepareSource = prepareMatch[1].replace(/^        /gm, "");

  async function runPreparation(options = {}) {
    const stdout = [];
    const stderr = [];
    const processMock = {
      env: { OPENAI_BASE_URL: "cursor-proxy.example", OPENAI_API_KEY: "test-api-key", ...options.env },
      stdout: { write: message => stdout.push(message) },
      stderr: { write: message => stderr.push(message) },
      exitCode: undefined,
    };
    const fetchMock = vi.fn(async () => ({
      ok: options.ok ?? true,
      status: options.status ?? 200,
      json: async () => options.data ?? { accessToken: "test-session-token" },
    }));
    await vm.runInNewContext(prepareSource, { process: processMock, fetch: fetchMock, URL, AbortSignal });
    return { stdout, stderr, fetchMock, processMock };
  }

  it("exchanges the API key on the selected upstream and masks the captured session token", async () => {
    const result = await runPreparation();
    const [url, options] = result.fetchMock.mock.calls[0];
    expect(url.href).toBe("https://cursor-proxy.example/auth/exchange_user_api_key");
    expect(options).toMatchObject({ method: "POST", redirect: "error", body: "{}", headers: { Authorization: "Bearer test-api-key" } });
    expect(result.stdout).toEqual(["test-session-token"]);
    expect(result.stderr).toEqual(["::add-mask::test-session-token\n"]);
    expect(result.processMock.exitCode).toBeUndefined();
  });

  it.each([
    { env: { OPENAI_BASE_URL: "" } },
    { env: { OPENAI_API_KEY: "" } },
    { env: { OPENAI_BASE_URL: "http://cursor-proxy.example" } },
    { env: { OPENAI_BASE_URL: "https://user:pass@cursor-proxy.example" } },
    { ok: false, status: 401 },
    { data: {} },
    { data: { accessToken: "token\ninjection" } },
  ])("fails closed without emitting any credential for invalid authentication %j", async options => {
    const result = await runPreparation(options);
    expect(result.stdout).toEqual([]);
    expect(result.processMock.exitCode).toBe(1);
    expect(result.stderr.join("")).toContain("Cursor gateway authentication failed");
    expect(result.stderr.join("")).not.toContain("test-api-key");
  });
});
