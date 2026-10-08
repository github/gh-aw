// @ts-check
"use strict";

const path = require("node:path");
const fs = require("node:fs");
const { createHash } = require("node:crypto");
const { parseDiagnosticLanguages } = require("./command_diagnostics.cjs");

/** @param {string} value */
function quote(value) {
  return "'" + value.replaceAll("'", "'\\''") + "'";
}

/** @param {unknown} value @returns {unknown} */
function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === "object")
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map(key => [key, canonical(value[key])])
    );
  return value;
}

/**
 * Trust only the generated handler, not project or plugin hooks. Mirrors
 * rust-v0.159.3 hooks/engine/discovery.rs hook_hash and config/fingerprint.rs.
 * @param {Record<string, any>} config
 * @param {unknown} selected
 * @param {string | undefined} home
 */
function withCodexDiagnostics(config, selected, home = process.env.CODEX_HOME) {
  const languages = parseDiagnosticLanguages(selected);
  if (!languages.length) return config;
  if (!home || !path.isAbsolute(home)) throw new Error("Codex diagnostics requires an absolute CODEX_HOME");
  for (const table of [config.features, config.hooks, config.hooks?.state]) {
    if (table !== undefined && (!table || typeof table !== "object" || Array.isArray(table))) throw new Error("Codex diagnostics requires features, hooks, and hooks.state to be tables");
  }
  if (config.features?.hooks === false || config.features?.codex_hooks === false) throw new Error("tools.diagnostics requires Codex hooks; remove features.hooks = false");
  const existing = config.hooks?.PostToolUse ?? [];
  if (!Array.isArray(existing)) throw new Error("Codex hooks.PostToolUse must be an array");
  const command = [process.execPath, path.join(__dirname, "command_diagnostics_hook.cjs"), "codex", JSON.stringify(languages)].map(quote).join(" ");
  const group = { matcher: "^(Bash|mcp__.*__(bash|read_bash|go_repository))$", hooks: [{ type: "command", command, timeout: 10 }] };
  const identity = { event_name: "post_tool_use", matcher: group.matcher, hooks: [{ ...group.hooks[0], async: false }] };
  const hash =
    "sha256:" +
    createHash("sha256")
      .update(JSON.stringify(canonical(identity)))
      .digest("hex");
  let configPath;
  try {
    fs.mkdirSync(home, { recursive: true, mode: 0o700 });
    configPath = path.join(fs.realpathSync(home), "config.toml");
  } catch (error) {
    throw new Error("Failed to prepare Codex diagnostic hook trust in CODEX_HOME", { cause: error });
  }
  const key = `${configPath}:post_tool_use:${existing.length}:0`;
  return {
    ...config,
    features: { ...config.features, hooks: true },
    hooks: {
      ...config.hooks,
      PostToolUse: [...existing, group],
      state: { ...config.hooks?.state, [key]: { enabled: true, trusted_hash: hash } },
    },
  };
}

module.exports = { withCodexDiagnostics };
