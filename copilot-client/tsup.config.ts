import { defineConfig } from 'tsup';

export default defineConfig([
  // Library entry (no shebang)
  {
    entry: {
      index: 'src/index.ts',
    },
    format: ['esm'],
    target: 'es2024',
    platform: 'node',
    outDir: 'dist',
    dts: true,
    sourcemap: true,
    clean: true,
    bundle: true,
    splitting: false,
    treeshake: true,
    minify: false,
  },
  // CLI entry (with shebang)
  {
    entry: {
      cli: 'src/cli.ts',
    },
    format: ['esm'],
    target: 'es2024',
    platform: 'node',
    outDir: 'dist',
    dts: true,
    sourcemap: true,
    clean: false, // Don't clean as the first config already did
    bundle: true,
    splitting: false,
    treeshake: true,
    minify: false,
    esbuildOptions(options) {
      options.banner = {
        js: '#!/usr/bin/env node',
      };
    },
  },
]);
