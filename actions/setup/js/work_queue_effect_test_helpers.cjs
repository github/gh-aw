// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

function temporaryDirectory(prefix) {
  // macOS's temporary-directory alias must not trip trusted path guards.
  return fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), `gh-aw-${prefix}-`));
}

module.exports = { temporaryDirectory };
