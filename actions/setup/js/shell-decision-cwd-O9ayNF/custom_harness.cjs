const result = require("node:child_process").spawnSync(process.execPath, ["-e", "process.stdout.write(process.cwd())"], { cwd: process.env.GH_AW_ENGINE_CWD, encoding: "utf8" });
if (result.error) throw result.error;
process.stdout.write(result.stdout);
process.exitCode = result.status;