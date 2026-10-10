// @ts-check
"use strict";

const { closed, canonical } = require("./work_queue_codec.cjs");
const { API_VERSION, nativeId } = require("./work_queue_native.cjs");

/** @type {WeakMap<object, {client: DispatchClient, profile: string}>} */
const credentialProofs = new WeakMap();

/**
 * @typedef {{kind: string, app_slug?: string}} DispatchCredential
 * @typedef {{principal: string, kind: string}} DispatchCredentialProof
 * @typedef {{status: number, data: {id: unknown, type: string, login?: string}}} IdentityResponse
 * @typedef {{headers: Record<string, string>, request: {retries: number, timeout: number}, username?: string}} IdentityRequest
 * @typedef {{rest?: {users?: {getAuthenticated?: (parameters: IdentityRequest) => Promise<IdentityResponse>, getByUsername?: (parameters: IdentityRequest & {username: string}) => Promise<IdentityResponse>}}, auth?: (options: {type: "token"}) => Promise<unknown>}} DispatchClient
 */
/** @param {unknown} value @returns {{readonly kind: string, readonly app_slug?: string}} */
function normalizeDispatchCredential(value) {
  if (!value || typeof value !== "object" || Array.isArray(value) || !("kind" in value) || typeof value.kind !== "string") throw new Error("work_queue_dispatch_credential_kind_invalid");
  closed(value, ["kind"], ["app_slug"], "protected dispatch credential");
  if (!["github_token", "github_app", "authenticated"].includes(value.kind)) throw new Error("work_queue_dispatch_credential_kind_invalid");
  if (value.kind === "github_app") {
    if (!("app_slug" in value) || typeof value.app_slug !== "string" || !/^[A-Za-z0-9][A-Za-z0-9-]{0,99}$/.test(value.app_slug)) throw new Error("work_queue_dispatch_app_metadata_missing");
    return Object.freeze({ kind: value.kind, app_slug: value.app_slug });
  }
  if ("app_slug" in value) throw new Error("work_queue_dispatch_credential_metadata_conflict");
  return Object.freeze({ kind: value.kind });
}

/** @param {DispatchClient} client @param {unknown} supplied */
async function resolveDispatchCredentialPrincipal(client, supplied) {
  const credential = normalizeDispatchCredential(supplied);
  const users = client?.rest?.users;
  const parameters = { headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } };
  let response;
  if (credential.kind === "authenticated") {
    if (typeof users?.getAuthenticated !== "function") throw new Error("work_queue_dispatch_credential_identity_unavailable: an opaque token requires authenticated user metadata or compiler-minted GitHub App metadata");
    response = await users.getAuthenticated(parameters);
    if (response?.status !== 200 || !["User", "Bot"].includes(response.data?.type)) throw new Error("work_queue_dispatch_credential_identity_unverified");
  } else {
    const username = credential.kind === "github_token" ? "github-actions[bot]" : `${credential.app_slug}[bot]`;
    if (typeof users?.getByUsername !== "function") throw new Error("work_queue_dispatch_credential_identity_unavailable");
    response = await users.getByUsername({ ...parameters, username });
    if (response?.status !== 200 || response.data?.type !== "Bot" || typeof response.data?.login !== "string" || response.data.login.toLowerCase() !== username.toLowerCase())
      throw new Error("work_queue_dispatch_credential_identity_unverified");
  }
  return nativeId(response.data.id, "dispatch credential principal");
}

/**
 * The token is selected by protected compiler configuration, never by a staged
 * message. Bot lookups alone prove an account exists, not which client will POST.
 * @param {DispatchClient} client
 * @param {unknown} supplied
 * @param {unknown} [selectedToken]
 * @returns {(options: {assignment?: unknown, profile: Record<string, unknown> & {principal?: string}}) => Promise<DispatchCredentialProof>}
 */
function createDispatchCredentialValidator(client, supplied, selectedToken) {
  const credential = normalizeDispatchCredential(supplied);
  if (credential.kind !== "authenticated" && (typeof selectedToken !== "string" || !selectedToken || selectedToken.length > 16384 || /[\x00-\x1f\x7f]/.test(selectedToken))) {
    throw new Error("work_queue_dispatch_credential_binding_required");
  }
  return async ({ profile }) => {
    if (credential.kind !== "authenticated") {
      if (typeof client?.auth !== "function") throw new Error("work_queue_dispatch_credential_auth_unavailable");
      const authentication = await client.auth({ type: "token" });
      if (!authentication || typeof authentication !== "object" || !("type" in authentication) || !("token" in authentication) || authentication.type !== "token" || authentication.token !== selectedToken)
        throw new Error("work_queue_dispatch_credential_client_mismatch");
    }
    const expected = Object.hasOwn(profile, "principal") ? nativeId(profile.principal, "approved worker principal") : undefined;
    const actual = await resolveDispatchCredentialPrincipal(client, credential);
    if (expected !== undefined && actual !== expected) throw new Error("work_queue_dispatch_credential_principal_mismatch");
    const proof = Object.freeze({ principal: actual, kind: credential.kind });
    credentialProofs.set(proof, { client, profile: canonical(profile) });
    return proof;
  };
}

/** @param {unknown} proof @param {DispatchClient} client @param {Record<string, unknown> & {principal?: string}} profile */
function isDispatchCredentialProof(proof, client, profile) {
  const facts = proof !== null && typeof proof === "object" ? credentialProofs.get(proof) : undefined;
  return !!facts && facts.client === client && facts.profile === canonical(profile);
}

module.exports = { normalizeDispatchCredential, resolveDispatchCredentialPrincipal, createDispatchCredentialValidator, isDispatchCredentialProof };
