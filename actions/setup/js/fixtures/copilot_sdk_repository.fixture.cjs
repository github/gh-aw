"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { execFileSync } = require("node:child_process");
const { createRequire } = require("node:module");
const { copySafeOutputsBundle } = require("./safeoutputs_bundle.fixture.cjs");
const { requireGatewayBinary, startMCPGateway, withTimeout } = require("./copilot_sdk_gateway.fixture.cjs");

// Capture the explicit test input before prepareFixture erases the ambient environment.
const gatewayBinaryInput = process.env.GH_AW_TEST_MCP_GATEWAY_BINARY;
const modeArgument = process.argv[2] ?? (gatewayBinaryInput === undefined ? "--direct" : "--gateway");
assert.ok(process.argv.length <= 3 && ["--direct", "--gateway"].includes(modeArgument), "Expected --direct or --gateway");
const mode = modeArgument.slice(2);
const gatewayBinary = mode === "gateway" ? requireGatewayBinary(gatewayBinaryInput) : null;

const runtimeDirectory = path.resolve(__dirname, "..");
const load = createRequire(path.join(runtimeDirectory, "copilot_sdk_session.cjs"));
const sdk = load("@github/copilot-sdk");
const { parseCopilotSDKToolConfig, buildCopilotSDKSessionToolConfig } = load("./copilot_sdk_tool_config.cjs");
const { createCopilotSDKRepositoryRuntime } = load("./copilot_sdk_repo_tools.cjs");
const { restrictCopilotSDKRepositoryCatalog } = load("./copilot_sdk_tool_catalog.cjs");
const { buildCopilotSDKPermissionHandler } = load("./copilot_sdk_permissions.cjs");
const { parseCopilotSDKMCPConfig } = load("./copilot_sdk_mcp_config.cjs");
const { getPatchPathForBranch, getPatchPathForBranchInRepo } = load("./git_patch_utils.cjs");
const { MAX_REPOSITORY_FAILURE_BYTES } = load("./copilot_sdk_repo_diagnostics.cjs");

function prepareFixture(scratch, remaining) {
  const root = path.join(scratch, "checkout");
  const home = path.join(scratch, "home");
  const sdkHome = path.join(scratch, "sdk-home");
  const goCache = path.join(scratch, "go-build");
  const goModCache = path.join(scratch, "go-mod");
  for (const directory of [root, home, sdkHome, goCache, goModCache]) fs.mkdirSync(directory);
  const safeOutputsBundle = path.join(scratch, "safeoutputs-bundle");
  copySafeOutputsBundle(safeOutputsBundle);
  const loadSafeOutputs = createRequire(path.join(safeOutputsBundle, "safe_outputs_mcp_server_http.cjs"));
  fs.mkdirSync(path.join(root, "docs"));
  const branch = `automation/sdk-${randomUUID()}`;
  const artifacts = [getPatchPathForBranch(branch), getPatchPathForBranchInRepo(branch, "fixture/repository")].map(filename => path.resolve(filename));
  const minimalEnv = Object.fromEntries(["PATH", "Path", "SystemRoot", "WINDIR", "PATHEXT"].filter(key => process.env[key]).map(key => [key, process.env[key]]));
  const gitConfig = path.join(home, "gitconfig");
  fs.writeFileSync(gitConfig, "");
  Object.assign(minimalEnv, { HOME: home, USERPROFILE: home, TEMP: scratch, TMP: scratch, TMPDIR: scratch, GIT_CONFIG_GLOBAL: gitConfig, GIT_CONFIG_NOSYSTEM: "1", GIT_ALLOW_PROTOCOL: "", GIT_TERMINAL_PROMPT: "0" });
  const goRoot = execFileSync("go", ["env", "GOROOT"], {
    cwd: home,
    env: { ...minimalEnv, GOTOOLCHAIN: "local", GOWORK: "off", GOENV: "off" },
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    timeout: Math.min(30_000, remaining()),
  }).trim();
  function git(args) {
    return execFileSync("git", ["-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", ...args], {
      cwd: root,
      env: minimalEnv,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      timeout: Math.min(30_000, remaining()),
    }).trim();
  }
  git(["init", "-q", "-b", "main"]);
  fs.writeFileSync(path.join(root, ".gitignore"), "validation-receipt.json\n");
  fs.writeFileSync(path.join(root, "go.mod"), "module fixture\n\ngo 1.20\n");
  fs.writeFileSync(path.join(root, "main.go"), 'package main\n\nimport "fmt"\n\nfunc greeting() string { return "Hello" }\n\nfunc main() { fmt.Println(greeting()) }\n');
  const testSource = 'package main\n\nimport "testing"\n\nfunc TestGreeting(t *testing.T) {\n\tif greeting() != "Hello" {\n\t\tt.Fatal("unexpected greeting")\n\t}\n}\n';
  const diagnosticSource = "package diagnostics\n\nfunc Value() int { return 1 }\n";
  fs.writeFileSync(path.join(root, "main_test.go"), testSource);
  fs.writeFileSync(path.join(root, "docs", "diagnostic.go"), diagnosticSource);
  fs.writeFileSync(path.join(root, "README.md"), "A greeting command.\n");
  git(["add", "--all", "--", "."]);
  git(["commit", "-q", "-m", "Greeting command\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"]);
  const baseline = git(["rev-parse", "HEAD"]);
  git(["remote", "add", "origin", "https://github.com/fixture/repository.git"]);
  git(["update-ref", "refs/remotes/origin/main", baseline]);
  git(["symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main"]);

  const policy = {
    "target-repo": "fixture/repository",
    base_branch: "main",
    allowed_branches: ["automation/*"],
    allowed_files: ["README.md", "*.go", "docs/**"],
    protected_files: ["go.mod", "go.sum", "CHANGELOG.md"],
    protected_files_policy: "blocked",
  };
  const toolConfig = parseCopilotSDKToolConfig(
    JSON.stringify({
      version: 2,
      capabilities: { bash: false, edit: true, webFetch: false, webSearch: false, mcp: true, cliProxy: false },
      explicitlyDisabledTools: ["bash", "cli-proxy"],
      permissions: { allowedTools: ["read", "write", "go_repository", "safeoutputs", "github(get_file_contents)", "reference"] },
      profile: { id: "go-repository", repositoryDefaultBranch: "main", policy },
    })
  );
  const configuration = path.join(scratch, "safeoutputs.json");
  const definitions = path.join(scratch, "tools.json");
  const output = path.join(scratch, "output.jsonl");
  fs.writeFileSync(configuration, JSON.stringify({ noop: { max: 1 }, create_pull_request: { ...policy, max: 1, draft: true, patch_format: "am" } }));
  const tools = JSON.parse(fs.readFileSync(path.join(runtimeDirectory, "safe_outputs_tools.json"), "utf8"));
  fs.writeFileSync(definitions, JSON.stringify(tools.filter(tool => ["noop", "create_pull_request"].includes(tool.name))));
  for (const key of Object.keys(process.env)) delete process.env[key];
  Object.assign(process.env, minimalEnv, {
    GITHUB_WORKSPACE: root,
    GITHUB_REPOSITORY: "fixture/repository",
    GITHUB_SHA: baseline,
    GITHUB_REF_NAME: "main",
    GITHUB_REF: "refs/heads/main",
    GITHUB_EVENT_NAME: "workflow_dispatch",
    RUNNER_TEMP: scratch,
    GH_AW_SAFE_OUTPUTS_CONFIG_PATH: configuration,
    GH_AW_SAFE_OUTPUTS_TOOLS_PATH: definitions,
    GH_AW_SAFE_OUTPUTS: output,
    GH_AW_MCP_LOG_DIR: path.join(scratch, "mcp-logs"),
    GOROOT: goRoot,
    GOCACHE: goCache,
    GOMODCACHE: goModCache,
  });
  return { root, sdkHome, loadSafeOutputs, branch, artifacts, minimalEnv, git, baseline, toolConfig, output, testSource, diagnosticSource };
}

async function main() {
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-sdk-repository-fixture-"));
  // Leave the existing 240s outer timeout room for cancellation and owned-process cleanup.
  const deadline = Date.now() + 175_000;
  function remaining() {
    const milliseconds = deadline - Date.now();
    if (milliseconds <= 0) throw new Error("SDK repository fixture work deadline exceeded");
    return milliseconds;
  }
  const controller = new AbortController();
  const failures = [];
  const backendRequests = {};
  const backendErrors = [];
  let backendAuthFailures = 0;
  let reads = 0;
  let forbiddenCalls = 0;
  let providerRequests = 0;
  let unexpectedRequests = 0;
  let fixture;
  let listener;
  let gateway;
  let repository;
  let client;
  let session;
  let unsubscribe;
  let verifiedToolMetadata = [];
  let catalogVerified = false;
  let recorded = [];
  const permissionIdentities = [];
  let permissionDenials = 0;
  const actions = [];
  const nativeFailures = [];
  const nativeEvents = [];
  const operationTimings = [];
  async function checked(operation) {
    controller.signal.throwIfAborted();
    const result = await operation();
    controller.signal.throwIfAborted();
    return result;
  }
  async function cleanup(label, operation, timeoutMs = 5_000) {
    try {
      await withTimeout(operation, timeoutMs, label);
      return true;
    } catch (error) {
      failures.push(error);
      return false;
    }
  }
  try {
    fixture = prepareFixture(scratch, remaining);
    const { root, sdkHome, loadSafeOutputs, branch, minimalEnv, git, baseline, toolConfig, testSource, diagnosticSource } = fixture;
    const { MCPServer, MCPHTTPTransport } = load("./mcp_http_transport.cjs");
    const { createMCPServer } = loadSafeOutputs("./safe_outputs_mcp_server_http.cjs");
    const safeoutputs = createMCPServer().server;
    const github = new MCPServer({ name: "github", version: "1.0.0" }, { logDir: path.join(scratch, "mcp-logs") });
    github.tool(
      "get_file_contents",
      "Read a synthetic repository file.",
      { type: "object", additionalProperties: false, required: ["owner", "repo", "path"], properties: { owner: { type: "string" }, repo: { type: "string" }, path: { type: "string" } } },
      args => {
        assert.deepEqual(args, { owner: "fixture", repo: "repository", path: "README.md" });
        reads++;
        return { content: [{ type: "text", text: JSON.stringify({ path: args.path, content: "A greeting command.\n" }) }] };
      }
    );
    github.tool("delete_file", "Forbidden synthetic mutation.", { type: "object", properties: {} }, () => {
      forbiddenCalls++;
      throw new Error("An unapproved tool must not execute");
    });
    const reference = new MCPServer({ name: "reference", version: "1.0.0" }, { logDir: path.join(scratch, "mcp-logs") });
    for (let index = 0; index < 35; index++) reference.tool(`lookup_${index}`, "Read a synthetic reference.", { type: "object", properties: {} }, () => ({ content: [{ type: "text", text: "Reference" }] }));
    const transports = new Map();
    for (const [name, server] of [
      ["safeoutputs", safeoutputs],
      ["github", github],
      ["reference", reference],
    ]) {
      const transport = new MCPHTTPTransport({ enableJsonResponse: true, enableDnsRebindingProtection: false });
      await server.connect(transport);
      transports.set(`/mcp/${name}`, transport);
      backendRequests[`/mcp/${name}`] = { authenticatedRequests: 0, catalogRequests: 0, calls: [] };
    }
    // The SDK never receives this upstream token in gateway mode, so a direct bypass fails authentication.
    const backendToken = `fixture-backend-${randomUUID()}`;
    const backendHeaders = { Authorization: backendToken, "X-Agent-ID": backendToken };
    listener = http.createServer(async (request, response) => {
      const transport = transports.get(request.url);
      if (!transport) {
        if (request.url.startsWith("/provider")) providerRequests++;
        else unexpectedRequests++;
        response.writeHead(500, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ error: "External API and model requests are forbidden in this fixture" }));
        return;
      }
      if (request.headers.authorization !== backendToken || request.headers["x-agent-id"] !== backendToken) {
        backendAuthFailures++;
        response.writeHead(401, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ error: "Synthetic backend authentication failed" }));
        return;
      }
      try {
        const evidence = backendRequests[request.url];
        evidence.authenticatedRequests++;
        const chunks = [];
        let bytes = 0;
        for await (const chunk of request) {
          bytes += chunk.length;
          assert.ok(bytes <= 1024 * 1024, "Backend request exceeds fixture limit");
          chunks.push(chunk);
        }
        const body = chunks.length ? JSON.parse(Buffer.concat(chunks).toString("utf8")) : undefined;
        if (body?.method === "tools/list") evidence.catalogRequests++;
        if (body?.method === "tools/call") {
          assert.ok(evidence.calls.length < 16, "Backend tool calls exceed fixture limit");
          evidence.calls.push(body.params?.name);
        }
        await transport.handleRequest(request, response, body);
      } catch (error) {
        if (backendErrors.length < 8) backendErrors.push(error.message.slice(0, 1_000));
        if (response.headersSent) response.destroy(error);
        else {
          response.writeHead(500, { "Content-Type": "application/json" });
          response.end(JSON.stringify({ error: error.message }));
        }
      }
    });
    const listening = new Promise((resolve, reject) => {
      listener.once("error", reject);
      listener.listen(0, "127.0.0.1", resolve);
    });
    await withTimeout(() => listening, Math.min(5_000, remaining()), "Backend listener startup");
    const address = `http://127.0.0.1:${listener.address().port}`;
    process.env.GITHUB_API_URL = `${address}/github-api`;
    const upstreamServers = Object.fromEntries([...transports.keys()].map(route => [route.split("/").at(-1), { type: "http", url: `${address}${route}`, headers: backendHeaders }]));
    if (mode === "gateway") gateway = await startMCPGateway({ binary: gatewayBinary, scratch, mcpServers: upstreamServers, env: minimalEnv, startupTimeoutMs: Math.min(30_000, remaining()) });
    const mcpServers = parseCopilotSDKMCPConfig({
      mcpServers: Object.fromEntries(Object.entries(upstreamServers).map(([name, server]) => [name, { ...server, ...(gateway ? { url: `${gateway.address}/mcp/${name}`, headers: gateway.headers } : {}), tools: ["*"], timeout: 10_000 }])),
    });
    repository = createCopilotSDKRepositoryRuntime(sdk.defineTool, toolConfig.profile, { diagnosticSecrets: [backendToken, ...Object.values(gateway?.headers ?? {})] });
    client = new sdk.CopilotClient({
      connection: sdk.RuntimeConnection.forStdio(),
      mode: "empty",
      baseDirectory: sdkHome,
      workingDirectory: root,
      env: { ...minimalEnv, HOME: sdkHome, USERPROFILE: sdkHome, APPDATA: sdkHome, LOCALAPPDATA: sdkHome },
      useLoggedInUser: false,
      logLevel: "warning",
      telemetry: { exporterType: "file", filePath: path.join(scratch, "telemetry.jsonl"), captureContent: false },
    });
    async function exercise() {
      await checked(() => repository.initialize());
      await checked(() => client.start());
      const sessionTools = buildCopilotSDKSessionToolConfig(toolConfig, sdk, { repositoryTool: repository.tool });
      const permissionHandler = buildCopilotSDKPermissionHandler(toolConfig.permissions, sdk.approveAll, { workspaceRoot: root, getMCPToolMetadata: () => verifiedToolMetadata });
      session = await checked(() =>
        client.createSession({
          model: "offline-fixture",
          provider: { type: "openai", baseUrl: `${address}/provider`, wireApi: "completions" },
          onPermissionRequest: async (request, invocation) => {
            permissionIdentities.push(Object.fromEntries(["kind", "serverName", "toolName"].filter(key => key in request).map(key => [key, request[key]])));
            const decision = await permissionHandler(request, invocation);
            if (decision.kind === "reject") permissionDenials++;
            return decision;
          },
          ...sessionTools,
          mcpServers,
        })
      );
      const catalog = await checked(() =>
        restrictCopilotSDKRepositoryCatalog(session, { ToolSet: sdk.ToolSet, availableTools: sessionTools.availableTools, allowedTools: toolConfig.permissions.allowedTools, mcpServers, signal: controller.signal })
      );
      verifiedToolMetadata = catalog;
      assert.ok(catalog.length > 30, "Deferral threshold must be exercised");
      assert.ok(catalog.every(tool => !tool.deferLoading));
      assert.ok(!catalog.some(tool => /bash|powershell|^task$|agent$|^sql$|delete_file/.test(tool.name)));
      const mcpTools = catalog.filter(tool => tool.mcpServerName);
      assert.deepEqual(
        mcpTools.map(tool => `${tool.mcpServerName}/${tool.mcpToolName}`).sort(),
        ["github/get_file_contents", "safeoutputs/create_pull_request", "safeoutputs/noop", ...Array.from({ length: 35 }, (_, index) => `reference/lookup_${index}`)].sort()
      );
      for (const tool of mcpTools) assert.equal(tool.name, `${tool.mcpServerName}-${tool.mcpToolName}`, "Routed MCP catalogs must preserve canonical identities");
      catalogVerified = true;
      const repositoryCalls = new Set();
      let pendingCompletion;
      unsubscribe = session.on(event => {
        if (nativeEvents.length < 64) nativeEvents.push({ type: event.type, toolName: event.data.toolName ?? event.data.toolDescription?.name });
        if (event.type === "external_tool.requested" && event.data.toolName === "go_repository") repositoryCalls.add(event.data.requestId);
        if (event.type !== "external_tool.completed") return;
        if (repositoryCalls.delete(event.data.requestId)) pendingCompletion?.resolve(event.data);
      });
      const invoke = async (name, args) => {
        const result = await checked(() => session.rpc.tools.execute({ name, arguments: args }));
        assert.equal(result.resultType, "success", `${name}: ${result.textResultForLlm}`);
        return result;
      };
      await invoke("view", { path: path.join(root, "README.md") });
      await invoke("grep", { pattern: "greeting", paths: root, glob: "*.go", output_mode: "content" });
      await invoke("glob", { pattern: "*.go", paths: root });
      await invoke("github-get_file_contents", { owner: "fixture", repo: "repository", path: "README.md" });
      for (const name of ["bash", "write_bash", "task", "noop", "github-delete_file"]) {
        const result = await checked(() => session.rpc.tools.execute({ name, arguments: {} }));
        assert.equal(result.resultType, "failure", `${name} must be unavailable`);
      }
      const nativeRepo = async (action, extra = {}) => {
        assert.equal(pendingCompletion, undefined, "Repository invocations must stay sequential");
        const completion = Promise.withResolvers();
        pendingCompletion = completion;
        actions.push(action);
        const timing = { action, durationMs: 0, resultType: "pending" };
        operationTimings.push(timing);
        const started = Date.now();
        try {
          const result = await checked(() => session.rpc.tools.execute({ name: "go_repository", arguments: { action, ...extra } }));
          timing.resultType = result.resultType;
          const event = await checked(() => withTimeout(() => completion.promise, Math.min(5_000, remaining()), `${action} native completion event`));
          assert.ok(typeof event.requestId === "string" && event.requestId.length > 0, "External tool completion must correlate to a native request");
          return { result, event };
        } finally {
          timing.durationMs = Date.now() - started;
          pendingCompletion = undefined;
        }
      };
      const repo = async (action, extra = {}) => {
        const { result } = await nativeRepo(action, extra);
        assert.equal(result.resultType, "success", `${action}: ${result.textResultForLlm}`);
        return JSON.parse(result.textResultForLlm);
      };
      const repoFailure = async (action, markers) => {
        const { result, event } = await nativeRepo(action);
        assert.equal(result.resultType, "failure", `${action}: native result must fail`);
        assert.equal(typeof result.textResultForLlm, "string");
        assert.equal(typeof result.error, "string");
        assert.ok(Buffer.byteLength(JSON.stringify(result), "utf8") <= MAX_REPOSITORY_FAILURE_BYTES);
        for (const marker of markers) {
          assert.ok(result.textResultForLlm.includes(marker), `${action}: missing native result diagnostic ${marker}`);
          assert.ok(result.error.includes(marker), `${action}: missing native error diagnostic ${marker}`);
        }
        nativeFailures.push({ action, resultType: result.resultType, textResultForLlm: result.textResultForLlm, error: result.error, completionRequestId: event.requestId });
        assert.equal(git(["rev-parse", "HEAD"]), baseline, "Failed validation must not publish a commit");
      };
      await repo("status");
      await repo("prepare_branch", { branch });
      const receiptSource =
        testSource.replace('import "testing"', 'import (\n\t"os"\n\t"testing"\n)') +
        '\nfunc TestReceipt(t *testing.T) {\n\tif err := os.WriteFile("validation-receipt.json", []byte("receipt"), 0o600); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n';
      await invoke("edit", { path: path.join(root, "main_test.go"), old_str: testSource, new_str: receiptSource });
      await repoFailure("validate", ["Go validation changed or added files", "untracked addition: validation-receipt.json"]);
      assert.equal(fs.existsSync(path.join(root, "validation-receipt.json")), false, "The receipt belongs only to the rejected projection");
      await repoFailure("commit", ["Run validate successfully"]);
      const failingSource = testSource + '\nfunc TestFailure(t *testing.T) {\n\tt.Fatal("NATIVE_STDOUT_DIAGNOSTIC")\n}\n';
      await invoke("edit", { path: path.join(root, "main_test.go"), old_str: receiptSource, new_str: failingSource });
      // Go merges a test binary's streams. A second package's compiler failure
      // exercises the go command's real stderr independently of test stdout.
      await invoke("edit", { path: path.join(root, "docs", "diagnostic.go"), old_str: diagnosticSource, new_str: diagnosticSource.replace("return 1", "return NATIVE_STDERR_DIAGNOSTIC") });
      await repoFailure("validate", ["go test failed with exit code 1", "stdout:", "stderr:", "NATIVE_STDOUT_DIAGNOSTIC", "NATIVE_STDERR_DIAGNOSTIC"]);
      await repoFailure("commit", ["Run validate successfully"]);
      await invoke("edit", { path: path.join(root, "main_test.go"), old_str: failingSource, new_str: testSource });
      await invoke("edit", { path: path.join(root, "docs", "diagnostic.go"), old_str: diagnosticSource.replace("return 1", "return NATIVE_STDERR_DIAGNOSTIC"), new_str: diagnosticSource });
      await invoke("edit", { path: path.join(root, "main.go"), old_str: 'func greeting() string { return "Hello" }', new_str: '// greeting returns the default salutation.\nfunc greeting()string{return "Hello"}' });
      await invoke("create", { path: path.join(root, "docs", "usage.md"), file_text: "Run the command to print Hello.\n" });
      await repo("format");
      await repo("validate");
      const committed = await repo("commit");
      assert.equal(git(["rev-parse", "HEAD"]), committed.commit);
      assert.equal(git(["rev-parse", "main"]), baseline);
      await repo("diff");
      await invoke("safeoutputs-create_pull_request", {
        title: "Clarify greeting documentation",
        body: "Documents the greeting helper and adds the usage example. The existing Go tests, vet, and build completed successfully.",
        branch,
      });
      await invoke("safeoutputs-noop", { message: "COMPLETE: Native reporting was exercised without a model request." });
    }
    await withTimeout(exercise, remaining(), `SDK repository ${mode} fixture`);
  } catch (error) {
    failures.push(error);
  } finally {
    controller.abort(new Error("SDK repository fixture stopped"));
    repository?.abort();
    try {
      unsubscribe?.();
    } catch (error) {
      failures.push(error);
    }
    if (repository) await cleanup("Repository cleanup", () => repository.close());
    if (session) await cleanup("Session disconnect", () => session.disconnect());
    if (client) {
      const stopped = await cleanup("SDK stop", async () => {
        const errors = await client.stop();
        if (errors.length) throw new AggregateError(errors, `SDK fixture cleanup failed: ${errors.map(error => error.message).join("; ")}`);
      });
      if (!stopped) await cleanup("SDK force stop", () => client.forceStop());
    }
    if (gateway) {
      if (!failures.length) await cleanup("Gateway liveness", () => gateway.assertRunning());
      await cleanup("Gateway cleanup", () => gateway.stop(), 7_000);
    }
    if (listener?.listening) {
      await cleanup("Backend listener cleanup", async () => {
        await new Promise((resolve, reject) => {
          listener.close(error => (error ? reject(error) : resolve()));
          listener.closeAllConnections();
        });
      });
    }
    if (fixture) {
      await cleanup("Recorded output inspection", () => {
        if (fs.existsSync(fixture.output)) {
          recorded = fs
            .readFileSync(fixture.output, "utf8")
            .trim()
            .split(/\r?\n/)
            .map(line => JSON.parse(line));
        }
      });
      for (const artifact of fixture.artifacts) await cleanup("Fixture artifact cleanup", () => fs.rmSync(artifact, { force: true }));
    }
    await cleanup("Fixture scratch cleanup", () => fs.rmSync(scratch, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 }));
  }
  const routes = Object.keys(backendRequests).sort();
  const gatewayEvidence = gateway?.evidence() ?? null;
  const authChecked = Boolean(
    routes.length === 3 &&
    backendAuthFailures === 0 &&
    routes.every(route => backendRequests[route].authenticatedRequests > 0) &&
    (mode === "direct" || (gatewayEvidence && routes.every(route => gatewayEvidence.routesUsed.includes(route) && ["missing", "invalid", "bearer"].every(name => gatewayEvidence.authRejections[route]?.[name] === 401))))
  );
  try {
    assert.equal(forbiddenCalls, 0);
    assert.equal(providerRequests, 0);
    assert.equal(unexpectedRequests, 0);
    assert.equal(backendAuthFailures, 0);
    assert.equal(permissionDenials, 0);
    assert.deepEqual(backendErrors, []);
    if (!failures.length) {
      assert.ok(catalogVerified);
      assert.ok(authChecked, "Authenticated MCP routes must actually be exercised");
      assert.equal(reads, 1);
      assert.deepEqual(
        recorded.map(item => item.type),
        ["create_pull_request", "noop"]
      );
      assert.equal(recorded[0].branch, fixture.branch);
      for (const route of routes) assert.ok(backendRequests[route].catalogRequests > 0, `${route} backend catalog must be read`);
      assert.deepEqual(backendRequests["/mcp/github"].calls, ["get_file_contents"]);
      assert.deepEqual(backendRequests["/mcp/safeoutputs"].calls, ["create_pull_request", "noop"]);
      assert.deepEqual(backendRequests["/mcp/reference"].calls, []);
      for (const [serverName, toolName] of [
        ["github", "get_file_contents"],
        ["safeoutputs", "create_pull_request"],
        ["safeoutputs", "noop"],
      ]) {
        assert.ok(permissionIdentities.some(request => request.kind === "mcp" && request.serverName === serverName && request.toolName === `${serverName}-${toolName}`));
      }
    }
  } catch (error) {
    failures.push(error);
  }
  const evidence = {
    mode,
    catalogVerified,
    authChecked,
    providerRequests,
    unexpectedRequests,
    reads,
    forbiddenCalls,
    backendAuthFailures,
    backendErrors,
    backendRequests,
    nativeTools: verifiedToolMetadata.map(tool => tool.name),
    permissionIdentities,
    permissionDenials,
    actions,
    nativeFailures,
    nativeEvents,
    operationTimings,
    outputs: recorded.map(item => item.type),
    gateway: gatewayEvidence,
  };
  if (failures.length) {
    console.error(`SDK_REPOSITORY_DIAGNOSTICS=${JSON.stringify(evidence)}`);
    if (gateway) console.error(gateway.diagnostics());
    throw new AggregateError(failures, `SDK repository ${mode} fixture failed: ${failures.map(error => error.message).join("; ")}`);
  }
  return evidence;
}

main().then(
  evidence => console.log(`SDK_REPOSITORY_RESULT=${JSON.stringify(evidence)}`),
  error => {
    console.error(error.stack);
    process.exitCode = 1;
  }
);
