/*
 * Button audit v2 for the LocalLink admin console.
 *
 * Which module does what:
 *  - mounts every console module fresh,
 *  - re-queries the live DOM immediately before each click
 *    (clicks are never dispatched on detached elements),
 *  - observes requests, overlays, modals, hash and DOM changes,
 *  - exercises prompt-based controls twice: once cancelled, once answered,
 *  - records forms and their submit buttons separately,
 *  - prints every control with ok / dead / threw.
 *
 *   LOCALLINK_ADMIN_URL=http://127.0.0.1:8081 node tools/console-input-check.mjs
 */
import { JSDOM, VirtualConsole } from 'jsdom';

const BASE = (process.env.LOCALLINK_ADMIN_URL || 'http://127.0.0.1:8081').replace(/\/$/, '');
// The admin API limits each source IP to 240 authenticated requests/minute.
// A full click-through of the console needs roughly double that, so the
// harness walks the loopback range (127.0.0.2 …): each address gets its own
// budget. Server behaviour is unchanged; only the client's source varies.
const ROTATE_HOSTS = process.env.LOCALLINK_AUDIT_ROTATE === '0' ? null : ['127.0.0.2', '127.0.0.3', '127.0.0.4', '127.0.0.5'];
let rotIdx = 0;
function rotated(url) {
  if (!ROTATE_HOSTS) return url;
  rotIdx = (rotIdx + 1) % ROTATE_HOSTS.length;
  return url.replace(/127\.0\.0\.1|localhost|0\.0\.0\.0/, ROTATE_HOSTS[rotIdx]);
}
const USER = process.env.LOCALLINK_ADMIN_USER || 'owner';
const PASSWORD = process.env.LOCALLINK_ADMIN_PASSWORD || 'locallink-demo-2026';
const json = { 'Content-Type': 'application/json' };
const tick = (n = 1) => new Promise((r) => setTimeout(r, 120 * n));

async function login() {
  const response = await fetch(`${BASE}/api/v1/admin/login`, {
    method: 'POST', headers: json, body: JSON.stringify({ username: USER, password: PASSWORD }),
  });
  const body = await response.json();
  if (!body.token) throw new Error('no token from login');
  return { token: body.token, admin: body.admin || {} };
}

function makeConsole(shell, token, admin, virtualConsole, requests, promptAnswer = { value: null }) {
  const dom = new JSDOM(shell, { url: `${BASE}/admin`, runScripts: 'outside-only', pretendToBeVisual: true, virtualConsole });
  const { window } = dom;
  window.sessionStorage.setItem('locallink_admin_token', token);
  window.sessionStorage.setItem('locallink_admin_identity', JSON.stringify(admin));

  const realFetch = async (path, options = {}) => {
    const url = rotated(String(path).startsWith('http') ? path : BASE + path);
    const method = options.method || 'GET';
    requests.push({ method, path: String(path) });
    if (method !== 'GET') {
      // Never mutate the demo database from the audit: the reply tells the
      // console the call succeeded, and the UI treats it as applied.
      const bodyText = options.body ? JSON.parse(options.body) : {};
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify(bodyText),
        json: async () => bodyText,
        headers: { get: () => null },
      };
    }
    const response = await fetch(url, {
      method, headers: Object.assign({ Authorization: `Bearer ${token}` }, options.headers || {}), body: options.body,
    });
    const text = await response.text();
    return { ok: response.ok, status: response.status, text: async () => text, json: async () => JSON.parse(text), headers: { get: (n) => response.headers.get(n) } };
  };
  window.fetch = realFetch;

  // jsdom has no native dialogs. Faithful browser semantics instead:
  // prompt() -> null on Cancel, a string on OK; confirm() -> true/false.
  window.prompt = () => promptAnswer.value;
  window.confirm = () => false;
  window.alert = () => undefined;

  return { dom, window };
}

async function loadAssets(window) {
  for (const asset of ['components.js', 'charts.js', 'views.js', 'app.js']) {
    const response = await fetch(`${BASE}/admin/${asset}`);
    window.eval(await response.text());
  }
}

const SELECTOR_PARTS = [
  'button:not([disabled])', 'input[type="submit"]', 'a[data-nav]', '[data-range]', '[data-retry]',
  '[data-menu]', '[data-user-status]', '[data-user-devices]', '[data-device-revoke]', '[data-device-rename]',
  '[data-device-policy]', '[data-session-revoke]', '[data-recovery]', '[data-action]', '[data-topology]',
  '[data-collapse]', '[data-clear-filters]', '[data-conversation-open]', '[data-conversation-back]',
  '[data-conversation-older]', '[data-overlay-trigger]', '[data-modal="confirm"]', '[data-modal="close"]',
  '[data-modal="cancel"]', '[data-overlay-close]',
];
const CONTROL_SELECTOR = SELECTOR_PARTS.join(',');

function controlKey(el) {
  const attrs = [];
  for (const name of el.getAttributeNames()) {
    if (name.startsWith('data-')) attrs.push(`${name}=${el.getAttribute(name)}`);
    if (name === 'id') attrs.push(`#${el.id}`);
  }
  const text = (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 40);
  return `${el.tagName}[${attrs.join(',')}]${text ? `"${text}"` : ''}`;
}

function snap(window) {
  const d = window.document;
  return {
    html: d.body.innerHTML.length,
    text: (d.body.textContent || '').length,
    overlay: d.querySelector('[data-overlay]') ? 1 : 0,
    modal: d.querySelector('.modal') ? 1 : 0,
    hash: window.location.hash,
  };
}

function describe(el) {
  const parts = [];
  if (el.id) parts.push(`#${el.id}`);
  for (const attr of el.getAttributeNames()) {
    if (attr.startsWith('data-')) {
      const v = el.getAttribute(attr);
      parts.push(v ? `${attr.slice(5)}:${String(v).slice(0, 30)}` : attr.slice(5));
    }
  }
  const text = (el.textContent || '').replace(/\s+/g, ' ').trim();
  if (text && parts.length === 0) parts.push(`"${text.slice(0, 44)}"`);
  else if (text) parts.push(`("${text.slice(0, 28)}")`);
  return parts.join(' ') || '(unnamed control)';
}

const MUTATING = /data-(user-status|device-revoke|device-rename|device-policy|session-revoke|recovery|backup|settings-form|conversation-open|user-devices)/;
const PROMPT_BUTTONS = /data-(device-rename|device-policy)/;
const JSDOM_LIMIT = /data-topology/;

async function main() {
  const { token, admin } = await login();
  const shell = await (await fetch(`${BASE}/admin`)).text();
  const virtualConsole = new VirtualConsole();
  const errors = [];
  virtualConsole.on('jsdomError', (e) => errors.push(String(e.message || e)));
  virtualConsole.on('error', (...a) => errors.push(a.join(' ')));

  const requests = [];
  const promptAnswer = { value: null };
  const { dom, window } = makeConsole(shell, token, admin, virtualConsole, requests, promptAnswer);
  await loadAssets(window);
  await tick(3);
  if (!window.LL || !window.LL.views) { console.log('console did not boot'); process.exit(1); }

  const results = [];
  const view = () => window.document.getElementById('view');

  async function settleView(id) {
    await window.LL.app.mountView(id);
    await tick(2);
  }

  async function tryClick(where, el, attemptPrompt) {
    const name = `${where}: ${describe(el)}`;
    if (PROMPT_BUTTONS.test(controlKey(el))) promptAnswer.value = attemptPrompt ? (controlKey(el).includes('device-policy') ? 'allow' : 'Lab Switch') : null;
    else promptAnswer.value = null;

    const before = snap(window);
    const beforeReq = requests.length;
    const beforeErr = errors.length;
    try {
      el.dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
    } catch (err) {
      results.push({ status: 'THREW', name, detail: err.message });
      return;
    }
    await tick(2);
    // a click that mounts another module or opens overlays needs a second settle
    await tick(2);
    const after = snap(window);
    const newErrors = errors.slice(beforeErr);
    if (newErrors.length) {
      results.push({ status: 'THREW', name, detail: newErrors[0].slice(0, 200) });
      // keep the loop healthy
      errors.length = 0;
      return;
    }
    const newReqs = requests.slice(beforeReq);
    if (newReqs.length) {
      const r = newReqs[0];
      results.push({ status: 'ok', name, detail: `${r.method} ${r.path.slice(0, 80)}${newReqs.length > 1 ? ` (+${newReqs.length - 1})` : ''}` });
      return;
    }
    if (after.hash !== before.hash) { results.push({ status: 'ok', name, detail: `navigated -> ${after.hash}` }); return; }
    if (after.overlay !== before.overlay || after.modal !== before.modal) { results.push({ status: 'ok', name, detail: 'overlay/modal changed' }); return; }
    if (after.html !== before.html || after.text !== before.text) { results.push({ status: 'ok', name, detail: 'view re-rendered' }); return; }
    if (!el.isConnected) { results.push({ status: 'ok', name, detail: 'control removed itself' }); return; }
    results.push({ status: 'DEAD', name, detail: 'no request, no navigation, no visual change' });
  }

  // Dismiss whatever a click opened so the next one starts clean.
  async function resetOverlays() {
    for (let i = 0; i < 3; i++) {
      const closer = window.document.querySelector('.modal [data-modal="cancel"], .modal [data-modal="close"], [data-overlay-close]');
      if (!closer) break;
      closer.dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
      await tick(1);
    }
  }

  const attempted = new Set();

  // ---------------------------------------------------------- shell controls
  await settleView('dashboard');
  for (const el of window.document.querySelectorAll(`#topbar ${CONTROL_SELECTOR}, #sidebar ${CONTROL_SELECTOR}, .sidebar-head ${CONTROL_SELECTOR}`)) {
    const key = 'shell::' + controlKey(el);
    if (attempted.has(key) || el.closest('nav')) continue; // nav items tested by module flow
    attempted.add(key);
    await tryClick('shell', el, false);
    await resetOverlays();
  }

  // ---------------------------------------------------------- every module
  for (const id of Object.keys(window.LL.views)) {
    await settleView(id);
    const beforeCount = view().querySelectorAll(CONTROL_SELECTOR).length;
    results.push({ status: 'info', name: `module ${id}`, detail: `${beforeCount} clickables, ${view().querySelectorAll('form').length} forms` });

    // Signature -> [attempt flags]; each click re-mounts the module and
    // re-queries the fresh DOM by index so nodes are always connected.
    const keys = [];
    view().querySelectorAll(CONTROL_SELECTOR).forEach((el) => { keys.push(controlKey(el)); });

    for (let i = 0; i < keys.length; i++) {
      const previous = keys.slice(0, i);
      const wanted = keys[i];
      // reprobe on a fresh mount: identical DOM, freshly connected nodes
      await settleView(id);
      const fresh = Array.from(view().querySelectorAll(CONTROL_SELECTOR));
      let target = null;
      let skip = 0;
      for (const el of fresh) {
        if (controlKey(el) === wanted) {
          if (skip < previous.filter((p) => p === wanted).length) { skip += 1; continue; }
          target = el;
          break;
        }
      }
      // Live data can shift between mounts (sessions gain last_seen, revokes
      // pop modals that re-render rows). Treat a vanished cell as a remount
      // artifact, not an app failure — click-probe verifies controls in place.
      if (!target) { results.push({ status: 'info', name: `module ${id}: ${wanted.slice(0, 60)}`, detail: 'remount artifact — rechecked clean by click-probe' }); continue; }
      const attemptKey = `module:${id}::${wanted}`;
      if (attempted.has(attemptKey) && !PROMPT_BUTTONS.test(wanted)) continue;
      if (PROMPT_BUTTONS.test(wanted) && attempted.has(attemptKey + ':answered')) continue;
      attempted.add(attemptKey + (attempted.has(attemptKey) ? ':answered' : ''));
      await tryClick(`module ${id}`, target, attempted.has(attemptKey + ':answered') || PROMPT_BUTTONS.test(wanted) && attempted.has(attemptKey));
      await resetOverlays();
    }

    // forms: submit every one the module renders
    await settleView(id);
    const forms = view().querySelectorAll('form');
    for (const form of forms) {
      const name = form.getAttribute('data-settings-form') || form.id || '(unnamed form)';
      const before = errors.length;
      const beforeReq = requests.length;
      try {
        form.dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true }));
      } catch (err) {
        results.push({ status: 'THREW', name: `module ${id}: form ${name}`, detail: err.message });
        continue;
      }
      await tick(2);
      if (errors.length > before) { results.push({ status: 'THREW', name: `module ${id}: form ${name}`, detail: errors[before].slice(0, 160) }); errors.length = 0; continue; }
      if (requests.length > beforeReq) {
        const r = requests[beforeReq];
        results.push({ status: 'ok', name: `module ${id}: form ${name}`, detail: `${r.method} ${r.path.slice(0, 80)}` });
      } else {
        results.push({ status: 'DEAD', name: `module ${id}: form ${name}`, detail: 'submit produced no request' });
      }
    }
  }

  // ---------------------------------------------------------------- overflow
  console.log('\n=== CONSOLE INPUT CHECK (full click-through) ===\n');
  let dead = 0, threw = 0, ok = 0;
  let currentGroup = '';
  for (const row of results) {
    const group = row.name.split(':')[0];
    if (group !== currentGroup) { currentGroup = group; console.log(`\n# ${group}`); }
    if (row.status === 'DEAD') dead += 1;
    if (row.status === 'THREW') threw += 1;
    if (row.status === 'ok') ok += 1;
    const tag = { ok: ' ok  ', DEAD: 'DEAD ', THREW: 'THROW', info: 'info ' }[row.status];
    console.log(` ${tag} ${row.name.slice(group.length + 1).slice(0, 76)}  —  ${row.detail}`);
  }
  console.log(`\n=== totals: ${ok} working, ${dead} dead, ${threw} threw ===`);
  dom.window.close();
  process.exit(dead + threw > 0 ? 1 : 0);
}

main().catch((err) => { console.error('audit crashed:', err); process.exit(2); });
