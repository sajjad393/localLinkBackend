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
    const head = columns.map((c) => `<th class="${c.numeric ? 'num' : ''}">${esc(c.label)}</th>`).join('');
    const body = rows.map((row) => {
      const cells = (Array.isArray(row) ? row : row.cells).map((cell, i) =>
        `<td class="${columns[i] && columns[i].numeric ? 'num' : ''}">${cell === null || cell === undefined ? '—' : cell}</td>`).join('');
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

  LL.ui = {
    esc, icon, badge, statusBadge, severityBadge, statusInfo, severityTone, severityIcon, token,
    panel, sectionHead, metricCard, statRow, headline, table, timeline, notificationItem,
    emptyState, errorState, skeletonPanel, skeletonCards, segmented, healthRow, quickAction, note, filterBar,
  };

  LL.fmt = { number, bytes, relative, dateTime, bucket, duration, titleCase };
})();
