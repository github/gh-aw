# Copilot Client Action

A GitHub composite action for executing the copilot-client with comprehensive logging and verification.

## Features

- ✅ Prerequisite verification (copilot-client.js, config file, Node.js)
- 📊 Detailed logging of execution with timestamps
- 🔍 Event log validation (JSONL format checking)
- 📤 Automatic artifact upload of logs
- ⏱️ Execution duration tracking
- 🐛 Optional debug mode for troubleshooting

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `config-file` | Path to the copilot client configuration JSON file | Yes | - |
| `event-log-file` | Path where event log should be written (JSONL format) | No | `/tmp/copilot-events.jsonl` |
| `debug-mode` | Enable debug logging (`DEBUG=copilot-client`) | No | `false` |

## Outputs

| Output | Description |
|--------|-------------|
| `exit-code` | Exit code from copilot-client execution |
| `events-count` | Number of events logged to the event log file |

## Prerequisites

1. **copilot-client.js**: Must be present at `/opt/gh-aw/copilot/copilot-client.js`
   - This is automatically copied by the `actions/setup` action
2. **Node.js**: Must be installed and available in PATH
3. **Configuration file**: Must exist and contain valid JSON configuration

## Usage

### Basic Usage

```yaml
- name: Setup Actions
  uses: ./actions/setup

- name: Run Copilot Client
  uses: ./.github/actions/copilot-client
  with:
    config-file: /tmp/copilot-config.json
```

### With Debug Mode

```yaml
- name: Run Copilot Client
  uses: ./.github/actions/copilot-client
  with:
    config-file: /tmp/copilot-config.json
    event-log-file: /tmp/my-events.jsonl
    debug-mode: 'true'
```

### Using Outputs

```yaml
- name: Run Copilot Client
  id: copilot
  uses: ./.github/actions/copilot-client
  with:
    config-file: /tmp/copilot-config.json

- name: Check Results
  run: |
    echo "Exit code: ${{ steps.copilot.outputs.exit-code }}"
    echo "Events logged: ${{ steps.copilot.outputs.events-count }}"
```

## Configuration File Format

The configuration file should be a JSON file with the following structure (see `copilot-client/src/types.ts` for complete schema):

```json
{
  "promptFile": "/tmp/prompt.txt",
  "eventLogFile": "/tmp/events.jsonl",
  "cliUrl": "localhost:8080",
  "session": {
    "model": "gpt-5"
  }
}
```

## Logging

This action produces extensive logging:

1. **copilot-client.log**: Complete execution log with timestamps
   - Prerequisites verification
   - Environment setup
   - Copilot client execution output
   - Event log verification
   - Execution summary

2. **Event log file**: JSONL file containing copilot events
   - Each line is a valid JSON object
   - Automatically validated by the action

Both logs are automatically uploaded as artifacts with the name `copilot-client-logs-{run_id}`.

## Example Log Output

```
=== Copilot Client Prerequisites Check ===
Timestamp: 2024-02-14T06:10:40+00:00
Working directory: /home/runner/work/gh-aw/gh-aw

Checking for copilot-client.js...
✓ Found copilot-client.js (189234 bytes)

Checking for configuration file...
✓ Found config file at /tmp/config.json (234 bytes)
Configuration contents:
{"promptFile":"/tmp/prompt.txt",...}

Checking Node.js environment...
✓ Node.js version: v24.0.0

=== Running Copilot Client ===
Timestamp: 2024-02-14T06:10:41+00:00
Configuration: /tmp/config.json
Event log: /tmp/copilot-events.jsonl
Debug mode: false

Starting execution at 2024-02-14T06:10:41+00:00...
[copilot output here]
Execution completed at 2024-02-14T06:10:45+00:00
Duration: 4 seconds
Exit code: 0

✓ Copilot client completed successfully

=== Verifying Copilot Client Output ===
✓ Event log exists: /tmp/copilot-events.jsonl
  File size: 1234 bytes
  Events logged: 12

Validating JSONL format...
✓ All 12 events are valid JSON

=== Copilot Client Execution Summary ===
Completed at: 2024-02-14T06:10:46+00:00
Exit code: 0
Events logged: 12
Full logs available in copilot-client.log

✓ Copilot client execution successful
```

## Error Handling

The action handles errors gracefully:

- Missing prerequisites → Fails with clear error message
- Invalid config file → Fails with error details
- Copilot execution failure → Logs the error and returns non-zero exit code
- Missing event log → Warns but continues (sets `events-count` to 0)
- Invalid JSONL → Warns about invalid lines but continues

## Artifacts

After execution (successful or failed), the following artifacts are uploaded:

- `copilot-client-logs-{run_id}`:
  - `copilot-client.log` - Complete execution log
  - Event log file (if created)

These artifacts are retained for 90 days (default GitHub Actions artifact retention).
