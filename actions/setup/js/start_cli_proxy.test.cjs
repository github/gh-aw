// @ts-check
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { redactCliProxyDiagnostics } = require("./redact_cli_proxy_diagnostics.cjs");

describe("proxy network lifecycle", () => {
  let root;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(__dirname, ".gh-aw-proxy-network-test-"));
  });
  afterEach(() => {
    fs.rmSync(root, { recursive: true, force: true });
  });

  function executable(name, content) {
    fs.writeFileSync(path.join(root, name), `#!/bin/bash\n${content}\n`, { mode: 0o700 });
  }

  function runScript(name, env = {}) {
    const source = fs.readFileSync(path.join(__dirname, "../sh", name), "utf8");
    fs.writeFileSync(path.join(root, name), source.replaceAll("/tmp/gh-aw", root));
    for (const helper of ["proxy_env_lib.sh", "proxy_network_lib.sh"]) {
      fs.copyFileSync(path.join(__dirname, "../sh", helper), path.join(root, helper));
    }
    return spawnSync("bash", [path.join(root, name)], {
      encoding: "utf8",
      env: {
        ...process.env,
        PATH: `${root}:${process.env.PATH}`,
        GH_AW_NETWORK_ISOLATION: "false",
        DIFC_PROXY_POLICY: '{"allow-only":{"repos":"all"}}',
        DIFC_PROXY_IMAGE: "synthetic-proxy:latest",
        GITHUB_SERVER_URL: "https://github.com",
        ...env,
      },
    });
  }

  it.each([false, true])("scopes DIFC binding to its network namespace (isolated=%s)", isolated => {
    executable("docker", `printf '%s\\n' "$*"; mkdir -p "${root}/proxy-logs/proxy-tls"; touch "${root}/proxy-logs/proxy-tls/ca.crt"`);
    executable("curl", "exit 0");
    executable("git", "exit 0");
    executable("sudo", "exit 1");
    const result = runScript("start_difc_proxy.sh", { GH_AW_NETWORK_ISOLATION: String(isolated) });
    expect(result.status).toBe(0);
    expect(result.stdout).toContain(isolated ? "--network bridge -p 127.0.0.1:18443:18443" : "--network host");
    expect(result.stdout).toContain(isolated ? "--listen 0.0.0.0:18443" : "--listen 127.0.0.1:18443");
    expect(result.stdout).toContain("DIFC proxy ready");
  });

  it.each([false, true])("removes the legacy filter only after stopping its listener (stop failure=%s)", failure => {
    executable("docker", `case "$1" in container) echo fixture-id ;; inspect) echo host ;; rm) echo docker-stop; exit ${failure ? 1 : 0} ;; esac`);
    executable("sudo", 'shift; exec "$@"');
    executable("iptables", 'if [ "$*" = "-w -S" ]; then echo "-N GH_AW_CLI_PROXY"; else printf "iptables %s\\n" "$*"; fi');
    const result = runScript("stop_cli_proxy.sh");
    expect(result.status).toBe(failure ? 1 : 0);
    expect(result.stdout).toContain("docker-stop");
    if (failure) expect(result.stdout).not.toContain("iptables");
    else {
      expect(result.stdout).toContain("-D INPUT -p tcp --dport 18443 -j GH_AW_CLI_PROXY");
      expect(result.stdout).toContain("-X GH_AW_CLI_PROXY");
      expect(result.stdout.indexOf("docker-stop")).toBeLessThan(result.stdout.indexOf("iptables"));
    }
  });

  it("does not remove firewall rules when Docker cannot establish whether the proxy is stopped", () => {
    executable("docker", "exit 1");
    executable("sudo", "echo unexpected-firewall-access; exit 1");
    const result = runScript("stop_cli_proxy.sh");
    expect(result.status).toBe(1);
    expect(result.stdout).not.toContain("unexpected-firewall-access");
  });

  it("leaves host firewall rules untouched when stopping an isolated proxy", () => {
    executable("docker", 'case "$1" in container) echo fixture-id ;; inspect) echo bridge ;; esac');
    executable("sudo", "echo unexpected-firewall-access; exit 1");
    const result = runScript("stop_cli_proxy.sh");
    expect(result.status).toBe(0);
    expect(result.stdout).not.toContain("unexpected-firewall-access");
  });

  it("retains a restrictive filter when no container remains to identify its network mode", () => {
    executable("docker", "exit 0");
    executable("sudo", "echo unexpected-firewall-access; exit 1");
    const result = runScript("stop_cli_proxy.sh");
    expect(result.status).toBe(0);
    expect(result.stdout).toContain("retaining any legacy ingress filter");
    expect(result.stdout).not.toContain("unexpected-firewall-access");
  });
});

describe("CLI proxy diagnostic redaction", () => {
  let root;
  const custom = 'synthetic"\\credential%with-spaces';
  const builtin = `ghs_${"a".repeat(40)}`;
  const diagnostics = `connection refused: ${custom}\nbuiltin: ${builtin}\nretry with a different port\n`;

  beforeEach(() => {
    root = fs.mkdtempSync(path.join(__dirname, ".gh-aw-cli-proxy-test-"));
  });

  afterEach(() => {
    fs.rmSync(root, { recursive: true, force: true });
  });

  function executable(name, content) {
    fs.writeFileSync(path.join(root, name), `#!/bin/bash\n${content}\n`, { mode: 0o700 });
  }

  function runProxy(options = {}) {
    const source = fs.readFileSync(path.join(__dirname, "../sh/start_cli_proxy.sh"), "utf8");
    const tls = path.join(root, "tls");
    const systemTemp = path.join(root, "system-temp");
    const runnerTemp = path.join(root, "runner");
    const privateParent = path.join(root, "private");
    fs.mkdirSync(systemTemp);
    fs.mkdirSync(runnerTemp);
    const artifactRoot = path.join(options.unsafeSystemTemp ? systemTemp : runnerTemp, "gh-aw");
    fs.mkdirSync(artifactRoot);
    if (options.unsafeCapture) fs.symlinkSync(artifactRoot, privateParent);
    else fs.mkdirSync(privateParent);
    fs.writeFileSync(
      path.join(root, "start_cli_proxy.sh"),
      source
        .replaceAll("/tmp/gh-aw-cli-proxy.XXXXXX", `${privateParent}/gh-aw-cli-proxy.XXXXXX`)
        .replaceAll("/tmp/gh-aw/difc-proxy-tls", tls)
        .replaceAll("/tmp/gh-aw/mcp-logs", path.join(root, "mcp-logs"))
        .replace("for parent in /tmp", `for parent in "${systemTemp}"`)
    );
    fs.copyFileSync(path.join(__dirname, "../sh/proxy_env_lib.sh"), path.join(root, "proxy_env_lib.sh"));
    fs.copyFileSync(path.join(__dirname, "../sh/proxy_network_lib.sh"), path.join(root, "proxy_network_lib.sh"));
    if (!options.missingSanitizer) fs.symlinkSync(path.join(__dirname, "redact_cli_proxy_diagnostics.cjs"), path.join(root, "redact_cli_proxy_diagnostics.cjs"));
    executable("sudo", 'test "$1" = -n && shift; exec "$@"');
    executable(
      "iptables",
      `printf '%s\\n' "iptables $*" >> "$TEST_CALLS"
if [ "$TEST_FILTER_FAILURE" = true ] || [ "$*" = "$TEST_FILTER_FAILURE" ]; then exit 1; fi
case "$*" in
  *" -C "*|*"-S GH_AW_CLI_PROXY") exit 1 ;;
esac`
    );
    executable(
      "docker",
      `printf '%s\\n' "$*" >> "$TEST_CALLS"
case "$1" in
  run)
    if [ "$TEST_OVERSIZED_RUN" = true ]; then
      "$TEST_NODE" -e 'process.stdout.write("x".repeat(2 * 1024 * 1024) + process.env.GH_TOKEN + "\\n")'
    fi
    printf '%s' "$TEST_DIAGNOSTICS"
    printf '%s' "$TEST_DIAGNOSTICS" >&2
    if [ "$TEST_RUN_FAILURE" = true ]; then exit 125; fi
    touch "$TEST_TLS_DIR/ca.crt"
    ;;
  logs)
    if [ "$TEST_NUL" = true ]; then
      printf 'synthetic"\\\\\\000credential%%with-spaces\\n'
    fi
    if [ "$TEST_OVERSIZED" = true ]; then
      "$TEST_NODE" -e 'process.stdout.write("::add-mask::runtime-overflow-secret\\n" + "x".repeat(2 * 1024 * 1024) + process.env.GH_TOKEN + "\\nfinal diagnostic: runtime-overflow-secret " + process.env.GH_TOKEN + "\\n")'
      exit 1
    fi
    printf '%s' "$TEST_DIAGNOSTICS"
    printf '%s' "$TEST_DIAGNOSTICS" >&2
    exit 1
    ;;
  rm) printf 'removed container %s\\n' "$GH_TOKEN" ;;
esac`
    );
    executable(
      "curl",
      `printf '%s\\n' "curl $*" >> "$TEST_CALLS"
case "$*" in
    */rate_limit*)
      attempts=$(cat "$TEST_UPSTREAM_ATTEMPTS" 2>/dev/null || echo 0)
      attempts=$((attempts + 1))
      printf '%s' "$attempts" > "$TEST_UPSTREAM_ATTEMPTS"
      [ "$attempts" -gt "$TEST_UPSTREAM_FAIL_COUNT" ] ;;
    *) [ "$TEST_READY" = true ] ;;
esac`
    );
    executable("sleep", ":");
    executable(
      "rm",
      `for directory in "$@"; do
  if [ -f "$directory/startup.log" ]; then wc -c < "$directory/startup.log" > "$TEST_CAPTURE_SIZE"; fi
done
if [ "$TEST_CLEANUP_FAILURE" = true ]; then
  printf '%s' "$GH_TOKEN"
  printf '%s' "$GH_TOKEN" >&2
  exit 1
fi
exec /bin/rm "$@"`
    );
    if (options.captureFailure) executable("mktemp", 'printf "%s" "$TEST_DIAGNOSTICS"; printf "%s" "$TEST_DIAGNOSTICS" >&2; exit 1');
    if (options.sanitizerFailure) {
      executable("node", 'printf "%s" "$TEST_DIAGNOSTICS"; printf "%s" "$TEST_DIAGNOSTICS" >&2; exit 1');
    } else if (options.redactorThrows) {
      const preload = path.join(root, "throw.cjs");
      fs.writeFileSync(preload, `require(${JSON.stringify(path.join(__dirname, "redact_secrets.cjs"))}).redactArtifactContent = () => { throw new Error(process.env.GH_TOKEN); };`);
      executable("node", `exec ${JSON.stringify(process.execPath)} --require ${JSON.stringify(preload)} "$@"`);
    }
    const result = spawnSync("bash", [path.join(root, "start_cli_proxy.sh")], {
      encoding: "utf8",
      env: {
        ...process.env,
        ...Object.fromEntries(["HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"].map(name => [name, undefined])),
        ...options.proxyEnv,
        PATH: `${root}:${path.dirname(process.execPath)}:${process.env.PATH}`,
        TMPDIR: artifactRoot,
        RUNNER_TEMP: runnerTemp,
        GH_TOKEN: custom,
        GH_AW_NETWORK_ISOLATION: options.isolated ? "true" : "false",
        CLI_PROXY_IMAGE: options.noImage ? "" : "synthetic-proxy:latest",
        GITHUB_SERVER_URL: "https://github.com",
        TEST_DIAGNOSTICS: options.diagnostics || diagnostics,
        TEST_CALLS: path.join(root, "calls"),
        TEST_TLS_DIR: tls,
        TEST_READY: options.ready ? "true" : "false",
        TEST_UPSTREAM_FAIL_COUNT: options.upstreamFailure ? "3" : String(options.upstreamFailCount || 0),
        TEST_UPSTREAM_ATTEMPTS: path.join(root, "upstream-attempts"),
        TEST_RUN_FAILURE: options.runFailure ? "true" : "false",
        TEST_OVERSIZED: options.oversized ? "true" : "false",
        TEST_OVERSIZED_RUN: options.oversizedRun ? "true" : "false",
        TEST_NUL: options.nul ? "true" : "false",
        TEST_NODE: process.execPath,
        TEST_CLEANUP_FAILURE: options.cleanupFailure ? "true" : "false",
        TEST_CAPTURE_SIZE: path.join(root, "capture-size"),
        TEST_FILTER_FAILURE: String(options.filterFailure || "false"),
      },
    });
    const output = result.stdout + result.stderr;
    expect(output).not.toContain(custom);
    expect(output).not.toContain(builtin);
    if (!options.cleanupFailure) expect(fs.readdirSync(privateParent)).toEqual([]);
    return {
      ...result,
      output,
      calls: fs.existsSync(path.join(root, "calls")) ? fs.readFileSync(path.join(root, "calls"), "utf8") : "",
      captureSize: fs.existsSync(path.join(root, "capture-size")) ? Number(fs.readFileSync(path.join(root, "capture-size"), "utf8")) : 0,
    };
  }

  it("redacts arbitrary supplied secrets, short values, multiline fragments and built-in credentials", () => {
    const env = {
      GH_TOKEN: custom,
      GITHUB_TOKEN: "short",
      GH_AW_SECRET_NAMES: " CUSTOM, MULTILINE ",
      SECRET_CUSTOM: "x$",
      MULTILINE: " first-line \nsecond-line\rthird-line",
    };
    const output = redactCliProxyDiagnostics(`${diagnostics}short x$ first-line second-line third-line`, env);
    for (const value of [custom, builtin, "short", "x$", "first-line", "second-line", "third-line"]) expect(output).not.toContain(value);
    expect(output).toContain("retry with a different port");
  });

  it("redacts escaped secret values and masks registered in diagnostics", () => {
    const output = redactCliProxyDiagnostics(`::add-mask::runtime-value\nruntime-value ${JSON.stringify(custom)}`, { GH_TOKEN: custom });
    expect(output).not.toContain("runtime-value");
    expect(output).not.toContain("::add-mask::");
    expect(output).not.toContain(JSON.stringify(custom).slice(1, -1));
  });

  it("redacts the complete log before limiting publication to twenty lines", () => {
    const result = runProxy({ diagnostics: `::add-mask::runtime-synthetic\n${"safe diagnostic\n".repeat(30)}runtime-synthetic\n${custom}\n${builtin}\n` });
    expect(result.status).toBe(1);
    expect(result.output).not.toContain("runtime-synthetic");
    expect(result.stdout.trimEnd().split("\n")).toHaveLength(20);
    expect(result.output).toContain("CLI proxy failed to start within 30s");
    expect(result.output).toContain("CLI proxy startup failed (exit status 1)");
  });

  it("preserves successful startup and only publishes sanitized output", () => {
    const result = runProxy({ ready: true });
    expect(result.status).toBe(0);
    expect(result.output).toContain("CLI proxy ready on port 18443");
    expect(result.output).toContain("connection refused: ***");
    expect(result.calls).not.toContain("logs awmg-cli-proxy");
  });

  it("restricts legacy ingress before starting the host-network listener", () => {
    const result = runProxy({ ready: true });
    expect(result.status).toBe(0);
    expect(result.calls).toContain("--network host");
    const calls = result.calls.split("\n");
    const deny = calls.indexOf("iptables -w -A GH_AW_CLI_PROXY -j DROP");
    const hook = calls.indexOf("iptables -w -I INPUT 1 -p tcp --dport 18443 -j GH_AW_CLI_PROXY");
    const start = calls.findIndex(line => line.startsWith("run "));
    expect(deny).toBeGreaterThan(-1);
    expect(hook).toBeGreaterThan(deny);
    expect(start).toBeGreaterThan(hook);
    for (const iface of ["lo", "docker0", "br-+"]) {
      expect(result.calls).toContain(`-I GH_AW_CLI_PROXY 1 -i ${iface} -j RETURN`);
    }
  });

  it.each([true, "-w -A GH_AW_CLI_PROXY -j DROP", "-w -I GH_AW_CLI_PROXY 1 -i docker0 -j RETURN", "-w -I INPUT 1 -p tcp --dport 18443 -j GH_AW_CLI_PROXY"])(
    "refuses to start the legacy listener when ingress enforcement fails (%s)",
    filterFailure => {
      const result = runProxy({ ready: true, filterFailure });
      expect(result.status).toBe(1);
      expect(result.output).toContain("Cannot restrict legacy CLI proxy ingress");
      expect(result.calls).not.toContain("run -d");
    }
  );

  it("keeps the isolated bridge topology and does not require host firewall changes", () => {
    const result = runProxy({ ready: true, isolated: true, filterFailure: true });
    expect(result.status).toBe(0);
    expect(result.calls).toContain("--network bridge -p 127.0.0.1:18443:18443");
    expect(result.calls).toContain("--listen 0.0.0.0:18443");
    expect(result.calls).not.toContain("iptables");
  });

  it.each(["HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"])("forwards %s by name without publishing its value", name => {
    const proxy = new URL("http://proxy.example:8080");
    proxy.username = "synthetic-user";
    proxy.password = "synthetic-password";
    const value = proxy.href;
    const result = runProxy({ ready: true, proxyEnv: { [name]: value }, diagnostics: `proxy: ${value}\n` });
    expect(result.status).toBe(0);
    expect(result.calls).toContain(`-e ${name} `);
    expect(result.calls).not.toContain(value);
    expect(result.output).not.toContain(value);
  });

  it("leaves unset proxy variables to Docker client defaults", () => {
    const result = runProxy({ ready: true });
    expect(result.status).toBe(0);
    for (const name of ["HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"]) expect(result.calls).not.toContain(`-e ${name} `);
  });

  it("preserves an explicitly empty proxy setting", () => {
    const result = runProxy({ ready: true, proxyEnv: { HTTPS_PROXY: "" } });
    expect(result.status).toBe(0);
    expect(result.calls).toContain("-e HTTPS_PROXY ");
  });

  it("checks upstream access through the CLI proxy with bounded, proxy-free local probes", () => {
    const result = runProxy({ ready: true });
    expect(result.status).toBe(0);
    const probes = result.calls.split("\n").filter(line => line.startsWith("curl "));
    expect(probes).toHaveLength(2);
    for (const probe of probes) {
      expect(probe).toContain("--noproxy *");
      expect(probe).toContain("--connect-timeout");
      expect(probe).toContain("--max-time");
      expect(probe).toContain("--cacert");
    }
    expect(probes[1]).toContain("https://localhost:18443/api/v3/rate_limit");
  });

  it("fails startup and removes the container when local health succeeds but upstream access fails", () => {
    const result = runProxy({ ready: true, upstreamFailure: true });
    expect(result.status).toBe(1);
    expect(result.output).toContain("CLI proxy could not reach the upstream GitHub API");
    expect(result.output).not.toContain("CLI proxy ready");
    expect(result.calls).toContain("logs awmg-cli-proxy");
    expect(result.calls.match(/rm -f awmg-cli-proxy/g)).toHaveLength(2);
    expect(result.calls.split("\n").filter(line => line.includes("/rate_limit"))).toHaveLength(3);
  });

  it("retries a transient upstream failure and succeeds on the next probe", () => {
    const result = runProxy({ ready: true, upstreamFailCount: 1 });
    expect(result.status).toBe(0);
    expect(result.output).toContain("CLI proxy ready on port 18443");
    expect(result.calls.split("\n").filter(line => line.includes("/rate_limit"))).toHaveLength(2);
    expect(result.calls.split("\n").filter(line => line === "rm -f awmg-cli-proxy")).toHaveLength(1);
  });

  it.each(["HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"])("keeps %s private when upstream connectivity fails", name => {
    const value = name.toLowerCase() === "no_proxy" ? "private-backend.internal,private-api.internal" : "http://synthetic-user:synthetic-password@proxy.example:8080";
    const registered = "synthetic-runtime-proxy-credential";
    const result = runProxy({
      ready: true,
      upstreamFailure: true,
      proxyEnv: { [name]: value },
      diagnostics: `${registered}\nproxy: ${value}\nescaped proxy: ${JSON.stringify(value)}\n::add-mask::${registered}\n`,
    });
    expect(result.status).toBe(1);
    expect(result.output).toContain("CLI proxy could not reach the upstream GitHub API");
    for (const sensitive of [value, registered, "synthetic-password", "private-backend.internal"]) {
      expect(result.output).not.toContain(sensitive);
    }
    expect(result.output).not.toContain("::add-mask::");
    expect(result.calls).not.toContain(value);
    expect(result.calls).toContain("logs awmg-cli-proxy");
    expect(result.calls.match(/rm -f awmg-cli-proxy/g)).toHaveLength(2);
  });

  it("ignores TMPDIR inside the artifact tree", () => {
    const result = runProxy({ ready: true });
    expect(result.status).toBe(0);
    expect(fs.readdirSync(path.join(root, "runner", "gh-aw"))).toEqual([]);
  });

  it.each([false, true])("rejects resolved capture directories inside artifact roots (system temp=%s)", unsafeSystemTemp => {
    const result = runProxy({ unsafeCapture: true, unsafeSystemTemp });
    expect(result.status).toBe(1);
    expect(result.output).toBe("::error::CLI proxy diagnostics could not be captured safely\n");
    expect(result.calls).toBe("");
  });

  it.each([false, true])("withholds oversized logs while preserving the failure reason (upstream=%s)", upstreamFailure => {
    const result = runProxy({ oversized: true, ready: upstreamFailure, upstreamFailure });
    expect(result.status).toBe(1);
    expect(result.stderr).toContain(upstreamFailure ? "CLI proxy could not reach the upstream GitHub API" : "CLI proxy failed to start within 30s");
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe(
      (upstreamFailure ? "::error::CLI proxy could not reach the upstream GitHub API. Check runner connectivity and HTTP(S)_PROXY/NO_PROXY settings.\n" : "::error::CLI proxy failed to start within 30s\n") +
        "::error::CLI proxy diagnostics withheld because capture exceeded the byte limit\n"
    );
    expect(result.captureSize).toBe(0);
    expect(result.calls.match(/rm -f awmg-cli-proxy/g)).toHaveLength(2);
  });

  it("bounds diagnostics emitted before readiness checks too", () => {
    const result = runProxy({ ready: true, oversizedRun: true });
    expect(result.status).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("::error::CLI proxy diagnostics withheld because capture exceeded the byte limit\n");
    expect(result.captureSize).toBe(0);
    expect(result.calls).not.toContain("logs awmg-cli-proxy");
  });

  it("redacts a supplied secret split by an actual Docker log NUL", () => {
    const result = runProxy({ nul: true });
    expect(result.status).toBe(1);
    expect(result.output).not.toContain("\0");
    expect(result.output).not.toContain("credential%with-spaces");
  });

  it("preserves the last twenty physical lines when quoted and escaped secrets appear in long logs", () => {
    const lines = Array.from({ length: 32 }, (_, index) => `line-${index} token="${custom}" escaped=${JSON.stringify(custom)}\\n`);
    const result = runProxy({ diagnostics: `${lines.join("\n")}\n` });
    expect(result.status).toBe(1);
    expect(result.stdout.trimEnd().split("\n")).toHaveLength(20);
    expect(result.output).toContain("line-31");
    expect(result.output).not.toContain("line-0 ");
    expect(result.output).not.toContain(JSON.stringify(custom).slice(1, -1));
    expect(result.output).toContain("CLI proxy failed to start within 30s");
  });

  it("reports cleanup failure without publishing its secret-bearing payload", () => {
    const result = runProxy({ ready: true, cleanupFailure: true });
    expect(result.status).toBe(1);
    expect(result.stderr).toBe("::warning::CLI proxy diagnostic cleanup failed\n");
  });

  it("sanitizes readiness failure logs, reports failure and removes the container", () => {
    const result = runProxy();
    expect(result.status).toBe(1);
    expect(result.output).toContain("CLI proxy failed to start within 30s");
    expect(result.output).toContain("retry with a different port");
    expect(result.calls).toContain("logs awmg-cli-proxy");
    expect(result.calls.match(/rm -f awmg-cli-proxy/g)).toHaveLength(2);
  });

  it("sanitizes docker run errors and preserves the original failure status", () => {
    const result = runProxy({ runFailure: true });
    expect(result.status).toBe(125);
    expect(result.output).toContain("connection refused: ***");
    expect(result.calls).not.toContain("logs awmg-cli-proxy");
  });

  it.each([false, true])("withholds partial sanitizer output on failure, even when ready=%s", ready => {
    const result = runProxy({ ready, sanitizerFailure: true });
    expect(result.status).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe((ready ? "" : "::error::CLI proxy failed to start within 30s\n") + "::error::CLI proxy diagnostics withheld because secret redaction failed\n");
  });

  it("withholds diagnostics when the shared redactor throws a secret-bearing exception", () => {
    const result = runProxy({ redactorThrows: true });
    expect(result.status).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("::error::CLI proxy failed to start within 30s\n::error::CLI proxy diagnostics withheld because secret redaction failed\n");
  });

  it("withholds diagnostics when the sanitizer cannot be loaded", () => {
    const result = runProxy({ missingSanitizer: true });
    expect(result.status).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("::error::CLI proxy failed to start within 30s\n::error::CLI proxy diagnostics withheld because secret redaction failed\n");
  });

  it("fails closed before starting Docker if private capture cannot be created", () => {
    const result = runProxy({ captureFailure: true });
    expect(result.status).toBe(1);
    expect(result.output).toBe("::error::CLI proxy diagnostics could not be captured safely\n");
    expect(result.calls).toBe("");
  });

  it("preserves the no-image skip", () => {
    const result = runProxy({ noImage: true });
    expect(result.status).toBe(0);
    expect(result.output).toContain("container image not specified, skipping proxy start");
    expect(result.calls).toBe("");
  });
});
