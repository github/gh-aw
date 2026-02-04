---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
mcp-servers:
  # New direct field format - stdio with command
  my-stdio-server:
    command: npx
    args: ["-y", "my-server"]
    registry: "https://registry.example.com/servers/my-stdio-server"
    allowed: ["process_data", "get_info"]
    

  # New direct field format - http with url
  my-http-server:
    url: "https://api.example.com/mcp"
    headers:
      Authorization: "Bearer ${{ secrets.API_TOKEN }}"
    registry: "https://registry.example.com/servers/my-http-server"
    allowed: ["fetch_data"]
    
  # Type inference - local type alias
  local-server:
    type: local
    command: "local-tool"
    args: ["--local"]
    allowed: ["local_action"]
    
  # Type inference - no type specified, inferred from command
  inferred-stdio:
    command: "inferred-server"
    args: ["--mode", "stdio"]
    allowed: ["inferred_tool"]
    
  # Type inference - no type specified, inferred from url  
  inferred-http:
    url: "https://inferred.api.com/mcp"
    allowed: ["inferred_http_tool"]
---

# Test Workflow

Test workflow with new MCP configuration format.
