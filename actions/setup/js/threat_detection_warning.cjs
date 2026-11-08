// @ts-check

/**
 * Normalizes schema-defined threat-kind labels for public messages.
 * Accepts comma/space-delimited values and falls back to "unknown".
 *
 * @param {string | undefined | null} reason
 * @returns {string}
 */
function normalizeThreatKinds(reason) {
  const value = String(reason || "").trim();
  if (!value) return "unknown";
  const kinds = value
    .split(/[\s,]+/)
    .map(kind => kind.toLowerCase())
    .filter(Boolean)
    .filter(kind => Object.hasOwn(THREAT_KIND_LABELS, kind));
  return kinds.length > 0 ? Array.from(new Set(kinds)).join(",") : "unknown";
}

const THREAT_KIND_LABELS = {
  prompt_injection: "prompt injection",
  secret_leak: "secret leak",
  malicious_patch: "malicious patch",
};

/**
 * @param {string | undefined | null} [threatKinds]
 * @returns {string}
 */
function getDetectedThreatKindsText(threatKinds = process.env.GH_AW_DETECTION_THREAT_KINDS) {
  const normalized = normalizeThreatKinds(threatKinds);
  return normalized === "unknown"
    ? "Threat types were not reported."
    : `Detected threat types: ${normalized
        .split(",")
        .map(kind => THREAT_KIND_LABELS[kind])
        .join(", ")}.`;
}

/**
 * Returns a human-readable reason text for detection warnings.
 *
 * @param {string | undefined | null} reason
 * @param {string | undefined | null} [threatKinds]
 * @returns {string}
 */
function getDetectionReasonText(reason, threatKinds = process.env.GH_AW_DETECTION_THREAT_KINDS) {
  const reasonDescriptions = {
    threat_detected: `Potential security threats were detected in the agent output. ${getDetectedThreatKindsText(threatKinds)}`,
    agent_failure: "The threat detection engine failed to produce results.",
    parse_error: "The threat detection results could not be parsed.",
  };
  const normalizedReason = String(reason || "").trim();
  return reasonDescriptions[normalizedReason] || "The threat detection analysis could not be completed.";
}

/**
 * Returns true when the reason indicates a tooling failure rather than an actual
 * security finding. Tooling failures (agent_failure, parse_error) mean the
 * detection engine itself crashed or could not produce a verdict — they should be
 * surfaced as a distinct infrastructure error, not as a security threat.
 *
 * @param {string | undefined | null} reason
 * @returns {boolean}
 */
function isToolingFailureReason(reason) {
  const normalized = String(reason || "").trim();
  return normalized === "agent_failure" || normalized === "parse_error";
}

/**
 * Returns the detection conclusion that safe-output consumers must act on.
 *
 * safe_outputs only runs when the detection job result is success, so a
 * "failure" conclusion seen there was swallowed by the warn-mode step-level
 * continue-on-error. When its reason is a tooling failure (agent_failure,
 * parse_error) the detection engine failed without a verdict, and the warn-mode
 * policy (WTD1a annotate, WTD2 convert, WTD3 abort) applies exactly as for
 * "warning". Every other conclusion is returned unchanged.
 *
 * @param {string | undefined | null} conclusion
 * @param {string | undefined | null} reason
 * @returns {string}
 */
function getEffectiveDetectionConclusion(conclusion, reason) {
  const normalized = String(conclusion || "").trim();
  if (normalized === "failure" && isToolingFailureReason(reason)) {
    return "warning";
  }
  return normalized;
}

/**
 * Returns the XML marker used to identify threat-engine-error output.
 * This marker is distinct from the real-threat marker so that automated tools
 * can distinguish a tooling failure from an actual security finding.
 *
 * @returns {string}
 */
function getThreatEngineErrorMarker() {
  return "<!-- gh-aw-threat-engine-error -->";
}

/**
 * Returns the marker template for configured engine-error message rendering.
 *
 * @returns {string}
 */
function getThreatEngineErrorMarkerTemplate() {
  return "<!-- gh-aw-threat-engine-error -->";
}

/**
 * Returns the XML marker used to identify threat-detected output.
 * When the reason indicates a tooling failure (agent_failure, parse_error) a
 * distinct engine-error marker is returned so automated tools can distinguish
 * "detection engine crashed" from "detection engine found something".
 *
 * @param {string | undefined | null} reason
 * @returns {string}
 */
function getThreatDetectedMarker(reason) {
  if (isToolingFailureReason(reason)) {
    return getThreatEngineErrorMarker();
  }
  return "<!-- gh-aw-threat-detected -->";
}

/**
 * Returns the marker template for configured message rendering.
 * Always returns the real-threat marker; use getThreatEngineErrorMarkerTemplate()
 * for tooling-failure templates where the reason is known at template-build time.
 *
 * @returns {string}
 */
function getThreatDetectedMarkerTemplate() {
  return "<!-- gh-aw-threat-detected -->";
}

module.exports = {
  normalizeThreatKinds,
  getDetectedThreatKindsText,
  getThreatDetectedMarker,
  getThreatDetectedMarkerTemplate,
  getThreatEngineErrorMarker,
  getThreatEngineErrorMarkerTemplate,
  getDetectionReasonText,
  isToolingFailureReason,
  getEffectiveDetectionConclusion,
};
