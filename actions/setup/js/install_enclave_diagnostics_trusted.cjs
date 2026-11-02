const fs = require("node:fs");
const { createHash } = require("node:crypto");

function reject(category) {
  throw new Error(`Trusted enclave collector rejected: ${category}`);
}

function protectedDirectory(path) {
  const info = fs.lstatSync(path);
  if (!info.isDirectory() || info.uid !== 0 || info.mode & 0o022) reject("unsafe-directory");
}

function readRegular(path, limit) {
  const fd = fs.openSync(path, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  try {
    const info = fs.fstatSync(fd);
    if (!info.isFile() || info.nlink !== 1) reject("unsafe-file");
    if (info.size <= 0 || info.size > limit) reject("file-size");
    const data = Buffer.alloc(info.size + 1);
    let length = 0;
    while (length < data.length) {
      const count = fs.readSync(fd, data, length, data.length - length, null);
      if (count === 0) break;
      length += count;
    }
    if (length !== info.size) reject("file-size");
    return data.subarray(0, length);
  } finally {
    fs.closeSync(fd);
  }
}

function installVerified(path, data, mode) {
  let info;
  try {
    info = fs.lstatSync(path);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  if (info) {
    if (info.uid !== 0 || (info.mode & 0o7777) !== mode) reject("unsafe-installed-file");
    if (!readRegular(path, data.length).equals(data)) reject("installed-checksum");
    return;
  }
  const fd = fs.openSync(path, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, mode);
  try {
    fs.writeFileSync(fd, data);
    fs.fchmodSync(fd, mode);
  } finally {
    fs.closeSync(fd);
  }
}

function installEnclaveDiagnosticsTrusted(args) {
  const pins = {
    x64: "e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb",
    arm64: "c8c5721679d8d1a09e9987e345d5f9a64899bb85f7a384783ba8a93e6e62348a",
  };
  if (typeof process.getuid !== "function" || typeof process.geteuid !== "function" || process.getuid() !== 0 || process.geteuid() !== 0) reject("bootstrap-must-run-root");
  if (args.length !== 2 || !Object.hasOwn(pins, process.arch)) reject("unsupported-architecture");
  const executable = readRegular(args[0], 256 * 1024 * 1024);
  if (createHash("sha256").update(executable).digest("hex") !== pins[process.arch]) reject("node-checksum");
  const collector = readRegular(args[1], 1024 * 1024);
  if (createHash("sha256").update(collector).digest("hex") !== "7722affc5c076d79692d4268a543ef267281424ad4b374eb8f80c24cbec08cae") reject("collector-checksum");
  const root = "/var/lib/awf-enclave-smoke-export";
  for (const directory of ["/", "/var", "/var/lib"]) protectedDirectory(directory);
  try {
    fs.mkdirSync(root, { mode: 0o711 });
    fs.chmodSync(root, 0o711);
  } catch (error) {
    if (error.code !== "EEXIST") throw error;
  }
  protectedDirectory(root);
  installVerified(`${root}/node`, executable, 0o500);
  installVerified(`${root}/collector.cjs`, collector, 0o600);
}

module.exports = { installEnclaveDiagnosticsTrusted, readRegular, protectedDirectory, installVerified };

if (require.main === module) {
  try {
    installEnclaveDiagnosticsTrusted(process.argv.slice(2));
  } catch (error) {
    console.error(`::error::${error.message}`);
    process.exitCode = 1;
  }
}
