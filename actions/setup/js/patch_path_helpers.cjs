// @ts-check
function decodeGitQuotedPath(token) {
  if (!token.startsWith('"') || !token.endsWith('"')) {
    return null;
  }

  const bytes = [];
  const end = token.length - 1;
  const escapes = { a: 7, b: 8, t: 9, n: 10, v: 11, f: 12, r: 13, '"': 34, "\\": 92 };
  for (let i = 1; i < end; i++) {
    if (token[i] === "\\") {
      if (i + 1 >= end) {
        return null;
      }
      if (/[0-7]/.test(token[i + 1])) {
        let octal = "";
        while (i + 1 < end && octal.length < 3 && /[0-7]/.test(token[i + 1])) {
          octal += token[++i];
        }
        const byte = Number.parseInt(octal, 8);
        if (byte > 0xff) {
          return null;
        }
        bytes.push(byte);
      } else {
        const byte = escapes[token[++i]];
        if (byte === undefined) {
          return null;
        }
        bytes.push(byte);
      }
      continue;
    }

    const codePoint = token.codePointAt(i);
    for (const byte of Buffer.from(String.fromCodePoint(codePoint), "utf8")) {
      bytes.push(byte);
    }
    if (codePoint > 0xffff) {
      i++;
    }
  }

  return Buffer.from(bytes).toString("utf8");
}

function parsePathToken(token) {
  let value = token;
  if (token.startsWith('"')) {
    value = decodeGitQuotedPath(token);
    if (value === null) {
      return { path: null, parseable: false };
    }
  } else if (token.endsWith('"')) {
    return { path: null, parseable: false };
  }

  if (value.startsWith("a/") || value.startsWith("b/")) {
    value = value.slice(2);
  }
  return { path: value || null, parseable: true };
}

/**
 * Parses a single `diff --git` header line and extracts both old/new paths.
 * Handles unquoted and C-style quoted pathspecs.
 *
 * @param {string} headerLine
 * @returns {{ oldPath: string|null, newPath: string|null, parseable: boolean }}
 */
function parseDiffGitHeader(headerLine) {
  const sanitizedHeaderLine = headerLine.endsWith("\r") ? headerLine.slice(0, -1) : headerLine;
  const rest = sanitizedHeaderLine.replace(/^diff --git /, "");
  if (rest === sanitizedHeaderLine) {
    return { oldPath: null, newPath: null, parseable: false };
  }

  // Git may emit unquoted paths that still contain spaces in `diff --git`
  // headers. In that case, split using the required ` b/` token boundary
  // instead of generic whitespace tokenization.
  if (rest.startsWith("a/")) {
    const quotedSep = rest.indexOf(' "b/');
    const unquotedSep = rest.indexOf(" b/");
    const foundSeparatorIndices = [quotedSep, unquotedSep].filter(idx => idx >= 0);
    if (foundSeparatorIndices.length > 0) {
      const sep = foundSeparatorIndices.reduce((smallest, idx) => (idx < smallest ? idx : smallest), foundSeparatorIndices[0]);
      const oldToken = rest.slice(0, sep);
      const newToken = rest.slice(sep + 1).trimEnd();
      const oldResult = parsePathToken(oldToken);
      const newResult = parsePathToken(newToken);
      if (oldResult.parseable && newResult.parseable && (oldResult.path || newResult.path)) {
        return { oldPath: oldResult.path, newPath: newResult.path, parseable: true };
      }
    }
  }

  /** @type {string[]} */
  const tokens = [];
  const isWhitespace = ch => ch === " " || ch === "\t" || ch === "\r" || ch === "\n";
  let i = 0;
  while (i < rest.length && tokens.length < 2) {
    while (i < rest.length && isWhitespace(rest[i])) {
      i++;
    }
    if (i >= rest.length) {
      break;
    }

    let token = "";
    if (rest[i] === '"') {
      token += rest[i++];
      let closedQuote = false;
      while (i < rest.length) {
        const ch = rest[i++];
        token += ch;
        if (ch === "\\" && i < rest.length) {
          token += rest[i++];
        } else if (ch === '"') {
          closedQuote = true;
          break;
        }
      }
      if (!closedQuote) {
        return { oldPath: null, newPath: null, parseable: false };
      }
    } else {
      while (i < rest.length && !isWhitespace(rest[i])) {
        token += rest[i++];
      }
    }
    tokens.push(token);
  }

  if (tokens.length < 2) {
    return { oldPath: null, newPath: null, parseable: false };
  }

  const oldResult = parsePathToken(tokens[0]);
  const newResult = parsePathToken(tokens[1]);
  if (!oldResult.parseable || !newResult.parseable || (!oldResult.path && !newResult.path)) {
    return { oldPath: null, newPath: null, parseable: false };
  }

  return { oldPath: oldResult.path, newPath: newResult.path, parseable: true };
}

/**
 * Extracts parsed entries for all `diff --git` headers in a patch.
 *
 * @param {string} patchContent
 * @returns {{ oldPath: string|null, newPath: string|null, parseable: boolean, headerIndex: number, headerLine: string }[]}
 */
function extractDiffGitHeaderEntries(patchContent) {
  if (!patchContent || !patchContent.trim()) {
    return [];
  }

  /** @type {{ oldPath: string|null, newPath: string|null, parseable: boolean, headerIndex: number, headerLine: string }[]} */
  const entries = [];
  const headerRe = /^diff --git .*$/gm;
  let match;
  while ((match = headerRe.exec(patchContent)) !== null) {
    entries.push({
      ...parseDiffGitHeader(match[0]),
      headerIndex: match.index,
      headerLine: match[0],
    });
  }
  return entries;
}

module.exports = { parseDiffGitHeader, extractDiffGitHeaderEntries };
