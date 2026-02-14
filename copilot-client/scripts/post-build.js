#!/usr/bin/env node
/**
 * Post-build script to copy compiled files to actions/setup/js
 */
import { readFileSync, writeFileSync, mkdirSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const distDir = join(__dirname, '..', 'dist');
const targetDir = join(__dirname, '..', '..', 'actions', 'setup', 'js');

console.log('Copying compiled files to actions/setup/js...');

// Read the compiled index.js
const indexPath = join(distDir, 'index.js');
const indexContent = readFileSync(indexPath, 'utf-8');

// Convert to CommonJS format for actions/setup/js
const cjsContent = `/**
 * GitHub Copilot SDK Client
 * Auto-generated from copilot-client TypeScript source
 * DO NOT EDIT MANUALLY - changes will be overwritten
 */
${indexContent}`;

// Write to actions/setup/js/copilot_client.cjs
const targetPath = join(targetDir, 'copilot_client.cjs');
writeFileSync(targetPath, cjsContent, 'utf-8');

console.log(`✓ Copied to ${targetPath}`);
