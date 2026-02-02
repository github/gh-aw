// @ts-check

const { getErrorMessage } = require("./error_helpers.cjs");

const fs = require("fs");

/**
 * Load tools from tools.json file
 * @param {Object} server - The MCP server instance for logging
 * @returns {Array} Array of tool definitions
 */
function loadTools(server) {
  const toolsPath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH || "/opt/gh-aw/safeoutputs/tools.json";

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
 * @returns {Array} Tools with handlers attached
 */
function attachHandlers(tools, handlers) {
  const handlerMap = {
    create_pull_request: handlers.createPullRequestHandler,
    push_to_pull_request_branch: handlers.pushToPullRequestBranchHandler,
    upload_asset: handlers.uploadAssetHandler,
    create_project: handlers.createProjectHandler,
  };

  tools.forEach(tool => {
    const handler = handlerMap[tool.name];
    if (handler) {
      tool.handler = handler;
    }

    // Check if this is a dispatch_workflow tool (dynamic tool with workflow metadata)
    if (tool._workflow_name) {
      // Create a custom handler that wraps args in inputs and adds workflow_name
      const workflowName = tool._workflow_name;
      tool.handler = args => {
        // Wrap args in inputs property to match dispatch_workflow schema
        return handlers.defaultHandler("dispatch_workflow")({
          inputs: args,
          workflow_name: workflowName,
        });
      };
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
  tools.forEach(tool => {
    // Check if this is a regular tool matching a config key
    if (Object.keys(config).find(configKey => normalizeTool(configKey) === tool.name)) {
      registerTool(server, tool);
      return;
    }

    // Check if this is a dispatch_workflow tool (has _workflow_name metadata)
    // These tools are dynamically generated with workflow-specific names
    if (tool._workflow_name) {
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

    // Skip if it's already a predefined tool
    if (server.tools[normalizedKey] || tools.find(t => t.name === normalizedKey)) {
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
        fs.appendFileSync(outputFile, `${JSON.stringify(entry)}\n`);

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
