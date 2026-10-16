import { isIP } from 'node:net';

export const config = {
  api: {
    bodyParser: false,
    responseLimit: false,
  },
  maxDuration: 60,
};

const requestHeaders = new Set([
  'accept',
  'accept-language',
  'authorization',
  'content-type',
  'cookie',
  'if-match',
  'if-none-match',
  'user-agent',
  'x-artifact-sha256',
  'x-artifact-signature',
  'x-csrf-token',
]);
const responseHeaders = new Set([
  'allow',
  'cache-control',
  'content-disposition',
  'content-type',
  'etag',
  'last-modified',
  'x-nli-controller',
]);

const MAX_REQUEST_BYTES = 4_400_000;
const UPSTREAM_TIMEOUT_MS = 50_000;
const SSE_TIMEOUT_MS = 55_000;

export function isPrivateHost(hostname) {
  const host = hostname.toLowerCase().replace(/^\[|\]$/g, '').replace(/\.$/, '');
  if (host === 'localhost' || ['.localhost', '.local', '.internal', '.lan', '.corp', '.intranet', '.home.arpa'].some(suffix => host.endsWith(suffix))) return true;
  if (isIP(host) === 4) {
    const [a, b, c] = host.split('.').map(Number);
    return a === 0 || a === 10 || a === 127 || a >= 224 ||
      (a === 100 && b >= 64 && b <= 127) || (a === 169 && b === 254) ||
      (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) ||
      (a === 192 && b === 0 && c <= 2) || (a === 198 && (b === 18 || b === 19)) ||
      (a === 198 && b === 51 && c === 100) || (a === 203 && b === 0 && c === 113);
  }
  // Deployments use public DNS or public IPv4. Reject IPv6 literals to avoid
  // private/mapped/transition-address ambiguity in a fixed-origin proxy.
  return isIP(host) === 6 || !host.includes('.');
}

function controllerOrigin() {
  const raw = String(process.env.NLI_CONTROLLER_ORIGIN || '').trim();
  let value;
  try {
    value = new URL(raw);
  } catch {
    throw new Error('NLI_CONTROLLER_ORIGIN is not configured as an absolute URL');
  }
  const host = value.hostname.toLowerCase();
  if (value.protocol !== 'https:' || value.username || value.password || value.search || value.hash || value.pathname !== '/' || isPrivateHost(host) || isIP(host) !== 0) {
    throw new Error('NLI_CONTROLLER_ORIGIN must be a public HTTPS hostname without credentials, path, query, or fragment');
  }
  value.pathname = '/';
  return value;
}

function copySetCookies(upstream, response) {
  const values = typeof upstream.headers.getSetCookie === 'function'
    ? upstream.headers.getSetCookie()
    : [upstream.headers.get('set-cookie')].filter(Boolean);
  // Scope controller cookies to this application origin and enforce HTTPS.
  if (values.length) response.setHeader('Set-Cookie', values.map(value => {
    const cookie = value.replace(/;\s*Domain=[^;]*/ig, '');
    return /;\s*Secure(?:;|$)/i.test(cookie) ? cookie : `${cookie}; Secure`;
  }));
}

export default async function handler(request, response) {
  response.setHeader('Cache-Control', 'no-store');
  response.setHeader('X-NLI-Upstream', 'controller');
  response.setHeader('X-NLI-Stream-Mode', 'poll');
  if (!['GET', 'HEAD', 'POST', 'PUT', 'DELETE', 'OPTIONS'].includes(request.method || '')) {
    response.setHeader('Allow', 'GET, HEAD, POST, PUT, DELETE, OPTIONS');
    response.status(405).json({ error: 'method not allowed' });
    return;
  }

  let base;
  try {
    base = controllerOrigin();
  } catch (error) {
    response.status(503).json({ error: error.message });
    return;
  }

  let incoming;
  try {
    incoming = new URL(request.url, 'https://nli-proxy.invalid');
    let decoded = incoming.pathname;
    for (let i = 0; i < 3; i++) {
      decoded = decodeURIComponent(decoded);
      if (decoded.includes('\\') || decoded.split('/').some(part => part === '..' || part === '.') || /[\u0000-\u001f]/.test(decoded)) throw new Error('invalid path');
    }
  } catch {
    response.status(400).json({ error: 'invalid controller route' });
    return;
  }
  if (!incoming.pathname.startsWith('/api/v1/')) {
    response.status(404).json({ error: 'route not found' });
    return;
  }
  const target = new URL(incoming.pathname + incoming.search, base);
  if (target.origin !== base.origin || !target.pathname.startsWith('/api/v1/')) {
    response.status(400).json({ error: 'invalid controller route' });
    return;
  }
  if (target.pathname === '/api/v1/stream') {
    response.status(426).json({ error: 'This hosted proxy uses metadata polling. Use the controller endpoint for WebSocket streaming.' });
    return;
  }
  const isSSE = target.pathname === '/api/v1/telemetry/stream';
  if (Number(request.headers?.['content-length'] || 0) > MAX_REQUEST_BYTES) {
    response.status(413).json({ error: 'Hosted requests are limited to 4.4 MB. Open the controller URL to upload larger artifacts.' });
    return;
  }

  const headers = new Headers();
  for (const [name, value] of Object.entries(request.headers || {})) {
    if (requestHeaders.has(name.toLowerCase()) && value !== undefined) {
      headers.set(name, Array.isArray(value) ? value.join(', ') : value);
    }
  }
  headers.set('x-nli-proxy', 'vercel');
  // Avoid forwarding a compressed Content-Length after fetch decompression.
  headers.set('accept-encoding', 'identity');

  const options = { method: request.method, headers, redirect: 'manual', signal: AbortSignal.timeout(isSSE ? SSE_TIMEOUT_MS : UPSTREAM_TIMEOUT_MS) };
  if (['POST', 'PUT', 'DELETE'].includes(request.method)) {
    options.body = request;
    options.duplex = 'half';
  }

  let upstream;
  try {
    upstream = await fetch(target, options);
  } catch (error) {
    const timedOut = error.name === 'TimeoutError' || error.name === 'AbortError';
    response.status(timedOut ? 504 : 502).json({ error: timedOut ? 'controller request timed out' : 'controller is unreachable from Vercel' });
    return;
  }

  if (upstream.status >= 300 && upstream.status < 400 && upstream.status !== 304) {
    await upstream.body?.cancel();
    response.status(502).json({ error: 'controller API redirected; configure its final HTTPS origin' });
    return;
  }

  response.statusCode = upstream.status;
  if (isSSE) response.setHeader('X-NLI-Stream-Mode', 'sse');
  if (isSSE) response.setHeader('Cache-Control', upstream.headers.get('cache-control') || 'no-cache, no-transform');
  for (const [name, value] of upstream.headers.entries()) {
    if (responseHeaders.has(name.toLowerCase()) && (!isSSE || name.toLowerCase() !== 'cache-control') && name.toLowerCase() !== 'cache-control') response.setHeader(name, value);
  }
  copySetCookies(upstream, response);
  response.setHeader('X-NLI-Upstream', 'controller');

  if (!upstream.body || request.method === 'HEAD' || upstream.status === 204 || upstream.status === 304) {
    response.end();
    return;
  }
  try {
    const reader = upstream.body.getReader();
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      if (!response.write(Buffer.from(value))) await new Promise(resolve => response.once('drain', resolve));
    }
    response.end();
  } catch { if (!response.destroyed) response.destroy(); }
}
