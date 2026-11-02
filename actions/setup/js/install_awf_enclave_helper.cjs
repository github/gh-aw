const fs = require("node:fs");
const { createHash, randomBytes } = require("node:crypto");
const { execFileSync } = require("node:child_process");

const environment = { PATH: "/usr/bin:/bin", LANG: "C" };

function readRegular(path, limit) {
  const fd = fs.openSync(path, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  try {
    const info = fs.fstatSync(fd);
    if (!info.isFile() || info.nlink !== 1 || info.size <= 0 || info.size > limit) {
      throw new Error("Unsafe helper installer input");
    }
    const data = Buffer.alloc(info.size + 1);
    let length = 0;
    while (length < data.length) {
      const count = fs.readSync(fd, data, length, data.length - length, null);
      if (count === 0) break;
      length += count;
    }
    if (length !== info.size) throw new Error("Invalid helper installer input size");
    return data.subarray(0, length);
  } finally {
    fs.closeSync(fd);
  }
}

function trustedDirectory(fd) {
  const info = fs.fstatSync(fd);
  if (!info.isDirectory() || info.uid !== 0 || info.mode & 0o022) {
    throw new Error("Unsafe helper installation directory");
  }
}

function openDirectory(path, create = false) {
  let fd = fs.openSync("/", fs.constants.O_RDONLY | fs.constants.O_DIRECTORY | fs.constants.O_NOFOLLOW);
  try {
    trustedDirectory(fd);
    for (const component of path.split("/").filter(Boolean)) {
      // Anchor every lookup to the verified directory, like openat(2).
      const childPath = `/proc/self/fd/${fd}/${component}`;
      if (create) {
        try {
          fs.mkdirSync(childPath, { mode: 0o755 });
        } catch (error) {
          if (error.code !== "EEXIST") throw error;
        }
      }
      const child = fs.openSync(childPath, fs.constants.O_RDONLY | fs.constants.O_DIRECTORY | fs.constants.O_NOFOLLOW);
      fs.closeSync(fd);
      fd = child;
      trustedDirectory(fd);
    }
    return fd;
  } catch (error) {
    fs.closeSync(fd);
    throw error;
  }
}

function removeStaging(path) {
  try {
    fs.unlinkSync(path);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
}

function publish(directory, name, data, mode, validate) {
  const staging = `/proc/self/fd/${directory}/${name}.pending-${randomBytes(16).toString("hex")}`;
  try {
    const fd = fs.openSync(staging, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, mode);
    try {
      fs.fchownSync(fd, 0, 0);
      fs.fchmodSync(fd, mode);
      fs.writeFileSync(fd, data);
      fs.fsyncSync(fd);
    } finally {
      fs.closeSync(fd);
    }
    if (validate) validate(staging);
    fs.renameSync(staging, `/proc/self/fd/${directory}/${name}`);
    fs.fsyncSync(directory);
  } finally {
    removeStaging(staging);
  }
}

function installSudoers(uid) {
  const directory = openDirectory("/etc/sudoers.d");
  try {
    const policy = Buffer.from(`#${uid} ALL=(root) NOPASSWD: /usr/bin/python3 -I /var/lib/awf/enclave-storage-helper.py *\n`);
    publish(directory, `awf-enclave-storage-${uid}`, policy, 0o440, staging => {
      // The child cannot resolve this process's non-inherited descriptors.
      const path = staging.replace("/proc/self/", () => `/proc/${process.pid}/`);
      execFileSync("/usr/sbin/visudo", ["-cf", path], { env: environment, stdio: "inherit", timeout: 30000 });
    });
  } finally {
    fs.closeSync(directory);
  }
}

function installAWFEnclaveHelper(args) {
  if (args.length !== 6 || typeof process.getuid !== "function" || typeof process.geteuid !== "function" || process.getuid() !== 0 || process.geteuid() !== 0) {
    throw new Error("Invalid helper installer invocation");
  }
  const [manifestPath, helperPath, manifestSHA256, helperSHA256, version, uid] = args;
  if (manifestSHA256.length !== 64 || helperSHA256.length !== 64 || !/^[0-9a-f]{64}$/.test(manifestSHA256) || !/^[0-9a-f]{64}$/.test(helperSHA256)) {
    throw new Error("Invalid reviewed helper digest");
  }
  if (version.trim() !== version || !/^[0-9]+\.[0-9]+\.[0-9]+$/.test(version)) throw new Error("Invalid helper version");
  if (uid.trim() !== uid || !/^[1-9][0-9]{0,9}$/.test(uid) || Number(uid) > 2147483647) throw new Error("Invalid installing user");
  const manifest = readRegular(manifestPath, 1024 * 1024);
  if (createHash("sha256").update(manifest).digest("hex") !== manifestSHA256) throw new Error("Reviewed checksum manifest changed");
  const helper = readRegular(helperPath, 16 * 1024 * 1024);
  if (createHash("sha256").update(helper).digest("hex") !== helperSHA256) throw new Error("Helper checksum mismatch");
  if (manifest.some(byte => byte > 127)) throw new Error("Invalid checksum manifest encoding");
  const entries = manifest
    .toString("ascii")
    .split(/\r?\n/)
    .map(line => line.trim().split(/\s+/))
    .filter(fields => fields.length === 2 && fields[1] === "enclave-storage-helper.py");
  if (entries.length !== 1 || entries[0][0] !== helperSHA256) throw new Error("Helper checksum is not bound to the reviewed manifest");
  if (!helper.includes(Buffer.from(`VERSION = "${version}"`))) throw new Error("Helper version mismatch");
  const directory = openDirectory("/var/lib/awf", true);
  try {
    fs.fchmodSync(directory, 0o755);
    publish(directory, "enclave-storage-helper.py", helper, 0o644);
  } finally {
    fs.closeSync(directory);
  }
  execFileSync("/usr/bin/python3", ["-I", "/var/lib/awf/enclave-storage-helper.py", "install"], { env: environment, stdio: "inherit", timeout: 120000 });
  installSudoers(uid);
}

module.exports = { installAWFEnclaveHelper, readRegular, openDirectory, publish };

if (require.main === module) {
  try {
    installAWFEnclaveHelper(process.argv.slice(2));
  } catch (error) {
    console.error(`::error::${error.message}`);
    process.exitCode = 1;
  }
}
