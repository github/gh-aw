// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const fs = require("fs");
const path = require("path");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_code_scanning_alert";

/**
 * Main handler factory for create_code_scanning_alert
 * Returns a message handler function that processes individual create_code_scanning_alert messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const maxFindings = config.max || 0; // 0 means unlimited
  const driverName = config.driver || "GitHub Agentic Workflows Security Scanner";
  const workflowFilename = config.workflow_filename || "workflow";

  core.info(`Create code scanning alert configuration: max=${maxFindings === 0 ? "unlimited" : maxFindings}`);
  core.info(`Driver name: ${driverName}`);
  core.info(`Workflow filename for rule ID prefix: ${workflowFilename}`);

  // Track how many items we've processed for max limit
  let processedCount = 0;

  // Collect valid findings across all messages
  const validFindings = [];

  // SARIF file path
  const sarifFileName = "code-scanning-alert.sarif";
  const sarifFilePath = path.join(process.cwd(), sarifFileName);

  /**
   * Generate and write SARIF file with all collected findings
   */
  function generateSarifFile() {
    if (validFindings.length === 0) {
      core.info("No findings to write to SARIF file");
      return;
    }

    const sarifContent = {
      $schema: "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
      version: "2.1.0",
      runs: [
        {
          tool: {
            driver: {
              name: driverName,
              version: "1.0.0",
              informationUri: "https://github.com/github/gh-aw",
            },
          },
          results: validFindings.map((finding, index) => ({
            ruleId: finding.ruleIdSuffix ? `${workflowFilename}-${finding.ruleIdSuffix}` : `${workflowFilename}-security-finding-${index + 1}`,
            message: { text: finding.message },
            level: finding.sarifLevel,
            locations: [
              {
                physicalLocation: {
                  artifactLocation: { uri: finding.file },
                  region: {
                    startLine: finding.line,
                    startColumn: finding.column,
                  },
                },
              },
            ],
          })),
        },
      ],
    };

    fs.writeFileSync(sarifFilePath, JSON.stringify(sarifContent, null, 2));
    core.info(`✓ Updated SARIF file with ${validFindings.length} finding(s): ${sarifFilePath}`);
  }

  /**
   * Message handler function that processes a single create_code_scanning_alert message
   * @param {Object} message - The create_code_scanning_alert message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleCreateCodeScanningAlert(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (maxFindings > 0 && processedCount >= maxFindings) {
      core.warning(`Skipping create_code_scanning_alert: max count of ${maxFindings} reached`);
      return {
        success: false,
        error: `Max count of ${maxFindings} reached`,
      };
    }

    const securityItem = message;

    core.info(
      `Processing create-code-scanning-alert: file=${securityItem.file}, line=${securityItem.line}, severity=${securityItem.severity}, messageLength=${securityItem.message ? securityItem.message.length : "undefined"}, ruleIdSuffix=${securityItem.ruleIdSuffix || "not specified"}`
    );

    // Validate required fields
    if (!securityItem.file) {
      core.warning('Missing required field "file" in code scanning alert item');
      return {
        success: false,
        error: 'Missing required field "file"',
      };
    }

    if (!securityItem.line || (typeof securityItem.line !== "number" && typeof securityItem.line !== "string")) {
      core.warning('Missing or invalid required field "line" in code scanning alert item');
      return {
        success: false,
        error: 'Missing or invalid required field "line"',
      };
    }

    if (!securityItem.severity || typeof securityItem.severity !== "string") {
      core.warning('Missing or invalid required field "severity" in code scanning alert item');
      return {
        success: false,
        error: 'Missing or invalid required field "severity"',
      };
    }

    if (!securityItem.message || typeof securityItem.message !== "string") {
      core.warning('Missing or invalid required field "message" in code scanning alert item');
      return {
        success: false,
        error: 'Missing or invalid required field "message"',
      };
    }

    // Parse line number
    const line = parseInt(securityItem.line, 10);
    if (isNaN(line) || line <= 0) {
      core.warning(`Invalid line number: ${securityItem.line}`);
      return {
        success: false,
        error: `Invalid line number: ${securityItem.line}`,
      };
    }

    // Parse optional column number
    let column = 1; // Default to column 1
    if (securityItem.column !== undefined) {
      if (typeof securityItem.column !== "number" && typeof securityItem.column !== "string") {
        core.warning('Invalid field "column" in code scanning alert item (must be number or string)');
        return {
          success: false,
          error: 'Invalid field "column" (must be number or string)',
        };
      }
      const parsedColumn = parseInt(securityItem.column, 10);
      if (isNaN(parsedColumn) || parsedColumn <= 0) {
        core.warning(`Invalid column number: ${securityItem.column}`);
        return {
          success: false,
          error: `Invalid column number: ${securityItem.column}`,
        };
      }
      column = parsedColumn;
    }

    // Parse optional rule ID suffix
    let ruleIdSuffix = null;
    if (securityItem.ruleIdSuffix !== undefined) {
      if (typeof securityItem.ruleIdSuffix !== "string") {
        core.warning('Invalid field "ruleIdSuffix" in code scanning alert item (must be string)');
        return {
          success: false,
          error: 'Invalid field "ruleIdSuffix" (must be string)',
        };
      }
      // Validate that the suffix doesn't contain invalid characters
      const trimmedSuffix = securityItem.ruleIdSuffix.trim();
      if (trimmedSuffix.length === 0) {
        core.warning('Invalid field "ruleIdSuffix" in code scanning alert item (cannot be empty)');
        return {
          success: false,
          error: 'Invalid field "ruleIdSuffix" (cannot be empty)',
        };
      }
      // Check for characters that would be problematic in rule IDs
      if (!/^[a-zA-Z0-9_-]+$/.test(trimmedSuffix)) {
        core.warning(`Invalid ruleIdSuffix "${trimmedSuffix}" (must contain only alphanumeric characters, hyphens, and underscores)`);
        return {
          success: false,
          error: `Invalid ruleIdSuffix "${trimmedSuffix}" (must contain only alphanumeric characters, hyphens, and underscores)`,
        };
      }
      ruleIdSuffix = trimmedSuffix;
    }

    // Validate severity level and map to SARIF level
    /** @type {Record<string, string>} */
    const severityMap = {
      error: "error",
      warning: "warning",
      info: "note",
      note: "note",
    };

    const normalizedSeverity = securityItem.severity.toLowerCase();
    if (!severityMap[normalizedSeverity]) {
      core.warning(`Invalid severity level: ${securityItem.severity} (must be error, warning, info, or note)`);
      return {
        success: false,
        error: `Invalid severity level: ${securityItem.severity} (must be error, warning, info, or note)`,
      };
    }

    const sarifLevel = severityMap[normalizedSeverity];

    processedCount++;

    // Create a valid finding object
    const finding = {
      file: securityItem.file.trim(),
      line: line,
      column: column,
      severity: normalizedSeverity,
      sarifLevel: sarifLevel,
      message: securityItem.message.trim(),
      ruleIdSuffix: ruleIdSuffix,
    };

    validFindings.push(finding);

    core.info(`Added security finding ${validFindings.length}: ${finding.severity} in ${finding.file}:${finding.line}`);

    // Generate/update SARIF file after each finding
    try {
      generateSarifFile();

      // Set outputs for the GitHub Action (these will be overwritten with each call)
      core.setOutput("sarif_file", sarifFilePath);
      core.setOutput("findings_count", validFindings.length);
      core.setOutput("artifact_uploaded", "pending");
      core.setOutput("codeql_uploaded", "pending");
    } catch (error) {
      core.error(`✗ Failed to write SARIF file: ${getErrorMessage(error)}`);
      return {
        success: false,
        error: `Failed to write SARIF file: ${getErrorMessage(error)}`,
      };
    }

    return {
      success: true,
      finding: finding,
      findingsCount: validFindings.length,
      sarifFile: sarifFilePath,
    };
  };
}

module.exports = { main };
