import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

// These browser regressions verify frontend behavior independently of network
// and database availability. e2e.spec.js separately exercises a real controller.
const origin = 'http://127.0.0.1:4176';
const tabs = ['operations', 'assurance', 'streams', 'api-monitor', 'field-integrity', 'fleet', 'parser-sync', 'clients', 'controller', 'parsers', 'migration', 'input-paths', 'kv', 'static', 'projection', 'studio', 'regex', 'knownkeys', 'semantic', 'engine-network', 'engine-url', 'engine-email', 'engine-subject', 'outputs', 'metadata', 'unknowns', 'drift', 'performance', 'rollouts', 'artifacts', 'security', 'system', 'ai'];
const assets = Object.fromEntries(await Promise.all(['index.html', 'app.js', 'styles.css', 'nli-mark.svg'].map(async name => [name, await readFile(new URL(name, import.meta.url), 'utf8')])));

async function mockController(page, { role = 'admin', healthIdentity = 'v13' } = {}) {
  const observed = [];
  const cases = [];
  const uploads = [];
  const assuranceRequests = [];
  const assuranceCorpora = [];
  const outputSaves = [];
  const inputSaves = [];
  const outputProfiles = [];
  const inputProfiles = [];
  const rolloutSaves = [];
  let authenticated = false;
  await page.route(`${origin}/**`, async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (!path.startsWith('/api/v1/')) {
      const name = path === '/' ? 'index.html' : path.slice(1);
      return route.fulfill({ status: assets[name] ? 200 : 404, contentType: name.endsWith('.js') ? 'application/javascript' : name.endsWith('.css') ? 'text/css' : name.endsWith('.svg') ? 'image/svg+xml' : 'text/html', body: assets[name] || 'Not found' });
    }
    observed.push(`${request.method()} ${path}`);
    const headers = { 'content-type': 'application/json', 'x-nli-controller': healthIdentity, 'x-nli-upstream': 'controller' };
    const fulfill = (value, status = 200) => route.fulfill({ status, headers, body: JSON.stringify(value) });
    if (path === '/api/v1/health') return fulfill({ status: 'ok', service: 'nli-controller-agent', api_version: 'v1' });
    if (path === '/api/v1/auth/login' && request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: { ...headers, allow: 'POST, OPTIONS' } });
    if (path === '/api/v1/auth/login') { authenticated = true; return fulfill({ status: 'ok' }); }
    if (path === '/api/v1/auth/me') return authenticated ? fulfill({ username: 'test-operator', role, permissions: ['assurance:run'] }) : fulfill({ error: 'unauthorized' }, 401);
    if (path === '/api/v1/auth/csrf') return fulfill({ csrf_token: 'browser-test-csrf' });
    if (path === '/api/v1/auth/logout') { authenticated = false; return fulfill({ status: 'ok' }); }
    if (path === '/api/v1/assurance/run') {
      const requestBody = request.postDataJSON();
      assuranceRequests.push(requestBody);
      return fulfill({ passed: true, route: 'normal', actual: { message: requestBody.raw, service: requestBody.service }, parser: { name: 'test-parser', version: '7', strategy: 'json' }, counts: { actual: 2, attempts: 1 }, trace: [{ stage: 'parser_attempt', parser: 'test-parser', success: true, fields: 2 }], evidence_spans: [{ field: 'message', source_backed: true, start: 0, end: requestBody.raw.length }], parser_creation_helper: { normalized_sample: requestBody.raw.replace(/\r\n/g,'\n').split('\n').map(line=>line.replace(/[\t ]+$/,'')).join('\n'), sample_lines: 2, detected_format: 'key_value', requested_family: requestBody.service, selected_parser: 'test-parser', selected_strategy: 'key_value', match_scope: { kind: 'line', prefix: 'level=' }, suggested_regex: 'level=(?P<level>\\S+) user=(?P<user>\\S+)', kv_delimiters: ['='], fields: [{ name: 'level', source: 'group:level', type: 'string' }, { name: 'user', source: 'group:user', type: 'string' }], projection: ['level','user'], template: { level: '{level}', user: '{user}' }, requires_human_approval: true } });
    }
    if (path === '/api/v1/assurance/corpus' && request.method() === 'POST') { const body=request.postDataJSON();assuranceCorpora.push(body);return fulfill({total:body.cases.length,passed:body.cases.length,failed:0,results:[]}); }
    if (path === '/api/v1/assurance/upload' && request.method() === 'POST') { uploads.push(request.postData() || ''); return fulfill({ retained: true, fixture_id: 'fixture-42', encrypted: true, filename: 'sample.log', content_type: 'text/plain', size_bytes: 28, sha256: 'a'.repeat(64), parser_revision: 'smtp@13.0.0', storage_status: 'encrypted_supabase' }); }
    if (path === '/api/v1/assurance/cases/fixture-42/download') return route.fulfill({ status: 200, headers: { ...headers, 'content-type': 'application/octet-stream' }, body: 'service=example user=operator' });
    if (path === '/api/v1/assurance/cases' && request.method() === 'POST') { const body = request.postDataJSON(); cases.push({ id: 'case-1', name: body.name, revision: 1, manual_retention: true, parser_id: 'test-parser', passed: true }); return fulfill(cases.at(-1)); }
    if (path === '/api/v1/assurance/cases') return fulfill({ cases });
    if (path === '/api/v1/assurance/parsers' && request.method() === 'POST') return fulfill({ id: 'draft-1', state: 'draft' }, 201);
    if (path === '/api/v1/assurance/parsers') return fulfill({ parsers: [] });
    if (path === '/api/v1/assurance/history') return fulfill({ events: [] });
    if (path === '/api/v1/quarantine/records/q-1/raw') return route.fulfill({ status: 200, headers: { ...headers, 'content-type': 'text/plain' }, body: 'exact raw source line' });
    if (path === '/api/v1/admin/ai-provider') return fulfill({ provider: { provider: 'openai', model: 'test-model', configured: false, enabled: false } });
    if (path === '/api/v1/dashboard/snapshot') return fulfill({ overview: { fleet: {}, metadata: {}, storage: { mode: 'supabase', status: 'connected' } }, metadata: { records: [], routes: {} }, system: {}, ai_usage: {}, fleet_sync: { nodes: [] }, timeseries: { points: [] }, stream_files: [] });
    if (path === '/api/v1/overview') return fulfill({ fleet: {}, metadata: {}, storage: { mode: 'supabase', status: 'connected' } });
    if (path === '/api/v1/telemetry/stats') return fulfill({ received: 120, parsed: 116, quarantined: 4 });
    if (path === '/api/v1/telemetry/stream/snapshot') return fulfill({ bounded: true, files: [{ node_id: 'node-1', source: '/var/log/mail.log', phase: 'tailing', read_lines_per_second: 90, normalized_per_second: 80, quarantined_per_second: 5, bytes_per_second: 4096, queue_depth: 4, queue_capacity: 128, backpressure: false, read_duration_ns: 1000000, parse_duration_ns: 2000000, output_duration_ns: 3000000, total_duration_ns: 6000000, read_lines: 900, updated_at: '2026-10-14T00:00:00Z', thresholds: { read_lines_per_second: 80, bytes_per_second: 5000, quarantined_per_second: 10, queue_depth: 100 } }] });
    if (path === '/api/v1/telemetry/query') return fulfill({ telemetry: [] });
    if (path === '/api/v1/quarantine/summary') return fulfill({ waiting: 1, reprocessing: 0, resolved: 2, raw_available: 1 });
    if (path === '/api/v1/quarantine/records') return fulfill({ records: [{ id: 'q-1', node_id: 'node-1', service: 'smtp', first_seen: '2026-10-10T00:00:00Z', attempt_count: 2, status: 'waiting', error_codes: ['no_match'], raw_available: true }] });
    if (path === '/api/v1/quarantine/history') return fulfill({ events: [] });
    if (path === '/api/v1/quarantine/reprocess' && request.method() === 'POST') return fulfill({ rollout_id: 'replay-1', status: 'pending' }, 202);
    if (path.startsWith('/api/v1/metrics/timeseries')) return fulfill({ points: [] });
    if (path === '/api/v1/verification/sample-corpus') return fulfill({ verdict: 'pass', known_key_inventory: { declared_family_keys: 278, covered_family_keys: 278, unmapped_family_keys: 0 }, totals: { records: 728, accounted_records: 728, parsed_records: 723, exact_parity_records: 723, failing_records: 0, expected_quarantined_records: 5, unexpected_quarantined_records: 0, parsed_comparable_raw_fields: 13101, parsed_generated_json_fields: 13101 }, families: [{ family: 'IMAP', records: 52, passing_records: 52, comparable_raw_fields: 1154, generated_json_fields: 1154, duplicate_occurrences: 0 }], records: [{ corpus: 'samples.log', source_line: 50, family: 'SMTP-OUT', expected_disposition: 'quarantined', disposition_reason: 'truncated record', record_sha256: 'a'.repeat(64) }] });
    if (path === '/api/v1/admin/ai-usage') return fulfill({ requests: 0, tokens: 0, pending_drafts: 0 });
    if (path === '/api/v1/fleet/sync') return fulfill({ nodes: [] });
    if (path === '/api/v1/parser-sync' && request.method() === 'GET') return fulfill({ count: 1, assets: [{ asset_class: 'regex', asset_id: 'nginx-access', name: 'Nginx access pattern', desired: { revision: 3, sha256: 'a'.repeat(64), state: 'published' }, controller: { revision: 3, sha256: 'a'.repeat(64), status: 'applied' }, auto_rollout: { enabled: true, status: 'enabled' }, rollout: { id: 'rollout-1', status: 'complete', total_nodes: 1, done_nodes: 1, failed_nodes: 0, errors: [] }, clients: [{ node_id: 'node-1', desired_revision: 3, desired_hash: 'a'.repeat(64), applied_revision: 3, applied_hash: 'a'.repeat(64), last_seen: '2026-10-09T10:00:00Z', status: 'online', drifted: false }] }] });
    if (path === '/api/v1/parser-sync/verify' && request.method() === 'POST') return fulfill({ verified: true, message: 'Reported state refreshed' });
    if (path === '/api/v1/parser-sync/retry' && request.method() === 'POST') return fulfill({ proposal_id: 'proposal-1', state: 'pending_approval', message: 'Rollout proposal created; independent approval is required.' });
    if (path === '/api/v1/parser-packs') return fulfill({ parser_packs: [] });
    if (path === '/api/v1/parsers') return fulfill([{ id: 'smtp', version: 7, format: 'syslog', description: 'SMTP parser', fields: ['message'] }]);
    if (path === '/api/v1/migration/reports') return fulfill({ reports: [] });
    if (path === '/api/v1/config/output-destinations' && request.method() === 'POST') { outputSaves.push(request.postDataJSON()); return fulfill({ id: 'customer-output', state: 'draft', revision: 1 }); }
    if (path === '/api/v1/config/output-destinations') return fulfill({ items: outputProfiles, count: outputProfiles.length });
    if (path.startsWith('/api/v1/config/output-destinations/')) { const item=outputProfiles.find(profile=>path.endsWith(`/${profile.id}`));return item?fulfill(item):fulfill({error:'not found'},404); }
    if (path === '/api/v1/config/input-paths' && request.method() === 'POST') { inputSaves.push(request.postDataJSON()); return fulfill({ id: 'mail-paths', state: 'draft', revision: 1 }); }
    if (path === '/api/v1/config/input-paths') return fulfill({ items: inputProfiles, count: inputProfiles.length });
    if (path.startsWith('/api/v1/config/input-paths/')) { const item=inputProfiles.find(profile=>path.endsWith(`/${profile.id}`));return item?fulfill(item):fulfill({error:'not found'},404); }
    if (path.startsWith('/api/v1/config/')) return fulfill({ items: [] });
    if (path === '/api/v1/metadata') return fulfill({ records: [], stats: {}, payload_retained: false });
    if (path === '/api/v1/rollouts' && request.method() === 'POST') { rolloutSaves.push(request.postDataJSON()); return fulfill({ id: 'proposal-test', state: 'pending_approval' }, 201); }
    if (path === '/api/v1/rollouts') return fulfill({ proposals: [], live: [] });
    if (path === '/api/v1/artifacts') return fulfill({ artifacts: [], signing_keys_configured: 0 });
    if (path === '/api/v1/rbac/roles') return fulfill({ roles: { viewer: ['assurance:run'], admin: ['*'] } });
    if (path === '/api/v1/users') return fulfill({ users: [] });
    if (path === '/api/v1/audit') return fulfill({ verification: 'ok', events: [] });
    if (path === '/api/v1/system') return fulfill({ version: '13.0.0', health: 'ok', storage: { mode: 'supabase', status: 'connected' }, security: { audit_chain: 'ok' } });
    return fulfill({ error: `Unmocked API: ${path}` }, 500);
  });
  return { observed, uploads, assuranceRequests, assuranceCorpora, outputSaves, inputSaves, outputProfiles, inputProfiles, rolloutSaves };
}

async function login(page) {
  await page.goto(origin);
  await expect(page.getByRole('button', { name: 'Sign in securely' })).toBeEnabled();
  await page.getByLabel('Username').fill('test-operator');
  await page.getByLabel('Password').fill('test-only-password');
  await page.getByRole('button', { name: 'Sign in securely' }).click();
  await expect(page.getByTestId('nav-operations')).toBeVisible();
  await expect(page.locator('.loading-state')).toHaveCount(0);
}

test('all workspaces render and audit is visible', async ({ page }) => {
  const { observed } = await mockController(page);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await login(page);
  await expect(page.locator('.topbar-brand')).toContainText('Nexora Log Inetelligence Ver13');
  await expect(page.locator('.topbar-brand')).toContainText('Infinite Logs. One Engine (NLI). Intelligent Parsing. Multiple Endpoints. Fully Secured.');
  await expect(page.locator('#stream-state')).toHaveText(/Live telemetry|Reconnecting|API polling/);
  for (const tab of tabs) {
    await page.getByTestId(`nav-${tab}`).click();
    await expect(page.locator('#page-title')).not.toBeEmpty();
    await expect(page.locator('.loading-state')).toHaveCount(0);
    await expect(page.locator('.error-state')).toHaveCount(0);
    await expect(page.locator('#page')).not.toContainText('undefined');
    if (tab === 'fleet') await expect(page.getByText('No client has checked in yet.')).toBeVisible();
    if (tab === 'operations') await expect(page.getByRole('heading', { name: 'Live parsing at a glance' })).toBeVisible();
    if (tab === 'api-monitor') await expect(page.getByRole('heading', { name: 'API monitor', exact: true })).toBeVisible();
    if (tab === 'unknowns') await expect(page.getByRole('heading', { name: 'Quarantine and reprocessing' })).toBeVisible();
    if (tab === 'parsers') await expect(page.getByText('SMTP parser', { exact: true }).first()).toBeVisible();
    if (tab === 'security') await expect(page.getByRole('heading', { name: 'Immutable audit chain' })).toBeVisible();
  }
  expect(observed.filter(item => item.endsWith('/api/v1/stream'))).toEqual([]);
  const fleetSyncRequests = observed.filter(item => item === 'GET /api/v1/fleet/sync');
  expect(fleetSyncRequests.length).toBeGreaterThanOrEqual(1);
  expect(fleetSyncRequests.length).toBeLessThanOrEqual(6);
  expect(errors).toEqual([]);
  await page.getByRole('button', { name: 'Notifications' }).click();
  await expect(page.getByText('Notifications', { exact: true })).toBeVisible();
});

test('streams render live per-node rates and file processing timings', async ({ page }) => {
  await mockController(page);
  await login(page);
  await page.getByTestId('nav-streams').click();
  await expect(page.getByRole('heading', { name: 'Streams', exact: true, level: 1 })).toBeVisible();
  await expect(page.locator('#stream-live-table')).toContainText('/var/log/mail.log');
  await expect(page.locator('#stream-live-table')).toContainText('90.0');
  await expect(page.locator('#stream-live-table')).toContainText('high volume');
  await page.getByTestId('nav-performance').click();
  await expect(page.getByRole('heading', { name: 'Per-file processing durations' })).toBeVisible();
  await expect(page.locator('#page')).toContainText('6.00 ms');
});

test('operations uses one dashboard snapshot aggregate request', async ({ page }) => {
  const { observed } = await mockController(page);
  await login(page);
  const start = observed.length;
  await page.getByTestId('nav-operations').click();
  const requests = observed.slice(start);
  expect(requests.filter(item => item.startsWith('GET /api/v1/dashboard/snapshot'))).toHaveLength(1);
  expect(requests.some(item => /GET \/api\/v1\/(overview|metadata\?|system|fleet\/sync|metrics\/timeseries)/.test(item))).toBe(false);
});

test('telemetry SSE reconnect includes last-event position, updates DOM, and falls back to snapshot polling', async ({ page }) => {
  await page.clock.install();
  await mockController(page);
  await page.addInitScript(() => {
    const instances=[];
    class FailingEventSource extends EventTarget {
      constructor(url){super();this.url=url;this.closed=false;instances.push(this);window.__failingSources=instances;setTimeout(()=>this.onerror?.(new Event('error')),0);}
      close(){this.closed=true;}
    }
    window.EventSource=FailingEventSource;
  });
  await login(page);
  await page.getByTestId('nav-streams').click();
  await expect.poll(() => page.evaluate(() => window.__failingSources?.length||0)).toBe(1);
  await page.evaluate(() => window.__failingSources[0].onerror?.(new Event('error')));
  await page.clock.fastForward(5_100);
  await expect.poll(() => page.evaluate(() => window.__failingSources.length)).toBeGreaterThan(1);
  expect(await page.evaluate(() => window.__failingSources.at(-1).url)).toContain('last_event_id=');
  await expect(page.locator('#stream-live-table')).toContainText('/var/log/mail.log');
});

test('SSE snapshot and telemetry events update stream rows incrementally and retain the last event ID', async ({ page }) => {
  await mockController(page);
  await page.addInitScript(() => {
    const instances=[];
    class FakeEventSource extends EventTarget {
      constructor(url){super();this.url=url;this.closed=false;instances.push(this);window.__streamSources=instances;setTimeout(()=>this.onopen?.(),0);}
      close(){this.closed=true;}
      emit(type,data,id=''){const event=new MessageEvent(type,{data:JSON.stringify(data)});Object.defineProperty(event,'lastEventId',{value:id});this.dispatchEvent(event);}
    }
    window.EventSource=FakeEventSource;
  });
  await login(page);
  await page.getByTestId('nav-streams').click();
  await expect.poll(() => page.evaluate(() => window.__streamSources?.length||0)).toBeGreaterThan(0);
  await page.evaluate(() => window.__streamSources.at(-1).emit('snapshot',{files:[{node_id:'node-live',source:'/var/log/live.log',phase:'tailing',read_lines_per_second:12,normalized_per_second:10,quarantined_per_second:2,bytes_per_second:512,queue_depth:1,queue_capacity:20,updated_at:'2026-10-14T00:00:00Z'}]},'evt-42'));
  await expect(page.locator('#stream-live-table')).toContainText('node-live');
  await expect(page.locator('#stream-live-table')).toContainText('12.0');
  await page.evaluate(() => window.__streamSources.at(-1).emit('telemetry',{node_id:'node-live',source:'/var/log/live.log',phase:'eof',read_lines_per_second:20,normalized_per_second:18,quarantined_per_second:1,bytes_per_second:700,queue_depth:0,queue_capacity:20,updated_at:'2026-10-14T00:00:05Z'}));
  await expect(page.locator('#stream-live-table')).toContainText('20.0');
	await page.evaluate(() => window.__streamSources.at(-1).onerror?.(new Event('error')));
	await new Promise(resolve => setTimeout(resolve, 1_100));
  expect(await page.evaluate(() => window.__streamSources.at(-1).url)).toContain('last_event_id=evt-42');
});

test('production Assurance response fills helper using normalized backend proposal and saves a draft', async ({ page }) => {
  const { observed } = await mockController(page);
  await login(page);
  await page.getByTestId('nav-assurance').click();
  await page.getByTestId('assurance-raw').fill('level=INFO user=alice   \r\nlevel=WARN user=bob\t  \r\n');
  await page.getByRole('button', { name: 'Run assurance', exact: true }).click();
  await page.getByRole('button', { name: 'Parser Creation helper' }).click();
  const form=page.locator('#parser-helper-form');
  await expect(form.locator('[name=samples]')).toHaveValue('level=INFO user=alice\nlevel=WARN user=bob');
  const parser=JSON.parse(await form.locator('[name=parser]').inputValue());
  expect(parser.known_fields).toEqual(['level', 'user']);
  expect(parser.header_pattern).toMatchObject({ kind: 'line', prefix: 'level=' });
  expect(parser.template).toEqual({ level: '{level}', user: '{user}' });
  await expect(page.locator('#parser-helper-overview')).toContainText('key_value');
  await page.getByRole('button', { name: 'Save review-gated draft' }).click();
  await expect.poll(()=>observed.includes('POST /api/v1/assurance/parsers')).toBe(true);
  expect(observed.some(path=>/assurance\/parsers\/.*\/(approve|publish)/.test(path))).toBe(false);
});

test('Assurance helper trims trailing whitespace, generates editable parser, validates and saves only a draft', async ({ page }) => {
  const { observed } = await mockController(page);
  await login(page);
  await page.getByTestId('nav-assurance').click();
  const labels = await page.locator('.assurance-subnav .tab').allTextContents();
  expect(labels).toEqual(['Analyse', 'Corpus reports', 'Parser drafts', 'Review Queue', 'History', 'Fixtures', 'Parser Creation helper']);
  await page.getByRole('button', { name: 'Parser Creation helper' }).click();
  const form = page.locator('#parser-helper-form');
  await form.locator('[name=samples]').fill('level=INFO user=alice   \r\nlevel=WARN user=bob\t  \r\n');
  await page.getByRole('button', { name: 'Generate parser draft' }).click();
  await expect(form.locator('[name=samples]')).toHaveValue('level=INFO user=alice\nlevel=WARN user=bob');
  const parser = JSON.parse(await form.locator('[name=parser]').inputValue());
  expect(parser.pattern_type).toBe('regex');
  expect(parser.known_fields).toEqual(['level', 'user']);
  await expect(page.locator('#parser-helper-overview')).toContainText('key_value_regex');
  await page.getByRole('button', { name: 'Validate draft', exact: true }).click();
  await expect.poll(() => observed.includes('POST /api/v1/assurance/corpus')).toBe(true);
  await page.getByRole('button', { name: 'Save review-gated draft' }).click();
  await expect.poll(() => observed.includes('POST /api/v1/assurance/parsers')).toBe(true);
  expect(observed.some(path => /assurance\/parsers\/.*\/(approve|publish)/.test(path))).toBe(false);
});

test('operations requests one aggregate snapshot and does not fan out to legacy summary endpoints', async ({ page }) => {
  const { observed } = await mockController(page);
  await login(page);
  const before = observed.length;
  await page.getByTestId('nav-operations').click();
  const requests = observed.slice(before);
  expect(requests.filter(item => item.startsWith('GET /api/v1/dashboard/snapshot'))).toHaveLength(1);
  expect(requests.some(item => /\/api\/v1\/(overview|metadata|system|fleet\/sync|metrics\/timeseries)/.test(item))).toBe(false);
});

test('stream polling fallback timer is stopped at signout', async ({ page }) => {
  await page.clock.install();
  const { observed } = await mockController(page);
  await page.addInitScript(() => { window.EventSource = undefined; });
  await login(page);
  await expect(page.getByTestId('nav-streams')).toBeVisible();
  await page.clock.fastForward(5_100);
  await expect.poll(() => observed.filter(item => item === 'GET /api/v1/telemetry/stream/snapshot').length).toBeGreaterThan(0);
  const beforeSignout = observed.filter(item => item === 'GET /api/v1/telemetry/stream/snapshot').length;
  await page.getByRole('button', { name: 'Sign out' }).click();
  await page.clock.fastForward(15_000);
  expect(observed.filter(item => item === 'GET /api/v1/telemetry/stream/snapshot')).toHaveLength(beforeSignout);
});

test('API monitor starts its independent probes concurrently and displays latency percentiles', async ({ page }) => {
  const starts = [];
  await mockController(page);
  await page.route(`${origin}/api/v1/**`, async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/auth/login' || path === '/api/v1/auth/csrf' || path === '/api/v1/auth/me' || path === '/api/v1/health') return route.fallback();
    starts.push(Date.now());
    await new Promise(resolve => setTimeout(resolve, 80));
    await route.fallback();
  });
  await login(page);
  starts.length = 0;
  await page.getByTestId('nav-api-monitor').click();
  await expect(page.getByRole('heading', { name: 'API monitor', exact: true })).toBeVisible();
  await expect(page.getByText('Latency p50')).toBeVisible();
  await expect(page.getByText('Latency p95')).toBeVisible();
  expect(starts.length).toBeGreaterThan(10);
  expect(Math.max(...starts.slice(0, 12)) - Math.min(...starts.slice(0, 12))).toBeLessThan(70);
});

test('Endpoint Configuration tests and saves a safe ClickHouse field-mapped profile', async ({ page }) => {
  const mock = await mockController(page);
  await login(page);
  await page.getByTestId('nav-outputs').click();
  await expect(page.getByRole('heading', { name: 'Endpoint Configuration', level: 1 })).toBeVisible();
  const form = page.locator('#output-profile-form');
  await form.locator('[name=normalised_enabled]').uncheck();
  await form.locator('[name=destinations][value=clickhouse]').check();
  await form.locator('[name=clickhouse_url]').fill('https://clickhouse.example.com:8443');
  await form.locator('[name=field_columns]').fill('{"fromhost":"client_ip"}');
  await form.getByRole('button', { name: 'Test configuration' }).click();
  await expect(page.locator('#output-config-test-result')).toContainText('Configuration test passed');
  await expect(page.locator('#output-profile-preview')).toContainText('normal');
  await form.getByRole('button', { name: 'Validate and save draft' }).click();
  await expect(page.locator('#output-profile-preview')).toContainText('client_ip');
  expect(mock.outputSaves).toHaveLength(1);
  const outputs = mock.outputSaves[0].config.outputs;
  expect(outputs.find(item => item.type === 'clickhouse')).toMatchObject({ url: 'https://clickhouse.example.com:8443/', field_columns: { fromhost: 'client_ip' }, fallback_path: '/var/log/nli/output/delivery-fallback.jsonl', routes: ['normal', 'warning'] });
  expect(outputs.some(item => item.type === 'file' && item.routes.includes('quarantine'))).toBe(true);
  expect(outputs.some(item => item.type === 'file' && item.routes.includes('normal'))).toBe(false);
});

test('published endpoint profile creates an outputs-only approval rollout', async ({ page }) => {
  const mock = await mockController(page);
  const profile = { id: 'remote-delivery', revision: 4, state: 'active', updated_at: '2026-10-14T00:00:00Z', payload: { name: 'Remote delivery', config: { outputs: [
    { type: 'loki', url: 'https://loki.example/loki/api/v1/push', routes: ['normal', 'warning'], fallback_path: '/var/lib/nli/spool/loki.jsonl' },
    { type: 'file', path_template: '/var/log/nli/output/auto_quarantine_{hour_utc}.jsonl', routes: ['quarantine', 'unknown'], durable_raw: true },
  ] } } };
  mock.outputProfiles.push(profile);
  await login(page);
  await page.getByTestId('nav-outputs').click();
  const form = page.locator('#output-rollout-form');
  await form.locator('[name=profile_id]').selectOption('remote-delivery');
  await form.locator('[name=targets]').fill('node-a, node-b');
  await form.getByRole('button', { name: 'Request independent approval' }).click();
  await expect.poll(() => mock.rolloutSaves.length).toBe(1);
  expect(mock.rolloutSaves[0]).toMatchObject({ type: 'config_push', target_nodes: ['node-a', 'node-b'], payload: { config_patch: { outputs: profile.payload.config.outputs } } });
});

test('Parser Fleet Sync shows live revision, drift, rollout and approval-gated verify/retry actions', async ({ page }) => {
  const { observed } = await mockController(page);
  await login(page);
  await page.getByTestId('nav-parser-sync').click();
  await expect(page.locator('#page')).toContainText('Nginx access pattern');
  await expect(page.locator('#page')).toContainText('published');
  await expect(page.locator('#page')).toContainText('node-1');
  await expect(page.locator('#page')).toContainText('1 / 1 complete');
  await page.getByRole('button', { name: 'Verify' }).first().click();
  await expect.poll(() => observed.includes('POST /api/v1/parser-sync/verify')).toBe(true);
  await page.getByRole('button', { name: 'Propose retry' }).last().click();
  await expect.poll(() => observed.includes('POST /api/v1/parser-sync/retry')).toBe(true);
  await expect(page.locator('#toast-region')).toContainText('independent approval is required');
});

test('Quarantine queue exposes exact raw access, exports and auditable replay controls', async ({ page }) => {
  const { observed } = await mockController(page);
  await login(page);
  await page.getByTestId('nav-unknowns').click();
  await expect(page.getByRole('heading', { name: 'Quarantine and reprocessing' })).toBeVisible();
  await expect(page.getByText('exact raw source line')).toHaveCount(0);
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download exact raw' }).click();
  expect((await download).suggestedFilename()).toBe('nli-quarantine-q-1.log');
  await page.getByRole('button', { name: 'Diagnose in Assurance' }).click();
  await expect(page.getByRole('heading', { name: 'Assurance Lab' })).toBeVisible();
  await expect(page.getByTestId('assurance-raw')).toHaveValue('exact raw source line');
  await page.getByTestId('nav-unknowns').click();
  await page.getByLabel('Record IDs').fill('q-1');
  await page.getByLabel('Target client ID').fill('node-1');
  await page.getByRole('button', { name: 'Request replay' }).click();
  await expect.poll(() => observed.includes('POST /api/v1/quarantine/reprocess')).toBe(true);
  await page.getByTestId('nav-api-monitor').click();
  await expect(page.getByRole('heading', { name: 'API monitor', exact: true })).toBeVisible();
  await expect(page.getByText('/system', { exact: true })).toBeVisible();
});

test('Assurance enables clear and download immediately and preserves results across navigation', async ({ page }) => {
  await mockController(page);
  await login(page);
  await page.getByTestId('nav-assurance').click();
  const raw = '<img src=x onerror="window.injected=true"> service=test user=operator';
  await page.getByTestId('assurance-raw').fill(raw);
  await page.getByRole('button', { name: 'Run assurance', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Clear manually' })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Download result JSON' })).toBeEnabled();
  await expect(page.locator('#assurance-result')).toContainText('service=test user=operator');
  expect(await page.evaluate(() => window.injected)).toBeUndefined();
  await page.locator('#assurance-save-form [name=name]').fill('browser-retained-case');
  await page.getByRole('button', { name: 'Encrypt and retain in Supabase' }).click();
  await expect(page.getByText('browser-retained-case', { exact: true })).toBeVisible();
  await page.getByTestId('nav-fleet').click();
  await page.getByTestId('nav-assurance').click();
  await expect(page.getByTestId('assurance-raw')).toHaveValue(raw);
  await expect(page.locator('#assurance-result')).toContainText('service=test user=operator');
  const downloadEvent = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download result JSON' }).click();
  expect((await downloadEvent).suggestedFilename()).toBe('nli-v13-assurance-result.json');
  await page.getByRole('button', { name: 'Clear manually' }).click();
  await expect(page.getByText('Run a case to inspect complete output', { exact: false })).toBeVisible();
  await expect(page.getByTestId('assurance-raw')).toHaveValue('');
});

test('Assurance parser and family catalogs drive single and corpus requests', async ({ page }) => {
  const { assuranceRequests, assuranceCorpora } = await mockController(page);
  await login(page);
  await page.getByTestId('nav-assurance').click();
  await expect(page.getByText('Live runtime parsers', { exact: true })).toBeVisible();
  const single = page.locator('#assurance-form');
  await single.locator('[name=service]').selectOption('smtp-in');
  await single.locator('[name=parser_id]').selectOption('smtp');
  await single.locator('[name=raw]').fill('level=INFO user=alice');
  await page.getByRole('button', { name: 'Run assurance', exact: true }).click();
  await expect.poll(() => assuranceRequests.length).toBe(1);
  expect(assuranceRequests[0]).toMatchObject({ service: 'smtp-in', parser_id: 'smtp' });

  const corpus = page.locator('#assurance-corpus-form');
  await corpus.locator('[name=service]').selectOption('imap');
  await corpus.locator('[name=parser_id]').selectOption('smtp');
  await corpus.locator('[name=cases]').fill('[{"raw":"level=INFO user=alice","desired":{}}]');
  await page.getByRole('button', { name: 'Run corpus', exact: true }).click();
  await expect.poll(() => assuranceCorpora.length).toBe(1);
  expect(assuranceCorpora[0].cases[0]).toMatchObject({ service: 'imap', parser_id: 'smtp' });
});

test('Assurance upload defaults to transient and shows protected retention metadata/download', async ({ page }) => {
  const { uploads } = await mockController(page);
  await login(page);
  await page.getByTestId('nav-assurance').click();
  const retain = page.locator('#assurance-upload-form [name=retain]');
  await expect(retain).not.toBeChecked();
  await page.locator('#assurance-upload-form [name=service]').selectOption('smtp-in');
  await page.locator('#assurance-upload-form [name=parser_id]').selectOption('smtp');
  await retain.check();
  await page.locator('#assurance-upload-form [name=file]').setInputFiles({ name: 'sample.log', mimeType: 'text/plain', buffer: Buffer.from('service=example user=operator') });
  await page.getByRole('button', { name: 'Analyze uploaded sample' }).click();
  await expect(page.locator('#assurance-upload-status')).toContainText('encrypted and retained');
  await expect(page.locator('#assurance-upload-status')).toContainText('sample.log');
  await expect(page.locator('#assurance-upload-status')).toContainText('smtp@13.0.0');
  await expect(page.locator('#assurance-upload-status')).toContainText('encrypted_supabase');
  await expect(page.locator('#assurance-upload-status')).toContainText('controller encrypted the fixture');
  expect(uploads).toHaveLength(1);
  expect(uploads[0]).toContain('name="retain"\r\n\r\ntrue');
  expect(uploads[0]).toContain('name="parser_id"\r\n\r\nsmtp');
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download retained sample through controller' }).click();
  expect((await download).suggestedFilename()).toBe('sample.log');
});

test('30-second polling refreshes dashboard but leaves Assurance inputs and results intact', async ({ page }) => {
  await page.clock.install();
  const { observed } = await mockController(page);
  await login(page);
  await page.clock.fastForward(31_000);
  await expect.poll(() => observed.filter(item => item === 'GET /api/v1/dashboard/snapshot').length).toBeGreaterThan(1);
  await page.getByTestId('nav-assurance').click();
  await page.getByTestId('assurance-raw').fill('message=keep-me');
  await page.getByRole('button', { name: 'Run assurance', exact: true }).click();
  await expect(page.locator('#assurance-result')).toContainText('keep-me');
  await page.clock.fastForward(61_000);
  await expect(page.getByTestId('assurance-raw')).toHaveValue('message=keep-me');
  await expect(page.locator('#assurance-result')).toContainText('keep-me');
});

test('viewer permissions apply to newly rendered Assurance actions', async ({ page }) => {
  await mockController(page, { role: 'viewer' });
  await login(page);
  await page.getByTestId('nav-assurance').click();
  await page.getByTestId('assurance-raw').fill('message=viewer-test');
  await page.getByRole('button', { name: 'Run assurance', exact: true }).click();
  await expect(page.getByText('Complete actual output')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Encrypt and retain in Supabase' })).toBeHidden();
});

test('sign-out clears the last operator input and results', async ({ page }) => {
  await mockController(page);
  await login(page);
  await page.getByTestId('nav-assurance').click();
  await page.getByTestId('assurance-raw').fill('user=private-operator');
  await page.getByRole('button', { name: 'Run assurance', exact: true }).click();
  await expect(page.locator('#assurance-result')).toContainText('private-operator');
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByLabel('Password')).toHaveValue('');
  await login(page);
  await page.getByTestId('nav-assurance').click();
  await expect(page.getByTestId('assurance-raw')).toHaveValue('');
});

test('incompatible controller keeps credentials disabled', async ({ page }) => {
  const { observed } = await mockController(page, { healthIdentity: 'v5' });
  await page.goto(origin);
  await expect(page.getByText('Controller connection required')).toBeVisible();
  await expect(page.getByLabel('Password')).toBeDisabled();
  expect(observed.filter(item => item === 'POST /api/v1/auth/login')).toEqual([]);
});

test('input paths workspace saves a revisioned family profile using the dedicated collection', async ({ page }) => {
  const { inputSaves } = await mockController(page);
  await login(page);
  await page.getByTestId('nav-input-paths').click();
  await expect(page.getByRole('heading', { name: 'Input paths and family profiles' })).toBeVisible();
  await page.locator('#input-profile-form [name=quick_path]').fill('/var/log/nli/input/pop-extra.log');
  await page.locator('#input-profile-form [name=quick_family]').selectOption('pop');
  await page.getByRole('button', { name: 'Add path with selected family' }).click();
  await expect(page.locator('#input-profile-form [name=config]')).toHaveValue(/pop-extra\.log/);
  await page.locator('#input-profile-form [name=id]').fill('mail-family-paths');
  await page.locator('#input-profile-form [name=name]').fill('Mail family paths');
  await page.locator('#input-profile-form [name=config]').fill(JSON.stringify({ inputs: [{ path: '/var/log/imap.log', service: 'imap' }, { path: '/var/log/smtp.log', service: 'smtp-in' }] }));
  await page.getByRole('button', { name: 'Save profile revision' }).click();
  await expect.poll(() => inputSaves.length).toBe(1);
  expect(inputSaves[0]).toMatchObject({ id: 'mail-family-paths', config: { inputs: [{ path: '/var/log/imap.log', service: 'imap' }, { path: '/var/log/smtp.log', service: 'smtp-in' }] } });
});

test('input path rollout uses a preserving config patch and explicit node IDs', async ({ page }) => {
  const mock = await mockController(page);
  const profile = { id: 'mail-inputs', revision: 2, state: 'draft', updated_at: '2026-10-14T00:00:00Z', payload: { name: 'Mail inputs', config: { inputs: [{ path: '/var/log/nli/input/imap.log', service: 'imap' }] } } };
  mock.inputProfiles.push(profile);
  await login(page);
  await page.getByTestId('nav-input-paths').click();
  const form = page.locator('#input-rollout-form');
  await form.locator('[name=profile_id]').selectOption('mail-inputs');
  await form.locator('[name=targets]').fill('node-mail-1');
  await expect(form.locator('[name=replace_outputs]')).not.toBeChecked();
  await form.getByRole('button', { name: 'Request independent approval' }).click();
  await expect.poll(() => mock.rolloutSaves.length).toBe(1);
  expect(mock.rolloutSaves[0].payload.config_patch.inputs).toEqual(profile.payload.config.inputs);
  expect(mock.rolloutSaves[0].payload.config_patch.outputs).toBeUndefined();
  expect(mock.rolloutSaves[0].target_nodes).toEqual(['node-mail-1']);
});

test('mobile navigation exposes all workspaces without horizontal page overflow', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockController(page);
  await login(page);
  for (const tab of tabs) {
    await page.getByLabel('Toggle navigation').click();
    await page.getByTestId(`nav-${tab}`).click();
    await expect(page.locator('.loading-state')).toHaveCount(0);
    await expect(page.locator('.error-state')).toHaveCount(0);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
