"use strict";

const manifest = require("../.github/ci/go-unit-slow-tests.json");

function slowPattern(shard = "all") {
  const tests = manifest.tests.filter(test => shard === "all" || test.shard === shard);
  if (!tests.length) throw new Error(`Unknown or empty slow unit-test shard: ${shard}`);
  if (tests.some(test => !/^Test[A-Za-z0-9_]+$/.test(test.name))) throw new Error("Invalid slow unit-test name");
  return `^(${tests.map(test => test.name).join("|")})$`;
}

if (require.main === module) {
  try {
    if (process.argv.length > 3) throw new Error("Usage: node scripts/go-unit-suite.cjs [shard]");
    console.log(slowPattern(process.argv[2]));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}

module.exports = { slowPattern };
