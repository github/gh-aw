import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";
import { readDocumentMetadata } from "./markdown_metadata.cjs";
import { Readable } from "node:stream";
import { readLockFileHeaders } from "./work_queue_provisioning.cjs";

const require = createRequire(import.meta.url);
const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");
const { parseToolArgs } = require("./mcp_cli_bridge.cjs");
const root = resolve(import.meta.dirname, "../../..");
const names = ["draft-pr-cleanup", "instructions-janitor", "daily-mcp-concurrency-analysis"];
const manifests = new Map(
  await Promise.all(
    [...names, "daily-file-diet", "daily-caveman-optimizer"].map(async name => {
      const { manifest } = await readLockFileHeaders(Readable.from([readFileSync(resolve(root, `.github/workflows/${name}.lock.yml`), "utf8")]));
      return [name, manifest];
    })
  )
);
const readWorkflow = name => {
  const source = readFileSync(resolve(root, `.github/workflows/${name}.md`), "utf8");
  const [, frontmatter, prompt] = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
  const document = readDocumentMetadata(frontmatter, { version: "1.2", uniqueKeys: true });
  const lock = readFileSync(resolve(root, `.github/workflows/${name}.lock.yml`), "utf8");
  const compiled = readDocumentMetadata(lock, { version: "1.2", uniqueKeys: true });
  const manifest = manifests.get(name);
  const steps = compiled.value.jobs.agent.steps;
  const sdkConfig = steps.find(step => step.env?.GH_AW_COPILOT_SDK_TOOL_CONFIG)?.env.GH_AW_COPILOT_SDK_TOOL_CONFIG;
  return { workflow: document.value, prompt, lock, manifest, steps, sdkConfig: sdkConfig && JSON.parse(sdkConfig) };
};

describe("workflow native MCP permission alignment", () => {
  it.each([799, 800, 801])("prepares exact File Diet metrics at the %i-line threshold without agent shell discovery", lines => {
    const { workflow, prompt, steps } = readWorkflow("daily-file-diet");
    const preparation = workflow.steps.find(step => step.name === "Prepare Go file size metrics");
    const execution = steps.find(step => step.name === "Execute GitHub Copilot CLI");
    expect(preparation.uses).toBe("actions/github-script@v9.0.0");
    expect(steps.findIndex(step => step.name === preparation.name)).toBeLessThan(steps.findIndex(step => step.name === "Execute GitHub Copilot CLI"));
    expect(workflow.tools.bash).toEqual(["cat /tmp/gh-aw/agent/file-diet-metrics.json"]);
    expect(execution.run).toContain("shell(cat /tmp/gh-aw/agent/file-diet-metrics.json)");
    expect(execution.run).not.toContain("-exec wc -l");
    expect(execution.run).not.toContain("--allow-all-tools");
    expect(prompt).toContain("`noop` safe-output tool");
    expect(prompt).toContain("largest_file.test_file");
    expect(prompt).not.toMatch(/^\s*(?:find |TEST_FILE=|if \[)/m);
    const directory = mkdtempSync(resolve(tmpdir(), "gh-aw-file-diet-"));
    try {
      const nested = resolve(directory, "pkg/nested/deeper");
      mkdirSync(nested, { recursive: true });
      writeFileSync(resolve(nested, "a.go"), "line\n".repeat(lines) + "unterminated");
      writeFileSync(resolve(nested, "a_test.go"), "test\n".repeat(400));
      writeFileSync(resolve(nested, "z.go"), "tie\n".repeat(lines));
      writeFileSync(resolve(nested, "ignored_test.go"), "test\n".repeat(2000));
      writeFileSync(resolve(nested, "ignored.txt"), "text\n".repeat(2000));
      writeFileSync(resolve(directory, "outside.go"), "outside\n".repeat(3000));
      symlinkSync(resolve(directory, "outside.go"), resolve(nested, "symlink.go"));
      symlinkSync(directory, resolve(nested, "cycle"));
      const outputPath = resolve(directory, "metrics/result.json");
      const prepare = () =>
        runInNewContext(
          preparation.with.script,
          {
            require,
            process: { env: { GITHUB_WORKSPACE: directory, FILE_DIET_METRICS_PATH: outputPath } },
          },
          { timeout: 1000 }
        );
      prepare();
      expect(JSON.parse(readFileSync(outputPath, "utf8"))).toEqual({
        healthy_threshold: 800,
        source_file_count: 2,
        largest_file: {
          path: "pkg/nested/deeper/a.go",
          line_count: lines,
          test_file: { path: "pkg/nested/deeper/a_test.go", line_count: 400 },
        },
      });
      rmSync(resolve(nested, "a_test.go"));
      prepare();
      expect(JSON.parse(readFileSync(outputPath, "utf8")).largest_file.test_file).toBeNull();
      symlinkSync(resolve(directory, "outside.go"), resolve(nested, "a_test.go"));
      prepare();
      expect(JSON.parse(readFileSync(outputPath, "utf8")).largest_file.test_file).toBeNull();
      rmSync(resolve(nested, "a.go"));
      rmSync(resolve(nested, "z.go"));
      expect(prepare).toThrow("No non-test Go source files found under pkg/");
      rmSync(outputPath);
      rmSync(resolve(directory, "pkg"), { recursive: true });
      expect(prepare).toThrow();
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it("advertises Caveman ledger tools and requires the CLI audit before clean completion", () => {
    const { workflow, prompt, lock, manifest, steps } = readWorkflow("daily-caveman-optimizer");
    expect(workflow.tools["cli-proxy"]).toBe(true);
    expect(workflow.tools.ledger["caveman-run-history"]).toMatchObject({ type: "table", key: "run_id" });
    const config = JSON.parse(steps.find(step => step.env?.GH_AW_SAFE_OUTPUTS_CONFIG).env.GH_AW_SAFE_OUTPUTS_CONFIG);
    expect(config.ledger_append.ledgers[0]).toMatchObject({ name: "caveman-run-history", type: "table", key: "run_id" });
    expect(manifest.mcp_servers.find(server => server.name === "mcpscripts").tools).toContain("caveman-run-state");
    expect(lock).toMatch(/Tools: [^\n]*ledger_append/);
    expect(prompt).toContain("safeoutputs ledger_append '<json>'");
    expect(prompt).toContain("safeoutputs report_incomplete");
    expect(prompt).toContain("do not emit a clean `noop`, request a PR, or claim the audit record was persisted");
    expect(workflow["mcp-scripts"]["caveman-run-state"].env).toEqual({ CAVEMAN_RUN_ID: "${{ github.run_id }}" });
  });

  it("queries Caveman audit state with real read-only SQLite without a sqlite3 executable", () => {
    const { workflow } = readWorkflow("daily-caveman-optimizer");
    const script = workflow["mcp-scripts"]["caveman-run-state"].script;
    const directory = mkdtempSync(resolve(tmpdir(), "gh-aw-caveman-state-"));
    const databasePath = resolve(directory, "ledger.db");
    const runID = "39494738069";
    const value = { record_type: "caveman_run", run_id: runID, files_processed: 5, files_optimized: 0, date: "2026-10-20", planned_outcome: "noop" };
    let connection;
    const query = id =>
      runInNewContext(
        `(function () { ${script}\n})()`,
        {
          process: { env: { CAVEMAN_RUN_ID: id } },
          require: name => {
            expect(name).toBe("node:sqlite");
            return {
              DatabaseSync: function (path, options) {
                expect(path).toBe("/tmp/gh-aw/ledgers/caveman-run-history/ledger.db");
                expect(options).toEqual({ readOnly: true });
                connection = new DatabaseSync(databasePath, options);
                return connection;
              },
            };
          },
        },
        { timeout: 1000 }
      );
    try {
      expect(() => query(runID)).toThrow();
      const fixture = new DatabaseSync(databasePath);
      try {
        fixture.exec("CREATE TABLE state (key TEXT PRIMARY KEY, value TEXT)");
        fixture.prepare("INSERT INTO state VALUES (?, ?)").run(runID, JSON.stringify(value));
        fixture.prepare("INSERT INTO state VALUES (?, ?)").run("invalid", "{");
      } finally {
        fixture.close();
      }
      expect(query(runID)).toEqual({ current_run: value });
      expect(connection.isOpen).toBe(false);
      expect(query("missing")).toEqual({ current_run: null });
      expect(connection.isOpen).toBe(false);
      expect(query("' OR 1=1 --")).toEqual({ current_run: null });
      expect(() => query("invalid")).toThrow();
      expect(connection.isOpen).toBe(false);
      const persisted = new DatabaseSync(databasePath, { readOnly: true });
      try {
        expect(persisted.prepare("SELECT COUNT(*) AS count FROM state").get().count).toBe(2);
      } finally {
        persisted.close();
      }
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each([
    ["audit-workflows", "audit-history"],
    ["copilot-centralization-optimizer", "centralization"],
  ])("%s keeps table upsert instructions aligned with the ledger protocol", (name, ledger) => {
    const { workflow, prompt } = readWorkflow(name);
    expect(workflow.tools.ledger[ledger]).toMatchObject({ type: "table", key: "run_id" });
    expect(prompt).toContain("current run ID as a string in `run_id`");
    expect(prompt).toContain("The table primary key is `value.run_id`; do not send a top-level `key` for `upsert`.");
    expect(prompt).not.toMatch(/(?:run ID|`run_id`) as `key`/);
  });

  it("round-trips documented multiline CLI examples without helper commands", () => {
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["shell(safeoutputs:*)"] }, () => ({ kind: "approve-once" }));
    const expected = { item_number: 42, body: '### Result\n\nIt\'s ready.\nQuoted text: "hello".\nLiteral path: C:\\temp\\file.txt' };
    const schema = { item_number: { type: "integer" }, body: { type: "string" } };
    for (const name of ["mcp_cli_tools_with_safeoutputs_prompt", "safe_outputs_cli_only_transport_prompt", "shell_expansion_guard_rejected"]) {
      const prompt = readFileSync(resolve(root, `actions/setup/md/${name}.md`), "utf8");
      expect(prompt).not.toMatch(/\bjq\b|\bnode\b|printf|<<|payload\.json/);
      expect(prompt).toContain("Stdin is also supported");
      const example = prompt.match(/```bash\n(safeoutputs add_comment '[^\n]+')\n```/)[1];
      expect(handler({ kind: "shell", commands: [], fullCommandText: example })).toEqual({ kind: "approve-once" });
      const result = spawnSync("bash", ["-c", 'safeoutputs() { printf "%s\\0" "$@"; }\n' + example], { encoding: "utf8" });
      expect(result.error).toBeUndefined();
      expect(result.status, result.stderr).toBe(0);
      const argv = result.stdout.split("\0").slice(0, -1);
      expect(argv[0]).toBe("add_comment");
      expect(parseToolArgs(argv.slice(1), schema).args).toEqual(expected);
    }
    expect(parseToolArgs(["."], schema, JSON.stringify(expected)).args).toEqual(expected);
    expect(handler({ kind: "shell", commands: [], fullCommandText: "jq -Rs '{body: .}' body.md | safeoutputs add_comment ." }).kind).toBe("reject");
  });

  it("preserves arbitrary inline JSON text without a second escape pass", () => {
    const schema = { body: { type: "string" } };
    for (const body of ["", " \nindented\n ", 'café 日本語 😀\n"quotes" and apostrophe\'s', "Literal \\n and \\t; path C:\\new\\report.md", "Long paragraph.\n".repeat(1000)]) {
      const payload = JSON.stringify({ body }).replaceAll("'", "\\u0027");
      expect(parseToolArgs([payload], schema).args).toEqual({ body });
      expect(parseToolArgs(["."], schema, payload).args).toEqual({ body });
    }
  });

  it.each(names)("%s retains native MCP registration without widening authorization", name => {
    const { workflow, prompt, lock, manifest } = readWorkflow(name);
    expect(workflow.tools["cli-proxy"]).toBe(false);
    expect(lock).not.toContain("export GH_AW_MCP_CLI_SERVERS=");
    expect(manifest.mcp_servers.find(server => server.name === "safeoutputs").tools).toContain("report_incomplete");
    expect(prompt).toContain("native");
    expect(prompt).toContain("report_incomplete");
    expect(workflow.permissions.contents).toBe("read");
    expect(workflow.permissions["pull-requests"]).toBe("read");
    expect(workflow.strict).toBe(true);
    expect(lock).not.toContain("--permission-mode bypassPermissions");
    expect(lock).not.toContain("--allow-all-tools");
  });

  it("allows draft reads and safe outputs but rejects the observed compound commands", () => {
    const { workflow, prompt, sdkConfig } = readWorkflow("draft-pr-cleanup");
    expect(workflow.tools.bash).toBe(false);
    expect(workflow["max-tool-denials"]).toBe(3);
    expect(sdkConfig.capabilities).toMatchObject({ bash: false, cliProxy: false, mcp: true });
    const denied = [];
    const handler = buildCopilotSDKPermissionHandler(sdkConfig.permissions, () => ({ kind: "approve-once" }), { onDenied: command => denied.push(command) });
    for (const [serverName, toolName] of [
      ["github", "search_pull_requests"],
      ["safeoutputs", "add_comment"],
      ["safeoutputs", "report_incomplete"],
    ]) {
      expect(handler({ kind: "mcp", serverName, toolName })).toEqual({ kind: "approve-once" });
    }
    for (const fullCommandText of [
      'github search_pull_requests --query "repo:github/gh-aw is:pr is:open is:draft" --perPage 100 | python3 -c "import json"',
      'github search_pull_requests --query "repo:github/gh-aw is:pr is:open is:draft" > /tmp/gh-aw/agent/o.json; python3 -c "import json"',
      'cd /tmp/gh-aw/agent && github search_pull_requests --query "repo:github/gh-aw is:pr is:open is:draft"',
    ]) {
      expect(handler({ kind: "shell", commands: [], fullCommandText }).kind).toBe("reject");
    }
    expect(denied).toHaveLength(3);
    expect(handler({ kind: "mcp", serverName: "undeclared", toolName: "write" }).kind).toBe("reject");
    expect(prompt).toContain("at most 20 results per page");
    expect(prompt).not.toMatch(/"type": "(?:add_labels|add_comment|close_pull_request)"/);
    expect(prompt).toContain("Label removal is not enabled");
  });

  it("routes Janitor publication through native MCP with dontAsk still enforced", () => {
    const { workflow, prompt, lock } = readWorkflow("instructions-janitor");
    expect(lock).toContain("--permission-mode dontAsk");
    expect(lock).toContain("mcp__safeoutputs");
    expect(workflow["safe-outputs"]["create-pull-request"]["allowed-files"]).toEqual([".github/aw/**"]);
    expect(prompt).toContain("Use native `get_latest_release`");
    expect(prompt).toContain("Call native `create_pull_request` once");
    expect(prompt).toContain("Never repeat a denied write in another syntax");
    expect(workflow.tools.bash).not.toContain("jq *");
  });

  it("exposes concurrency script and raw ledger append with real compiled SDK permissions", () => {
    const { workflow, sdkConfig, manifest, steps, prompt } = readWorkflow("daily-mcp-concurrency-analysis");
    expect(workflow["max-tool-denials"]).toBe(3);
    expect(sdkConfig.capabilities).toMatchObject({ bash: true, edit: true, cliProxy: false, mcp: true });
    expect(manifest.mcp_servers.find(server => server.name === "mcpscripts").tools).toContain("concurrency-audit-state");
    const config = JSON.parse(steps.find(step => step.env?.GH_AW_SAFE_OUTPUTS_CONFIG).env.GH_AW_SAFE_OUTPUTS_CONFIG);
    expect(config.ledger_append.ledgers[0]).toMatchObject({ name: "concurrency-audits", max_record_kb: 4, max_patch_kb: 10 });
    const handler = buildCopilotSDKPermissionHandler(sdkConfig.permissions, () => ({ kind: "approve-once" }));
    for (const [serverName, toolName] of [
      ["mcpscripts", "concurrency-audit-state"],
      ["safeoutputs", "ledger_append"],
      ["safeoutputs", "noop"],
      ["safeoutputs", "report_incomplete"],
    ]) {
      expect(handler({ kind: "mcp", serverName, toolName })).toEqual({ kind: "approve-once" });
    }
    expect(handler({ kind: "write", fileName: "/tmp/gh-aw/cache-memory/concurrency-state.json" })).toEqual({ kind: "approve-once" });
    expect(handler({ kind: "shell", commands: [], fullCommandText: workflow.tools.bash[0] })).toEqual({ kind: "approve-once" });
    expect(handler({ kind: "shell", commands: [], fullCommandText: 'mkdir -p /tmp/gh-aw/cache-memory && echo "{}" > /tmp/gh-aw/cache-memory/concurrency-state.json && safeoutputs noop \'{"message":"done"}\'' }).kind).toBe("reject");
    expect(prompt).toContain("Complete the cache read-back and ledger query/submission before calling `noop`");
  });

  it.each(names)("%s produces native gateway config rather than CLI-only config", name => {
    const { workflow, manifest, steps } = readWorkflow(name);
    const directory = mkdtempSync(resolve(tmpdir(), "gh-aw-tool-policy-"));
    try {
      const gateway = resolve(directory, "gateway.json");
      const toolsOutput = resolve(directory, "tools.json");
      const configPath = resolve(directory, "config.json");
      const githubEnv = resolve(directory, "github_env");
      writeFileSync(configPath, steps.find(step => step.env?.GH_AW_SAFE_OUTPUTS_CONFIG).env.GH_AW_SAFE_OUTPUTS_CONFIG);
      writeFileSync(githubEnv, "");
      const registryResult = spawnSync(process.execPath, [resolve(import.meta.dirname, "generate_safe_outputs_tools.cjs")], {
        env: {
          PATH: process.env.PATH,
          HOME: directory,
          RUNNER_TEMP: directory,
          GH_AW_SAFE_OUTPUTS_TOOLS_SOURCE_PATH: resolve(root, "pkg/workflow/js/safe_outputs_tools.json"),
          GH_AW_SAFE_OUTPUTS_CONFIG_PATH: configPath,
          GH_AW_SAFE_OUTPUTS_TOOLS_META_PATH: resolve(directory, "tools_meta.json"),
          GH_AW_SAFE_OUTPUTS_TOOLS_PATH: toolsOutput,
          GH_AW_TOOLS_META_JSON: steps.find(step => step.env?.GH_AW_TOOLS_META_JSON).env.GH_AW_TOOLS_META_JSON,
        },
        encoding: "utf8",
        timeout: 10000,
      });
      expect(registryResult.error).toBeUndefined();
      expect(registryResult.status, registryResult.stderr).toBe(0);
      const safeOutputTools = JSON.parse(readFileSync(toolsOutput, "utf8"));
      expect(safeOutputTools.map(tool => tool.name)).toContain("report_incomplete");
      if (name === "daily-mcp-concurrency-analysis") {
        const ledgerAppend = safeOutputTools.find(tool => tool.name === "ledger_append");
        expect(ledgerAppend).toBeDefined();
        expect(ledgerAppend.inputSchema.properties).toHaveProperty("record");
        expect(ledgerAppend.inputSchema.properties).toHaveProperty("ledger");
      }
      const mcpServers = Object.fromEntries(manifest.mcp_servers.map(server => [server.name, { url: `http://fixture.invalid:80/mcp/${server.name}`, tools: server.tools }]));
      mcpServers.safeoutputs.tools = safeOutputTools.map(tool => tool.name);
      writeFileSync(gateway, JSON.stringify({ mcpServers }));
      const result = spawnSync(process.execPath, [resolve(import.meta.dirname, `convert_gateway_config_${workflow.engine.id}.cjs`)], {
        env: {
          PATH: process.env.PATH,
          HOME: directory,
          RUNNER_TEMP: directory,
          MCP_GATEWAY_OUTPUT: gateway,
          MCP_GATEWAY_DOMAIN: "fixture.invalid",
          MCP_GATEWAY_PORT: "80",
          GH_AW_MCP_CLI_SERVERS: "[]",
          GITHUB_ENV: githubEnv,
          GITHUB_RUN_ID: "1",
          GITHUB_RUN_ATTEMPT: "1",
          GITHUB_JOB: "test",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      expect(result.error).toBeUndefined();
      expect(result.status, result.stderr).toBe(0);
      expect(result.stderr).toContain("0 filtered (CLI-mounted)");
      const output = resolve(directory, workflow.engine.id === "claude" ? "gh-aw/mcp-config/mcp-servers.json" : ".copilot/mcp-config.json");
      const converted = JSON.parse(readFileSync(output, "utf8"));
      expect(Object.keys(converted.mcpServers).sort()).toEqual(Object.keys(mcpServers).sort());
      expect(converted.mcpServers.safeoutputs.url).toBe("http://fixture.invalid:80/mcp/safeoutputs");
      expect(statSync(output).mode & 0o777).toBe(0o600);
      if (workflow.engine.id === "claude") expect(converted.mcpServers.safeoutputs.type).toBe("http");
      else expect(converted.mcpServers.safeoutputs.tools).toContain("report_incomplete");
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it("does not force native MCP scripts through a shell-only runtime prompt", () => {
    const prompt = readFileSync(resolve(root, "actions/setup/md/mcp_cli_tools_with_safeoutputs_prompt.md"), "utf8");
    expect(prompt).toContain("prefer its native functions when they are explicitly exposed in your tool inventory");
    expect(prompt).not.toContain("For `mcpscripts`, always use the CLI commands above");
    expect(prompt).toContain("A CLI example does not grant permission");
  });
});
