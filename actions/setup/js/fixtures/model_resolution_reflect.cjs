// @ts-check
"use strict";

/**
 * Shared /reflect fixtures for the cross-engine "resolve configured model" tests.
 *
 * The base fixture is the recorded AWF /reflect from the Design Decision Gate run
 * https://github.com/github/gh-aw/actions/runs/38945917145 (agent artifact,
 * sandbox/firewall/awf-reflect.json): `models_fetch_complete: true`, 40 Copilot models
 * with `claude-opus-5.5` as the only Opus, and the merged alias map as `model_aliases`.
 *
 * Variants are derived from that file (no recorded run with these shapes exists) and
 * each derivation is named for what it changes.
 */

const recorded = require("./awf-reflect-ddg-38945917145.json");

/** @returns {any} A fresh deep copy of the recorded /reflect. */
function ddgReflect() {
  return JSON.parse(JSON.stringify(recorded));
}

/** @returns {Record<string, string[]>} The recorded merged alias map (built-in aliases). */
function builtinAliases() {
  return ddgReflect().model_aliases;
}

/**
 * Derived: add models to the configured Copilot endpoint (for example claude-opus-6,
 * which did not exist when the run was recorded).
 * @param {string[]} models
 */
function withCopilotModels(models) {
  const reflect = ddgReflect();
  const endpoint = reflect.endpoints.find(entry => entry.provider === "copilot");
  endpoint.models = [...endpoint.models, ...models];
  return reflect;
}

/**
 * Derived: remove models from the configured Copilot endpoint (for example gpt-5-mini,
 * which the recording lists, to exercise an alias whose exact id is not live).
 * @param {string[]} models
 */
function withoutCopilotModels(models) {
  const reflect = ddgReflect();
  const endpoint = reflect.endpoints.find(entry => entry.provider === "copilot");
  endpoint.models = endpoint.models.filter(model => !models.includes(model));
  return reflect;
}

/**
 * Derived: incomplete discovery. The recorded file with `models_fetch_complete: false`
 * and the Copilot endpoint's model list cleared (`models: null`), the shape AWF reports
 * when the upstream /models fetch fails. No recorded run with this shape exists.
 */
function incompleteDiscovery() {
  const reflect = ddgReflect();
  reflect.models_fetch_complete = false;
  for (const endpoint of reflect.endpoints) {
    if (endpoint.provider === "copilot") endpoint.models = null;
  }
  return reflect;
}

/**
 * Derived: the recorded file with its (unconfigured) Gemini endpoint configured and
 * given a model list, for Agy. The DDG run did not configure Gemini.
 * @param {string[]} models
 */
function withGeminiModels(models) {
  const reflect = ddgReflect();
  const endpoint = reflect.endpoints.find(entry => entry.provider === "gemini");
  endpoint.configured = true;
  endpoint.models = models;
  endpoint.models_url = "http://api-proxy:10003/v1beta/models";
  return reflect;
}

/**
 * Derived: the recorded file (which has `routing: null`) with a selected AWF routing
 * block added, in the /reflect `routing.selection` shape (`wire_model`, `effort`,
 * `endpoint`). Not a downloaded artifact of any run; the recorded Copilot endpoint's
 * `routing_models` (for example gpt-5.6-luna advertising none..max) are unchanged.
 * @param {string} wireModel
 * @param {string|null} effort
 * @param {string} [endpoint]
 */
function withRoutingSelection(wireModel, effort, endpoint = "/responses") {
  const reflect = ddgReflect();
  reflect.routing = { status: "selected", selection: { provider: "copilot", model: `github-copilot/${wireModel}`, wire_model: wireModel, effort, endpoint } };
  return reflect;
}

module.exports = { ddgReflect, builtinAliases, withCopilotModels, withoutCopilotModels, incompleteDiscovery, withGeminiModels, withRoutingSelection };
