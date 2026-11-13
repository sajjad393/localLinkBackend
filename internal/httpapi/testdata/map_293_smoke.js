// Bundle 293 (TODO_293): runs the real web map code (views.js) in node:
// phone widths (2 in a row, no stagger, world never wider than the stage),
// phone default zoom 80 - 90 %, server lines stay inside the map, computer unchanged.
const fs = require('fs');
const src = fs.readFileSync(process.argv[2] || 'internal/httpapi/adminweb/views.js', 'utf8');
function grab(name) {
  const i = src.indexOf('  function ' + name + '(');
  if (i < 0) throw new Error('missing ' + name);
  let depth = 0; const j = src.indexOf('{', i);
  for (let k = j; k < src.length; k++) { if (src[k] === '{') depth++; else if (src[k] === '}') { depth--; if (depth === 0) return src.slice(i, k + 1); } }
  throw new Error('unclosed ' + name);
}
const names = ['reach294', 'layout216', 'apart291', 'fit217', 'fit292', 'outside292', 'arc217', 'ctl291', 'bez291', 'flip291', 'route291'];
const m = new Function(names.map(grab).join('\n') + '; return { ' + names.join(', ') + ' };')();
let fails = 0;
const ok = (c, msg) => { if (!c) { fails++; console.log('FAIL', msg); } else console.log('ok  ', msg); };
const make = (on, off) => { const ns = []; for (let i = 0; i < on; i++) ns.push({ device_id: 'on' + i, online: true }); for (let i = 0; i < off; i++) ns.push({ device_id: 'off' + i, online: false }); return ns; };
// the same edge points the map uses (views.js "edge293")
const edges293 = (box) => { const e = []; for (let x = 0; x <= box.W; x += 30) e.push({ x, y: -24 }, { x, y: box.H + 24 }); for (let y = 0; y <= box.H; y += 30) e.push({ x: -24, y }, { x: box.W + 24, y }); return e; };
const pathPts = (d) => (d.match(/-?\d+(\.\d+)?/g) || []).map(Number);
const curveIn = (d, box, pad) => {
  // "M x y Q cx cy x2 y2": sample the quadratic curve
  const n = pathPts(d); if (n.length < 6) return true;
  const [x0, y0, cx, cy, x1, y1] = n;
  for (let s = 0; s <= 24; s++) { const t = s / 24; const x = (1 - t) * (1 - t) * x0 + 2 * (1 - t) * t * cx + t * t * x1; const y = (1 - t) * (1 - t) * y0 + 2 * (1 - t) * t * cy + t * t * y1;
    if (x < -pad || x > box.W + pad || y < -pad || y > box.H + pad) return false; }
  return true;
};

// 1. Phone width (330 px stage): 2 in a row, no stagger, world not wider than the stage.
for (const [on, off] of [[2, 2], [5, 3], [9, 4]]) {
  const SW = 330; const SH = 471;
  const ns = make(on, off); const ids = ns.map((n) => n.device_id);
  const pos = m.layout216(ns, [], SW); m.apart291(pos, ids, new Set());
  const n = on + off;
  ok(pos.box.W <= SW, n + ' phones at 330 px: world ' + pos.box.W + ' <= stage');
  // every full row uses the same 2 spots (no half-step shift); a lone last phone sits in the middle
  const rows = {}; ids.forEach((id) => { const r = Math.round(pos[id].y / 20); (rows[r] = rows[r] || []).push(Math.round(pos[id].x)); });
  const full = Object.values(rows).filter((r) => r.length === 2).map((r) => r.sort((a, b) => a - b).join(','));
  ok(full.length > 0 && new Set(full).size === 1 && Object.values(rows).every((r) => r.length <= 2), n + ' phones at 330 px: 2 in a row, no stagger (' + [...new Set(full)].join(' ') + ')');
  const view = m.fit292(ids.map((id) => pos[id]).concat([pos.server]), SW, SH);
  ok(view.k >= 0.8 && view.k <= 0.9, n + ' phones at 330 px: default zoom ' + view.k.toFixed(2) + ' (80 - 90 %)');
  // server lines with the edge points stay inside the map
  const C = { x: pos.box.W / 2, y: pos.box.H / 2 };
  const edge = edges293(pos.box); const mids = []; let inside = 0;
  ids.forEach((id) => {
    const p = pos.server; const q = pos[id];
    const obst = ids.filter((x) => x !== id).map((x) => pos[x]).concat(edge);
    const d = m.route291(p, q, C, [0.60, 0.35, 0.85, 0.15], m.arc217(p, q, C, 0.60), obst, mids);
    if (curveIn(d, pos.box, 4)) inside++;
  });
  ok(inside === ids.length, n + ' phones at 330 px: ' + inside + '/' + ids.length + ' server lines inside the map');
}
// 2. 4 phones on a phone: everybody visible at 90 %.
{
  const ns = make(2, 2); const ids = ns.map((n) => n.device_id);
  const pos = m.layout216(ns, [], 330); m.apart291(pos, ids, new Set());
  const view = m.fit292(ids.map((id) => pos[id]).concat([pos.server]), 330, 471);
  ok(Math.abs(view.k - 0.9) < 1e-9, '4 phones on a phone: 90 %');
  ok(m.outside292(ids.map((id) => pos[id]), view, 330, 471) === 0, '4 phones on a phone: nobody hidden');
}
// 3. Computer unchanged: 3+ in a row keeps the stagger and 100 - 140 %.
{
  const ns = make(8, 4); const ids = ns.map((n) => n.device_id);
  const pos = m.layout216(ns, [], 794);
  ok(pos.box.W >= 794, 'computer: world at least as wide as the stage');
  const rows = {}; ids.filter((id) => id.startsWith('on')).forEach((id) => { const r = Math.round(pos[id].y / 40); (rows[r] = rows[r] || []).push(pos[id].x); });
  const r = Object.values(rows);
  ok(r.length >= 2 && !r[1].some((x) => r[0].some((a) => Math.abs(a - x) < 1)), 'computer: second row still staggered');
  const view = m.fit292(ids.map((id) => pos[id]).concat([pos.server]), 794, 457);
  ok(view.k >= 1 && view.k <= 1.4, 'computer: default zoom ' + view.k.toFixed(2) + ' (100 - 140 %)');
}
console.log(fails ? 'SMOKE_FAIL ' + fails : 'SMOKE_OK');
process.exit(fails ? 1 : 0);
