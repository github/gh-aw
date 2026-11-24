// @ts-check
// @safe-outputs-exempt USE-001 — standalone credential transport with no dependency on safe-output handlers.
const { startBroker, runBroker, stopBroker, renewableToken, validBearer } = require("./connector_credential_broker.cjs");

const hosts = { public: "login.microsoftonline.com", usgovernment: "login.microsoftonline.us", china: "login.chinacloudapi.cn" };
const uuid = /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/iu;
const resourceScope = /^(?:(?:https|api):\/\/[A-Za-z0-9.-]+(?:\/[A-Za-z0-9._~-]+)*|[0-9a-fA-F-]{36})\/\.default$/u;
const failure = () => new Error("Connector Azure credential unavailable; check federation, scope, and job OIDC permission");
const bounded = value => typeof value === "string" && value.length <= 2048 && !/[\s\x00-\x1f\x7f]/u.test(value);

async function fetchJSON(fetchImpl, url, init, signal) {
  const response = await fetchImpl(url, { ...init, signal, redirect: "error", credentials: "omit", cache: "no-store" });
  if (!response.ok || response.redirected || !response.body) {
    await response.body?.cancel();
    throw failure();
  }
  const chunks = [];
  let size = 0;
  for await (const chunk of response.body) {
    size += chunk.length;
    if (size > 64 * 1024) throw failure();
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8"));
}

// Only the job-side broker receives this environment. No socket request may
// specify an audience, tenant, client, scope, or upstream endpoint.
function tokenSource(env, fetchImpl = globalThis.fetch, now = Date.now) {
  const { GH_AW_AZURE_TENANT_ID: tenant, GH_AW_AZURE_CLIENT_ID: client, GH_AW_AZURE_SCOPE: scope, ACTIONS_ID_TOKEN_REQUEST_TOKEN: requestToken } = env;
  const cloud = env.GH_AW_AZURE_CLOUD || "public";
  const audience = env.GH_AW_AZURE_AUDIENCE || "api://AzureADTokenExchange";
  if (!bounded(tenant) || !uuid.test(tenant) || !bounded(client) || !uuid.test(client) || !Object.hasOwn(hosts, cloud) || !validBearer(requestToken)) throw failure();
  if (!bounded(scope) || !resourceScope.test(scope) || !bounded(audience)) throw failure();
  let oidc;
  try {
    oidc = new URL(env.ACTIONS_ID_TOKEN_REQUEST_URL);
  } catch {
    throw failure();
  }
  if (oidc.protocol !== "https:" || oidc.username || oidc.password || oidc.hash) throw failure();
  oidc.searchParams.set("audience", audience);
  const authority = `https://${hosts[cloud]}/${tenant}/oauth2/v2.0/token`;
  return renewableToken(async () => {
    const signal = AbortSignal.timeout(10000);
    try {
      const { value } = await fetchJSON(fetchImpl, oidc.href, { method: "GET", headers: { authorization: ["Bearer", requestToken].join(" ") } }, signal);
      if (!validBearer(value)) throw failure();
      const started = now();
      const result = await fetchJSON(
        fetchImpl,
        authority,
        {
          method: "POST",
          body: new URLSearchParams({ client_id: client, scope, grant_type: "client_credentials", client_assertion_type: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer", client_assertion: value }),
        },
        signal
      );
      if (!validBearer(result.access_token) || result.token_type?.toLowerCase() !== "bearer" || !["string", "number"].includes(typeof result.expires_in) || !/^[1-9][0-9]{0,8}$/u.test(String(result.expires_in))) throw failure();
      return { value: result.access_token, expiresAt: started + Number(result.expires_in) * 1000 };
    } catch {
      // Fetch errors, response text and JSON parse diagnostics can contain credentials.
      throw failure();
    }
  }, now);
}

async function start(env) {
  tokenSource(env); // Validate before advertising a socket; do not mint on startup.
  return startBroker(__filename, env, ["ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN", "GH_AW_AZURE_TENANT_ID", "GH_AW_AZURE_CLIENT_ID", "GH_AW_AZURE_SCOPE", "GH_AW_AZURE_AUDIENCE", "GH_AW_AZURE_CLOUD"]);
}

module.exports = { tokenSource, start };
if (require.main === module) {
  (async () => {
    if (process.argv[2] === "start") return start(process.env);
    if (process.argv[2] === "stop") return stopBroker(process.env);
    if (process.argv[2] !== "serve" || !process.send) throw failure();
    runBroker(process.argv[3], tokenSource(process.env), Number(process.argv[4]));
  })().catch(() => {
    process.stderr.write(failure().message + "\n");
    process.exitCode = 1;
  });
}
