# Copilot SDK Client

TypeScript client for running GitHub Copilot agentic sessions using the `@github/copilot-sdk` Node.js package.

## Features

- ES6 JavaScript with TypeScript annotations
- Async/await for Node 24
- ESM module format
- Fully bundled with all dependencies included
- JSONL event logging with timestamps
- Debug package for logging
- Configuration from stdin for testability

## Building

```bash
npm install
npm run build
```

The build uses [tsup](https://tsup.egoist.dev/) to bundle the TypeScript source into a single ESM JavaScript file targeting Node 24 (ES2024). All dependencies are bundled into the output. The compiled output will be in the `dist/` directory:

- `dist/index.js` - Main library entry point (fully bundled, ~190KB)
- `dist/index.d.ts` - TypeScript type declarations

## Usage

Create a configuration file and pipe it to the client:

```bash
echo '{
  "promptFile": "/path/to/prompt.txt",
  "eventLogFile": "/tmp/events.jsonl",
  "githubToken": "ghp_...",
  "session": {
    "model": "gpt-5"
  }
}' | node dist/cli.js
```

## Configuration

The client accepts a JSON configuration object with the following properties:

- `promptFile` (required): Path to the file containing the prompt
- `eventLogFile` (required): Path where events will be logged in JSONL format
- `githubToken` (optional): GitHub token for authentication
- `cliPath` (optional): Path to copilot CLI executable (mutually exclusive with `cliUrl`)
- `cliUrl` (optional): URL of existing CLI server (mutually exclusive with `cliPath`/`useStdio`)
- `useStdio` (optional): Use stdio transport instead of TCP (default: true, mutually exclusive with `cliUrl`)
- `session` (optional): Session configuration
  - `model` (optional): Model to use (e.g., "gpt-5", "claude-sonnet-4.5")
  - `reasoningEffort` (optional): "low" | "medium" | "high" | "xhigh"
  - `systemMessage` (optional): Custom system message
  - `mcpServers` (optional): MCP server configurations (see example below)

### MCP Server Configuration

You can configure MCP servers to provide additional tools to the Copilot session:

```json
{
  "promptFile": "/path/to/prompt.txt",
  "eventLogFile": "/tmp/events.jsonl",
  "session": {
    "model": "gpt-5",
    "mcpServers": {
      "myserver": {
        "type": "http",
        "url": "https://example.com/mcp",
        "tools": ["*"],
        "headers": {
          "Authorization": "Bearer token"
        }
      },
      "localserver": {
        "type": "local",
        "command": "/path/to/server",
        "args": ["--port", "8080"],
        "tools": ["tool1", "tool2"],
        "env": {
          "API_KEY": "secret"
        }
      }
    }
  }
}
```

**Note:** When using `cliUrl` to connect to an existing server, do not specify `cliPath`, `useStdio`, `autoStart`, or `autoRestart`. These options are mutually exclusive.

## Testing

```bash
npm test
```

Integration tests require `COPILOT_GITHUB_TOKEN` environment variable.

### Running Tests Locally

The copilot-client includes a test script that can be run locally:

```bash
# Set your GitHub token
export COPILOT_GITHUB_TOKEN=ghp_your_token_here

# Run the local test script
cd copilot-client
./test-local.sh
```

This script:
1. Builds the copilot-client
2. Creates a test prompt
3. Runs the client with the copilot CLI
4. Verifies event logging

You can also run tests through Make:

```bash
cd ..
make test-copilot-client
```

## Debugging

Enable debug logging:

```bash
DEBUG=copilot-client npm run build
DEBUG=copilot-client node dist/cli.js < config.json
```

## Event Logging

All events are logged to the specified JSONL file with timestamps:

```jsonl
{"timestamp":"2026-02-14T03:50:00.000Z","type":"prompt.loaded","data":{"file":"/path/to/prompt.txt","length":123}}
{"timestamp":"2026-02-14T03:50:01.000Z","type":"client.created","data":{"cliPath":"copilot","useStdio":true}}
{"timestamp":"2026-02-14T03:50:02.000Z","type":"session.created","sessionId":"abc123","data":{"model":"gpt-5"}}
```
