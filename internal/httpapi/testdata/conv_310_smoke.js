// Bundle 310 (TODO_310): runs the real web code (views.js) in node.
// P3 why each phone waits, P4 old app on every phone, V2 voice bars,
// V3 short file names. Regression: the 307 steps stay the same.
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
const names = ['num', 'step307', 'color307', 'hm310', 'why310', 'isVoice310', 'len310', 'bars310', 'shortName310'];
const m = new Function(pal + '\n' + names.map(grab).join('\n') + '; return { ' + names.join(', ') + ' };')();
let fails = 0;
const ok = (c, msg) => { if (!c) { fails++; console.log('FAIL', msg); } else console.log('ok  ', msg); };
const s = (q) => m.step307(Object.assign({ action: 'get_chat', state: 'pending', parts: 0, of: 0, phones: 2, reached: 0 }, q));

// ---- P4: old app on every phone (after 30 min the server says stuck=old_app)
let x = s({ stuck: 'old_app', who: [{ why: 'old' }, { why: 'old' }] });
ok(x.k === 'old' && x.live && x.chip === 'Old app', 'old app: own step, still live');
ok(x.big.indexOf('on 2 phones') > 0 && x.sub.indexOf('Update LinkUp on these phones') === 0 && x.sub.indexOf('still open') > 0, 'old app: "Old app on 2 phones \u00b7 Update LinkUp on these phones \u00b7 still open"');
x = s({ stuck: 'old_app', who: [{ why: 'old' }], phones: 1 });
ok(x.big.indexOf('on 1 phone<') < 0 && x.big.indexOf('on 1 phone') > 0, 'old app: 1 phone (no plural s)');
// regression: no stuck flag -> the 307 steps as before
ok(s({}).k === 'sent' && s({ old_app: 1 }).sub.indexOf('must update') >= 0, 'regression: Sent + old LinkUp words unchanged');
ok(s({ reached: 1 }).k === 'reached' && s({ parts: 3, of: 9 }).k === 'coming' && s({ parts: 9, of: 9 }).k === 'ready', 'regression: reached / coming / ready unchanged');
ok(s({ stuck: 'old_app', parts: 9, of: 9 }).k === 'ready', 'a finished answer is Ready even with an old stuck flag');

// ---- P3: why each phone waits
const now = new Date(2026, 10, 15, 23, 0, 0);
const at = new Date(2026, 10, 15, 22, 2, 0).toISOString();
ok(m.why310({ why: 'wait', checkin: at }, now).text === 'Not checked in since 10:02 PM', 'wait: "Not checked in since 10:02 PM"');
ok(m.why310({ why: 'wait' }, now).text === 'Not checked in yet', 'wait, never checked in');
ok(m.why310({ why: 'wait', checkin: new Date(2026, 10, 13, 9, 5).toISOString() }, now).text === 'Not checked in since 13 Nov, 9:05 AM', 'wait: older day shows the date');
let w = m.why310({ why: 'old', version: '3.0.100' }, now);
ok(w.k === 'old' && w.text === 'Old app 3.0.100 \u00b7 update to answer', 'old: "Old app 3.0.100 \u00b7 update to answer"');
ok(m.why310({ why: 'old' }, now).text === 'Old app \u00b7 update to answer', 'old without a version');
ok(m.why310({ why: 'empty' }, now).text === 'No copy' && m.why310({ why: 'parts' }, now).text === 'Sending parts', 'No copy / Sending parts');
ok(m.why310({ why: 'looking' }, now).text === 'Looking for the chat' && m.why310({ why: 'got' }, now).k === 'look', 'Looking for the chat / got');
ok(m.why310({ why: 'no_trust' }, now).k === 'wait', 'no_trust waits');

// ---- V2: voice notes
ok(m.isVoice310({ type: 'audio/mp4', name: 'x' }) && m.isVoice310({ type: '', name: 'voice_1731.m4a' }), 'audio/* and voice_*.m4a are voice');
ok(!m.isVoice310({ type: 'image/jpeg', name: 'voice_1.jpg' }) && !m.isVoice310({ type: 'application/pdf', name: 'a.pdf' }), 'photos / files are not voice');
ok(m.len310(7000) === '0:07' && m.len310(65000) === '1:05' && m.len310(0) === '0:00', 'length 0:07 / 1:05');
let svg = m.bars310([0, 50, 100, 20], 150, 0, false);
const rects = svg.match(/<rect /g).length;
ok(rects === Math.floor(151 / 3.5), 'bars every 3.5 px (' + rects + ')');
ok(/width="2\.5"/.test(svg) && /rx="1\.25"/.test(svg) && /<circle /.test(svg), 'bars 2.5 px, round, with the dot');
const hs = (svg.match(/height="([0-9.]+)" rx/g) || []).map((t) => Number(t.match(/[0-9.]+/)[0]));
ok(Math.min.apply(null, hs) >= 3 && Math.max.apply(null, hs) <= 22, 'bar height 3..22');
ok(Math.max.apply(null, hs) === 22, '0..100 levels: the loudest bar is full height');
ok(Math.max.apply(null, (m.bars310([0.1, 1], 60, 0, false).match(/height="([0-9.]+)" rx/g) || []).map((t) => Number(t.match(/[0-9.]+/)[0]))) === 22, '0..1 levels work too');
svg = m.bars310('0000000009', 60, 0, false);
ok(Math.max.apply(null, (svg.match(/height="([0-9.]+)" rx/g) || []).map((t) => Number(t.match(/[0-9.]+/)[0]))) === 22, 'the phone\'s digits 0-9 work (9 = full height)');
svg = m.bars310('5'.repeat(40), 150, 0, false);
ok((svg.match(/height="13\.56"/g) || []).length === rects, 'digit 5 = 13.56 px on every bar');
svg = m.bars310(null, 150, 0, true);
ok(!/<circle /.test(svg) && (svg.match(/height="3\.00"/g) || []).length === rects, 'old phone: flat bars, no dot');
svg = m.bars310([50, 50, 50, 50], 150, 0.5, false);
const wp = (svg.match(/class="wp"/g) || []).length;
ok(wp === Math.floor(rects / 2), 'half played = half the bars blue');

// ---- V3: short file names
ok(m.shortName310('Invoice_Nov.pdf', 28) === 'Invoice_Nov.pdf', 'short names stay');
const sn = m.shortName310('LinkUp_monthly_report_November_2026_final_v3.pdf', 28);
ok(sn.length === 28 && sn.endsWith('.pdf') && sn.indexOf('\u2026') > 0 && sn.startsWith('LinkUp_'), 'long names: start \u2026 end.pdf (' + sn + ')');
ok(m.shortName310('a'.repeat(40), 20).length === 20, 'no extension: still cut');

if (fails) { console.log('FAILS', fails); process.exit(1); }
console.log('SMOKE_OK');
