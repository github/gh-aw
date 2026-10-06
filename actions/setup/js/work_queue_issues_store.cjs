// @ts-check
"use strict";

const { queueError } = require("./work_queue_codec.cjs");

function unsupportedIssuesStorage() {
  throw queueError("unsupported_backend", "Issues storage cannot serialize a fair queue; use the Git work-queue backend");
}

module.exports = {
  readIssues: unsupportedIssuesStorage,
  applyAndPublishIssues: unsupportedIssuesStorage,
  serializeRecord: unsupportedIssuesStorage,
  record: unsupportedIssuesStorage,
};
