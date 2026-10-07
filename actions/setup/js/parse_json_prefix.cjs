// @ts-check

/**
 * Parses the JSON value at the start of a possibly truncated input string.
 * Completed object properties are retained when a nested value is incomplete.
 * @param {string} input
 * @returns {any}
 */
function parseJsonPrefix(input) {
  const incomplete = Symbol("incomplete");
  let index = 0;

  function isWhitespace(character) {
    return character === " " || character === "\t" || character === "\n" || character === "\r";
  }

  function skipWhitespace() {
    while (isWhitespace(input[index])) index++;
  }

  function parseString() {
    const start = index++;
    let escaped = false;
    while (index < input.length) {
      const character = input[index++];
      if (escaped) {
        escaped = false;
      } else if (character === "\\") {
        escaped = true;
      } else if (character === '"') {
        try {
          return JSON.parse(input.slice(start, index));
        } catch {
          return incomplete;
        }
      }
    }
    return incomplete;
  }

  function parseValue(depth = 0) {
    if (depth > 64) throw new Error("JSON nesting limit exceeded");
    skipWhitespace();
    const character = input[index];
    if (character === '"') {
      const value = parseString();
      return { value, complete: value !== incomplete };
    }
    if (character === "{" || character === "[") {
      const isObject = character === "{";
      const endCharacter = isObject ? "}" : "]";
      const value = isObject ? Object.create(null) : [];
      index++;
      skipWhitespace();
      if (input[index] === endCharacter) {
        index++;
        return { value, complete: true };
      }
      while (index < input.length) {
        let key;
        if (isObject) {
          if (input[index] !== '"') return { value, complete: false };
          key = parseString();
          if (key === incomplete) return { value, complete: false };
          skipWhitespace();
          if (input[index++] !== ":") return { value, complete: false };
        }
        const child = parseValue(depth + 1);
        if (child.value !== incomplete) {
          if (isObject) value[key] = child.value;
          else value.push(child.value);
        }
        if (!child.complete) return { value, complete: false };
        skipWhitespace();
        if (input[index] === endCharacter) {
          index++;
          return { value, complete: true };
        }
        if (input[index++] !== ",") return { value, complete: false };
        skipWhitespace();
      }
      return { value, complete: false };
    }

    const start = index;
    while (index < input.length && !isWhitespace(input[index]) && !",[]{}".includes(input[index])) {
      index++;
    }
    if (start === index) return { value: incomplete, complete: false };
    try {
      return { value: JSON.parse(input.slice(start, index)), complete: true };
    } catch {
      return { value: incomplete, complete: false };
    }
  }

  try {
    const result = parseValue().value;
    return result === incomplete ? undefined : result;
  } catch {
    return undefined;
  }
}

module.exports = { parseJsonPrefix };
