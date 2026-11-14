// Bundle 307 (TODO_307 Part D): runs the real web step code (views.js step307)
// in node: Sent -> 1 of 2 phones -> 12 of 40 parts -> Ready / No messages.
const fs = require('fs');
const src = fs.readFileSync(process.argv[2] || 'internal/httpapi/adminweb/views.js', 'utf8');
function grab(name) {
  const i = src.indexOf('  function ' + name + '(');
  if (i < 0) throw new Error('missing ' + name);
  let depth = 0; const j = src.indexOf('{', i);
  for (let k = j; k < src.length; k++) { if (src[k] === '{') depth++; else if (src[k] === '}') { depth--; if (depth === 0) return src.slice(i, k + 1); } }
  throw new Error('unclosed ' + name);
}
const pal = src.match(/const PAL307 = \[[^\]]+\];/)[0];
const names = ['num', 'step307', 'color307'];
const m = new Function(pal + '\n' + names.map(grab).join('\n') + '; return { ' + names.join(', ') + ' };')();
let fails = 0;
const ok = (c, msg) => { if (!c) { fails++; console.log('FAIL', msg); } else console.log('ok  ', msg); };
const s = (q) => m.step307(Object.assign({ action: 'get_chat', state: 'pending', parts: 0, of: 0, phones: 2, reached: 0 }, q));

let x = s({});
ok(x.k === 'sent' && x.chip === 'Sent' && x.p < 33 && x.live, 'sent: chip Sent, before stop 1, live');
ok(x.big.indexOf('to 2 phones') > 0, 'sent: "Sent to 2 phones"');
x = s({ old_app: 1 });
ok(x.sub.indexOf('must update') >= 0, 'sent: old app says it must update');
x = s({ reached: 1 });
ok(x.k === 'reached' && x.chip === '1 of 2 phones' && Math.abs(x.p - 33.333) < 0.01, 'reached: 1 of 2 phones at stop 1');
x = s({ reached: 5 });
ok(x.chip === '2 of 2 phones', 'reached never above phones');
x = s({ reached: 2, parts: 12, of: 40, from_name: 'ayesha' });
ok(x.k === 'coming' && x.chip === '12 of 40' && x.ring === '30%', 'coming: 12 of 40, ring 30%');
ok(x.p > 66.6 && x.p < 100 && x.sub === "Answer coming from ayesha's phone", 'coming: between stop 2 and the end, from ayesha');
ok(x.big.indexOf('data-n307="12"') > 0, 'coming: the number can count up');
x = s({ parts: 26, of: 26 });
ok(x.k === 'ready' && x.p === 100 && x.chip === 'Ready' && x.live, 'ready: 26 of 26 = Ready, still live until opened');
x = s({ parts: 26, of: 26, opened: true });
ok(x.k === 'ready' && !x.live, 'ready + opened: leaves the live row');
x = s({ state: 'cancelled', empty: true });
ok(x.k === 'empty' && x.chip === 'No messages' && !x.live, 'empty: No messages, not live');
x = s({ state: 'cancelled' });
ok(x.k === 'cancel' && !x.live, 'cancelled: not live');
ok(m.color307('kiran') === m.color307('kiran') && /^#[0-9A-F]{6}$/.test(m.color307('ayesha')), 'avatar colour is stable');
let h = 0; for (const c of 'sajjad') h = (h * 31 + c.charCodeAt(0)) & 0x7fffffff;
ok(m.color307('sajjad') === ['#2563EB', '#DB5C9A', '#F59E0B', '#22C55E', '#14A3A0', '#6366F1', '#EF4444', '#8B5CF6'][h % 8], 'same colour rule as the app (h*31+u mod 8)');
if (fails) { console.log('FAILS', fails); process.exit(1); }
console.log('SMOKE_OK');
