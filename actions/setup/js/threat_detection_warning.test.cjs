import { describe, it, expect } from "vitest";
import {
  normalizeThreatKinds,
  getThreatWarningPresentation,
  getThreatDetectedMarker,
  getThreatDetectedMarkerTemplate,
  getThreatEngineErrorMarker,
  getThreatEngineErrorMarkerTemplate,
  getDetectionReasonText,
  isToolingFailureReason,
} from "./threat_detection_warning.cjs";

describe("threat_detection_warning", () => {
  describe("normalizeThreatKinds", () => {
    it("returns unknown for empty input", () => {
      expect(normalizeThreatKinds(undefined)).toBe("unknown");
      expect(normalizeThreatKinds(null)).toBe("unknown");
      expect(normalizeThreatKinds("   ")).toBe("unknown");
    });

    it("accepts only schema categories and de-duplicates without publishing arbitrary tokens", () => {
      expect(normalizeThreatKinds("PROMPT_INJECTION, private-text secret_leak prompt_injection malicious_patch")).toBe("prompt_injection,secret_leak,malicious_patch");
      expect(normalizeThreatKinds("agent_failure private-text <script>")).toBe("unknown");
    });
  });

  describe("marker helpers", () => {
    it("emits the normative threat marker for real threats", () => {
      expect(getThreatDetectedMarker("threat_detected")).toBe("<!-- gh-aw-threat-detected -->");
      expect(getThreatDetectedMarker(null)).toBe("<!-- gh-aw-threat-detected -->");
      expect(getThreatDetectedMarker(undefined)).toBe("<!-- gh-aw-threat-detected -->");
      expect(getThreatDetectedMarker("")).toBe("<!-- gh-aw-threat-detected -->");
      expect(getThreatDetectedMarkerTemplate()).toBe("<!-- gh-aw-threat-detected -->");
    });

    it("emits the engine-error marker for tooling failures", () => {
      expect(getThreatDetectedMarker("agent_failure")).toBe("<!-- gh-aw-threat-engine-error -->");
      expect(getThreatDetectedMarker("parse_error")).toBe("<!-- gh-aw-threat-engine-error -->");
    });

    it("getThreatEngineErrorMarker always returns the engine-error marker", () => {
      expect(getThreatEngineErrorMarker()).toBe("<!-- gh-aw-threat-engine-error -->");
      expect(getThreatEngineErrorMarkerTemplate()).toBe("<!-- gh-aw-threat-engine-error -->");
    });

    it("returns a centralized warning presentation for tooling failures and threats", () => {
      for (const reason of ["agent_failure", "parse_error"]) {
        expect(getThreatWarningPresentation(reason)).toEqual({
          admonition: "WARNING",
          title: "Threat detection engine error",
          summary: "Threat detection could not complete its analysis due to a tooling error. This is not a security finding.",
          marker: "<!-- gh-aw-threat-engine-error -->",
        });
      }
      expect(getThreatWarningPresentation("threat_detected")).toEqual({
        admonition: "CAUTION",
        title: "agentic threat detected",
        summary: "Threat detection flagged this output in warn mode. Manual review is REQUIRED before any follow-up automation.",
        marker: "<!-- gh-aw-threat-detected -->",
      });
    });
  });

  describe("getDetectionReasonText", () => {
    it("returns mapped description for known reason", () => {
      expect(getDetectionReasonText("threat_detected")).toContain("Potential security threats were detected");
    });

    it("returns fallback description for unknown reason", () => {
      expect(getDetectionReasonText("new_reason")).toBe("The threat detection analysis could not be completed.");
    });

    it("describes only the detector-defined types, not arbitrary explanation text", () => {
      expect(getDetectionReasonText("threat_detected", "prompt_injection,secret_leak,malicious_patch,PRIVATE_TEXT")).toBe(
        "Potential security threats were detected in the agent output. Detected threat types: prompt injection, secret leak, malicious patch."
      );
      expect(getDetectionReasonText("threat_detected", "PRIVATE_TEXT")).toContain("Threat types were not reported.");
      expect(getDetectionReasonText("parse_error", "secret_leak")).not.toContain("secret leak");
    });
  });

  describe("isToolingFailureReason", () => {
    it("returns true for agent_failure", () => {
      expect(isToolingFailureReason("agent_failure")).toBe(true);
    });

    it("returns true for parse_error", () => {
      expect(isToolingFailureReason("parse_error")).toBe(true);
    });

    it("returns false for threat_detected", () => {
      expect(isToolingFailureReason("threat_detected")).toBe(false);
    });

    it("returns false for empty/null/undefined", () => {
      expect(isToolingFailureReason("")).toBe(false);
      expect(isToolingFailureReason(null)).toBe(false);
      expect(isToolingFailureReason(undefined)).toBe(false);
    });

    it("returns false for unknown reason", () => {
      expect(isToolingFailureReason("some_new_reason")).toBe(false);
    });
  });
});
