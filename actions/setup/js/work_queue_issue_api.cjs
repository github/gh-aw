// @ts-check
"use strict";

const { queueError } = require("./work_queue_codec.cjs");
const { nativeId } = require("./work_queue_native.cjs");
const { withRetry } = require("./error_recovery.cjs");

const STATUSES = ["Queued", "Blocked", "Assigned", "Running", "Verifying", "Needs review", "Done", "Needs attention", "Cancelled"];
const FIELD_SELECTION = `__typename ... on IssueFieldText { id name } ... on IssueFieldNumber { id name }
  ... on IssueFieldDate { id name } ... on IssueFieldMultiSelect { id name }
  ... on IssueFieldSingleSelect { id name options { id name } }`;
const VALUE_SELECTION = `... on IssueFieldSingleSelectValue { optionId name field { ... on IssueFieldSingleSelect { id } } }`;
const ISSUE_SELECTION = `id databaseId number state viewerCanSetFields repository { id databaseId nameWithOwner } author { login }
  labels(first:100) { nodes { id name } pageInfo { hasNextPage endCursor } }
  issueFieldValues(first:25) { nodes { ${VALUE_SELECTION} } pageInfo { hasNextPage endCursor } }`;

async function ensureTrackingLabel(github, repositoryId, label, name) {
  if (label) {
    if (label.name !== name || typeof label.id !== "string") throw queueError("projection_label_pending", "tracking label identity is invalid");
    return label.id;
  }
  const response = await github.graphql("mutation WorkQueueTrackingLabel($input:CreateLabelInput!) { createLabel(input:$input) { label { id name } } }", {
    input: { repositoryId, name, color: "808080", description: "Git-backed Work queue tracking" },
    request: { retries: 0, timeout: 15000 },
  });
  if (!response?.createLabel?.label?.id || response.createLabel.label.name !== name) throw queueError("projection_label_pending", "tracking label creation is uncertain");
  return response.createLabel.label.id;
}

async function discoverTarget(github, repository, config, firstPage = undefined) {
  const [owner, name] = repository.split("/");
  /** @type {string | null} */
  let cursor = null;
  let field;
  let repositoryId;
  let label;
  try {
    for (let page = 0; page < 16; page++) {
      const fieldQuery = config["status-field"] ? `issueFields(first:25,after:$cursor) { nodes { ${FIELD_SELECTION} } pageInfo { hasNextPage endCursor } }` : "";
      const result =
        page === 0 && firstPage
          ? { repository: firstPage }
          : await github.graphql(
              `query WorkQueueIssueTarget($owner:String!,$name:String!,$label:String!${config["status-field"] ? ",$cursor:String" : ""}) {
        repository(owner:$owner,name:$name) { id nameWithOwner label(name:$label) { id name } ${fieldQuery} }
      }`,
              { owner, name, label: config.label, ...(config["status-field"] ? { cursor } : {}) }
            );
      const target = result?.repository;
      if (!target || target.nameWithOwner !== repository) throw queueError("projection_target_unavailable", "Issue target is inaccessible or transferred");
      repositoryId = target.id;
      label = target.label;
      if (!config["status-field"]) return { repositoryId, labelId: await ensureTrackingLabel(github, repositoryId, label, config.label) };
      const fields = target.issueFields;
      if (!Array.isArray(fields?.nodes)) throw queueError("projection_field_pending", "native organization Issue fields are inaccessible");
      for (const node of fields.nodes) {
        if (node.name !== config["status-field"]) continue;
        if (field || node.__typename !== "IssueFieldSingleSelect" || !Array.isArray(node.options)) throw queueError("projection_field_pending", "status field is ambiguous or not single-select");
        const options = new Map();
        for (const option of node.options) {
          if (options.has(option.name) || typeof option.id !== "string") throw queueError("projection_field_pending", "status options are ambiguous");
          options.set(option.name, option.id);
        }
        if (STATUSES.some(status => !options.has(status))) throw queueError("projection_field_pending", "status field is missing required options");
        field = { id: node.id, options };
      }
      if (fields.pageInfo?.hasNextPage === false) {
        if (!field) throw queueError("projection_field_pending", "configured native status field is missing or inaccessible");
        return { repositoryId, labelId: await ensureTrackingLabel(github, repositoryId, label, config.label), field };
      }
      const next = fields.pageInfo?.endCursor;
      if (typeof next !== "string" || !next || next === cursor) throw queueError("projection_field_pending", "status field discovery is incomplete");
      cursor = next;
    }
    throw queueError("projection_field_pending", "status field discovery pagination exhausted");
  } catch (error) {
    if (error.code !== "projection_field_pending") throw error;
    return { repositoryId, labelId: await ensureTrackingLabel(github, repositoryId, label, config.label), fieldPending: error.message };
  }
}

function issuePreflightQuery(targets, fields) {
  if (targets.length > 25) throw queueError("projection_limit", "at most 25 projection targets");
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
      selections.push(`i${index}: repository(owner:$o${index},name:$r${index}) { issue(number:$n${index}) { ${fields ? ISSUE_SELECTION : ISSUE_SELECTION.replace(/issueFieldValues[\s\S]*$/, "")} } }`);
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
  const read = issuePreflightQuery(targets, !!config["status-field"]);
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
    read.selections.push(`${alias}: repository(owner:$${alias}Owner,name:$${alias}Name) { id nameWithOwner label(name:$trackingLabel) { id name }
      ${config["status-field"] ? `issueFields(first:25) { nodes { ${FIELD_SELECTION} } pageInfo { hasNextPage endCursor } }` : ""} }`);
  }
  return { ...read, repositoryAliases };
}

async function preflightIssues(github, targets, { fields = true, response = undefined } = {}) {
  const read = issuePreflightQuery(targets, fields);
  if (!read.selections.length) return new Map();
  const result = response || (await github.graphql(`query WorkQueueIssuePreflight(${read.declarations.join(",")}) { ${read.selections.join("\n")} }`, read.variables));
  const issues = new Map();
  for (const [index, target] of targets.entries()) {
    if (!target.resource) continue;
    const issue = result?.[`i${index}`]?.issue;
    if (
      !issue ||
      nativeId(issue.databaseId) !== target.resource.resource_id ||
      nativeId(issue.repository?.databaseId) !== target.resource.repository_id ||
      issue.repository.nameWithOwner !== target.resource.repository ||
      nativeId(issue.number) !== target.resource.number
    )
      throw queueError("projection_target_unavailable", "backing Issue was deleted, transferred, or changed identity");
    if (!Array.isArray(issue.labels?.nodes)) throw queueError("projection_target_unavailable", "Issue labels are inaccessible");
    if (fields && !Array.isArray(issue.issueFieldValues?.nodes)) throw queueError("projection_field_pending", "Issue field values are inaccessible");
    issue.comments = new Map();
    for (const [handle, alias] of Object.entries(target.commentAliases || {})) {
      const comment = result?.[alias];
      if (!comment || comment.id !== target.comments[handle] || comment.issue?.id !== issue.id || typeof comment.body !== "string")
        throw queueError("projection_comment_pending", "canonical comment was deleted or transferred; immutable handle cannot be replaced");
      issue.comments.set(handle, comment);
    }
    issues.set(target.work_id, issue);
  }
  await completeIssueConnections(github, [...issues.values()], fields);
  return issues;
}

async function completeIssueConnections(github, issues, fields) {
  const seen = new Map();
  for (let page = 0; page < 16; page++) {
    const declarations = [];
    const selections = [];
    const variables = {};
    const pending = [];
    for (const [index, issue] of issues.entries()) {
      for (const connection of fields ? ["labels", "issueFieldValues"] : ["labels"]) {
        const values = issue[connection];
        if (values?.pageInfo?.hasNextPage === false) continue;
        const cursor = values?.pageInfo?.endCursor;
        const key = `${index}:${connection}`;
        const cursors = seen.get(key) || new Set();
        if (typeof cursor !== "string" || !cursor || cursors.has(cursor)) throw queueError("projection_pagination_pending", "Issue connection pagination is incomplete");
        cursors.add(cursor);
        seen.set(key, cursors);
        const alias = `p${pending.length}`;
        declarations.push(`$${alias}:ID!,$${alias}Cursor:String!`);
        Object.assign(variables, { [alias]: issue.id, [`${alias}Cursor`]: cursor });
        selections.push(
          `${alias}: node(id:$${alias}) { ... on Issue { id repository { id } ${connection}(first:${connection === "labels" ? 100 : 25},after:$${alias}Cursor) { nodes { ${connection === "labels" ? "id name" : VALUE_SELECTION} } pageInfo { hasNextPage endCursor } } } }`
        );
        pending.push({ issue, connection, alias });
      }
    }
    if (!pending.length) return;
    const response = await github.graphql(`query WorkQueueIssuePages(${declarations.join(",")}) { ${selections.join("\n")} }`, variables);
    for (const { issue, connection, alias } of pending) {
      const node = response?.[alias];
      if (node?.id !== issue.id || node.repository?.id !== issue.repository.id || !Array.isArray(node[connection]?.nodes))
        throw queueError("projection_target_unavailable", "Issue was deleted, transferred, or its pagination is inaccessible");
      issue[connection] = { nodes: [...issue[connection].nodes, ...node[connection].nodes], pageInfo: node[connection].pageInfo };
    }
  }
  throw queueError("projection_pagination_pending", "Issue connection pagination exhausted");
}

async function mutateIssues(github, operations, { sleep = delay => new Promise(resolve => setTimeout(resolve, delay)) } = {}) {
  if (operations.length > 50) {
    const results = [];
    for (let offset = 0; offset < operations.length; offset += 50) {
      if (offset) await sleep(1000);
      const batch = await mutateIssues(github, operations.slice(offset, offset + 50), { sleep });
      results.push(...batch.results);
      if (batch.ambiguous) {
        results.push(...operations.slice(offset + 50).map(operation => ({ operation, value: undefined, pending: true, uncertain: false, unattempted: true })));
        return { results, ambiguous: true };
      }
    }
    return { results, ambiguous: false };
  }
  if (operations.length > 100) throw queueError("projection_limit", "projection mutation batch is oversized");
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
    result = operations.some(operation => operation.name === "createIssue") ? await withRetry(send, { maxRetries: 3, shouldRetry: isRejectedIssueCreation }, "work_queue createIssue batch") : await send();
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
    const rejectedBeforeExecution = isRejectedIssueCreation({ data: result, errors });
    const uncertain = pending && !rejectedBeforeExecution && !errors.some(error => (!error.path || error.path[0] === `m${index}`) && ["FORBIDDEN", "NOT_FOUND", "GRAPHQL_VALIDATION_FAILED"].includes(error.type));
    return { operation, value: failed ? undefined : value, pending, uncertain };
  });
  if (results.some(result => result.uncertain)) ambiguous = true;
  return { results, ambiguous };
}

function isRejectedIssueCreation(error) {
  // No deduplication guarantee exists for createIssue: partial results and
  // transport failures must never be replayed, even when normally transient.
  return Array.isArray(error.errors) && error.errors.length > 0 && error.errors.every(item => item.type === "RATE_LIMITED" && (!item.path || item.path.length === 0)) && Object.values(error.data || {}).every(value => value == null);
}

function issueResource(issue) {
  return { kind: "issue", host: "github.com", repository: issue.repository.nameWithOwner, repository_id: nativeId(issue.repository.databaseId), resource_id: nativeId(issue.databaseId), number: nativeId(issue.number) };
}

module.exports = { STATUSES, ISSUE_SELECTION, discoverTarget, preflightIssues, mutateIssues, issueResource, issueReadQuery };
