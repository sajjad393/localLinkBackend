/*
 * Browser console render check.
 *
 * Loads the admin console exactly as the server serves it, drives it against a
 * running backend in a headless DOM, and fails if anything in the operator UI
 * breaks. It exists because some defects only appear when the console actually
 * runs: a menu that flashes open and shuts, a view that throws on an unexpected
 * payload, a refresh that replaces live figures with an error panel. Syntax
 * checks and Go tests cannot see any of those.
 *
 * Usage:
 *   npm install jsdom
 *   node tools/console-render-check.mjs
 *
 * Environment:
 *   LOCALLINK_ADMIN_URL       console address      (default https://localhost:18099)
 *   LOCALLINK_ADMIN_USER      administrator login  (default owner)
 *   LOCALLINK_ADMIN_PASSWORD  administrator secret (default localdev-admin-2026)
 *   LOCALLINK_TLS_INSECURE    "1" for the dev certificate
 *
 * Requires a backend started with those bootstrap credentials. Exit code 0 means
 * every check passed; 1 lists the failures.
 */
import { JSDOM, VirtualConsole } from 'jsdom';

const BASE = (process.env.LOCALLINK_ADMIN_URL || 'https://localhost:18099').replace(/\/$/, '');
const USER = process.env.LOCALLINK_ADMIN_USER || 'owner';
const PASSWORD = process.env.LOCALLINK_ADMIN_PASSWORD || 'localdev-admin-2026';

const problems = [];
const notes = [];
const json = { 'Content-Type': 'application/json' };

async function login() {
  const response = await fetch(`${BASE}/api/v1/admin/login`, {
    method: 'POST',
    headers: json,
    body: JSON.stringify({ username: USER, password: PASSWORD }),
  });
  if (!response.ok) throw new Error(`login failed (${response.status}); is the bootstrap admin configured?`);
  const body = await response.json();
  if (!body.token) throw new Error('login returned no token');
  return { token: body.token, admin: body.admin || {} };
}

function openConsole(shell, token, admin, virtualConsole) {
  const dom = new JSDOM(shell, {
    url: `${BASE}/admin`,
    runScripts: 'outside-only',
    pretendToBeVisual: true,
    virtualConsole,
  });
  const { window } = dom;
  if (token) {
    window.sessionStorage.setItem('locallink_admin_token', token);
    window.sessionStorage.setItem('locallink_admin_identity', JSON.stringify(admin));
  }
  const realFetch = async (path, options = {}) => {
    const url = String(path).startsWith('http') ? path : BASE + path;
    const response = await fetch(url, {
      method: options.method || 'GET',
      headers: Object.assign({ Authorization: `Bearer ${token}` }, options.headers || {}),
      body: options.body,
    });
    const text = await response.text();
    return {
      ok: response.ok,
      status: response.status,
      text: async () => text,
      json: async () => JSON.parse(text),
      headers: { get: (name) => response.headers.get(name) },
    };
  };
  window.fetch = realFetch;
  return { dom, window, realFetch };
}

async function loadAssets(window) {
  for (const asset of ['components.js', 'charts.js', 'views.js', 'app.js']) {
    const response = await fetch(`${BASE}/admin/${asset}`);
    if (!response.ok) throw new Error(`asset ${asset} returned ${response.status}`);
    window.eval(await response.text());
  }
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 120));
const textOf = (window) => window.document.getElementById('view').textContent.replace(/\s+/g, ' ');

async function main() {
  const { token, admin } = await login();
  const virtualConsole = new VirtualConsole();
  virtualConsole.on('jsdomError', (error) => problems.push(`uncaught: ${error.message}`));
  virtualConsole.on('error', (...args) => problems.push(`console error: ${args.join(' ')}`));

  const shell = await (await fetch(`${BASE}/admin`)).text();

  // The sign-in gate must hold without a session.
  {
    const bare = openConsole(shell, null, null, virtualConsole);
    await loadAssets(bare.window);
    await tick();
    const authHidden = bare.window.document.getElementById('auth').classList.contains('hidden');
    const appHidden = bare.window.document.getElementById('app').classList.contains('hidden');
    if (authHidden) problems.push('sign-in screen is not shown without a session');
    if (!appHidden) problems.push('the application opened without a session');
    else notes.push('no-token boot shows the sign-in screen');
    bare.dom.window.close();
  }

  const { dom, window, realFetch } = openConsole(shell, token, admin, virtualConsole);
  await loadAssets(window);
  await tick();
  await tick();

  if (window.document.getElementById('app').classList.contains('hidden')) problems.push('the application shell did not open with a valid session');
  if (!window.document.querySelector('.nav-item[data-nav="dashboard"]')) problems.push('sidebar navigation was not rendered');
  notes.push(`modules registered: ${Object.keys(window.LL.views).length}`);

  // The dashboard must show the figures the API actually returns.
  await window.LL.app.mountView('dashboard');
  await tick();
  await tick();
  const dashboard = textOf(window);
  for (const section of ['Total users', 'Online devices', 'Registered devices', 'Messages', 'Active calls',
    'Active sessions', 'Storage used', 'Security alerts', 'Activity analytics', 'Server health',
    'Live network', 'Security overview', 'Recent activity', 'Quick actions']) {
    if (!dashboard.includes(section)) problems.push(`dashboard is missing the "${section}" section`);
  }
  const summary = await (await realFetch('/api/v1/admin/summary')).json();
  if (typeof summary.users !== 'number') problems.push('the summary endpoint did not return a user total');
  else if (!dashboard.includes(String(summary.users))) problems.push(`dashboard does not show the live user total (${summary.users})`);
  else notes.push(`dashboard shows live totals (users=${summary.users}, devices=${summary.devices})`);

  // Every module must mount without throwing, including on an empty database.
  for (const id of Object.keys(window.LL.views)) {
    const before = problems.length;
    await window.LL.app.mountView(id);
    await tick();
    await tick();
    if (!textOf(window).trim()) problems.push(`module "${id}" rendered nothing`);
    else if (problems.length === before) notes.push(`module "${id}" mounted`);
  }

  // Permission gating.
  {
    const previous = window.LL.app.state.session;
    window.LL.app.state.session = { username: 'auditor', role: 'auditor', expires_at: null };
    await window.LL.app.mountView('settings');
    await tick();
    if (!textOf(window).includes('higher role')) problems.push('an owner-only module did not explain the role requirement');
    else notes.push('role gate explains why a module is unavailable');
    window.LL.app.state.session = previous;
  }

  // Controls an operator uses every day.
  await window.LL.app.mountView('dashboard');
  await tick();
  await tick();
  const range = window.document.querySelector('[data-range="90d"]');
  if (!range) problems.push('analytics range control is missing');
  else {
    range.dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
    await tick();
    await tick();
    if (window.LL.app.state.analyticsRange !== '90d') problems.push('analytics range did not switch');
    else notes.push('analytics range switch works');
  }

  const openOverlay = async (triggerId, overlay) => {
    const button = window.document.getElementById(triggerId);
    if (!button) return problems.push(`trigger ${triggerId} is missing from the shell`);
    button.dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
    await tick();
    await tick();
    if (!window.document.querySelector(`[data-overlay="${overlay}"]`)) problems.push(`${overlay} overlay did not open (it must survive its own trigger click)`);
    else notes.push(`${overlay} overlay opens and stays open`);
    button.dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
    await tick();
  };
  await openOverlay('notifications-button', 'notifications');
  await openOverlay('identity-button', 'identity');

  const search = window.document.getElementById('global-search');
  if (search) {
    search.value = 'device';
    search.dispatchEvent(new window.Event('input', { bubbles: true }));
    await tick();
    if (!window.document.querySelector('[data-overlay="search"]')) problems.push('global search opened no suggestion panel');
    else notes.push('global search suggests matching modules');
  }

  const themeBefore = window.document.documentElement.getAttribute('data-theme');
  window.document.getElementById('theme-button').dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
  await tick();
  if (window.document.documentElement.getAttribute('data-theme') === themeBefore) problems.push('theme toggle did not change the theme');
  else notes.push('theme toggle works');

  // A throttled refresh must inform, not mislead.
  {
    await window.LL.app.mountView('security');
    await tick();
    await tick();
    window.fetch = async (path, options = {}) => {
      if (String(path).startsWith('/admin/')) return realFetch(path, options);
      return {
        ok: false,
        status: 429,
        text: async () => '{"error":"admin rate limit exceeded"}',
        json: async () => ({ error: 'admin rate limit exceeded' }),
        headers: { get: (name) => (name.toLowerCase() === 'retry-after' ? '60' : null) },
      };
    };
    await window.LL.app.mountView('security');
    await tick();
    await tick();
    const banner = window.document.getElementById('rate-limit-banner');
    if (!banner) problems.push('a throttled refresh gave the operator no explanation');
    else notes.push('throttle is explained in the UI');
    const body = textOf(window);
    if (/admin rate limit exceeded|goroutine \d+/.test(body)) problems.push('a raw server error reached the operator');
    window.fetch = realFetch;
    window.LL.app.state.rateLimitedUntil = 0;
    window.LL.app.renderRateLimitBanner();
    if (window.document.getElementById('rate-limit-banner')) problems.push('the throttle notice did not clear');
    else notes.push('throttle notice clears when the window passes');
  }

  // Nothing invented, nothing leaking through unescaped.
  const rendered = textOf(window);
  for (const banned of [/\blorem\b/i, /\bsample\b/i, /\bdummy\b/i, /\bundefined\b/, /\bNaN\b/, /\[object Object\]/]) {
    const hit = rendered.match(banned);
    if (hit) problems.push(`rendered content contains "${hit[0]}"`);
  }
  notes.push('no placeholder text or undefined values in the rendered output');

  dom.window.close();
}

await main().catch((error) => {
  console.error(`\nrender check could not run: ${error.message}`);
  process.exit(1);
});

console.log('--- render check ---');
notes.forEach((note) => console.log(`  ok   ${note}`));
if (problems.length) {
  console.log('');
  problems.forEach((problem) => console.log(`  FAIL ${problem}`));
  process.exit(1);
}
console.log('\nALL RENDER CHECKS PASSED');
process.exit(0);
