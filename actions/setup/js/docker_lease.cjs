// Serialize the fixed-name AWF compatibility boundary on a shared Docker daemon.
const { execFileSync } = require("node:child_process");
const { createHash, randomUUID } = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { setTimeout: delay } = require("node:timers/promises");
const { iterateJsonl } = require("./jsonl_helpers.cjs");

const LEASE_NAME = "gh-aw-daemon-lease";
const OWNER_LABEL = "io.github.gh-aw.invocation";
const OWNER_ENV = "GH_AW_DOCKER_OWNER";
const READY_ENV = "GH_AW_DOCKER_READY";
const MCP_SCRIPTS_PID_FILE = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "actions", "mcp-scripts-server.pid");
const DIFC_PROXY_STATE_FILE = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "actions", "proxy-host-state");

function docker(args, options = {}) {
  try {
    return execFileSync("docker", args, { encoding: "utf8", timeout: 30000, stdio: ["pipe", "pipe", "pipe"], ...options }).trim();
  } catch (cause) {
    // Docker stderr may contain daemon configuration; do not expose it.
    throw new Error("Docker lease operation failed; shared-daemon execution is blocked.", { cause });
  }
}

function inspectContainer(name, run) {
  try {
    const records = JSON.parse(run(["container", "inspect", name]));
    if (!Array.isArray(records) || records.length !== 1) throw new Error("Expected one container.");
    return records[0];
  } catch (cause) {
    throw new Error("Unable to inspect Docker lease ownership.", { cause });
  }
}

function persist(file, text) {
  try {
    fs.appendFileSync(file, text);
  } catch (cause) {
    throw new Error("Unable to persist Docker lease ownership.", { cause });
  }
}

function errorCode(error) {
  return error && typeof error === "object" && "code" in error ? error.code : undefined;
}

function invocationOwner(env = process.env) {
  return createHash("sha256")
    .update(JSON.stringify([env.GITHUB_REPOSITORY, env.GITHUB_RUN_ID, env.GITHUB_RUN_ATTEMPT, env.GITHUB_JOB, randomUUID()]))
    .digest("hex");
}

function inspectLease(run = docker) {
  // Listing first distinguishes absence from permission/daemon failures.
  const listArgs = ["container", "ls", "--all", "--filter", `name=^/${LEASE_NAME}$`, "--format", "{{.ID}}"];
  const ids = run(listArgs);
  if (!ids) return null;
  try {
    return inspectContainer(LEASE_NAME, run);
  } catch (error) {
    // The previous owner can finish between ls and inspect.
    if (!run(listArgs)) return null;
    throw error;
  }
}

function assertOwner(owner, run = docker) {
  const lease = inspectLease(run);
  if (!/^[a-f0-9]{64}$/.test(owner || "") || lease?.Config?.Labels?.[OWNER_LABEL] !== owner) {
    throw new Error("Shared-daemon lease is not owned by this invocation; refusing AWF startup or cleanup.");
  }
  if (lease.State?.Running !== true) {
    throw new Error("Shared-daemon lease container is not running; refusing AWF startup or cleanup.");
  }
  return lease;
}

function assertServiceOwner(owner, name, run = docker) {
  if (!["awmg-mcpg", "awmg-cli-proxy", "awmg-proxy"].includes(name)) throw new Error("Unknown shared-daemon service.");
  const ids = run(["container", "ls", "--all", "--filter", `name=^/${name}$`, "--format", "{{.ID}}"]);
  if (!ids) return;
  const service = inspectContainer(name, run);
  if (service?.Config?.Labels?.[OWNER_LABEL] !== owner) {
    throw new Error(`Refusing to replace or clean ${name}: its invocation label does not match the current owner.`);
  }
}

function sharedResources(run = docker, listenerPidFile = MCP_SCRIPTS_PID_FILE) {
  const found = [];
  for (const kind of ["container", "network", "volume"]) {
    const args = [kind, "ls"];
    if (kind === "container") args.push("--all");
    const rows = run([...args, "--format", "{{json .}}"]);
    for (const row of iterateJsonl(rows, { strict: true })) {
      const name = row.Names || row.Name;
      if (/^(awf|awmg)-/.test(name) || /(?:^|,)com\.docker\.compose\.project=awf-/.test(row.Labels || "")) {
        found.push(`${kind}:${name}`);
      }
    }
  }
  try {
    const stat = fs.lstatSync(listenerPidFile);
    const [owner, pidText] = fs.readFileSync(listenerPidFile, "utf8").trim().split("\t");
    const pid = Number(pidText);
    if (!stat.isFile() || stat.isSymbolicLink() || !/^[a-f0-9]{64}$/.test(owner) || !Number.isInteger(pid) || pid <= 1) {
      found.push("host-listener:mcp-scripts-server:unknown");
    } else {
      try {
        process.kill(pid, 0);
        found.push(`host-listener:mcp-scripts-server:${pid}`);
      } catch (error) {
        if (errorCode(error) === "EPERM") found.push(`host-listener:mcp-scripts-server:${pid}`);
      }
    }
  } catch (error) {
    if (errorCode(error) !== "ENOENT") found.push("host-listener:mcp-scripts-server:unknown");
  }
  try {
    const stat = fs.lstatSync(DIFC_PROXY_STATE_FILE);
    if (stat.isFile() && !stat.isSymbolicLink()) found.push("host-state:difc-proxy");
    else found.push("host-state:difc-proxy:unknown");
  } catch (error) {
    if (errorCode(error) !== "ENOENT") found.push("host-state:difc-proxy:unknown");
  }
  return found;
}

function requireCleanDaemon(run = docker) {
  const resources = sharedResources(run);
  if (resources.length) {
    throw new Error(`AWF resources remain (${resources.join(", ")}). Lease retained; an administrator must confirm the owner has stopped before recovery.`);
  }
}

async function acquire({ env = process.env, run = docker, pause = delay, timeoutMs = 600000 } = {}) {
  const owner = invocationOwner(env);
  if (!env.GITHUB_ENV) throw new Error("GITHUB_ENV is required to persist the shared-daemon lease owner.");
  // Persist before Docker mutation, so the always/post path can recover a setup failure.
  persist(env.GITHUB_ENV, `${OWNER_ENV}=${owner}\n`);
  if (env.GITHUB_STATE) persist(env.GITHUB_STATE, `${OWNER_ENV}=${owner}\n`);

  const architecture = run(["info", "--format", "{{.Architecture}}"]).toLowerCase();
  const executable = pauseExecutable(architecture);
  const archive = tarArchive("gh-aw-lease", executable, 0o755);
  const image = run(["image", "import", "--change", `LABEL ${OWNER_LABEL}=${owner}`, "-"], { input: archive });
  const metadata = ["GITHUB_REPOSITORY", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB"].flatMap(key => ["--label", `io.github.gh-aw.${key.toLowerCase()}=${env[key] || ""}`]);
  let acquired = false;
  const deadline = Date.now() + timeoutMs;
  try {
    while (true) {
      if (!inspectLease(run)) {
        try {
          run(["container", "create", "--name", LEASE_NAME, "--network", "none", "--restart", "unless-stopped", "--label", `${OWNER_LABEL}=${owner}`, ...metadata, image, "/gh-aw-lease"]);
          acquired = true;
          run(["container", "start", LEASE_NAME]);
          break;
        } catch (error) {
          // Only a competing lease is retryable; daemon/create errors fail closed.
          const current = inspectLease(run);
          if (!current) throw error;
          // A timed-out client request may nevertheless have created our container.
          if (current.Config?.Labels?.[OWNER_LABEL] === owner) {
            acquired = true;
            if (current.State?.Running !== true) run(["container", "start", LEASE_NAME]);
            break;
          }
        }
      }
      if (Date.now() >= deadline) {
        throw new Error("Timed out waiting for the shared-daemon AWF lease. It may belong to a running or interrupted job; never remove it without checking its owner.");
      }
      await pause(Math.min(1000, Math.max(0, deadline - Date.now())));
    }
    assertOwner(owner, run);
    if (inspectLease(run)?.State?.Running !== true) {
      throw new Error("Unable to start the shared-daemon lease container; setup is blocked.");
    }
    requireCleanDaemon(run);
    // A reserved lease alone must not authorize always-run service cleanup when
    // preflight rejected resources belonging to an older/unmanaged invocation.
    persist(env.GITHUB_ENV, `${READY_ENV}=${owner}\n`);
    if (env.GITHUB_STATE) persist(env.GITHUB_STATE, `${READY_ENV}=${owner}\n`);
    return owner;
  } finally {
    // An acquired lease (including failed setup) is released only by always/post.
    if (!acquired) run(["image", "rm", image]);
  }
}

function release(owner, run = docker) {
  const lease = inspectLease(run);
  if (!lease) return;
  // Never remove a successor's lease, including a delayed post from an older retry.
  if (!/^[a-f0-9]{64}$/.test(owner || "") || lease.Config?.Labels?.[OWNER_LABEL] !== owner) return;
  requireCleanDaemon(run);
  run(["container", "rm", "--force", lease.Id]);
  run(["image", "rm", lease.Image]);
}

function pauseExecutable(architecture) {
  const machines = {
    x86_64: { machine: 62, code: [0xb8, 0x22, 0, 0, 0, 0x0f, 0x05, 0xeb, 0xf7] },
    amd64: { machine: 62, code: [0xb8, 0x22, 0, 0, 0, 0x0f, 0x05, 0xeb, 0xf7] },
    aarch64: { machine: 183, code: [0xa8, 0x03, 0x80, 0xd2, 0x01, 0x00, 0x00, 0xd4, 0xfe, 0xff, 0xff, 0x17] },
    arm64: { machine: 183, code: [0xa8, 0x03, 0x80, 0xd2, 0x01, 0x00, 0x00, 0xd4, 0xfe, 0xff, 0xff, 0x17] },
  };
  const target = machines[architecture];
  if (!target) throw new Error(`Unsupported Docker daemon architecture for shared-daemon lease: ${architecture || "unknown"}.`);
  const code = Buffer.from(target.code);
  const header = Buffer.alloc(120 + code.length);
  header.set([0x7f, 0x45, 0x4c, 0x46, 2, 1, 1], 0);
  header.writeUInt16LE(2, 16);
  header.writeUInt16LE(target.machine, 18);
  header.writeUInt32LE(1, 20);
  header.writeBigUInt64LE(0x400078n, 24);
  header.writeBigUInt64LE(64n, 32);
  header.writeUInt16LE(64, 52);
  header.writeUInt16LE(56, 54);
  header.writeUInt16LE(1, 56);
  header.writeUInt32LE(1, 64);
  header.writeUInt32LE(5, 68);
  header.writeBigUInt64LE(0n, 72);
  header.writeBigUInt64LE(0x400000n, 80);
  header.writeBigUInt64LE(0x400000n, 88);
  header.writeBigUInt64LE(BigInt(header.length), 96);
  header.writeBigUInt64LE(BigInt(header.length), 104);
  header.writeBigUInt64LE(0x1000n, 112);
  code.copy(header, 120);
  return header;
}

function tarArchive(name, contents, mode) {
  const header = Buffer.alloc(512);
  header.write(name, 0, 100, "utf8");
  header.write(mode.toString(8).padStart(7, "0") + "\0", 100, 8, "ascii");
  header.write("0000000\0", 108, 8, "ascii");
  header.write("0000000\0", 116, 8, "ascii");
  header.write(contents.length.toString(8).padStart(11, "0") + "\0", 124, 12, "ascii");
  header.write("00000000000\0", 136, 12, "ascii");
  header.fill(0x20, 148, 156);
  header[156] = 0x30;
  header.write("ustar\0", 257, 6, "ascii");
  header.write("00", 263, 2, "ascii");
  const checksum = header.reduce((sum, byte) => sum + byte, 0);
  header.write(checksum.toString(8).padStart(6, "0") + "\0 ", 148, 8, "ascii");
  const paddedContents = Buffer.alloc(Math.ceil(contents.length / 512) * 512);
  contents.copy(paddedContents);
  return Buffer.concat([header, paddedContents, Buffer.alloc(1024)]);
}

async function main(command, env = process.env, service = "") {
  if (env.RUNNER_ENVIRONMENT !== "self-hosted") return;
  if (command === "acquire") await acquire({ env });
  else if (command === "assert") {
    const owner = env[OWNER_ENV] || env[`STATE_${OWNER_ENV}`];
    const ready = env[READY_ENV] || env[`STATE_${READY_ENV}`];
    if (!owner || ready !== owner) {
      throw new Error("Shared-daemon lease is not owned and validated by this invocation; refusing AWF startup or cleanup.");
    }
    assertOwner(owner);
    if (service) assertServiceOwner(owner, service);
  } else if (command === "release") release(env[`STATE_${OWNER_ENV}`] || env[OWNER_ENV]);
  else throw new Error(`Unknown Docker lease command: ${command}`);
}

module.exports = { LEASE_NAME, OWNER_LABEL, OWNER_ENV, READY_ENV, invocationOwner, inspectLease, assertOwner, assertServiceOwner, sharedResources, acquire, release, pauseExecutable, tarArchive, main };

if (require.main === module) {
  main(process.argv[2], process.env, process.argv[3]).catch(error => {
    console.error(`::error::${error instanceof Error ? error.message : "Docker lease operation failed."}`);
    process.exitCode = 1;
  });
}
