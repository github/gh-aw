// @ts-check
"use strict";

/**
 * Runner of the `engine: none` program job step. Resolves GH_AW_PROGRAM inside the
 * workspace, imports it (TypeScript runs through Node.js type stripping), and calls its
 * default export with the gh-aw program SDK. A thrown error fails the step.
 */

const fs = require("fs");
const path = require("path");
const { pathToFileURL } = require("url");
const { createProgramSdk } = require("./program_sdk.cjs");

/**
 * Resolve the program to a regular file inside the workspace, following symlinks
 * before the containment check so a link cannot escape the checkout.
 * @param {NodeJS.ProcessEnv} env
 */
function resolveProgramPath(env) {
  const relative = env.GH_AW_PROGRAM;
  if (!env.GITHUB_WORKSPACE || !relative) throw new Error("program_unconfigured: GH_AW_PROGRAM and GITHUB_WORKSPACE are required");
  if (path.isAbsolute(relative)) throw new Error("program_path_invalid: GH_AW_PROGRAM must be relative to the workspace");
  const root = fs.realpathSync(env.GITHUB_WORKSPACE);
  let resolved;
  try {
    resolved = fs.realpathSync(path.resolve(root, relative));
  } catch {
    throw new Error(`program_missing: ${relative} was not found in the checked-out repository`);
  }
  if (!resolved.startsWith(root + path.sep)) throw new Error("program_path_invalid: the program resolves outside the workspace");
  if (!fs.statSync(resolved).isFile()) throw new Error(`program_missing: ${relative} is not a regular file`);
  return resolved;
}

/** The program's default export; CommonJS modules expose module.exports as the default. */
function programEntry(module) {
  const entry = module?.default;
  if (typeof entry === "function") return entry;
  if (typeof entry?.default === "function") return entry.default;
  throw new TypeError("program_entry_invalid: the program must default-export an async function receiving the gh-aw program SDK");
}

/**
 * @param {{core?: any, github?: any, context?: any, exec?: any, io?: any, getOctokit?: any, env?: NodeJS.ProcessEnv, importModule?: (url: string) => Promise<any>}} [options]
 */
async function main(options = {}) {
  const env = options.env || process.env;
  const core = options.core || global.core;
  const programPath = resolveProgramPath(env);
  const importModule = options.importModule || (url => import(url));
  const entry = programEntry(await importModule(pathToFileURL(programPath).href));
  const { sdk, finalize } = await createProgramSdk({
    core,
    github: options.github || global.github,
    context: options.context || global.context,
    exec: options.exec || global.exec,
    io: options.io || global.io,
    getOctokit: options.getOctokit || global.getOctokit,
    env,
  });
  core.info(`Running program ${env.GH_AW_PROGRAM}`);
  try {
    await entry(sdk);
  } finally {
    await finalize();
  }
}

module.exports = { main, programEntry, resolveProgramPath };
