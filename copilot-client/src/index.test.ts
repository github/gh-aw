/**
 * Tests for the Copilot SDK client
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { runCopilotSession } from './index.js';
import type { CopilotClientConfig } from './types.js';
import { readFileSync, existsSync, unlinkSync, mkdirSync, writeFileSync } from 'fs';
import { join } from 'path';

describe('CopilotClient', () => {
  const testDir = join(process.cwd(), 'test-output');
  const eventLogFile = join(testDir, 'events.jsonl');
  const promptFile = join(testDir, 'test-prompt.txt');

  beforeEach(() => {
    // Create test directory
    mkdirSync(testDir, { recursive: true });

    // Create a simple prompt file
    writeFileSync(promptFile, 'What is 2+2?', 'utf-8');
  });

  afterEach(() => {
    // Clean up test files
    if (existsSync(eventLogFile)) {
      unlinkSync(eventLogFile);
    }
    if (existsSync(promptFile)) {
      unlinkSync(promptFile);
    }
  });

  it('should validate config structure', () => {
    const config: CopilotClientConfig = {
      promptFile: promptFile,
      eventLogFile: eventLogFile,
      githubToken: 'test-token',
      session: {
        model: 'gpt-5'
      }
    };

    expect(config.promptFile).toBe(promptFile);
    expect(config.eventLogFile).toBe(eventLogFile);
    expect(config.githubToken).toBe('test-token');
    expect(config.session?.model).toBe('gpt-5');
  });

  it('should read prompt from file', () => {
    const prompt = readFileSync(promptFile, 'utf-8');
    expect(prompt).toBe('What is 2+2?');
  });

  // Note: Actual integration tests that connect to the Copilot CLI
  // should be run in the CI workflow with proper authentication
  it.skip('should run a simple session', async () => {
    const config: CopilotClientConfig = {
      promptFile: promptFile,
      eventLogFile: eventLogFile,
      githubToken: process.env.COPILOT_GITHUB_TOKEN,
      session: {
        model: 'gpt-5'
      }
    };

    await runCopilotSession(config);

    // Verify event log was created
    expect(existsSync(eventLogFile)).toBe(true);

    // Verify events were logged
    const events = readFileSync(eventLogFile, 'utf-8')
      .split('\n')
      .filter(line => line.trim())
      .map(line => JSON.parse(line));

    expect(events.length).toBeGreaterThan(0);
    expect(events[0].type).toBe('prompt.loaded');
  });
});
