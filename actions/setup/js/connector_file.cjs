// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { readGuardedFile, writeGuardedFile, normalizeGuardedPath } = require("./symlink_guard.cjs");

const stagedName = /^[0-9a-f]{64}(?:\.[A-Za-z0-9]{1,16})?$/;
const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);

function validBinaryPolicy(policy, inputs, request, phase) {
  if (!policy || typeof policy !== "object" || Array.isArray(policy) || Object.keys(policy).some(key => !["input", "maxBytes", "extensions", "rejectContains"].includes(key))) return false;
  const field = inputs?.[policy.input];
  return (
    phase === "safe-output" &&
    request.method === "POST" &&
    !request.graphql &&
    !Object.keys(request.body || {}).length &&
    !request.patch?.length &&
    !request.additionalPatch?.length &&
    field?.required === true &&
    field.type === "string" &&
    !field.sanitize &&
    field.maxLength <= 1024 &&
    Number.isInteger(policy.maxBytes) &&
    policy.maxBytes >= 1 &&
    policy.maxBytes <= 10485760 &&
    (policy.extensions === undefined || (Array.isArray(policy.extensions) && policy.extensions.length <= 16 && policy.extensions.every(value => typeof value === "string" && /^\.[a-z0-9]{1,16}$/.test(value)))) &&
    (policy.rejectContains === undefined ||
      (Array.isArray(policy.rejectContains) && policy.rejectContains.length <= 16 && policy.rejectContains.every(value => typeof value === "string" && Buffer.byteLength(value) >= 1 && Buffer.byteLength(value) <= 128)))
  );
}

function validateConnectorFile(name, content, policy) {
  if (!Buffer.isBuffer(content) || content.length > policy.maxBytes || !stagedName.test(name) || !name.startsWith(createHash("sha256").update(content).digest("hex")))
    throw new Error("Connector staged file digest or byte bound does not match");
  if (policy.extensions?.length && !policy.extensions.includes(path.extname(name).toLowerCase())) throw new Error("Connector staged file extension is not approved");
  if ((policy.rejectContains || []).some(value => content.includes(Buffer.from(value)))) throw new Error("Connector staged file contains a rejected byte sequence");
}

function readConnectorFile(policy, args, root, read = readGuardedFile) {
  const name = args[policy.input];
  if (!root || typeof name !== "string" || !stagedName.test(name)) throw new Error("Binary input requires a content-addressed file in the trusted host staging root");
  const content = read(path.join(root, name), policy.maxBytes);
  validateConnectorFile(name, content, policy);
  return content;
}

function stageConnectorFiles(entry, policies, { root, workspace, rejectPath, read = readGuardedFile, write = writeGuardedFile }) {
  const result = { ...entry };
  for (const [input, policy] of Object.entries(policies)) {
    if (!own(entry, input) || typeof entry[input] !== "string") throw new Error("Connector file input requires a source file path");
    const staged = stagedName.test(entry[input]) ? path.join(root, entry[input]) : null;
    const source = normalizeGuardedPath(path.isAbsolute(entry[input]) ? entry[input] : staged && fs.existsSync(staged) ? staged : path.resolve(workspace || root, entry[input]));
    const roots = [root, workspace].filter(Boolean).map(normalizeGuardedPath);
    if (!roots.some(root => source.startsWith(root + path.sep)) || rejectPath(source)) throw new Error("Connector source file is outside approved nonsensitive staging/workspace roots");
    const content = read(source, policy.maxBytes);
    const extension = path.extname(source).toLowerCase();
    const name = createHash("sha256").update(content).digest("hex") + extension;
    validateConnectorFile(name, content, policy);
    const destination = path.join(root, name);
    if (source !== normalizeGuardedPath(destination)) {
      try {
        write(destination, content);
      } catch (error) {
        const code = error instanceof Error && "code" in error ? Reflect.get(error, "code") : undefined;
        if (code !== "EEXIST") throw error;
        const existing = read(destination, policy.maxBytes);
        validateConnectorFile(name, existing, policy);
      }
    }
    result[input] = name;
  }
  return result;
}

module.exports = { validBinaryPolicy, validateConnectorFile, readConnectorFile, stageConnectorFiles };
