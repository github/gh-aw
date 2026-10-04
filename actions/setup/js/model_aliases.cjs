// @ts-check

/**
 * Build a deterministic compact model identifier for summary rendering.
 *
 * @param {string|undefined|null} modelName
 * @returns {string}
 */
function reduceModelNameToIdentifier(modelName) {
  const normalized = stripModelProviderPrefix(
    String(modelName || "")
      .trim()
      .toLowerCase()
  );
  if (!normalized) return "";

  if (normalized === "opus" || normalized === "sonnet" || normalized === "haiku") {
    return normalized;
  }

  // Exact-name shortcuts for opus/sonnet/haiku are handled above. This guard
  // returns any remaining short (< 6 chars) pure-alphanumeric name verbatim so
  // that tokens like "o1", "auto", or "gpt" are not padded with spurious zeros.
  // Names containing non-alphanumeric characters (hyphens, pipes, etc.) fall
  // through to the family shortcuts or the fallback sanitizer below.
  if (normalized.length < 6 && /^[a-z0-9]+$/.test(normalized)) {
    return normalized;
  }

  const VERSION_SUFFIX_PATTERN = "[-_\\s]*([0-9]+)(?:[._-]+([0-9]+))?";
  const FALLBACK_LETTER_LENGTH = 3;
  const FALLBACK_DIGIT_LENGTH = 2;
  const FALLBACK_PADDING_CHAR = "x";

  /**
   * @param {string} family - Fixed alphanumeric family name from the table below
   * @returns {RegExp}
   */
  const buildFamilyVersionPattern = family => new RegExp(`${family.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}${VERSION_SUFFIX_PATTERN}`);

  /** @type {Array<{ familyPattern: RegExp, versionPattern: RegExp, prefix: string }>} */
  const shortcuts = [
    { familyPattern: /sonnet/, versionPattern: buildFamilyVersionPattern("sonnet"), prefix: "sonnet" },
    { familyPattern: /opus/, versionPattern: buildFamilyVersionPattern("opus"), prefix: "opus" },
    { familyPattern: /haiku/, versionPattern: buildFamilyVersionPattern("haiku"), prefix: "haiku" },
    { familyPattern: /gpt/, versionPattern: buildFamilyVersionPattern("gpt"), prefix: "gpt" },
    { familyPattern: /^o[0-9](?:$|[-_])/, versionPattern: buildFamilyVersionPattern("o"), prefix: "o" },
    { familyPattern: /gemini/, versionPattern: buildFamilyVersionPattern("gemini"), prefix: "gem" },
  ];

  for (const { familyPattern, versionPattern, prefix } of shortcuts) {
    if (!familyPattern.test(normalized)) continue;
    const version = extractModelVersionDigits(normalized, versionPattern);
    return `${prefix}${version}${extractKnownModelTierSuffix(normalized)}`;
  }

  return buildFallbackModelIdentifier(normalized, FALLBACK_LETTER_LENGTH, FALLBACK_DIGIT_LENGTH, FALLBACK_PADDING_CHAR);
}

/**
 * Strip a provider prefix from a model name (e.g. "copilot/mai-code-1-flash-picker"
 * becomes "mai-code-1-flash-picker") so the identifier reflects the model, not the provider.
 *
 * @param {string} normalizedModelName
 * @returns {string}
 */
function stripModelProviderPrefix(normalizedModelName) {
  const slashIndex = normalizedModelName.lastIndexOf("/");
  if (slashIndex === -1) return normalizedModelName;
  const modelPart = normalizedModelName.slice(slashIndex + 1).trim();
  return modelPart || normalizedModelName;
}

/**
 * @param {string|undefined|null} modelName
 * @returns {string}
 */
function formatModelEmojiAlias(modelName) {
  return reduceModelNameToIdentifier(modelName);
}

/**
 * @param {string[]} models
 * @returns {string}
 */
function formatModelEmojiAliasLegend(models) {
  const seen = new Set();
  const entries = [];

  for (const model of models || []) {
    const normalized = String(model || "").trim();
    if (!normalized || seen.has(normalized)) continue;
    seen.add(normalized);
    const alias = formatModelEmojiAlias(normalized);
    if (!alias) continue;
    entries.push(`${alias}=${escapeHtml(normalized)}`);
  }

  return entries.join(" · ");
}

/**
 * @param {string} normalizedModelName
 * @returns {string}
 */
function extractKnownModelTierSuffix(normalizedModelName) {
  if (hasDelimitedModelQualifier(normalizedModelName, "mini")) return "mini";
  if (hasDelimitedModelQualifier(normalizedModelName, "nano")) return "nano";
  if (hasDelimitedModelQualifier(normalizedModelName, "codex")) return "codex";
  if (hasDelimitedModelQualifier(normalizedModelName, "pro")) return "pro";
  return "";
}

/**
 * @param {string} normalizedModelName
 * @param {string} qualifier
 * @returns {boolean}
 */
function hasDelimitedModelQualifier(normalizedModelName, qualifier) {
  const escapedQualifier = qualifier.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(^|[-_\\s])${escapedQualifier}($|[-_\\s])`).test(normalizedModelName);
}

/**
 * @param {string} normalizedModelName
 * @param {RegExp} familyVersionPattern
 * @returns {string}
 */
function extractModelVersionDigits(normalizedModelName, familyVersionPattern) {
  const familyMatch = normalizedModelName.match(familyVersionPattern);
  if (familyMatch) {
    return normalizeVersionDigits(familyMatch[1], familyMatch[2]);
  }

  const firstNumericMatch = normalizedModelName.match(/([0-9]+)(?:[._-]+([0-9]+))?/);
  if (firstNumericMatch) {
    return normalizeVersionDigits(firstNumericMatch[1], firstNumericMatch[2]);
  }

  return "00";
}

/**
 * @param {string|undefined} major
 * @param {string|undefined} minor
 * @returns {string}
 */
function normalizeVersionDigits(major, minor) {
  const majorDigit = getFirstDigit(major);
  const minorIsDateLike = minor && /^\d{3,}$/.test(minor);
  const minorDigit = getFirstDigit(minor, Boolean(minorIsDateLike));
  return `${majorDigit}${minorDigit}`;
}

/**
 * @param {string|undefined} value
 * @param {boolean} [treatAsMissing=false]
 * @returns {string}
 */
function getFirstDigit(value, treatAsMissing = false) {
  if (!value || treatAsMissing) return "0";
  const digitMatch = value.match(/\d/);
  return digitMatch ? digitMatch[0] : "0";
}

/**
 * @param {string} normalizedModelName
 * @param {number} fallbackLetterLength
 * @param {number} fallbackDigitLength
 * @param {string} fallbackPaddingChar
 * @returns {string}
 */
function buildFallbackModelIdentifier(normalizedModelName, fallbackLetterLength, fallbackDigitLength, fallbackPaddingChar) {
  const compact = normalizedModelName.replace(/[^a-z0-9]+/g, "");
  if (!compact) return "";

  const letterPart = compact.replace(/[0-9]/g, "").slice(0, fallbackLetterLength).padEnd(fallbackLetterLength, fallbackPaddingChar);
  const digitPart = compact
    .replace(/[^0-9]/g, "")
    .slice(0, fallbackDigitLength)
    .padEnd(fallbackDigitLength, "0");
  return `${letterPart}${digitPart}`.slice(0, 5);
}

/**
 * @param {string} value
 * @returns {string}
 */
function escapeHtml(value) {
  return String(value).replace(/[&<>"']/g, ch => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[ch] ?? ch);
}

module.exports = {
  formatModelEmojiAlias,
  formatModelEmojiAliasLegend,
  reduceModelNameToIdentifier,
};
