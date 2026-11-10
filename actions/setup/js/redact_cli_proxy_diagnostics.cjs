// @ts-check
const { PROXY_ENV_NAMES } = require("./constants.cjs");
const { redactArtifactContent } = require("./redact_secrets.cjs");
const { collectAddMaskedValues } = require("./add_mask_redaction.cjs");

const MAX_DIAGNOSTIC_BYTES = 1024 * 1024;
const MAX_DIAGNOSTIC_MASKS = 1024;

/**
 * @param {string} content
 * @param {NodeJS.ProcessEnv} env
 * @returns {string}
 */
function redactCliProxyDiagnostics(content, env, credentialPatterns = []) {
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
  const values = collectAddMaskedValues(content);
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
 * Drain startup output, discarding the entire capture on overflow.
 * @param {NodeJS.ReadableStream} input
 * @returns {Promise<string | null>}
 */
async function captureCliProxyDiagnostics(input) {
  const chunks = [];
  let size = 0;
  let overflow = false;
  for await (const chunk of input) {
    if (overflow) continue;
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    size += bytes.length;
    if (size > MAX_DIAGNOSTIC_BYTES) {
      overflow = true;
      chunks.length = 0;
    } else {
      chunks.push(bytes);
    }
  }
  // Even an unterminated partial line is withheld when the capture overflows.
  return overflow ? null : Buffer.concat(chunks, size).toString("utf8");
}

async function main() {
  try {
    const content = await captureCliProxyDiagnostics(process.stdin);
    if (content === null) {
      process.exitCode = 2;
      return;
    }
    const masks = collectAddMaskedValues(content.replaceAll("\0", ""));
    if (masks.length > MAX_DIAGNOSTIC_MASKS || masks.reduce((size, value) => size + Buffer.byteLength(value), 0) > MAX_DIAGNOSTIC_BYTES) {
      throw new Error("CLI proxy mask limit exceeded");
    }
    const redacted = redactCliProxyDiagnostics(content, process.env);
    const output = redacted.trimEnd().split("\n").slice(-20).join("\n");
    if (Buffer.byteLength(output) > MAX_DIAGNOSTIC_BYTES) throw new Error("CLI proxy diagnostics exceed the output limit");
    process.stdout.write(output);
  } catch {
    // The caller withholds both streams and emits a fixed, payload-free error.
    process.exitCode = 1;
  }
}

if (require.main === module)
  main().catch(() => {
    process.exitCode = 1;
  });

module.exports = { MAX_DIAGNOSTIC_BYTES, redactCliProxyDiagnostics };
