import { describe, it, expect } from "vitest";
import { readFileSync, existsSync } from "fs";
import { resolve, dirname } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const SETUP_SH = resolve(__dirname, "../setup.sh");
const SETUP_ACTION_INDEX = resolve(__dirname, "../index.js");
const INSTALL_COPILOT_CLI_SH = resolve(__dirname, "../sh/install_copilot_cli.sh");
const setupShContent = readFileSync(SETUP_SH, "utf8");
const setupActionIndexContent = readFileSync(SETUP_ACTION_INDEX, "utf8");
const installCopilotCliContent = readFileSync(INSTALL_COPILOT_CLI_SH, "utf8");

describe("setup action Windows support", () => {
  it("runs setup.sh with Bash", () => {
    expect(setupActionIndexContent).toContain('spawnSync("bash", [path.join(__dirname, "setup.sh")]');
  });

  it("converts Windows paths for Bash without changing RUNNER_TEMP", () => {
    expect(setupShContent).toContain('RUNNER_TEMP_BASH="$(cygpath -u "${RUNNER_TEMP}")"');
    expect(setupShContent).toContain('DESTINATION="$(cygpath -u "${DESTINATION}")"');
  });

  it("installs the Windows Copilot CLI without sudo", () => {
    expect(installCopilotCliContent).toContain("MINGW*|MSYS*|CYGWIN*)");
    expect(installCopilotCliContent).toContain('TARBALL_NAME="copilot-${PLATFORM}-${ARCH_NAME}.zip"');
    expect(installCopilotCliContent).toContain("unzip or 7z is required for Windows installation");
    expect(installCopilotCliContent).toContain('unzip -qo "${TEMP_DIR}/${TARBALL_NAME}" -d "${INSTALL_DIR}"');
    expect(installCopilotCliContent).toContain('7z x -y "-o${INSTALL_DIR}" "${TEMP_DIR}/${TARBALL_NAME}"');
  });
});

/**
 * Parse a bash array from setup.sh, e.g.:
 *   MCP_SCRIPTS_FILES=(
 *     "file1.cjs"
 *     "file2.cjs"
 *   )
 */
function parseSetupShArray(arrayName) {
  // Match the array declaration and capture everything inside the parentheses
  const pattern = new RegExp(`${arrayName}=\\(([\\s\\S]*?)\\)`, "m");
  const match = setupShContent.match(pattern);
  if (!match) throw new Error(`Could not find ${arrayName} in setup.sh`);
  return [...match[1].matchAll(/"([^"]+)"/g)].map(m => m[1]);
}

/**
 * Return all local ./relative requires from a .cjs file (non-recursive).
 * Only captures static string literals, ignores dynamic requires.
 */
function getDirectLocalRequires(filename) {
  const filePath = resolve(__dirname, filename);
  if (!existsSync(filePath)) return [];
  const content = readFileSync(filePath, "utf8");
  const deps = [];
  for (const match of content.matchAll(/require\(["']\.\/([\w\-./]+\.cjs)["']\)/g)) {
    deps.push(match[1]);
  }
  return deps;
}

/**
 * Collect all transitive local dependencies starting from the given root files.
 * Returns a Set of all filenames reachable via require('./...') chains.
 */
function collectTransitiveDeps(rootFiles) {
  const visited = new Set();
  const queue = [...rootFiles];
  while (queue.length > 0) {
    const file = queue.shift();
    if (visited.has(file)) continue;
    visited.add(file);
    for (const dep of getDirectLocalRequires(file)) {
      if (!visited.has(dep)) queue.push(dep);
    }
  }
  return visited;
}

function filesCallingCoreSetSecret(files) {
  return files.filter(file => /\bcore\.setSecret\s*\(/.test(readFileSync(resolve(__dirname, file), "utf8")));
}

const mcpScriptsFiles = parseSetupShArray("MCP_SCRIPTS_FILES");
const safeOutputsFiles = parseSetupShArray("SAFE_OUTPUTS_FILES");

// safe-outputs-mcp-server.cjs is the entry point copied separately as mcp-server.cjs
// Its deps must also be covered by SAFE_OUTPUTS_FILES
const safeOutputsRoots = [...safeOutputsFiles, "safe-outputs-mcp-server.cjs"];

describe("setup.sh MCP_SCRIPTS_FILES", () => {
  it("contains all transitive local dependencies", () => {
    const listed = new Set(mcpScriptsFiles);
    const allDeps = collectTransitiveDeps(mcpScriptsFiles);
    const missing = [...allDeps].filter(f => !listed.has(f));
    expect(missing).toEqual([]);
  });

  it("all listed files exist in js/", () => {
    const missing = mcpScriptsFiles.filter(f => !existsSync(resolve(__dirname, f)));
    expect(missing).toEqual([]);
  });

  it("does not deploy files that call core.setSecret", () => {
    expect(filesCallingCoreSetSecret(mcpScriptsFiles)).toEqual([]);
  });
});

describe("setup.sh SAFE_OUTPUTS_FILES", () => {
  it("contains all transitive local dependencies (including entry point safe-outputs-mcp-server.cjs)", () => {
    const listed = new Set(safeOutputsFiles);
    // Entry point is also deployed (as mcp-server.cjs), so its deps must be covered too
    const allDeps = collectTransitiveDeps(safeOutputsRoots);
    const missing = [...allDeps]
      .filter(f => !listed.has(f))
      // The entry point itself is copied separately, not in the list
      .filter(f => f !== "safe-outputs-mcp-server.cjs");
    expect(missing).toEqual([]);
  });

  it("all listed files exist in js/", () => {
    const missing = safeOutputsFiles.filter(f => !existsSync(resolve(__dirname, f)));
    expect(missing).toEqual([]);
  });

  it("does not deploy files that call core.setSecret", () => {
    expect(filesCallingCoreSetSecret(safeOutputsRoots)).toEqual([]);
  });

  it("copies safe_outputs_tools.json to both safe_outputs_tools.json and tools.json", () => {
    expect(setupShContent).toContain('cp "${JS_SOURCE_DIR}/safe_outputs_tools.json" "${SAFE_OUTPUTS_DEST}/safe_outputs_tools.json"');
    expect(setupShContent).toContain('cp "${JS_SOURCE_DIR}/safe_outputs_tools.json" "${SAFE_OUTPUTS_DEST}/tools.json"');
  });
});
