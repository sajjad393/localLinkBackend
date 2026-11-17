// Bundle 322 (W4): runs the REAL App version mount code (adminweb/views.js)
// in node with six fake server answers and checks the words the owner sees.
// Regression: the 321 form (Save rule / Remove rule / very old) stays.
const fs = require('fs');
const path = process.argv[2] || 'internal/httpapi/adminweb/views.js';
const src = fs.readFileSync(path, 'utf8');

// --- a tiny DOM: only what the view touches (innerHTML + querySelector) ----
function el(tag) {
  return {
    tagName: (tag || 'div').toUpperCase(),
    _html: '',
    get innerHTML() { return this._html; },
    set innerHTML(v) { this._html = String(v); },
    textContent: '',
    classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
    style: {},
    getAttribute() { return null; },
    setAttribute() {},
    addEventListener() {},
    querySelector() { return null; },
    querySelectorAll() { return []; },
    appendChild() {},
  };
}
global.document = {
  documentElement: { dataset: {} },
  body: el('body'),
  head: el('head'),
  createElement: el,
  getElementById: () => el('div'),
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};
global.window = global;
global.localStorage = { getItem: () => null, setItem() {}, removeItem() {} };
global.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
global.requestAnimationFrame = (f) => setTimeout(f, 0);
global.L = { t: {} };

// --- load the real modules ------------------------------------------------
eval(fs.readFileSync(path.replace('views.js', 'components.js'), 'utf8'));
eval(src);
if (!LL.views || !LL.views.appversion) throw new Error('appversion view missing');

// --- fake server answers -------------------------------------------------
function up(b, v, opts) {
  const o = opts || {};
  return { id: b, version: v, build: b, file_name: 'LinkUp-' + v + '.apk', size: 18_400_000,
    sha256: 'a'.repeat(64), added_at: '2026-11-16T11:20:00Z', added_by: 'sajjad',
    downloads: o.dl || 4, current: !!o.newest, is_rule: !!o.rule, on_disk: o.disk !== false,
    phones_here: o.phones || 3, newest: !!o.newest };
}
const rule = (b, v, mode) => ({ min_build: b, min_version: v, mode: mode || 'warn',
  stop_at: '2026-11-20T11:20:00Z', very_old: false, seq: 9, at: '2026-11-17T09:00:00Z',
  by: 'sajjad', signer: 'server' });

const CASES = {
  newest_behind_rule: {
    rule: rule(183, '3.0.112'),
    uploads: [up(185, '3.0.114', { newest: true, phones: 3 }), up(184, '3.0.113'), up(183, '3.0.112', { rule: true })],
    builds: [{ build: 185, devices: 3, last_seen: '2026-11-17T07:40:00Z' }],
    want: ['Use this build', 'Pick another upload', 'the rule is still build 183', 'value="3.0.112"', 'value="183"', 'readonly'],
  },
  rule_is_newest: {
    rule: rule(185, '3.0.114'),
    uploads: [up(185, '3.0.114', { newest: true, rule: true })],
    builds: [{ build: 185, devices: 9, last_seen: '2026-11-17T07:40:00Z' }],
    want: ['The rule is the newest upload', 'value="3.0.114"', 'readonly'],
    notWant: ['Pick another upload'],
  },
  no_upload: {
    rule: null, uploads: [], builds: [],
    want: ['No APK on this server yet', 'Type it by hand', 'Save rule', 'Remove rule'],
    notWant: ['value="3.0.112" readonly', 'Use this build'],
  },
  rule_typed_by_hand: {
    rule: rule(150, '3.0.90'), uploads: [], builds: [{ build: 150, devices: 2, last_seen: '2026-11-17T07:40:00Z' }],
    want: ['No APK on this server yet', 'value="3.0.90"', 'value="150"'],
    notWant: ['value="3.0.90" readonly'],
  },
  file_missing: {
    rule: rule(184, '3.0.113'),
    uploads: [up(184, '3.0.113', { newest: true, rule: true, disk: false })],
    builds: [],
    want: ['file missing'],
  },
  empty_uploads_key: { // an old app build of the API without the new keys
    rule: rule(183, '3.0.112'), uploads: undefined, builds: [],
    want: ['No APK on this server yet', 'value="3.0.112"'],
  },
};

(async () => {
  const report = [];
  for (const [name, c] of Object.entries(CASES)) {
    const host = el('div');
    const data = { rule: c.rule, builds: c.builds };
    if (c.uploads !== undefined) {
      data.uploads = c.uploads;
      data.newest_upload = c.uploads.length ? c.uploads[0] : null;
    }
    await LL.views.appversion.mount(host, { api: async () => data });
    const html = host.innerHTML;
    for (const w of c.want) if (html.indexOf(w) < 0) report.push(`${name}: missing ${JSON.stringify(w)}`);
    for (const w of c.notWant || []) if (html.indexOf(w) >= 0) report.push(`${name}: must not contain ${JSON.stringify(w)}`);
    // the 321 form pieces stay on every state
    for (const w of ['data-version-form', 'name="min_version"', 'name="min_build"', 'data-version-remove', 'name="very_old"']) {
      if (html.indexOf(w) < 0) report.push(`${name}: 321 form piece ${w} lost`);
    }
  }
  if (report.length) {
    console.log('SMOKE_FAIL\n' + report.join('\n'));
    process.exit(1);
  }
  console.log('SMOKE_OK 322 appversion: ' + Object.keys(CASES).length + ' server states');
})();
