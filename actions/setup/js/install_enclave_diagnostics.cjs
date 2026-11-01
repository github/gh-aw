const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

// Host Node runs rootless; only isolated system Python installs the protected
// interpreter and collector. Never elevate the runner's toolcache Node.
const TRUSTED_INSTALLER = String.raw`
import hashlib, io, os, stat, sys, tarfile

def reject(category):
    sys.exit("::error::Trusted enclave collector rejected: " + category)

def protected_directory(path):
    info = os.lstat(path)
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
        reject("unsafe-directory")

def read_regular(path, limit):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            reject("unsafe-file")
        if not 0 < info.st_size <= limit:
            reject("file-size")
        data = source.read(limit + 1)
    if len(data) != info.st_size:
        reject("file-size")
    return data

def install_verified(path, data, mode):
    if os.path.lexists(path):
        info = os.lstat(path)
        if info.st_uid != 0 or stat.S_IMODE(info.st_mode) != mode:
            reject("unsafe-installed-file")
        if read_regular(path, len(data)) != data:
            reject("installed-checksum")
        return
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, mode)
    with os.fdopen(fd, "wb") as destination:
        destination.write(data)
        os.fchmod(destination.fileno(), mode)

try:
    pins = {
        "x86_64": ("x64", "6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6"),
        "aarch64": ("arm64", "dbabf7a1cbf947e6059ffad2c27b80041db97dea1e553b62afdee7f13fca336e"),
    }
    if os.uname().machine not in pins:
        reject("unsupported-architecture")
    arch, digest = pins[os.uname().machine]
    data = read_regular(sys.argv[1], 128 * 1024 * 1024)
    if hashlib.sha256(data).hexdigest() != digest:
        reject("archive-checksum")
    collector = read_regular(sys.argv[2], 1024 * 1024)
    if hashlib.sha256(collector).hexdigest() != "7722affc5c076d79692d4268a543ef267281424ad4b374eb8f80c24cbec08cae":
        reject("collector-checksum")
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:xz") as archive:
        member = archive.getmember("node-v24.22.1-linux-" + arch + "/bin/node")
        if not member.isfile() or not member.mode & 0o111:
            reject("unsafe-executable")
        if not 0 < member.size <= 256 * 1024 * 1024:
            reject("executable-size")
        executable = archive.extractfile(member).read(member.size + 1)
        if len(executable) != member.size:
            reject("executable-size")
    root = "/var/lib/awf-enclave-smoke-export"
    for directory in ("/", "/var", "/var/lib"):
        protected_directory(directory)
    if not os.path.lexists(root):
        os.mkdir(root, 0o711)
        os.chmod(root, 0o711)
    protected_directory(root)
    install_verified(root + "/node", executable, 0o500)
    install_verified(root + "/collector.cjs", collector, 0o600)
except OSError:
    reject("filesystem-check")
except (tarfile.TarError, KeyError, EOFError):
    reject("archive-member")
`;

function installEnclaveDiagnostics() {
  if (process.getuid() === 0) {
    throw new Error("The enclave diagnostic bootstrap must run rootless");
  }
  if (!process.env.RUNNER_TEMP) {
    throw new Error("RUNNER_TEMP is required");
  }
  if (!["x64", "arm64"].includes(process.arch)) {
    throw new Error("Unsupported collector Node architecture");
  }
  const directory = path.join(process.env.RUNNER_TEMP, `awf-collector-install-${process.pid}`);
  try {
    fs.mkdirSync(directory, { mode: 0o700 });
  } catch (cause) {
    throw new Error("Collector installation staging could not be created", { cause });
  }
  try {
    const archive = path.join(directory, "node.tar.xz");
    const collector = path.join(directory, "collector.cjs");
    const download = (url, destination, limit) => {
      try {
        execFileSync("/usr/bin/curl", ["--proto", "=https", "--proto-redir", "=https", "-fsSL", "--max-time", "120", "--max-filesize", String(limit), url, "-o", destination], { stdio: "inherit", timeout: 130000 });
      } catch (cause) {
        throw new Error("Trusted collector download failed", { cause });
      }
    };
    download(`https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-${process.arch}.tar.xz`, archive, 128 * 1024 * 1024);
    download("https://raw.githubusercontent.com/github/gh-aw-firewall/3929c9a15c0f239b772affcaae6e55fec92f41c2/scripts/ci/collect-enclave-smoke-debug.cjs", collector, 1024 * 1024);
    execFileSync("/usr/bin/sudo", ["-n", "--", "/usr/bin/python3", "-I", "-", archive, collector], {
      input: TRUSTED_INSTALLER,
      stdio: ["pipe", "inherit", "inherit"],
      env: { PATH: "/usr/bin:/bin", LANG: "C", LC_ALL: "C" },
      timeout: 120000,
    });
  } catch (cause) {
    throw new Error("Protected collector runtime installation failed", { cause });
  } finally {
    try {
      fs.rmSync(directory, { recursive: true, force: true });
    } catch (cause) {
      throw new Error("Collector installation staging cleanup failed", { cause });
    }
  }
}

module.exports = { TRUSTED_INSTALLER, installEnclaveDiagnostics };

if (require.main === module) {
  try {
    installEnclaveDiagnostics();
  } catch {
    console.error("::error::Trusted enclave collector bootstrap failed");
    process.exitCode = 1;
  }
}
