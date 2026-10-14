/* Single-control probe: click one button on a pristine mount and report. */
import { JSDOM, VirtualConsole } from 'jsdom';

const BASE = (process.env.LOCALLINK_ADMIN_URL || 'http://127.0.0.1:8081').replace(/\/$/, '');
const MODULE = process.env.PROBE_MODULE || 'devices';
const SELECTOR = process.env.PROBE_SELECTOR || '[data-device-revoke]';
const tick = (n = 1) => new Promise((r) => setTimeout(r, 120 * n));

async function main() {
  const login = await fetch(`${BASE}/api/v1/admin/login`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'owner', password: 'locallink-demo-2026' }),
  }).then((r) => r.json());

  const shell = await (await fetch(`${BASE}/admin`)).text();
  const vc = new VirtualConsole();
  const errs = [];
  vc.on('jsdomError', (e) => errs.push(String(e.message || e)));
  vc.on('error', (...a) => errs.push(a.join(' ')));

  const dom = new JSDOM(shell, { url: `${BASE}/admin`, runScripts: 'outside-only', pretendToBeVisual: true, virtualConsole: vc });
  const { window } = dom;
  window.sessionStorage.setItem('locallink_admin_token', login.token);
  window.sessionStorage.setItem('locallink_admin_identity', JSON.stringify(login.admin || {}));
  window.prompt = () => null;
  window.confirm = () => false;
  const requests = [];
  window.fetch = async (path, options = {}) => {
    const url = String(path).startsWith('http') ? path : BASE + path;
    const method = options.method || 'GET';
    requests.push(`${method} ${path}`);
    // Never mutate the demo DB: report success and never touch the server.
    if (method !== 'GET') return { ok: true, status: 200, text: async () => '{}', json: async () => ({}), headers: { get: () => null } };
    const r = await fetch(url, { method, headers: Object.assign({ Authorization: `Bearer ${login.token}` }, options.headers || {}) });
    const t = await r.text();
    return { ok: r.ok, status: r.status, text: async () => t, json: async () => JSON.parse(t), headers: { get: (n) => r.headers.get(n) } };
  };
  for (const a of ['components.js', 'charts.js', 'views.js', 'app.js']) {
    window.eval(await (await fetch(`${BASE}/admin/${a}`)).text());
  }
  await tick(3);

  for (let i = 0; i < 3; i++) {
    await window.LL.app.mountView(MODULE);
    await tick(3);
    const list = Array.from(window.document.getElementById('view').querySelectorAll(SELECTOR));
    const started = errs.length;
    for (const el of list) {
      try {
        el.dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
        await tick(2);
      } catch (err) { errs.push('dispatch: ' + err.message); }
    }
    console.log(`pass ${i + 1}: ${list.length} matches "${SELECTOR}" in ${MODULE}; errors so far: ${errs.length - started}`);
  }
  const modal = window.document.querySelector('.modal');
  console.log(`modal open after clicks: ${!!modal}; requests seen: ${requests.length}`);
  if (errs.length) { console.log('ERRORS:', errs.slice(0, 3)); process.exit(1); }
  console.log('probe clean');
  dom.window.close(); process.exit(0);
}
main().catch((e) => { console.error('probe crashed:', e); process.exit(2); });
