// Serialize the fixed-name AWF compatibility boundary on a shared Docker daemon.
const { execFileSync } = require("node:child_process");
const { createHash, randomUUID } = require("node:crypto");
const fs = require("node:fs");
const { setTimeout: delay } = require("node:timers/promises");
const { iterateJsonl } = require("./jsonl_helpers.cjs");

const LEASE_NAME = "gh-aw-daemon-lease";
const OWNER_LABEL = "io.github.gh-aw.invocation";
const OWNER_ENV = "GH_AW_DOCKER_OWNER";
const READY_ENV = "GH_AW_DOCKER_READY";

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

function sharedResources(run = docker) {
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

  // An empty, non-executable image needs no registry access or third-party image.
  // It is never started. Its unique label also isolates interrupted acquisition.
  const image = run(["image", "import", "--change", `LABEL ${OWNER_LABEL}=${owner}`, "-"], { input: Buffer.alloc(1024) });
  const metadata = ["GITHUB_REPOSITORY", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB"].flatMap(key => ["--label", `io.github.gh-aw.${key.toLowerCase()}=${env[key] || ""}`]);
  let acquired = false;
  const deadline = Date.now() + timeoutMs;
  try {
    while (true) {
      if (!inspectLease(run)) {
        try {
          run(["container", "create", "--name", LEASE_NAME, "--network", "none", "--label", `${OWNER_LABEL}=${owner}`, ...metadata, image, "/never-executed"]);
          acquired = true;
          break;
        } catch (error) {
          // Only a competing lease is retryable; daemon/create errors fail closed.
          const current = inspectLease(run);
          if (!current) throw error;
          // A timed-out client request may nevertheless have created our container.
          if (current.Config?.Labels?.[OWNER_LABEL] === owner) {
            acquired = true;
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
    requireCleanDaemon(run);
    // A reserved lease alone must not authorize always-run service cleanup when
    // preflight rejected resources belonging to an older/unmanaged invocation.
    persist(env.GITHUB_ENV, `${READY_ENV}=${owner}\n`);
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
  if (lease.Config?.Labels?.[OWNER_LABEL] !== owner) return;
  assertOwner(owner, run);
  requireCleanDaemon(run);
  run(["container", "rm", lease.Id]);
  run(["image", "rm", lease.Image]);
}

async function main(command, env = process.env, service = "") {
  if (env.RUNNER_ENVIRONMENT !== "self-hosted") return;
  if (command === "acquire") await acquire({ env });
  else if (command === "assert") {
    if (!env[OWNER_ENV] || env[READY_ENV] !== env[OWNER_ENV]) {
      throw new Error("Shared-daemon lease is not owned and validated by this invocation; refusing AWF startup or cleanup.");
    }
    assertOwner(env[OWNER_ENV]);
    if (service) assertServiceOwner(env[OWNER_ENV], service);
  } else if (command === "release") release(env[`STATE_${OWNER_ENV}`] || env[OWNER_ENV]);
  else throw new Error(`Unknown Docker lease command: ${command}`);
}

module.exports = { LEASE_NAME, OWNER_LABEL, OWNER_ENV, READY_ENV, invocationOwner, inspectLease, assertOwner, assertServiceOwner, sharedResources, acquire, release, main };

if (require.main === module) {
  main(process.argv[2], process.env, process.argv[3]).catch(error => {
    console.error(`::error::${error instanceof Error ? error.message : "Docker lease operation failed."}`);
    process.exitCode = 1;
  });
}
