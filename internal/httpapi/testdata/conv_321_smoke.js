// Bundle 321 (R9 = the app's R4 / R1): runs the real web code (views.js) in
// node - the files card, the photo / video tiles, the voice label and the
// list words. Regression: the 317 file words stay.
const fs = require('fs');
const src = fs.readFileSync(process.argv[2] || 'internal/httpapi/adminweb/views.js', 'utf8');
function grab(name) {
  const i = src.indexOf('  function ' + name + '(');
  if (i < 0) throw new Error('missing ' + name);
  let depth = 0; const j = src.indexOf('{', i);
  for (let k = j; k < src.length; k++) { if (src[k] === '{') depth++; else if (src[k] === '}') { depth--; if (depth === 0) return src.slice(i, k + 1); } }
  throw new Error('unclosed ' + name);
}
function grabConst(name) {
  const i = src.indexOf('const ' + name + ' = ');
  if (i < 0) throw new Error('missing const ' + name);
  return src.slice(i, src.indexOf(';\n', i) + 1);
}
const fns = ['num', 'step307', 'partsDone', 'fromPhone317', 'fileState317', 'fileWords317', 'files317', 'viewFiles321', 'fill321',
  'filesCard321', 'tile321', 'mb299', 'finishedRow307'];
const consts = ['MAX_FILE317', 'CHECK307', 'PLAY310', 'WAIT321', 'BLOCK321', 'MIC321'];
const harness = `
  const scope = { requests: [], view: null, viewReq: null, viewId: '' };
  const ui = { esc: (s) => String(s == null ? '' : s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'),
    icon: (n) => '<svg class="' + n + '"></svg>' };
  const names307 = (q) => ({ list: [q.label || 'x'], title: q.label || 'x', group: false });
  const pair307 = () => '';
  const dayLabel = () => '17 Nov';
  ${consts.map(grabConst).join('\n')}
  ${fns.map(grab).join('\n')}
  return { scope, ${fns.join(', ')} };`;
const m = new Function(harness)();
let fails = 0;
const ok = (c, msg) => { if (!c) { fails++; console.log('FAIL', msg); } else console.log('ok  ', msg); };
const MB = 1048576;
const att = (id, type, size) => ({ file_id: id, name: id, type, size, thumb: 'QUJD' });
const view = { from_name: 'ayesha', complete: true, of: 4, parts: 4, messages: [
  { id: 'm1', attachments: [att('p1', 'image/jpeg', 2 * MB), att('v1', 'video/mp4', 6 * MB), att('p2', 'image/jpeg', MB)] },
  { id: 'm2', attachments: [Object.assign(att('g1', 'image/jpeg', MB), { file_gone: true })] },
] };
const chat = { id: 'r1', action: 'get_chat', state: 'done', parts: 4, of: 4, label: 'ayesha & kiran', from_name: 'ayesha', made_at: '2026-11-17T10:00:00Z' };
const file = (id, fid, parts, of, extra) => Object.assign({ id, action: 'get_file', parent: 'r1', file_id: fid, state: 'done', parts, of }, extra || {});
m.scope.view = view; m.scope.viewReq = chat;
m.scope.requests = [chat, file('aa_p1', 'p1', 25, 40), file('aa_p2', 'p2', 9, 9)];

// ---- files card
let card = m.filesCard321();
ok(card.indexOf('<span>1 of 3</span> files here') > 0, 'files card: "1 of 3 files here" (gone file not counted)');
ok(card.indexOf("from ayesha's phone") > 0 && card.indexOf(' MB of 9.0 MB') > 0, 'files card: MB of MB + from X\'s phone');
ok((card.match(/<i class=/g) || []).length === 3 && (card.match(/class="d"/g) || []).length === 1, 'one piece per file, 1 done');
ok(card.indexOf('data-conv') < 0 && card.indexOf('Stop') < 0, 'no Stop / no button on the page');
m.scope.requests = [chat, file('aa_p1', 'p1', 40, 40), file('aa_v1', 'v1', 9, 9), file('aa_p2', 'p2', 9, 9)];
card = m.filesCard321();
ok(card.indexOf('All files here') > 0 && card.indexOf('3 files') > 0 && card.indexOf('tc321 done') > 0, 'all here: green "All files here · 3 files"');
m.scope.viewReq = Object.assign({}, chat, { parts: 1, of: 4 });
ok(m.filesCard321() === '', 'chat still coming: no files card');
m.scope.viewReq = chat;

// ---- tiles
m.scope.requests = [chat, file('aa_p1', 'p1', 25, 40), file('aa_p2', 'p2', 9, 9)];
const tile = (a) => m.tile321(a, m.fileState317(a), m.fileState317(a).f || null, /^video/.test(a.type), a.name, '2.0 MB', '<span class="ft310">6:44 PM</span>');
let t = tile(view.messages[0].attachments[0]);
ok(t.indexOf('t321 wait') > 0 && t.indexOf('Coming by itself') > 0 && t.indexOf('63%') > 0 && t.indexOf('25 / 40 parts') > 0, 'coming: blur + ring 63% + "Coming by itself" + parts');
ok(t.indexOf("from ayesha's phone") > 0 && t.indexOf('6:44 PM') > 0 && t.indexOf('Photo \u00b7 2.0 MB') > 0, 'coming: footer from X\'s phone + time, chip');
t = tile(view.messages[0].attachments[1]);
ok(t.indexOf('Next in line') > 0 && t.indexOf('Video \u00b7 2.0 MB') > 0, 'video not asked yet: "Next in line" + Video chip');
t = tile(view.messages[0].attachments[2]);
ok(t.indexOf('t321 here') > 0 && t.indexOf('data-conv221-show="aa_p2"') > 0 && t.indexOf('tm321') > 0 && t.indexOf('Coming') < 0, 'here: picture + time chip, click shows the full photo');
t = tile(view.messages[1].attachments[0]);
ok(t.indexOf('t321 off') > 0 && t.indexOf('File removed after 60 days') > 0 && t.indexOf('tm321') > 0, 'gone: grey + reason + time chip');
m.scope.requests = [chat, file('aa_p1', 'p1', 0, 0, { refused: 'gone' })];
t = tile(view.messages[0].attachments[0]);
ok(t.indexOf("Not on ayesha's phone any more") > 0 && t.indexOf('t321 off') > 0, 'refused: "Not on X\'s phone any more" (317 words kept)');
ok(m.fileWords317({ k: 'big' }) === 'Too big to bring (over 50 MB)', 'regression: 317 too big words');
ok(tile(Object.assign(att('x', 'image/jpeg', 1), { name: '<b>' })).indexOf('<b>"') < 0, 'names are escaped');

// ---- list words (R1)
const row = () => m.finishedRow307(chat, 0);
m.scope.requests = [chat, file('aa_p1', 'p1', 25, 40), file('aa_p2', 'p2', 9, 9)];
ok(row().indexOf('Messages here \u00b7 1 of 2 files coming') > 0 && row().indexOf('Ready to read') < 0, 'files coming: "Messages here · 1 of 2 files coming", not Ready');
m.scope.requests = [chat, file('aa_p1', 'p1', 40, 40), file('aa_p2', 'p2', 9, 9)];
ok(row().indexOf('Ready to read') > 0, 'all here: "Ready to read"');
m.scope.requests = [chat, file('aa_p1', 'p1', 40, 40), file('aa_p2', 'p2', 0, 0, { refused: 'gone' })];
ok(row().indexOf('1 file not available') > 0 && row().indexOf('Ready to read') < 0, 'a file gone: grey "1 file not available"');
m.scope.requests = [chat];
ok(row().indexOf('Ready to read') > 0, 'no files: "Ready to read"');
ok(m.finishedRow307(Object.assign({}, chat, { empty: true }), 0).indexOf('No messages') > 0, 'regression: No messages');

if (fails) { console.log('FAILS', fails); process.exit(1); }
console.log('SMOKE_OK');
