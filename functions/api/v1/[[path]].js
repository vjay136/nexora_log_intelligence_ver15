const REQUEST_HEADERS = ['accept', 'accept-language', 'authorization', 'content-type', 'cookie', 'if-match', 'if-none-match', 'user-agent', 'x-artifact-sha256', 'x-artifact-signature', 'x-csrf-token'];
const RESPONSE_HEADERS = ['allow', 'content-disposition', 'content-type', 'etag', 'last-modified', 'x-nli-controller'];
const MAX_BODY_BYTES = 4_400_000;

function originFromEnv(raw) {
  let origin;
  try { origin = new URL(String(raw || '').trim()); } catch { throw new Error('Set NLI_CONTROLLER_ORIGIN to the controller public HTTPS origin.'); }
  const host = origin.hostname.toLowerCase().replace(/\.$/, '');
  const ipv4Literal = /^\d{1,3}(\.\d{1,3}){3}$/.test(host);
  if (origin.protocol !== 'https:' || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash || !host.includes('.') || /(^|\.)(localhost|local|internal|lan|corp|intranet|home\.arpa|test|invalid)$/.test(host) || ipv4Literal || host.includes(':')) {
    throw new Error('NLI_CONTROLLER_ORIGIN must be a public HTTPS DNS hostname, optionally with a port, without credentials or a path.');
  }
  return origin;
}

export async function onRequest({ request, env }) {
  const method = request.method.toUpperCase();
  if (!['GET', 'HEAD', 'POST', 'PUT', 'DELETE', 'OPTIONS'].includes(method)) return new Response(JSON.stringify({ error: 'method not allowed' }), { status: 405, headers: { Allow: 'GET, HEAD, POST, PUT, DELETE, OPTIONS', 'Content-Type': 'application/json' } });
  let origin, url, isSSE = false;
  try {
    origin = originFromEnv(env.NLI_CONTROLLER_ORIGIN);
    const incoming = new URL(request.url);
    let path = incoming.pathname;
    for (let i = 0; i < 3; i++) { path = decodeURIComponent(path); if (path.includes('\\') || path.split('/').some(part => part === '.' || part === '..') || /[\u0000-\u001f]/.test(path)) throw new Error('Invalid controller route.'); }
    if (!path.startsWith('/api/v1/')) throw new Error('Invalid controller route.');
    if (path === '/api/v1/stream') return Response.json({ error: 'Hosted proxy uses metadata polling; use the controller endpoint for WebSocket streaming.' }, { status: 426 });
    isSSE = path === '/api/v1/telemetry/stream';
    url = new URL(path + incoming.search, origin);
    if (url.origin !== origin.origin || !url.pathname.startsWith('/api/v1/')) throw new Error('Invalid controller route.');
  } catch (error) {
    return Response.json({ error: error.message }, { status: 503 });
  }
  const length = Number(request.headers.get('content-length') || 0);
  if (length > MAX_BODY_BYTES) return Response.json({ error: 'Hosted requests are limited to 4.4 MB. Open the controller URL to upload larger artifacts.' }, { status: 413 });
  const headers = new Headers();
  for (const name of REQUEST_HEADERS) { const value = request.headers.get(name); if (value !== null) headers.set(name, value); }
  headers.set('x-nli-proxy', 'cloudflare-pages');
  headers.set('accept-encoding', 'identity');
  let upstream;
  try {
    upstream = await fetch(url, { method, headers, body: ['GET', 'HEAD', 'OPTIONS'].includes(method) ? undefined : request.body, redirect: 'manual', ...(isSSE ? {} : { signal: AbortSignal.timeout(50_000) }) });
  } catch (error) {
    const timedOut = error.name === 'TimeoutError' || error.name === 'AbortError';
    return Response.json({ error: timedOut ? 'Controller request timed out. Check its health and network path.' : 'Controller is unreachable from Cloudflare. Verify its public HTTPS origin, DNS, tunnel, firewall and certificate.' }, { status: timedOut ? 504 : 502 });
  }
  if (upstream.status >= 300 && upstream.status < 400 && upstream.status !== 304) return Response.json({ error: 'Controller API redirected; configure its final HTTPS origin.' }, { status: 502 });
  const responseHeaders = new Headers({ 'Cache-Control': 'no-store', 'X-NLI-Upstream': 'controller', 'X-NLI-Stream-Mode': isSSE ? 'sse' : 'poll' });
  for (const name of RESPONSE_HEADERS) { const value = upstream.headers.get(name); if (value !== null) responseHeaders.set(name, value); }
  if (isSSE) { responseHeaders.set('X-NLI-Stream-Mode', 'sse'); responseHeaders.set('Cache-Control', upstream.headers.get('cache-control') || 'no-cache, no-transform'); }
  const setCookies = typeof upstream.headers.getSetCookie === 'function' ? upstream.headers.getSetCookie() : [upstream.headers.get('set-cookie')].filter(Boolean);
  for (const value of setCookies) { const cookie = value.replace(/;\s*Domain=[^;]*/ig, ''); responseHeaders.append('Set-Cookie', /;\s*Secure(?:;|$)/i.test(cookie) ? cookie : `${cookie}; Secure`); }
  if (upstream.status === 204 || upstream.status === 304 || method === 'HEAD') return new Response(null, { status: upstream.status, headers: responseHeaders });
  return new Response(upstream.body, { status: upstream.status, headers: responseHeaders });
}
