// @ts-check
"use strict";

const { queueError } = require("./work_queue_codec.cjs");
const { nativeId } = require("./work_queue_native.cjs");
const { withRetry } = require("./error_recovery.cjs");

const MAX_PROJECTION_TARGETS = 25;
const MAX_PROJECTION_MUTATIONS = 50;
const LABELS_PAGE_SIZE = 100;
const STATUSES = ["Queued", "Blocked", "Assigned", "Running", "Verifying", "Needs review", "Done", "Needs attention", "Cancelled"];
const ISSUE_SELECTION = `id databaseId number state repository { id databaseId nameWithOwner } author { login }
  labels(first:${LABELS_PAGE_SIZE}) { nodes { id name } pageInfo { hasNextPage endCursor } }`;
const QUEUE_LABEL_COLOR = "7057FF";

async function ensureLabel(github, repositoryId, label, name) {
  if (label) {
    if (label.name !== name || typeof label.id !== "string") throw queueError("projection_label_pending", "tracking label identity is invalid");
    if (label.color?.toUpperCase() !== QUEUE_LABEL_COLOR) {
      const response = await github.graphql("mutation WorkQueueLabelColor($input:UpdateLabelInput!) { updateLabel(input:$input) { label { id name color } } }", {
        input: { id: label.id, color: QUEUE_LABEL_COLOR },
        request: { retries: 0, timeout: 15000 },
      });
      if (response?.updateLabel?.label?.id !== label.id || response.updateLabel.label.name !== name || response.updateLabel.label.color?.toUpperCase() !== QUEUE_LABEL_COLOR)
        throw queueError("projection_label_pending", "work queue label color update is uncertain");
    }
    return label.id;
  }
  const response = await github.graphql("mutation WorkQueueTrackingLabel($input:CreateLabelInput!) { createLabel(input:$input) { label { id name color } } }", {
    input: { repositoryId, name, color: QUEUE_LABEL_COLOR, description: "Git-backed Work queue" },
    request: { retries: 0, timeout: 15000 },
  });
  if (!response?.createLabel?.label?.id || response.createLabel.label.name !== name || response.createLabel.label.color?.toUpperCase() !== QUEUE_LABEL_COLOR)
    throw queueError("projection_label_pending", "work queue label creation is uncertain");
  return response.createLabel.label.id;
}

function statusLabelName(config, status) {
  return `${config.label}: ${status}`;
}

async function ensureStatusLabel(github, repositoryId, config, status) {
  const name = statusLabelName(config, status);
  const response = await findStatusLabel(github, repositoryId, name);
  if (response?.node?.id !== repositoryId) throw queueError("projection_label_pending", "status label target is inaccessible");
  try {
    return await ensureLabel(github, repositoryId, response.node.label, name);
  } catch (error) {
    if (response.node.label || !labelAlreadyExists(error)) throw error;
    const concurrent = await findStatusLabel(github, repositoryId, name);
    if (concurrent?.node?.id !== repositoryId || !concurrent.node.label) throw error;
    return ensureLabel(github, repositoryId, concurrent.node.label, name);
  }
}

async function findStatusLabel(github, repositoryId, name) {
  return github.graphql("query WorkQueueStatusLabel($repositoryId:ID!,$name:String!) { node(id:$repositoryId) { ... on Repository { id label(name:$name) { id name color } } } }", { repositoryId, name });
}

function labelAlreadyExists(error) {
  const failure = error?.originalError || error;
  const messages = [failure?.message, ...(Array.isArray(failure?.errors) ? failure.errors.map(item => item?.message) : [])];
  return messages.some(message => typeof message === "string" && /already (?:exists|been taken)/i.test(message));
}

async function discoverTarget(github, repository, config, firstPage = undefined) {
  const [owner, name] = repository.split("/");
  const result = firstPage
    ? { repository: firstPage }
    : await github.graphql("query WorkQueueIssueTarget($owner:String!,$name:String!,$label:String!) { repository(owner:$owner,name:$name) { id nameWithOwner label(name:$label) { id name color } } }", { owner, name, label: config.label });
  const target = result?.repository;
  if (!target || target.nameWithOwner !== repository) throw queueError("projection_target_unavailable", "Issue target is inaccessible or transferred");
  return { repositoryId: target.id, labelId: await ensureLabel(github, target.id, target.label, config.label) };
}

function issuePreflightQuery(targets) {
  if (targets.length > MAX_PROJECTION_TARGETS) throw queueError("projection_limit", `at most ${MAX_PROJECTION_TARGETS} projection targets`);
  const variables = {};
  const declarations = [];
  const selections = [];
  for (const [index, target] of targets.entries()) {
    if (target.resource) {
      const [owner, name] = target.resource.repository.split("/");
      const number = Number(target.resource.number);
      if (!Number.isSafeInteger(number) || number < 1 || number > 2147483647) throw queueError("projection_target_unavailable", "Issue number cannot be represented by GraphQL");
      declarations.push(`$o${index}:String!,$r${index}:String!,$n${index}:Int!`);
      Object.assign(variables, { [`o${index}`]: owner, [`r${index}`]: name, [`n${index}`]: number });
      selections.push(`i${index}: repository(owner:$o${index},name:$r${index}) { issue(number:$n${index}) { ${ISSUE_SELECTION} } }`);
    }
    for (const [handle, id] of Object.entries(target.comments || {})) {
      const alias = `c${index}_${selections.length}`;
      declarations.push(`$${alias}:ID!`);
      variables[alias] = id;
      selections.push(`${alias}: node(id:$${alias}) { id ... on IssueComment { body issue { id } } }`);
      target.commentAliases ||= {};
      target.commentAliases[handle] = alias;
    }
  }
  return { variables, declarations, selections };
}

function issueReadQuery(targets, config, repository) {
  const read = issuePreflightQuery(targets);
  const repositories = [...new Set(targets.map(target => target.resource?.repository || repository))];
  const repositoryAliases = new Map();
  read.declarations.push("$trackingLabel:String!");
  read.variables.trackingLabel = config.label;
  for (const [index, repository] of repositories.entries()) {
    const [owner, name] = repository.split("/");
    const alias = `d${index}`;
    repositoryAliases.set(repository, alias);
    read.declarations.push(`$${alias}Owner:String!,$${alias}Name:String!`);
    Object.assign(read.variables, { [`${alias}Owner`]: owner, [`${alias}Name`]: name });
    read.selections.push(`${alias}: repository(owner:$${alias}Owner,name:$${alias}Name) { id nameWithOwner label(name:$trackingLabel) { id name color } }`);
  }
  return { ...read, repositoryAliases };
}

/** @param {{response?: Record<string, any>}} [options] */
async function preflightIssues(github, targets, options = {}) {
  const { response } = options;
  const read = issuePreflightQuery(targets);
  if (!read.selections.length) return new Map();
  const result = response || (await github.graphql(`query WorkQueueIssuePreflight(${read.declarations.join(",")}) { ${read.selections.join("\n")} }`, read.variables));
  const issues = new Map();
  for (const [index, target] of targets.entries()) {
    if (!target.resource) continue;
    const issue = result?.[`i${index}`]?.issue;
    if (
      !issue ||
      !issue.repository ||
      nativeId(issue.databaseId) !== target.resource.resource_id ||
      nativeId(issue.repository.databaseId) !== target.resource.repository_id ||
      issue.repository.nameWithOwner !== target.resource.repository ||
      nativeId(issue.number) !== target.resource.number
    )
      throw queueError("projection_target_unavailable", "backing Issue was deleted, transferred, or changed identity");
    if (!Array.isArray(issue.labels?.nodes)) throw queueError("projection_target_unavailable", "Issue labels are inaccessible");
    issue.comments = new Map();
    for (const [handle, alias] of Object.entries(target.commentAliases || {})) {
      const comment = result?.[alias];
      if (!comment || comment.id !== target.comments[handle] || comment.issue?.id !== issue.id || typeof comment.body !== "string")
        throw queueError("projection_comment_pending", "canonical comment was deleted or transferred; immutable handle cannot be replaced");
      issue.comments.set(handle, comment);
    }
    issues.set(target.work_id, issue);
  }
  const issueList = [...issues.values()];
  const seen = new Map();
  for (let page = 0; page < 16; page++) {
    const declarations = [];
    const selections = [];
    const variables = {};
    const pending = [];
    for (const [index, issue] of issueList.entries()) {
      if (issue.labels?.pageInfo?.hasNextPage === false) continue;
      const cursor = issue.labels?.pageInfo?.endCursor;
      const cursors = seen.get(index) || new Set();
      if (typeof cursor !== "string" || !cursor || cursors.has(cursor)) throw queueError("projection_pagination_pending", "Issue label pagination is incomplete");
      cursors.add(cursor);
      seen.set(index, cursors);
      const alias = `p${pending.length}`;
      declarations.push(`$${alias}:ID!,$${alias}Cursor:String!`);
      Object.assign(variables, { [alias]: issue.id, [`${alias}Cursor`]: cursor });
      selections.push(`${alias}: node(id:$${alias}) { ... on Issue { id repository { id } labels(first:${LABELS_PAGE_SIZE},after:$${alias}Cursor) { nodes { id name } pageInfo { hasNextPage endCursor } } } }`);
      pending.push({ issue, alias });
    }
    if (!pending.length) return issues;
    const response = await github.graphql(`query WorkQueueIssuePages(${declarations.join(",")}) { ${selections.join("\n")} }`, variables);
    for (const { issue, alias } of pending) {
      const node = response?.[alias];
      if (node?.id !== issue.id || node.repository?.id !== issue.repository.id || !Array.isArray(node.labels?.nodes)) throw queueError("projection_target_unavailable", "Issue was deleted, transferred, or its pagination is inaccessible");
      issue.labels = { nodes: [...issue.labels.nodes, ...node.labels.nodes], pageInfo: node.labels.pageInfo };
    }
  }
  throw queueError("projection_pagination_pending", "Issue label pagination exhausted");
}

async function mutateIssues(github, operations, { sleep = delay => new Promise(resolve => setTimeout(resolve, delay)) } = {}) {
  if (operations.length > MAX_PROJECTION_MUTATIONS) {
    const results = [];
    for (let offset = 0; offset < operations.length; offset += MAX_PROJECTION_MUTATIONS) {
      if (offset) await sleep(1000);
      const batch = await mutateIssues(github, operations.slice(offset, offset + MAX_PROJECTION_MUTATIONS), { sleep });
      results.push(...batch.results);
      if (batch.ambiguous) {
        results.push(...operations.slice(offset + MAX_PROJECTION_MUTATIONS).map(operation => ({ operation, value: undefined, pending: true, uncertain: false, unattempted: true })));
        return { results, ambiguous: true };
      }
    }
    return { results, ambiguous: false };
  }
  if (!operations.length) return { results: [], ambiguous: false };
  const variables = {};
  const declarations = [];
  const selections = [];
  operations.forEach((operation, index) => {
    declarations.push(`$m${index}:${operation.type}!`);
    variables[`m${index}`] = operation.input;
    selections.push(`m${index}: ${operation.name}(input:$m${index}) { ${operation.selection} }`);
  });
  let result;
  let errors = [];
  let ambiguous = false;
  try {
    const send = () => github.graphql(`mutation WorkQueueIssueProjection(${declarations.join(",")}) { ${selections.join("\n")} }`, { ...variables, request: { retries: 0, timeout: 30000 } });
    result = operations.some(operation => operation.name === "createIssue") ? await withRetry(send, { maxRetries: 3, shouldRetry: isRetryableBeforeExecution }, "work_queue createIssue batch") : await send();
  } catch (error) {
    const nativeError = error.originalError || error;
    result = nativeError.data;
    errors = nativeError.errors || [];
    ambiguous = !Array.isArray(nativeError.errors) || nativeError.errors.length === 0;
    if (!result && !ambiguous && !errors.length) throw error;
  }
  const results = operations.map((operation, index) => {
    const value = result?.[`m${index}`];
    const failed = errors.some(error => !error.path || error.path[0] === `m${index}`);
    const pending = failed || !value;
    const rejectedBeforeExecution = isConfirmedRejectionBeforeExecution({ data: result, errors });
    const uncertain = pending && !rejectedBeforeExecution && !errors.some(error => (!error.path || error.path[0] === `m${index}`) && ["FORBIDDEN", "NOT_FOUND", "GRAPHQL_VALIDATION_FAILED"].includes(error.type));
    return { operation, value: failed ? undefined : value, pending, uncertain };
  });
  if (results.some(result => result.uncertain)) ambiguous = true;
  return { results, ambiguous };
}

function isConfirmedRejectionBeforeExecution(error) {
  // { errors: [{ type: "RATE_LIMITED" }], data: { m0: null } } is safe.
  // Alias-specific errors, partial results, and transport failures are not proof.
  if (!Array.isArray(error.errors) || !error.errors.length) return false;
  const allRejected = error.errors.every(item => item?.type === "RATE_LIMITED" && (!item.path || item.path.length === 0));
  const noReceipts = Object.values(error.data || {}).every(value => value == null);
  return allRejected && noReceipts;
}

function isRetryableBeforeExecution(error) {
  return isConfirmedRejectionBeforeExecution(error);
}

function issueResource(issue) {
  return { kind: "issue", host: "github.com", repository: issue.repository.nameWithOwner, repository_id: nativeId(issue.repository.databaseId), resource_id: nativeId(issue.databaseId), number: nativeId(issue.number) };
}

module.exports = {
  MAX_PROJECTION_TARGETS,
  MAX_PROJECTION_MUTATIONS,
  LABELS_PAGE_SIZE,
  STATUSES,
  ISSUE_SELECTION,
  statusLabelName,
  ensureStatusLabel,
  discoverTarget,
  preflightIssues,
  mutateIssues,
  issueResource,
  issueReadQuery,
  isRetryableBeforeExecution,
  isConfirmedRejectionBeforeExecution,
};
