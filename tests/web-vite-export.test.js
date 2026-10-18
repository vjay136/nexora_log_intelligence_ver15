import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile, stat } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
test('Vite source keeps a same-origin API proxy and CSP-compatible local assets', async () => {
  const config = await readFile(resolve(root, 'web/vite.config.js'), 'utf8');
  const html = await readFile(resolve(root, 'web/index.html'), 'utf8');
  const headers = await readFile(resolve(root, 'web/_headers'), 'utf8');
  assert.match(config, /['"]\/api\/v1['"]\s*:\s*\{\s*target:\s*['"]http:\/\/127\.0\.0\.1:12382/);
  assert.match(html, /type="module" src="\.\/app\.js"/);
  assert.match(headers, /Content-Security-Policy:.*script-src 'self';.*style-src 'self'/);
  assert.match(html, /nexora-logo\.png/);
  const css = await readFile(resolve(root, 'web/styles.css'), 'utf8');
  assert.match(html, /data-banner="login-banner\.png"/);
  assert.match(css, /url\('\.\/login-banner\.png'\)/);
  await stat(resolve(root, 'web/nexora-logo.png'));
  await stat(resolve(root, 'web/login-banner.png'));
});

test('dashboard chart shows record-count axes and keyboard accessible data points', async () => {
  const app = await readFile(resolve(root, 'web/app.js'), 'utf8');
  assert.match(app, /Records<\/text>/);
  assert.match(app, /controller time on the horizontal axis/);
  assert.match(app, /tabindex="0" role="img"/);
  assert.match(app, /logs_received/);
});

test('Vercel root switches output to Vite and preserves the serverless proxy route', async () => {
  const vercel = JSON.parse(await readFile(resolve(root, 'vercel.json'), 'utf8'));
  assert.equal(vercel.outputDirectory, 'build/vite-web');
  assert.equal(vercel.installCommand, 'npm ci --prefix web --ignore-scripts');
  assert.match(vercel.buildCommand, /npm --prefix web run build/);
  assert.ok(vercel.functions['api/v1/[...path].js']);
});
