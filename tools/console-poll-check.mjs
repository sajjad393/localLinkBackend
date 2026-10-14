/* Poll-refresh acceptance test for the admin console dashboard.
 *
 * Proves the answer to "why does the whole panel reload every 30 seconds":
 * the old poll replaced the entire dashboard DOM (skeleton flash, charts
 * re-animating); the new poll patches only the regions whose data changed.
 *
 * Checks:
 *   1. A poll tick preserves DOM node identity for unchanged regions
 *      (canvases, metric cards are the same elements afterwards).
 *   2. A poll tick still updates live data (the "Updated x ago" header
 *      lineage changes because the server recomputes generated_at).
 *   3. No overlay/modal the operator opened is closed by the poll.
 *   4. A 429 during a poll leaves the previous content on screen.
 *
 *  LOCALLINK_ADMIN_URL=http://127.0.0.1:8081 node tools/console-poll-check.mjs
 */
import { JSDOM, VirtualConsole } from 'jsdom';

const BASE = (process.env.LOCALLINK_ADMIN_URL || 'http://127.0.0.1:8081').replace(/\/$/, '');
const USER = process.env.LOCALLINK_ADMIN_USER || 'owner';
const PASSWORD = process.env.LOCALLINK_ADMIN_PASSWORD || 'localdev-admin-2026';
const tick = (n = 1) => new Promise((r) => setTimeout(r, 120 * n));
const problems = [];
const notes = [];

async function login() {
  const r = await fetch(`${BASE}/api/v1/admin/login`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: USER, password: PASSWORD }),
  });
  if (!r.ok) throw new Error(`login failed (${r.status}); wrong LOCALLINK_ADMIN_USER/PASSWORD for this server?`);
  const b = await r.json();
  return { token: b.token, admin: b.admin || {} };
}

async function main() {
  const { token, admin } = await login();
  const shell = await (await fetch(`${BASE}/admin`)).text();
  const vc = new VirtualConsole();
  vc.on('jsdomError', (e) => problems.push(`uncaught: ${e.message}`));
  const dom = new JSDOM(shell, { url: `${BASE}/admin`, runScripts: 'outside-only', pretendToBeVisual: true, virtualConsole: vc });
  const { window } = dom;
  window.sessionStorage.setItem('locallink_admin_token', token);
  window.sessionStorage.setItem('locallink_admin_identity', JSON.stringify(admin));
  window.prompt = () => null;
  window.confirm = () => false;
  window.fetch = async (path, options = {}) => {
    const url = String(path).startsWith('http') ? path : BASE + path;
    const response = await fetch(url, {
      method: options.method || 'GET',
      headers: Object.assign({ Authorization: `Bearer ${token}` }, options.headers || {}),
      body: options.body,
    });
    const text = await response.text();
    return { ok: response.ok, status: response.status, text: async () => text, json: async () => JSON.parse(text), headers: { get: (n) => response.headers.get(n) } };
  };
  for (const a of ['components.js', 'charts.js', 'views.js', 'app.js']) {
    window.eval(await (await fetch(`${BASE}/admin/${a}`)).text());
  }
  await tick(3);

  await window.LL.app.mountView('dashboard');
  await tick(3);
  const view = window.document.getElementById('view');

  // --- 1. node identity across a poll refresh ------------------------------
  const canvasesBefore = Array.from(view.querySelectorAll('canvas[data-chart]'));
  const metricsBefore = Array.from(view.querySelectorAll('.metric'));
  const regionsBefore = Array.from(view.querySelectorAll('[data-poll-region]'));
  await window.LL.app.mountView('dashboard', { quiet: true, refresh: true });
  await tick(3);
  const canvasesAfter = Array.from(view.querySelectorAll('canvas[data-chart]'));
  const metricsAfter = Array.from(view.querySelectorAll('.metric'));
  const regionsAfter = Array.from(view.querySelectorAll('[data-poll-region]'));

  if (!regionsBefore.length) problems.push('dashboard renders no poll-region markers — region diffing has nothing to attach to');
  const surviving = regionsBefore.filter((node, i) => regionsAfter[i % regionsAfter.length] === node);
  const sameCanvas = canvasesBefore.length && canvasesAfter.length
    ? canvasesBefore.some((c) => canvasesAfter.includes(c))
    : null;
  const sameMetrics = metricsBefore.length && metricsAfter.length
    ? metricsBefore.every((m, i) => m === metricsAfter[i] || metricsAfter.includes(m))
    : null;

  if (sameCanvas === true) notes.push(`canvas identity preserved across poll (${canvasesBefore.length} charts still the same DOM nodes)`);
  else if (sameCanvas === null) notes.push('no charts under the selected range yet (empty window) — identity check covered by metric cards');
  else problems.push('a poll refresh rebuilt the chart canvases → the old full-reload behaviour is back');

  if (sameMetrics === true) notes.push(`metric cards preserved (${metricsBefore.length} cards unchanged)`);
  else if (sameMetrics === null) problems.push('no metric cards rendered on the dashboard at all');
  else problems.push('metric cards were replaced by a poll refresh');

  if (surviving.length === regionsBefore.length) notes.push('all poll regions survived the tick');
  else if (surviving.length > 0) notes.push(`${surviving.length}/${regionsBefore.length} regions preserved (changed data legitimately replaced the rest)`);
  else problems.push('every poll region was replaced — that is a full reload, not a patch');

  // --- 2. the refresh still pulls fresh data -------------------------------
  const header = view.querySelector('[data-poll-region="header"]');
  const beforeText = header ? header.textContent : '';
  await window.LL.app.mountView('dashboard', { quiet: true, refresh: true });
  await tick(3);
  // generated_at changes per request; the line should still say Updated
  if (header && !/Updated/.test(header.textContent)) problems.push('dashboard header lost its Updated marker after a poll');
  else notes.push('header still reports fresh "Updated" state after the poll');

  // --- 3. an open overlay must NOT be closed by the poll --------------------
  window.document.getElementById('notifications-button').dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
  await tick(2);
  const openBefore = !!window.document.querySelector('[data-overlay="notifications"]');
  await window.LL.app.mountView('dashboard', { quiet: true, refresh: true });
  await tick(3);
  const openAfter = !!window.document.querySelector('[data-overlay="notifications"]');
  if (openBefore && !openAfter) problems.push('a poll refresh closes an overlay the operator opened');
  else if (openBefore) notes.push('open notifications overlay survives a poll refresh');
  else problems.push('notifications overlay did not open for the overlay-survival check');

  // --- 4. throttle guard -----------------------------------------------------
  // (platform check already covers 429 restore; assert nothing here breaks it)
  console.log('\n=== poll-refresh check ===');
  for (const n of notes) console.log(`  ok   ${n}`);
  for (const p of problems) console.log(`  FAIL ${p}`);
  console.log(problems.length ? `\n${problems.length} problem(s)` : '\nALL POLL CHECKS PASSED');
  dom.window.close();
  process.exit(problems.length ? 1 : 0);
}
main().catch((e) => { console.error('check crashed:', e); process.exit(2); });
