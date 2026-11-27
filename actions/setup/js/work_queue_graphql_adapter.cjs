// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
// @safe-outputs-exempt SEC-005 — work_queue_claim_adapters.cjs validates the fixed adapter repository; assertClaimAuthorized and the guarded effect client authorize resolved GraphQL targets before mutations.

const { canonicalBytes, closed, utf8Compare } = require("./work_queue_codec.cjs");
const { assertClaimAuthorized, currentClaimHandle, claimIdentity, assertClaimIdentity } = require("./work_queue_claim_scope.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

const NAME = /^[A-Za-z][A-Za-z_0-9]*$/;
const PATH = /^[A-Za-z][A-Za-z_0-9]*(?:\.[A-Za-z][A-Za-z_0-9]*){0,7}$/;
const RESERVED = new Set([
  "owner",
  "repo",
  "repositoryId",
  "repositoryNameWithOwner",
  "query",
  "variables",
  "method",
  "url",
  "baseUrl",
  "headers",
  "request",
  "token",
  "auth",
  "data",
  "mediaType",
  "constructor",
  "prototype",
  "claim_handle",
  "claim_id",
  "work_id",
  "dispatch_id",
  "receipt_id",
]);

// `fields`, `number-field` and `resource-kind` are accepted for compiled-config
// compatibility but unused: v4 records effects without delivery readback.
function validateGraphqlAdapter(adapter) {
  const config = adapter.graphql;
  closed(config, ["mutation", "input-type", "response-field", "resource-type", "repository-field", "repository-input", "permission"], ["fields", "number-field", "resource-kind"], "trusted GraphQL adapter");
  for (const field of ["mutation", "input-type", "response-field", "resource-type"]) {
    if (typeof config[field] !== "string" || config[field].length > 128 || !NAME.test(config[field]) || RESERVED.has(config[field])) throw new Error(`${SAFE_OUTPUT_E001}: Trusted GraphQL adapter requires fixed native operation names`);
  }
  if (!["none", "repositoryId", "repositoryNameWithOwner"].includes(config["repository-input"])) throw new Error("Trusted GraphQL adapter requires explicit repository input binding");
  if (!["checks", "contents", "issues", "pull-requests", "deployments", "discussions"].includes(config.permission)) throw new Error("Trusted GraphQL adapter requires an explicit native write permission");
  const declarations = Object.assign(Object.create(null), adapter.expected || {});
  for (const field of Object.keys(adapter["field-map"] || {})) declarations[field] = true;
  if (!Object.keys(declarations).length || Object.keys(declarations).length > 64 || Object.keys(declarations).some(field => !NAME.test(field) || RESERVED.has(field)))
    throw new Error("Trusted GraphQL adapter has invalid or reserved effect fields");
  const field = config["repository-field"];
  if (typeof field !== "string" || field.length > 256 || !PATH.test(field) || field.split(".").some(component => RESERVED.has(component))) throw new Error("Trusted GraphQL adapter requires a bounded native repository field path");
  return adapter;
}

function selection(paths) {
  const root = Object.create(null);
  for (const field of ["id", "__typename", ...paths]) {
    let node = root;
    const components = field.split(".");
    for (let index = 0; index < components.length; index++) {
      const component = components[index];
      if (node[component] === true) {
        if (index !== components.length - 1) throw new Error("GraphQL selection contains conflicting scalar and object projections");
        break;
      }
      if (index === components.length - 1) {
        if (node[component] !== undefined) throw new Error("GraphQL selection contains conflicting scalar and object projections");
        node[component] = true;
      } else {
        node[component] ||= Object.create(null);
        node = node[component];
      }
    }
  }
  const render = node =>
    Object.keys(node)
      .sort(utf8Compare)
      .map(field => (node[field] === true ? field : `${field} { ${render(node[field])} }`))
      .join(" ");
  return render(root);
}

function observedField(node, field) {
  for (const component of field.split(".")) {
    if (!node || typeof node !== "object" || Array.isArray(node) || !Object.hasOwn(node, component)) throw new Error("GraphQL mutation response is missing a declared field");
    node = node[component];
  }
  return node;
}

function nodeIdentity(id) {
  if (typeof id !== "string" || !id || Buffer.byteLength(id) > 256 || /[\x00-\x1f\x7f]/.test(id)) throw new Error("Trusted GraphQL response has no exact native node identity");
  return id;
}

function createGraphqlEffectHandler(adapter, github) {
  adapter = validateGraphqlAdapter(structuredClone(adapter));
  const config = adapter.graphql;
  const identity = claimIdentity(currentClaimHandle());
  const client = wrapClaimEffectClient(github, { claim_handle: identity.claim_handle, targetRepository: adapter["target-repo"] });
  const staged = isStagedMode();
  return async message => {
    assertClaimIdentity(identity);
    if (Object.hasOwn(message, "repo") && message.repo !== adapter["target-repo"]) throw new Error("Explicit Claim repository conflicts with its trusted GraphQL adapter");
    await assertClaimAuthorized({ ...message, repo: adapter["target-repo"] }, { requireCompletion: !staged });
    if (staged) return { success: true, staged: true, claim_handle: identity.claim_handle };
    const fields = { ...(adapter.expected || {}) };
    for (const field of Object.keys(adapter["field-map"] || {})) {
      if (!Object.hasOwn(message, field)) throw new Error("Trusted GraphQL adapter is missing a declared input field");
      fields[field] = message[field];
    }
    if (canonicalBytes(fields) > 1024 * 1024) throw new Error("Trusted GraphQL effect input exceeds its byte ceiling");
    const input = structuredClone(fields);
    if (config["repository-input"] === "repositoryId") {
      const [owner, name] = adapter["target-repo"].split("/");
      const response = await client.graphql("query ClaimAdapterRepository($owner:String!,$name:String!) { repository(owner:$owner,name:$name) { id nameWithOwner } }", { owner, name });
      if (response?.repository?.nameWithOwner !== adapter["target-repo"]) throw new Error("Trusted GraphQL repository identity mismatch");
      input.repositoryId = nodeIdentity(response.repository.id);
    } else if (config["repository-input"] === "repositoryNameWithOwner") input.repositoryNameWithOwner = adapter["target-repo"];
    const query = `mutation ClaimAdapterEffect($input:${config["input-type"]}!) { ${config.mutation}(input:$input) { ${config["response-field"]} { ${selection([config["repository-field"]])} } } }`;
    const response = await client.graphql(query, { input });
    const node = response?.[config.mutation]?.[config["response-field"]];
    const id = nodeIdentity(node?.id);
    if (node.__typename !== config["resource-type"] || observedField(node, config["repository-field"]) !== adapter["target-repo"]) throw new Error("GraphQL mutation returned a foreign native resource");
    return { success: true, repo: adapter["target-repo"], id };
  };
}

module.exports = { validateGraphqlAdapter, createGraphqlEffectHandler };
