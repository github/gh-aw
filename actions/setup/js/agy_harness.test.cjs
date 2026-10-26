import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";
import childProcess from "node:child_process";
import crypto from "node:crypto";

const require = createRequire(import.meta.url);
const { resolveAgyModel } = require("./agy_harness.cjs");
const reflectFixtures = require("./fixtures/model_resolution_reflect.cjs");

// The recorded /reflect from run 38945917145 did not configure Gemini, so these tests derive a
// configured Gemini endpoint from it (withGeminiModels); the alias map is the recorded one.
const geminiModels = ["gemini-3.5-pro", "gemini-3.8-flash", "gemini-3.9-flash"];

describe("agy_harness.cjs verified bootstrap", () => {
  let tmpDir, spawnSync, install;
  const checksum = "e88ed835cbc4af504f64dfe20175f5572dc2a425dbfa2fcf55c0358bb444d80c";
  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "agy-bootstrap-"));
    vi.stubEnv("RUNNER_TEMP", tmpDir);
    vi.stubEnv("GH_AW_ENGINE_VERSION", "1.3.6");
    spawnSync = vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args) => {
      if (command === "curl") fs.writeFileSync(args[args.indexOf("--output") + 1], "archive fixture");
      if (command === "tar") fs.writeFileSync(path.join(args[args.indexOf("-C") + 1], "antigravity"), "binary fixture");
      return { status: 0, stdout: "1.3.6\n" };
    });
    delete require.cache[require.resolve("./agy_harness.cjs")];
    // Capture the mocked process/hash functions when loading the CommonJS module.
    install = () => require("./agy_harness.cjs").install();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    delete require.cache[require.resolve("./agy_harness.cjs")];
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });
  const acceptChecksum = () => {
    const hash = { update: vi.fn().mockReturnThis(), digest: vi.fn().mockReturnValue(checksum) };
    vi.spyOn(crypto, "createHash").mockReturnValue(hash);
    return hash;
  };
  const target = () => path.join(tmpDir, "gh-aw/engine-cli/bin/agy");
  const expectTemporaryDownloadRemoved = () => {
    const args = spawnSync.mock.calls.find(([command]) => command === "curl")[1];
    expect(fs.existsSync(path.dirname(args[args.indexOf("--output") + 1]))).toBe(false);
  };

  it("stages only a checksum- and version-verified binary before agent execution", () => {
    const hash = acceptChecksum();
    install();
    expect(crypto.createHash).toHaveBeenCalledWith("sha256");
    expect(hash.update).toHaveBeenCalledWith(Buffer.from("archive fixture"));
    expect(hash.digest).toHaveBeenCalledWith("hex");
    expect(spawnSync.mock.calls.map(([command]) => path.basename(command))).toEqual(["curl", "tar", "antigravity"]);
    expect(spawnSync.mock.calls[0][1]).toEqual(expect.arrayContaining(["--proto", "=https", "--tlsv1.2", "https://github.com/google-antigravity/antigravity-cli/releases/download/1.3.6/agy_cli_linux_x64.tar.gz"]));
    expect(spawnSync.mock.calls[1][1].slice(-2)).toEqual(["--", "antigravity"]);
    expect(spawnSync.mock.calls[2][1]).toEqual(["--version"]);
    expect(fs.readFileSync(target(), "utf8")).toBe("binary fixture");
    expect(fs.statSync(target()).mode & 0o777).toBe(0o700);
    expectTemporaryDownloadRemoved();
  });

  it("rejects a checksum mismatch before extraction or execution", () => {
    expect(install).toThrow("Agy archive checksum did not match");
    expect(spawnSync).toHaveBeenCalledTimes(1);
    expect(fs.existsSync(target())).toBe(false);
    expectTemporaryDownloadRemoved();
  });

  it.each(["download", "extraction", "version verification"])("does not stage after failed %s", action => {
    acceptChecksum();
    const failureIndex = ["download", "extraction", "version verification"].indexOf(action);
    for (let index = 0; index < failureIndex; index++) {
      const implementation = spawnSync.getMockImplementation();
      spawnSync.mockImplementationOnce(implementation);
    }
    spawnSync.mockReturnValueOnce({ status: 1 });
    expect(install).toThrow(`Agy ${action} failed`);
    expect(fs.existsSync(target())).toBe(false);
    expectTemporaryDownloadRemoved();
  });

  it("rejects an executable version mismatch", () => {
    acceptChecksum();
    const implementation = spawnSync.getMockImplementation();
    spawnSync.mockImplementationOnce(implementation).mockImplementationOnce(implementation).mockReturnValueOnce({ status: 0, stdout: "1.3.3\n" });
    expect(install).toThrow("Agy executable does not match engine.version");
    expect(fs.existsSync(target())).toBe(false);
    expectTemporaryDownloadRemoved();
  });

  it("rejects unsupported versions without downloading", () => {
    vi.stubEnv("GH_AW_ENGINE_VERSION", "9.9.9");
    expect(install).toThrow("Agy requires a verified");
    expect(spawnSync).not.toHaveBeenCalled();
  });
});

describe("agy_harness.cjs resolveAgyModel", () => {
  let tmpDir;
  let env;
  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "agy-model-resolution-"));
    env = { GH_AW_TMP_DIR: tmpDir, GH_AW_AWF_CONFIG_PATH: path.join(tmpDir, "missing-awf-config.json") };
  });
  afterEach(() => fs.rmSync(tmpDir, { recursive: true, force: true }));
  const records = () => JSON.parse(fs.readFileSync(path.join(tmpDir, "agent/model-resolution.json"), "utf8"));
  const resolve = (model, reflectData, options = {}) => resolveAgyModel({ model, reflectData, env, logger: () => {}, ...options });
  const withAliases = (reflect, aliases) => ({ ...reflect, model_aliases: { ...reflect.model_aliases, ...aliases } });

  it("passes an exact slug unchanged", async () => {
    expect(await resolve("gemini-3.8-flash", reflectFixtures.withGeminiModels(geminiModels))).toBe("gemini-3.8-flash");
    expect(await resolve("gemini-9-unlisted", reflectFixtures.withGeminiModels(geminiModels))).toBe("gemini-9-unlisted");
    expect(records().at(-1)).toEqual({ engine: "agy", role: "main", configured_model: "gemini-9-unlisted", resolved_model: "gemini-9-unlisted", provider: "gemini", status: "exact", cause: null });
  });

  it("resolves an alias to a native Gemini slug", async () => {
    const reflect = withAliases(reflectFixtures.withGeminiModels(geminiModels), { flash: ["gemini/gemini-*-flash"] });
    const logs = [];
    expect(await resolve("flash", reflect, { logger: message => logs.push(message) })).toBe("gemini-3.9-flash");
    expect(await resolve("gemini/flash", reflect)).toBe("gemini-3.9-flash");
    expect(logs.filter(line => line.startsWith("model resolution: engine=agy"))).toEqual([expect.stringContaining("configured=flash resolved=gemini-3.9-flash")]);
    expect(records()).toEqual([{ engine: "agy", role: "main", configured_model: "gemini/flash", resolved_model: "gemini-3.9-flash", provider: "gemini", status: "resolved", cause: null }]);
  });

  // The recorded alias map predates the built-in gemini-3.8-flash alias, so the test adds the
  // built-in definition; gemini-3.8-flash is then both an alias and a live slug.
  it("a live exact slug wins over an alias with the same name", async () => {
    const aliases = { "gemini-3.8-flash": ["copilot/gemini-3.8*flash*", "google/gemini-3.8*flash*", "gemini/gemini-3.8*flash*"] };
    const live = withAliases(reflectFixtures.withGeminiModels(["gemini-3.8-flash-lite", ...geminiModels]), aliases);
    expect(await resolve("gemini-3.8-flash", live)).toBe("gemini-3.8-flash");
    expect(records().at(-1)).toEqual({ engine: "agy", role: "main", configured_model: "gemini-3.8-flash", resolved_model: "gemini-3.8-flash", provider: "gemini", status: "exact", cause: null });
    const absent = withAliases(reflectFixtures.withGeminiModels(["gemini-3.5-pro", "gemini-3.8-flash-lite"]), aliases);
    expect(await resolve("gemini-3.8-flash", absent)).toBe("gemini-3.8-flash-lite");
  });

  it("rejects provider/model syntax after resolution", async () => {
    await expect(resolve("gemini/gemini-3.8-flash", reflectFixtures.withGeminiModels(geminiModels))).rejects.toThrow("Agy requires a native model slug");
  });

  it("workflow-defined alias overrides built-in", async () => {
    const reflect = reflectFixtures.withGeminiModels(geminiModels);
    expect(await resolve("any", reflect)).toBe("gemini-3.9-flash");
    expect(await resolve("any", withAliases(reflect, { any: ["gemini/gemini-3.8*"] }))).toBe("gemini-3.8-flash");
  });

  // Incomplete discovery is derived from the recorded file (models_fetch_complete: false);
  // the Gemini endpoint is configured but has no model list. No recorded run has this shape.
  it("incomplete discovery passes exact ids through and fails aliases after one refetch", async () => {
    const incomplete = () => ({ ...reflectFixtures.withGeminiModels(null), models_fetch_complete: false });
    expect(await resolve("gemini-3.8-flash", incomplete())).toBe("gemini-3.8-flash");
    const refetchReflectData = vi.fn().mockResolvedValue(incomplete());
    await expect(resolve("any", incomplete(), { refetchReflectData })).rejects.toMatchObject({ name: "ModelAliasResolutionError", reason: "catalog_unavailable" });
    expect(refetchReflectData).toHaveBeenCalledTimes(1);
    expect(records().at(-1)).toEqual({ engine: "agy", role: "main", configured_model: "any", resolved_model: null, provider: "gemini", status: "failed", cause: "catalog_unavailable" });
  });

  it("does not use the Copilot integration sentence for Gemini failures", async () => {
    const reflect = withAliases(reflectFixtures.withGeminiModels(geminiModels), { flash: ["gemini/gemini-*-ultra"] });
    const error = await resolve("flash", reflect).catch(err => err);
    expect(error).toMatchObject({ name: "ModelAliasResolutionError", reason: "model_unavailable" });
    expect(error.message).toContain("The requested model is not supported");
    expect(error.message).not.toContain("agentic-workflows integration");
  });
});
