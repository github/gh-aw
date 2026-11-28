// @ts-check
const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");

/**
 * Helpers that make rendered agent logs aware of GitHub Actions `::add-mask::`
 * workflow commands.
 *
 * The GitHub Actions runner masks values registered with `::add-mask::` in the
 * live job log, but raw log files captured as artifacts (e.g. agent-stdio.log)
 * still contain both the `::add-mask::` command lines and the unmasked values.
 * Rendering those raw lines into issues or comments would leak the secrets, so
 * every log excerpt copied into generated content must be redacted first.
 */

/** Matches an `::add-mask::` workflow command and captures its payload. */
const ADD_MASK_COMMAND_RE = /::add-mask::(.*)$/;

const { MASK_REPLACEMENT, redactMaskedValues, maskedValueMatches, overlappingMatchGroups } = require("./mask_value_matcher.cjs");
const { transformJSONStrings } = require("./json_string_scanner.cjs");

/**
 * Decode a GitHub Actions workflow command payload.
 *
 * @param {string} value
 * @returns {string}
 */
function unescapeWorkflowCommandValue(value) {
  return value.replace(/%0D/gi, "\r").replace(/%0A/gi, "\n").replace(/%25/g, "%");
}

/**
 * Check whether a log line is an `::add-mask::` workflow command.
 *
 * @param {string} line
 * @param {boolean} [sourceOnly] - Preserve variable references in known source text
 * @returns {boolean}
 */
function isAddMaskCommandLine(line, sourceOnly = false) {
  const match = line.match(ADD_MASK_COMMAND_RE);
  return !!match && !(sourceOnly && isUnexpandedMaskSource(line, match));
}

/**
 * @param {string} line
 * @param {RegExpMatchArray} match
 * @returns {boolean}
 */
function isUnexpandedMaskSource(line, match) {
  return /^\s*[+-]?\s*(?:run:\s*)?echo\s+["']?$/.test(line.slice(0, match.index)) && /^\$(?:\{[A-Za-z_][A-Za-z0-9_]*\}|[A-Za-z_][A-Za-z0-9_]*)["']?\s*$/.test(match[1]);
}

/**
 * Collect every value registered through `::add-mask::` in the given log content.
 *
 * Multi-line masked values are expanded into their individual lines as well, so
 * that partial occurrences are redacted too.
 *
 * @param {string} logContent
 * @param {{sourceOnly?: boolean}} [options] - Ignore variable references only for known source text
 * @returns {string[]} Unique masked values, longest first
 */
function collectAddMaskedValues(logContent, options = {}) {
  /** @type {Set<string>} */
  const values = new Set();
  if (!logContent) return [];
  for (const line of logContent.split("\n")) {
    const match = line.match(ADD_MASK_COMMAND_RE);
    if (!match || (options.sourceOnly && isUnexpandedMaskSource(line, match))) continue;
    const decoded = unescapeWorkflowCommandValue(match[1]);
    for (const candidate of decoded.split(/\r\n|\r|\n/)) {
      if (!candidate.trim()) continue;
      // Register both the verbatim value and its trimmed form so that surrounding
      // whitespace in either the command payload or the log text never defeats redaction.
      values.add(candidate);
      values.add(candidate.trim());
    }
  }
  // Both verbatim and trimmed forms are included; trimming cannot increase length,
  // so descending-length order always prefers verbatim before trimmed variants.
  return Array.from(values).sort((a, b) => b.length - a.length);
}

/**
 * Remove `::add-mask::` command lines and redact every masked value from the text.
 *
 * @param {string} text - Text about to be rendered into generated content
 * @param {string[]} maskedValues - Values collected via {@link collectAddMaskedValues}
 * @param {(value: string) => string} [replacementForValue]
 * @returns {string}
 */
function applyAddMaskRedaction(text, maskedValues, replacementForValue, preserveUnexpandedMaskSources = false) {
  if (!text) return text;
  const withoutCommands = text
    .split("\n")
    .filter(line => !isAddMaskCommandLine(line, preserveUnexpandedMaskSources))
    .join("\n");
  return redactMaskedValues(withoutCommands, maskedValues, replacementForValue);
}

/**
 * Decode ambiguous escapes for matching only, mapping matches back to source
 * spans so unrelated plain-text paths and literal escapes keep their bytes.
 * @param {string} text
 * @param {string[]} maskedValues
 * @param {(value: string) => string} replacementForValue
 * @returns {string}
 */
function redactAmbiguousMaskedValues(text, maskedValues, replacementForValue) {
  const escapes = { '"': '"', "\\": "\\", "/": "/", b: "\b", f: "\f", n: "\n", r: "\r", t: "\t" };
  const pattern = /\\(["\\/bfnrt]|u[0-9a-fA-F]{4})/g;
  const unescaped = text.replace(pattern, (_, escape) => {
    if (escape[0] === "u") return String.fromCharCode(parseInt(escape.slice(1), 16));
    return escapes[escape];
  });
  let nextEscape = pattern.exec(text);
  let offset = 0;
  const sourceOffset = position => {
    while (nextEscape && nextEscape.index - offset < position) {
      offset += nextEscape[0].length - 1;
      nextEscape = pattern.exec(text);
    }
    return position + offset;
  };
  const parts = [];
  let cursor = 0;
  for (const match of maskedValueMatches(unescaped, maskedValues)) {
    const start = sourceOffset(match.start);
    const end = sourceOffset(match.end);
    parts.push(text.slice(cursor, start), replacementForValue(match.value));
    cursor = end;
  }
  // Ambiguous records retain their JSON-string wrapper, but its decoded value
  // now changes only at the source spans that actually matched a mask.
  return cursor ? JSON.stringify(parts.join("") + text.slice(cursor)) : text;
}

/**
 * @param {string} encoded
 * @returns {string}
 */
function decodeJSONString(encoded) {
  try {
    return JSON.parse(encoded);
  } catch (error) {
    throw new Error("Unable to decode JSON log string during secret redaction", { cause: error });
  }
}

/**
 * Resolve raw and encoded matches together before a malformed record can be
 * rewritten. Quoted-token interiors use decoded semantics; raw matches spanning
 * token boundaries remain eligible so a shorter mask cannot fragment them.
 * @param {string} text
 * @param {string[]} maskedValues
 * @param {(value: string) => string} replacementForValue
 * @param {boolean} [matchRawText] - Include raw matches inside quoted spans of invalid artifacts
 * @param {Array<{start: number, end: number, value: string, replacement: string}>} [extraMatches]
 * @returns {string}
 */
function redactMixedMaskedValues(text, maskedValues, replacementForValue, matchRawText = false, extraMatches = [], preserveUnexpandedMaskSources = false) {
  const commandLines = new Set();
  const sourceLineRanges = [];
  if (matchRawText) {
    const lines = text.split("\n");
    let lineStart = 0;
    for (let index = 0; index < lines.length; index++) {
      const line = lines[index];
      const match = line.match(ADD_MASK_COMMAND_RE);
      if (isAddMaskCommandLine(line, preserveUnexpandedMaskSources)) commandLines.add(line + (index < lines.length - 1 ? "\n" : ""));
      else if (preserveUnexpandedMaskSources && match && isUnexpandedMaskSource(line, match)) {
        sourceLineRanges.push({ start: lineStart, end: lineStart + line.length });
      }
      lineStart += line.length + (index < lines.length - 1 ? 1 : 0);
    }
    maskedValues = [...maskedValues, ...commandLines];
  }
  const isPreservedSourceMatch = match => {
    let low = 0;
    let high = sourceLineRanges.length;
    while (low < high) {
      const mid = (low + high) >>> 1;
      if (sourceLineRanges[mid].start <= match.start) low = mid + 1;
      else high = mid;
    }
    const line = sourceLineRanges[low - 1];
    return !!line && match.start >= line.start && match.end <= line.end;
  };
  maskedValues = [...maskedValues].sort((a, b) => b.length - a.length);
  const tokens = [];
  let scanned = 0;
  transformJSONStrings(
    text,
    token => {
      decodeJSONString(token);
      tokens.push(scanned, scanned + token.length);
      scanned += token.length;
      return token;
    },
    plain => {
      scanned += plain.length;
      return plain;
    }
  );
  const tokenAt = position => {
    let low = 0;
    let high = tokens.length / 2;
    while (low < high) {
      const mid = (low + high) >>> 1;
      if (tokens[mid * 2] <= position) low = mid + 1;
      else high = mid;
    }
    return (low - 1) * 2;
  };
  const escapeStarts = [];
  const escapeOffsets = [];
  const escapes = { '"': '"', "\\": "\\", "/": "/", b: "\b", f: "\f", n: "\n", r: "\r", t: "\t" };
  let offset = 0;
  const unescaped = text.replace(/\\(["\\/bfnrt]|u[0-9a-fA-F]{4})/g, (match, escape, index) => {
    escapeStarts.push(index - offset);
    offset += match.length - 1;
    escapeOffsets.push(offset);
    return escape[0] === "u" ? String.fromCharCode(parseInt(escape.slice(1), 16)) : escapes[escape];
  });
  const sourceOffset = position => {
    let low = 0;
    let high = escapeStarts.length;
    while (low < high) {
      const mid = (low + high) >>> 1;
      if (escapeStarts[mid] < position) low = mid + 1;
      else high = mid;
    }
    return position + (low ? escapeOffsets[low - 1] : 0);
  };
  const toSource = match => ({ start: sourceOffset(match.start), end: sourceOffset(match.end), value: match.value });
  const rawMatches = maskedValueMatches(
    text,
    maskedValues,
    match => {
      if (isPreservedSourceMatch(match)) return false;
      if (matchRawText) return true;
      const token = tokenAt(match.start);
      return token < 0 || match.end > tokens[token + 1];
    },
    true
  );
  const decodedMatches = maskedValueMatches(
    unescaped,
    maskedValues,
    match => {
      const source = toSource(match);
      if (isPreservedSourceMatch(source)) return false;
      const token = tokenAt(source.start);
      return token < 0 || source.end > tokens[token + 1] || (source.start > tokens[token] && source.end < tokens[token + 1]);
    },
    true
  );
  function* sourceMatches() {
    let raw = rawMatches.next();
    let decoded = decodedMatches.next();
    let decodedSource = decoded.done ? undefined : toSource(decoded.value);
    while (!raw.done || !decoded.done) {
      const useRaw = !decodedSource || (!raw.done && (raw.value.start < decodedSource.start || (raw.value.start === decodedSource.start && raw.value.end >= decodedSource.end)));
      const match = useRaw && !raw.done ? raw.value : decodedSource;
      if (!match) throw new Error("Unable to select log spans during secret redaction");
      const token = tokenAt(match.start);
      const insideToken = token >= 0 && match.start > tokens[token] && match.end < tokens[token + 1];
      const overlapsToken = (token >= 0 && match.start < tokens[token + 1]) || (token + 2 < tokens.length && tokens[token + 2] < match.end);
      yield { ...match, ambiguous: !insideToken && (overlapsToken || text.slice(match.start, match.end) !== match.value) };
      if (useRaw) raw = rawMatches.next();
      else {
        decoded = decodedMatches.next();
        decodedSource = decoded.done ? undefined : toSource(decoded.value);
      }
    }
  }
  function* combinedMatches() {
    const matches = [...sourceMatches(), ...extraMatches].sort((a, b) => a.start - b.start || b.end - a.end);
    yield* matches;
  }
  const parts = [];
  let cursor = 0;
  let ambiguous = false;
  let repeated = "";
  let repetitions = 0;
  for (const match of overlappingMatchGroups(combinedMatches())) {
    const token = tokenAt(match.start);
    const insideToken = token >= 0 && match.start > tokens[token] && match.end < tokens[token + 1];
    const wholeLine = commandLines.has(match.value) || ((match.start === 0 || text[match.start - 1] === "\n") && (match.end === text.length || text[match.end] === "\n") && maskedValues.includes(text.slice(match.start, match.end)));
    ambiguous ||= match.ambiguous && !wholeLine;
    const marker = commandLines.has(match.value) ? "" : (match.replacement ?? replacementForValue(match.value));
    const replacement = insideToken ? JSON.stringify(marker).slice(1, -1) : marker;
    if (match.start > cursor || replacement !== repeated) {
      if (repetitions) parts.push(repeated.repeat(repetitions));
      repetitions = 0;
      if (match.start > cursor) parts.push(text.slice(cursor, match.start));
    }
    repeated = replacement;
    repetitions++;
    cursor = match.end;
  }
  if (repetitions) parts.push(repeated.repeat(repetitions));
  parts.push(text.slice(cursor));
  const result = parts.join("");
  let position = 0;
  let lineStart = 0;
  let lineEnd = result.indexOf("\n", lineStart);
  let sourceLineClassified = false;
  let preserveSourceLine = false;
  const redacted = transformJSONStrings(
    result,
    encoded => {
      const start = position;
      position += encoded.length;
      if (matchRawText && preserveUnexpandedMaskSources) {
        while (lineEnd >= 0 && start >= lineEnd) {
          lineStart = lineEnd + 1;
          lineEnd = result.indexOf("\n", lineStart);
          sourceLineClassified = false;
        }
        if (!sourceLineClassified) {
          const line = result.slice(lineStart, lineEnd < 0 ? undefined : lineEnd);
          const match = line.match(ADD_MASK_COMMAND_RE);
          preserveSourceLine = !!match && isUnexpandedMaskSource(line, match);
          sourceLineClassified = true;
        }
        if (preserveSourceLine) return encoded;
      }
      const value = decodeJSONString(encoded);
      const withoutCommands = applyAddMaskRedaction(value, []);
      return withoutCommands === value ? encoded : JSON.stringify(withoutCommands);
    },
    plain => {
      position += plain.length;
      return plain;
    }
  );
  // A registered raw whole-line value removes all of the original syntax.
  return ambiguous && !maskedValues.includes(text) ? JSON.stringify(redacted) : redacted;
}

/**
 * Sanitize artifact sources while runtime masks are still available in memory.
 * Decode JSON strings first so escaping cannot hide a registered value, redact
 * matching numeric tokens as JSON strings, and preserve untouched records
 * byte-for-byte.
 * @param {string} content
 * @param {string[]} maskedValues
 * @param {(value: any) => any} [redactJSONValue] - Structured protocol redactor for valid JSON records
 * @param {(value: string) => string} [replacementForValue] - Marker for each matched registered value
 * @param {{textMode?: boolean, patternMatches?: Array<{start: number, end: number, value: string, replacement: string}>, preserveUnexpandedMaskSources?: boolean}} [options] - Match raw credential patterns and preserve source references only when provenance is known
 * @returns {string}
 */
function redactArtifactMaskedValues(content, maskedValues, redactJSONValue, replacementForValue, options = {}) {
  if (!content || (!maskedValues.length && !redactJSONValue && !options.patternMatches?.length && !content.includes("::add-mask::"))) return content;
  const preserveLexemes = !!replacementForValue && !redactJSONValue;
  const replacement = replacementForValue || (() => MASK_REPLACEMENT);
  const preserveUnexpandedMaskSources = !!options.preserveUnexpandedMaskSources;
  if (options.textMode && preserveLexemes) {
    // Only multiline credentials can overlap a removed command-line boundary.
    if (!maskedValues.some(value => /[\r\n]/.test(value))) {
      content = content
        .split("\n")
        .filter(line => !isAddMaskCommandLine(line, preserveUnexpandedMaskSources))
        .join("\n");
    }
    return redactMixedMaskedValues(content, maskedValues, replacement, true, options.patternMatches, preserveUnexpandedMaskSources);
  }
  const redactValue = value => {
    if (typeof value === "string") return applyAddMaskRedaction(value, maskedValues, replacementForValue, preserveUnexpandedMaskSources);
    if (Array.isArray(value)) return value.map(redactValue);
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, nested]) => [redactMaskedValues(key, maskedValues, replacementForValue), redactValue(nested)]));
    return value;
  };
  const redactString = (encoded, isKey = false) => {
    const value = decodeJSONString(encoded);
    const redacted = isKey ? redactMaskedValues(value, maskedValues, replacementForValue) : redactValue(value);
    return redacted === value ? encoded : JSON.stringify(redacted);
  };
  const redactJson = (text, value) => {
    // Skip quoted strings and match original numeric tokens at every depth so
    // rounding or exponent notation cannot hide a mask.
    const numericRedacted = transformJSONStrings(
      text,
      token => token,
      plain =>
        plain.replace(/-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g, token => {
          const redacted = redactMaskedValues(token, maskedValues, replacementForValue);
          return redacted === token ? token : JSON.stringify(redacted);
        })
    );
    if (numericRedacted !== text) {
      try {
        value = JSON.parse(numericRedacted.trim());
      } catch (error) {
        throw new Error("Unable to decode redacted JSON log record during secret redaction", { cause: error });
      }
    }
    if (preserveLexemes) {
      let cursor = 0;
      return transformJSONStrings(
        numericRedacted,
        token => {
          cursor += token.length;
          let next = cursor;
          while (next < numericRedacted.length && " \t\r\n".includes(numericRedacted[next])) next++;
          return redactString(token, numericRedacted[next] === ":");
        },
        plain => {
          cursor += plain.length;
          return plain;
        }
      );
    }
    const redacted = JSON.stringify((redactJSONValue || redactValue)(value));
    return redacted === JSON.stringify(value) ? numericRedacted : text.match(/^\s*/)[0] + redacted + text.match(/\s*$/)[0];
  };
  // Preserve support for complete, possibly pretty-printed JSON documents.
  let document;
  let validDocument = true;
  try {
    document = JSON.parse(content);
  } catch {
    validDocument = false;
  }
  if (validDocument) return redactJson(content, document);
  const redactedLines = [];
  for (const event of iterateJsonlEvents(content)) {
    const newline = event.rawLine.endsWith("\n") ? "\n" : "";
    const line = newline ? event.rawLine.slice(0, -1) : event.rawLine;
    if (isAddMaskCommandLine(line.replace(/\r$/, ""), preserveUnexpandedMaskSources)) continue;
    if (event.type === "json") {
      redactedLines.push(redactJson(event.rawLine, event.value));
    } else if (preserveLexemes) {
      redactedLines.push(redactMixedMaskedValues(line, maskedValues, replacement, false, [], preserveUnexpandedMaskSources), newline);
    } else {
      // Mixed or malformed logs can still contain recoverable JSON strings.
      const decoded = transformJSONStrings(line, redactString);
      const redacted = redactMaskedValues(decoded, maskedValues, replacementForValue);
      redactedLines.push(redactAmbiguousMaskedValues(redacted, maskedValues, replacement), newline);
    }
  }
  return redactedLines.join("");
}

module.exports = {
  ADD_MASK_COMMAND_RE,
  MASK_REPLACEMENT,
  applyAddMaskRedaction,
  collectAddMaskedValues,
  isAddMaskCommandLine,
  redactArtifactMaskedValues,
  redactMaskedValues,
  transformJSONStrings,
  unescapeWorkflowCommandValue,
};
