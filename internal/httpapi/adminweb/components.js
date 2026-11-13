/* ===========================================================================
   LocalLink admin console — render primitives.

   Every helper returns an HTML string built from server data that has been
   escaped. Colours come from CSS custom properties, never hard-coded here, so
   light and dark themes stay in sync with the Flutter admin surface.
   =========================================================================== */
window.LL = window.LL || {};

(function () {
  'use strict';

  // ------------------------------------------------------------------ escape
  const ESCAPE_MAP = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;', '`': '&#96;' };
  function esc(value) {
    if (value === null || value === undefined) return '';
    return String(value).replace(/[&<>"'`]/g, (c) => ESCAPE_MAP[c]);
  }

  function icon(name, cls, size) {
    const style = size ? ` style="width:${size}px;height:${size}px"` : '';
    return `<svg class="icon${cls ? ' ' + cls : ''}" viewBox="0 0 24 24" aria-hidden="true"${style}><use href="#${esc(name)}"></use></svg>`;
  }

  // -------------------------------------------------------------- formatting
  function number(value) {
    if (value === null || value === undefined || value === '') return '—';
    const v = Number(value);
    if (!isFinite(v)) return '—';
    const abs = Math.abs(v);
    if (abs >= 1e9) return (v / 1e9).toFixed(1) + 'B';
    if (abs >= 1e6) return (v / 1e6).toFixed(1) + 'M';
    if (abs >= 1e4) return (v / 1e3).toFixed(1) + 'K';
    return Number.isInteger(v) ? v.toLocaleString() : v.toFixed(1);
  }

  function bytes(value) {
    let n = Number(value);
    if (!isFinite(n)) return '—';
    if (Math.abs(n) < 1024) return n.toFixed(0) + ' B';
    const units = ['KB', 'MB', 'GB', 'TB'];
    let i = -1;
    do { n /= 1024; i += 1; } while (Math.abs(n) >= 1024 && i < units.length - 1);
    return n.toFixed(n >= 100 ? 0 : 1) + ' ' + units[i];
  }

  function relative(iso, now) {
    if (!iso) return '—';
    const then = new Date(iso);
    if (isNaN(then.getTime())) return '—';
    const diff = ((now ? now.getTime() : Date.now()) - then.getTime()) / 1000;
    if (diff < 0) return 'just now';
    if (diff < 45) return 'just now';
    if (diff < 3600) return Math.round(diff / 60) + 'm ago';
    if (diff < 86400) return Math.round(diff / 3600) + 'h ago';
    if (diff < 2592000) return Math.round(diff / 86400) + 'd ago';
    if (diff < 31536000) return Math.floor(diff / 2592000) + 'mo ago';
    return Math.floor(diff / 31536000) + 'y ago';
  }

  function dateTime(iso) {
    if (!iso) return '—';
    const d = new Date(iso);
    if (isNaN(d.getTime())) return '—';
    return d.toLocaleString(undefined, {
      year: 'numeric', month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit',
    });
  }

  const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  function bucket(iso, longRange) {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d.getTime())) return String(iso);
    if (longRange) return `${MONTHS[d.getMonth()]} ${d.getDate()}`;
    return String(d.getHours()).padStart(2, '0') + ':00';
  }

  function duration(seconds) {
    const s = Number(seconds);
    if (!isFinite(s) || s < 0) return '—';
    const d = Math.floor(s / 86400);
    const h = Math.floor((s % 86400) / 3600);
    const m = Math.floor((s % 3600) / 60);
    if (d > 0) return `${d}d ${h}h`;
    if (h > 0) return `${h}h ${m}m`;
    if (m > 0) return `${m}m ${Math.floor(s % 60)}s`;
    return `${Math.floor(s)}s`;
  }

  function titleCase(value) {
    const s = String(value || '').replace(/_/g, ' ').trim();
    if (!s) return '';
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  // ------------------------------------------------------- status and severity
  const STATUS_TONES = {
    healthy: ['success', 'Healthy'], ok: ['success', 'Healthy'], active: ['success', 'Active'],
    online: ['success', 'Online'], connected: ['success', 'Connected'], success: ['success', 'Healthy'],
    warning: ['warning', 'Warning'], warn: ['warning', 'Warning'], degraded: ['warning', 'Warning'],
    pending: ['warning', 'Pending'],
    critical: ['danger', 'Critical'], error: ['danger', 'Error'], failed: ['danger', 'Failed'],
    revoked: ['danger', 'Revoked'], disabled: ['danger', 'Disabled'], suspended: ['danger', 'Suspended'],
    offline: ['neutral', 'Offline'], disconnected: ['neutral', 'Disconnected'], inactive: ['neutral', 'Inactive'],
    unknown: ['neutral', 'Unknown'],
  };

  function statusInfo(status) {
    const key = String(status || 'unknown').toLowerCase();
    const entry = STATUS_TONES[key];
    if (entry) return { tone: entry[0], label: entry[1] };
    return { tone: 'neutral', label: titleCase(key) || 'Unknown' };
  }

  const SEVERITY_TONES = { critical: 'danger', warning: 'warning', info: 'info' };
  function severityTone(severity) {
    return SEVERITY_TONES[String(severity || 'info').toLowerCase()] || 'info';
  }
  function severityIcon(severity) {
    const tone = severityTone(severity);
    if (tone === 'danger') return 'i-alert';
    if (tone === 'warning') return 'i-alert';
    if (tone === 'info') return 'i-info';
    return 'i-info';
  }

  // Colors used by canvas charts. Values are read from the live cascade so the
  // charts follow the active theme without duplicating the palette in JS.
  function token(name, fallback) {
    try {
      const value = getComputedStyle(document.documentElement).getPropertyValue('--' + name).trim();
      return value || fallback;
    } catch (err) {
      return fallback;
    }
  }

  // ------------------------------------------------------------------ builders
  function badge(label, tone, options) {
    const opts = options || {};
    const cls = `badge badge-${tone || 'neutral'}`;
    const dot = opts.dot ? '<span class="dot"></span>' : '';
    const ic = opts.icon ? icon(opts.icon) : '';
    return `<span class="${cls}">${dot}${ic}${esc(label)}</span>`;
  }

  function statusBadge(status, options) {
    const info = statusInfo(status);
    return badge((options && options.label) || info.label, info.tone, { dot: !(options && options.noDot) });
  }

  function severityBadge(severity) {
    const tone = severityTone(severity);
    const label = titleCase(severity || 'info');
    return badge(label, tone, { icon: severityIcon(severity) });
  }

  function panel(options) {
    const o = options || {};
    const pad = o.pad0 ? ' pad-0' : '';
    const head = (o.title || o.actions)
      ? `<div class="panel-head">
           <div class="grow">
             <h2 class="type-section-title">${esc(o.title || '')}</h2>
             ${o.subtitle ? `<span class="sub">${esc(o.subtitle)}</span>` : ''}
           </div>
           ${o.actions ? `<div class="panel-actions">${o.actions}</div>` : ''}
         </div>`
      : '';
    return `<section class="panel${pad}">${head}${o.body || ''}</section>`;
  }

  function sectionHead(options) {
    const o = options || {};
    return `<div class="section-head">
      ${o.icon ? `<span class="metric-icon icon-wrap" style="width:30px;height:30px;border-radius:8px;background:var(--surface-muted);display:grid;place-items:center;color:var(--text-secondary)">${icon(o.icon)}</span>` : ''}
      <div class="grow">
        <h2 class="type-section-title">${esc(o.title || '')}</h2>
        ${o.subtitle ? `<span class="sub">${esc(o.subtitle)}</span>` : ''}
      </div>
      ${o.actions ? `<div class="section-actions">${o.actions}</div>` : ''}
    </div>`;
  }

  function metricCard(options) {
    const o = options || {};
    const accentClass = o.accent ? ` is-accent-${o.accent}` : '';
    return `<article class="metric${accentClass}">
      <div class="top">
        <span class="icon-wrap">${icon(o.icon || 'i-activity')}</span>
        <span class="label">${esc(o.label || '')}</span>
      </div>
      <div class="value">${esc(o.value)}</div>
      ${o.context ? `<div class="context">${esc(o.context)}</div>` : ''}
      ${o.sparkline && o.sparkline.length ? `<div class="spark" data-sparkline='${JSON.stringify(o.sparkline)}'></div>` : ''}
    </article>`;
  }

  function statRow(items) {
    return `<div class="stat-row">${items.map((it) => `<div class="stat">
      <span class="stat-label">${esc(it.label)}</span>
      <span class="stat-value">${esc(it.value)}</span>
    </div>`).join('')}</div>`;
  }

  function headline(tone, text, iconName) {
    return `<div class="headline headline-${tone}">${icon(iconName || 'i-info')}<span>${esc(text)}</span></div>`;
  }

  function table(options) {
    const o = options || {};
    const columns = o.columns || [];
    const rows = o.rows || [];
    if (!rows.length) {
      return emptyState({
        icon: o.emptyIcon || 'i-database',
        title: o.emptyTitle || 'Nothing to show',
        message: o.emptyMessage || 'No records match this view.',
      });
    }
    const head = columns.map((c) => `<th class="${[c.numeric ? 'num' : '', c.hideTablet ? 'hide-tab' : ''].filter(Boolean).join(' ')}">${esc(c.label)}</th>`).join('');
    const body = rows.map((row) => {
      // Bundle 217: data-label + classes so a phone shows each row as a card.
      // The status pill goes top-right: a column marked status, else the first pill cell.
      let statusDone = columns.some((c) => c && c.status);
      const cells = (Array.isArray(row) ? row : row.cells).map((cell, i) => {
        const c = columns[i] || {};
        const html = cell === null || cell === undefined ? '—' : String(cell);
        const cls = [c.numeric ? 'num' : '', i === 0 ? 'col-first' : '', c.label === '' ? 'col-act' : '', c.hideTablet ? 'hide-tab' : ''];
        if (c.status && i > 0) cls.push('col-status');
        else if (i > 0 && !statusDone && html.indexOf('class="pill216') >= 0) { cls.push('col-status'); statusDone = true; }
        return `<td class="${cls.filter(Boolean).join(' ')}" data-label="${esc(c.label || '')}">${html}</td>`;
      }).join('');
      const attrs = row && row.rowAttrs ? ' ' + row.rowAttrs : '';
      return `<tr${attrs}>${cells}</tr>`;
    }).join('');
    return `<div class="table-wrap"><table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table></div>`;
  }

  function timeline(entries, options) {
    const o = options || {};
    const list = (entries || []).slice(0, o.max || 12);
    if (!list.length) {
      return emptyState({
        icon: 'i-activity',
        title: 'No activity yet',
        message: o.emptyMessage || 'Nothing has been recorded on this server yet.',
      });
    }
    return `<div class="timeline">${list.map((entry) => {
      const tone = severityTone(entry.severity);
      return `<div class="timeline-item sev-${tone}">
        <div class="timeline-rail"><span class="timeline-marker">${icon(entry.icon || severityIcon(entry.severity))}</span></div>
        <div class="timeline-body">
          <span class="title">${esc(entry.title || 'Event')}</span>
          ${entry.detail ? `<span class="detail">${esc(entry.detail)}</span>` : ''}
          <span class="meta">${esc(relative(entry.timestamp))}${entry.meta ? ' · ' + esc(entry.meta) : ''}</span>
        </div>
      </div>`;
    }).join('')}</div>`;
  }

  function notificationItem(n) {
    const tone = severityTone(n.severity);
    return `<article class="notification">
      <span class="timeline-marker" style="width:26px;height:26px;border:none">${icon(n.icon || severityIcon(n.severity))}</span>
      <div class="body">
        <span class="title">${esc(n.title)}</span>
        ${n.detail ? `<span class="detail">${esc(n.detail)}</span>` : ''}
        <span class="meta">${esc(relative(n.timestamp))}${n.severity ? ' · ' + esc(titleCase(n.severity)) : ''} · ${esc(tone)}</span>
      </div>
    </article>`;
  }

  function emptyState(options) {
    const o = options || {};
    return `<div class="state">
      <span class="icon-wrap">${icon(o.icon || 'i-database')}</span>
      <strong>${esc(o.title || 'Nothing here yet')}</strong>
      ${o.message ? `<p>${esc(o.message)}</p>` : ''}
      ${o.action || ''}
    </div>`;
  }

  function errorState(message, retryId) {
    return `<div class="state error">
      <span class="icon-wrap">${icon('i-alert')}</span>
      <strong>Could not load this section</strong>
      <p>${esc(message || 'The server did not return a usable response.')}</p>
      ${retryId ? `<button class="btn btn-sm" data-retry="${esc(retryId)}">${icon('i-refresh')}Retry</button>` : ''}
    </div>`;
  }

  function skeletonPanel(rows) {
    const n = rows || 4;
    let html = '<div class="skeleton-card">';
    html += '<span class="skeleton h-lg w-40"></span>';
    for (let i = 0; i < n; i += 1) html += '<span class="skeleton"></span>';
    html += '</div>';
    return `<section class="panel">${html}</section>`;
  }

  function skeletonCards(count) {
    let html = '';
    for (let i = 0; i < (count || 4); i += 1) {
      html += `<div class="metric"><span class="skeleton w-40"></span><span class="skeleton h-lg w-60" style="height:24px"></span><span class="skeleton"></span></div>`;
    }
    return html;
  }

  // Bundle 219: shimmer shaped like the page that is loading (refresh and
  // page switch). Unknown pages keep the plain panel.
  const LIST219 = ['users', 'devices', 'sessions', 'audit', 'security', 'recovery', 'actions'];
  function bootSkeleton219(viewId) {
    const sk = (cls) => `<span class="skeleton ${cls || ''}"></span>`;
    const row = (cells) => `<div class="sk219-row">${sk('sk219-circle')}<div class="sk219-lines">${sk()}${sk()}</div>${cells}</div>`;
    if (viewId === 'map') {
      return `<div class="sk219 sk219-page-map" aria-busy="true">
        <div class="sk219-chips">${sk('sk219-chip')}${sk('sk219-chip wide')}</div>
        ${sk('sk219-map')}
        <div class="sk219-legend">${sk()}${sk()}</div>
        <section class="sk219-panel">${sk('h-lg w-40')}${row(sk('sk219-pill'))}${row(sk('sk219-pill'))}</section>
      </div>`;
    }
    if (viewId === 'dashboard') {
      return `<div class="sk219 sk219-page-dashboard" aria-busy="true">
        <div class="grid grid-metrics">${skeletonCards(4)}</div>
        <section class="sk219-panel">${sk('h-lg w-40')}${sk('sk219-chart')}</section>
        <section class="sk219-panel">${sk('h-lg w-40')}${sk()}${sk('w-60')}${sk()}${sk('w-60')}</section>
      </div>`;
    }
    if (LIST219.indexOf(viewId) >= 0) {
      const cells = sk('sk219-cell') + sk('sk219-cell') + sk('sk219-cell') + sk('sk219-pill');
      let rows = '';
      for (let i = 0; i < 5; i += 1) rows += row(cells);
      return `<div class="sk219 sk219-page-list" aria-busy="true">
        <div class="sk219-search">${sk()}${sk()}</div>
        <section class="sk219-panel sk219-list">
          <div class="sk219-head">${sk()}${sk()}${sk()}${sk()}</div>
          ${rows}
        </section>
      </div>`;
    }
    return skeletonPanel(5);
  }

  function segmented(options, value, attribute) {
    const attr = attribute || 'data-segment';
    return `<div class="segmented" role="group">${options.map((o) =>
      `<button type="button" ${attr}="${esc(o.value)}" aria-pressed="${o.value === value}">${esc(o.label)}</button>`).join('')}</div>`;
  }

  function healthRow(component) {
    const info = statusInfo(component.status);
    const hasValue = component.value !== null && component.value !== undefined;
    const unit = component.unit ? (component.unit === '%' ? '%' : ' ' + component.unit) : '';
    const isPercent = component.unit === '%';
    const ratio = isPercent && hasValue ? Math.max(0, Math.min(1, Number(component.value) / 100)) : null;
    const toneVar = `var(--${info.tone === 'neutral' ? 'text-tertiary' : info.tone})`;
    const threshold = isPercent && component.threshold != null ? Number(component.threshold) / 100 : null;
    return `<div class="health-row">
      <div class="health-main">
        <span class="dot" style="width:8px;height:8px;border-radius:50%;background:${toneVar}"></span>
        <span class="label">${esc(component.label || component.key || '')}</span>
        <span class="reading">${hasValue ? esc(number(component.value)) + esc(unit) : '—'}</span>
        ${statusBadge(component.status)}
      </div>
      ${ratio !== null ? `<div class="meter ${threshold !== null ? 'threshold' : ''}" ${threshold !== null ? `style="--threshold-pos:${(threshold * 100).toFixed(1)}%"` : ''}>
        <span style="width:${(ratio * 100).toFixed(1)}%;background:${toneVar}"></span>
      </div>` : ''}
      ${(component.detail || component.updated_at || component.trend) ? `<div class="health-detail">
        ${component.trend && component.trend !== 'flat' ? `${icon(component.trend === 'up' ? 'i-activity' : 'i-activity')}<span>${esc(component.trend === 'up' ? 'rising' : 'falling')}</span>` : ''}
        ${component.detail ? `<span>${esc(component.detail)}</span>` : ''}
        ${component.updated_at ? `<span class="when">${esc(relative(component.updated_at))}</span>` : ''}
      </div>` : ''}
    </div>`;
  }

  function quickAction(options) {
    const o = options || {};
    return `<button type="button" class="quick-action" data-action="${esc(o.action || '')}">
      ${icon(o.icon || 'i-chevron-right')}
      <span class="grow">${esc(o.label)}</span>
      ${icon('i-chevron-right', 'chev')}
    </button>`;
  }

  function note(title, message, iconName) {
    return `<div class="note">
      ${icon(iconName || 'i-lock')}
      <div><strong>${esc(title)}</strong><p>${esc(message)}</p></div>
    </div>`;
  }

  function filterBar(options) {
    const o = options || {};
    const controls = [];
    if (o.search) {
      controls.push(`<input class="input" type="search" data-filter-search placeholder="${esc(o.searchPlaceholder || 'Search…')}" value="${esc(o.searchValue || '')}" aria-label="${esc(o.searchPlaceholder || 'Search')}">`);
    }
    if (o.selects) {
      o.selects.forEach((select) => {
        controls.push(`<select class="input row gap-sm" data-filter="${esc(select.key)}" aria-label="${esc(select.label)}">
          ${select.options.map((opt) => `<option value="${esc(opt.value)}" ${opt.value === select.value ? 'selected' : ''}>${esc(opt.label)}</option>`).join('')}
        </select>`);
      });
    }
    if (o.active && o.active.length) {
      controls.push(o.active.map((chip) => `<span class="filter-chip">${icon('i-filter')}${esc(chip.label)}</span>`).join(''));
      if (o.onClear) controls.push(`<button type="button" class="btn btn-ghost btn-sm" data-clear-filters>Clear</button>`);
    }
    controls.push(`<span class="grow"></span><span class="type-caption" data-count></span>`);
    return `<div class="filter-bar">${controls.join('')}</div>`;
  }

  // ------------------------------------------------------------ Bundle 216
  // Crown A (same shape as the app's CrownPainter216), person cells with the
  // real photo (loaded by app.js through the admin avatar route), tone pills
  // and chips. Every server value still goes through esc().
  const TONES_216 = {
    green: ['#12351F', '#4ADE80'], red: ['#3B1518', '#F87171'], orange: ['#3A2A10', '#FBBF24'],
    blue: ['#16274A', '#7AA7FF'], grey: ['#262C36', '#C0C7D1'], gold: ['#3A2F0E', '#F5C542'],
    purple: ['#2A2245', '#B49CFF'], teal: ['#0F3330', '#5EEAD4'],
  };

  // Bundle 217 / TODO 290: the crown (same drawing as the app's paintCrown217).
  // Since 290 it is crown C ("round soft crown"): units width 100, height 50,
  // rounded points, no pearls. Each admin level has its own colours; owner
  // keeps the 217 gold (#FFE985 / #FFC727 / #E09A12 / #EFA300 / #A87400,
  // gems #E5484D + #3B82F6). Unknown level = gold.
  const CROWN_290 = {
    owner: { id: 'cr217', top: '#FFE985', fill: '#FFC727', low: '#E09A12', band: '#EFA300', line: '#A87400', gem: '#E5484D', side: '#3B82F6' },
    operator: { id: 'cr217-operator', top: '#DBEAFE', fill: '#60A5FA', low: '#2563EB', band: '#1D4ED8', line: '#1E3A8A', gem: '#FBBF24', side: '#E0F2FE' },
    security: { id: 'cr217-security', top: '#FECACA', fill: '#F87171', low: '#DC2626', band: '#B91C1C', line: '#7F1D1D', gem: '#FBBF24', side: '#FFFFFF' },
    auditor: { id: 'cr217-auditor', top: '#FFFFFF', fill: '#D5DCE6', low: '#97A3B6', band: '#8C98AB', line: '#4B5565', gem: '#3B82F6', side: '#E0F2FE' },
  };
  // TODO 290: 'owner' / 'operator' / 'security' / 'auditor', else 'owner'.
  function crownRole290(role) {
    const r = String(role || '').trim().toLowerCase();
    return CROWN_290[r] ? r : 'owner';
  }
  function crown216(width, role) {
    const w = Math.max(10, Math.min(32, Number(width) || 14));
    const c = CROWN_290[crownRole290(role)];
    return `<svg class="crown216" width="${(w * 1.12).toFixed(1)}" height="${(w * 0.53).toFixed(1)}" viewBox="-6 -2 112 53" aria-label="Admin" role="img">
      <defs><linearGradient id="${c.id}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${c.top}"/><stop offset=".5" stop-color="${c.fill}"/><stop offset="1" stop-color="${c.low}"/></linearGradient></defs>
      <g stroke="${c.line}" stroke-width="3.5" stroke-linejoin="round">
        <path d="M9,42 L6,17 Q6,11 11,14 L29,27 L45,7 Q50,1 55,7 L71,27 L89,14 Q94,11 94,17 L91,42 Z" fill="url(#${c.id})"/>
        <rect x="6" y="37" width="88" height="12" rx="6" fill="${c.band}"/>
      </g>
      <g stroke="${c.line}" stroke-width="1.75">
        <ellipse cx="50" cy="43" rx="5" ry="4.2" fill="${c.gem}"/>
        <ellipse cx="27" cy="43" rx="3.5" ry="3" fill="${c.side}"/><ellipse cx="73" cy="43" rx="3.5" ry="3" fill="${c.side}"/>
      </g>
      <circle cx="48.3" cy="41.5" r="1" fill="#fff" fill-opacity=".65"/><circle cx="25.8" cy="42" r=".7" fill="#fff" fill-opacity=".65"/><circle cx="71.8" cy="42" r=".7" fill="#fff" fill-opacity=".65"/>
      <path d="M19 31L16 20" stroke="#fff" stroke-opacity=".47" stroke-width="3" stroke-linecap="round"/>
    </svg>`;
  }

  // Bundle 217: mix two #rrggbb colours (t = 0 -> a, 1 -> b).
  function mix217(a, b, t) {
    const p = (h) => [1, 3, 5].map((i) => parseInt(String(h).slice(i, i + 2), 16));
    const x = p(a); const y = p(b);
    return '#' + x.map((v, i) => Math.round(v + (y[i] - v) * t).toString(16).padStart(2, '0')).join('');
  }

  function initials216(name) {
    const parts = String(name || '?').trim().split(/\s+/).filter(Boolean);
    if (!parts.length) return '?';
    return (parts[0].charAt(0) + (parts.length > 1 ? parts[parts.length - 1].charAt(0) : '')).toUpperCase();
  }

  // avatar216({ userId, name, hasAvatar, admin, blocked, online, size })
  // TODO 290: admin = true or the admin level ('operator' ...): the crown and
  // the photo ring take that level's colour (true / unknown = owner gold).
  function avatar216(o) {
    const size = Number(o.size) || 32;
    const role = o.admin ? crownRole290(typeof o.admin === 'string' ? o.admin : '') : '';
    const cls = ['av216', o.admin ? 'is-admin' : '', o.admin ? `is-admin-${role}` : '', o.blocked ? 'is-blocked' : ''].filter(Boolean).join(' ');
    const photo = o.hasAvatar && o.userId
      ? ` data-avatar-user="${esc(o.userId)}"` : '';
    return `<span class="${cls}" style="--av:${size}px"${photo} title="${esc(o.name || '')}">
      <span class="av216-in">${esc(initials216(o.name))}</span>
      ${o.admin && !o.blocked ? `<span class="av216-crown">${crown216(size * 0.55, role)}</span>` : ''}
      ${o.online ? '<span class="av216-dot"></span>' : ''}
    </span>`;
  }

  // person216({ userId, name, sub, hasAvatar, admin, blocked, online, size })
  function person216(o) {
    return `<div class="p216">${avatar216(o)}<span class="p216-t"><b>${esc(o.name || '—')}</b>${o.sub ? `<small>${esc(o.sub)}</small>` : ''}</span></div>`;
  }

  function pill216(label, tone) {
    const t = TONES_216[tone] || TONES_216.grey;
    return `<span class="pill216" style="background:${t[0]};color:${t[1]}">${esc(label)}</span>`;
  }

  // chips216(list, active, attr) -> one row of chips; list = [{ value, label }]
  function chips216(list, active, attr) {
    return `<div class="chips216">${list.map((c) => `<button type="button" class="chip216 ${c.value === active ? 'on' : ''}" ${attr}="${esc(c.value)}">${esc(c.label)}</button>`).join('')}</div>`;
  }

  // Short, plain "when": Online now / 5 min ago / Yesterday / 2 Nov.
  function when216(value) {
    if (!value) return '—';
    const t = new Date(value);
    if (isNaN(t.getTime())) return '—';
    const diff = (Date.now() - t.getTime()) / 1000;
    if (diff < 120) return 'Online now';
    if (diff < 3600) return `${Math.floor(diff / 60)} min ago`;
    const today = new Date(); today.setHours(0, 0, 0, 0);
    if (t >= today) return `${Math.floor(diff / 3600)} h ago`;
    const yesterday = new Date(today.getTime() - 86400000);
    if (t >= yesterday) return 'Yesterday';
    if (diff < 7 * 86400) return `${Math.floor(diff / 86400)} days ago`;
    return t.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
  }

  const LEVELS_216 = { owner: 'Owner', operator: 'Operator', security: 'Security', auditor: 'Auditor' };
  const LINKS_216 = {
    server: { label: 'Server', tone: 'blue', color: '#3B82C4' }, // Bundle 272: server lines blue
    wifi_direct: { label: 'Wi-Fi Direct', tone: 'orange', color: '#F5A524' },
    mesh: { label: 'Mesh', tone: 'purple', color: '#8B5CF6' },
    lan: { label: 'Local Wi-Fi', tone: 'teal', color: '#2DD4BF' },
  };

  // Bundle 276: a person's name. A name the phone gave itself ("My Android
  // Phone (1a2b)") is never shown: display name -> username (no @) -> "LinkUp user".
  function isDefaultPhoneName276(v) {
    const n = String(v || '').trim().toLowerCase();
    return !n || n === 'my linkup phone' || n === 'phone' || n === 'linkup user' || n.indexOf('my android phone') === 0;
  }
  function name276(o) {
    const x = o || {};
    const d = String(x.display_name || x.displayName || x.name || '').trim();
    if (!isDefaultPhoneName276(d)) return d;
    const u = String(x.username || '').trim();
    return u || 'LinkUp user';
  }
  // Bundle 276: a phone: its own name when a person typed one, else "<name>'s phone".
  function phone276(phoneName, owner) {
    const p = String(phoneName || '').trim();
    if (!isDefaultPhoneName276(p)) return p;
    const o = String(owner || '').trim();
    return o && o !== 'LinkUp user' ? `${o}'s phone` : 'Phone';
  }

  LL.ui = {
    esc, icon, badge, statusBadge, severityBadge, statusInfo, severityTone, severityIcon, token,
    panel, sectionHead, metricCard, statRow, headline, table, timeline, notificationItem,
    emptyState, errorState, skeletonPanel, skeletonCards, bootSkeleton219, segmented, healthRow, quickAction, note, filterBar,
    crown216, crownRole290, mix217, avatar216, person216, pill216, chips216, when216, initials216, TONES_216, LEVELS_216, LINKS_216,
    isDefaultPhoneName276, name276, phone276,
  };

  LL.fmt = { number, bytes, relative, dateTime, bucket, duration, titleCase };
})();
