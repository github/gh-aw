---
mcp-servers:
  fabric-rti:
    command: "uvx"
    args:
      - "microsoft-fabric-rti-mcp==0.6.2"
    env:
      AZURE_TENANT_ID: "${{ secrets.AZURE_TENANT_ID }}"
      AZURE_CLIENT_ID: "${{ secrets.AZURE_CLIENT_ID }}"
      AZURE_CLIENT_SECRET: "${{ secrets.AZURE_CLIENT_SECRET }}"
    allowed:
      - "kusto_known_services"
      - "kusto_query"
      - "kusto_list_databases"
      - "kusto_list_tables"
      - "kusto_get_entities_schema"
      - "kusto_get_table_schema"
      - "kusto_get_function_schema"
      - "kusto_sample_table_data"
      - "kusto_sample_function_data"
      - "kusto_get_shots"
      - "list_eventstreams"
      - "get_eventstream"
      - "get_eventstream_definition"
---

<!--
## Microsoft Fabric Real-Time Intelligence (RTI) MCP Server

> **Security note**: this server runs through `uvx`, which is auto-containerized with
> `python:alpine`. That image currently ships CPython 3.14.7, which has High severity
> CVEs with no fixed release available (see #51711). This configuration is therefore not
> imported by any workflow in this repository until a patched Python image is published.

This shared configuration provides the Microsoft Fabric Real-Time Intelligence (RTI) MCP Server with **read-only access** for AI-assisted data querying and analysis.

The Fabric RTI MCP Server enables AI agents to interact with Microsoft Fabric RTI services by providing tools through the MCP interface, allowing for seamless data querying and analysis capabilities.

### 🔍 Supported Services

**Eventhouse (Kusto)**: Execute KQL queries against Microsoft Fabric RTI [Eventhouse](https://aka.ms/eventhouse) and [Azure Data Explorer (ADX)](https://aka.ms/adx).

**Eventstreams**: Manage Microsoft Fabric [Eventstreams](https://learn.microsoft.com/fabric/real-time-intelligence/eventstream/eventstream-introduction) for real-time data processing:
- List Eventstreams in workspaces
- Get Eventstream details and definitions

### Available Tools

This configuration provides read-only access to the following tools:

#### Eventhouse (Kusto) - 10 Read-Only Tools:
- **`kusto_known_services`** - List all available Kusto services configured in the MCP
- **`kusto_query`** - Execute KQL queries on the specified database
- **`kusto_list_databases`** - List all databases in the Kusto cluster
- **`kusto_list_tables`** - List all tables in a specified database
- **`kusto_get_entities_schema`** - Get schema information for all entities (tables, materialized views, functions) in a database
- **`kusto_get_table_schema`** - Get detailed schema information for a specific table
- **`kusto_get_function_schema`** - Get schema information for a specific function, including parameters and output schema
- **`kusto_sample_table_data`** - Retrieve random sample records from a specified table
- **`kusto_sample_function_data`** - Retrieve random sample records from the result of a function call
- **`kusto_get_shots`** - Retrieve semantically similar query examples from a shots table using AI embeddings

#### Eventstreams - 3 Read-Only Tools:
- **`list_eventstreams`** - List all Eventstreams in your Fabric workspace
- **`get_eventstream`** - Get detailed information about a specific Eventstream
- **`get_eventstream_definition`** - Retrieve complete JSON definition of an Eventstream

**Excluded Tools (Destructive Operations):**
- `kusto_command` - Execute Kusto management commands
- `kusto_ingest_inline_into_table` - Ingest inline CSV data

### 🔑 Authentication

The MCP Server uses Azure Identity via [`DefaultAzureCredential`](https://learn.microsoft.com/azure/developer/python/sdk/authentication/credential-chains?tabs=dac) for authentication. When using environment variables (recommended for CI/CD), the server authenticates using the provided Azure Service Principal credentials.

**Required Secrets:**
- `AZURE_TENANT_ID`: Your Azure tenant ID
- `AZURE_CLIENT_ID`: Your Azure client (application) ID
- `AZURE_CLIENT_SECRET`: Your Azure client secret

**Authentication Requirements:**
- The Azure Service Principal must have access to the Microsoft Fabric workspace and resources
- The identity should have appropriate permissions for Eventhouse and Eventstreams (Reader role recommended)

### Setup

1. **Create an Azure Service Principal** with read-only permissions for Microsoft Fabric:
   ```bash
   az ad sp create-for-rbac --name "gh-aw-fabric-rti-readonly" --role Reader --scopes /subscriptions/{subscription-id}
   ```

2. **Add the following secrets to your GitHub repository**:
   - `AZURE_TENANT_ID`: Tenant ID from the service principal output
   - `AZURE_CLIENT_ID`: App ID from the service principal output
   - `AZURE_CLIENT_SECRET`: Password from the service principal output

3. **Include this configuration in your workflow**:
   ```yaml
   imports:
     - shared/mcp/fabric-rti.md
   ```

### Example Usage

```aw
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
imports:
  - shared/mcp/fabric-rti.md
---

# Fabric RTI Data Analyzer

Analyze data mentioned in issue #${{ github.event.issue.number }} using Microsoft Fabric RTI.

Review the issue content and identify any data analysis requests related to Eventhouse or Eventstreams.

Use the Fabric RTI MCP tools to:
- List available databases and tables
- Execute KQL queries for data analysis
- Retrieve Eventstream information
- Provide insights based on the data
```

### Example Prompts

**Eventhouse Analytics:**
- "Get databases in my Eventhouse"
- "Sample 10 rows from table 'StormEvents' in Eventhouse"
- "What can you tell me about StormEvents data?"
- "Analyze the StormEvents to come up with trend analysis across past 10 years of data"
- "Analyze the commands in 'CommandExecution' table and categorize them as low/medium/high risks"

**Eventstream Management:**
- "List all Eventstreams in my workspace"
- "Show me the details of my IoT data Eventstream"

### Security

- **Read-only mode**: Only read operations are permitted - destructive operations are excluded
- **Credential Security**: Your credentials are always handled securely through the official [Azure Identity SDK](https://github.com/Azure/azure-sdk-for-net/blob/main/sdk/identity/Azure.Identity/README.md) - credentials are never stored or managed directly
- **Least Privilege**: Use a Service Principal with minimal Reader role permissions
- **Excluded Operations**: `kusto_command` and `kusto_ingest_inline_into_table` are excluded to prevent write operations

### More Information

- **GitHub Repository**: https://github.com/microsoft/fabric-rti-mcp
- **PyPI Package**: https://pypi.org/project/microsoft-fabric-rti-mcp/
- **Microsoft Fabric RTI Documentation**: https://aka.ms/fabricrti
- **License**: MIT
- **Status**: Public Preview

-->
