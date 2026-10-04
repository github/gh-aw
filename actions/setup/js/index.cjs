// @ts-check

const { setupGlobals } = require("./setup_globals.cjs");
const { createIssue } = require("./create_issue.cjs");
const { logSpan } = require("./otlp.cjs");

module.exports = { setupGlobals, createIssue, logSpan };
