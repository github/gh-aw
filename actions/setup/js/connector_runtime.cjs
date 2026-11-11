// @ts-check

// Compiler-owned snapshots grant authority. The digest detects corruption, not
// authorship; callers must never accept a snapshot from tool arguments.
const { createHash, createHmac, timingSafeEqual } = require("node:crypto");
const { matchesConnectorPattern } = require("./connector_regex.cjs");

const VERSION = "github.com/gh-aw/connectors/v1";
const MAX_DOCUMENT = 256 * 1024;
const MAX_RESPONSE = 1024 * 1024;
const REQUEST_TIMEOUT = 20000;
const PROTOCOL_VERSIONS = ["2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"];
const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
const entries = value => Object.entries(value || {});
const names = value => Object.keys(value || {});
const count = value => names(value).length;
const namePattern = /^[a-z][a-z0-9-]{0,62}$/;
const toolPattern = /^[A-Za-z][A-Za-z0-9_.-]{0,127}$/;
const argumentPattern = /^[A-Za-z_][A-Za-z0-9_-]{0,127}$/;
const temporaryPattern = /^#?aw_[A-Za-z0-9_]{3,12}$/;
const receiptFields = ["Run", "Digest", "Connector", "Producer", "Endpoint", "Resource", "Scope", "ScopeValue", "TemporaryID", "Value"];
const operationFields = [
  "credential",
  "inputs",
  "resources",
  "request",
  "maxResponseBytes",
  "responseChecks",
  "responseResources",
  "responseRequired",
  "responseOptionalEmpty",
  "responseTypes",
  "responseInputs",
  "responseResourceInputs",
  "project",
  "creates",
  "createdResources",
  "trustedInputs",
  "preflight",
  "responseContains",
  "responseLimits",
  "responseAnyInputs",
  "lookups",
  "retry",
  "checks",
  "responseWhen",
];
const numberTokens = new WeakMap();
const readFields = [
  "inputs",
  "resources",
  "request",
  "maxResponseBytes",
  "responseRequired",
  "responseOptionalEmpty",
  "responseTypes",
  "responseInputs",
  "responseResourceInputs",
  "responseWhen",
  "responseContains",
  "responseChecks",
  "responseResources",
  "responseLimits",
  "responseAnyInputs",
  "project",
];

class ConnectorError extends Error {}
class ConnectorHTTPError extends ConnectorError {
  constructor(status, retryAfter = "") {
    super(`Connector HTTP request returned a non-success status (HTTP status ${status}); no retry performed`);
    this.status = status;
    this.retryAfter = retryAfter;
  }
}
function fail(message) {
  throw new ConnectorError(message);
}
/** @param {unknown} condition @param {string} [message] @returns {asserts condition} */
function assert(condition, message = "Invalid compiled connector plan") {
  if (!condition) fail(message);
}
function object(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function fields(value, allowed, required = []) {
  assert(object(value) && names(value).every(key => allowed.includes(key)) && required.every(key => own(value, key)));
}
function boundedString(value, maximum, minimum = 0) {
  return typeof value === "string" && Buffer.byteLength(value) >= minimum && Buffer.byteLength(value) <= maximum;
}
function fixedPattern(pattern, value) {
  // Only short, runtime-owned patterns reach this helper.
  return typeof value === "string" && pattern.test(value) && !value.endsWith("\n");
}
function pointerValid(value) {
  return typeof value === "string" && value.startsWith("/") && !/~(?![01])/u.test(value);
}
function jsonBytes(value) {
  return Buffer.byteLength(goJSON(value));
}
function goJSON(value, sorted = false) {
  /** @param {any} current @param {any} [parent] @param {string} [key] @returns {string} */
  const encode = (current, parent = undefined, key = "") => {
    if (typeof current === "number") return (parent && numberTokens.get(parent)?.get(key)) || JSON.stringify(current);
    if (Array.isArray(current)) return "[" + current.map((child, index) => encode(child, current, String(index))).join(",") + "]";
    if (object(current)) {
      const keys = names(current).filter(key => current[key] !== undefined);
      if (sorted) keys.sort();
      return "{" + keys.map(key => encode(key) + ":" + encode(current[key], current, key)).join(",") + "}";
    }
    return JSON.stringify(current).replace(/[<>&\u2028\u2029]/gu, char => "\\u" + char.charCodeAt(0).toString(16).padStart(4, "0"));
  };
  return encode(value);
}
function wellFormed(value) {
  return value.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/gu, "\uFFFD");
}

// A small JSON reader retains exact value slices for the Go snapshot digest,
// rejects duplicate keys, and refuses integers JavaScript cannot represent.
function parseJSON(text, maximum = MAX_DOCUMENT, captureRoot = false) {
  assert(typeof text === "string" && Buffer.byteLength(text) <= maximum, "Connector JSON exceeds its byte bound");
  let index = 0;
  const raw = Object.create(null);
  let numberToken;
  const whitespace = () => {
    while (index < text.length && " \r\n\t".includes(text[index])) index++;
  };
  const string = () => {
    const start = index++;
    while (index < text.length) {
      const char = text[index++];
      if (char === '"') {
        try {
          return wellFormed(JSON.parse(text.slice(start, index)));
        } catch {
          fail("Connector document must be valid JSON");
        }
      }
      if (char === "\\") index++;
    }
    return fail("Connector document must be valid JSON");
  };
  const value = (depth = 0) => {
    assert(depth <= 64, "Connector JSON exceeds its depth bound");
    whitespace();
    const char = text[index];
    if (char === '"') return string();
    if (char === "{") {
      index++;
      const result = Object.create(null);
      const tokens = new Map();
      numberTokens.set(result, tokens);
      whitespace();
      if (text[index] === "}") {
        index++;
        return result;
      }
      while (index < text.length) {
        whitespace();
        assert(text[index] === '"', "Connector document must be valid JSON");
        const key = string();
        assert(!own(result, key), "Connector JSON contains a duplicate field");
        whitespace();
        assert(text[index++] === ":", "Connector document must be valid JSON");
        whitespace();
        const start = index;
        result[key] = value(depth + 1);
        if (typeof result[key] === "number") tokens.set(key, numberToken);
        if (captureRoot && depth === 0) raw[key] = text.slice(start, index);
        whitespace();
        const delimiter = text[index++];
        if (delimiter === "}") return result;
        assert(delimiter === ",", "Connector document must be valid JSON");
      }
      return fail("Connector document must be valid JSON");
    }
    if (char === "[") {
      index++;
      const result = [];
      const tokens = new Map();
      numberTokens.set(result, tokens);
      whitespace();
      if (text[index] === "]") {
        index++;
        return result;
      }
      while (index < text.length) {
        const child = value(depth + 1);
        if (typeof child === "number") tokens.set(String(result.length), numberToken);
        result.push(child);
        whitespace();
        const delimiter = text[index++];
        if (delimiter === "]") return result;
        assert(delimiter === ",", "Connector document must be valid JSON");
      }
      return fail("Connector document must be valid JSON");
    }
    for (const { token, result } of [
      { token: "true", result: true },
      { token: "false", result: false },
      { token: "null", result: null },
    ]) {
      if (text.startsWith(token, index)) {
        index += token.length;
        return result;
      }
    }
    const token = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/u.exec(text.slice(index));
    assert(token, "Connector document must be valid JSON");
    index += token[0].length;
    const number = Number(token[0]);
    assert(Number.isFinite(number) && (!Number.isInteger(number) || Number.isSafeInteger(number)), "Connector JSON contains an unsafe number");
    numberToken = token[0];
    return number;
  };
  const document = value();
  whitespace();
  assert(index === text.length, "Connector document must contain exactly one JSON value");
  return { document, raw };
}
function decodeBase64(value, url = false) {
  assert(typeof value === "string", "Invalid connector base64");
  const pattern = url ? /^[A-Za-z0-9_-]*$/ : /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;
  assert(fixedPattern(pattern, value), "Invalid connector base64");
  const buffer = Buffer.from(value, url ? "base64url" : "base64");
  assert(buffer.toString(url ? "base64url" : "base64") === value, "Invalid connector base64");
  return buffer;
}
function origin(value, full = false) {
  assert(typeof value === "string" && !/[\\\r\n\t${}]/u.test(value), "Connector endpoint must be fixed HTTPS");
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    return fail("Connector endpoint must be fixed HTTPS");
  }
  assert(
    parsed.protocol === "https:" && parsed.hostname && !parsed.username && !parsed.password && !/[?#]/u.test(value) && (full || (parsed.pathname === "/" && (value === parsed.origin || value === parsed.origin + "/"))),
    "Connector endpoint must be fixed HTTPS"
  );
  return parsed.origin;
}
function validateInputs(inputs) {
  assert(inputs === undefined || inputs === null || object(inputs));
  for (const [key, input] of entries(inputs)) {
    fields(input, ["type", "required", "enum", "maxLength", "minLength", "sanitize", "minimum", "maximum", "pattern"], ["type"]);
    assert(fixedPattern(argumentPattern, key) && ["string", "integer", "number", "boolean"].includes(input.type));
    assert(input.required === undefined || typeof input.required === "boolean");
    if (input.type === "string") {
      assert(Number.isInteger(input.maxLength) && input.maxLength >= 1 && input.maxLength <= 65536);
      assert(input.minLength === undefined || (Number.isInteger(input.minLength) && input.minLength >= 0 && input.minLength <= input.maxLength));
      assert(input.sanitize === undefined || ["title", "body", "body-plain", "body-plain-compact"].includes(input.sanitize));
      assert(input.sanitize !== "title" || input.maxLength >= 20);
      assert(input.enum === undefined || (Array.isArray(input.enum) && input.enum.every(item => typeof item === "string")));
      assert(input.pattern === undefined || boundedString(input.pattern, 1024));
      assert(input.minimum === undefined && input.maximum === undefined);
    } else {
      assert(!input.maxLength && !input.minLength && !input.sanitize && !input.enum?.length && !input.pattern);
      assert(["number", "integer"].includes(input.type) || (input.minimum === undefined && input.maximum === undefined));
    }
    for (const key of ["minimum", "maximum"]) assert(input[key] === undefined || (typeof input[key] === "number" && Number.isFinite(input[key])));
    assert(input.minimum === undefined || input.maximum === undefined || input.minimum <= input.maximum);
  }
}
function validateValue(value, inputs, depth = 0) {
  fields(value, ["input", "literal", "object", "array", "encoding", "prefix"]);
  assert(depth <= 16);
  assert([!!value.input, value.literal !== undefined && value.literal !== null, value.object !== undefined, value.array !== undefined].filter(Boolean).length === 1);
  if (value.input) {
    assert(own(inputs || {}, value.input));
    assert(!value.encoding || (inputs[value.input].type === "string" && ["jira-adf", "confluence-storage"].includes(value.encoding)));
    if (value.prefix !== undefined) assert(inputs[value.input].type === "string" && !value.encoding && boundedString(value.prefix, 1024) && !/[\r\n]/u.test(value.prefix) && !value.prefix.includes("${"));
  } else assert(!value.encoding);
  if (!value.input) assert(value.prefix === undefined);
  if (value.object !== undefined) {
    assert(object(value.object));
    for (const child of Object.values(value.object)) validateValue(child, inputs, depth + 1);
  }
  if (value.array !== undefined) {
    assert(Array.isArray(value.array));
    for (const child of value.array) validateValue(child, inputs, depth + 1);
  }
}
function validateRequest(request, inputs, phase) {
  fields(request, ["endpoint", "method", "path", "pathInputs", "query", "body", "graphql", "patch", "headers", "additionalPatch", "binary"], ["endpoint", "method", "path"]);
  if (request.binary !== undefined) assert(require("./connector_file.cjs").validBinaryPolicy(request.binary, inputs, request, phase));
  assert(request.additionalPatch === undefined || (Array.isArray(request.additionalPatch) && request.additionalPatch.length <= 32));
  assert(["GET", "POST", "PUT", "PATCH", "DELETE"].includes(request.method));
  validateRequestHeaders(request);
  assert(request.pathInputs === undefined || object(request.pathInputs));
  assert(request.graphql === undefined || typeof request.graphql === "string");
  assert(
    typeof request.path === "string" && request.path.startsWith("/") && !request.path.startsWith("//") && !/[?#\\%\r\n\t]/u.test(request.path) && !request.path.includes("..") && !request.path.includes("${"),
    "Connector request requires a fixed absolute path"
  );
  let path = request.path;
  for (const [placeholder, input] of entries(request.pathInputs)) {
    assert(fixedPattern(namePattern, placeholder) && path.includes("{" + placeholder + "}"));
    assert(inputs?.[input]?.type === "string" && inputs[input].required);
    path = path.replaceAll("{" + placeholder + "}", "value");
  }
  function validateRequestHeaders(request) {
    const headers = request.headers;
    assert(headers === undefined || object(headers), "Connector request headers must be a literal metadata map");
    assert(count(headers) <= 16, "Connector request headers exceed their count bound");
    const seen = new Set();
    const forbidden = new Set(["authorization", "proxy-authorization", "host", "cookie", "set-cookie", "connection", "content-length", "transfer-encoding", "te", "trailer", "upgrade", "keep-alive", "private-token"]);
    for (const [name, value] of entries(headers)) {
      const lower = name.toLowerCase();
      assert(fixedPattern(/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/, name) && boundedString(value, 1024) && !/[\r\n]/u.test(value) && !value.includes("${"), "Connector request headers require bounded literal HTTP metadata");
      assert(!seen.has(lower), "Connector request headers contain a case-insensitive duplicate");
      seen.add(lower);
      assert(!forbidden.has(lower) && !/-(?:key|token|secret)$/u.test(lower), "Connector request metadata cannot set authority or secret-bearing headers");
      if (lower === "content-type")
        assert(
          value === (request.binary ? "application/octet-stream" : request.patch?.length || request.additionalPatch?.length ? "application/json-patch+json" : "application/json"),
          "Connector request Content-Type must match its canonical payload type"
        );
    }
  }
  assert(!/[{}]/u.test(path), "Connector path contains unresolved placeholders");
  if (phase !== "safe-output") {
    assert(request.method === "GET" || (request.method === "POST" && typeof request.graphql === "string" && request.graphql.trimStart().startsWith("query ")));
    assert(!request.graphql || !/\b(mutation|subscription)\b/u.test(request.graphql));
  }
  if (request.method === "GET") assert(!count(request.body) && !request.graphql && !request.patch?.length && !request.additionalPatch?.length);
  if (request.graphql) assert(typeof request.graphql === "string" && request.method === "POST" && !count(request.body));
  for (const values of [request.query, request.body]) {
    assert(values === undefined || object(values));
    for (const [key, value] of entries(values)) {
      assert(key !== "");
      validateValue(value, inputs);
    }
  }
  if (!request.graphql)
    for (const value of Object.values(request.query || {})) {
      assert(value.object === undefined && value.array === undefined && !value.encoding && (value.literal === undefined || value.literal === null || ["string", "number", "boolean"].includes(typeof value.literal)));
    }
  if (request.patch !== undefined || request.additionalPatch !== undefined) {
    assert(request.patch === undefined || Array.isArray(request.patch));
    const patches = [...(request.patch || []), ...(request.additionalPatch || [])];
    if (patches.length) assert(phase === "safe-output" && ["PATCH", "POST"].includes(request.method) && !request.graphql && !count(request.body));
    const seen = new Set();
    for (const patch of patches) {
      fields(patch, ["op", "path", "value"], ["op", "path", "value"]);
      assert(["add", "replace", "test"].includes(patch.op) && pointerValid(patch.path) && !/[{}$]/u.test(patch.path));
      validateValue(patch.value, inputs);
      assert(!request.additionalPatch?.length || !seen.has(patch.path.toLowerCase()), "Additional patch cannot override an existing path");
      seen.add(patch.path.toLowerCase());
    }
  }
}
function validateOperation(spec, op, phase, readTool = false) {
  fields(op, readTool ? [...operationFields, "tool"] : operationFields, ["credential", "request", "maxResponseBytes"]);
  assert(spec.credentials[op.credential]?.phase === phase, "Connector credential phase mismatch");
  assert(own(spec.endpoints, op.request?.endpoint), "Connector request references an unknown endpoint");
  assert(Number.isInteger(op.maxResponseBytes) && op.maxResponseBytes >= 1 && op.maxResponseBytes <= MAX_RESPONSE);
  validateInputs(op.inputs);
  for (const key of ["resources", "project", "createdResources", "trustedInputs", "responseContains", "responseLimits", "responseAnyInputs"]) assert(op[key] === undefined || object(op[key]));
  validateRequest(op.request, op.inputs, phase);
  if (op.request.binary) assert(!count(op.trustedInputs) && !op.preflight && !op.lookups?.length, "Binary operations require canonical or receipt-bound inputs without secret preflights");
  if (op.responseWhen !== undefined) assert(require("./event_predicate.cjs").validEventPredicate(op.responseWhen));
  if (op.checks !== undefined) {
    assert(phase === "safe-output" && Array.isArray(op.checks) && op.checks.length <= 4);
    for (const check of op.checks) {
      fields(check, readFields, ["inputs", "request", "maxResponseBytes"]);
      assert(check.request.endpoint === op.request.endpoint && !count(check.project), "Checks require the operation endpoint without projection");
      validateRequest(check.request, check.inputs, "context");
      for (const [input, field] of entries(check.inputs)) assert(field.required && equivalent(field, op.inputs?.[input]), "Check inputs must be canonical required operation inputs");
      for (const [input, resource] of entries(check.resources)) assert(op.resources?.[input] === resource, "Checks cannot change resource authority");
      validateOperation(spec, { ...check, credential: op.credential }, phase);
    }
  }
  if (op.retry !== undefined) {
    fields(op.retry, ["maxAttempts", "backoffMs", "retryOn", "idempotent"], ["maxAttempts", "backoffMs", "retryOn"]);
    assert(Number.isInteger(op.retry.maxAttempts) && op.retry.maxAttempts >= 1 && op.retry.maxAttempts <= 4);
    assert(Number.isInteger(op.retry.backoffMs) && op.retry.backoffMs >= 1 && op.retry.backoffMs <= 1000);
    assert(Array.isArray(op.retry.retryOn) && op.retry.retryOn.length >= 1 && op.retry.retryOn.length <= 5);
    assert(new Set(op.retry.retryOn).size === op.retry.retryOn.length && op.retry.retryOn.every(status => [429, 500, 502, 503, 504].includes(status)));
    assert(op.retry.idempotent === undefined || typeof op.retry.idempotent === "boolean");
    assert(phase !== "safe-output" || op.retry.maxAttempts === 1 || (op.retry.idempotent === true && ["PUT", "PATCH", "DELETE"].includes(op.request.method)), "Connector write retries require an explicitly idempotent PUT, PATCH or DELETE");
  }
  for (const [input, resource] of entries(op.resources)) {
    assert(op.inputs?.[input]?.required && op.inputs[input].type === "string" && fixedPattern(namePattern, resource));
  }
  for (const key of ["responseChecks", "responseResources", "responseTypes", "responseInputs", "responseResourceInputs"]) {
    assert(op[key] === undefined || object(op[key]));
    for (const [pointer, value] of entries(op[key])) {
      assert(pointerValid(pointer));
      if (key === "responseResources") assert(fixedPattern(namePattern, value));
      if (key === "responseTypes") assert(["string", "boolean", "number", "integer"].includes(value));
      if (key === "responseInputs") assert(op.inputs?.[value]?.required);
      if (key === "responseResourceInputs") assert(op.inputs?.[value]?.required && op.inputs[value].type === "string" && own(op.resources || {}, value), "Connector response resource inputs require required resource-constrained strings");
    }
  }
  assert(op.responseRequired === undefined || (Array.isArray(op.responseRequired) && op.responseRequired.every(pointerValid)));
  assert(op.responseOptionalEmpty === undefined || (Array.isArray(op.responseOptionalEmpty) && op.responseOptionalEmpty.every(pointerValid)));
  for (const [key, pointer] of entries(op.project)) assert(fixedPattern(namePattern, key) && pointerValid(pointer));
  for (const [pointer, input] of entries(op.responseContains)) {
    assert(pointerValid(pointer) && pointer.split("/*/").length === 2 && op.inputs?.[input]?.required && op.inputs[input].type === "string");
  }
  for (const [pointer, maximum] of entries(op.responseLimits)) assert((pointer === "" || pointerValid(pointer)) && Number.isInteger(maximum) && maximum >= 1 && maximum <= 100);
  for (const [input, pointers] of entries(op.responseAnyInputs)) {
    assert(op.inputs?.[input]?.required && op.inputs[input].type === "string" && Array.isArray(pointers) && pointers.length >= 1 && pointers.length <= 4 && pointers.every(pointerValid));
  }
  validateLookups(spec, op, phase);
  if (op.creates || count(op.createdResources) || count(op.trustedInputs) || op.preflight) assert(phase === "safe-output");
  for (const [input, source] of entries(op.trustedInputs)) {
    fields(source, ["secret", "value"]);
    assert(op.inputs?.[input]?.required && op.inputs[input].type === "string" && own(op.resources || {}, input) && !op.createdResources?.[input]);
    assert(!!source.secret !== !!source.value);
    if (source.secret) secretName(source.secret);
    if (source.value) assert(boundedString(source.value, 1024, 1) && !source.value.includes("${"));
  }
  if (op.preflight) {
    const lookup = op.preflight;
    fields(lookup, readFields, ["inputs", "request", "maxResponseBytes", "project"]);
    assert(count(op.trustedInputs) && lookup.request.endpoint === op.request.endpoint && count(lookup.project));
    assert(names(lookup.inputs).length === names(op.trustedInputs).length && names(lookup.inputs).every(key => own(op.trustedInputs, key)));
    assert(names(lookup.project).every(key => own(op.trustedInputs, key)));
    validateRequest(lookup.request, lookup.inputs, "context");
    validateOperation(spec, { ...lookup, credential: op.credential }, phase);
  }
  function validateLookups(spec, op, phase) {
    if (op.lookups === undefined) return;
    assert(Array.isArray(op.lookups) && op.lookups.length <= 4, "Connector lookups require at most four fixed reads");
    if (!op.lookups.length) return;
    assert(!count(op.trustedInputs) && !count(op.createdResources) && !op.preflight && !op.creates, "Connector lookups cannot mix with trusted-input or creation-receipt bindings");
    const targets = new Map();
    for (const [index, lookup] of op.lookups.entries()) {
      fields(lookup, [...readFields, "input"], ["inputs", "request", "maxResponseBytes"]);
      assert(lookup.input === undefined || typeof lookup.input === "string");
      const input = lookup.input;
      if (!input) {
        assert(!count(lookup.project), "Relationship lookup cannot project arguments");
        continue;
      }
      assert(op.inputs?.[input]?.required && op.inputs[input].type === "string" && !op.inputs[input].sanitize && op.resources?.[input] && !targets.has(input));
      targets.set(input, index);
      const alias = lookup.inputs?.[input];
      const pointer = lookup.project?.[input];
      assert(alias?.required && alias.type === "string" && !alias.sanitize && lookup.resources?.[input] && lookup.resources[input] !== op.resources[input]);
      assert(count(lookup.project) === 1 && pointer && lookup.responseResources?.[pointer] === op.resources[input], "Lookup projection requires an independent canonical grant");
      assert(Object.values(lookup.responseInputs || {}).includes(input) || lookup.responseAnyInputs?.[input]?.length, "Lookup response must match its requested alias");
    }
    assert(targets.size, "Connector lookups require an alias target");
    for (const [index, lookup] of op.lookups.entries()) {
      assert(lookup.request.endpoint === op.request.endpoint && count(lookup.inputs), "Connector lookup requires bounded inputs on the operation endpoint");
      for (const [input, field] of entries(lookup.inputs)) {
        if (input === lookup.input) continue;
        assert(op.inputs?.[input] && field.required && equivalent(field, op.inputs[input]) && (!targets.has(input) || targets.get(input) < index), "Lookup dependencies must be canonical and acyclic");
      }
      validateRequest(lookup.request, lookup.inputs, "context");
      const { input, ...read } = lookup;
      validateOperation(spec, { ...read, credential: op.credential }, phase);
    }
  }
  if (op.creates) {
    const created = op.creates;
    fields(created, ["resource", "value", "scope", "temporaryId", "scopePrefix", "valueType"], ["resource", "value", "scope", "temporaryId"]);
    assert(created.valueType === undefined || ["string", "integer"].includes(created.valueType));
    assert(op.request.method === "POST" && fixedPattern(namePattern, created.resource) && pointerValid(created.value));
    assert(op.inputs?.[created.scope]?.required && op.inputs[created.scope].type === "string" && op.resources?.[created.scope]);
    const temporary = op.inputs?.[created.temporaryId];
    assert(temporary?.type === "string" && temporary.maxLength <= 80 && temporary.pattern === "^#?aw_[A-Za-z0-9_]{3,12}$");
    assert(created.scopePrefix === undefined || (boundedString(created.scopePrefix, 16) && !created.scopePrefix.includes("${")));
  }
  if (readTool) assert(fixedPattern(toolPattern, op.tool));
}
function secretName(value) {
  assert(fixedPattern(/^[A-Z_][A-Z0-9_]*$/, value) && !value.startsWith("GITHUB_"), "Invalid connector secret reference");
}
function validatePlan(plan) {
  const { spec, connection, binding } = plan;
  fields(spec, ["apiVersion", "kind", "id", "endpoints", "credentials", "triggers", "mcp", "httpMcp", "safeOutputs", "context"], ["apiVersion", "kind", "id", "endpoints", "credentials"]);
  fields(connection, ["apiVersion", "kind", "connector", "credentials", "resources"], ["apiVersion", "kind", "connector", "credentials", "resources"]);
  fields(binding, ["apiVersion", "kind", "definition", "connection", "triggers", "mcp", "httpMcp", "safeOutputs", "context"], ["apiVersion", "kind", "definition", "connection"]);
  assert(spec.apiVersion === VERSION && connection.apiVersion === VERSION && binding.apiVersion === VERSION);
  assert(spec.kind === "ConnectorSpec" && connection.kind === "ConnectorConnection" && binding.kind === "ConnectorBinding" && connection.connector === spec.id);
  assert(fixedPattern(/^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+\/[a-z][a-z0-9-]*$/, spec.id));
  assert(object(spec.endpoints) && count(spec.endpoints) && object(spec.credentials) && object(connection.credentials) && object(connection.resources));
  for (const [name, endpoint] of entries(spec.endpoints)) {
    assert(fixedPattern(namePattern, name));
    origin(endpoint);
  }
  for (const [name, credential] of entries(spec.credentials)) {
    fields(credential, ["phase", "header", "scheme", "scopes"], ["phase", "header", "scheme", "scopes"]);
    assert(fixedPattern(namePattern, name) && ["agent", "context", "safe-output"].includes(credential.phase));
    assert(["Authorization", "X-API-Key", "PRIVATE-TOKEN"].includes(credential.header) && ["bearer", "basic", "token", "raw"].includes(credential.scheme));
    assert(Array.isArray(credential.scopes) && credential.scopes.length && credential.scopes.every(scope => boundedString(scope, MAX_DOCUMENT, 1)));
  }
  for (const [name, ref] of entries(connection.credentials)) {
    fields(ref, ["secret"], ["secret"]);
    assert(own(spec.credentials, name));
    secretName(ref.secret);
  }
  for (const [name, values] of entries(connection.resources)) {
    assert(fixedPattern(namePattern, name) && Array.isArray(values) && values.length && values.every(value => typeof value === "string" && value !== "" && value !== "*"));
  }
  for (const [group, phase] of [
    ["mcp", "agent"],
    ["context", "context"],
    ["safeOutputs", "safe-output"],
  ]) {
    assert(spec[group] === undefined || spec[group] === null || object(spec[group]));
    for (const [name, op] of entries(spec[group])) {
      assert(fixedPattern(namePattern, name));
      validateOperation(spec, op, phase, group === "mcp");
    }
    for (const group of ["httpMcp", "triggers"]) assert(spec[group] === undefined || spec[group] === null || object(spec[group]));
    for (const [name, trigger] of entries(spec.triggers)) {
      fields(
        trigger,
        ["verificationProfile", "nativeEvents", "match", "normalize", "resources", "types", "resourceOrigins", "when", "filters", "variants", "urlIdentity", "urlPathPrefix"],
        ["verificationProfile", "nativeEvents", "match", "normalize"]
      );
      const { validEventPredicate, validEventFilters, validEventURLBinding } = require("./event_predicate.cjs");
      assert(validEventURLBinding(trigger.urlIdentity, trigger.urlPathPrefix, trigger.normalize), "Invalid connector event URL binding");
      if (trigger.when !== undefined) assert(validEventPredicate(trigger.when), "Invalid connector event predicate");
      if (trigger.filters !== undefined) assert(validEventFilters(trigger.filters), "Invalid connector event filters");
      if (trigger.variants !== undefined) {
        assert(Array.isArray(trigger.variants) && trigger.variants.length <= 4);
        for (const variant of trigger.variants) {
          fields(variant, ["when", "normalize", "resources", "types"], ["when", "normalize", "resources"]);
          assert(validEventPredicate(variant.when));
          assert(object(variant.normalize) && count(variant.normalize) === count(trigger.normalize) && names(variant.normalize).every(key => own(trigger.normalize, key) && pointerValid(variant.normalize[key])));
          assert(object(variant.resources) && JSON.stringify(Object.values(variant.resources).sort()) === JSON.stringify(Object.values(trigger.resources || {}).sort()));
          for (const pointer of names(variant.resources)) assert(pointerValid(pointer));
          for (const [pointer, type] of entries(variant.types)) assert(pointerValid(pointer) && ["string", "boolean", "number", "integer"].includes(type));
        }
      }
      if (trigger.resourceOrigins !== undefined)
        assert(Array.isArray(trigger.resourceOrigins) && trigger.resourceOrigins.length <= 16 && trigger.resourceOrigins.every(value => typeof value === "string" && !value.includes("*") && origin(value)));
      assert(fixedPattern(namePattern, name) && fixedPattern(/^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+\/[a-z][a-z0-9-]*$/, trigger.verificationProfile));
      assert(Array.isArray(trigger.nativeEvents) && trigger.nativeEvents.length && trigger.nativeEvents.every(value => typeof value === "string"));
      assert(object(trigger.match) && count(trigger.match) && object(trigger.normalize) && count(trigger.normalize));
      for (const [pointer, value] of entries(trigger.match)) assert(pointerValid(pointer) && typeof value === "string");
      for (const [name, pointer] of entries(trigger.normalize)) assert(fixedPattern(namePattern, name) && pointerValid(pointer));
      for (const [pointer, resource] of entries(trigger.resources)) assert(pointerValid(pointer) && fixedPattern(namePattern, resource));
      for (const [pointer, type] of entries(trigger.types)) assert(pointerValid(pointer) && ["string", "boolean", "number", "integer"].includes(type));
    }
  }
  for (const [name, server] of entries(spec.httpMcp)) {
    fields(server, ["url", "credential", "headers", "readOnly", "tools", "maxResponseBytes"], ["url", "credential", "readOnly", "tools", "maxResponseBytes"]);
    assert(fixedPattern(namePattern, name));
    origin(server.url, true);
    assert(server.readOnly === true && spec.credentials[server.credential]?.phase === "agent" && count(server.tools));
    assert(Number.isInteger(server.maxResponseBytes) && server.maxResponseBytes >= 1 && server.maxResponseBytes <= MAX_RESPONSE);
    for (const [key, value] of entries(server.headers)) assert(key.startsWith("X-MCP-") && !/[\r\n$]/u.test(key + value) && typeof value === "string");
    for (const [tool, constraints] of entries(server.tools)) {
      assert(fixedPattern(toolPattern, tool));
      fields(constraints, ["inputs", "resources"], ["inputs"]);
      validateInputs(constraints.inputs);
      for (const [input, resource] of entries(constraints.resources)) assert(constraints.inputs[input]?.required && constraints.inputs[input].type === "string" && fixedPattern(namePattern, resource));
    }
  }
  for (const [name, op] of entries(spec.safeOutputs)) {
    for (const [input, producer] of entries(op.createdResources)) {
      const creator = spec.safeOutputs[producer];
      assert(creator?.creates && producer !== name && op.resources?.[input] === creator.creates.resource);
      assert(spec.endpoints[op.request.endpoint] === spec.endpoints[creator.request.endpoint]);
    }
  }
  let selected = 0;
  const tools = new Set();
  for (const group of ["mcp", "httpMcp", "safeOutputs", "context", "triggers"]) {
    const selection = binding[group] || [];
    assert(Array.isArray(selection) && new Set(selection).size === selection.length);
    for (const name of selection) {
      assert(typeof name === "string" && own(spec[group] || {}, name), "Connector capability is not selected");
      selected++;
      if (group === "triggers") {
        const trigger = spec.triggers[name];
        const { eventPredicateConfigured } = require("./event_predicate.cjs");
        assert(eventPredicateConfigured(trigger.when) && (trigger.variants || []).every(variant => eventPredicateConfigured(variant.when)), "Selected transition trigger requires explicit terminal values");
        for (const resource of Object.values(trigger.resources || {})) assert(connection.resources[resource]?.length, "Connector trigger resource grant is missing");
        continue;
      }
      const op = spec[group][name];
      assert(own(connection.credentials, op.credential), "Connector selected credential reference is missing");
      for (const [input, resource] of entries(op.resources)) {
        assert(connection.resources[resource]?.length || (group === "safeOutputs" && (op.trustedInputs?.[input] || binding.safeOutputs?.includes(op.createdResources?.[input]))), "Connector resource grant is missing");
      }
      for (const resource of Object.values(op.responseResources || {})) assert(connection.resources[resource]?.length, "Connector response resource grant is missing");
      if (op.preflight)
        for (const resource of [...Object.values(op.preflight.resources || {}), ...Object.values(op.preflight.responseResources || {})]) assert(connection.resources[resource]?.length, "Connector preflight resource grant is missing");
      const publicNames = group === "mcp" ? [op.tool] : group === "httpMcp" ? names(op.tools).map(tool => name + "__" + tool) : [];
      for (const publicName of publicNames) {
        assert(publicName.length <= 128 && !tools.has(publicName), "Connector public tool name collision");
        tools.add(publicName);
      }
      if (group === "httpMcp")
        for (const constraints of Object.values(op.tools)) for (const resource of Object.values(constraints.resources || {})) assert(connection.resources[resource]?.length, "Connector remote resource grant is missing");
    }
  }
  assert(selected > 0);
}
function decodePlan(encoded) {
  assert(typeof encoded === "string" && encoded.length <= 65536, "Compiled connector plan exceeds its byte bound");
  const { document, raw } = parseJSON(decodeBase64(encoded).toString("utf8"), MAX_DOCUMENT, true);
  fields(document, ["digest", "spec", "connection", "binding"], ["digest", "spec", "connection", "binding"]);
  assert(fixedPattern(/^[a-f0-9]{64}$/, document.digest), "Invalid connector plan digest");
  const hash = createHash("sha256")
    .update('{"Spec":' + raw.spec + ',"Connection":' + raw.connection + ',"Binding":' + raw.binding + "}")
    .digest("hex");
  assert(hash === document.digest, "Compiled connector plan digest mismatch");
  validatePlan(document);
  return document;
}

function restrictPhase(plan, phase) {
  const { spec, connection, binding } = plan;
  const groups = phase === "agent" ? ["mcp", "httpMcp"] : phase === "context" ? ["context"] : ["safeOutputs"];
  const credentials = new Set();
  const endpoints = new Set();
  const resources = new Set();
  for (const group of ["mcp", "httpMcp", "context", "safeOutputs", "triggers"]) {
    assert(groups.includes(group) || (!count(spec[group]) && !binding[group]?.length), "Connector host requires a compiler-restricted single-phase plan");
    if (!groups.includes(group)) continue;
    const selected = binding[group] || [];
    assert(names(spec[group]).length === selected.length && names(spec[group]).every(key => selected.includes(key)), "Connector snapshot contains an unselected capability");
    for (const name of selected) {
      const op = spec[group][name];
      credentials.add(op.credential);
      if (group !== "httpMcp") endpoints.add(op.request.endpoint);
      for (const resource of [...Object.values(op.resources || {}), ...Object.values(op.responseResources || {})]) resources.add(resource);
      for (const read of op.lookups || []) for (const resource of [...Object.values(read.resources || {}), ...Object.values(read.responseResources || {})]) resources.add(resource);
      for (const read of op.checks || []) for (const resource of [...Object.values(read.resources || {}), ...Object.values(read.responseResources || {})]) resources.add(resource);
      if (op.preflight) for (const resource of [...Object.values(op.preflight.resources || {}), ...Object.values(op.preflight.responseResources || {})]) resources.add(resource);
      if (group === "httpMcp") for (const tool of Object.values(op.tools)) for (const resource of Object.values(tool.resources || {})) resources.add(resource);
      for (const producer of Object.values(op.createdResources || {})) assert(binding.safeOutputs?.includes(producer), "Connector snapshot contains an unselected creator");
    }
  }
  // ForPhase names the fixed remote origins after reserving REST endpoint keys.
  let index = 0;
  for (const name of binding.httpMcp || []) {
    while (endpoints.has("remote-mcp-" + index)) index++;
    const key = "remote-mcp-" + index++;
    assert(spec.endpoints[key] === origin(spec.httpMcp[name].url, true), "Connector remote endpoint origin is not approved");
    endpoints.add(key);
  }
  assert(names(spec.endpoints).length === endpoints.size && names(spec.endpoints).every(key => endpoints.has(key)), "Connector snapshot contains an unselected endpoint");
  for (const map of [spec.credentials, connection.credentials]) assert(names(map).length === credentials.size && names(map).every(key => credentials.has(key)), "Connector snapshot contains an unselected credential");
  assert(
    names(connection.resources).every(key => resources.has(key)),
    "Connector snapshot contains an unselected resource grant"
  );
}
async function validateArgument(input, value) {
  if (input.type === "string") {
    const minimum = Math.max(input.minLength || 0, input.sanitize === "title" ? 20 : 0);
    assert(typeof value === "string" && Array.from(value).length <= input.maxLength && Array.from(value).length >= minimum, "Connector argument must be a bounded Unicode string");
    assert(!input.enum?.length || input.enum.includes(value), "Connector argument is outside its declared enum");
    if (input.pattern) {
      let matched = false;
      try {
        matched = await matchesConnectorPattern(input.pattern, value);
      } catch {
        fail("Connector argument pattern could not be validated");
      }
      assert(matched, "Connector argument does not match its declared pattern");
    }
  } else if (input.type === "boolean") {
    assert(typeof value === "boolean", "Connector argument must be a boolean");
  } else {
    assert(
      typeof value === "number" && Number.isFinite(value) && (!Number.isInteger(value) || Number.isSafeInteger(value)) && (input.type !== "integer" || Number.isInteger(value)),
      "Connector argument must be a finite safe number of its declared type"
    );
    assert((input.minimum === undefined || value >= input.minimum) && (input.maximum === undefined || value <= input.maximum), "Connector number is outside its declared bounds");
  }
}
async function validateArguments(inputs, resources, allowed, args) {
  assert(object(args), "Connector arguments must be a JSON object");
  assert(
    names(args).every(key => own(inputs || {}, key)),
    "Unknown connector argument"
  );
  for (const [key, input] of entries(inputs)) {
    if (!own(args, key)) {
      assert(!input.required, "Required connector argument is missing");
      continue;
    }
    await validateArgument(input, args[key]);
    if (resources?.[key]) assert(typeof args[key] === "string" && allowed[resources[key]]?.includes(args[key]), "Connector argument is outside approved resources");
  }
}
function normalizeArguments(args) {
  assert(object(args), "Connector arguments must be a JSON object");
  assert(
    names(args).every(key => args[key] !== undefined && typeof args[key] !== "function" && typeof args[key] !== "symbol"),
    "Connector arguments must be JSON data"
  );
  let text;
  try {
    text = JSON.stringify(args);
  } catch {
    return fail("Connector arguments must be JSON data");
  }
  const document = parseJSON(text).document;
  if (numberTokens.has(args)) numberTokens.set(document, numberTokens.get(args));
  return document;
}
function resolveValue(value, args) {
  let resolved;
  if (value.input) {
    if (!own(args, value.input)) return undefined;
    resolved = args[value.input];
    if (value.prefix !== undefined) {
      assert(typeof resolved === "string", "Prefixed input must be a string");
      resolved = value.prefix + resolved;
    }
    if (value.encoding) {
      assert(typeof resolved === "string", "Encoded connector input must be a string");
      const text = resolved.replaceAll("\r\n", "\n");
      if (value.encoding === "confluence-storage") {
        const escaped = text.replace(/[&<>"']/gu, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&#34;", "'": "&#39;" })[char]);
        resolved = "<p>" + escaped.replaceAll("\n", "<br />") + "</p>";
      } else {
        const content = [];
        text.split("\n").forEach((line, index) => {
          if (index) content.push({ type: "hardBreak" });
          if (line) content.push({ type: "text", text: line });
        });
        resolved = { version: 1, type: "doc", content: [{ type: "paragraph", content }] };
      }
    }
  } else if (value.object !== undefined) {
    resolved = resolveValues(value.object, args);
  } else if (value.array !== undefined) {
    let size = 2;
    const tokens = new Map();
    resolved = value.array.map((child, index) => {
      const result = resolveValue(child, args);
      assert(result !== undefined, "Structured connector array input is missing");
      size += jsonBytes(result) + 1;
      assert(size <= MAX_DOCUMENT, "Structured connector array exceeds its byte bound");
      if (typeof result === "number") tokens.set(String(index), valueNumberToken(child, args));
      return result;
    });
    numberTokens.set(resolved, tokens);
  } else resolved = value.literal;
  assert(jsonBytes(resolved) <= MAX_DOCUMENT, "Resolved connector value exceeds its byte bound");
  return resolved;
}
function valueNumberToken(value, args) {
  return value.input ? numberTokens.get(args)?.get(value.input) : numberTokens.get(value)?.get("literal");
}
function resolveValues(values, args) {
  const result = Object.create(null);
  const tokens = new Map();
  numberTokens.set(result, tokens);
  let size = 2;
  for (const [key, value] of entries(values)) {
    const resolved = resolveValue(value, args);
    if (resolved === undefined) continue;
    size += jsonBytes(resolved) + Buffer.byteLength(key) * 6 + 4;
    assert(size <= MAX_DOCUMENT, "Structured connector object exceeds its byte bound");
    result[key] = resolved;
    if (typeof resolved === "number") tokens.set(key, valueNumberToken(value, args));
  }
  return result;
}
function escapeComponent(value) {
  return encodeURIComponent(value).replace(/[!'()*]/gu, char => "%" + char.charCodeAt(0).toString(16).toUpperCase());
}
/** @returns {{url: string, init: {method: string, headers: Record<string,string>, body?: string|Buffer}}} */
function buildRequest(plan, op, args) {
  let path = op.request.path;
  for (const [placeholder, input] of entries(op.request.pathInputs)) {
    const value = args[input];
    assert(typeof value === "string" && value !== "" && value !== "." && !/[\/\\%?#\r\n]/u.test(value) && !value.includes(".."), "Connector path argument must be one safe segment");
    const escaped = escapeComponent(value).replace(/%(?:24|26|2B|3A|3D|40)/gu, code => decodeURIComponent(code));
    path = path.replaceAll("{" + placeholder + "}", () => escaped);
  }
  const endpoint = plan.spec.endpoints[op.request.endpoint];
  const approvedOrigin = origin(endpoint);
  const url = new URL(endpoint.replace(/\/$/u, "") + path);
  assert(url.origin === approvedOrigin && !url.username && !url.password && !url.hash, "Connector request changed its approved origin");
  let body;
  let contentType = "application/json";
  if (op.request.graphql) {
    body = { query: op.request.graphql, variables: resolveValues(op.request.query, args) };
  } else {
    const query = resolveValues(op.request.query, args);
    url.search = names(query)
      .sort()
      .map(key => {
        const value = typeof query[key] === "number" ? numberTokens.get(query)?.get(key) || String(query[key]) : String(query[key]);
        return escapeComponent(key).replaceAll("%20", "+") + "=" + escapeComponent(value).replaceAll("%20", "+");
      })
      .join("&");
    if (op.request.patch?.length || op.request.additionalPatch?.length) {
      contentType = "application/json-patch+json";
      body = [...(op.request.patch || []), ...(op.request.additionalPatch || [])].map(patch => {
        const value = resolveValue(patch.value, args);
        assert(value !== undefined, "Connector JSON Patch value is missing");
        const result = { op: patch.op, path: patch.path, value };
        if (typeof value === "number") numberTokens.set(result, new Map([["value", valueNumberToken(patch.value, args)]]));
        return result;
      });
    } else if (count(op.request.body)) body = resolveValues(op.request.body, args);
  }
  const encoded = body === undefined ? undefined : goJSON(body);
  assert(encoded === undefined || Buffer.byteLength(encoded) <= MAX_DOCUMENT, "Connector request body exceeds its byte bound");
  const headers = { Accept: "application/json" };
  if (encoded !== undefined) headers["Content-Type"] = contentType;
  for (const [name, value] of entries(op.request.headers)) setHeader(headers, name, value);
  return { url: url.href, init: { method: op.request.method, headers, body: encoded } };
}
function setHeader(headers, name, value) {
  for (const existing of names(headers)) if (existing.toLowerCase() === name.toLowerCase()) delete headers[existing];
  Object.defineProperty(headers, name, { value, enumerable: true, configurable: true, writable: true });
}
function secret(env, name, credential = true) {
  const value = env["GH_AW_CONNECTOR_SECRET_" + name];
  assert(typeof value === "string" && value !== "" && (!credential || !/[\r\n]/u.test(value)), credential ? "Connector credential resolution failed" : "Trusted connector input is unavailable");
  return value;
}
function authenticate(plan, name, phase, headers, env) {
  const credential = plan.spec.credentials[name];
  const ref = plan.connection.credentials[name];
  assert(credential?.phase === phase && ref, "Connector credential phase mismatch");
  const prefix = { bearer: "Bearer ", token: "token ", basic: "Basic ", raw: "" }[credential.scheme];
  setHeader(headers, credential.header, prefix + secret(env, ref.secret));
}
function lookup(document, pointer) {
  if (pointer === "") return document;
  if (!pointerValid(pointer)) return undefined;
  let current = document;
  for (const part of pointer.slice(1).split("/")) {
    const token = part.replaceAll("~1", "/").replaceAll("~0", "~");
    if (Array.isArray(current)) {
      if (!/^(?:0|[1-9][0-9]*)$/u.test(token) || !Number.isSafeInteger(Number(token))) return undefined;
      current = current[Number(token)];
    } else if (object(current) && own(current, token)) current = current[token];
    else return undefined;
  }
  return current;
}
function equivalent(left, right) {
  if (left === undefined || right === undefined) return false;
  return goJSON(left, true) === goJSON(right, true);
}
function numericToken(document, pointer) {
  const slash = pointer.lastIndexOf("/");
  const parent = slash === 0 ? document : lookup(document, pointer.slice(0, slash));
  const key = pointer
    .slice(slash + 1)
    .replaceAll("~1", "/")
    .replaceAll("~0", "~");
  return object(parent) || Array.isArray(parent) ? numberTokens.get(parent)?.get(key) : undefined;
}
function responsePointers(document, pointer) {
  const split = pointer.indexOf("/*");
  if (split < 0) return [pointer];
  const prefix = pointer.slice(0, split);
  const suffix = pointer.slice(split + 2);
  const array = lookup(document, prefix);
  assert(Array.isArray(array) && array.length <= 100 && !suffix.includes("*") && (!suffix || suffix.startsWith("/")), "Connector resource response array is missing or exceeds its bound");
  return array.map((_, index) => prefix + "/" + index + suffix);
}
function compareResponse(document, pointer, expected, expectedToken) {
  const actual = lookup(document, pointer);
  if (typeof actual === "number" && typeof expected === "number") return (numericToken(document, pointer) || goJSON(actual)) === (expectedToken || goJSON(expected));
  return equivalent(actual, expected);
}
async function validateResponse(op, text, args, allowed) {
  if (!text.length) {
    assert(
      !op.request.graphql &&
        ![
          "responseChecks",
          "responseResources",
          "responseRequired",
          "responseOptionalEmpty",
          "responseTypes",
          "responseInputs",
          "responseResourceInputs",
          "responseContains",
          "responseLimits",
          "responseAnyInputs",
          "project",
          "responseWhen",
        ].some(key => count(op[key])),
      "Connector response is missing required evidence"
    );
    return null;
  }
  const document = parseJSON(text, op.maxResponseBytes).document;
  if (op.responseWhen !== undefined) {
    const result = require("./event_predicate.cjs").evaluateEventPredicate(op.responseWhen, document);
    assert(result.known && result.matched, "Connector response does not satisfy its required source-evidence policy");
  }
  for (const [pointer, maximum] of entries(op.responseLimits)) {
    const value = lookup(document, pointer);
    assert(Array.isArray(value) && value.length <= maximum, "Connector response array is missing or exceeds its declared result bound");
  }
  if (op.request.graphql) {
    const errors = lookup(document, "/errors");
    assert(errors === undefined || errors === null || (Array.isArray(errors) && !errors.length), "Connector GraphQL response reports errors");
    assert(lookup(document, "/data") !== undefined, "Connector GraphQL response is missing data");
  }
  for (const [pointer, expected] of entries(op.responseChecks)) assert(compareResponse(document, pointer, expected, numberTokens.get(op.responseChecks)?.get(pointer)), "Connector response check failed");
  for (const pointer of op.responseRequired || []) {
    const value = lookup(document, pointer);
    assert(value !== undefined && value !== null && value !== "", "Connector response is missing required evidence");
  }
  for (const pointer of op.responseOptionalEmpty || []) {
    const value = lookup(document, pointer);
    assert(value === undefined || value === null || value === "", "Connector response contains nonempty optional evidence");
  }
  for (const [pointer, resource] of entries(op.responseResources)) {
    for (const current of responsePointers(document, pointer)) {
      const value = lookup(document, current);
      const text = typeof value === "number" ? numericToken(document, current) : value;
      assert((typeof value === "string" || typeof value === "number") && allowed[resource]?.includes(text), "Connector response is outside approved resources");
    }
  }
  for (const [pointer, type] of entries(op.responseTypes)) await validateArgument({ type, maxLength: MAX_DOCUMENT }, lookup(document, pointer));
  for (const [pointer, input] of entries(op.responseInputs))
    for (const current of responsePointers(document, pointer)) assert(own(args, input) && compareResponse(document, current, args[input], numberTokens.get(args)?.get(input)), "Connector response does not match its request input");
  for (const [input, pointers] of entries(op.responseAnyInputs)) {
    assert(typeof args[input] === "string" && pointers.some(pointer => lookup(document, pointer) === args[input]), "Connector response does not match its requested alias identity");
  }
  for (const [pointer, input] of entries(op.responseResourceInputs)) {
    const actual = lookup(document, pointer);
    const expected = args[input];
    const token = typeof actual === "number" ? numericToken(document, pointer) : undefined;
    assert(
      typeof expected === "string" && ((typeof actual === "string" && actual === expected) || (typeof actual === "number" && Number.isSafeInteger(actual) && actual >= 0 && fixedPattern(/^(?:0|[1-9][0-9]*)$/, token) && token === expected)),
      "Connector response does not match its exact resource input"
    );
  }
  for (const [pointer, input] of entries(op.responseContains)) {
    const split = pointer.indexOf("/*");
    const array = lookup(document, pointer.slice(0, split));
    assert(Array.isArray(array) && array.some(item => lookup(item, pointer.slice(split + 2)) === args[input]), "Connector response does not establish its required resource relationship");
  }
  if (!count(op.project)) return document;
  const projected = Object.create(null);
  const tokens = new Map();
  numberTokens.set(projected, tokens);
  for (const [field, pointer] of entries(op.project)) {
    const value = lookup(document, pointer);
    assert(value !== undefined && value !== null, "Connector response projection is missing required evidence");
    projected[field] = value;
    if (typeof value === "number") tokens.set(field, numericToken(document, pointer));
  }
  return projected;
}

function sseResult(text, id, final = false) {
  const blocks = text
    .replace(/^\uFEFF/u, "")
    .replaceAll("\r\n", "\n")
    .replaceAll("\r", "\n")
    .split("\n\n");
  if (!final) blocks.pop();
  for (const block of blocks) {
    const data = block
      .split("\n")
      .filter(line => line.startsWith("data:"))
      .map(line => line.slice(5).replace(/^ /u, ""))
      .join("\n");
    if (!data) continue;
    const message = parseJSON(data, MAX_RESPONSE).document;
    assert(!message.method || !own(message, "id"), "Connector HTTP MCP callbacks are unsupported");
    if (message.id === id) return message;
  }
  return undefined;
}
/** @param {any} fetchImpl @param {string} url @param {any} init @param {number} maximum @param {number|undefined} [rpcId] @param {number} [timeout] */
async function boundedHTTP(fetchImpl, url, init, maximum, rpcId = undefined, timeout = REQUEST_TIMEOUT) {
  const controller = new AbortController();
  let timer;
  let reader;
  const deadline = new Promise((_, reject) => {
    timer = setTimeout(() => {
      controller.abort();
      reject(new ConnectorError("Connector HTTP request timed out; outcome may be unknown, no retry performed"));
    }, timeout);
  });
  const operation = async () => {
    let response;
    try {
      response = await fetchImpl(url, {
        ...init,
        signal: controller.signal,
        redirect: "manual",
        credentials: "omit",
        cache: "no-store",
        referrerPolicy: "no-referrer",
      });
    } catch {
      return fail("Connector HTTP request failed; outcome may be unknown, no retry performed");
    }
    assert(!response.redirected && (!response.url || response.url === url), "Connector redirects and endpoint changes are prohibited");
    assert(Number.isInteger(response.status), "Connector HTTP response has an invalid status");
    if (response.status < 200 || response.status >= 300) throw new ConnectorHTTPError(response.status, response.headers.get("retry-after") || "");
    const length = response.headers.get("content-length");
    assert(!length || (/^[0-9]+$/u.test(length) && Number(length) <= maximum), "Connector response exceeds its byte bound");
    const mediaType = (response.headers.get("content-type") || "").split(";")[0].trim().toLowerCase();
    if (rpcId !== undefined) assert(["application/json", "text/event-stream"].includes(mediaType), "Connector HTTP MCP response must be JSON or SSE");
    if (!response.body) return { text: "", response };
    reader = response.body.getReader();
    const chunks = [];
    let size = 0;
    const sse = rpcId !== undefined && mediaType === "text/event-stream";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.byteLength;
      assert(size <= maximum, "Connector response exceeds its byte bound");
      chunks.push(Buffer.from(value));
      if (sse) {
        const message = sseResult(Buffer.concat(chunks).toString("utf8"), rpcId);
        if (message !== undefined) return { text: goJSON(message), response };
      }
    }
    const text = Buffer.concat(chunks).toString("utf8");
    if (sse) {
      const message = sseResult(text, rpcId, true);
      assert(message !== undefined, "Connector HTTP MCP response is missing its request result");
      return { text: goJSON(message), response };
    }
    return { text, response };
  };
  try {
    return await Promise.race([operation(), deadline]);
  } finally {
    clearTimeout(timer);
    controller.abort();
    if (reader) void reader.cancel().catch(() => {});
  }
}
/** @param {Buffer|undefined} [binaryBody] */
async function runOperation(plan, op, capability, name, args, allowed, env, fetchImpl, deadline = Date.now() + REQUEST_TIMEOUT, binaryBody = undefined) {
  const request = buildRequest(plan, op, args);
  if (op.request.binary) {
    assert(Buffer.isBuffer(binaryBody), "Binary content was not prepared by the authorized host");
    request.init.body = binaryBody;
    request.init.headers["Content-Type"] = "application/octet-stream";
  }
  authenticate(plan, op.credential, capability === "mcp" ? "agent" : capability === "preflight" ? "safe-output" : capability, request.init.headers, env);
  const attempts = op.retry?.maxAttempts || 1;
  for (let attempt = 0; attempt < attempts; attempt++) {
    assert(deadline > Date.now(), "Connector retries exceeded the bounded execution window");
    let result;
    try {
      result = await boundedHTTP(fetchImpl, request.url, request.init, op.maxResponseBytes, undefined, deadline - Date.now());
    } catch (error) {
      if (!(error instanceof ConnectorHTTPError) || !op.retry?.retryOn.includes(error.status)) throw error;
      assert(attempt + 1 < attempts, `Connector HTTP status ${error.status} after ${attempt + 1} attempt(s)`);
      let delay = op.retry.backoffMs * 2 ** attempt;
      if (error.retryAfter) {
        const header = error.retryAfter.trim();
        const numeric = /^[0-9]+$/u.test(header);
        const seconds = Number(header);
        if (numeric) assert(Number.isSafeInteger(seconds) && seconds <= REQUEST_TIMEOUT / 1000, "Connector retry delay exceeds the bounded execution window");
        const suggested = numeric ? seconds * 1000 : Date.parse(header) - Date.now();
        if (Number.isFinite(suggested)) delay = Math.max(delay, suggested);
      }

      assert(delay < deadline - Date.now(), "Connector retry delay exceeds the bounded execution window");
      await new Promise(resolve => setTimeout(resolve, delay));
      continue;
    }
    const data = await validateResponse(op, result.text, args, allowed);
    return { connector: plan.spec.id, capability: capability + "/" + name, untrusted: true, statusCode: result.response.status, data };
  }
  throw new ConnectorError("Connector retry attempts exhausted");
}

async function lookupArguments(op, args, allowed) {
  const inputs = { ...op.inputs };
  const resources = { ...op.resources };
  const unresolved = [];
  for (const read of op.lookups || []) {
    const input = read.input;
    if (!input) continue;
    let canonical = true;
    try {
      await validateArgument(op.inputs[input], args[input]);
    } catch (error) {
      if (!(error instanceof ConnectorError)) throw error;
      canonical = false;
    }
    if (canonical) continue;
    inputs[input] = read.inputs[input];
    resources[input] = read.resources[input];
    unresolved.push(input);
  }
  await validateArguments(inputs, resources, allowed, args);
  if (unresolved.length) {
    for (const read of op.lookups) {
      if (read.input && !unresolved.includes(read.input)) continue;
      for (const resource of [...Object.values(read.resources || {}), ...Object.values(read.responseResources || {})]) assert(allowed[resource]?.length, "Connector lookup resource grant is missing");
    }
  }
  return unresolved.sort();
}

async function resolveLookups(plan, op, args, allowed, env, fetchImpl, capability) {
  const unresolved = await lookupArguments(op, args, allowed);
  if (!unresolved.length) return args;
  const resolved = { ...args };
  if (numberTokens.has(args)) numberTokens.set(resolved, numberTokens.get(args));
  const deadline = Date.now() + REQUEST_TIMEOUT;
  for (const read of op.lookups) {
    if (read.input && !unresolved.includes(read.input)) continue;
    const inputs = Object.fromEntries(names(read.inputs).map(input => [input, resolved[input]]));
    await validateArguments(read.inputs, read.resources, allowed, inputs);
    const result = await runOperation(plan, { ...read, credential: op.credential }, capability, "lookup", inputs, allowed, env, fetchImpl, deadline);
    if (read.input) {
      const value = result.data?.[read.input];
      await validateArgument(op.inputs[read.input], value);
      resolved[read.input] = value;
    }
  }
  await validateArguments(op.inputs, op.resources, allowed, resolved);
  return resolved;
}

function bindingHost(env) {
  const run = env.GH_AW_CONNECTOR_BINDING_RUN;
  const key = decodeBase64(env.GH_AW_CONNECTOR_BINDING_KEY || "");
  const receipts = parseJSON(env.GH_AW_CONNECTOR_BINDING_RECEIPTS || "").document;
  assert(boundedString(run, 128, 1) && key.length === 32 && Array.isArray(receipts) && receipts.length <= 128 && receipts.every(token => boundedString(token, 8192, 1)), "Created-resource operations require fresh trusted execution state");
  assert(jsonBytes(receipts) <= MAX_DOCUMENT, "Created-resource receipt state exceeds its byte bound");
  return { run, key, receipts };
}
function receiptJSON(receipt) {
  return goJSON(Object.fromEntries(receiptFields.map(field => [field, receipt[field]])));
}
function readReceipt(plan, host, token) {
  assert(boundedString(token, 8192, 1), "Invalid created-resource receipt");
  const parts = token.split(".");
  assert(parts.length === 2, "Invalid created-resource receipt");
  const data = decodeBase64(parts[0], true);
  const signature = decodeBase64(parts[1], true);
  const expected = createHmac("sha256", host.key).update(data).digest();
  assert(signature.length === expected.length && timingSafeEqual(signature, expected), "Created-resource receipt authentication failed");
  const receipt = parseJSON(data.toString("utf8")).document;
  fields(receipt, receiptFields, receiptFields);
  assert(
    receiptFields.every(field => typeof receipt[field] === "string"),
    "Invalid created-resource receipt"
  );
  assert(receipt.Run === host.run && receipt.Digest === plan.digest && receipt.Connector === plan.spec.id, "Created-resource receipt belongs to a different execution or connector plan");
  const creator = plan.spec.safeOutputs?.[receipt.Producer];
  const created = creator?.creates;
  assert(
    created &&
      plan.binding.safeOutputs?.includes(receipt.Producer) &&
      receipt.Endpoint === plan.spec.endpoints[creator.request.endpoint] &&
      receipt.Resource === created.resource &&
      receipt.Scope === created.scope &&
      fixedPattern(temporaryPattern, receipt.TemporaryID) &&
      !receipt.TemporaryID.startsWith("#"),
    "Created-resource receipt does not match its approved creation scope"
  );
  assert(creator.trustedInputs?.[receipt.Scope] || plan.connection.resources[creator.resources[receipt.Scope]]?.includes(receipt.ScopeValue), "Created-resource receipt does not match its approved creation scope");
  assert(boundedString(receipt.Value, 256, 1) && (!created.scopePrefix || receipt.Value.startsWith(receipt.ScopeValue + created.scopePrefix)), "Created-resource receipt violates its approved identifier scope");
  return receipt;
}
async function stageIntent(plan, name, op, args) {
  const inputs = { ...op.inputs };
  const resources = { ...op.resources };
  const allowed = { ...plan.connection.resources };
  const resolved = { ...args };
  if (numberTokens.has(args)) numberTokens.set(resolved, numberTokens.get(args));
  for (const input of names(op.trustedInputs)) {
    assert(!own(args, input), "Connector author-bound argument cannot be supplied by the agent");
    delete inputs[input];
    delete resources[input];
  }
  for (const [input, producer] of entries(op.createdResources)) {
    const value = args[input];
    if (!fixedPattern(temporaryPattern, value) || !plan.binding.safeOutputs.includes(producer)) continue;
    inputs[input] = { ...inputs[input], pattern: "^#?aw_[A-Za-z0-9_]{3,12}$", enum: [] };
    resolved[input] = value.replace(/^#/u, "");
    allowed[resources[input]] = [...(allowed[resources[input]] || []), resolved[input]];
  }
  const unresolved = await lookupArguments({ ...op, inputs, resources }, resolved, allowed);
  if (!unresolved.length) buildRequest(plan, op, resolved);
  assert(jsonBytes(resolved) <= MAX_DOCUMENT, "Safe-output connector arguments exceed their byte bound");
  return { staged: true, request: { digest: plan.digest, operation: name, arguments: resolved, ...(unresolved.length ? { unresolvedInputs: unresolved } : {}) } };
}
async function resolveSafeOutput(plan, name, op, args, env, fetchImpl) {
  const allowed = { ...plan.connection.resources };
  const resolved = { ...args };
  if (numberTokens.has(args)) numberTokens.set(resolved, numberTokens.get(args));
  const inputs = { ...op.inputs };
  const resources = { ...op.resources };
  for (const input of names(op.trustedInputs)) {
    assert(!own(args, input), "Connector author-bound argument cannot be supplied by the agent");
    delete inputs[input];
    delete resources[input];
  }
  for (const input of names(op.createdResources))
    if (fixedPattern(temporaryPattern, args[input])) {
      delete inputs[input];
      delete resources[input];
    }
  const ordinary = Object.fromEntries(entries(args).filter(([input]) => !own(op.createdResources || {}, input) || !fixedPattern(temporaryPattern, args[input])));
  await lookupArguments({ ...op, inputs, resources }, ordinary, allowed);
  let host;
  if (env.GH_AW_CONNECTOR_BINDING_RUN) {
    host = bindingHost(env);
    const receipts = new Map();
    for (const token of host.receipts) {
      const receipt = readReceipt(plan, host, token);
      assert(!receipts.has(receipt.TemporaryID), "Duplicate created-resource receipt");
      receipts.set(receipt.TemporaryID, receipt);
    }
    if (op.creates && typeof args[op.creates.temporaryId] === "string") assert(!receipts.has(args[op.creates.temporaryId].replace(/^#/u, "")), "Temporary created-resource reference is already bound");
    for (const [input, producer] of entries(op.createdResources)) {
      if (!fixedPattern(temporaryPattern, args[input])) continue;
      const receipt = receipts.get(args[input].replace(/^#/u, ""));
      assert(receipt && receipt.Producer === producer && receipt.Resource === op.resources[input] && receipt.Endpoint === plan.spec.endpoints[op.request.endpoint], "Unknown or mismatched temporary created-resource reference");
      resolved[input] = receipt.Value;
      allowed[op.resources[input]] = [...(allowed[op.resources[input]] || []), receipt.Value];
    }
  }
  for (const input of names(op.createdResources)) assert(!fixedPattern(temporaryPattern, resolved[input]), "Temporary created-resource references require authenticated trusted execution state");
  const bound = Object.create(null);
  for (const [input, source] of entries(op.trustedInputs)) {
    const value = source.secret ? secret(env, source.secret, false) : source.value;
    assert(typeof value === "string" && value !== "", "Trusted connector input is unavailable");
    bound[input] = wellFormed(value);
  }
  if (op.preflight) {
    const lookup = { ...op.preflight, credential: op.credential };
    await validateArguments(lookup.inputs, lookup.resources, allowed, bound);
    const result = await runOperation(plan, lookup, "preflight", name, bound, allowed, env, fetchImpl);
    assert(object(result.data), "Trusted connector preflight projection is invalid");
    Object.assign(bound, result.data);
  }
  for (const [input, value] of entries(bound)) {
    await validateArgument(op.inputs[input], value);
    assert(typeof value === "string" && value !== "", "Trusted connector input must resolve to one exact resource");
    allowed[op.resources[input]] = [value];
    resolved[input] = value;
  }
  await lookupArguments(op, resolved, allowed);
  return { resolved, allowed, host };
}
async function createdReceipt(plan, name, op, args, resolved, host, result) {
  if (!op.creates || !own(args, op.creates.temporaryId)) return;
  assert(host, "Created-resource bindings require authenticated trusted execution state");
  let value = lookup(result.data, op.creates.value);
  if (op.creates.valueType === "integer") {
    const token = numericToken(result.data, op.creates.value);
    assert(typeof value === "number" && Number.isSafeInteger(value) && value > 0 && fixedPattern(/^[1-9][0-9]*$/, token), "Created-resource response is missing its canonical positive integer identifier");
    value = token;
  }
  assert(boundedString(value, 256, 1), "Created-resource response is missing its bounded identifier; no binding established");
  for (const consumer of Object.values(plan.spec.safeOutputs)) for (const [input, producer] of entries(consumer.createdResources)) if (producer === name) await validateArgument(consumer.inputs[input], value);
  const scope = resolved[op.creates.scope];
  assert(typeof scope === "string" && (!op.creates.scopePrefix || value.startsWith(scope + op.creates.scopePrefix)), "Created-resource identifier does not match its approved creation scope");
  const receipt = {
    Run: host.run,
    Digest: plan.digest,
    Connector: plan.spec.id,
    Producer: name,
    Endpoint: plan.spec.endpoints[op.request.endpoint],
    Resource: op.creates.resource,
    Scope: op.creates.scope,
    ScopeValue: scope,
    TemporaryID: args[op.creates.temporaryId].replace(/^#/u, ""),
    Value: value,
  };
  const data = Buffer.from(receiptJSON(receipt));
  result.createdResource = data.toString("base64url") + "." + createHmac("sha256", host.key).update(data).digest("base64url");
}

async function executeConnector(encoded, capability, name, args, options = {}) {
  try {
    const { staged = false, env = process.env, fetchImpl = globalThis.fetch } = options;
    assert(["mcp", "context", "safe-output"].includes(capability), "Connector capability must be mcp, context or safe-output");
    assert(!staged || capability === "safe-output", "Connector staging requires safe-output");
    const plan = decodePlan(encoded);
    restrictPhase(plan, capability === "mcp" ? "agent" : capability);
    const group = { mcp: "mcp", context: "context", "safe-output": "safeOutputs" }[capability];
    assert(plan.binding[group]?.includes(name), "Connector capability is not selected");
    const op = plan.spec[group][name];
    const normalized = normalizeArguments(args);
    if (capability === "safe-output") {
      const intent = await stageIntent(plan, name, op, normalized);
      if (staged) return intent;
      const preparation = await resolveSafeOutput(plan, name, op, normalized, env, fetchImpl);
      const { allowed, host } = preparation;
      const binaryBody = op.request.binary ? require("./connector_file.cjs").readConnectorFile(op.request.binary, preparation.resolved, options.stagedFilesRoot, options.readStagedFile) : undefined;
      const resolved = await resolveLookups(plan, op, preparation.resolved, allowed, env, fetchImpl, capability);
      const deadline = Date.now() + REQUEST_TIMEOUT;
      for (const check of op.checks || []) {
        const inputs = Object.fromEntries(names(check.inputs).map(input => [input, resolved[input]]));
        await validateArguments(check.inputs, check.resources, allowed, inputs);
        await runOperation(plan, { ...check, credential: op.credential }, capability, "check", inputs, allowed, env, fetchImpl, deadline);
      }
      if (op.creates && own(normalized, op.creates.temporaryId)) assert(host, "Created-resource bindings require authenticated trusted execution state");
      const result = await runOperation(plan, op, capability, name, resolved, allowed, env, fetchImpl, deadline, binaryBody);
      await createdReceipt(plan, name, op, normalized, resolved, host, result);
      return result;
    }
    const resolved = await resolveLookups(plan, op, normalized, plan.connection.resources, env, fetchImpl, capability);
    return await runOperation(plan, op, capability, name, resolved, plan.connection.resources, env, fetchImpl);
  } catch (error) {
    if (error instanceof ConnectorError) throw error;
    return fail("Connector execution failed; no retry performed");
  }
}

async function callMCP(encoded, registration, tool, args, options = {}) {
  try {
    const { env = process.env, fetchImpl = globalThis.fetch } = options;
    const plan = decodePlan(encoded);
    restrictPhase(plan, "agent");
    assert(plan.binding.httpMcp?.includes(registration), "Connector HTTP MCP registration is not selected");
    const server = plan.spec.httpMcp[registration];
    assert(own(server.tools, tool), "Connector HTTP MCP tool is not in its exact allowlist");
    const normalized = normalizeArguments(args);
    const constraints = server.tools[tool];
    await validateArguments(constraints.inputs, constraints.resources, plan.connection.resources, normalized);
    const endpoint = new URL(server.url).href;
    let session;
    let protocol = "2025-03-26";
    /** @param {string} method @param {any} params @param {number|undefined} [id] */
    const request = async (method, params, id = undefined) => {
      const headers = { ...server.headers, "Content-Type": "application/json", Accept: "application/json, text/event-stream" };
      if (session) headers["Mcp-Session-Id"] = session;
      if (method !== "initialize") headers["MCP-Protocol-Version"] = protocol;
      authenticate(plan, server.credential, "agent", headers, env);
      const message = { jsonrpc: "2.0", method, params };
      if (id !== undefined) message["id"] = id;
      const { text, response } = await boundedHTTP(fetchImpl, endpoint, { method: "POST", headers, body: goJSON(message) }, server.maxResponseBytes, id);
      const nextSession = response.headers.get("mcp-session-id");
      if (nextSession !== null) {
        assert(boundedString(nextSession, 256, 1) && /^[\x21-\x7E]+$/u.test(nextSession) && (!session || session === nextSession), "Connector HTTP MCP returned an invalid session header");
        session = nextSession;
      }
      if (id === undefined) return undefined;
      const result = parseJSON(text, server.maxResponseBytes).document;
      assert(object(result) && result.jsonrpc === "2.0" && result.id === id && !own(result, "error") && own(result, "result") && !own(result, "method"), "Connector HTTP MCP request failed");
      return result.result;
    };
    const initialized = await request("initialize", { protocolVersion: protocol, capabilities: {}, clientInfo: { name: "gh-aw-connector", version: "v1" } }, 1);
    assert(object(initialized) && PROTOCOL_VERSIONS.includes(initialized.protocolVersion), "Connector HTTP MCP protocol version is unsupported");
    protocol = initialized.protocolVersion;
    await request("notifications/initialized", {});
    const result = await request("tools/call", { name: tool, arguments: normalized }, 2);
    assert(object(result) && !result.isError && [undefined, "", "complete"].includes(result.resultType) && !result.inputRequests && !result.needsInput, "Connector HTTP MCP tool returned an error or requires unsupported interactive input");
    assert(Array.isArray(result.content) && jsonBytes(result) <= server.maxResponseBytes, "Connector HTTP MCP result exceeds its byte bound or has invalid content");
    return result;
  } catch (error) {
    if (error instanceof ConnectorError) throw error;
    return fail("Connector HTTP MCP request failed; no retry performed");
  }
}
function inputSchema(inputs) {
  const properties = Object.create(null);
  const required = [];
  for (const [name, input] of entries(inputs)) {
    const { required: mandatory, sanitize, ...property } = input;
    if (sanitize === "title") property.minLength = Math.max(property.minLength || 0, 20);
    properties[name] = property;
    if (mandatory) required.push(name);
  }
  return { type: "object", properties, required: required.sort(), additionalProperties: false };
}
function operationInputSchema(op) {
  const inputs = { ...op.inputs };
  for (const read of op.lookups || []) {
    if (!read.input) continue;
    const field = inputs[read.input];
    const alias = read.inputs[read.input];
    inputs[read.input] = {
      ...field,
      maxLength: Math.max(field.maxLength, alias.maxLength),
      minLength: Math.min(field.minLength || 0, alias.minLength || 0),
      pattern: field.pattern && alias.pattern ? "(?:" + field.pattern + ")|(?:" + alias.pattern + ")" : undefined,
      enum: undefined,
    };
  }
  const schema = inputSchema(inputs);
  if (op.creates) schema["x-gh-aw-temporary-id"] = op.creates.temporaryId;
  if (op.request.binary) {
    schema.properties[op.request.binary.input]["x-gh-aw-staged-file"] = op.request.binary;
    schema.properties[op.request.binary.input].description = "Existing regular file path under the workspace or trusted staging directory; the recorder stages a bounded content-addressed snapshot before delivery.";
  }
  return schema;
}
async function serve(encoded, input = process.stdin, output = process.stdout, options = {}) {
  const plan = decodePlan(encoded);
  restrictPhase(plan, "agent");
  const tools = new Map();
  for (const name of plan.binding.mcp || []) {
    const op = plan.spec.mcp[name];
    tools.set(op.tool, { inputSchema: operationInputSchema(op), call: args => executeConnector(encoded, "mcp", name, args, options) });
  }
  for (const registration of plan.binding.httpMcp || [])
    for (const [tool, constraints] of entries(plan.spec.httpMcp[registration].tools)) {
      tools.set(registration + "__" + tool, {
        inputSchema: inputSchema(constraints.inputs),
        call: async args => ({ untrusted: true, connector: plan.spec.id, result: await callMCP(encoded, registration, tool, args, options) }),
      });
    }
  let pending = Buffer.alloc(0);
  const dispatch = async line => {
    const message = parseJSON(line, MAX_DOCUMENT).document;
    assert(object(message) && message.jsonrpc === "2.0" && typeof message.method === "string", "Invalid connector MCP request");
    if (!own(message, "id")) return;
    assert(typeof message.id === "string" || (typeof message.id === "number" && Number.isSafeInteger(message.id)), "Invalid connector MCP request ID");
    let result;
    let error;
    if (message.method === "initialize")
      result = { protocolVersion: PROTOCOL_VERSIONS.includes(message.params?.protocolVersion) ? message.params.protocolVersion : "2025-03-26", capabilities: { tools: {} }, serverInfo: { name: "gh-aw-connectors", version: "v1" } };
    else if (message.method === "ping") result = {};
    else if (message.method === "tools/list") result = { tools: [...tools].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)).map(([name, value]) => ({ name, inputSchema: value.inputSchema })) };
    else if (message.method === "tools/call") {
      try {
        assert(typeof message.params?.name === "string" && tools.has(message.params.name), "Connector MCP tool is not selected");
        const args = message.params.arguments === undefined ? {} : message.params.arguments;
        const data = await tools.get(message.params.name).call(args);
        result = { content: [{ type: "text", text: goJSON(data) }] };
      } catch (cause) {
        result = {
          isError: true,
          content: [{ type: "text", text: cause instanceof ConnectorError ? cause.message : "Connector MCP request failed" }],
          ...(cause instanceof ConnectorHTTPError ? { structuredContent: { httpStatus: cause.status } } : {}),
        };
      }
    } else error = { code: -32601, message: "Method not found" };
    output.write(goJSON({ jsonrpc: "2.0", id: message.id, ...(error ? { error } : { result }) }) + "\n");
  };
  for await (const chunk of input) {
    pending = Buffer.concat([pending, Buffer.from(chunk)]);
    let newline;
    while ((newline = pending.indexOf(10)) >= 0) {
      assert(newline <= MAX_DOCUMENT, "Connector MCP request exceeds its byte bound");
      const line = pending.subarray(0, newline).toString("utf8").trim();
      pending = pending.subarray(newline + 1);
      if (line) await dispatch(line);
    }
    assert(pending.length <= MAX_DOCUMENT, "Connector MCP request exceeds its byte bound");
  }
  if (pending.length) await dispatch(pending.toString("utf8"));
}
async function main(argv) {
  const encoded = process.env.GH_AW_CONNECTOR_PLAN || "";
  if (argv.length === 1 && argv[0] === "serve") return serve(encoded);
  assert(argv[0] === "invoke" && argv.length >= 3, "Usage: connector_runtime.cjs invoke <mcp|context|safe-output> <operation> [--stage|--allow-writes]");
  const [capability, name, ...flags] = argv.slice(1);
  assert(flags.length <= 1 && flags.every(flag => ["--stage", "--allow-writes"].includes(flag)), "Invalid connector invocation flags");
  const staged = flags.includes("--stage");
  assert(!staged || capability === "safe-output", "Connector --stage requires safe-output");
  assert(capability !== "safe-output" || staged || flags.includes("--allow-writes"), "Connector safe-output execution requires explicit host write authorization");
  let size = 0;
  const chunks = [];
  for await (const chunk of process.stdin) {
    size += Buffer.byteLength(chunk);
    assert(size <= MAX_DOCUMENT, "Connector arguments exceed their byte bound");
    chunks.push(Buffer.from(chunk));
  }
  const args = parseJSON(Buffer.concat(chunks).toString("utf8")).document;
  const result = await executeConnector(encoded, capability, name, args, { staged, stagedFilesRoot: process.env.GH_AW_CONNECTOR_FILE_ROOT });
  process.stdout.write(goJSON(result) + "\n");
}

module.exports = { decodePlan, executeConnector, callMCP, serve };
if (require.main === module) {
  main(process.argv.slice(2)).catch(error => {
    process.stderr.write((error instanceof ConnectorError ? error.message : "Connector execution failed; no retry performed") + "\n");
    process.exitCode = 1;
  });
}
