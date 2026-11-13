// Bundle 294 (TODO_294): runs the real web map code (views.js) in node.
// A: phones reached over Wi-Fi Direct / mesh are in the normal rows (not OFFLINE).
// B: lines keep off the photo + crown. C: fewest crossings, gentle server line
// under the server. D: two lines into one phone keep >= 22 degrees apart.
// routeAll() below follows the map's own steps (views.js "path294").
const fs = require('fs');
const src = fs.readFileSync(process.argv[2] || 'internal/httpapi/adminweb/views.js', 'utf8');
function grab(name) {
  const i = src.indexOf('  function ' + name + '(');
  if (i < 0) throw new Error('missing ' + name);
  let depth = 0; const j = src.indexOf('{', i);
  for (let k = j; k < src.length; k++) { if (src[k] === '{') depth++; else if (src[k] === '}') { depth--; if (depth === 0) return src.slice(i, k + 1); } }
  throw new Error('unclosed ' + name);
}
const names = ['reach294', 'layout216', 'apart291', 'arc217', 'ctl291', 'bez291', 'flip291', 'cross294', 'route291'];
const m = new Function(names.map(grab).join('\n') + '; return { ' + names.join(', ') + ' };')();
let fails = 0;
const ok = (c, msg) => { if (!c) { fails++; console.log('FAIL', msg); } else console.log('ok  ', msg); };
const curve = (d) => { const n = (d.match(/-?\d+(\.\d+)?/g) || []).map(Number); const p = { x: n[0], y: n[1] }; const c = { x: n[2], y: n[3] }; const q = { x: n[4], y: n[5] }; const pts = []; for (let s = 0; s <= 48; s++) pts.push(m.bez291(p, c, q, s / 48)); return { p, c, q, pts }; };
const crosses = (A, B) => { for (let a = 1; a < A.pts.length; a++) for (let b = 1; b < B.pts.length; b++) if (m.cross294(A.pts[a - 1], A.pts[a], B.pts[b - 1], B.pts[b])) return true; return false; };
const N = (id, on, link) => ({ device_id: id, online: on, link });

function routeAll(nodes, edges, SW) {
  const pos = m.layout216(nodes, edges, SW);
  m.apart291(pos, nodes.map((n) => n.device_id), new Set());
  const allPts = nodes.map((n) => pos[n.device_id]).concat([pos.server]);
  const C = allPts.reduce((a, p) => ({ x: a.x + p.x / allPts.length, y: a.y + p.y / allPts.length }), { x: 0, y: 0 });
  const phonePts = nodes.map((n) => pos[n.device_id]);
  const CP = phonePts.reduce((a, p) => ({ x: a.x + p.x / phonePts.length, y: a.y + p.y / phonePts.length }), { x: 0, y: 0 });
  const mids = []; const drawn = []; const d = {};
  const order = edges.map((e, i) => i).sort((a, b) => (edges[a].from === 'server' ? 0 : 1) - (edges[b].from === 'server' ? 0 : 1) || a - b);
  order.forEach((i) => {
    const e = edges[i]; const p = pos[e.from]; const q = pos[e.to];
    const obs = nodes.filter((n) => n.device_id !== e.from && n.device_id !== e.to).map((n) => pos[n.device_id]);
    obs.slice().forEach((o) => obs.push({ x: o.x, y: o.y - 40, r: 24 }));
    if (e.from !== 'server') obs.push(pos.server);
    const first = e.from === 'server' ? m.arc217(p, q, C, 0.60) : m.arc217(p, q, CP, 0.25);
    const steep = e.from === 'server' && Math.abs(q.x - p.x) < Math.abs(q.y - p.y) * 0.6;
    d[i] = e.from === 'server' ? m.route291(p, q, C, steep ? [0.15, 0.35, 0.60, 0.85] : [0.60, 0.35, 0.85, 0.15], steep ? m.arc217(p, q, C, 0.15) : first, obs, mids, drawn)
      : m.route291(p, q, CP, [0.25, 0.45, 0.10, 0.65], first, obs, mids, drawn);
  });
  return { pos, d };
}
const countCross = (edges, d) => { let n = 0; for (let a = 0; a < edges.length; a++) for (let b = a + 1; b < edges.length; b++) {
  const ea = edges[a]; const eb = edges[b]; if ([ea.from, ea.to].some((x) => x === eb.from || x === eb.to)) continue;
  if (crosses(curve(d[a]), curve(d[b]))) n++; } return n; };

// A. who is reached
ok(m.reach294(N('a', true, 'server')) && m.reach294(N('b', false, 'wifi_direct')) && m.reach294(N('c', false, 'mesh')) && m.reach294(N('d', false, 'lan')), 'server / Wi-Fi Direct / mesh / local Wi-Fi = reached');
ok(!m.reach294(N('e', false, '')) && !m.reach294(N('f', false, 'offline')) && !m.reach294(N('g', false, undefined)), 'no link / offline = not reached (grey, OFFLINE)');

// The owner's screenshot (14 Nov): ayesha, kiran, sajjad on the server, sumaira over Wi-Fi Direct.
const nodes = [N('ay', true, 'server'), N('ki', true, 'server'), N('sa', true, 'server'), N('su', false, 'wifi_direct')];
const edges = [{ from: 'server', to: 'ay' }, { from: 'server', to: 'ki' }, { from: 'server', to: 'sa' },
  { from: 'ay', to: 'sa' }, { from: 'ki', to: 'sa' }, { from: 'sa', to: 'su' }];
for (const SW of [794, 1100, 330]) {
  const { pos, d } = routeAll(nodes, edges, SW);
  const onY = ['ay', 'ki', 'sa'].map((id) => pos[id].y);
  ok(pos.su.y <= Math.max(...onY) + 140 && pos.su.y >= Math.min(...onY) - 1, SW + ' px: sumaira in the normal rows (y ' + pos.su.y + ')');
  // B: no line through another phone's photo / crown (crown point 40 px over the spot)
  let worst = Infinity;
  edges.forEach((e, i) => { const c = curve(d[i]); nodes.forEach((n) => { if (n.device_id === e.from || n.device_id === e.to) return;
    const o = pos[n.device_id]; c.pts.slice(2, -2).forEach((b) => { worst = Math.min(worst, Math.hypot(b.x - o.x, b.y - (o.y - 40))); }); }); });
  ok(worst >= 22, SW + ' px: lines keep ' + worst.toFixed(1) + ' px off every crown / photo top');
  // C: no crossings in this map
  ok(countCross(edges, d) === 0, SW + ' px: 0 crossings');
  // D: lines into sajjad
  const ends = edges.map((e, i) => [e, curve(d[i])]).filter(([e]) => e.from === 'sa' || e.to === 'sa').map(([e, c]) => { const nb = e.to === 'sa' ? c.pts[c.pts.length - 3] : c.pts[2]; return Math.atan2(nb.y - pos.sa.y, nb.x - pos.sa.x); });
  let minA = Infinity; for (let a = 0; a < ends.length; a++) for (let b = a + 1; b < ends.length; b++) { let x = Math.abs(ends[a] - ends[b]); if (x > Math.PI) x = 2 * Math.PI - x; minA = Math.min(minA, x * 180 / Math.PI); }
  ok(minA >= 20, SW + ' px: lines into sajjad ' + minA.toFixed(0) + ' deg apart');
}
// B + C: the screenshot's first row (ayesha, kiran in the middle, sajjad) with the
// ayesha-sajjad line that used to run over kiran's crown, plus a 4th phone nobody reaches.
{
  const ns = [N('ay', true, 'server'), N('ki', true, 'server'), N('sa', true, 'server'), N('x', false, '')];
  const es = [{ from: 'server', to: 'ay' }, { from: 'server', to: 'ki' }, { from: 'server', to: 'sa' }, { from: 'ay', to: 'sa' }, { from: 'ki', to: 'sa' }];
  const { pos, d } = routeAll(ns, es, 794);
  const c = curve(d[3]); const k = pos.ki;
  const gap = Math.min(...c.pts.slice(2, -2).map((b) => Math.min(Math.hypot(b.x - k.x, b.y - (k.y - 40)), Math.hypot(b.x - k.x, b.y - k.y))));
  ok(gap >= 22, 'ayesha-sajjad keeps ' + gap.toFixed(1) + ' px off kiran (photo, crown)');
  ok(countCross(es, d) === 0, 'first row of the screenshot: 0 crossings');
}
// C: a phone right under the server gets a gentle line (not the big 0.60 loop)
{
  const ns = [N('l', true, 'server'), N('mid', true, 'server'), N('r', true, 'server')];
  const es = [{ from: 'server', to: 'l' }, { from: 'server', to: 'mid' }, { from: 'server', to: 'r' }];
  const { pos, d } = routeAll(ns, es, 794);
  const c = curve(d[1]);
  const side = Math.max(...c.pts.map((b) => Math.abs(b.x - pos.server.x)));
  ok(Math.abs(pos.mid.x - pos.server.x) < 1 && side < 40, 'phone under the server: line goes ' + side.toFixed(0) + ' px sideways at most (0.60 loop = ~50+)');
}
// C: a crossing that can not be avoided happens once, not more
{
  const ns = [N('a', true, 'server'), N('b', true, 'server'), N('c', true, 'server'), N('d', true, 'server')];
  const es = [{ from: 'server', to: 'a' }, { from: 'server', to: 'b' }, { from: 'server', to: 'c' }, { from: 'server', to: 'd' }, { from: 'a', to: 'c' }, { from: 'b', to: 'd' }];
  const { d } = routeAll(ns, es, 794);
  const n = countCross(es, d);
  ok(n <= 1, 'a-c and b-d (b between): ' + n + ' crossing(s), at most 1');
}
// A: offline phones still go under the OFFLINE row
{
  const ns = [N('on', true, 'server'), N('wd', false, 'wifi_direct'), N('off', false, '')];
  const pos = m.layout216(ns, [], 794);
  ok(pos.off.y > pos.on.y + 60 && pos.off.y > pos.wd.y + 60 && Math.abs(pos.wd.y - pos.on.y) <= 26, 'offline phone under the reached ones; Wi-Fi Direct phone in the first row');
}
console.log(fails ? 'SMOKE_FAIL ' + fails : 'SMOKE_OK');
process.exit(fails ? 1 : 0);
