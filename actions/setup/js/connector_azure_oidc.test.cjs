import { describe, expect, it, vi } from "vitest";
import { once } from "node:events";
import fs from "node:fs";
import path from "node:path";
import { randomBytes } from "node:crypto";
import { tokenSource, start } from "./connector_azure_oidc.cjs";
import { startBroker, stopBroker, readToken } from "./connector_credential_broker.cjs";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";

const env = {
  GH_AW_AZURE_TENANT_ID: "00000000-0000-0000-0000-000000000001",
  GH_AW_AZURE_CLIENT_ID: "00000000-0000-0000-0000-000000000002",
  GH_AW_AZURE_SCOPE: "https://monitor.azure.com/.default",
  ACTIONS_ID_TOKEN_REQUEST_URL: "https://issuer.example/assertion?api-version=2&audience=old",
  ACTIONS_ID_TOKEN_REQUEST_TOKEN: "runner-fixture",
};
const json = value => new Response(JSON.stringify(value));
const granted = id => ({ token_type: "Bearer", access_token: "access-" + id, expires_in: 3600 });

describe("trusted connector Entra exchange", () => {
  it.each([
    ["public", "login.microsoftonline.com", ""],
    ["usgovernment", "login.microsoftonline.us", "api://sovereign"],
    ["china", "login.chinacloudapi.cn", "api://sovereign"],
  ])("pins %s authority, assertion audience, client and scope", async (cloud, host, audience) => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(json({ value: "assertion-fixture" }))
      .mockResolvedValueOnce(json(granted(1)));
    const getToken = tokenSource({ ...env, GH_AW_AZURE_CLOUD: cloud, GH_AW_AZURE_AUDIENCE: audience }, fetchImpl);
    expect(fetchImpl).not.toHaveBeenCalled();
    expect(await getToken()).toBe("access-1");
    const [[url, issue], [authority, exchange]] = fetchImpl.mock.calls;
    expect(new URL(url).searchParams.getAll("audience")).toEqual([audience || "api://AzureADTokenExchange"]);
    expect(new URL(url).searchParams.get("api-version")).toBe("2");
    expect(issue.headers.authorization).toBe(["Bearer", env.ACTIONS_ID_TOKEN_REQUEST_TOKEN].join(" "));
    expect(authority).toBe(`https://${host}/${env.GH_AW_AZURE_TENANT_ID}/oauth2/v2.0/token`);
    expect(exchange.headers).toBeUndefined();
    expect(Object.fromEntries(exchange.body)).toEqual({
      client_id: env.GH_AW_AZURE_CLIENT_ID,
      scope: env.GH_AW_AZURE_SCOPE,
      grant_type: "client_credentials",
      client_assertion_type: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
      client_assertion: "assertion-fixture",
    });
    for (const opts of [issue, exchange]) expect(opts).toMatchObject({ redirect: "error", cache: "no-store", credentials: "omit" });
  });

  it("reuses a healthy token, coalesces renewal, and never falls back to expired credentials", async () => {
    let time = 0;
    let issued = 0;
    const fetchImpl = vi.fn(async (_url, init) => {
      if (init.method === "GET") return json({ value: "assertion-" + ++issued });
      if (issued === 3) return new Response("sensitive provider diagnostic", { status: 401 });
      return json(granted(issued));
    });
    const get = tokenSource(env, fetchImpl, () => time);
    expect(await Promise.all([get(), get()])).toEqual(["access-1", "access-1"]);
    time += (3600 - 61) * 1000;
    expect(await get()).toBe("access-1");
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    time += 1000;
    expect(await Promise.all([get(), get()])).toEqual(["access-2", "access-2"]);
    time += 3600 * 1000;
    await expect(get()).rejects.toThrow(/^Connector Azure credential unavailable;/);
    expect(await get()).toBe("access-4");
    expect(fetchImpl).toHaveBeenCalledTimes(8);
  });

  it.each([undefined, 0, -1, "Infinity", "", [100], [], null])("rejects malformed or absent expires_in (%j)", async expiry => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(json({ value: "assertion" }))
      .mockResolvedValueOnce(json({ ...granted(1), expires_in: expiry }));
    await expect(tokenSource(env, fetchImpl)()).rejects.toThrow("unavailable");
  });
  it.each([
    { GH_AW_AZURE_TENANT_ID: "../outside" },
    { GH_AW_AZURE_CLIENT_ID: "not-an-id" },
    { GH_AW_AZURE_SCOPE: "https://example.invalid/.default another/.default" },
    { GH_AW_AZURE_CLOUD: "other.example" },
    { ACTIONS_ID_TOKEN_REQUEST_TOKEN: undefined },
    { ACTIONS_ID_TOKEN_REQUEST_URL: "http://insecure.example" },
  ])("rejects invalid routing before any request", override => {
    const fetchImpl = vi.fn();
    expect(() => tokenSource({ ...env, ...override }, fetchImpl)).toThrow("unavailable");
    expect(fetchImpl).not.toHaveBeenCalled();
  });
  it("fails closed on redirect, oversized response, or malformed assertion", async () => {
    for (const response of [new Response("", { status: 307 }), json({ value: "a".repeat(65537) }), json({ value: "\r\nheader" })]) {
      const fetchImpl = vi.fn().mockResolvedValueOnce(response);
      await expect(tokenSource(env, fetchImpl)()).rejects.toThrow("unavailable");
      expect(fetchImpl).toHaveBeenCalledTimes(1);
    }
  });

  it.skipIf(process.platform === "win32").each([undefined, "/etc/company-ca.pem"])("passes only approved environment to the broker (extra CA: %s)", async extraCA => {
    const root = path.resolve(".az-" + randomBytes(4).toString("hex"));
    fs.mkdirSync(root, { mode: 0o700 });
    const fixture = path.join(root, "broker.cjs");
    const observed = path.join(root, "observed.json");
    const output = path.join(root, "output");
    fs.writeFileSync(fixture, `require("node:fs").writeFileSync(${JSON.stringify(observed)}, JSON.stringify({ env: process.env, execArgv: process.execArgv })); process.send("ready");`);
    const job = {
      RUNNER_TEMP: root,
      GITHUB_OUTPUT: output,
      RUNNER_TRACKING_ID: "job-a",
      GH_AW_CONNECTOR_BROKER_TIMEOUT_MINUTES: "30",
      ALLOWED: "allowed-value",
      NODE_EXTRA_CA_CERTS: extraCA,
      NODE_OPTIONS: "--require=nonexistent-preload",
      NODE_PATH: "untrusted-modules",
      UNRELATED_SECRET: "secret-value",
    };
    vi.stubEnv("PARENT_SECRET", "parent-secret");
    vi.stubEnv("NODE_OPTIONS", "--require=nonexistent-parent-preload");
    try {
      const child = await startBroker(fixture, job, ["ALLOWED"]);
      if (child.exitCode === null && child.signalCode === null) await once(child, "exit");
      const result = JSON.parse(fs.readFileSync(observed, "utf8"));
      expect(result.env).toEqual({
        ALLOWED: "allowed-value",
        RUNNER_TRACKING_ID: "job-a",
        ...(extraCA === undefined ? {} : { NODE_EXTRA_CA_CERTS: extraCA }),
      });
      expect(result.execArgv).toEqual([]);
    } finally {
      vi.unstubAllEnvs();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it.skipIf(process.platform === "win32")("starts a private broker with no identity fetch or token in files or step outputs", async () => {
    const root = path.resolve(".az-" + randomBytes(4).toString("hex"));
    fs.mkdirSync(root, { mode: 0o700 });
    let proc;
    try {
      const output = path.join(root, "output");
      const job = { RUNNER_TEMP: root, GITHUB_OUTPUT: output, RUNNER_TRACKING_ID: "job-a", GH_AW_CONNECTOR_BROKER_TIMEOUT_MINUTES: "30" };
      for (const invalid of [{ RUNNER_TRACKING_ID: "" }, { RUNNER_TRACKING_ID: " " }, { GH_AW_CONNECTOR_BROKER_TIMEOUT_MINUTES: "" }, { GH_AW_CONNECTOR_BROKER_TIMEOUT_MINUTES: "7201" }]) {
        await expect(start({ ...env, ...job, ...invalid })).rejects.toThrow("unavailable");
        expect(fs.readdirSync(root)).toEqual([]);
      }
      proc = await start({ ...env, ...job });
      const directory = fs.readFileSync(output, "utf8").match(/^directory=(.+)\n$/u)[1];
      expect(path.dirname(directory)).toBe(root);
      expect(fs.statSync(directory).mode & 0o077).toBe(0);
      expect(fs.readdirSync(directory)).toEqual(["token.sock"]);
      const socket = fs.lstatSync(path.join(directory, "token.sock"));
      expect(socket.isSocket()).toBe(true);
      expect(socket.mode & 0o077).toBe(0);
      const stopEnv = { ...job, GH_AW_CONNECTOR_BROKER_DIRECTORY: directory };
      expect(() => stopBroker({ ...stopEnv, RUNNER_TRACKING_ID: "job-b" })).toThrow();
      expect(() => stopBroker({ ...stopEnv, GH_AW_CONNECTOR_BROKER_DIRECTORY: path.join(root, "unrelated") })).toThrow();
      const link = directory.slice(0, -6) + "symlnk";
      fs.symlinkSync(directory, link);
      expect(() => stopBroker({ ...stopEnv, GH_AW_CONNECTOR_BROKER_DIRECTORY: link })).toThrow();
      const exited = once(proc, "exit");
      stopBroker(stopEnv);
      await exited;
      proc = undefined;
      expect(fs.existsSync(directory)).toBe(false);
      stopBroker(stopEnv);
    } finally {
      if (proc) {
        const exited = once(proc, "exit");
        proc.kill();
        await exited;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it.skipIf(process.platform === "win32")("automatically retires a serving broker at its original deadline", async () => {
    const tracking = "tracked-job";
    const prefix = "aw-" + createHash("sha256").update(tracking).digest("hex").slice(0, 16) + "-";
    const dir = fs.mkdtempSync(path.resolve(prefix));
    const socket = path.join(dir, "token.sock");
    const child = spawn(
      process.execPath,
      [
        "-e",
        `
      require("./connector_credential_broker.cjs").runBroker(process.argv[1], async () => "bounded-credential", Date.now() + 1000)
    `,
        socket,
      ],
      { env: { RUNNER_TRACKING_ID: tracking }, stdio: ["ignore", "ignore", "ignore", "ipc"] }
    );
    const exited = once(child, "exit");
    try {
      expect((await Promise.race([once(child, "message"), exited]))[0]).toBe("ready");
      expect(await readToken(socket)).toBe("bounded-credential");
      expect(await exited).toEqual([0, null]);
      expect(fs.existsSync(dir)).toBe(false);
      await expect(readToken(socket)).rejects.toThrow("unavailable");
    } finally {
      if (child.exitCode === null && child.signalCode === null) child.kill();
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});
