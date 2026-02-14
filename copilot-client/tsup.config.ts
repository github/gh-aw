import { defineConfig } from 'tsup';

export default defineConfig({
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
  // Bundle all dependencies (no external dependencies)
  noExternal: [/.*/],
});
