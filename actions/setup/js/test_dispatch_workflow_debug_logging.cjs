// Test to verify debug logging for dispatch_workflow registration
const { registerPredefinedTools } = require("./safe_outputs_tools_loader.cjs");

// Mock server with captured debug messages
const debugMessages = [];
const mockServer = {
  debug: (msg) => {
    console.log("[DEBUG]", msg);
    debugMessages.push(msg);
  },
  tools: {},
};

// Test tools
const tools = [
  {
    name: "test_workflow",
    description: "Dispatch test workflow",
    _workflow_name: "test-workflow",
    inputSchema: { type: "object", properties: {} },
  },
];

// Mock functions
const registerTool = (server, tool) => {
  console.log("[REGISTER]", tool.name);
  server.tools[tool.name] = tool;
};
const normalizeTool = (name) => name.replace(/-/g, "_").toLowerCase();

console.log("=== Test Case 1: dispatch_workflow config EXISTS ===");
debugMessages.length = 0;
mockServer.tools = {};
const config1 = {
  dispatch_workflow: { max: 1, workflows: ["test-workflow"] },
};

registerPredefinedTools(mockServer, tools, config1, registerTool, normalizeTool);

console.log("Registered tools:", Object.keys(mockServer.tools));
console.log("Debug messages:");
debugMessages.forEach(msg => console.log("  ", msg));
console.log("");

console.log("=== Test Case 2: dispatch_workflow config MISSING ===");
debugMessages.length = 0;
mockServer.tools = {};
const config2 = {
  missing_tool: {},
  noop: {},
};

registerPredefinedTools(mockServer, tools, config2, registerTool, normalizeTool);

console.log("Registered tools:", Object.keys(mockServer.tools));
console.log("Debug messages:");
debugMessages.forEach(msg => console.log("  ", msg));
console.log("");

if (mockServer.tools.test_workflow) {
  console.log("❌ UNEXPECTED: tool was registered even though dispatch_workflow config is missing");
} else {
  console.log("✅ CORRECT: tool was NOT registered when dispatch_workflow config is missing");
}

// Check that warning was logged
const hasWarning = debugMessages.some(msg => msg.includes("WARNING"));
if (hasWarning) {
  console.log("✅ CORRECT: Warning message was logged");
} else {
  console.log("❌ MISSING: Warning message was not logged");
}
