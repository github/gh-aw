// @ts-check
"use strict";

const fs = require("fs");

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isTable(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {unknown} catalog @returns {Record<string, unknown>} */
function directToolCatalog(catalog) {
  if (!isTable(catalog) || !Array.isArray(catalog.models) || catalog.models.length === 0) {
    throw new Error("Invalid bundled Codex model catalog: expected a nonempty models array");
  }
  return {
    ...catalog,
    models: catalog.models.map(model => {
      if (!isTable(model) || typeof model.slug !== "string" || !model.slug) {
        throw new Error("Invalid bundled Codex model catalog: each model must have a slug");
      }
      return { ...model, tool_mode: "direct" };
    }),
  };
}

/** @param {Record<string, unknown>} base @param {Record<string, unknown>} overrides @returns {Record<string, unknown>} */
function mergeConfig(base, overrides) {
  const keys = [...new Set([...Object.keys(base), ...Object.keys(overrides)])].sort();
  return Object.fromEntries(
    keys.map(key => {
      if (!Object.hasOwn(overrides, key)) return [key, base[key]];
      let previous = base[key];
      const next = overrides[key];
      if (key === "shell_environment_policy" && isTable(next)) {
        const canonical = Object.hasOwn(next, "filters");
        const legacy = Object.hasOwn(next, "include_only") || Object.hasOwn(next, "exclude");
        if (canonical && legacy) throw new Error("Codex shell environment policy cannot mix filters with legacy exclude or include_only");
        if (isTable(previous)) {
          previous = Object.fromEntries(Object.entries(previous).filter(([name]) => !(canonical && ["include_only", "exclude"].includes(name)) && !(legacy && name === "filters")));
        }
      }
      return [key, isTable(previous) && isTable(next) ? mergeConfig(previous, next) : next];
    })
  );
}

/** @param {string} key */
function tomlKey(key) {
  return /^[A-Za-z0-9_-]+$/.test(key) ? key : JSON.stringify(key);
}

/** @param {unknown} value @returns {string} */
function tomlValue(value) {
  if (typeof value === "string") return JSON.stringify(value).replace(/\u007f/g, "\\u007f");
  if (typeof value === "boolean") return String(value);
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  if (Array.isArray(value)) return `[${value.map(tomlValue).join(", ")}]`;
  if (isTable(value)) {
    return `{ ${Object.keys(value)
      .sort()
      .map(key => `${tomlKey(key)} = ${tomlValue(value[key])}`)
      .join(", ")} }`;
  }
  throw new Error("Codex configuration contains an unsupported TOML value");
}

/** @param {Record<string, unknown>} config @returns {string} */
function serializeConfig(config) {
  /** @type {string[]} */
  const lines = [];
  /** @param {Record<string, unknown>} table @param {string[]} path */
  function writeTable(table, path) {
    if (path.length > 0) lines.push(`[${path.map(tomlKey).join(".")}]`);
    const keys = Object.keys(table).sort();
    const nested = keys.filter(key => isTable(table[key]) && !["http_headers", "env_http_headers", "env", "set"].includes(key));
    for (const key of keys.filter(key => !nested.includes(key))) {
      lines.push(`${tomlKey(key)} = ${tomlValue(table[key])}`);
    }
    lines.push("");
    for (const key of nested) {
      const value = table[key];
      if (isTable(value)) writeTable(value, [...path, key]);
    }
  }
  writeTable(config, []);
  return lines.join("\n");
}

/** @param {unknown} value @returns {unknown} */
function expandConfigEnv(value) {
  if (typeof value === "string") {
    return value.replace(/\$\{([A-Za-z_][A-Za-z0-9_]*)\}/g, (_match, name) => {
      const replacement = process.env[name];
      if (replacement === undefined) throw new Error(`Codex configuration environment variable ${name} is not set`);
      return replacement;
    });
  }
  if (Array.isArray(value)) return value.map(expandConfigEnv);
  if (isTable(value)) return Object.fromEntries(Object.entries(value).map(([key, entry]) => [key, expandConfigEnv(entry)]));
  return value;
}

/** @param {string} name @param {unknown} fallback */
function runtimeTimeout(name, fallback) {
  const raw = process.env[name];
  if (raw === undefined || raw === "") return fallback;
  if (!/^[0-9]+$/.test(raw) || !Number.isSafeInteger(Number(raw)) || Number(raw) <= 0) {
    throw new Error(`${name} must be a positive integer number of seconds`);
  }
  return Number(raw);
}

/** @returns {{ defaults: Record<string, unknown>, overrides: Record<string, unknown>, disablePlugins: boolean, envExpanded?: boolean }} */
function loadCompiledConfig() {
  const inline = process.env.GH_AW_CODEX_CONFIG_JSON;
  const configPath = process.env.GH_AW_CODEX_CONFIG;
  if (!inline && !configPath) {
    return { defaults: { history: { persistence: "none" }, otel: { metrics_exporter: "none" } }, overrides: {}, disablePlugins: false };
  }
  const payload = JSON.parse(inline || fs.readFileSync(configPath || "", "utf8"));
  if (!isTable(payload) || !isTable(payload.defaults) || !isTable(payload.overrides) || typeof payload.disablePlugins !== "boolean") {
    throw new Error("Invalid compiled Codex configuration: expected defaults, overrides, and disablePlugins");
  }
  if (payload.envExpanded !== undefined && typeof payload.envExpanded !== "boolean") throw new Error("Invalid compiled Codex environment expansion state");
  return { defaults: payload.defaults, overrides: payload.overrides, disablePlugins: payload.disablePlugins, envExpanded: payload.envExpanded === true };
}

/** @param {Record<string, Record<string, unknown>>} servers @param {string} urlPrefix */
function buildConfig(servers, urlPrefix) {
  const compiled = loadCompiledConfig();
  const defaultServers = isTable(compiled.defaults.mcp_servers) ? compiled.defaults.mcp_servers : {};
  const defaults = {
    ...compiled.defaults,
    mcp_servers: Object.fromEntries(
      Object.entries(defaultServers).map(([name, value]) => {
        if (!isTable(value)) throw new Error(`Invalid compiled Codex MCP settings for ${name}`);
        return [
          name,
          {
            ...value,
            startup_timeout_sec: runtimeTimeout("GH_AW_STARTUP_TIMEOUT", value.startup_timeout_sec),
            tool_timeout_sec: runtimeTimeout("GH_AW_TOOL_TIMEOUT", value.tool_timeout_sec),
          },
        ];
      })
    ),
  };
  const merged = mergeConfig(defaults, compiled.overrides);
  const configuredServers = isTable(merged.mcp_servers) ? merged.mcp_servers : {};
  merged.mcp_servers = Object.fromEntries(
    Object.entries(servers).map(([name, value]) => {
      const configured = configuredServers[name];
      const options = isTable(configured) ? configured : {};
      const headers = isTable(value.headers) ? Object.fromEntries(Object.entries(value.headers).filter(([, entry]) => typeof entry === "string")) : {};
      return [
        name,
        {
          ...options,
          url: `${urlPrefix}/mcp/${encodeURIComponent(name)}`,
          http_headers: { ...(isTable(options.http_headers) ? options.http_headers : {}), ...headers, Authorization: headers.Authorization || "" },
        },
      ];
    })
  );
  if (compiled.disablePlugins) {
    merged.features = { ...(isTable(merged.features) ? merged.features : {}), plugins: false };
  }
  const expanded = compiled.envExpanded ? merged : expandConfigEnv(merged);
  if (!isTable(expanded)) throw new Error("Invalid Codex configuration root");
  return expanded;
}

module.exports = { isTable, directToolCatalog, mergeConfig, tomlValue, serializeConfig, buildConfig, loadCompiledConfig, expandConfigEnv };
