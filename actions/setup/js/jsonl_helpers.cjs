// @ts-check

const fs = require("node:fs");
const { StringDecoder } = require("node:string_decoder");
const { prepareJsonSchema } = require("./json_schema_validation.cjs");

/**
 * @typedef {object} JsonlOptions
 * @property {(line: string, lineNumber: number) => boolean} [lineFilter] Receives trimmed text before transformation and decoding.
 * @property {(line: string, lineNumber: number) => string} [lineTransform] Transforms trimmed record text after filtering, retaining the original rawLine.
 * @property {(value: any, lineNumber: number) => boolean} [valueFilter] Selects decoded, schema-validated values without skipping invalid records.
 * @property {(error: Error, lineNumber: number) => void} [onError] Reports rejected nonblank lines without exposing their contents.
 * @property {number} [maxLineLength] Optional limit in UTF-16 code units; unlimited by default.
 * @property {{field: string, value: string|number|boolean|null}} [fieldFilter] Match an own top-level field, prefiltering text before decoding.
 * @property {Record<string, any>} [schema] Optional bounded JSON schema, validated once per input.
 * @property {boolean} [fatalUTF8] Report malformed byte sequences as invalid events instead of replacing them.
 * @property {boolean} [jsonWhitespaceOnly] Permit only JSON whitespace around records instead of String.trim() whitespace.
 */

/**
 * @typedef {JsonlOptions & {strict?: boolean}} JsonlValueOptions
 * @typedef {JsonlOptions & {maxPendingRecordLength?: number}} JsonlStreamOptions
 * @typedef {{type: "json", value: any, rawLine: string, lineNumber: number} |
 * {type: "invalid", error: Error, reason: "blank"|"syntax"|"too-long"|"schema"|"encoding", validationError?: {path: string, message: string}, rawLine: string, lineNumber: number}} JsonlEvent
 */

/** @param {string} line */
function isJsonCandidate(line) {
  if (line[0] === "{") return /^\{\s*["}]/.test(line);
  if (line[0] === "[") return /^\[\s*(?:[{\["\d\]-]|(?:true|false|null)(?:[\s,\]]|$))/.test(line);
  return /^(?:["\d-]|(?:true|false|null)$)/.test(line);
}

/**
 * Lazily split existing text without allocating an array of every line.
 * LF and CRLF are supported; a final unterminated line is included.
 * @param {string} content
 * @param {boolean} [includeNewline] Preserve original line endings in each yielded line.
 */
function* iterateLines(content, includeNewline = false) {
  let start = 0;
  while (start < content.length) {
    const end = content.indexOf("\n", start);
    if (end === -1) {
      yield includeNewline ? content.slice(start) : content.slice(start).replace(/\r$/, "");
      return;
    }
    yield includeNewline ? content.slice(start, end + 1) : content.slice(start, end).replace(/\r$/, "");
    start = end + 1;
  }
  if (!includeNewline) yield "";
}

/**
 * Chunk framing retains only the current line, joining fragments once rather than
 * repeatedly copying a growing multi-megabyte string.
 */
class LineFramer {
  /** @param {boolean} [includeNewline] @param {number} [maxPendingRecordLength] */
  constructor(includeNewline = false, maxPendingRecordLength) {
    this.includeNewline = includeNewline;
    this.maxPendingRecordLength = maxPendingRecordLength;
    this.pendingLength = 0;
    /** @type {RangeError|undefined} */
    this.error = undefined;
    /** @type {string[]} */
    this.fragments = [];
  }

  /** @param {number} length */
  #checkLength(length) {
    if (this.error) throw this.error;
    if (this.maxPendingRecordLength !== undefined && length > this.maxPendingRecordLength) {
      this.fragments = [];
      this.pendingLength = 0;
      this.error = new RangeError("JSONL record exceeds maxPendingRecordLength");
      throw this.error;
    }
  }

  /** @param {string} chunk */
  *push(chunk) {
    if (this.error) throw this.error;
    let start = 0;
    let end;
    while ((end = chunk.indexOf("\n", start)) !== -1) {
      this.#checkLength(this.pendingLength + end + 1 - start);
      const part = chunk.slice(start, this.includeNewline ? end + 1 : end);
      const line = this.fragments.length ? this.fragments.join("") + part : part;
      this.fragments = [];
      this.pendingLength = 0;
      start = end + 1;
      yield this.includeNewline ? line : line.replace(/\r$/, "");
    }
    if (start < chunk.length) {
      this.#checkLength(this.pendingLength + chunk.length - start);
      this.fragments.push(chunk.slice(start));
      this.pendingLength += chunk.length - start;
    }
  }

  *finish() {
    if (this.error) throw this.error;
    if (this.fragments.length) {
      const line = this.fragments.join("");
      this.fragments = [];
      this.pendingLength = 0;
      yield this.includeNewline ? line : line.replace(/\r$/, "");
    }
  }
}

/**
 * Synchronous, bounded-read file iterator. Opens on first next(), closes on
 * completion, errors, and early return (including a consumer's break).
 * @param {string} filePath
 */
function* readFileChunks(filePath) {
  const fd = fs.openSync(filePath, "r");
  try {
    const buffer = Buffer.allocUnsafe(64 * 1024);
    let count;
    while ((count = fs.readSync(fd, buffer, 0, buffer.length, null)) !== 0) {
      yield buffer.subarray(0, count);
    }
  } finally {
    fs.closeSync(fd);
  }
}

/** @param {string} filePath @param {boolean} [includeNewline] */
function* readLines(filePath, includeNewline = false) {
  const decoder = new StringDecoder("utf8");
  const framer = new LineFramer(includeNewline);
  for (const chunk of readFileChunks(filePath)) yield* framer.push(decoder.write(chunk));
  yield* framer.push(decoder.end());
  yield* framer.finish();
}

/**
 * Lazily frame byte or UTF-8 text chunks with backpressure. Byte chunks may split
 * UTF-8 characters. Closing the iterator also closes the source iterator.
 * @param {AsyncIterable<Buffer|string>|Iterable<Buffer|string>} source
 * @param {boolean} [includeNewline]
 */
async function* streamLines(source, includeNewline = false) {
  const decoder = new StringDecoder("utf8");
  const framer = new LineFramer(includeNewline);
  for await (const chunk of source) {
    // Flush pending bytes before switching to already-decoded text.
    yield* framer.push(typeof chunk === "string" ? decoder.end() + chunk : decoder.write(chunk));
  }
  yield* framer.push(decoder.end());
  yield* framer.finish();
}

/** @param {JsonlOptions} options */
function validateOptions(options) {
  if (options.maxLineLength !== undefined && (!Number.isSafeInteger(options.maxLineLength) || options.maxLineLength < 1)) {
    throw new RangeError("maxLineLength must be a positive safe integer");
  }
}

/** @param {JsonlOptions} options */
function prepareFieldFilter(options) {
  const filter = options.fieldFilter;
  if (filter === undefined) return;
  if (!filter || typeof filter.field !== "string" || !(filter.value === null || typeof filter.value === "string" || typeof filter.value === "boolean" || (typeof filter.value === "number" && Number.isFinite(filter.value)))) {
    throw new TypeError("fieldFilter requires a string field and a finite JSON primitive value");
  }
  const fieldToken = JSON.stringify(filter.field);
  const valueToken = typeof filter.value === "number" ? undefined : JSON.stringify(filter.value);
  return { ...filter, fieldToken, valueToken, escapedSearch: fieldToken.includes("\\") || valueToken?.includes("\\") === true };
}

/**
 * Escapes can spell keys/values differently, and numbers have alternate exponent
 * forms. Only reject text whose absence proves that the decoded field cannot match.
 * @param {string} text
 * @param {ReturnType<typeof prepareFieldFilter>} filter
 */
function mayMatchField(text, filter) {
  return !filter || filter.escapedSearch || text.includes("\\") || (text.includes(filter.fieldToken) && (filter.valueToken === undefined || text.includes(filter.valueToken)));
}

/**
 * @param {string} rawLine
 * @param {number} lineNumber
 * @param {"blank"|"syntax"|"too-long"|"schema"|"encoding"} reason
 * @param {JsonlOptions} options
 * @param {{path: string, message: string}} [validationError]
 * @returns {JsonlEvent}
 */
function invalidEvent(rawLine, lineNumber, reason, options, validationError) {
  /** @type {Error|undefined} */
  let error;
  /** @type {JsonlEvent} */
  const event = {
    type: "invalid",
    rawLine,
    lineNumber,
    reason,
    ...(validationError ? { validationError } : {}),
    // Plain-text logs must not pay for an error stack unless it is consumed.
    get error() {
      return (error ??=
        reason === "too-long"
          ? new RangeError("JSONL record exceeds maxLineLength")
          : reason === "schema"
            ? new TypeError("JSONL record does not match schema")
            : new SyntaxError(reason === "blank" ? "Blank JSONL record" : reason === "encoding" ? "JSONL record contains invalid UTF-8" : "Invalid JSONL record"));
    },
  };
  if (reason !== "blank" && options.onError) options.onError(event.error, lineNumber);
  return event;
}

/**
 * @param {string} rawLine
 * @param {number} lineNumber
 * @param {JsonlOptions} options
 * @param {ReturnType<typeof prepareFieldFilter>} fieldFilter
 * @param {ReturnType<typeof prepareJsonSchema>|undefined} schemaValidator
 * @returns {JsonlEvent|undefined}
 */
function parseEvent(rawLine, lineNumber, options, fieldFilter, schemaValidator) {
  let line = options.jsonWhitespaceOnly ? rawLine.replace(/^[ \t\r\n]+|[ \t\r\n]+$/g, "") : rawLine.trim();
  if (!line || (options.jsonWhitespaceOnly && !line.trim())) {
    return invalidEvent(rawLine, lineNumber, "blank", options);
  }
  if (options.lineFilter && !options.lineFilter(line, lineNumber)) return;
  if (options.lineTransform) {
    line = options.lineTransform(line, lineNumber);
    line = options.jsonWhitespaceOnly ? line.replace(/^[ \t\r\n]+|[ \t\r\n]+$/g, "") : line.trim();
  }
  if (!mayMatchField(line, fieldFilter)) return;
  const length = rawLine.endsWith("\r\n") ? rawLine.length - 2 : rawLine.endsWith("\n") ? rawLine.length - 1 : rawLine.length;
  const tooLong = options.maxLineLength !== undefined && length > options.maxLineLength;
  if (!tooLong && isJsonCandidate(line)) {
    let value;
    try {
      value = JSON.parse(line);
    } catch {
      // Report a sanitized error below, not JSON.parse's input-bearing message.
      return invalidEvent(rawLine, lineNumber, "syntax", options);
    }
    if (fieldFilter && (!value || typeof value !== "object" || Array.isArray(value) || !Object.hasOwn(value, fieldFilter.field) || value[fieldFilter.field] !== fieldFilter.value)) return;
    const validationError = schemaValidator?.(value, line);
    if (validationError) return invalidEvent(rawLine, lineNumber, "schema", options, validationError);
    if (options.valueFilter && !options.valueFilter(value, lineNumber)) return;
    return { type: "json", value, rawLine, lineNumber };
  }
  return invalidEvent(rawLine, lineNumber, tooLong ? "too-long" : "syntax", options);
}

/**
 * Incremental JSONL event parser for callback-driven byte/text streams. Keep one
 * instance per stream and consume each push iterator before the next one.
 * maxPendingRecordLength optionally bounds raw UTF-16 code units, including
 * LF/CRLF, before retaining/joining fragments. Exceeding it is a terminal
 * RangeError, not an invalid event. The default remains unlimited.
 */
class JsonlParser {
  /** @param {JsonlStreamOptions} [options] */
  constructor(options = {}) {
    validateOptions(options);
    if (options.maxPendingRecordLength !== undefined && (!Number.isSafeInteger(options.maxPendingRecordLength) || options.maxPendingRecordLength < 1)) {
      throw new RangeError("maxPendingRecordLength must be a positive safe integer");
    }
    this.options = options;
    this.fieldFilter = prepareFieldFilter(options);
    this.schemaValidator = options.schema === undefined ? undefined : prepareJsonSchema(options.schema);
    this.framer = new LineFramer(true, options.maxPendingRecordLength);
    this.decoder = new StringDecoder("utf8");
    this.utf8Validator = options.fatalUTF8 ? new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }) : undefined;
    this.invalidUTF8 = false;
    this.lineNumber = 0;
  }

  /** @param {Iterable<string>} lines @returns {Generator<JsonlEvent>} */
  *#events(lines) {
    for (const line of lines) {
      const event = this.invalidUTF8 ? invalidEvent(line, ++this.lineNumber, "encoding", this.options) : parseEvent(line, ++this.lineNumber, this.options, this.fieldFilter, this.schemaValidator);
      if (event) yield event;
    }
  }

  /** @param {Buffer|string} chunk @returns {Generator<JsonlEvent>} */
  *push(chunk) {
    if (this.utf8Validator) {
      if (typeof chunk === "string") {
        if (!this.invalidUTF8) {
          try {
            this.utf8Validator.decode();
          } catch {
            this.invalidUTF8 = true;
          }
        }
        this.utf8Validator = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
        for (const part of iterateLines(this.decoder.end() + chunk, true)) {
          yield* this.#events(this.framer.push(part));
          if (part.endsWith("\n")) this.invalidUTF8 = false;
        }
        return;
      }
      // Validate through each LF separately so failures retain physical positions.
      const bytes = chunk;
      let start = 0;
      while (start < bytes.length) {
        const newline = bytes.indexOf(0x0a, start);
        const end = newline === -1 ? bytes.length : newline + 1;
        const part = bytes.subarray(start, end);
        if (!this.invalidUTF8) {
          try {
            this.utf8Validator.decode(part, { stream: true });
          } catch {
            this.invalidUTF8 = true;
          }
        }
        yield* this.#events(this.framer.push(this.decoder.write(part)));
        if (newline !== -1) {
          if (this.invalidUTF8) this.utf8Validator = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
          this.invalidUTF8 = false;
        }
        start = end;
      }
      return;
    }
    const text = typeof chunk === "string" ? this.decoder.end() + chunk : this.decoder.write(chunk);
    yield* this.#events(this.framer.push(text));
  }

  /** @returns {Generator<JsonlEvent>} */
  *finish() {
    if (this.utf8Validator && !this.invalidUTF8) {
      try {
        this.utf8Validator.decode();
      } catch {
        this.invalidUTF8 = true;
      }
    }
    yield* this.#events(this.framer.push(this.decoder.end()));
    yield* this.#events(this.framer.finish());
  }
}

/**
 * Emit valid and invalid record events without dropping raw input. rawLine
 * includes its original LF/CRLF terminator when present. Blank lines are invalid
 * events with reason "blank", but do not invoke onError. Input text is untrusted:
 * redact rawLine before logging it. break/return stops parsing immediately.
 * @param {string|Iterable<string>} source Text or complete raw lines.
 * @param {JsonlOptions} [options]
 * @returns {Generator<JsonlEvent>}
 */
function* iterateJsonlEvents(source, options = {}) {
  validateOptions(options);
  const fieldFilter = prepareFieldFilter(options);
  const schemaValidator = options.schema === undefined ? undefined : prepareJsonSchema(options.schema);
  if (typeof source === "string" && !options.lineTransform && !mayMatchField(source, fieldFilter)) return;
  let lineNumber = 0;
  for (const rawLine of typeof source === "string" ? iterateLines(source, true) : source) {
    const event = parseEvent(rawLine, ++lineNumber, options, fieldFilter, schemaValidator);
    if (event) yield event;
  }
}

/**
 * Lazy file-backed record events, including malformed and blank lines.
 * The file closes on completion, errors, break, and return.
 * @param {string} filePath
 * @param {JsonlOptions} [options]
 * @returns {Generator<JsonlEvent>}
 */
function* readJsonlFileEvents(filePath, options = {}) {
  if (options.fatalUTF8) {
    const parser = new JsonlParser(options);
    for (const chunk of readFileChunks(filePath)) yield* parser.push(chunk);
    yield* parser.finish();
    return;
  }
  yield* iterateJsonlEvents(readLines(filePath, true), options);
}

/**
 * Async, backpressured record events over UTF-8 byte or text chunks.
 * @param {AsyncIterable<Buffer|string>|Iterable<Buffer|string>} source
 * @param {JsonlStreamOptions} [options]
 * @returns {AsyncGenerator<JsonlEvent>}
 */
async function* streamJsonlEvents(source, options = {}) {
  const parser = new JsonlParser(options);
  for await (const chunk of source) yield* parser.push(chunk);
  yield* parser.finish();
}

/**
 * Tolerant, lazy JSONL parser over text or an iterable of complete lines.
 * All JSON values (including null, false, and zero) are preserved. Corrupted
 * records are skipped; onError can count/report them. Filter/callback failures
 * and input I/O errors propagate, rather than being mistaken for corrupt JSON.
 * @param {string|Iterable<string>} source
 * @param {JsonlValueOptions} [options] strict throws on any invalid record, including blanks.
 * @returns {Generator<any>}
 */
function* iterateJsonl(source, options = {}) {
  for (const event of iterateJsonlEvents(source, options)) {
    if (event.type === "json") yield event.value;
    else if (options.strict) throw event.error;
  }
}

/**
 * Read massive files without loading them into memory. Memory is proportional
 * to the largest line, not the file. Results are not retained by the iterator.
 * @param {string} filePath
 * @param {JsonlValueOptions} [options]
 * @returns {Generator<any>}
 */
function* readJsonlFile(filePath, options = {}) {
  for (const event of readJsonlFileEvents(filePath, options)) {
    if (event.type === "json") yield event.value;
    else if (options.strict) throw event.error;
  }
}

/**
 * Async streaming JSONL parser for fs.createReadStream(), network responses,
 * and other chunk iterables. No reads occur until iteration starts.
 * @param {AsyncIterable<Buffer|string>|Iterable<Buffer|string>} source
 * @param {JsonlStreamOptions & {strict?: boolean}} [options]
 * @returns {AsyncGenerator<any>}
 */
async function* streamJsonl(source, options = {}) {
  for await (const event of streamJsonlEvents(source, options)) {
    if (event.type === "json") yield event.value;
    else if (options.strict) throw event.error;
  }
}

/**
 * Parse JSONL content into an array of entries.
 * Malformed lines are ignored so callers can safely consume partially written logs.
 *
 * @param {string} content
 * @param {(line: string) => boolean} [lineFilter] Optional predicate to pre-filter lines before JSON parsing.
 * @returns {unknown[]}
 */
function parseJsonlContent(content, lineFilter) {
  if (typeof content !== "string" || content.length === 0) {
    return [];
  }

  return Array.from(iterateJsonl(content, { lineFilter: typeof lineFilter === "function" ? lineFilter : undefined }));
}

module.exports = {
  parseJsonlContent,
  iterateLines,
  readLines,
  streamLines,
  iterateJsonl,
  readJsonlFile,
  streamJsonl,
  iterateJsonlEvents,
  readJsonlFileEvents,
  streamJsonlEvents,
  LineFramer,
  JsonlParser,
};
