// @ts-check
import { describe, it, expect } from "vitest";
const {
  AGENT_OUTPUT_FILENAME,
  TMP_GH_AW_PATH,
  COPILOT_REVIEWER_BOT,
  COPILOT_REVIEWER_BOT_ID,
  FAQ_CREATE_PR_PERMISSIONS_URL,
  MAX_LABELS,
  MAX_ASSIGNEES,
  GATEWAY_JSONL_PATH,
  RPC_MESSAGES_PATH,
  MANIFEST_FILE_PATH,
  TEMPORARY_ID_MAP_FILE_PATH,
  DETECTION_LOG_FILENAME,
  DEFAULT_MCP_CALL_WATCHDOG_MS,
  MCP_CALL_TRANSPORT_GRACE_MS,
  MAX_RESTORE_RUNS,
  MAX_RESTORE_PAGES,
  SCAN_WINDOW_MS,
} = require("./constants.cjs");

describe("constants", () => {
  describe("file names", () => {
    it("should export AGENT_OUTPUT_FILENAME", () => {
      expect(AGENT_OUTPUT_FILENAME).toBe("agent_output.json");
    });

    it("should export DETECTION_LOG_FILENAME", () => {
      expect(DETECTION_LOG_FILENAME).toBe("detection.log");
    });
  });

  describe("paths", () => {
    it("should export TMP_GH_AW_PATH", () => {
      expect(TMP_GH_AW_PATH).toBe("/tmp/gh-aw");
    });

    it("should export GATEWAY_JSONL_PATH under TMP_GH_AW_PATH", () => {
      expect(GATEWAY_JSONL_PATH).toBe("/tmp/gh-aw/mcp-logs/gateway.jsonl");
      expect(GATEWAY_JSONL_PATH.startsWith(TMP_GH_AW_PATH)).toBe(true);
    });

    it("should export RPC_MESSAGES_PATH under TMP_GH_AW_PATH", () => {
      expect(RPC_MESSAGES_PATH).toBe("/tmp/gh-aw/mcp-logs/rpc-messages.jsonl");
      expect(RPC_MESSAGES_PATH.startsWith(TMP_GH_AW_PATH)).toBe(true);
    });

    it("should export MANIFEST_FILE_PATH under TMP_GH_AW_PATH", () => {
      expect(MANIFEST_FILE_PATH).toBe("/tmp/gh-aw/safe-output-items.jsonl");
      expect(MANIFEST_FILE_PATH.startsWith(TMP_GH_AW_PATH)).toBe(true);
    });

    it("should export TEMPORARY_ID_MAP_FILE_PATH under TMP_GH_AW_PATH", () => {
      expect(TEMPORARY_ID_MAP_FILE_PATH).toBe("/tmp/gh-aw/temporary-id-map.json");
      expect(TEMPORARY_ID_MAP_FILE_PATH.startsWith(TMP_GH_AW_PATH)).toBe(true);
    });
  });

  describe("GitHub bot names", () => {
    it("should export COPILOT_REVIEWER_BOT", () => {
      expect(COPILOT_REVIEWER_BOT).toBe("copilot-pull-request-reviewer[bot]");
    });

    it("should export COPILOT_REVIEWER_BOT_ID", () => {
      expect(COPILOT_REVIEWER_BOT_ID).toBe("BOT_kgDOCnlnWA");
    });
  });

  describe("documentation URLs", () => {
    it("should export FAQ_CREATE_PR_PERMISSIONS_URL as a valid URL", () => {
      expect(typeof FAQ_CREATE_PR_PERMISSIONS_URL).toBe("string");
      expect(FAQ_CREATE_PR_PERMISSIONS_URL).toMatch(/^https:\/\//);
    });
  });

  describe("array size limits", () => {
    it("should export MAX_LABELS as a positive integer", () => {
      expect(MAX_LABELS).toBe(10);
      expect(Number.isInteger(MAX_LABELS)).toBe(true);
      expect(MAX_LABELS).toBeGreaterThan(0);
    });

    it("should export MAX_ASSIGNEES as a positive integer", () => {
      expect(MAX_ASSIGNEES).toBe(5);
      expect(Number.isInteger(MAX_ASSIGNEES)).toBe(true);
      expect(MAX_ASSIGNEES).toBeGreaterThan(0);
    });
  });

  describe("Codex MCP watchdog timeouts", () => {
    it("should export the default call timeout and transport grace period", () => {
      expect(DEFAULT_MCP_CALL_WATCHDOG_MS).toBe(120_000);
      expect(MCP_CALL_TRANSPORT_GRACE_MS).toBe(60_000);
    });
  });

  describe("daily AIC scan cache restore limits", () => {
    it("should export the bounded run and page counts and scan window", () => {
      expect(MAX_RESTORE_RUNS).toBe(10);
      expect(MAX_RESTORE_PAGES).toBe(25);
      expect(SCAN_WINDOW_MS).toBe(24 * 60 * 60 * 1000);
    });
  });

  describe("module exports", () => {
    it("should export all expected constants", () => {
      const exported = require("./constants.cjs");
      const expectedKeys = [
        "AGENT_OUTPUT_FILENAME",
        "TMP_GH_AW_PATH",
        "COPILOT_REVIEWER_BOT",
        "COPILOT_REVIEWER_BOT_ID",
        "FAQ_CREATE_PR_PERMISSIONS_URL",
        "MAX_LABELS",
        "MAX_ASSIGNEES",
        "GATEWAY_JSONL_PATH",
        "RPC_MESSAGES_PATH",
        "MANIFEST_FILE_PATH",
        "TEMPORARY_ID_MAP_FILE_PATH",
        "DETECTION_LOG_FILENAME",
        "DEFAULT_MCP_CALL_WATCHDOG_MS",
        "MCP_CALL_TRANSPORT_GRACE_MS",
        "MAX_RESTORE_RUNS",
        "MAX_RESTORE_PAGES",
        "SCAN_WINDOW_MS",
      ];
      for (const key of expectedKeys) {
        expect(exported).toHaveProperty(key);
      }
    });
  });
});
