// @ts-check
const fs = require("node:fs");
const { PROXY_ENV_NAMES } = require("./constants.cjs");
const { redactArtifactContent } = require("./redact_secrets.cjs");
const { collectAddMaskedValues } = require("./add_mask_redaction.cjs");

const MAX_DIAGNOSTIC_BYTES = 1024 * 1024;

/**
 * @param {string} content
 * @param {NodeJS.ProcessEnv} env
 * @returns {string}
 */
function redactCliProxyDiagnostics(content, env, credentialPatterns = [], maskedValues = []) {
  // Bash command substitution removes NUL bytes; normalize before matching.
  content = content.replaceAll("\0", "");
  const names = new Set(["GH_TOKEN", "GITHUB_TOKEN", ...PROXY_ENV_NAMES, ...Object.keys(env).filter(name => name.startsWith("SECRET_"))]);
  for (const name of (env.GH_AW_SECRET_NAMES || "")
    .split(",")
    .map(name => name.trim())
    .filter(Boolean)) {
    names.add(name);
    names.add(`SECRET_${name}`);
  }
  const values = [...maskedValues, ...collectAddMaskedValues(content)];
  for (const name of names) {
    const value = env[name];
    if (!value) continue;
    // Match short, whitespace-padded and individual multiline secret values too.
    values.push(
      value,
      ...value
        .split(/\r\n|\r|\n/)
        .flatMap(line => [line, line.trim()])
        .filter(Boolean)
    );
  }
  // Artifact redaction may JSON-quote ambiguous text. Redact each physical line
  // separately so that representation cannot hide lines from the publication cap.
  return content
    .split("\n")
    .map(line => {
      const patternMatches = credentialPatterns.flatMap(({ pattern, replacement }) =>
        Array.from(line.matchAll(pattern), match => ({
          start: match.index,
          end: match.index + match[0].length,
          value: match[0],
          replacement: match[0].replace(pattern, replacement),
        }))
      );
      return redactArtifactContent(line, [], values, { patternMatches }).content;
    })
    .join("\n");
}

/**
 * Drain startup output while retaining a bounded tail and masks from all lines.
 * @param {NodeJS.ReadableStream} input
 * @returns {Promise<{content: string, truncated: boolean, masks: string[]}>}
 */
async function captureCliProxyDiagnostics(input) {
  let tail = Buffer.alloc(0);
  let truncated = false;
  let pending = "";
  let maskBytes = 0;
  const masks = new Set();
  input.setEncoding("utf8");
  for await (const chunk of input) {
    const text = chunk.toString().replaceAll("\0", "");
    const bytes = Buffer.from(text);
    truncated ||= tail.length + bytes.length > MAX_DIAGNOSTIC_BYTES;
    tail = Buffer.concat([tail, bytes]).subarray(-MAX_DIAGNOSTIC_BYTES);
    const lines = (pending + text).split("\n");
    pending = lines.pop() || "";
    for (const line of lines) {
      for (const value of collectAddMaskedValues(line)) {
        if (masks.has(value)) continue;
        maskBytes += Buffer.byteLength(value);
        if (maskBytes > MAX_DIAGNOSTIC_BYTES) throw new Error("CLI proxy mask limit exceeded");
        masks.add(value);
      }
    }
    // Keep scanning giant lines for commands without retaining their full prefix.
    const command = pending.indexOf("::add-mask::");
    if (command !== -1) pending = pending.slice(command);
    if (Buffer.byteLength(pending) > MAX_DIAGNOSTIC_BYTES) {
      if (command !== -1) throw new Error("CLI proxy mask limit exceeded");
      pending = pending.slice(-MAX_DIAGNOSTIC_BYTES);
    }
  }
  for (const value of collectAddMaskedValues(pending)) {
    maskBytes += Buffer.byteLength(value);
    if (maskBytes > MAX_DIAGNOSTIC_BYTES) throw new Error("CLI proxy mask limit exceeded");
    masks.add(value);
  }
  // Never publish the first partial line: truncation could split a credential.
  const content = truncated ? tail.subarray(tail.indexOf(10) + 1).toString("utf8") : tail.toString("utf8");
  return { content: truncated && !tail.includes(10) ? "" : content, truncated, masks: [...masks] };
}

/**
 * @param {string} filePath
 * @returns {string}
 */
function readCliProxyDiagnostics(filePath) {
  try {
    const stat = fs.statSync(filePath);
    if (!stat.isFile() || stat.size > MAX_DIAGNOSTIC_BYTES) {
      throw new Error("CLI proxy diagnostics exceed the input limit or are not a regular file");
    }
    return fs.readFileSync(filePath, "utf8");
  } catch (error) {
    throw new Error("Unable to read CLI proxy diagnostics", { cause: error });
  }
}

async function main() {
  try {
    const captured = process.argv[2] === "--capture" ? await captureCliProxyDiagnostics(process.stdin) : null;
    const content = captured ? captured.content : readCliProxyDiagnostics(process.argv[2]);
    const redacted = redactCliProxyDiagnostics(content, process.env, [], captured?.masks);
    let output = redacted.trimEnd().split("\n").slice(-20).join("\n");
    // Escaping during redaction can expand the retained text beyond its byte cap.
    if (captured && Buffer.byteLength(output) > MAX_DIAGNOSTIC_BYTES) {
      const tail = Buffer.from(output).subarray(-MAX_DIAGNOSTIC_BYTES);
      output = tail.includes(10) ? tail.subarray(tail.indexOf(10) + 1).toString("utf8") : "";
      captured.truncated = true;
    }
    process.stdout.write(output);
    if (captured?.truncated) process.exitCode = 2;
  } catch {
    // The caller withholds both streams and emits a fixed, payload-free error.
    process.exitCode = 1;
  }
}

if (require.main === module)
  main().catch(() => {
    process.exitCode = 1;
  });

module.exports = { MAX_DIAGNOSTIC_BYTES, readCliProxyDiagnostics, redactCliProxyDiagnostics };
