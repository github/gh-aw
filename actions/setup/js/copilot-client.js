/**
 * GitHub Copilot SDK Client
 *
 * This module provides a client for running GitHub Copilot agentic sessions
 * using the @github/copilot-sdk Node.js package.
 */
import { CopilotClient } from '@github/copilot-sdk';
import { readFileSync, appendFileSync, mkdirSync } from 'fs';
import { dirname } from 'path';
import debugFactory from 'debug';
const debug = debugFactory('copilot-client');
/**
 * Run a Copilot agentic session with the given configuration
 *
 * @param config - Configuration for the Copilot client
 * @returns Promise that resolves when the session completes
 */
export async function runCopilotSession(config) {
    debug('Starting Copilot session with config:', config);
    // Ensure event log directory exists
    mkdirSync(dirname(config.eventLogFile), { recursive: true });
    // Helper function to log events
    const logEvent = (type, data, sessionId) => {
        const event = {
            timestamp: new Date().toISOString(),
            type,
            sessionId,
            data
        };
        appendFileSync(config.eventLogFile, JSON.stringify(event) + '\n', 'utf-8');
        debug('Event logged:', type);
    };
    // Load prompt from file
    debug('Loading prompt from:', config.promptFile);
    const prompt = readFileSync(config.promptFile, 'utf-8');
    logEvent('prompt.loaded', { file: config.promptFile, length: prompt.length });
    // Create Copilot client
    debug('Creating Copilot client');
    // When connecting to an existing server (cliUrl), don't pass options for starting a new process
    // These options are mutually exclusive per the Copilot SDK
    const clientOptions = {
        logLevel: config.logLevel ?? 'info',
        githubToken: config.githubToken,
        useLoggedInUser: config.useLoggedInUser
    };
    if (config.cliUrl) {
        // Connecting to existing server - only pass cliUrl
        clientOptions.cliUrl = config.cliUrl;
    }
    else {
        // Starting new process - pass process-related options
        clientOptions.cliPath = config.cliPath;
        clientOptions.cliArgs = config.cliArgs;
        clientOptions.port = config.port;
        clientOptions.useStdio = config.useStdio ?? true;
        clientOptions.autoStart = config.autoStart ?? true;
        clientOptions.autoRestart = config.autoRestart ?? true;
    }
    const client = new CopilotClient(clientOptions);
    logEvent('client.created', {
        cliPath: config.cliPath,
        useStdio: config.useStdio,
        logLevel: config.logLevel
    });
    // Start the client
    debug('Starting Copilot client');
    await client.start();
    logEvent('client.started', {});
    let session = null;
    try {
        // Create session
        debug('Creating Copilot session');
        session = await client.createSession({
            model: config.session?.model,
            reasoningEffort: config.session?.reasoningEffort,
            systemMessage: config.session?.systemMessage ? {
                mode: 'replace',
                content: config.session.systemMessage
            } : undefined,
            mcpServers: config.session?.mcpServers
        });
        logEvent('session.created', {
            sessionId: session.sessionId,
            model: config.session?.model
        }, session.sessionId);
        // Set up event handlers
        debug('Setting up event handlers');
        // Listen to all events and log them
        session.on((event) => {
            logEvent(`session.${event.type}`, event.data, session.sessionId);
            // Also log to debug
            debug('Session event:', event.type, event.data);
        });
        // Wait for completion
        const done = new Promise((resolve, reject) => {
            let lastAssistantMessage = null;
            session.on('assistant.message', (event) => {
                lastAssistantMessage = event.data;
                debug('Assistant message:', event.data.content);
            });
            session.on('session.idle', () => {
                debug('Session became idle');
                resolve();
            });
            session.on('session.error', (event) => {
                debug('Session error:', event.data);
                reject(new Error(event.data.message || 'Session error'));
            });
        });
        // Send the prompt
        debug('Sending prompt');
        await session.send({ prompt });
        logEvent('prompt.sent', { prompt }, session.sessionId);
        // Wait for completion
        debug('Waiting for session to complete');
        await done;
        debug('Session completed successfully');
        logEvent('session.completed', {}, session.sessionId);
    }
    catch (error) {
        const errorMessage = error instanceof Error ? error.message : String(error);
        debug('Error during session:', errorMessage);
        logEvent('session.error', { error: errorMessage }, session?.sessionId);
        throw error;
    }
    finally {
        // Clean up
        if (session) {
            debug('Destroying session');
            try {
                await session.destroy();
                logEvent('session.destroyed', {}, session.sessionId);
            }
            catch (error) {
                debug('Error destroying session:', error);
            }
        }
        debug('Stopping client');
        try {
            const errors = await client.stop();
            if (errors.length > 0) {
                debug('Errors during client stop:', errors);
                logEvent('client.stopped', { errors: errors.map(e => e.message) });
            }
            else {
                logEvent('client.stopped', {});
            }
        }
        catch (error) {
            debug('Error stopping client:', error);
        }
    }
}
/**
 * Main entry point - reads config from stdin and runs the session
 */
export async function main() {
    debug('Reading configuration from stdin');
    // Read config from stdin
    const stdinBuffer = [];
    await new Promise((resolve, reject) => {
        process.stdin.on('data', (chunk) => {
            stdinBuffer.push(chunk);
        });
        process.stdin.on('end', () => {
            resolve();
        });
        process.stdin.on('error', (error) => {
            reject(error);
        });
    });
    const configJson = Buffer.concat(stdinBuffer).toString('utf-8');
    debug('Received config:', configJson);
    let config;
    try {
        config = JSON.parse(configJson);
    }
    catch (error) {
        console.error('Failed to parse configuration JSON:', error);
        process.exit(1);
    }
    debug('Parsed config:', config);
    try {
        await runCopilotSession(config);
        debug('Session completed successfully');
        process.exit(0);
    }
    catch (error) {
        console.error('Error running Copilot session:', error);
        process.exit(1);
    }
}
//# sourceMappingURL=index.js.map