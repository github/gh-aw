// @ts-check

const { getErrorMessage } = require("./error_helpers.cjs");
const { validateTargetRepo, parseAllowedRepos, getDefaultTargetRepo } = require("./repo_helpers.cjs");

const fs = require("fs");

const normalizeConfiguredToolName = name => String(name).replace(/-/g, "_").toLowerCase();

/**
 * Check whether a schema enforces strict object keys.
 * @param {any} inputSchema - Tool input schema
 * @returns {boolean} True when additional properties are disallowed
 */
function isStrictSchema(inputSchema) {
  if (!inputSchema || typeof inputSchema !== "object") {
    return false;
  }
  if (inputSchema.additionalProperties !== false) {
    return false;
  }
  const { properties } = inputSchema;
  return !!properties && typeof properties === "object" && !Array.isArray(properties);
}

/**
 * Strip unknown keys from tool arguments when schema is strict.
 * @param {any} args - Tool call arguments
 * @param {any} inputSchema - Tool input schema
 * @param {(keys: string[]) => void} [onUnknownKeysStripped] - Optional callback for stripped keys
 * @returns {any} Sanitized args
 */
function sanitizeArgsBySchema(args, inputSchema, onUnknownKeysStripped) {
  if (!args || typeof args !== "object" || Array.isArray(args)) {
    return args;
  }
  if (!isStrictSchema(inputSchema)) {
    return args;
  }

  const allowedKeys = new Set(Object.keys(inputSchema.properties));
  const sanitizedArgs = {};
  const strippedKeys = [];
  for (const [key, value] of Object.entries(args)) {
    if (allowedKeys.has(key)) {
      sanitizedArgs[key] = value;
    } else {
      strippedKeys.push(key);
    }
  }
  if (strippedKeys.length > 0 && typeof onUnknownKeysStripped === "function") {
    onUnknownKeysStripped(strippedKeys);
  }
  return sanitizedArgs;
}

/**
 * Map from safe-outputs config key to the tool-definition metadata field that marks a
 * dynamically generated tool belonging to that config key. Such tools are named after their
 * target (e.g. a tool named `my_workflow` for the `call_workflow` config key), so they cover
 * the config key even though their name differs from it.
 * @type {Record<string, string>}
 */
const DYNAMIC_TOOL_METADATA_BY_CONFIG_KEY = {
  dispatch_workflow: "_workflow_name",
  dispatch_repository: "_dispatch_repository_tool",
  call_workflow: "_call_workflow_name",
};

/**
 * Safe-outputs config keys that never correspond to an agent-facing tool: handler-only output
 * types produced by other handlers, and global configuration knobs.
 * @type {Set<string>}
 */
const NON_TOOL_CONFIG_KEYS = new Set(["create_report_incomplete_issue", "create_missing_tool_issue", "create_missing_data_issue", "mentions", "max_bot_mentions"]);

/**
 * Check whether a config key is already covered by a dynamically generated tool that was
 * renamed after its target workflow/tool (identified by tool metadata rather than by name).
 * @param {Array} tools - Array of tool definitions
 * @param {string} normalizedKey - Normalized config key
 * @returns {boolean} True when a tool of the same family is already defined
 */
function isConfigKeyCoveredByDynamicTool(tools, normalizedKey) {
  const metadataKey = DYNAMIC_TOOL_METADATA_BY_CONFIG_KEY[normalizedKey];
  if (!metadataKey) {
    return false;
  }
  if (metadataKey === "_workflow_name" || metadataKey === "_call_workflow_name") {
    return tools.some(tool => tool && hasValidWorkflowMetadataName(tool[metadataKey]));
  }
  return tools.some(tool => tool && tool[metadataKey]);
}

/**
 * Check whether workflow metadata name is a non-empty string after trimming.
 * @param {any} workflowName
 * @returns {boolean}
 */
function hasValidWorkflowMetadataName(workflowName) {
  return typeof workflowName === "string" && workflowName.trim().length > 0;
}

/**
 * Load tools from tools.json file
 * @param {Object} server - The MCP server instance for logging
 * @returns {Array} Array of tool definitions
 */
function loadTools(server) {
  const toolsPath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH || `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/tools.json`;

  server.debug(`Reading tools from file: ${toolsPath}`);

  if (!fs.existsSync(toolsPath)) {
    server.debug(`Tools file does not exist at: ${toolsPath}`);
    server.debug(`Using empty tools array`);
    return [];
  }

  try {
    server.debug(`Tools file exists at: ${toolsPath}`);
    const toolsFileContent = fs.readFileSync(toolsPath, "utf8");
    server.debug(`Tools file content length: ${toolsFileContent.length} characters`);
    server.debug(`Tools file read successfully, attempting to parse JSON`);
    const tools = JSON.parse(toolsFileContent);
    server.debug(`Successfully parsed ${tools.length} tools from file`);

    // Log details about dispatch_workflow tools for debugging
    const dispatchWorkflowTools = tools.filter(t => t._workflow_name);
    if (dispatchWorkflowTools.length > 0) {
      server.debug(`  Found ${dispatchWorkflowTools.length} dispatch_workflow tools:`);
      dispatchWorkflowTools.forEach(t => {
        server.debug(`    - ${t.name} (workflow: ${t._workflow_name})`);
      });
    }

    // Log details about dispatch_repository tools for debugging
    const dispatchRepositoryTools = tools.filter(t => t._dispatch_repository_tool);
    if (dispatchRepositoryTools.length > 0) {
      server.debug(`  Found ${dispatchRepositoryTools.length} dispatch_repository tools:`);
      dispatchRepositoryTools.forEach(t => {
        server.debug(`    - ${t.name} (tool: ${t._dispatch_repository_tool})`);
      });
    }

    // Log details about call_workflow tools for debugging
    const callWorkflowTools = tools.filter(t => t._call_workflow_name);
    if (callWorkflowTools.length > 0) {
      server.debug(`  Found ${callWorkflowTools.length} call_workflow tools:`);
      callWorkflowTools.forEach(t => {
        server.debug(`    - ${t.name} (workflow: ${t._call_workflow_name})`);
      });
    }

    return tools;
  } catch (error) {
    server.debug(`Error reading tools file: ${getErrorMessage(error)}`);
    server.debug(`Falling back to empty tools array`);
    return [];
  }
}

/**
 * Attach handlers to tools
 * @param {Array} tools - Array of tool definitions
 * @param {Object} handlers - Object containing handler functions
 * @param {{ debug?: Function }} [logger] - Optional logger
 * @returns {Array} Tools with handlers attached
 */
function attachHandlers(tools, handlers, logger) {
  const handlerMap = {
    create_issue: handlers.createIssueHandler,
    jira_create_issue: handlers.jiraCreateIssueHandler,
    jira_update_issue: handlers.jiraUpdateIssueHandler,
    jira_add_comment: handlers.jiraAddCommentHandler,
    jira_add_label: handlers.jiraAddLabelHandler,
    create_pull_request: handlers.createPullRequestHandler,
    push_to_pull_request_branch: handlers.pushToPullRequestBranchHandler,
    push_repo_memory: handlers.pushRepoMemoryHandler,
    upload_asset: handlers.uploadAssetHandler,
    upload_artifact: handlers.uploadArtifactHandler,
    upload_code_coverage: handlers.uploadCodeCoverageHandler,
    create_project: handlers.createProjectHandler,
    add_comment: handlers.addCommentHandler,
    create_pull_request_review_comment: handlers.createPullRequestReviewCommentHandler,
    submit_pull_request_review: handlers.submitPullRequestReviewHandler,
    dismiss_pull_request_review: handlers.dismissPullRequestReviewHandler,
    update_issue: handlers.updateIssueHandler,
    update_pull_request: handlers.updatePullRequestHandler,
    close_pull_request: handlers.closePullRequestHandler,
    merge_pull_request: handlers.mergePullRequestHandler,
    mark_pull_request_as_ready_for_review: handlers.markPullRequestAsReadyForReviewHandler,
    add_reviewer: handlers.addReviewerHandler,
    reply_to_pull_request_review_comment: handlers.replyToPullRequestReviewCommentHandler,
    close_issue: handlers.closeIssueHandler,
    add_labels: handlers.addLabelsHandler,
    remove_labels: handlers.removeLabelsHandler,
    update_discussion: handlers.updateDiscussionHandler,
    close_discussion: handlers.closeDiscussionHandler,
    ado_create_work_item: handlers.createWorkItemHandler,
    ado_update_work_item: handlers.updateWorkItemHandler,
    ado_comment_on_work_item: handlers.commentOnWorkItemHandler,
    ado_assign_work_item: handlers.assignWorkItemHandler,
    ado_link_work_items: handlers.linkWorkItemsHandler,
    ado_upload_workitem_attachment: handlers.uploadWorkItemAttachmentHandler,
  };

  tools.forEach(tool => {
    const handler = handlerMap[normalizeConfiguredToolName(tool.name)];
    if (handler) {
      tool.handler = handler;
    } else if (typeof handlers.defaultHandler === "function") {
      tool.handler = handlers.defaultHandler(tool.name);
    }

    // Check if this is a dispatch_workflow tool (dynamic tool with workflow metadata)
    if (hasValidWorkflowMetadataName(tool._workflow_name)) {
      // Create a custom handler that wraps args in inputs and adds workflow_name
      const workflowName = tool._workflow_name.trim();
      tool.handler = args => {
        const { ref, ...inputs } = args ?? {};
        // Wrap workflow inputs in inputs and pass dispatch ref as top-level field
        return handlers.defaultHandler("dispatch_workflow")({
          ...(ref && { ref }),
          ...(args !== undefined && { inputs }),
          workflow_name: workflowName,
        });
      };
    }

    // Check if this is a dispatch_repository tool (dynamic tool with dispatch_repository metadata)
    if (tool._dispatch_repository_tool) {
      const toolKey = tool._dispatch_repository_tool;
      tool.handler = args => {
        return handlers.defaultHandler("dispatch_repository")({
          inputs: args,
          tool_name: toolKey,
        });
      };
    }

    // Check if this is a call_workflow tool (dynamic tool with call workflow metadata)
    if (hasValidWorkflowMetadataName(tool._call_workflow_name)) {
      // Create a custom handler that wraps args in inputs and adds workflow_name
      const workflowName = tool._call_workflow_name.trim();
      tool.handler = args => {
        // Wrap args in inputs property to match call_workflow schema
        return handlers.defaultHandler("call_workflow")({
          inputs: args,
          workflow_name: workflowName,
        });
      };
    }

    if (typeof tool.handler === "function" && isStrictSchema(tool.inputSchema)) {
      const originalHandler = tool.handler;
      tool.handler = args =>
        originalHandler(
          sanitizeArgsBySchema(args, tool.inputSchema, strippedKeys => {
            logger?.debug?.(`Stripped unknown keys for strict schema tool '${tool.name}': ${JSON.stringify(strippedKeys)}`);
          })
        );
    }
  });

  return tools;
}

/**
 * Register predefined tools based on configuration
 * @param {Object} server - The MCP server instance
 * @param {Array} tools - Array of tool definitions
 * @param {Object} config - Safe outputs configuration
 * @param {Function} registerTool - Function to register a tool
 * @param {Function} normalizeTool - Function to normalize tool names
 */
function registerPredefinedTools(server, tools, config, registerTool, normalizeTool) {
  const toolSafetyWarnings = {
    add_comment: " This tool records a real comment intent. Do not use it for placeholder comments, auth checks, or probing. Call it only when the final comment body is ready; otherwise use noop or report_incomplete.",
    create_issue: " This tool records a real issue intent. Do not use it for placeholder titles/bodies, auth checks, or probing. Call it only when the final issue title/body are ready; otherwise use noop or report_incomplete.",
    create_pull_request: " This tool records a real pull request intent. Do not use it for tests, auth checks, or probing. Call it once only when the final PR title/body/branch are ready; otherwise use noop or report_incomplete.",
    push_to_pull_request_branch:
      " This tool records a real PR branch update intent. Do not use it for probe branches, placeholder commit messages, auth checks, or probing. Call it only when the final branch update is ready; otherwise use noop or report_incomplete.",
  };

  tools.forEach(tool => {
    // Check if this is a regular tool matching a config key
    const normalizedToolName = normalizeTool(tool.name);
    if (Object.keys(config).find(configKey => normalizeTool(configKey) === normalizedToolName)) {
      let toolToRegister = tool;
      const safetyWarning = toolSafetyWarnings[normalizedToolName];
      const isCreatePullRequestTool = normalizedToolName === "create_pull_request" && config.create_pull_request;
      // Enrich create_pull_request tool description when target-repo is configured
      if (safetyWarning || isCreatePullRequestTool) {
        // The handler is a function and cannot be structurally cloned, so it is
        // separated out and re-attached to the clone below.
        const { handler, ...cloneableTool } = tool;
        try {
          toolToRegister = structuredClone(cloneableTool);
        } catch (err) {
          throw new Error("Failed to deep-copy tool " + tool.name + ": " + getErrorMessage(err), { cause: err });
        }
        if (handler) {
          toolToRegister.handler = handler;
        }
        if (safetyWarning) {
          toolToRegister.description += safetyWarning;
        }
      }
      if (isCreatePullRequestTool) {
        const targetRepo = config.create_pull_request["target-repo"];
        if (targetRepo) {
          // Validate the configured target-repo against the allowed-repos list
          const allowedRepos = parseAllowedRepos(config.create_pull_request.allowed_repos);
          if (allowedRepos.size > 0) {
            const defaultRepo = getDefaultTargetRepo(config.create_pull_request);
            const validation = validateTargetRepo(targetRepo, defaultRepo, allowedRepos);
            if (!validation.valid) {
              server.debug(`WARNING: SEC-005: ${validation.error}`);
            }
          }
          toolToRegister.description += ` Note: This workflow is configured to create pull requests in '${targetRepo}'. You do not need to specify the repo parameter.`;
          if (toolToRegister.inputSchema && toolToRegister.inputSchema.properties && toolToRegister.inputSchema.properties.repo) {
            toolToRegister.inputSchema.properties.repo.description += ` Configured default: '${targetRepo}'.`;
          }
        }
      }
      registerTool(server, toolToRegister);
      return;
    }

    // Check if this is a dispatch_workflow tool (has _workflow_name metadata)
    // These tools are dynamically generated with workflow-specific names
    if (hasValidWorkflowMetadataName(tool._workflow_name)) {
      server.debug(`Found dispatch_workflow tool: ${tool.name} (_workflow_name: ${tool._workflow_name})`);
      if (config.dispatch_workflow) {
        server.debug(`  dispatch_workflow config exists, registering tool`);
        registerTool(server, tool);
        return;
      } else {
        // Note: Using server.debug() with "WARNING:" prefix since MCP server only provides
        // debug and debugError methods. The prefix helps identify severity in logs.
        server.debug(`  WARNING: dispatch_workflow config is missing or falsy - tool will NOT be registered`);
        server.debug(`  Config keys: ${Object.keys(config).join(", ")}`);
        server.debug(`  config.dispatch_workflow value: ${JSON.stringify(config.dispatch_workflow)}`);
      }
    }

    // Check if this is a dispatch_repository tool (has _dispatch_repository_tool metadata)
    // These tools are dynamically generated with tool-specific names
    if (tool._dispatch_repository_tool) {
      server.debug(`Found dispatch_repository tool: ${tool.name} (_dispatch_repository_tool: ${tool._dispatch_repository_tool})`);
      if (config.dispatch_repository) {
        server.debug(`  dispatch_repository config exists, registering tool`);
        registerTool(server, tool);
        return;
      } else {
        server.debug(`  WARNING: dispatch_repository config is missing or falsy - tool will NOT be registered`);
        server.debug(`  Config keys: ${Object.keys(config).join(", ")}`);
        server.debug(`  config.dispatch_repository value: ${JSON.stringify(config.dispatch_repository)}`);
      }
    }

    // Check if this is a call_workflow tool (has _call_workflow_name metadata)
    // These tools are dynamically generated with workflow-specific names
    if (hasValidWorkflowMetadataName(tool._call_workflow_name)) {
      server.debug(`Found call_workflow tool: ${tool.name} (_call_workflow_name: ${tool._call_workflow_name})`);
      if (config.call_workflow) {
        server.debug(`  call_workflow config exists, registering tool`);
        registerTool(server, tool);
        return;
      } else {
        server.debug(`  WARNING: call_workflow config is missing or falsy - tool will NOT be registered`);
        server.debug(`  Config keys: ${Object.keys(config).join(", ")}`);
        server.debug(`  config.call_workflow value: ${JSON.stringify(config.call_workflow)}`);
      }
    }
  });
}

/**
 * Register dynamic safe-job tools based on configuration
 * @param {Object} server - The MCP server instance
 * @param {Array} tools - Array of predefined tool definitions
 * @param {Object} config - Safe outputs configuration
 * @param {string} outputFile - Path to the output file
 * @param {Function} registerTool - Function to register a tool
 * @param {Function} normalizeTool - Function to normalize tool names
 */
function registerDynamicTools(server, tools, config, outputFile, registerTool, normalizeTool) {
  Object.keys(config).forEach(configKey => {
    const normalizedKey = normalizeTool(configKey);

    // Skip config keys that are never exposed as agent-facing tools (handler-only output
    // types and global configuration knobs).
    if (NON_TOOL_CONFIG_KEYS.has(normalizedKey)) {
      server.debug(`Skipping generic tool for non-tool config key: ${configKey}`);
      return;
    }

    // Skip if it's already a predefined tool, or if a dynamically generated tool named after
    // its target (identified by metadata) already covers this config key.
    if (server.tools[normalizedKey] || tools.find(t => normalizeTool(t.name) === normalizedKey)) {
      return;
    }
    if (isConfigKeyCoveredByDynamicTool(tools, normalizedKey)) {
      server.debug(`Skipping generic tool for '${configKey}': already covered by dynamically generated tool(s)`);
      return;
    }

    const jobConfig = config[configKey];

    // Create a dynamic tool for this safe-job
    const dynamicTool = {
      name: normalizedKey,
      description: jobConfig?.description ?? `Custom safe-job: ${configKey}`,
      inputSchema: {
        type: "object",
        properties: {},
        additionalProperties: true, // Allow any properties for flexibility
      },
      handler: args => {
        // Create a generic safe-job output entry
        const entry = { type: normalizedKey, ...args };

        // Write the entry to the output file in JSONL format
        // CRITICAL: Use JSON.stringify WITHOUT formatting parameters for JSONL format
        // Each entry must be on a single line, followed by a newline character
        try {
          fs.appendFileSync(outputFile, `${JSON.stringify(entry)}\n`);
        } catch (err) {
          throw new Error(`Failed to append to file ${outputFile}: ${getErrorMessage(err)}`, { cause: err });
        }

        // Use output from safe-job config if available
        const outputText = jobConfig?.output ?? `Safe-job '${configKey}' executed successfully with arguments: ${JSON.stringify(args)}`;

        return {
          content: [{ type: "text", text: JSON.stringify({ result: outputText }) }],
        };
      },
    };

    // Add input schema based on job configuration if available
    if (jobConfig?.inputs) {
      dynamicTool.inputSchema.properties = {};
      dynamicTool.inputSchema.required = [];

      Object.keys(jobConfig.inputs).forEach(inputName => {
        const inputDef = jobConfig.inputs[inputName];

        // Convert GitHub Actions choice type to JSON Schema string type
        // GitHub Actions uses "choice" type with "options" array
        // JSON Schema requires "string" type with "enum" array
        let jsonSchemaType = inputDef.type || "string";
        if (jsonSchemaType === "choice") {
          jsonSchemaType = "string";
        }

        const propSchema = {
          type: jsonSchemaType,
          description: inputDef.description || `Input parameter: ${inputName}`,
        };

        if (Array.isArray(inputDef.options)) {
          propSchema.enum = inputDef.options;
        }

        dynamicTool.inputSchema.properties[inputName] = propSchema;

        if (inputDef.required) {
          dynamicTool.inputSchema.required.push(inputName);
        }
      });
    }

    registerTool(server, dynamicTool);
  });
}

module.exports = {
  loadTools,
  attachHandlers,
  registerPredefinedTools,
  registerDynamicTools,
};
