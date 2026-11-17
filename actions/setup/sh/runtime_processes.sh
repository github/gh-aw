#!/usr/bin/env bash
set +o histexpand

register_gh_aw_process() {
  node - "$1" "$2" <<'NODE'
const fs = require("node:fs");
const path = require("node:path");
if (process.platform !== "linux") {
  console.warn("::warning::Host service identity tracking requires Linux; the Actions runner remains responsible for process teardown");
  process.exit(0);
}
const [pidText, role] = process.argv.slice(2);
const pid = Number(pidText);
if (!Number.isSafeInteger(pid) || pid <= 1 || !process.env.GH_AW_RUNTIME_DIR || !process.env.GH_AW_RUNTIME_ID) {
  throw new Error("Cannot record a service without its PID and private runtime identity");
}
const base = `/proc/${pid}`;
const stat = fs.readFileSync(`${base}/stat`, "utf8");
const start = stat.slice(stat.lastIndexOf(")") + 2).split(" ")[19];
const uid = fs.statSync(base).uid;
const env = fs.readFileSync(`${base}/environ`, "utf8").split("\0");
if (uid !== process.getuid() || !env.includes(`GH_AW_RUNTIME_DIR=${process.env.GH_AW_RUNTIME_DIR}`) ||
    !env.includes(`GH_AW_RUNTIME_ID=${process.env.GH_AW_RUNTIME_ID}`)) {
  throw new Error("Refusing to record a host service without matching invocation ownership");
}
const file = path.join(process.env.GH_AW_RUNTIME_DIR, ".gh-aw-processes.jsonl");
const fd = fs.openSync(file, fs.constants.O_WRONLY | fs.constants.O_APPEND | fs.constants.O_CREAT | fs.constants.O_NOFOLLOW, 0o600);
try {
  const target = fs.fstatSync(fd);
  if (!target.isFile() || target.uid !== uid || target.nlink !== 1) throw new Error("Unsafe host service ownership record");
  fs.writeSync(fd, JSON.stringify({ pid, start, uid, role }) + "\n");
} finally {
  fs.closeSync(fd);
}
NODE
}
