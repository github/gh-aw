const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { getErrorMessage } = require("./error_helpers.cjs");

const distribution = "Ubuntu-24.04";
const user = "gh-aw";

function windowsToWSL(value) {
  return value.replace(/^([a-z]):[\\/]/i, (_, drive) => `/mnt/${drive.toLowerCase()}/`).replaceAll("\\", "/");
}

function wslToWindows(value) {
  return value.replace(/\/mnt\/([a-z])\//g, (_, drive) => `${drive.toUpperCase()}:/`);
}

function wslEnvironment(env) {
  // Do not import the Windows executable search path or replace the Linux home.
  const excluded = new Set(["PATH", "HOME", "USER", "LOGNAME", "SHELL", "WSLENV"]);
  const forwarded = Object.keys(env)
    .filter(name => /^[a-z_][a-z0-9_]*$/i.test(name) && !excluded.has(name.toUpperCase()))
    .map(name => `${name}/${/^[a-z]:[\\/]/i.test(env[name]) ? "up" : "u"}`);
  return { ...env, WSLENV: forwarded.join(":") };
}

function invokeWSL(args, options = {}) {
  const result = spawnSync("wsl.exe", args, { stdio: "inherit", timeout: 900_000, ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`WSL command failed (exit ${result.status})`);
}

function setupWindowsRuntime() {
  const listed = spawnSync("wsl.exe", ["--list", "--quiet"], { encoding: "utf16le", timeout: 30_000 });
  if (listed.error) throw listed.error;
  const distributions = (listed.stdout || "")
    .replaceAll("\0", "")
    .split(/\r?\n/)
    .map(name => name.trim());
  if (!distributions.includes(distribution)) {
    invokeWSL(["--install", "--distribution", distribution, "--no-launch"]);
  }
  invokeWSL(["--set-version", distribution, "2"]);
  // The runner's Node release is trusted; authenticate the Linux archive against
  // its official release manifest before installing it in the WSL distribution.
  invokeWSL(["--distribution", distribution, "--user", "root", "--exec", "bash", "-euo", "pipefail", "-s", "--", process.versions.node], {
    input: `
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl docker.io docker-compose-v2 git gh jq sudo unzip xz-utils
id -u gh-aw >/dev/null 2>&1 || useradd --create-home --shell /bin/bash gh-aw
usermod -aG docker gh-aw
printf 'gh-aw ALL=(ALL) NOPASSWD:ALL\\n' > /etc/sudoers.d/gh-aw
chmod 0440 /etc/sudoers.d/gh-aw
service docker start
test "$(docker info --format '{{.OSType}}')" = linux
case "$(uname -m)" in
  x86_64) arch=x64 ;;
  aarch64) arch=arm64 ;;
  *) echo "Unsupported WSL architecture" >&2; exit 1 ;;
esac
archive="node-v$1-linux-$arch.tar.xz"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' -O "https://nodejs.org/dist/v$1/$archive"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' -O "https://nodejs.org/dist/v$1/SHASUMS256.txt"
awk -v archive="$archive" '$2 == archive { print }' SHASUMS256.txt > checksum
test "$(wc -l < checksum)" -eq 1
sha256sum --check checksum
tar -xJf "$archive" --strip-components=1 -C /usr/local
runuser -u gh-aw -- /usr/local/bin/npm config set prefix /home/gh-aw/.local
mkdir -p /tmp/gh-aw
chown gh-aw:gh-aw /tmp/gh-aw
`,
    stdio: ["pipe", "inherit", "inherit"],
  });

  // Native JS actions and artifact uploaders must see the same runtime tree.
  // Keep it separate from RUNNER_TEMP's read-only scripts/configuration tree.
  const sharedPath = path.resolve("/tmp/gh-aw");
  const target = `\\\\wsl.localhost\\${distribution}\\tmp\\gh-aw`;
  try {
    fs.mkdirSync(path.dirname(sharedPath), { recursive: true });
    try {
      if (fs.readlinkSync(sharedPath) === target) return;
      throw new Error(`Refusing to replace existing runtime path: ${sharedPath}`);
    } catch (error) {
      if (!(error instanceof Error)) throw error;
      if (!("code" in error) || error.code !== "ENOENT") throw error;
    }
    fs.symlinkSync(target, sharedPath, "dir");
  } catch (error) {
    throw new Error(`WSL runtime sharing requires a Windows directory symlink at ${sharedPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function runWSLScript(script, env = process.env) {
  try {
    // The Actions custom-shell script may have Windows line endings or a BOM.
    const normalized = `${script}.wsl.sh`;
    fs.writeFileSync(
      normalized,
      fs
        .readFileSync(script, "utf8")
        .replace(/^\uFEFF/, "")
        .replaceAll("\r\n", "\n")
    );
    const pathFile = path.join(env.RUNNER_TEMP, "gh-aw", "wsl-path");
    const githubPath = env.GITHUB_PATH;
    const isolatedPath = `${normalized}.path`;
    const linuxEnv = { ...env, GITHUB_PATH: isolatedPath };
    fs.mkdirSync(path.dirname(pathFile), { recursive: true });
    fs.writeFileSync(isolatedPath, "");
    try {
      const result = spawnSync(
        "wsl.exe",
        [
          "--distribution",
          distribution,
          "--user",
          user,
          "--exec",
          "bash",
          "--noprofile",
          "--norc",
          "-e",
          "-o",
          "pipefail",
          "-c",
          `export PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin"
if [ -f "$RUNNER_TEMP/gh-aw/wsl-path" ]; then
  while IFS= read -r entry; do export PATH="$entry:$PATH"; done < "$RUNNER_TEMP/gh-aw/wsl-path"
fi
exec bash --noprofile --norc -e -o pipefail "$1"`,
          "gh-aw",
          windowsToWSL(normalized),
        ],
        { stdio: "inherit", env: wslEnvironment(linuxEnv), timeout: 21_600_000 }
      );
      if (result.error) throw result.error;
      return result.status ?? 1;
    } finally {
      for (const file of [env.GITHUB_ENV, env.GITHUB_OUTPUT]) {
        if (file && fs.existsSync(file)) fs.writeFileSync(file, wslToWindows(fs.readFileSync(file, "utf8")));
      }
      const entries = fs.readFileSync(isolatedPath, "utf8");
      fs.appendFileSync(pathFile, entries);
      if (githubPath) {
        const hostEntries = entries.split(/\r?\n/).filter(entry => entry.startsWith("/mnt/"));
        if (hostEntries.length) fs.appendFileSync(githubPath, `${wslToWindows(hostEntries.join("\n"))}\n`);
      }
      fs.rmSync(normalized, { force: true });
      fs.rmSync(isolatedPath, { force: true });
    }
  } catch (error) {
    throw new Error(`WSL script dispatch failed: ${getErrorMessage(error)}`, { cause: error });
  }
}

function cleanupWindowsRuntime() {
  invokeWSL(["--distribution", distribution, "--user", "root", "--exec", "rm", "-rf", "--", "/tmp/gh-aw"]);
}

module.exports = { windowsToWSL, wslToWindows, wslEnvironment, setupWindowsRuntime, runWSLScript, cleanupWindowsRuntime };

if (require.main === module) {
  try {
    process.exitCode = runWSLScript(process.argv[2]);
  } catch (error) {
    console.error(`Windows WSL runtime failed: ${getErrorMessage(error)}`);
    process.exitCode = 1;
  }
}
