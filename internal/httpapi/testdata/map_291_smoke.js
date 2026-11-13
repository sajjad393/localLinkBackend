// Bundle 291 (TODO_291 B): web map phones never on top of each other, lines
// keep off third photos and off each other. Run by admin_web_291_test.go.
const fs = require('fs');
const src = fs.readFileSync(process.argv[2] || 'internal/httpapi/adminweb/views.js', 'utf8');
function grab(name) {
  const i = src.indexOf('  function ' + name + '(');
  if (i < 0) throw new Error('missing ' + name);
  let depth = 0, j = src.indexOf('{', i);
  for (let k = j; k < src.length; k++) { if (src[k] === '{') depth++; else if (src[k] === '}') { depth--; if (depth === 0) return src.slice(i, k + 1); } }
}
const code = ['layout216', 'arc217', 'apart291', 'ctl291', 'bez291', 'flip291', 'route291'].map(grab).join('\n');
const f = new Function(code + '; return { layout216, arc217, apart291, ctl291, bez291, flip291, route291 };');
const m = f();
let fails = 0;
const ok = (c, msg) => { if (!c) { fails++; console.log('FAIL', msg); } else console.log('ok  ', msg); };
const minGap = (pos, ids) => { let g = Infinity; for (let i = 0; i < ids.length; i++) for (let j = i + 1; j < ids.length; j++) g = Math.min(g, Math.hypot(pos[ids[i]].x - pos[ids[j]].x, pos[ids[i]].y - pos[ids[j]].y)); return g; };

// 1. The 13 Nov case: 2 phones online kept, a 3rd comes online.
let nodes = [{ device_id: 'sajjad', online: true }, { device_id: 'kiran', online: true }];
let keep = m.layout216(nodes, []);
nodes = [{ device_id: 'ayesha', online: true }, { device_id: 'kiran', online: true }, { device_id: 'sajjad', online: true }];
let pos = m.layout216(nodes, []);
const before = Math.hypot(pos.ayesha.x - keep.sajjad.x, pos.ayesha.y - keep.sajjad.y);
pos.sajjad = { ...keep.sajjad }; pos.kiran = { ...keep.kiran };
const kept = new Set(['sajjad', 'kiran']);
console.log('before fix: ayesha to kept sajjad =', before.toFixed(1));
const s0 = { ...pos.sajjad }, k0 = { ...pos.kiran };
m.apart291(pos, nodes.map((n) => n.device_id), kept);
const g = minGap(pos, ['ayesha', 'kiran', 'sajjad']);
ok(g >= 110, 'new phone moved off kept ones, min gap ' + g.toFixed(1));
ok(pos.sajjad.x === s0.x && pos.sajjad.y === s0.y && pos.kiran.x === k0.x, 'kept phones stay where they were');

// 2. Two kept phones already on top of each other get pulled apart too.
pos = { server: { x: 500, y: 110 }, a: { x: 300, y: 300 }, b: { x: 300, y: 300 }, c: { x: 305, y: 302 } };
m.apart291(pos, ['a', 'b', 'c'], new Set(['a', 'b', 'c']));
ok(minGap(pos, ['a', 'b', 'c']) >= 110, 'stacked kept phones separated ' + minGap(pos, ['a', 'b', 'c']).toFixed(1));

// 3. Many phones: 12 and 30 random, all inside the stage, no overlap of photos (48 px + tag => 64).
for (const n of [3, 6, 12, 30, 60]) {
  const ns = []; for (let i = 0; i < n; i++) ns.push({ device_id: 'p' + i, online: i % 3 !== 0 });
  pos = m.layout216(ns, []);
  const ids = ns.map((x) => x.device_id);
  ids.forEach((id, i) => { if (i % 2) pos[id] = { x: 300 + (i % 4), y: 300 }; }); // half stacked on purpose
  m.apart291(pos, ids, new Set(ids.filter((_, i) => i % 2)));
  const gg = minGap(pos, ids);
  const inside = ids.every((id) => pos[id].x >= 60 && pos[id].x <= 940 && pos[id].y >= 190 && pos[id].y <= 630);
  ok(gg >= 63 && inside, n + ' phones: min gap ' + gg.toFixed(1) + ', inside=' + inside);
}

// 4. A server line that would run under a third phone bows another way.
const C = { x: 500, y: 330 };
const p = { x: 500, y: 110 }, q = { x: 200, y: 500 };
const c0 = m.ctl291(p, q, C, 0.60);
const blocker = m.bez291(p, c0, q, 0.5);
const first = m.arc217(p, q, C, 0.60);
let mids = [];
const d = m.route291(p, q, C, [0.60, 0.35, 0.85, 0.15], first, [blocker], mids);
ok(d !== first, 'line moved off the photo in its way');
// the chosen path keeps 34 px from the blocker
const nums = d.match(/-?\d+(\.\d+)?/g).map(Number);
const cc = { x: nums[2], y: nums[3] };
let clear = Infinity; for (let s = 1; s < 24; s++) { const b = m.bez291(p, cc, q, s / 24); clear = Math.min(clear, Math.hypot(b.x - blocker.x, b.y - blocker.y)); }
ok(clear >= 34, 'new path clear of the photo by ' + clear.toFixed(1));
// 5. Nothing in the way -> the usual 217 line, unchanged.
mids = [];
ok(m.route291(p, q, C, [0.60], first, [{ x: 900, y: 650 }], mids) === first, 'free line keeps the usual 0.60 bow');
// 6. Two lines with the same middle: the second one moves.
mids = [];
const a1 = m.route291(p, q, C, [0.60, 0.35], first, [], mids);
const a2 = m.route291(p, q, C, [0.60, 0.35], first, [], mids);
ok(a1 === first && a2 !== first, 'second line on top of the first takes another bow');
console.log(fails ? 'SMOKE_FAIL ' + fails : 'SMOKE_OK');
process.exit(fails ? 1 : 0);
