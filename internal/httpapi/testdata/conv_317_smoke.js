// Bundle 317 (TODO_317): runs the real web code (views.js) in node.
// A: Ready only when every part is here. B: group system lines.
// Regression: the 307 / 310 steps stay the same.
const fs = require('fs');
const src = fs.readFileSync(process.argv[2] || 'internal/httpapi/adminweb/views.js', 'utf8');
function grab(name) {
  const i = src.indexOf('  function ' + name + '(');
  if (i < 0) throw new Error('missing ' + name);
  let depth = 0; const j = src.indexOf('{', i);
  for (let k = j; k < src.length; k++) { if (src[k] === '{') depth++; else if (src[k] === '}') { depth--; if (depth === 0) return src.slice(i, k + 1); } }
  throw new Error('unclosed ' + name);
}
const names = ['num', 'step307', 'sysLines317'];
const m = new Function(names.map(grab).join('\n') + '; return { ' + names.join(', ') + ' };')();
let fails = 0;
const ok = (c, msg) => { if (!c) { fails++; console.log('FAIL', msg); } else console.log('ok  ', msg); };
const s = (q) => m.step307(Object.assign({ action: 'get_chat', state: 'pending', parts: 0, of: 0, phones: 2, reached: 0 }, q));

// ---- A: done = the phone answered, not Ready
let x = s({ state: 'done', reached: 1, from_name: 'kiran' });
ok(x.k === 'coming' && x.live && x.ring === '0%', 'done without parts: Coming at 0 %, still live');
ok(x.big.indexOf("kiran's phone") >= 0 && x.big.indexOf('answered') > 0 && x.sub === 'The chat is on the way', '"kiran\'s phone answered \u00b7 The chat is on the way"');
ok(s({ state: 'done', action: 'get_file' }).sub === 'The file is on the way', 'get_file: "The file is on the way"');
ok(s({ state: 'done', from_name: '<b>' }).big.indexOf('<b><b>') < 0, 'the name is escaped');
x = s({ state: 'done', parts: 31, of: 80 });
ok(x.k === 'coming' && x.chip === '31 of 80', 'done + 31 of 80 parts: Coming 31 of 80');
ok(s({ state: 'done', parts: 80, of: 80 }).k === 'ready', 'all parts here: Ready');
ok(s({ state: 'done', closed: true }).k === 'ready', 'a closed copy stays Ready');
ok(m.step307({ action: 'delete_completely', state: 'done', parts: 0, of: 0 }).k === 'ready', 'other actions: done = Ready (unchanged)');
// regression
ok(s({}).k === 'sent' && s({ reached: 1 }).k === 'reached' && s({ parts: 3, of: 9 }).k === 'coming' && s({ parts: 9, of: 9 }).k === 'ready', 'regression: sent / reached / coming / ready');
ok(s({ empty: true }).k === 'empty' && s({ state: 'cancelled' }).k === 'cancel', 'regression: empty / cancelled');

// ---- B: group system lines
const ev = (o) => 'llev:v1:' + JSON.stringify(o);
ok(m.sysLines317('hello') === null && m.sysLines317('llev:v1:{bad') === null, 'normal text / broken json: not an event');
let l = m.sysLines317(ev({ k: 'create', by: 'k1', n: 'Friends', nm: { k1: 'kiran' } }));
ok(l.length === 1 && l[0] === 'kiran created group "Friends"', 'create: kiran created group "Friends"');
l = m.sysLines317(ev({ k: 'add', by: 'k1', t: ['a1', 'b1'], nm: { k1: 'kiran', a1: 'ayesha' } }), (id) => (id === 'b1' ? 'ibrahim' : ''));
ok(l[0] === 'kiran added ayesha and ibrahim', 'add: names from the copy + the event (' + l[0] + ')');
ok(m.sysLines317(ev({ k: 'add', by: 'zz', t: ['a1'] }))[0] === 'Someone added Someone', 'no names: Someone');
ok(m.sysLines317(ev({ k: 'leave', by: 'k1', o: 'a1', nm: { k1: 'kiran', a1: 'ayesha' } })).join('|') === 'kiran left|ayesha is now the group owner', 'leave + new owner = 2 lines');
ok(m.sysLines317(ev({ k: 'set', by: 'k1', f: 'send_messages', v: 'admins', nm: { k1: 'kiran' } }))[0] === 'kiran changed this group so only admins can send messages', 'set send_messages admins');
ok(m.sysLines317(ev({ k: 'zzz', by: 'k1', nm: { k1: 'kiran' } }))[0] === 'kiran changed the group', 'unknown kind: changed the group');
ok(m.sysLines317(ev({ k: 'add', by: 'k1', t: ['a1'], nm: { k1: 'kiran' } }), () => 'Group member')[0] === 'kiran added Someone', '"Group member" from the copy is not a name (the event name wins)');

if (fails) { console.log('FAILS', fails); process.exit(1); }
console.log('SMOKE_OK');
