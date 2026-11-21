const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const net = require("node:net");
const { fork, execFileSync } = require("node:child_process");
const { once } = require("node:events");
const { OWNER_LABEL, inspectLease, release, assertOwner, sharedResources } = require("./docker_lease.cjs");

// Opt in only on a disposable daemon; never recover or prune pre-existing state.
describe.skipIf(process.env.GH_AW_DOCKER_LEASE_TEST !== "1")("two jobs sharing a real Docker daemon", () => {
  const children = [];
  let root;
  const docker = args => execFileSync("docker", args, { encoding: "utf8" }).trim();
  beforeEach(() => {
    root = null;
    expect(inspectLease()).toBeNull();
    expect(sharedResources()).toEqual([]);
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-lease-integration-"));
  });
  afterEach(async () => {
    for (const child of children.splice(0)) {
      if (child.exitCode === null && child.signalCode === null) {
        child.kill("SIGKILL");
        await once(child, "exit");
      }
      if (!root) return;
    }
    // Tests remove only a lease whose owner was persisted in their private directory.
    for (const name of fs.readdirSync(root)) {
      const owner = fs.readFileSync(path.join(root, name), "utf8").split("\n")[0].split("=")[1];
      // Label discovery covers a child killed between Docker create and IPC.
      const ids = docker(["container", "ls", "--all", "--no-trunc", "--filter", `label=${OWNER_LABEL}=${owner}`, "--filter", "name=^/awmg-lease-regression$", "--format", "{{.ID}}"]);
      for (const id of ids.split("\n").filter(Boolean)) {
        const actual = docker(["container", "inspect", "--format", `{{index .Config.Labels "${OWNER_LABEL}"}}`, id]);
        if (actual !== owner) throw new Error("Refusing to clean a fixture owned by another invocation.");
        docker(["container", "rm", id]);
      }
      release(owner);
    }
    fs.rmSync(root, { recursive: true, force: true });
  });

  function job(name, port, timeoutMs = 30000) {
    // A real process per job, using the same run/job IDs to exercise matrix copies.
    const child = fork(path.join(__dirname, "docker_lease.cjs"), [], {
      execArgv: [
        "-e",
        `
        const { acquire, release, assertOwner } = require(${JSON.stringify(path.join(__dirname, "docker_lease.cjs"))});
        const { execFileSync } = require("node:child_process");
        const net = require("node:net");
        const docker = args => execFileSync("docker", args, {encoding:"utf8"}).trim();
        (async () => {
          process.send({state:"waiting"});
          const owner = await acquire({timeoutMs:${timeoutMs}});
          const lease = assertOwner(owner);
          const id = docker(["container","create","--name","awmg-lease-regression","--label",${JSON.stringify(`${OWNER_LABEL}=`)} + owner,lease.Image,"/never-executed"]);
          const server = net.createServer(socket => socket.end(owner));
          await new Promise((resolve,reject) => {server.once("error",reject);server.listen(${port},"127.0.0.1",resolve)});
          process.send({state:"running",owner,id});
          process.once("message", async () => {
            assertOwner(owner);
            docker(["container","rm",id]);
            await new Promise(resolve => server.close(resolve));
            release(owner);
            process.exit(0);
          });
        })().catch(error => {process.send({state:"error",message:error.message});process.exit(1)});
      `,
      ],
      env: { ...process.env, GITHUB_ENV: path.join(root, name), GITHUB_STATE: "", GITHUB_RUN_ID: "123", GITHUB_RUN_ATTEMPT: "1", GITHUB_JOB: "agent" },
      silent: true,
    });
    children.push(child);
    return child;
  }

  async function message(child, state) {
    while (true) {
      const [value] = await once(child, "message");
      if (value.state === "error") throw new Error(value.message);
      if (value.state === state) return value;
    }
  }

  it("serializes startup, listener use, and cleanup without terminating the other job", async () => {
    const probe = net.createServer();
    await new Promise(resolve => probe.listen(0, "127.0.0.1", resolve));
    const port = probe.address().port;
    await new Promise(resolve => probe.close(resolve));
    const first = job("first", port);
    const a = await message(first, "running");
    const second = job("second", port);
    const running = message(second, "running");
    expect(assertOwner(a.owner).Id).toBeTruthy();
    const socket = net.connect(port, "127.0.0.1");
    const [data] = await once(socket, "data");
    expect(data.toString()).toBe(a.owner);
    socket.destroy();
    const firstExit = once(first, "exit");
    first.send("finish");
    expect((await firstExit)[0]).toBe(0);
    const b = await running;
    expect(b.owner).not.toBe(a.owner);
    release(a.owner);
    expect(docker(["container", "inspect", "--format", "{{.Id}}", b.id])).toBe(b.id);
    const secondExit = once(second, "exit");
    second.send("finish");
    expect((await secondExit)[0]).toBe(0);
    expect(inspectLease()).toBeNull();
  }, 60000);

  it("fails closed after interruption and never steals the stale owner's resources", async () => {
    const first = job("interrupted", 0);
    const a = await message(first, "running");
    const exit = once(first, "exit");
    first.kill("SIGKILL");
    await exit;
    const second = job("retry", 0, 100);
    const [failure] = await once(second, "message").then(async ([value]) => (value.state === "error" ? [value] : once(second, "message")));
    expect(failure.state).toBe("error");
    expect(failure.message).toContain("Timed out");
    expect(assertOwner(a.owner).Id).toBeTruthy();
    expect(docker(["container", "inspect", "--format", `{{index .Config.Labels "${OWNER_LABEL}"}}`, a.id])).toBe(a.owner);
    // Operator recovery of this test's known, stopped fixture only.
    docker(["container", "rm", a.id]);
    release(a.owner);
    expect(inspectLease()).toBeNull();
  }, 60000);
});
