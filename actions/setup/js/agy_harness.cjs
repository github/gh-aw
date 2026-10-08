// @ts-check
"use strict";

const { spawn, spawnSync } = require("node:child_process");
const { createHash } = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { fetchAWFReflect, deriveBaseUrlFromModelsURL } = require("./awf_reflect.cjs");
const [command, ...commandArgs] = process.argv.slice(2);
const log = message => process.stderr.write(`[agy-harness] ${message}\n`);
const checksums = { "1.3.1": "0e313b309ea58c71431ce86bb820936a3700e17e44eabce7bf2264617dc822db" };
const fail = (result, action) => {
  if (result.error || result.signal || result.status !== 0) throw new Error(`${action} failed`);
};

const main = async () => {
  const version = process.env.GH_AW_ENGINE_VERSION;
  if (!version || !Object.hasOwn(checksums, version)) throw new Error("Agy requires the verified 1.3.1 release");
  if (!command) throw new Error("Agy command is required");
  const workspace = process.env.GITHUB_WORKSPACE;
  const promptPath = process.env.GH_AW_PROMPT;
  const model = process.env.GH_AW_AGY_MODEL;
  if (!workspace || !promptPath) throw new Error("GITHUB_WORKSPACE and GH_AW_PROMPT are required");
  if (!model || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$/.test(model)) throw new Error("Agy requires a native model slug, not provider/model syntax");
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-agy-"));
  try {
    let binary = command;
    if (command === "agy") {
      if (process.platform !== "linux" || process.arch !== "x64") throw new Error("Experimental Agy installation supports Linux x64 only");
      const archive = path.join(root, "agy.tar.gz");
      fail(
        spawnSync(
          "curl",
          [
            "--fail",
            "--location",
            "--silent",
            "--show-error",
            "--proto",
            "=https",
            "--tlsv1.2",
            "--max-time",
            "120",
            "--output",
            archive,
            `https://github.com/google-antigravity/antigravity-cli/releases/download/${version}/agy_cli_linux_x64.tar.gz`,
          ],
          { stdio: ["ignore", "ignore", "inherit"], timeout: 125000 }
        ),
        "Agy download"
      );
      if (createHash("sha256").update(fs.readFileSync(archive)).digest("hex") !== checksums[version]) throw new Error("Agy archive checksum did not match");
      fail(spawnSync("tar", ["-xzf", archive, "-C", root, "--", "antigravity"], { stdio: ["ignore", "ignore", "inherit"], timeout: 60000 }), "Agy extraction");
      binary = path.join(root, "antigravity");
    }
    const verification = spawnSync(binary, ["--version"], { encoding: "utf8", timeout: 10000 });
    fail(verification, "Agy version verification");
    if (verification.stdout.trim() !== version) throw new Error("Agy executable does not match engine.version");

    const env = { ...process.env };
    if (env.AWF_REFLECT_ENABLED === "1") {
      const reflection = await fetchAWFReflect({ logger: log });
      if (!reflection.ok || !reflection.reflectData || typeof reflection.reflectData !== "object") throw new Error("Agy could not discover the AWF Gemini endpoint; direct inference fallback is disabled");
      const endpoints = "endpoints" in reflection.reflectData && Array.isArray(reflection.reflectData.endpoints) ? reflection.reflectData.endpoints : [];
      const endpoint = endpoints.find(entry => entry?.configured === true && ["gemini", "google"].includes(entry.provider || ""));
      if (typeof endpoint?.models_url !== "string") throw new Error("Agy requires a configured Gemini models endpoint in AWF");
      // The native client appends the Gemini API version to its base URL.
      env.GOOGLE_GEMINI_BASE_URL = deriveBaseUrlFromModelsURL(endpoint.models_url).replace(/\/v1beta\/?$/, "");
      env.GEMINI_API_KEY = "awf-proxy";
    } else if (!env.GEMINI_API_KEY) {
      throw new Error("GEMINI_API_KEY is required when AWF is explicitly disabled");
    }
    const home = path.join(root, "home");
    const settings = path.join(home, ".gemini", "antigravity-cli");
    fs.mkdirSync(settings, { recursive: true, mode: 0o700 });
    fs.writeFileSync(path.join(settings, "settings.json"), JSON.stringify({ modelProvider: "gemini" }), { mode: 0o600, flag: "wx" });
    env.HOME = home;
    env.XDG_CONFIG_HOME = path.join(home, ".config");
    env.XDG_CACHE_HOME = path.join(home, ".cache");
    delete env.GOOGLE_APPLICATION_CREDENTIALS;
    delete env.AGY_ADC_AUTH;
    delete env.GOOGLE_API_KEY;
    delete env.CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE;
    delete env.GOOGLE_GENAI_USE_VERTEXAI;
    if (!env.GH_AW_MCP_CONFIG) {
      const directory = path.join(workspace, ".agents");
      const config = path.join(directory, "mcp_config.json");
      for (const target of [directory, config]) {
        if (fs.lstatSync(target, { throwIfNoEntry: false })?.isSymbolicLink()) throw new Error("Agy MCP configuration must not use symlinks");
      }
      fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
      fs.writeFileSync(config, JSON.stringify({ mcpServers: {} }), { mode: 0o600 });
      fs.chmodSync(config, 0o600);
    }
    const prompt = fs.readFileSync(promptPath, "utf8");
    const args = [...commandArgs, "--input-format", "stream-json", "--output-format", "stream-json", "--print-timeout", "5m", "--model", model, "--dangerously-skip-permissions"];
    log(`verified experimental Agy ${version}`);
    await new Promise(
      /** @param {(value?: void) => void} resolve */ (resolve, reject) => {
        const child = spawn(binary, args, { cwd: env.GH_AW_ENGINE_CWD || workspace, env, detached: true, stdio: ["pipe", "pipe", "pipe"] });
        let buffer = "",
          stderrTail = "",
          result,
          invalid = false,
          denied = false,
          interrupted = false,
          timedOut = false,
          settled = false,
          escalation,
          drain;
        const pendingTools = new Set();
        const signalGroup = signal => {
          if (!child.pid) return;
          try {
            process.kill(-child.pid, signal);
          } catch (error) {
            const code = error && typeof error === "object" && "code" in error ? String(error.code) : "unknown";
            if (code !== "ESRCH") {
              invalid = true;
              log(`Agy process group termination failed (${code})`);
              child.kill(signal);
            }
          }
        };
        const terminate = signal => {
          if (settled) return;
          signalGroup(signal);
          escalation ??= setTimeout(() => signalGroup("SIGKILL"), 1000);
          drain ??= setTimeout(() => {
            invalid = true;
            child.stdin.destroy();
            child.stdout.destroy();
            child.stderr.destroy();
            complete(child.exitCode, child.signalCode);
          }, 1500);
        };
        const onInterrupt = () => {
          interrupted = true;
          terminate("SIGINT");
        };
        const onTerminate = () => {
          interrupted = true;
          terminate("SIGTERM");
        };
        process.once("SIGINT", onInterrupt);
        process.once("SIGTERM", onTerminate);
        const timer = setTimeout(() => {
          timedOut = true;
          terminate("SIGTERM");
        }, 305000);
        const line = value => {
          if (!value.trim()) return;
          let event;
          try {
            event = JSON.parse(value);
          } catch {
            invalid = true;
            terminate("SIGTERM");
            return;
          }
          if (event.event === "result") result = event.result;
          else if (event.status === "ERROR") result = event;
          const step = event.step_update;
          if (step?.step_type === "tool") {
            if (step.state === "ACTIVE") pendingTools.add(step.step_index);
            if (step.state === "DONE") pendingTools.delete(step.step_index);
            if (/denied|permission|rejected/i.test(step.tool_info?.error?.type || "")) denied = true;
          }
        };
        child.stdout.setEncoding("utf8").on("data", chunk => {
          process.stdout.write(chunk);
          buffer += chunk;
          if (buffer.length > 2 * 1024 * 1024) {
            invalid = true;
            terminate("SIGTERM");
            return;
          }
          let newline;
          while ((newline = buffer.indexOf("\n")) >= 0) {
            line(buffer.slice(0, newline));
            buffer = buffer.slice(newline + 1);
          }
        });
        child.stderr.setEncoding("utf8").on("data", chunk => {
          stderrTail = (stderrTail + chunk).slice(-4096);
          if (/soft[- ]denied|permission[^\n]*denied|requires approval/i.test(stderrTail)) denied = true;
          process.stderr.write(chunk);
        });
        child.stdin.on("error", error => {
          if (!("code" in error) || error.code !== "EPIPE") {
            invalid = true;
            terminate("SIGTERM");
          }
        });
        const cleanup = () => {
          signalGroup("SIGKILL");
          clearTimeout(timer);
          clearTimeout(escalation);
          clearTimeout(drain);
          process.removeListener("SIGINT", onInterrupt);
          process.removeListener("SIGTERM", onTerminate);
        };
        child.once("error", () => {
          if (settled) return;
          settled = true;
          cleanup();
          reject(new Error("Agy executable could not be started"));
        });
        const complete = (code, signal) => {
          if (settled) return;
          settled = true;
          cleanup();
          line(buffer);
          const inferred =
            Number.isSafeInteger(result?.num_turns) &&
            result.num_turns > 0 &&
            Number.isSafeInteger(result?.usage?.input_tokens) &&
            result.usage.input_tokens > 0 &&
            Number.isSafeInteger(result?.usage?.output_tokens) &&
            result.usage.output_tokens > 0;
          if (code !== 0 || signal || invalid || denied || interrupted || timedOut || pendingTools.size || result?.status !== "SUCCESS" || !inferred) {
            if (result?.status === "SUCCESS") {
              process.stdout.write(JSON.stringify({ event: "result", result: { ...result, status: "ERROR", error: "Agy run was denied, interrupted, or lacked completed inference" } }) + "\n");
            }
            reject(new Error("Agy run failed, was denied, interrupted, or lacked completed inference"));
          } else resolve();
        };
        child.once("exit", () => terminate("SIGTERM"));
        child.once("close", complete);
        child.stdin.end(JSON.stringify({ event: "user", message: { content: prompt } }) + "\n");
      }
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
};

main().catch(error => {
  const message = error instanceof Error ? error.message : "Agy harness failed";
  log(message);
  process.stdout.write(JSON.stringify({ type: "session.error", data: { error: message } }) + "\n");
  process.exitCode = 1;
});
