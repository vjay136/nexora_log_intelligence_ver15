import { cp, mkdir, rm, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const target = resolve(root, 'build/web-only');
await rm(target, { recursive: true, force: true });
await mkdir(target, { recursive: true });
await mkdir(resolve(target, 'web'), { recursive: true });
for (const name of ['index.html', 'app.js', 'styles.css', 'nexora-logo.png', 'login-banner.png', 'nli-mark.svg', '_headers', 'vite.config.js', 'package.json', 'package-lock.json', 'README.txt']) {
  await cp(resolve(root, 'web', name), resolve(target, 'web', name));
}
for (const name of ['api', 'functions']) await cp(resolve(root, name), resolve(target, name), { recursive: true });
await mkdir(resolve(target, 'scripts'), { recursive: true });
await cp(resolve(root, 'scripts/build-webapp.mjs'), resolve(target, 'scripts/build-webapp.mjs'));
await cp(resolve(root, 'vercel.json'), resolve(target, 'vercel.json'));
await cp(resolve(root, 'package.json'), resolve(target, 'package.json'));
await cp(resolve(root, 'package-lock.json'), resolve(target, 'package-lock.json'));
const manifest = { name: 'nexora-log-intelligence-v15-web-deploy', version: '15.0.0', private: true, type: 'module', engines: { node: '24.x' }, scripts: { build: 'npm --prefix web run build' } };
await writeFile(resolve(target, 'package.json'), `${JSON.stringify(manifest, null, 2)}\n`);
await rm(resolve(target, 'package-lock.json'), { force: true });
await writeFile(resolve(target, '.gitignore'), `.vercel/\nbuild/\nnode_modules/\n.env\n.env.*\n!.env.example\n`);
await writeFile(resolve(target, '.env.example'), `# Server-side Vercel variable. Use the stable public HTTPS hostname created by your Cloudflare Tunnel.\n# Do not add a trailing slash or /api/v1 path. Do not put controller, Supabase, or AI secrets in this repository.\nNLI_CONTROLLER_ORIGIN=https://nli-controller.example.com\n`);
await writeFile(resolve(target, 'README.md'), `# Nexora Log Intelligence V15 Web Deployment\n\nThis repository contains only the Vite WebApp and its same-origin Vercel API proxy. It excludes the controller/client source, RPM build inputs, Supabase schema, browser tests, and local operator scripts.\n\n## Deploy on Vercel\n\n1. Import this repository and keep **Root Directory** set to \`./\`.\n2. Select the **Vite** preset. The committed \`vercel.json\` supplies the install, build, output, function, and security-header settings.\n3. Add one server-side environment variable in Production and Preview:\n   \`NLI_CONTROLLER_ORIGIN=https://<your-stable-public-controller-hostname>\`\n4. Use a public HTTPS DNS hostname provided by a named Cloudflare Tunnel or another trusted reverse proxy. Do not use an EC2 private DNS name, RFC1918 address, localhost, a port, a path, or a trailing slash.\n5. Deploy. The browser calls same-origin \`/api/v1/*\`; the included serverless function forwards those requests to the controller.\n\nNever add the enrollment token, controller HMAC/encryption keys, Supabase secret keys, AI keys, or \`.env*\` files to this repository or Vercel browser variables.\n\n## Verify locally\n\nRun \`npm ci --prefix web --ignore-scripts\` and \`npm run build\`. The output is generated at \`build/vite-web\`.\n`);
console.log(`Created clean web-only deployment source at ${target}`);
