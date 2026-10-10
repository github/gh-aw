// @ts-check

const childProcess = require("child_process");
const crypto = require("crypto");
const fs = require("fs");
const os = require("os");
const path = require("path");

const { getErrorMessage } = require("./error_helpers.cjs");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");
const { validateSchemaContract } = require("./memory_schema_contract.cjs");

const DEFAULT_VALIDATION_TIMEOUT_SECONDS = 60;
const MAX_VALIDATION_OUTPUT_BYTES = 12 * 1024;
const MAX_SCHEMA_FILE_BYTES = 100 * 1024 * 1024;

function removePath(targetPath, options) {
  try {
    fs.rmSync(targetPath, options);
  } catch (error) {
    throw new Error(`Failed to remove ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function makeDirectory(targetPath) {
  try {
    fs.mkdirSync(targetPath, { recursive: true });
  } catch (error) {
    throw new Error(`Failed to create directory ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function writeFile(targetPath, content, options) {
  try {
    fs.writeFileSync(targetPath, content, options);
  } catch (error) {
    throw new Error(`Failed to write ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function readDirectory(targetPath) {
  try {
    return fs.readdirSync(targetPath, { withFileTypes: true });
  } catch (error) {
    throw new Error(`Failed to read directory ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function pruneEmptyDirectories(dirPath) {
  for (const entry of readDirectory(dirPath)) {
    if (!entry.isDirectory()) {
      continue;
    }
    const childPath = path.join(dirPath, entry.name);
    pruneEmptyDirectories(childPath);
    if (readDirectory(childPath).length === 0) {
      removePath(childPath, { recursive: true, force: true });
    }
  }
}

/**
 * @param {string} targetPath
 * @param {BufferEncoding | undefined} [encoding]
 * @returns {any}
 */
function readFile(targetPath, encoding) {
  try {
    return encoding === undefined ? fs.readFileSync(targetPath) : fs.readFileSync(targetPath, encoding);
  } catch (error) {
    throw new Error(`Failed to read ${targetPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function makeTempDirectory() {
  try {
    return fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-memory-validation-"));
  } catch (error) {
    throw new Error(`Failed to create memory validation temporary directory: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * @param {string} value
 */
function sanitizeID(value) {
  return String(value || "default").replace(/[^A-Za-z0-9_.-]/g, "_");
}

/**
 * @param {string} kind
 * @param {string} memoryId
 */
function getValidationMarkerPath(kind, memoryId) {
  return path.join(os.tmpdir(), "gh-aw", "memory-validation", `${sanitizeID(kind)}-${sanitizeID(memoryId)}.ok`);
}

function getRepoMemoryBaselinePath(memoryId) {
  return path.join(os.tmpdir(), "gh-aw", "memory-validation", `repo-baseline-${sanitizeID(memoryId)}.json`);
}

/**
 * @param {string} kind
 * @param {string} memoryId
 */
function clearValidationMarker(kind, memoryId) {
  removePath(getValidationMarkerPath(kind, memoryId), { force: true });
}

/**
 * @param {string} kind
 * @param {string} memoryId
 */
function writeValidationMarker(kind, memoryId) {
  const markerPath = getValidationMarkerPath(kind, memoryId);
  makeDirectory(path.dirname(markerPath));
  writeFile(markerPath, "ok\n", "utf8");
  return markerPath;
}

/**
 * @param {Buffer | string | undefined | null} output
 */
function boundedOutput(output) {
  const text = Buffer.isBuffer(output) ? output.toString("utf8") : String(output || "");
  if (Buffer.byteLength(text, "utf8") <= MAX_VALIDATION_OUTPUT_BYTES) {
    return text;
  }
  return text.slice(0, MAX_VALIDATION_OUTPUT_BYTES) + "\n[output truncated]";
}

/**
 * @param {string} dirPath
 * @param {number} maxFileSize
 * @param {(relativePath: string) => boolean} [isEligibleFile]
 */
function formatJSONFiles(dirPath, maxFileSize, isEligibleFile = () => true) {
  if (!fs.existsSync(dirPath)) {
    return [];
  }
  /** @type {string[]} */
  const formattedFiles = [];

  /**
   * @param {string} currentDir
   * @param {string} relativePath
   */
  function visit(currentDir, relativePath) {
    const entries = readDirectory(currentDir);
    for (const entry of entries) {
      const fullPath = path.join(currentDir, entry.name);
      const relativeFilePath = relativePath ? path.join(relativePath, entry.name) : entry.name;
      if (entry.isDirectory()) {
        if (entry.name !== ".git") {
          visit(fullPath, relativeFilePath);
        }
        continue;
      }
      if (!entry.isFile() || !entry.name.endsWith(".json") || !isEligibleFile(relativeFilePath.replace(/\\/g, "/"))) {
        continue;
      }
      const raw = readFile(fullPath, "utf8");
      if (!raw.trim()) {
        continue;
      }
      let parsed;
      try {
        parsed = JSON.parse(raw);
      } catch (_error) {
        continue;
      }
      const formatted = JSON.stringify(parsed, null, 2) + "\n";
      if (raw === formatted) {
        continue;
      }
      const formattedSize = Buffer.byteLength(formatted, "utf8");
      if (formattedSize > maxFileSize) {
        throw new Error(`Formatted JSON exceeds max file size: ${path.relative(dirPath, fullPath)} (${formattedSize} bytes > ${maxFileSize} bytes)`);
      }
      writeFile(fullPath, formatted, "utf8");
      formattedFiles.push(path.relative(dirPath, fullPath).replace(/\\/g, "/"));
    }
  }

  visit(dirPath, "");
  return formattedFiles;
}

/**
 * @param {Record<string, string | undefined>} sourceEnv
 */
function sanitizedValidationEnv(sourceEnv) {
  const keep = ["PATH", "HOME", "TMPDIR", "TEMP", "TMP", "RUNNER_TEMP", "GITHUB_WORKSPACE", "CI"];
  /** @type {Record<string, string>} */
  const env = {};
  for (const key of keep) {
    const value = sourceEnv[key];
    if (value !== undefined) {
      env[key] = value;
    }
  }
  return env;
}

/**
 * @param {string} dirPath
 */
function memoryTreeDigest(dirPath) {
  const hash = crypto.createHash("sha256");

  /**
   * @param {string} currentDir
   */
  function visit(currentDir) {
    const entries = readDirectory(currentDir).sort((a, b) => a.name.localeCompare(b.name));
    for (const entry of entries) {
      const fullPath = path.join(currentDir, entry.name);
      const relativePath = path.relative(dirPath, fullPath).replace(/\\/g, "/");
      if (entry.isDirectory()) {
        if (relativePath === ".git") continue;
        hash.update(`directory\0${relativePath}\0`);
        visit(fullPath);
      } else if (entry.isFile()) {
        hash.update(`file\0${relativePath}\0`);
        hash.update(readFile(fullPath));
      } else if (entry.isSymbolicLink()) {
        hash.update(`symlink\0${relativePath}\0${fs.readlinkSync(fullPath)}\0`);
      } else {
        hash.update(`other\0${relativePath}\0`);
      }
    }
  }

  visit(dirPath);
  return hash.digest("hex");
}

/**
 * @param {string} file
 */
function validateSchemaFilePath(file) {
  if (
    typeof file !== "string" ||
    file === "" ||
    file.includes("\\") ||
    /[?[\]]/.test(file) ||
    path.posix.isAbsolute(file) ||
    path.win32.isAbsolute(file) ||
    /^[A-Za-z]:/.test(file) ||
    path.posix.normalize(file) !== file ||
    file.split("/").some(segment => segment === "" || segment === "." || segment === "..")
  ) {
    throw new TypeError("file must be a non-empty relative path or glob without traversal");
  }
}

/**
 * @param {string} file
 * @param {string} pattern
 */
function memorySchemaFileMatchesGlob(file, pattern) {
  let expression = "^";
  for (let index = 0; index < pattern.length;) {
    if (pattern.startsWith("**/", index)) {
      expression += "(?:.*/)?";
      index += 3;
    } else if (pattern.startsWith("**", index)) {
      expression += ".*";
      index += 2;
    } else if (pattern[index] === "*") {
      expression += "[^/]*";
      index++;
    } else {
      expression += pattern[index].replace(/[|\\{}()[\]^$+?.]/g, "\\$&");
      index++;
    }
  }
  return new RegExp(expression + "$").test(file);
}

/**
 * @param {string} memoryDir
 * @param {string} pattern
 * @returns {string[]}
 */
function findMemorySchemaFiles(memoryDir, pattern) {
  /** @type {string[]} */
  const matches = [];
  /**
   * @param {string} directory
   * @param {string} relativeDirectory
   */
  function visit(directory, relativeDirectory) {
    let entries;
    try {
      entries = fs.readdirSync(directory, { withFileTypes: true });
    } catch (error) {
      throw new Error(`Unable to search for schema files in '${relativeDirectory || "."}': ${getErrorMessage(error)}`, { cause: error });
    }
    for (const entry of entries) {
      const relativePath = relativeDirectory ? `${relativeDirectory}/${entry.name}` : entry.name;
      const fullPath = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        visit(fullPath, relativePath);
      } else if ((entry.isFile() || entry.isSymbolicLink()) && memorySchemaFileMatchesGlob(relativePath, pattern)) {
        matches.push(relativePath);
      }
    }
  }
  visit(memoryDir, "");
  return matches.sort();
}

/**
 * @param {string} text
 * @returns {Map<string, string>}
 */
function collectRawJSONNumbers(text) {
  const tokens = new Map();
  const numberPattern = /-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/y;
  let index = 0;
  const skipWhitespace = () => {
    while (/\s/.test(text[index] || "")) index++;
  };
  const parseString = () => {
    const start = index++;
    let escaped = false;
    while (index < text.length) {
      const character = text[index++];
      if (escaped) escaped = false;
      else if (character === "\\") escaped = true;
      else if (character === '"') return JSON.parse(text.slice(start, index));
    }
    return "";
  };
  /** @param {(string | number)[]} path */
  const parseValue = path => {
    skipWhitespace();
    const character = text[index];
    if (character === "{") {
      index++;
      skipWhitespace();
      while (text[index] !== "}") {
        skipWhitespace();
        const key = parseString();
        skipWhitespace();
        index++;
        parseValue([...path, key]);
        skipWhitespace();
        if (text[index] !== ",") break;
        index++;
      }
      index++;
    } else if (character === "[") {
      index++;
      skipWhitespace();
      let itemIndex = 0;
      while (text[index] !== "]") {
        parseValue([...path, itemIndex++]);
        skipWhitespace();
        if (text[index] !== ",") break;
        index++;
      }
      index++;
    } else if (character === '"') {
      parseString();
    } else if (character === "t" || character === "f" || character === "n") {
      index += character === "t" ? 4 : character === "f" ? 5 : 4;
    } else {
      numberPattern.lastIndex = index;
      const match = numberPattern.exec(text);
      if (!match) return;
      tokens.set(JSON.stringify(path), match[0]);
      index = numberPattern.lastIndex;
    }
  };
  parseValue([]);
  return tokens;
}

/**
 * Reject numeric tokens that JavaScript would round before numeric enum comparison.
 * @param {string} token
 */
function assertJSONNumberExactlyRepresentable(token) {
  if (token.length > 256) {
    throw new TypeError("contains a JSON number that cannot be represented exactly for numeric enum validation");
  }
  const number = Number(token);
  if (!Number.isFinite(number) || canonicalDecimal(token) !== canonicalDecimal(String(number))) {
    throw new TypeError("contains a JSON number that cannot be represented exactly for numeric enum validation");
  }
}

/**
 * @param {string} text
 * @param {any} value
 * @param {Record<string, any>} schema
 */
function assertSchemaNumericEnumValuesExactlyRepresentable(text, value, schema) {
  const tokens = collectRawJSONNumbers(text);
  /**
   * @param {any} currentValue
   * @param {Record<string, any>} currentSchema
   * @param {(string | number)[]} currentPath
   */
  const visit = (currentValue, currentSchema, currentPath) => {
    if (Array.isArray(currentSchema.enum) && currentSchema.enum.some(item => typeof item === "number") && typeof currentValue === "number") {
      const token = tokens.get(JSON.stringify(currentPath));
      if (token !== undefined) assertJSONNumberExactlyRepresentable(token);
    }
    if (currentValue && typeof currentValue === "object" && !Array.isArray(currentValue) && currentSchema.properties && typeof currentSchema.properties === "object" && !Array.isArray(currentSchema.properties)) {
      for (const [key, childSchema] of Object.entries(currentSchema.properties)) {
        if (Object.hasOwn(currentValue, key) && childSchema && typeof childSchema === "object") {
          visit(currentValue[key], childSchema, [...currentPath, key]);
        }
      }
    }
    if (Array.isArray(currentValue) && currentSchema.items && typeof currentSchema.items === "object") {
      currentValue.forEach((item, itemIndex) => visit(item, currentSchema.items, [...currentPath, itemIndex]));
    }
    for (const keyword of ["oneOf", "anyOf"]) {
      const alternatives = currentSchema[keyword];
      if (Array.isArray(alternatives)) {
        for (const alternative of alternatives) {
          if (alternative && typeof alternative === "object") visit(currentValue, alternative, currentPath);
        }
      }
    }
  };
  visit(value, schema, []);
}

/**
 * @param {Buffer} bytes
 * @returns {string}
 */
function decodeUTF8(bytes) {
  const text = bytes.toString("utf8");
  if (!Buffer.from(text, "utf8").equals(bytes)) {
    throw new TypeError("contains invalid UTF-8");
  }
  return text;
}

/**
 * @param {string} token
 * @returns {string}
 */
function canonicalDecimal(token) {
  const match = /^(-?)(\d+)(?:\.(\d+))?(?:[eE]([+-]?\d+))?$/.exec(token);
  if (!match) return token;
  const sign = match[1];
  let digits = `${match[2]}${match[3] || ""}`.replace(/^0+/, "");
  if (!digits) return "0";
  const trailingZeros = digits.length - digits.replace(/0+$/, "").length;
  digits = digits.slice(0, digits.length - trailingZeros);
  const exponent = BigInt(match[4] || "0") - BigInt((match[3] || "").length) + BigInt(trailingZeros);
  return `${sign}${digits}e${exponent}`;
}

/**
 * @param {string} filePath
 * @param {string} file
 * @param {Record<string, any>} schema
 */
function validateJSONLFile(filePath, file, schema) {
  let descriptor;
  try {
    descriptor = fs.openSync(filePath, "r");
  } catch (error) {
    throw new Error(`file '${file}' is unreadable: ${getErrorMessage(error)}`, { cause: error });
  }
  /** @type {Buffer[]} */
  let recordParts = [];
  let recordBytes = 0;
  let line = 1;
  const validateRecord = () => {
    let record = Buffer.concat(recordParts, recordBytes);
    if (record.length > 0 && record[record.length - 1] === 0x0d) record = record.subarray(0, record.length - 1);
    let text;
    try {
      text = decodeUTF8(record);
    } catch (error) {
      throw new Error(`file '${file}' line ${line} contains invalid UTF-8`, { cause: error });
    }
    if (!text.trim()) throw new Error(`file '${file}' line ${line} is a blank JSONL record`);
    let value;
    try {
      value = JSON.parse(text);
    } catch (error) {
      throw new Error(`file '${file}' line ${line} contains malformed JSON: ${getErrorMessage(error)}`, { cause: error });
    }
    try {
      assertSchemaNumericEnumValuesExactlyRepresentable(text, value, schema);
    } catch (error) {
      throw new Error(`file '${file}' line ${line} ${getErrorMessage(error)}`, { cause: error });
    }
    const validationError = validateValueAgainstSchema(value, schema);
    if (validationError) {
      throw new Error(`file '${file}' line ${line} at ${validationError.path || "(root)"} ${validationError.message}`);
    }
    recordParts = [];
    recordBytes = 0;
    line++;
  };
  try {
    while (true) {
      const chunk = Buffer.allocUnsafe(64 * 1024);
      const bytesRead = fs.readSync(descriptor, chunk, 0, chunk.length, null);
      if (bytesRead === 0) break;
      let start = 0;
      for (let index = 0; index < bytesRead; index++) {
        if (chunk[index] !== 0x0a) continue;
        if (index > start) {
          const part = chunk.subarray(start, index);
          recordParts.push(part);
          recordBytes += part.length;
        }
        validateRecord();
        start = index + 1;
      }
      if (start < bytesRead) {
        const part = chunk.subarray(start, bytesRead);
        recordParts.push(part);
        recordBytes += part.length;
      }
    }
    if (recordBytes > 0) validateRecord();
  } finally {
    fs.closeSync(descriptor);
  }
}

/**
 * @param {string} memoryDir
 * @param {Array<{file: string, format?: string, schema: Record<string, any>}>} schemas
 * @param {string} kind
 * @param {string} memoryId
 */
function validateMemoryJSONSchemas(memoryDir, schemas, kind, memoryId) {
  if (!Array.isArray(schemas) || schemas.length === 0) {
    throw new TypeError("json-schemas must be a non-empty list");
  }
  const seen = new Set();
  for (const declaration of schemas) {
    if (!declaration || typeof declaration !== "object" || Array.isArray(declaration)) {
      throw new TypeError("json-schemas entries must be objects");
    }
    const keys = Object.keys(declaration);
    if (keys.some(key => !["file", "format", "schema"].includes(key))) {
      throw new TypeError(`json-schemas entry for '${String(declaration.file)}' has unknown fields`);
    }
    validateSchemaFilePath(declaration.file);
    if (seen.has(declaration.file)) {
      throw new TypeError(`json-schemas contains duplicate file target '${declaration.file}'`);
    }
    seen.add(declaration.file);
    if (Object.hasOwn(declaration, "format") && declaration.format !== "json" && declaration.format !== "jsonl") {
      throw new TypeError(`file '${declaration.file}' has unsupported format`);
    }
    validateSchemaContract(declaration.schema, "Memory");
    const matchedFiles = declaration.file.includes("*") ? findMemorySchemaFiles(memoryDir, declaration.file) : [declaration.file];
    if (matchedFiles.length === 0) {
      throw new Error(`file pattern '${declaration.file}' matched no files`);
    }
    for (const file of matchedFiles) {
      validateMemoryJSONSchemaFile(memoryDir, file, declaration);
    }
  }
  return `Declarative ${kind}-memory schemas passed for '${memoryId}'`;
}

/**
 * @param {string} memoryDir
 * @param {string} file
 * @param {{file: string, format?: string, schema: Record<string, any>}} declaration
 */
function validateMemoryJSONSchemaFile(memoryDir, file, declaration) {
  const format = declaration.format ?? path.extname(file).slice(1).toLowerCase();
  if (format !== "json" && format !== "jsonl") {
    throw new TypeError(`file '${file}' has an unsupported extension; set format to json or jsonl`);
  }
  const relativeFile = file.split("/").join(path.sep);
  const lexicalPath = path.resolve(memoryDir, relativeFile);
  const lexicalRelative = path.relative(path.resolve(memoryDir), lexicalPath);
  if (lexicalRelative === ".." || lexicalRelative.startsWith(`..${path.sep}`) || path.isAbsolute(lexicalRelative)) {
    throw new TypeError(`file '${file}' escapes the memory directory`);
  }
  let root;
  let resolvedFile;
  try {
    root = fs.realpathSync(memoryDir);
    resolvedFile = fs.realpathSync(lexicalPath);
  } catch (error) {
    throw new Error(`file '${file}' is missing or unreadable: ${getErrorMessage(error)}`, { cause: error });
  }
  const relativeResolved = path.relative(root, resolvedFile);
  if (relativeResolved === ".." || relativeResolved.startsWith(`..${path.sep}`) || path.isAbsolute(relativeResolved)) {
    throw new Error(`file '${file}' resolves outside the memory directory`);
  }
  let stats;
  try {
    stats = fs.statSync(resolvedFile);
  } catch (error) {
    throw new Error(`file '${file}' is unreadable: ${getErrorMessage(error)}`, { cause: error });
  }
  if (!stats.isFile()) {
    throw new Error(`file '${file}' must be a regular file`);
  }
  if (stats.size > MAX_SCHEMA_FILE_BYTES) {
    throw new Error(`file '${file}' exceeds the schema validation size limit`);
  }
  if (format === "jsonl") {
    validateJSONLFile(resolvedFile, file, declaration.schema);
    return;
  }
  let contents;
  try {
    contents = decodeUTF8(fs.readFileSync(resolvedFile));
  } catch (error) {
    if (error instanceof TypeError && error.message.includes("invalid UTF-8")) {
      throw new Error(`file '${file}' contains invalid UTF-8`, { cause: error });
    }
    throw new Error(`file '${file}' is unreadable: ${getErrorMessage(error)}`, { cause: error });
  }
  if (format === "json") {
    if (!contents.trim()) throw new Error(`file '${file}' contains empty JSON`);
    let value;
    try {
      value = JSON.parse(contents);
    } catch (error) {
      throw new Error(`file '${file}' contains malformed JSON: ${getErrorMessage(error)}`, { cause: error });
    }
    try {
      assertSchemaNumericEnumValuesExactlyRepresentable(contents, value, declaration.schema);
    } catch (error) {
      throw new Error(`file '${file}' ${getErrorMessage(error)}`, { cause: error });
    }
    const validationError = validateValueAgainstSchema(value, declaration.schema);
    if (validationError) {
      throw new Error(`file '${file}' at ${validationError.path || "(root)"} ${validationError.message}`);
    }
    return;
  }
}

/**
 * @param {string} memoryDir
 * @param {Array<{file: string, format?: string, schema: Record<string, any>}>} schemas
 * @param {string} kind
 * @param {string} memoryId
 * @param {number} timeoutMs
 */
function runDeclarativeMemoryValidation(memoryDir, schemas, kind, memoryId, timeoutMs) {
  const worker = `const fs = require("fs");
const { validateMemoryJSONSchemas } = require(${JSON.stringify(__filename)});
try {
  const input = JSON.parse(fs.readFileSync(0, "utf8"));
  process.stdout.write(validateMemoryJSONSchemas(input.memoryDir, input.schemas, input.kind, input.memoryId));
} catch (error) {
  console.error(error && error.message ? error.message : String(error));
  process.exitCode = 1;
}`;
  let result;
  try {
    result = childProcess.spawnSync(process.execPath, ["-e", worker], {
      input: JSON.stringify({ memoryDir, schemas, kind, memoryId }),
      encoding: "utf8",
      env: sanitizedValidationEnv(process.env),
      timeout: timeoutMs,
      maxBuffer: MAX_VALIDATION_OUTPUT_BYTES * 2,
      windowsHide: true,
    });
  } catch (error) {
    return {
      ok: false,
      exitCode: null,
      timedOut: false,
      stdout: "",
      stderr: getErrorMessage(error),
    };
  }
  const timedOut = result.error && Reflect.get(result.error, "code") === "ETIMEDOUT";
  return {
    ok: result.status === 0 && !result.error,
    exitCode: result.status,
    timedOut,
    stdout: boundedOutput(result.stdout),
    stderr: timedOut ? `Declarative schema validation timed out after ${Math.ceil(timeoutMs / 1000)} second(s)` : boundedOutput(result.stderr || (result.error ? getErrorMessage(result.error) : "")),
  };
}

/**
 * @param {{
 *   script?: string,
 *   scriptBase64?: string,
 *   jsonSchemas?: Array<{file: string, format?: string, schema: Record<string, any>}>,
 *   requireJSONSchemas?: boolean,
 *   memoryDir: string,
 *   memoryId?: string,
 *   kind: "repo" | "cache" | "drive",
 *   timeoutSeconds?: number,
 *   isEligibleFile?: (relativePath: string) => boolean,
 * }} options
 */
function runCustomMemoryValidation(options) {
  let script = options.script || "";
  if (!script && options.scriptBase64) {
    script = Buffer.from(options.scriptBase64, "base64").toString("utf8");
  }
  const jsonSchemas = options.jsonSchemas;
  if (options.requireJSONSchemas && (!Array.isArray(jsonSchemas) || jsonSchemas.length === 0)) {
    return {
      ok: false,
      exitCode: 1,
      timedOut: false,
      stdout: "",
      stderr: "json-schemas configuration is missing or empty",
    };
  }
  if (!script.trim() && (!Array.isArray(jsonSchemas) || jsonSchemas.length === 0)) {
    return {
      ok: false,
      exitCode: null,
      timedOut: false,
      stdout: "",
      stderr: "validation.script is configured but empty or missing",
    };
  }

  const rawTimeoutSeconds = options.timeoutSeconds;
  const timeoutSeconds = typeof rawTimeoutSeconds === "number" && Number.isFinite(rawTimeoutSeconds) && rawTimeoutSeconds > 0 ? Math.max(1, Math.floor(rawTimeoutSeconds)) : DEFAULT_VALIDATION_TIMEOUT_SECONDS;
  const timeoutMs = timeoutSeconds * 1000;
  const deadline = Date.now() + timeoutMs;
  const memoryId = options.memoryId || "default";
  if (Array.isArray(jsonSchemas) && jsonSchemas.length > 0) {
    const schemaResult = runDeclarativeMemoryValidation(options.memoryDir, jsonSchemas, options.kind, memoryId, timeoutMs);
    if (!schemaResult.ok) {
      return {
        ok: false,
        exitCode: schemaResult.exitCode,
        timedOut: schemaResult.timedOut,
        stdout: "",
        stderr: `Declarative ${options.kind}-memory schema validation failed for '${memoryId}': ${schemaResult.stderr}`,
      };
    }
    if (!script.trim()) {
      return schemaResult;
    }
  }

  const remainingTimeoutMs = deadline - Date.now();
  if (remainingTimeoutMs <= 0) {
    return {
      ok: false,
      exitCode: null,
      timedOut: true,
      stdout: "",
      stderr: `Memory validation timed out after ${timeoutSeconds} second(s)`,
    };
  }
  const validationDir = makeTempDirectory();
  const validationMemoryDir = path.join(validationDir, "memory");
  const scriptPath = path.join(validationDir, "validator.cjs");
  try {
    fs.cpSync(options.memoryDir, validationMemoryDir, {
      recursive: true,
      dereference: true,
      filter: sourcePath => {
        const relativePath = path.relative(options.memoryDir, sourcePath);
        if (relativePath === "") return true;
        if (relativePath.split(path.sep)[0] === ".git") return false;
        return !options.isEligibleFile || fs.statSync(sourcePath).isDirectory() || options.isEligibleFile(relativePath.replace(/\\/g, "/"));
      },
    });
    pruneEmptyDirectories(validationMemoryDir);
  } catch (error) {
    removePath(validationDir, { recursive: true, force: true });
    return {
      ok: false,
      exitCode: null,
      timedOut: false,
      stdout: "",
      stderr: `Unable to prepare memory for custom validation: ${getErrorMessage(error)}`,
    };
  }

  let beforeDigest;
  try {
    beforeDigest = memoryTreeDigest(validationMemoryDir);
  } catch (error) {
    removePath(validationDir, { recursive: true, force: true });
    return {
      ok: false,
      exitCode: null,
      timedOut: false,
      stdout: "",
      stderr: `Unable to snapshot memory before custom validation: ${getErrorMessage(error)}`,
    };
  }

  const wrapper = `"use strict";
const fs = require("fs");
const path = require("path");
const memoryRoot = ${JSON.stringify(validationMemoryDir)};
const memoryDir = memoryRoot;
const memoryId = ${JSON.stringify(memoryId)};
const memoryKind = ${JSON.stringify(options.kind)};
process.env.GH_AW_MEMORY_ROOT = memoryRoot;
process.env.GH_AW_MEMORY_DIR = memoryRoot;
process.env.GH_AW_MEMORY_ID = memoryId;
process.env.GH_AW_MEMORY_KIND = memoryKind;
(async () => {
  return await (async () => {
${script}
  })();
})()
  .then(result => {
    if (result === false) {
      console.error("validation.script returned false");
      process.exit(1);
    }
  })
  .catch(error => {
    console.error(error && error.stack ? error.stack : String(error));
    process.exit(1);
  });
`;
  try {
    writeFile(scriptPath, wrapper, { encoding: "utf8", mode: 0o600 });
    const result = childProcess.spawnSync(process.execPath, [scriptPath], {
      cwd: validationMemoryDir,
      encoding: "utf8",
      env: sanitizedValidationEnv(process.env),
      timeout: remainingTimeoutMs,
      maxBuffer: MAX_VALIDATION_OUTPUT_BYTES * 2,
      windowsHide: true,
    });
    const spawnErrorCode = result.error ? Reflect.get(result.error, "code") : undefined;
    let memoryChanged = false;
    let snapshotError = "";
    try {
      memoryChanged = beforeDigest !== memoryTreeDigest(validationMemoryDir);
    } catch (error) {
      snapshotError = `Unable to snapshot memory after custom validation: ${getErrorMessage(error)}`;
    }
    const stderr = boundedOutput(result.stderr || (result.error ? getErrorMessage(result.error) : ""));
    const validationError = memoryChanged ? "Custom validation must not modify memory files" : snapshotError;
    return {
      ok: result.status === 0 && !result.error && !memoryChanged && !snapshotError,
      exitCode: result.status,
      timedOut: spawnErrorCode === "ETIMEDOUT",
      stdout: boundedOutput(result.stdout),
      stderr: validationError ? boundedOutput(`${stderr}${stderr ? "\n" : ""}${validationError}`) : stderr,
    };
  } finally {
    removePath(validationDir, { recursive: true, force: true });
  }
}

module.exports = {
  DEFAULT_VALIDATION_TIMEOUT_SECONDS,
  clearValidationMarker,
  formatJSONFiles,
  getRepoMemoryBaselinePath,
  getValidationMarkerPath,
  memoryTreeDigest,
  validateMemoryJSONSchemas,
  validateSchemaFilePath,
  runCustomMemoryValidation,
  writeValidationMarker,
};
