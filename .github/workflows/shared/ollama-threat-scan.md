---
# Ollama Llama Guard 3 Threat Scanning
# Instructions for adding Ollama-based threat scanning to agentic workflows
#
# This file provides documentation and example configuration for using
# Ollama with the Llama Guard 3:1b model to scan safe outputs and patches.
#
# Note: Ollama operations can be resource-intensive. Ensure your workflow has
# adequate timeout-minutes (recommended: 20+ minutes for model download and scanning).
---

# Ollama Llama Guard 3 Threat Scanning

This guide explains how to add Ollama-based threat scanning using the Llama Guard 3:1b model to your agentic workflows.

## Quick Start

Add the following single step to your workflow frontmatter:

```yaml
---
on: push

safe-outputs:
  create-pull-request:
  threat-detection:
    steps:
      - name: Ollama Llama Guard 3 Threat Scan
        id: ollama-scan
        uses: actions/github-script@v9
        with:
          script: |
            const fs = require('fs');
            const path = require('path');
            
            // ===== INSTALL OLLAMA =====
            core.info('🚀 Starting Ollama installation...');
            try {
              core.info('📥 Downloading Ollama installer...');
              await exec.exec('curl', ['-fsSL', 'https://ollama.com/install.sh', '-o', '/tmp/gh-aw/agent/install-ollama.sh']);
              
              core.info('📦 Installing Ollama...');
              await exec.exec('sh', ['/tmp/gh-aw/agent/install-ollama.sh']);
              
              core.info('✅ Verifying Ollama installation...');
              const versionOutput = await exec.getExecOutput('ollama', ['--version']);
              core.info(`Ollama version: ${versionOutput.stdout.trim()}`);
              core.info('✅ Ollama installed successfully');
            } catch (error) {
              core.setFailed(`Failed to install Ollama: ${error instanceof Error ? error.message : String(error)}`);
              throw error;
            }
            
            // ===== START OLLAMA SERVICE =====
            core.info('🚀 Starting Ollama service...');
            const logDir = '/tmp/gh-aw/agent/ollama-logs';
            if (!fs.existsSync(logDir)) {
              fs.mkdirSync(logDir, { recursive: true });
            }
            
            // Start Ollama service in background
            const ollamaServeLog = fs.openSync(`${logDir}/ollama-serve.log`, 'w');
            const ollamaServeErrLog = fs.openSync(`${logDir}/ollama-serve-error.log`, 'w');
            exec.exec('ollama', ['serve'], {
              detached: true,
              silent: true,
              outStream: fs.createWriteStream(`${logDir}/ollama-serve.log`),
              errStream: fs.createWriteStream(`${logDir}/ollama-serve-error.log`)
            }).then(() => {
              core.info('Ollama service started in background');
            }).catch(err => {
              core.warning(`Ollama service background start: ${err.message}`);
            });
            
            // Wait for service to be ready
            core.info('⏳ Waiting for Ollama service to be ready...');
            let retries = 30;
            while (retries > 0) {
              try {
                await exec.exec('curl', ['-f', 'http://localhost:11434/api/version'], {
                  silent: true
                });
                core.info('✅ Ollama service is ready');
                break;
              } catch (e) {
                retries--;
                if (retries === 0) {
                  throw new Error('Ollama service did not become ready in time');
                }
                await new Promise(resolve => setTimeout(resolve, 1000));
              }
            }
            
            // ===== DOWNLOAD LLAMA GUARD 3 MODEL =====
            core.info('📥 Checking for Llama Guard 3:1b model...');
            try {
              // Check if model is already available
              const modelsOutput = await exec.getExecOutput('ollama', ['list']);
              const modelExists = modelsOutput.stdout.includes('llama-guard3');
              
              if (modelExists) {
                core.info('✅ Llama Guard 3 model already available');
              } else {
                core.info('📥 Downloading Llama Guard 3:1b model...');
                core.info('This may take several minutes...');
                const startTime = Date.now();
                await exec.exec('ollama', ['pull', 'llama-guard3:1b']);
                
                const elapsed = ((Date.now() - startTime) / 1000).toFixed(1);
                core.info(`✅ Model downloaded successfully in ${elapsed}s`);
                
                // Verify model is now available
                const verifyOutput = await exec.getExecOutput('ollama', ['list']);
                if (!verifyOutput.stdout.includes('llama-guard3')) {
                  throw new Error('Llama Guard 3 model not found after download');
                }
              }
              core.info('✅ Llama Guard 3 model ready');
            } catch (error) {
              core.setFailed(`Failed to prepare model: ${error instanceof Error ? error.message : String(error)}`);
              throw error;
            }
            
            // ===== SCAN SAFE OUTPUTS =====
            core.info('🔍 Starting Llama Guard 3 threat scan...');
            const scanDir = '/tmp/gh-aw/agent/threat-detection';
            
            let threatsDetected = false;
            const results = [];
            
            // ===== SCAN AGENT OUTPUT ITEMS =====
            const agentOutputPath = path.join(scanDir, 'agent_output.json');
            core.info(`\n📄 Scanning Agent Output Items: ${agentOutputPath}`);
            
            if (fs.existsSync(agentOutputPath)) {
              try {
                const agentOutputContent = fs.readFileSync(agentOutputPath, 'utf8');
                const agentOutput = JSON.parse(agentOutputContent);
                
                if (agentOutput.items && Array.isArray(agentOutput.items)) {
                  core.info(`Found ${agentOutput.items.length} safe output items to scan`);
                  
                  for (let i = 0; i < agentOutput.items.length; i++) {
                    const item = agentOutput.items[i];
                    const itemName = `Agent Output Item #${i + 1} (${item.type || 'unknown'})`;
                    core.info(`\n📋 Scanning ${itemName}...`);
                    
                    try {
                      // Convert item to string for analysis
                      const itemContent = JSON.stringify(item, null, 2);
                      const itemSize = (itemContent.length / 1024).toFixed(2);
                      core.info(`Item size: ${itemSize} KB`);
                      
                      // Truncate very large items
                      const maxChars = 8000;
                      const content = itemContent.length > maxChars 
                        ? itemContent.substring(0, maxChars) + '\n\n[Content truncated for scanning]'
                        : itemContent;
                      
                      core.info('🤖 Running Llama Guard 3 analysis...');
                      const scanStart = Date.now();
                      
                      let output = '';
                      try {
                        const response = await exec.getExecOutput('curl', [
                          '-X', 'POST',
                          'http://localhost:11434/api/chat',
                          '-H', 'Content-Type: application/json',
                          '-d', JSON.stringify({
                            model: 'llama-guard3:1b',
                            messages: [{ role: 'user', content: content }],
                            stream: false
                          })
                        ]);
                        const apiResult = JSON.parse(response.stdout);
                        output = apiResult.message?.content || '';
                      } catch (error) {
                        core.warning(`Llama Guard 3 execution error: ${error instanceof Error ? error.message : String(error)}`);
                        output = error.stdout || '';
                      }
                      
                      const scanElapsed = ((Date.now() - scanStart) / 1000).toFixed(1);
                      core.info(`Analysis completed in ${scanElapsed}s`);
                      
                      core.info(`\n📊 Llama Guard 3 Response:\n${output}`);
                      
                      // Result must be "safe" or contain "S8" (Intellectual Property, which we allow)
                      const outputLower = output.toLowerCase();
                      const isSafe = outputLower.trim() === 'safe' || outputLower.includes('s8');
                      
                      results.push({
                        file: itemName,
                        path: agentOutputPath,
                        itemIndex: i,
                        itemType: item.type,
                        safe: isSafe,
                        response: output.trim()
                      });
                      
                      if (!isSafe) {
                        threatsDetected = true;
                        core.warning(`⚠️  Potential threat detected in ${itemName}`);
                      }
                    } catch (error) {
                      core.error(`Error scanning ${itemName}: ${error instanceof Error ? error.message : String(error)}`);
                      results.push({
                        file: itemName,
                        path: agentOutputPath,
                        itemIndex: i,
                        safe: false,
                        error: error instanceof Error ? error.message : String(error)
                      });
                      threatsDetected = true;
                    }
                  }
                } else {
                  core.info('No items array found in agent_output.json');
                }
              } catch (error) {
                core.error(`Error reading agent_output.json: ${error instanceof Error ? error.message : String(error)}`);
                results.push({
                  file: 'Agent Output',
                  path: agentOutputPath,
                  safe: false,
                  error: error instanceof Error ? error.message : String(error)
                });
                threatsDetected = true;
              }
            } else {
              core.info(`⚠️  Agent output file not found, skipping: ${agentOutputPath}`);
            }
            
            // ===== SCAN CODE PATCH =====
            const patchPath = path.join(scanDir, 'aw.patch');
            core.info(`\n📄 Scanning Code Patch: ${patchPath}`);
            
            if (fs.existsSync(patchPath)) {
              try {
                const patchContent = fs.readFileSync(patchPath, 'utf8');
                const patchSize = (patchContent.length / 1024).toFixed(2);
                core.info(`Patch size: ${patchSize} KB`);
                
                // Truncate very large patches
                const maxChars = 8000;
                const content = patchContent.length > maxChars 
                  ? patchContent.substring(0, maxChars) + '\n\n[Content truncated for scanning]'
                  : patchContent;
                
                core.info('🤖 Running Llama Guard 3 analysis...');
                const scanStart = Date.now();
                
                let output = '';
                try {
                  const response = await exec.getExecOutput('curl', [
                    '-X', 'POST',
                    'http://localhost:11434/api/chat',
                    '-H', 'Content-Type: application/json',
                    '-d', JSON.stringify({
                      model: 'llama-guard3:1b',
                      messages: [{ role: 'user', content: content }],
                      stream: false
                    })
                  ]);
                  const apiResult = JSON.parse(response.stdout);
                  output = apiResult.message?.content || '';
                } catch (error) {
                  core.warning(`Llama Guard 3 execution error: ${error instanceof Error ? error.message : String(error)}`);
                  output = error.stdout || '';
                }
                
                const scanElapsed = ((Date.now() - scanStart) / 1000).toFixed(1);
                core.info(`Analysis completed in ${scanElapsed}s`);
                
                core.info(`\n📊 Llama Guard 3 Response:\n${output}`);
                
                // Result must be "safe" or contain "S8" (Intellectual Property, which we allow)
                const outputLower = output.toLowerCase();
                const isSafe = outputLower.trim() === 'safe' || outputLower.includes('s8');
                
                results.push({
                  file: 'Code Patch',
                  path: patchPath,
                  safe: isSafe,
                  response: output.trim()
                });
                
                if (!isSafe) {
                  threatsDetected = true;
                  core.warning(`⚠️  Potential threat detected in Code Patch`);
                }
              } catch (error) {
                core.error(`Error scanning Code Patch: ${error instanceof Error ? error.message : String(error)}`);
                results.push({
                  file: 'Code Patch',
                  path: patchPath,
                  safe: false,
                  error: error instanceof Error ? error.message : String(error)
                });
                threatsDetected = true;
              }
            } else {
              core.info(`⚠️  Patch file not found, skipping: ${patchPath}`);
            }
            
            // Write results
            const resultsPath = '/tmp/gh-aw/agent/threat-detection/ollama-scan-results.json';
            fs.writeFileSync(resultsPath, JSON.stringify(results, null, 2));
            core.info(`\n📝 Results written to: ${resultsPath}`);
            
            // Summary
            core.info('\n' + '='.repeat(60));
            core.info('🔍 Llama Guard 3 Scan Summary');
            core.info('='.repeat(60));
            for (const result of results) {
              const status = result.safe ? '✅ SAFE' : '❌ UNSAFE';
              core.info(`${status} - ${result.file}`);
              if (!result.safe && result.response) {
                core.info(`  Reason: ${result.response.substring(0, 200)}`);
              }
            }
            core.info('='.repeat(60));
            
            if (threatsDetected) {
              core.setFailed('❌ Llama Guard 3 detected potential security threats in the safe outputs or patches');
            } else {
              core.info('✅ All scanned content appears safe');
            }
      
      
      - name: Upload scan results
        if: always()
        uses: actions/upload-artifact@v7.0.1
        with:
          name: ollama-scan-results
          path: |
            /tmp/gh-aw/agent/threat-detection/ollama-scan-results.json
            /tmp/gh-aw/agent/ollama-logs/
          if-no-files-found: ignore
---

# Ollama Llama Guard 3 Threat Scanning

This shared workflow adds Ollama-based threat scanning using the Llama Guard 3:1b model to analyze safe outputs and code patches for security threats.

## Features

- **Automatic Ollama Installation**: Installs Ollama on the GitHub Actions runner
- **Model Pre-download**: Downloads the llama-guard3:1b model before scanning
- **Safe Output Scanning**: Scans agent output files and code patches
- **Automatic Failure**: Fails the workflow if threats are detected
- **Detailed Logging**: Provides comprehensive logging of all operations

## How It Works

1. **Installation**: Downloads and installs Ollama
2. **Service Start**: Starts the Ollama service in the background
3. **Model Download**: Pulls the Llama Guard 3 model (may take several minutes)
4. **Scanning**: Analyzes agent outputs and patches for threats via HTTP API
5. **Results**: Parses Llama Guard 3 responses and fails if unsafe content is detected

## Llama Guard 3 Model

Llama Guard 3 is a safeguard model designed to detect potentially harmful content including:
- Malicious code patterns
- Security vulnerabilities
- Harmful instructions
- Data exfiltration attempts
- Backdoors and exploits

## Performance Notes

- **Model Download**: First run may take 5-10 minutes to download the model
- **Scanning**: Each file scan typically takes 10-30 seconds
- **Resource Usage**: Requires adequate CPU and memory on the runner
- **Recommended Timeout**: Set workflow `timeout-minutes` to at least 20 minutes
- **Content Truncation**: Files larger than 8KB are automatically truncated for analysis

## Usage Example

Copy the complete YAML configuration from the top of this file into your workflow's `safe-outputs.threat-detection.steps` section.

Example:

```yaml
---
on:
  pull_request:
    types: [opened, synchronize]

permissions:
  contents: read
  actions: read

engine: copilot

safe-outputs:
  create-pull-request:
  threat-detection:
    steps:
      # Copy all steps from the Quick Start section above

timeout-minutes: 20
---

# Your workflow prompt here
```

## Output Artifacts

The scan results are uploaded as artifacts including:
- `ollama-scan-results.json`: Detailed JSON results for each scanned file with safe/unsafe status
- Ollama service logs (`/tmp/gh-aw/agent/ollama-logs/`) for debugging

## Integration with Existing Threat Detection

This Ollama scanning complements the existing AI-based threat detection:
- Existing: Uses Claude/Copilot to analyze context and intent
- Ollama: Uses specialized Llama Guard 3 model for pattern-based threat detection
- Together they provide defense-in-depth security analysis

## Troubleshooting

**Ollama installation fails:**
- Check runner has internet access to ollama.com
- Verify curl is available on the runner
- Review installation logs in step output

**Model download times out:**
- Increase timeout in the download step (default: 600 seconds)
- Check network bandwidth  
- Model is ~3-4GB and may take 5-10 minutes on first download

**Service not ready:**
- Increase wait loop iterations (default: 30 seconds)
- Check `/tmp/gh-aw/agent/ollama-logs/ollama-serve-error.log` for startup errors
- Verify port 11434 is not already in use

**Scan produces false positives:**
- Review Llama Guard 3 response in step logs
- Adjust threat keywords in the scanning logic
- Consider customizing the prompt sent to Llama Guard 3

**Out of memory errors:**
- Reduce maxChars truncation limit (default: 8000)
- Scan fewer files or smaller chunks
- Use a runner with more memory
