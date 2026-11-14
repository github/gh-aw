import { defineConfig } from '@playwright/test';
export default defineConfig({ testDir: './tests', use: { baseURL: 'http://localhost:4321' }, projects:[{name:'chromium',use:{browserName:'chromium'}}],
 webServer: { command: 'python3 -m http.server 4321 --bind 127.0.0.1 --directory /var/folders/s5/9zj1xyxn77qf90_87s4tctp40000gn/T/tmp.jb04PosrOb', url: 'http://localhost:4321/gh-aw/cracktro/', stdout:'ignore', stderr:'ignore' } });
