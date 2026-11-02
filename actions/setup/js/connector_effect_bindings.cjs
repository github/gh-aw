// @ts-check

const { randomBytes, randomUUID } = require("node:crypto");

// Identity, not map contents, scopes authority to one trusted handler execution.
const executions = new WeakMap();

function connectorEffectBindings(temporaryIdMap, plan) {
  if (!(temporaryIdMap instanceof Map)) throw new Error("Connector effects require trusted execution state");
  let plans = executions.get(temporaryIdMap);
  if (!plans) {
    plans = new Map();
    executions.set(temporaryIdMap, plans);
  }
  let state = plans.get(plan);
  if (!state) {
    state = { run: randomUUID(), key: randomBytes(32).toString("base64"), receipts: [] };
    plans.set(plan, state);
  }
  return {
    env: {
      GH_AW_CONNECTOR_BINDING_RUN: state.run,
      GH_AW_CONNECTOR_BINDING_KEY: state.key,
      GH_AW_CONNECTOR_BINDING_RECEIPTS: JSON.stringify(state.receipts),
    },
    record(result) {
      if (result.createdResource !== undefined) {
        if (
          result.staged ||
          !Number.isInteger(result.statusCode) ||
          result.statusCode < 200 ||
          result.statusCode >= 300 ||
          typeof result.createdResource !== "string" ||
          result.createdResource.length > 8192 ||
          state.receipts.length >= 128
        ) {
          throw new Error("Invalid connector created-resource result");
        }
        if (Buffer.byteLength(JSON.stringify([...state.receipts, result.createdResource])) > 65536) throw new Error("Connector receipt state exceeds its byte bound");
        state.receipts.push(result.createdResource);
        delete result.createdResource;
      }
    },
  };
}

module.exports = { connectorEffectBindings };
