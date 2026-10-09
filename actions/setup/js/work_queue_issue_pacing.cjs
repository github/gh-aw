// @ts-check
"use strict";

const { queueError } = require("./work_queue_codec.cjs");

function wrapGithubClient(githubClient, { sleep = delay => new Promise(resolve => setTimeout(resolve, delay)), now = Date.now } = {}) {
  const metrics = { requests: 0, reads: 0, mutations: 0, paced_ms: 0 };
  let nextMutation = now();
  let rateLimited = false;
  const invoke = async (mutation, cost, operation) => {
    if (rateLimited) throw queueError("projection_rate_pending", "rate limit exhausted; remaining synchronization is pending");
    if (mutation) {
      const start = now();
      nextMutation = Math.max(start, nextMutation) + cost * 1000;
      const delay = nextMutation - start;
      metrics.paced_ms += delay;
      await sleep(delay);
    }
    metrics.requests++;
    metrics[mutation ? "mutations" : "reads"]++;
    try {
      return await operation();
    } catch (error) {
      const headers = error.response?.headers || {};
      if (error.status === 429 || headers["retry-after"] || headers["x-ratelimit-remaining"] === "0") rateLimited = true;
      throw error;
    }
  };
  const client = new Proxy(githubClient, {
    get(target, key) {
      if (key === "graphql")
        return async (query, variables) => {
          const mutation = /^\s*mutation/.test(query);
          const cost = Math.max(1, [...query.matchAll(/:\s*(?:createIssue|addComment|updateIssueComment|setIssueFieldValue|addLabelsToLabelable|removeLabelsFromLabelable|closeIssue|createRef|deleteRef)\(/g)].length);
          return invoke(mutation, cost, () => target.graphql(query, { ...variables, request: { ...variables?.request, retries: 0 } }));
        };
      if (key === "rest")
        return new Proxy(target.rest, {
          get(apis, scope) {
            return new Proxy(apis[scope], {
              get(methods, name) {
                return async (...args) => invoke(!/^(get|list)/.test(String(name)), 1, () => methods[name](...args));
              },
            });
          },
        });
      return target[key];
    },
  });
  return { client, metrics };
}

module.exports = { wrapGithubClient };
