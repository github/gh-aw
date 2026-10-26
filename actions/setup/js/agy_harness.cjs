// @ts-check
"use strict";

const { JsonlParser } = require("./jsonl_helpers.cjs");
const { spawn, spawnSync } = require("node:child_process");
const { createHash } = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { fetchAWFReflect, deriveBaseUrlFromModelsURL } = require("./awf_reflect.cjs");
const { resolveEngineModel } = require("./resolve_model_alias.cjs");
const [command, ...commandArgs] = process.argv.slice(2);
const log = message => process.stderr.write(`[agy-harness] ${message}\n`);
const maxPendingRecordLength = 16 * 1024 * 1024; // UTF-16 code units, including the record terminator.
const checksums = {
  "1.3.1": "0e313b309ea58c71431ce86bb820936a3700e17e44eabce7bf2264617dc822db",
  "1.3.3": "8ee3ca32574c431285efbf4d4732f7f8d53e959370bc4a61646e4fa496f7f2b6",
  "1.3.6": "e88ed835cbc4af504f64dfe20175f5572dc2a425dbfa2fcf55c0358bb444d80c",
};
const fail = (result, action) => {
  if (result.error || result.signal || result.status !== 0) throw new Error(`${action} failed`);
};

const verifiedVersion = () => {
  const version = process.env.GH_AW_ENGINE_VERSION;
  if (!version || !Object.hasOwn(checksums, version)) throw new Error("Agy requires a verified 1.3.1, 1.3.3 or 1.3.6 release");
  return version;
};

const stagedBinary = () => {
  if (!process.env.RUNNER_TEMP) throw new Error("RUNNER_TEMP is required for the staged Agy executable");
  return path.join(process.env.RUNNER_TEMP, "gh-aw", "engine-cli", "bin", "agy");
};

const verifyBinary = (binary, version) => {
  const verification = spawnSync(binary, ["--version"], { encoding: "utf8", timeout: 10000 });
  fail(verification, "Agy version verification");
  if (verification.stdout.trim() !== version) throw new Error("Agy executable does not match engine.version");
};

const install = () => {
  const version = verifiedVersion();
  const target = stagedBinary();
  if (process.platform !== "linux" || process.arch !== "x64") throw new Error("Experimental Agy installation supports Linux x64 only");
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-agy-install-"));
  try {
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
    const binary = path.join(root, "antigravity");
    verifyBinary(binary, version);
    fs.mkdirSync(path.dirname(target), { recursive: true, mode: 0o700 });
    fs.copyFileSync(binary, target, fs.constants.COPYFILE_EXCL);
    fs.chmodSync(target, 0o700);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
};

const NATIVE_SLUG_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$/;

/**
 * Run the shared "resolve configured model" step against the Gemini provider's /reflect
 * catalogue, then Agy's native-slug check. An alias resolves to the bare Gemini slug;
 * an exact id is passed through unchanged.
 * @param {{ model: string, reflectData?: any, refetchReflectData?: () => Promise<any>, env?: NodeJS.ProcessEnv, logger?: (message: string) => void }} options
 * @returns {Promise<string>}
 */
const resolveAgyModel = async options => {
  if (!options.model) throw new Error("Agy requires a native model slug, not provider/model syntax");
  const result = await resolveEngineModel({
    engine: "agy",
    role: "main",
    configuredModel: options.model,
    provider: "gemini",
    reflectData: options.reflectData,
    refetchReflectData: options.refetchReflectData,
    normalize: resolution => (resolution.alias ? resolution.modelId : resolution.resolvedModel),
    logger: options.logger || log,
    env: options.env || process.env,
  });
  if (!NATIVE_SLUG_PATTERN.test(result.engineModel)) throw new Error("Agy requires a native model slug, not provider/model syntax");
  return result.engineModel;
};

const main = async () => {
  const version = verifiedVersion();
  if (!command) throw new Error("Agy command is required");
  const workspace = process.env.GITHUB_WORKSPACE;
  const promptPath = process.env.GH_AW_PROMPT;
  const configuredModel = process.env.GH_AW_AGY_MODEL || "";
  if (!workspace || !promptPath) throw new Error("GITHUB_WORKSPACE and GH_AW_PROMPT are required");
  if (!configuredModel) throw new Error("Agy requires a native model slug, not provider/model syntax");
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-agy-"));
  try {
    const binary = command === "agy" ? stagedBinary() : command;
    verifyBinary(binary, version);

    const env = { ...process.env };
    /** @type {any} */
    let reflectData = null;
    if (env.AWF_REFLECT_ENABLED === "1") {
      const reflection = await fetchAWFReflect({ logger: log });
      if (!reflection.ok || !reflection.reflectData || typeof reflection.reflectData !== "object") throw new Error("Agy could not discover the AWF Gemini endpoint; direct inference fallback is disabled");
      reflectData = reflection.reflectData;
      const endpoints = "endpoints" in reflection.reflectData && Array.isArray(reflection.reflectData.endpoints) ? reflection.reflectData.endpoints : [];
      const endpoint = endpoints.find(entry => entry?.configured === true && ["gemini", "google"].includes(entry.provider || ""));
      if (typeof endpoint?.models_url !== "string") throw new Error("Agy requires a configured Gemini models endpoint in AWF");
      // The native client appends the Gemini API version to its base URL.
      env.GOOGLE_GEMINI_BASE_URL = deriveBaseUrlFromModelsURL(endpoint.models_url).replace(/\/v1beta\/?$/, "");
      env.GEMINI_API_KEY = "awf-proxy";
    } else if (!env.GEMINI_API_KEY) {
      throw new Error("GEMINI_API_KEY is required when AWF is explicitly disabled");
    }
    const model = await resolveAgyModel({
      model: configuredModel,
      reflectData,
      refetchReflectData: async () => {
        if (env.AWF_REFLECT_ENABLED !== "1") return null;
        const refreshed = await fetchAWFReflect({ logger: log });
        return refreshed.ok ? refreshed.reflectData || null : null;
      },
    });
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
        const parser = new JsonlParser({ maxPendingRecordLength });
        let outputFailure = "";
        let stderrTail = "",
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
        const consume = entry => {
          if (entry.type === "invalid") {
            if (entry.reason === "blank") return;
            invalid = true;
            terminate("SIGTERM");
            return;
          }
          const event = entry.value;
          if (!event || typeof event !== "object" || Array.isArray(event)) {
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
        const consumeEvents = events => {
          if (outputFailure) return;
          try {
            for (const event of events) consume(event);
          } catch (error) {
            if (!(error instanceof RangeError) || error.message !== "JSONL record exceeds maxPendingRecordLength") throw error;
            outputFailure = `Agy output exceeds the pending JSONL record limit (${maxPendingRecordLength} UTF-16 code units)`;
            invalid = true;
            terminate("SIGTERM");
          }
        };
        child.stdout.setEncoding("utf8").on("data", chunk => {
          if (outputFailure) return;
          process.stdout.write(chunk);
          consumeEvents(parser.push(chunk));
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
          consumeEvents(parser.finish());
          const inferred =
            Number.isSafeInteger(result?.num_turns) &&
            result.num_turns > 0 &&
            Number.isSafeInteger(result?.usage?.input_tokens) &&
            result.usage.input_tokens > 0 &&
            Number.isSafeInteger(result?.usage?.output_tokens) &&
            result.usage.output_tokens > 0;
          if (code !== 0 || signal || invalid || denied || interrupted || timedOut || pendingTools.size || result?.status !== "SUCCESS" || !inferred) {
            if (result?.status === "SUCCESS") {
              process.stdout.write(JSON.stringify({ event: "result", result: { ...result, status: "ERROR", error: outputFailure || "Agy run was denied, interrupted, or lacked completed inference" } }) + "\n");
            }
            reject(new Error(outputFailure || "Agy run failed, was denied, interrupted, or lacked completed inference"));
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

if (require.main === module) {
  (command === "--install" ? Promise.resolve().then(install) : main()).catch(error => {
    const message = error instanceof Error ? error.message : "Agy harness failed";
    log(message);
    process.stdout.write(JSON.stringify({ type: "session.error", data: { error: message } }) + "\n");
    process.exitCode = 1;
  });
}

module.exports = { resolveAgyModel, install };
