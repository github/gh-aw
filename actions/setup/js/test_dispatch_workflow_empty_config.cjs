// Test with empty dispatch_workflow config (no workflows key)
const { registerPredefinedTools } = require("./safe_outputs_tools_loader.cjs");

const mockServer = {
  debug: (msg) => console.log("[DEBUG]", msg),
  tools: {},
};

const tools = [
  {
    name: "test_workflow",
    description: "Dispatch test workflow",
    _workflow_name: "test-workflow",
    inputSchema: { type: "object", properties: {} },
  },
];

// Test with empty dispatch_workflow config
const config1 = {
  dispatch_workflow: {},  // Empty config
};

const registerTool = (server, tool) => {
  console.log("[REGISTER]", tool.name);
  server.tools[tool.name] = tool;
};

const normalizeTool = (name) => name.replace(/-/g, "_").toLowerCase();

console.log("=== Test 1: Empty dispatch_workflow config ===");
console.log("Config.dispatch_workflow:", config1.dispatch_workflow);
console.log("Truthy?", !!config1.dispatch_workflow);

registerPredefinedTools(mockServer, tools, config1, registerTool, normalizeTool);

console.log("Registered:", Object.keys(mockServer.tools));
console.log("Result:", mockServer.tools.test_workflow ? "✅ REGISTERED" : "❌ NOT REGISTERED");
console.log("");

// Test with null/undefined
mockServer.tools = {};
const config2 = {};  // No dispatch_workflow key

console.log("=== Test 2: No dispatch_workflow config ===");
console.log("Config.dispatch_workflow:", config2.dispatch_workflow);
console.log("Truthy?", !!config2.dispatch_workflow);

registerPredefinedTools(mockServer, tools, config2, registerTool, normalizeTool);

console.log("Registered:", Object.keys(mockServer.tools));
console.log("Result:", mockServer.tools.test_workflow ? "✅ REGISTERED" : "❌ NOT REGISTERED");
