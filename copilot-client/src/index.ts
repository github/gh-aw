/**
 * GitHub Copilot SDK Client
 * 
 * This module provides a client for running GitHub Copilot agentic sessions
 * using the @github/copilot-sdk Node.js package.
 */

import { CopilotClient, CopilotSession, type SessionEvent } from '@github/copilot-sdk';
import { readFileSync, appendFileSync, mkdirSync } from 'fs';
import { dirname } from 'path';
import debugFactory from 'debug';
import type { CopilotClientConfig, LoggedEvent } from './types.js';

const debug = debugFactory('copilot-client');

/**
 * Run a Copilot agentic session with the given configuration
 * 
 * @param config - Configuration for the Copilot client
 * @returns Promise that resolves when the session completes
 */
export async function runCopilotSession(config: CopilotClientConfig): Promise<void> {
  debug('Starting Copilot session with config:', config);

  // Ensure event log directory exists
  mkdirSync(dirname(config.eventLogFile), { recursive: true });

  // Helper function to log events
  const logEvent = (type: string, data: any, sessionId?: string): void => {
    const event: LoggedEvent = {
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
  const clientOptions: any = {
    logLevel: config.logLevel ?? 'info',
    githubToken: config.githubToken,
    useLoggedInUser: config.useLoggedInUser
  };
  
  if (config.cliUrl) {
    // Connecting to existing server - only pass cliUrl
    clientOptions.cliUrl = config.cliUrl;
  } else {
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

  let session: CopilotSession | null = null;

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
    session.on((event: SessionEvent) => {
      logEvent(`session.${event.type}`, event.data, session!.sessionId);
      
      // Also log to debug
      debug('Session event:', event.type, event.data);
    });

    // Wait for completion
    const done = new Promise<void>((resolve, reject) => {
      let lastAssistantMessage: any = null;

      session!.on('assistant.message', (event) => {
        lastAssistantMessage = event.data;
        debug('Assistant message:', event.data.content);
      });

      session!.on('session.idle', () => {
        debug('Session became idle');
        resolve();
      });

      session!.on('session.error', (event) => {
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

  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    debug('Error during session:', errorMessage);
    logEvent('session.error', { error: errorMessage }, session?.sessionId);
    throw error;
  } finally {
    // Clean up
    if (session) {
      debug('Destroying session');
      try {
        await session.destroy();
        logEvent('session.destroyed', {}, session.sessionId);
      } catch (error) {
        debug('Error destroying session:', error);
      }
    }

    debug('Stopping client');
    try {
      const errors = await client.stop();
      if (errors.length > 0) {
        debug('Errors during client stop:', errors);
        logEvent('client.stopped', { errors: errors.map(e => e.message) });
      } else {
        logEvent('client.stopped', {});
      }
    } catch (error) {
      debug('Error stopping client:', error);
    }
  }
}

/**
 * Main entry point - reads config from environment variable
 */
export async function main(): Promise<void> {
  debug('Reading configuration from GH_AW_COPILOT_CONFIG environment variable');
  
  const configJson = process.env.GH_AW_COPILOT_CONFIG;
  if (!configJson) {
    console.error('Error: GH_AW_COPILOT_CONFIG environment variable is not set');
    console.error('Please set the GH_AW_COPILOT_CONFIG environment variable with JSON configuration');
    process.exit(1);
  }

  let config: CopilotClientConfig;
  try {
    config = JSON.parse(configJson);
  } catch (error) {
    console.error('Failed to parse configuration JSON:', error);
    process.exit(1);
  }

  debug('Parsed config:', config);

  try {
    await runCopilotSession(config);
    debug('Session completed successfully');
    process.exit(0);
  } catch (error) {
    console.error('Error running Copilot session:', error);
    process.exit(1);
  }
}

// Export for testing
export type { CopilotClientConfig, LoggedEvent } from './types.js';
