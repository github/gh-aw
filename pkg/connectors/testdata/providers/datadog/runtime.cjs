const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { decodePlan, executeConnector } = require(process.argv[2]);
const { reader, writer, monitor, incident } = JSON.parse(readFileSync(0, "utf8"));
const incidentID = "11111111-2222-4333-8444-555555555555";
const otherIncidentID = "99999999-2222-4333-8444-555555555555";
const origin = "https://api.datadoghq.com";
const token = "synthetic-oauth-access-token";
const response = (body, status = 200) => new Response(body, { status, headers: { "content-type": "application/json", "retry-after": "1" } });
let checks = 0;

function phaseEnvironment(expected) {
  let resolutions = 0;
  const env = new Proxy(
    {},
    {
      get(_target, name) {
        if (!String(name).startsWith("GH_AW_CONNECTOR_SECRET_")) return undefined;
        assert.equal(name, "GH_AW_CONNECTOR_SECRET_" + expected);
        resolutions++;
        return token;
      },
    }
  );
  return { env, resolutions: () => resolutions };
}

function requestChecks(url, init, method, path) {
  assert.equal(url, origin + path);
  assert.equal(init.method, method);
  assert.equal(init.redirect, "manual");
  assert.equal(init.credentials, "omit");
  const headers = new Headers(init.headers);
  assert.equal(headers.get("authorization"), "Bearer " + token);
  assert.equal(headers.get("dd-api-key"), null);
  assert.equal(headers.get("dd-application-key"), null);
}

async function checkReads() {
  for (const [operation, args, secret, body, path, expected] of [
    ["get-monitor", { "monitor-id": "12345678" }, "DATADOG_MONITOR_READ_OAUTH_TOKEN", monitor, "/api/v1/monitor/12345678", { "monitor-id": 12345678, name: "Build latency", type: "metric alert", state: "Alert" }],
    [
      "get-monitor",
      { "monitor-id": "999999999999999" },
      "DATADOG_MONITOR_READ_OAUTH_TOKEN",
      monitor.replace('"id": 12345678', '"id": 999999999999999'),
      "/api/v1/monitor/999999999999999",
      { "monitor-id": 999999999999999, name: "Build latency", type: "metric alert", state: "Alert" },
    ],
    ["get-incident", { "incident-id": incidentID }, "DATADOG_INCIDENT_READ_OAUTH_TOKEN", incident, "/api/v2/incidents/" + incidentID, { "incident-id": incidentID, title: "Build latency investigation" }],
  ]) {
    let calls = 0;
    const credentials = phaseEnvironment(secret);
    const result = await executeConnector(reader, "mcp", operation, args, {
      env: credentials.env,
      fetchImpl: async (url, init) => {
        calls++;
        requestChecks(url, init, "GET", path);
        return response(body);
      },
    });
    assert.equal(result.untrusted, true);
    assert.deepEqual({ ...result.data }, expected);
    assert.equal(JSON.stringify(result).includes("synthetic-private"), false);
    assert.equal(credentials.resolutions(), 1);
    assert.equal(calls, 1);
    checks++;
  }
}

async function checkStagingAndTitleEffect() {
  const title = "Literal $(not-code) ${{ not.an.expression }}";
  const args = { "incident-id": incidentID, title };
  const noAccess = new Proxy({}, { get: () => assert.fail("staging must not access the environment") });
  const intent = await executeConnector(writer, "safe-output", "rename-incident", args, {
    staged: true,
    env: noAccess,
    fetchImpl: () => assert.fail("staging must not contact Datadog"),
  });
  assert.deepEqual({ ...intent.request.arguments }, args);
  assert.equal(intent.staged, true);
  assert.equal(intent.request.digest, decodePlan(writer).digest);
  assert.equal(JSON.stringify(intent).includes("OAUTH_TOKEN"), false);
  checks++;

  let calls = 0;
  const credentials = phaseEnvironment("DATADOG_INCIDENT_WRITE_OAUTH_TOKEN");
  const result = await executeConnector(writer, "safe-output", "rename-incident", args, {
    env: credentials.env,
    fetchImpl: async (url, init) => {
      calls++;
      requestChecks(url, init, "PATCH", "/api/v2/incidents/" + incidentID);
      assert.equal(new Headers(init.headers).get("content-type"), "application/json");
      assert.deepEqual(JSON.parse(init.body), { data: { id: incidentID, type: "incidents", attributes: { title } } });
      return response(incident.replace("Build latency investigation", title));
    },
  });
  assert.deepEqual({ ...result.data }, { "incident-id": incidentID, title });
  assert.equal(result.untrusted, true);
  assert.equal(credentials.resolutions(), 1);
  assert.equal(calls, 1);
  checks++;
}

async function checkArgumentBoundaries() {
  const noAccess = new Proxy({}, { get: () => assert.fail("denied requests must not resolve secrets") });
  const options = { staged: true, env: noAccess, fetchImpl: () => assert.fail("denied requests must not use the network") };
  for (const args of [
    { "incident-id": "77777777-2222-4333-8444-555555555555", title: "title" },
    { "incident-id": "../" + incidentID, title: "title" },
    { "incident-id": incidentID, title: "" },
    { "incident-id": incidentID, title: " \n\t" },
    { "incident-id": incidentID, title: "x".repeat(129) },
    { "incident-id": incidentID, title: "\u00e9".repeat(129) },
    { "incident-id": incidentID, title: "title", state: "resolved" },
    { "incident-id": incidentID, title: "title", url: "https://untrusted.invalid" },
    { "incident-id": incidentID, title: "title", credential: "other" },
    { "incident-id": incidentID },
  ]) {
    await assert.rejects(executeConnector(writer, "safe-output", "rename-incident", args, options));
    checks++;
  }
  const maximum = await executeConnector(writer, "safe-output", "rename-incident", { "incident-id": incidentID, title: "x".repeat(128) }, options);
  assert.equal(maximum.staged, true);
  checks++;
  const unicodeMaximum = await executeConnector(writer, "safe-output", "rename-incident", { "incident-id": incidentID, title: "\u00e9".repeat(128) }, options);
  assert.equal(unicodeMaximum.staged, true);
  checks++;
  for (const id of ["0", "012345678", "12345677", "../12345678", "1".repeat(16), 12345678]) {
    await assert.rejects(executeConnector(reader, "mcp", "get-monitor", { "monitor-id": id }, { ...options, staged: false }));
    checks++;
  }
  await assert.rejects(executeConnector(reader, "safe-output", "rename-incident", { "incident-id": incidentID, title: "title" }, options));
  await assert.rejects(executeConnector(writer, "mcp", "get-incident", { "incident-id": incidentID }, { ...options, staged: false }));
  checks += 2;
}

async function checkProviderFailures() {
  const renamedArgs = { "incident-id": incidentID, title: "Build latency investigation" };
  for (const [status, body] of [
    [401, '{"errors":["private provider detail"]}'],
    [403, incident],
    [429, incident],
    [503, incident],
    [307, incident],
    [200, ""],
    [200, "{"],
    [200, '{"errors":["failure"]}'],
    [200, incident.replace('"type": "incidents"', '"type": "users"')],
    [200, incident.replaceAll(incidentID, otherIncidentID)],
    [200, incident.replace("Build latency investigation", "Different title")],
    [200, incident.replace('"Build latency investigation"', '{"private":"not a title"}')],
    [200, "x".repeat(65537)],
  ]) {
    let calls = 0;
    const credentials = phaseEnvironment("DATADOG_INCIDENT_WRITE_OAUTH_TOKEN");
    await assert.rejects(
      executeConnector(writer, "safe-output", "rename-incident", renamedArgs, {
        env: credentials.env,
        fetchImpl: async () => {
          calls++;
          return response(body, status);
        },
      }),
      error => !error.message.includes("private provider detail")
    );
    assert.equal(calls, 1);
    assert.equal(credentials.resolutions(), 1);
    checks++;
  }
  for (const numericToken of ["12345679", "12345678.0", "1.2345678e7", "12345678.0000000001", "9007199254740991.1", "9007199254740992", "9007199254740993", "012345678"]) {
    let calls = 0;
    const credentials = phaseEnvironment("DATADOG_MONITOR_READ_OAUTH_TOKEN");
    await assert.rejects(
      executeConnector(
        reader,
        "mcp",
        "get-monitor",
        { "monitor-id": "12345678" },
        {
          env: credentials.env,
          fetchImpl: async () => {
            calls++;
            return response(monitor.replace('"id": 12345678', '"id": ' + numericToken));
          },
        }
      )
    );
    assert.equal(calls, 1);
    checks++;
  }
}

async function checkResponseByteBoundary() {
  const base = monitor.trim().slice(0, -1) + ',"padding":""}';
  for (const size of [65536, 65537]) {
    const body = base.slice(0, -2) + "x".repeat(size - Buffer.byteLength(base)) + '"}';
    assert.equal(Buffer.byteLength(body), size);
    const credentials = phaseEnvironment("DATADOG_MONITOR_READ_OAUTH_TOKEN");
    const promise = executeConnector(reader, "mcp", "get-monitor", { "monitor-id": "12345678" }, { env: credentials.env, fetchImpl: async () => response(body) });
    if (size === 65536) assert.equal((await promise).data["monitor-id"], 12345678);
    else await assert.rejects(promise);
    checks++;
  }
}

async function main() {
  assert.equal(decodePlan(reader).spec.mcp["get-monitor"].responseResourceInputs["/id"], "monitor-id");
  await checkReads();
  await checkStagingAndTitleEffect();
  await checkArgumentBoundaries();
  await checkProviderFailures();
  await checkResponseByteBoundary();
  process.stdout.write(JSON.stringify({ checks }));
}
main().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
