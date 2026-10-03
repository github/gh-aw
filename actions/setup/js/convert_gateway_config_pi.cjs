// @ts-check
"use strict";

require("./shim.cjs");

const path = require("node:path");
const { normalizeGatewayEntry, runGatewayConversion } = require("./convert_gateway_config_shared.cjs");

/** @param {Record<string, unknown>} entry @param {string} urlPrefix */
function transformPiEntry(entry, urlPrefix) {
  return normalizeGatewayEntry(entry, urlPrefix, transformed => {
    transformed.type = "http";
    transformed.exposure = "deferred";
    delete transformed.tools;
  });
}

function main() {
  return runGatewayConversion({
    format: "Pi",
    engine: "Pi",
    outputPath: path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json"),
    transformServer: (_name, entry, urlPrefix) => transformPiEntry(entry, urlPrefix),
    serialize: servers => JSON.stringify({ mcpServers: servers }, null, 2),
  });
}

if (require.main === module) main();

module.exports = { main, transformPiEntry };
