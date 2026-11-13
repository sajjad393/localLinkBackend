// Bundle 292 (TODO_292): runs the real web map code (views.js) in node:
// layout rows, default zoom never below 100 %, "+N more" count, offline row.
const fs = require('fs');
const src = fs.readFileSync(process.argv[2] || 'internal/httpapi/adminweb/views.js', 'utf8');
function grab(name) {
  const i = src.indexOf('  function ' + name + '(');
  if (i < 0) throw new Error('missing ' + name);
  let depth = 0; const j = src.indexOf('{', i);
  for (let k = j; k < src.length; k++) { if (src[k] === '{') depth++; else if (src[k] === '}') { depth--; if (depth === 0) return src.slice(i, k + 1); } }
  throw new Error('unclosed ' + name);
}
const names = ['reach294', 'layout216', 'apart291', 'fit217', 'fit292', 'outside292'];
const m = new Function(names.map(grab).join('\n') + '; return { ' + names.join(', ') + ' };')();
let fails = 0;
const ok = (c, msg) => { if (!c) { fails++; console.log('FAIL', msg); } else console.log('ok  ', msg); };
const minGap = (pos, ids) => { let g = Infinity; for (let i = 0; i < ids.length; i++) for (let j = i + 1; j < ids.length; j++) g = Math.min(g, Math.hypot(pos[ids[i]].x - pos[ids[j]].x, pos[ids[i]].y - pos[ids[j]].y)); return g; };
const SW = 794; const SH = 457; // the owner's screen (1352 x 607)

// 1. The owner's map: 3 online + 1 offline.
for (const [on, off] of [[3, 1], [8, 4], [22, 8]]) {
  const ns = [];
  for (let i = 0; i < on; i++) ns.push({ device_id: 'on' + i, online: true });
  for (let i = 0; i < off; i++) ns.push({ device_id: 'off' + i, online: false });
  const ids = ns.map((n) => n.device_id);
  const pos = m.layout216(ns, [], SW);
  m.apart291(pos, ids, new Set());
  const pts = ids.map((id) => pos[id]).concat([pos.server]);
  const view = m.fit292(pts, SW, SH);
  const n = on + off;
  ok(view.k >= 1 && view.k <= 1.4, n + ' phones: default zoom ' + view.k.toFixed(2) + ' (100 - 140 %)');
  const gap = minGap(pos, ids);
  ok(gap >= 110, n + ' phones: min gap ' + gap.toFixed(1));
  const onMaxY = Math.max(...ids.filter((id) => id.startsWith('on')).map((id) => pos[id].y));
  const offMinY = Math.min(...ids.filter((id) => id.startsWith('off')).map((id) => pos[id].y));
  ok(offMinY > onMaxY + 40, n + ' phones: offline row under every online phone');
  ok(ids.every((id) => pos[id].y > pos.server.y + 60), n + ' phones: server on top');
  const out = m.outside292(ids.map((id) => pos[id]), view, SW, SH);
  if (n <= 4) {
    ok(out === 0, n + ' phones: everybody visible, no pill');
    // Old view for the same map was 0.5 (names 5.5 px).
    ok(view.k >= 1, n + ' phones: names >= 12 px (zoom ' + view.k.toFixed(2) + ')');
  } else {
    // the pill count = phones whose spot is outside the stage
    const manual = ids.filter((id) => { const x = pos[id].x * view.k + view.x; const y = pos[id].y * view.k + view.y; return x < 0 || x > SW || y < 0 || y > SH - 40; }).length;
    ok(out === manual && out > 0, n + ' phones: "+' + out + ' more" pill');
    ok(pos.server.y * view.k + view.y >= 0, n + ' phones: server visible at the top');
    // The fit button still shows everybody (fit217, may go below 100 %).
    const all = m.fit217(pts, SW, SH);
    ok(m.outside292(ids.map((id) => pos[id]), all, SW, SH + 40) === 0 || all.k === 0.4, n + ' phones: fit button shows everybody');
  }
}
// 2. Rows wrap at the stage width; a row never sits right under the one above.
{
  const ns = []; for (let i = 0; i < 9; i++) ns.push({ device_id: 'p' + i, online: true });
  const pos = m.layout216(ns, [], SW);
  const ys = [...new Set(ns.map((n) => Math.round(pos[n.device_id].y / 50)))];
  ok(ys.length >= 2, '9 online phones at 794 px: more than one row');
  ok(pos.box && pos.box.W >= SW, 'world at least as wide as the stage');
}
// 3. Old callers without a box keep the 1000 x 700 bounds.
{
  const pos = { server: { x: 500, y: 110 }, a: { x: 300, y: 300 }, b: { x: 300, y: 300 } };
  m.apart291(pos, ['a', 'b'], new Set());
  ok(pos.a.y >= 190 && pos.b.y <= 630, 'apart291 without a box: old bounds');
}
console.log(fails ? 'SMOKE_FAIL ' + fails : 'SMOKE_OK');
process.exit(fails ? 1 : 0);
