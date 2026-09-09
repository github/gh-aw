// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const { normalizeTemporaryId, isTemporaryId } = require("./temporary_id.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { matchesSimpleGlob } = require("./glob_pattern_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");

const WORK_ITEM_RELATIONS = {
  parent: "System.LinkTypes.Hierarchy-Reverse",
  child: "System.LinkTypes.Hierarchy-Forward",
  related: "System.LinkTypes.Related",
  predecessor: "System.LinkTypes.Dependency-Reverse",
  successor: "System.LinkTypes.Dependency-Forward",
  duplicate: "System.LinkTypes.Duplicate-Forward",
  "duplicate-of": "System.LinkTypes.Duplicate-Reverse",
};
const FIELD_REFERENCE_PATTERN = /^[A-Za-z][A-Za-z0-9_.]*$/;
const RESERVED_ASSIGNEES = new Set(["agency", "github copilot"]);
const DEFAULT_ATTACHMENT_SIZE = 5 * 1024 * 1024;

function failure(error) {
  return { success: false, error };
}

function staged(message, extra = {}) {
  logStagedPreviewInfo(message);
  return { success: true, staged: true, message, ...extra };
}

function normalizeAssignee(value) {
  const assignee = String(value || "").trim();
  if (!assignee) {
    throw new Error("assignee must not be empty");
  }
  if (RESERVED_ASSIGNEES.has(assignee.toLowerCase())) {
    throw new Error(`assignee '${assignee}' is a reserved identity`);
  }
  return assignee;
}

function matchesPattern(value, pattern) {
  return matchesSimpleGlob(value.toLowerCase(), String(pattern).trim().toLowerCase());
}

function validateTags(tags) {
  if (!Array.isArray(tags)) {
    throw new Error("tags must be an array");
  }
  return tags.map(tag => {
    const value = String(tag).trim();
    if (!value) {
      throw new Error("tags must not contain empty values");
    }
    if (value.includes(";")) {
      throw new Error(`tag '${value}' contains a semicolon`);
    }
    return value;
  });
}

function validateAllowedTags(tags, allowedTags) {
  if (!Array.isArray(allowedTags) || allowedTags.length === 0) return;
  const disallowed = tags.filter(tag => !allowedTags.some(pattern => matchesPattern(tag, pattern)));
  if (disallowed.length > 0) {
    throw new Error(`tags are not permitted by allowed-tags: ${disallowed.join(", ")}`);
  }
}

function validateAllowedPath(value, allowedPrefixes, fieldName) {
  if (!Array.isArray(allowedPrefixes) || allowedPrefixes.length === 0) return;
  const normalized = String(value).trim().toLowerCase();
  const allowed = allowedPrefixes.some(prefix => {
    const normalizedPrefix = String(prefix).trim().replace(/\\+$/, "").toLowerCase();
    return normalized === normalizedPrefix || normalized.startsWith(`${normalizedPrefix}\\`);
  });
  if (!allowed) {
    throw new Error(`${fieldName} is not permitted by the configured ${fieldName.replace("_", "-")} prefixes`);
  }
}

function getAzureDevOpsContext() {
  const rawOrgUrl = String(process.env.AZURE_DEVOPS_ORG_URL || "").trim();
  const project = String(process.env.SYSTEM_TEAMPROJECT || "").trim();
  const systemToken = String(process.env.SYSTEM_ACCESSTOKEN || "");
  const pat = String(process.env.AZURE_DEVOPS_EXT_PAT || "");
  if (!rawOrgUrl) throw new Error("AZURE_DEVOPS_ORG_URL is required");
  if (!project) throw new Error("SYSTEM_TEAMPROJECT is required");
  if (!systemToken && !pat) throw new Error("SYSTEM_ACCESSTOKEN or AZURE_DEVOPS_EXT_PAT is required");

  let parsed;
  try {
    parsed = new URL(rawOrgUrl);
  } catch {
    throw new Error("AZURE_DEVOPS_ORG_URL must be a valid HTTPS URL");
  }
  if (parsed.protocol !== "https:" || parsed.username || parsed.password || parsed.search || parsed.hash) {
    throw new Error("AZURE_DEVOPS_ORG_URL must be an HTTPS URL without credentials, query parameters, or fragments");
  }
  const host = parsed.hostname.toLowerCase();
  if (host !== "dev.azure.com" && !host.endsWith(".visualstudio.com")) {
    throw new Error("AZURE_DEVOPS_ORG_URL must use dev.azure.com or an organization.visualstudio.com host");
  }
  const pathSegments = parsed.pathname.split("/").filter(Boolean);
  if ((host === "dev.azure.com" && pathSegments.length !== 1) || (host.endsWith(".visualstudio.com") && pathSegments.length !== 0)) {
    throw new Error("AZURE_DEVOPS_ORG_URL must identify exactly one Azure DevOps organization");
  }
  parsed.pathname = parsed.pathname.replace(/\/+$/, "");

  return {
    orgUrl: parsed.toString().replace(/\/$/, ""),
    project,
    authorization: systemToken ? ["Bearer", systemToken].join(" ") : ["Basic", Buffer.from(`:${pat}`, "utf8").toString("base64")].join(" "),
  };
}

async function adoRequest(ado, method, apiPath, body, contentType = "application/json") {
  const url = `${ado.orgUrl}/${encodeURIComponent(ado.project)}${apiPath}`;
  let response;
  core.debug(`Azure DevOps API request started: ${method}`);
  try {
    response = await fetch(url, {
      method,
      headers: {
        Accept: "application/json",
        Authorization: ado.authorization,
        ...(body !== undefined ? { "Content-Type": contentType } : {}),
      },
      body: body === undefined ? undefined : Buffer.isBuffer(body) ? body : JSON.stringify(body),
      redirect: "manual",
      signal: AbortSignal.timeout(30_000),
    });
  } catch (error) {
    throw new Error(`Azure DevOps ${method} request could not be sent`, { cause: error });
  }
  core.debug(`Azure DevOps API request completed: ${method} HTTP ${response.status}`);
  if (response.status >= 300 && response.status < 400) {
    throw new Error(`Azure DevOps rejected a redirected ${method} request`);
  }
  if (!response.ok) {
    throw new Error(`Azure DevOps ${method} request failed with HTTP ${response.status} ${response.statusText}`);
  }
  if (response.status === 204) return {};
  let text;
  try {
    text = await response.text();
  } catch (error) {
    throw new Error(`Azure DevOps ${method} response body could not be read`, { cause: error });
  }
  if (!text) return {};
  try {
    return JSON.parse(text);
  } catch (error) {
    throw new Error(`Azure DevOps ${method} response was not valid JSON`, { cause: error });
  }
}

function workItemUrl(ado, id) {
  return `${ado.orgUrl}/${encodeURIComponent(ado.project)}/_workitems/edit/${id}`;
}

function resolveWorkItemReference(value, resolvedTemporaryIds, allowStaged) {
  if (typeof value === "number" || (typeof value === "string" && /^[1-9][0-9]*$/.test(value.trim()))) {
    const id = Number(value);
    if (!Number.isSafeInteger(id) || id < 1) throw new Error("work item ID must be a positive safe integer");
    return { id, sameRun: false };
  }
  if (!isTemporaryId(value)) {
    throw new Error("work item ID must be a positive integer or #aw_ temporary ID");
  }
  const key = normalizeTemporaryId(String(value));
  const resolved = resolvedTemporaryIds?.[key];
  if (!resolved || resolved.provider !== "azure-devops" || resolved.resourceType !== "work-item") {
    throw new Error(`temporary work-item ID '#${key}' has not been resolved by ado_create_work_item in this run`);
  }
  const id = Number(resolved.workItemId);
  if (Number.isSafeInteger(id) && id > 0) return { id, sameRun: true };
  if (allowStaged && resolved.staged === true) return { id: null, sameRun: true };
  throw new Error(`temporary work-item ID '#${key}' does not reference a created work item`);
}

function targetAllowsId(target, id) {
  if (target === "*") return true;
  if (Number.isSafeInteger(target)) return target === id;
  if (Array.isArray(target)) return target.includes(id);
  return null;
}

async function getWorkItem(ado, id, fields = []) {
  const query = fields.length > 0 ? `&fields=${fields.map(encodeURIComponent).join(",")}` : "";
  return adoRequest(ado, "GET", `/_apis/wit/workitems/${id}?api-version=7.0${query}`);
}

async function enforceTarget(ado, target, id) {
  if (target == null) throw new Error("target is required for pre-existing work items");
  const allowed = targetAllowsId(target, id);
  if (allowed === true) return;
  if (allowed === false) throw new Error(`work item #${id} is not permitted by the target configuration`);
  if (typeof target !== "string" || !target.trim()) throw new Error("target must be '*', a positive ID, a list of IDs, or an area path");

  const workItem = await getWorkItem(ado, id, ["System.AreaPath"]);
  const areaPath = String(workItem?.fields?.["System.AreaPath"] || "");
  const prefix = target.trim();
  const areaLower = areaPath.toLowerCase();
  const prefixLower = prefix.toLowerCase();
  if (areaLower !== prefixLower && !areaLower.startsWith(`${prefixLower}\\`)) {
    throw new Error(`work item #${id} is outside the configured area-path target`);
  }
}

function fieldPatch(op, field, value) {
  if (!FIELD_REFERENCE_PATTERN.test(field)) throw new Error(`invalid Azure DevOps field reference '${field}'`);
  return { op, path: `/fields/${field}`, value };
}

function validateUniqueFields(entries) {
  const seen = new Map();
  for (const [label, field] of entries) {
    const key = field.toLowerCase();
    if (seen.has(key)) throw new Error(`${label} field '${field}' duplicates ${seen.get(key)} field`);
    seen.set(key, label);
  }
}

async function handleCreateWorkItem(message, config, resolvedTemporaryIds) {
  const temporaryId = String(message.temporary_id || "");
  if (!/^#aw_[A-Za-z0-9_]{3,12}$/.test(temporaryId)) return failure("ado_create_work_item requires a server-generated temporary_id");
  const normalized = normalizeTemporaryId(temporaryId);
  if (resolvedTemporaryIds?.[normalized]) return failure(`temporary_id '${temporaryId}' was already used in this run`);

  try {
    const title = String(message.title || "").trim();
    const description = String(message.description || "").trim();
    if (title.length < 6 || title.length > 255) throw new Error("title must contain 6 to 255 characters");
    if (description.length < 31 || description.length > 65000) throw new Error("description must contain 31 to 65000 characters");
    const agentTags = validateTags(message.tags || []);
    validateAllowedTags(agentTags, config.allowed_tags);
    const staticTags = validateTags(config.tags || []);
    const tags = [...staticTags];
    for (const tag of agentTags) {
      if (!tags.some(existing => existing.toLowerCase() === tag.toLowerCase())) tags.push(tag);
    }
    const workItemType = String(config.work_item_type || "Task").trim();
    const descriptionField = String(config.description_field || (workItemType.toLowerCase() === "bug" ? "Microsoft.VSTS.TCM.ReproSteps" : "System.Description"));
    const customFields = config.custom_fields && typeof config.custom_fields === "object" ? config.custom_fields : {};
    const configuredFields = [
      ["title", "System.Title"],
      ["description", descriptionField],
      ...(config.area_path ? [["area_path", "System.AreaPath"]] : []),
      ...(config.iteration_path ? [["iteration_path", "System.IterationPath"]] : []),
      ...(config.assignee ? [["assignee", "System.AssignedTo"]] : []),
      ...(tags.length > 0 ? [["tags", "System.Tags"]] : []),
      ...Object.keys(customFields).map(field => ["custom_fields", field]),
    ];
    validateUniqueFields(configuredFields);
    if (config.assignee) normalizeAssignee(config.assignee);

    if (isStagedMode(config)) {
      return staged(`Would create Azure DevOps ${workItemType}`, {
        temporaryId,
        temporaryIdEntry: { provider: "azure-devops", resourceType: "work-item", staged: true },
      });
    }

    const ado = getAzureDevOpsContext();
    const patch = [fieldPatch("add", "System.Title", title), fieldPatch("add", descriptionField, description), { op: "add", path: `/multilineFieldsFormat/${descriptionField}`, value: "Markdown" }];
    if (config.area_path) patch.push(fieldPatch("add", "System.AreaPath", String(config.area_path)));
    if (config.iteration_path) patch.push(fieldPatch("add", "System.IterationPath", String(config.iteration_path)));
    if (config.assignee) patch.push(fieldPatch("add", "System.AssignedTo", normalizeAssignee(config.assignee)));
    if (tags.length > 0) patch.push(fieldPatch("add", "System.Tags", tags.join("; ")));
    for (const field of Object.keys(customFields).sort((a, b) => a.localeCompare(b))) {
      patch.push(fieldPatch("add", field, String(customFields[field])));
    }

    if (config.artifact_link?.enabled === true) {
      const repository = String(config.artifact_link.repository || process.env.BUILD_REPOSITORY_NAME || "").trim();
      if (!repository) throw new Error("artifact-link requires a repository or BUILD_REPOSITORY_NAME");
      const repo = await adoRequest(ado, "GET", `/_apis/git/repositories/${encodeURIComponent(repository)}?api-version=7.0`);
      if (!repo?.id) throw new Error("Azure DevOps repository response did not contain an ID");
      const branch = String(config.artifact_link.branch || "main");
      patch.push({
        op: "add",
        path: "/relations/-",
        value: {
          rel: "ArtifactLink",
          url: `vstfs:///Git/Ref/${ado.project}%2F${repo.id}%2FGB${branch}`,
          attributes: { name: "Branch" },
        },
      });
    }

    const created = await adoRequest(ado, "POST", `/_apis/wit/workitems/$${encodeURIComponent(workItemType)}?api-version=7.0`, patch, "application/json-patch+json");
    const id = Number(created?.id);
    if (!Number.isSafeInteger(id) || id < 1) throw new Error("Azure DevOps create response did not contain a valid work-item ID");
    const url = workItemUrl(ado, id);
    return {
      success: true,
      number: id,
      url,
      temporaryId,
      metadata: { provider: "azure-devops", project: ado.project, work_item_id: id },
      temporaryIdEntry: { provider: "azure-devops", resourceType: "work-item", workItemId: id, url },
    };
  } catch (error) {
    return failure(error instanceof Error ? error.message : String(error));
  }
}

async function handleUpdateWorkItem(message, config, resolvedTemporaryIds) {
  try {
    const preview = isStagedMode(config);
    const resolved = resolveWorkItemReference(message.id, resolvedTemporaryIds, preview);
    const fields = [
      ["title", "System.Title", config.title],
      ["body", "System.Description", config.body],
      ["state", "System.State", config.status],
      ["area_path", "System.AreaPath", config.area_path],
      ["iteration_path", "System.IterationPath", config.iteration_path],
      ["assignee", "System.AssignedTo", config.assignee],
      ["tags", "System.Tags", config.tags],
    ];
    const requested = fields.filter(([name]) => message[name] !== undefined);
    if (requested.length === 0) throw new Error("at least one update field is required");
    const disabled = requested.find(([, , enabled]) => enabled !== true);
    if (disabled) throw new Error(`${disabled[0]} updates are not enabled by ado_update_work_item`);
    if (message.assignee !== undefined) message.assignee = normalizeAssignee(message.assignee);
    if (message.tags !== undefined) {
      message.tags = validateTags(message.tags);
      validateAllowedTags(message.tags, config.allowed_tags);
    }
    if (message.area_path !== undefined) {
      validateAllowedPath(message.area_path, config.allowed_area_prefixes, "area_path");
    }
    if (message.iteration_path !== undefined) {
      validateAllowedPath(message.iteration_path, config.allowed_iteration_prefixes, "iteration_path");
    }
    if (preview) return staged(`Would update Azure DevOps work item ${message.id}`);

    const ado = getAzureDevOpsContext();
    if (!resolved.sameRun) await enforceTarget(ado, config.target, resolved.id);
    if (config.title_prefix || config.tag_prefix) {
      const current = await getWorkItem(ado, resolved.id, ["System.Title", "System.Tags"]);
      if (config.title_prefix && !String(current?.fields?.["System.Title"] || "").startsWith(String(config.title_prefix))) {
        throw new Error(`work item #${resolved.id} does not match title-prefix`);
      }
      if (config.tag_prefix) {
        const tags = String(current?.fields?.["System.Tags"] || "")
          .split(";")
          .map(tag => tag.trim());
        if (!tags.some(tag => tag.startsWith(String(config.tag_prefix)))) throw new Error(`work item #${resolved.id} does not match tag-prefix`);
      }
    }
    const patch = requested.map(([name, field]) => {
      const value = name === "tags" ? message.tags.join("; ") : message[name];
      return fieldPatch("add", field, value);
    });
    if (message.body !== undefined && config.markdown_body === true) {
      patch.push({ op: "add", path: "/multilineFieldsFormat/System.Description", value: "Markdown" });
    }
    await adoRequest(ado, "PATCH", `/_apis/wit/workitems/${resolved.id}?api-version=7.0`, patch, "application/json-patch+json");
    return { success: true, number: resolved.id, url: workItemUrl(ado, resolved.id), metadata: { provider: "azure-devops", project: ado.project } };
  } catch (error) {
    return failure(error instanceof Error ? error.message : String(error));
  }
}

async function handleCommentOnWorkItem(message, config, resolvedTemporaryIds) {
  try {
    const preview = isStagedMode(config);
    const resolved = resolveWorkItemReference(message.work_item_id, resolvedTemporaryIds, preview);
    if (preview) return staged(`Would comment on Azure DevOps work item ${message.work_item_id}`);
    const ado = getAzureDevOpsContext();
    if (!resolved.sameRun) await enforceTarget(ado, config.target, resolved.id);
    const comment = await adoRequest(ado, "POST", `/_apis/wit/workItems/${resolved.id}/comments?api-version=7.1-preview.4`, { text: String(message.body) });
    return {
      success: true,
      number: resolved.id,
      url: workItemUrl(ado, resolved.id),
      metadata: { provider: "azure-devops", project: ado.project, comment_id: comment?.id },
    };
  } catch (error) {
    return failure(error instanceof Error ? error.message : String(error));
  }
}

async function handleAssignWorkItem(message, config, resolvedTemporaryIds) {
  try {
    const assignee = normalizeAssignee(message.assignee);
    if (Array.isArray(config.allowed) && config.allowed.length > 0 && !config.allowed.some(value => String(value).trim().toLowerCase() === assignee.toLowerCase())) {
      throw new Error(`assignee '${assignee}' is not permitted by ado-assign-work-item.allowed`);
    }
    if (Array.isArray(config.blocked) && config.blocked.some(pattern => matchesPattern(assignee, pattern))) {
      throw new Error(`assignee '${assignee}' is blocked by ado-assign-work-item.blocked`);
    }
    const preview = isStagedMode(config);
    const resolved = resolveWorkItemReference(message.work_item_id, resolvedTemporaryIds, preview);
    if (preview) return staged(`Would assign Azure DevOps work item ${message.work_item_id} to '${assignee}'`);
    const ado = getAzureDevOpsContext();
    if (!resolved.sameRun) await enforceTarget(ado, config.target, resolved.id);
    await adoRequest(ado, "PATCH", `/_apis/wit/workitems/${resolved.id}?api-version=7.0`, [fieldPatch("add", "System.AssignedTo", assignee)], "application/json-patch+json");
    return { success: true, number: resolved.id, url: workItemUrl(ado, resolved.id), metadata: { provider: "azure-devops", project: ado.project, assignee } };
  } catch (error) {
    return failure(error instanceof Error ? error.message : String(error));
  }
}

async function handleLinkWorkItems(message, config, resolvedTemporaryIds) {
  try {
    const relation = WORK_ITEM_RELATIONS[message.link_type];
    if (!relation) throw new Error(`invalid link_type '${message.link_type}'`);
    if (Array.isArray(config.allowed_link_types) && config.allowed_link_types.length > 0 && !config.allowed_link_types.includes(message.link_type)) {
      throw new Error(`link_type '${message.link_type}' is not permitted by allowed-link-types`);
    }
    const preview = isStagedMode(config);
    const source = resolveWorkItemReference(message.source_id, resolvedTemporaryIds, preview);
    const target = resolveWorkItemReference(message.target_id, resolvedTemporaryIds, preview);
    if (source.id !== null && source.id === target.id) throw new Error("source_id and target_id must identify different work items");
    if (preview) return staged(`Would link Azure DevOps work items ${message.source_id} and ${message.target_id}`);
    const ado = getAzureDevOpsContext();
    if (!source.sameRun) await enforceTarget(ado, config.target, source.id);
    if (!target.sameRun) await enforceTarget(ado, config.target, target.id);
    const value = {
      rel: relation,
      url: `${ado.orgUrl}/${encodeURIComponent(ado.project)}/_apis/wit/workitems/${target.id}`,
      ...(message.comment ? { attributes: { comment: String(message.comment) } } : {}),
    };
    await adoRequest(ado, "PATCH", `/_apis/wit/workitems/${source.id}?api-version=7.1`, [{ op: "add", path: "/relations/-", value }], "application/json-patch+json");
    return {
      success: true,
      number: source.id,
      url: workItemUrl(ado, source.id),
      metadata: { provider: "azure-devops", project: ado.project, target_work_item_id: target.id, link_type: message.link_type },
    };
  } catch (error) {
    return failure(error instanceof Error ? error.message : String(error));
  }
}

function readStagedAttachment(message, config) {
  const stagedFile = String(message.staged_file || "");
  if (!/^[A-Za-z0-9._/-]+$/.test(stagedFile) || stagedFile.startsWith("/") || stagedFile.split("/").some(segment => !segment || segment === "." || segment === "..")) {
    throw new Error("staged attachment path is invalid");
  }
  const root = path.resolve(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "upload-artifacts");
  const filePath = path.resolve(root, ...stagedFile.split("/"));
  if (!filePath.startsWith(root + path.sep)) throw new Error("staged attachment path escapes the staging directory");
  let current = root;
  let stat;
  for (const segment of stagedFile.split("/")) {
    current = path.join(current, segment);
    stat = fs.lstatSync(current);
    if (stat.isSymbolicLink()) throw new Error("staged attachment must not contain symbolic links");
  }
  if (!stat?.isFile()) throw new Error("staged attachment must be a regular file");
  const maxFileSize = Number(config.max_file_size || DEFAULT_ATTACHMENT_SIZE);
  if (!Number.isSafeInteger(maxFileSize) || maxFileSize < 1 || stat.size > maxFileSize) {
    throw new Error(`attachment exceeds max-file-size of ${maxFileSize} bytes`);
  }
  const originalPath = String(message.file_path || "");
  const allowedExtensions = Array.isArray(config.allowed_extensions) ? config.allowed_extensions : [];
  if (allowedExtensions.length > 0 && !allowedExtensions.some(extension => originalPath.toLowerCase().endsWith(String(extension).toLowerCase()))) {
    throw new Error("attachment extension is not permitted");
  }
  let bytes;
  try {
    bytes = fs.readFileSync(filePath);
  } catch (error) {
    throw new Error("staged attachment could not be read", { cause: error });
  }
  if (bytes.includes(Buffer.from("##vso["))) throw new Error("attachment contains an Azure Pipelines command sequence");
  const originalSegments = originalPath.split(/[\\/]+/).filter(Boolean);
  const originalBasename = originalSegments.length > 0 ? originalSegments[originalSegments.length - 1] : "";
  return { bytes, filename: originalBasename || path.basename(filePath) };
}

async function handleUploadWorkItemAttachment(message, config, resolvedTemporaryIds) {
  try {
    const preview = isStagedMode(config);
    const resolved = resolveWorkItemReference(message.work_item_id, resolvedTemporaryIds, preview);
    if (preview) return staged(`Would attach a file to Azure DevOps work item ${message.work_item_id}`);
    const { bytes, filename } = readStagedAttachment(message, config);
    const ado = getAzureDevOpsContext();
    if (!resolved.sameRun) await enforceTarget(ado, config.target, resolved.id);
    const upload = await adoRequest(ado, "POST", `/_apis/wit/attachments?fileName=${encodeURIComponent(filename)}&api-version=7.1`, bytes, "application/octet-stream");
    const attachmentUrl = String(upload?.url || "");
    let parsedAttachmentUrl;
    try {
      parsedAttachmentUrl = new URL(attachmentUrl);
    } catch {
      throw new Error("Azure DevOps attachment response did not contain a valid URL");
    }
    const attachmentHost = parsedAttachmentUrl.hostname.toLowerCase();
    if (parsedAttachmentUrl.protocol !== "https:" || (attachmentHost !== "dev.azure.com" && !attachmentHost.endsWith(".visualstudio.com"))) {
      throw new Error("Azure DevOps attachment response contained an untrusted URL");
    }
    const comment = `${config.comment_prefix || ""}${message.comment || "Uploaded by agent"}`;
    const patch = [
      {
        op: "add",
        path: "/relations/-",
        value: { rel: "AttachedFile", url: attachmentUrl, attributes: { comment } },
      },
    ];
    await adoRequest(ado, "PATCH", `/_apis/wit/workitems/${resolved.id}?api-version=7.1`, patch, "application/json-patch+json");
    return {
      success: true,
      number: resolved.id,
      url: workItemUrl(ado, resolved.id),
      metadata: { provider: "azure-devops", project: ado.project, attachment_url: attachmentUrl, file_name: filename },
    };
  } catch (error) {
    return failure(error instanceof Error ? error.message : String(error));
  }
}

const HANDLERS = {
  ado_create_work_item: handleCreateWorkItem,
  ado_update_work_item: handleUpdateWorkItem,
  ado_comment_on_work_item: handleCommentOnWorkItem,
  ado_assign_work_item: handleAssignWorkItem,
  ado_link_work_items: handleLinkWorkItems,
  ado_upload_workitem_attachment: handleUploadWorkItemAttachment,
};

function createAzureDevOpsWorkItemHandler(type, config = {}) {
  const handler = HANDLERS[type];
  if (!handler) throw new Error(`Unsupported Azure DevOps safe-output type '${type}'`);
  return async (message, resolvedTemporaryIds = {}) => handler({ ...message }, config, resolvedTemporaryIds);
}

module.exports = {
  createAzureDevOpsWorkItemHandler,
  getAzureDevOpsContext,
  resolveWorkItemReference,
  targetAllowsId,
};
