#!/usr/bin/env node
/**
 * CLI entry point for the Copilot SDK client
 * Reads configuration from GH_AW_COPILOT_CONFIG environment variable and runs a Copilot session
 */

import { main } from './index.js';

main().catch((error) => {
  console.error('Fatal error:', error);
  process.exit(1);
});
