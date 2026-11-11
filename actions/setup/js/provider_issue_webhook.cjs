// @ts-check

const { ERR_VALIDATION } = require("./error_codes.cjs");

function isProviderIssueEvent(_event) {
  return false;
}

function resolveProviderIssueItem(_event, _payload) {
  throw new Error(`${ERR_VALIDATION}: Webhook validation failed: unsupported provider issue event; use an imported connector with approved ingress`);
}

function providerIssueActivities(_event, _payload, _normalization) {
  throw new Error(`${ERR_VALIDATION}: Webhook validation failed: unsupported provider issue event; use an imported connector with approved ingress`);
}

function validProviderIssueSubscription(_subscription) {
  return false;
}

function matchesProviderIssueSubscription(_subscription, _payload) {
  return false;
}

module.exports = { isProviderIssueEvent, resolveProviderIssueItem, providerIssueActivities, validProviderIssueSubscription, matchesProviderIssueSubscription };
