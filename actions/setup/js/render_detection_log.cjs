// @ts-check
/// <reference types="@actions/github-script" />

/**
 * render_detection_log — Detection log renderer for the detection job.
 *
 * Reads the detection engine log file and pipes it to stdout wrapped in GitHub
 * Actions group (`::group::` / `::endgroup::`) and stop-commands
 * (`::stop-commands::` / `::<token>::`) macros so that:
 *   - the output is folded into a collapsible section in the Actions log UI, and
 *   - any workflow-command-shaped lines inside the log are not interpreted by
 *     the runner (preventing command injection from agent-controlled content).
 *
 * Secret redaction is applied before the content is written so that credential-
 * shaped strings are replaced with `***REDACTED***` and MCP gateway tokens are
 * masked via `::add-mask::` at the runner level.
 *
 * This script is intended to run AFTER redact_secrets so that secrets are
 * redacted from the file on disk before this helper reads and re-emits them.
 * The in-line redaction here is a defence-in-depth layer for the stdout copy.
 */

"use strict";

const fs = require("fs");
const { redactBuiltInPatterns } = require("./redact_secrets.cjs");
const { renderLogToStdout } = require("./render_log_to_stdout.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { lstatGuard } = require("./symlink_guard.cjs");

/** Default path to the detection engine log file. */
const DETECTION_LOG_PATH = require("./runtime_paths.cjs").runtimePath("threat-detection/detection.log");

/** Maximum number of bytes read from the log file before truncation. */
const MAX_LOG_BYTES = 1024 * 1024; // 1 MiB

/**
 * Reads a log file (with size capping), applies built-in redaction, and
 * renders it to stdout wrapped in GitHub Actions group + stop-commands macros.
 *
 * @param {string} filePath                     - Absolute path to the log file to render.
 * @param {string} groupTitle                   - Label shown in the collapsible group header.
 * @param {{ tailLines?: number }} [options]    - Optional bounded tail to render.
 * @returns {Promise<void>}
 */
async function renderLogFromFile(filePath, groupTitle, options) {
  if (!fs.existsSync(filePath)) {
    core.info("Log not found, skipping render: " + filePath);
    return;
  }

  let stat;
  try {
    stat = lstatGuard(filePath);
  } catch (error) {
    core.warning("Failed to stat log: " + getErrorMessage(error));
    return;
  }

  if (stat === null) {
    core.warning("Log is a symbolic link, skipping render: " + filePath);
    return;
  }

  if (!stat.isFile()) {
    core.warning("Log is not a regular file, skipping render: " + filePath);
    return;
  }

  if (stat.size === 0) {
    core.info("Log is empty, skipping render: " + filePath);
    return;
  }

  let content;
  try {
    const tailLines = options?.tailLines;
    if (stat.size > MAX_LOG_BYTES) {
      const readFrom = tailLines ? stat.size - MAX_LOG_BYTES : 0;
      core.warning("Log exceeds " + MAX_LOG_BYTES + " bytes (" + stat.size + " bytes); truncating to " + (tailLines ? "last " : "first ") + MAX_LOG_BYTES + " bytes: " + filePath);
      const fd = fs.openSync(filePath, "r");
      try {
        const buf = Buffer.alloc(MAX_LOG_BYTES);
        const bytesRead = fs.readSync(fd, buf, 0, MAX_LOG_BYTES, readFrom);
        content = buf.slice(0, bytesRead).toString("utf8");
        if (tailLines) {
          const firstNewline = content.indexOf("\n");
          content = firstNewline === -1 ? `[Log tail omitted: final line exceeds ${MAX_LOG_BYTES} bytes]\n` : content.slice(firstNewline + 1);
        }
      } finally {
        fs.closeSync(fd);
      }
    } else {
      content = fs.readFileSync(filePath, "utf8");
    }
    if (tailLines) {
      const lines = content.split("\n");
      content = lines.slice(-(tailLines + (content.endsWith("\n") ? 1 : 0))).join("\n");
    }
  } catch (error) {
    core.warning("Failed to read log: " + getErrorMessage(error));
    return;
  }

  // Apply in-line redaction of built-in credential patterns before emitting.
  const { content: redacted } = redactBuiltInPatterns(content);

  renderLogToStdout(groupTitle, redacted);

  core.info("Log rendered (" + stat.size + " bytes): " + filePath);
}

/**
 * Renders the detection log file to stdout wrapped in GitHub Actions macros.
 *
 * @param {string} [logPath] - Path to the log file; defaults to DETECTION_LOG_PATH.
 * @returns {Promise<void>}
 */
async function main(logPath) {
  await renderLogFromFile(logPath || DETECTION_LOG_PATH, "Detection Log");
}

module.exports = { main, renderLogFromFile, DETECTION_LOG_PATH, MAX_LOG_BYTES };
