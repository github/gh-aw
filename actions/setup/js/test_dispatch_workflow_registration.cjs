// Test script to verify dispatch_workflow tool registration
const { registerPredefinedTools } = require("./safe_outputs_tools_loader.cjs");

// Mock server
const mockServer = {
  debug: (msg) => console.log("[DEBUG]", msg),
  tools: {},
};

// Test tools with dispatch_workflow tool
const tools = [
  {
    name: "missing_tool",
    description: "Report missing tool",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "test_workflow",
    description: "Dispatch test workflow",
    _workflow_name: "test-workflow",
    inputSchema: { type: "object", properties: {} },
  },
];

// Test config
const config = {
  dispatch_workflow: {
    max: 1,
    workflows: ["test-workflow"],
    workflow_files: { "test-workflow": ".yml" },
  },
  missing_tool: {},
};

// Mock registerTool function
const registerTool = (server, tool) => {
  console.log("[REGISTER]", tool.name, tool._workflow_name ? `(_workflow_name: ${tool._workflow_name})` : "");
  server.tools[tool.name] = tool;
};

// Mock normalizeTool function
const normalizeTool = (name) => name.replace(/-/g, "_").toLowerCase();

console.log("=== Testing dispatch_workflow tool registration ===");
console.log("Tools:", tools.map(t => t.name));
console.log("Config keys:", Object.keys(config));
console.log("Config.dispatch_workflow:", config.dispatch_workflow);
console.log("");

registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

console.log("");
console.log("=== Registration Results ===");
console.log("Registered tools:", Object.keys(mockServer.tools));
console.log("");

if (mockServer.tools.test_workflow) {
  console.log("✅ SUCCESS: test_workflow was registered");
} else {
  console.log("❌ FAILURE: test_workflow was NOT registered");
  console.log("This is the bug!");
}

if (mockServer.tools.missing_tool) {
  console.log("✅ SUCCESS: missing_tool was registered");
} else {
  console.log("❌ FAILURE: missing_tool was NOT registered");
}
