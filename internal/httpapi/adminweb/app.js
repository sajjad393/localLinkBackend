/* ===========================================================================
   LocalLink admin console — shell.

   Owns authentication, routing, the sidebar/topbar chrome, global search, the
   notification centre, theming, confirmations and the polling loop. Views live
   in views.js and receive this module's context so they can fetch and act.
   =========================================================================== */
(function () {
  'use strict';

  const ui = LL.ui;
  const fmt = LL.fmt;

  const TOKEN_KEY = 'locallink_admin_token';
  const IDENTITY_KEY = 'locallink_admin_identity';
  const THEME_KEY = 'locallink_admin_theme';
  const RANGE_KEY = 'locallink_admin_range';

  // Role capabilities mirror the Flutter admin permissions model.
  const PERMISSIONS = {
    read: ['owner', 'operator', 'security', 'auditor'],
    manageUsers: ['owner'],
    manageDevices: ['owner', 'operator'],
    manageSessions: ['owner'],
    manageSettings: ['owner'],
    backup: ['owner'],
    manageRecovery: ['owner', 'operator', 'security'],
    manageNetworkPolicy: ['owner', 'operator'],
    managePrivacy: ['owner'],
    // Reading what users said to each other. Owner only: the most sensitive
    // capability in this console, and every read is audit-logged server-side.
    readConversations: ['owner'],
  };

  const RANGES = ['24h', '7d', '30d', '90d'];

  const state = {
    // Bundle 211: stays signed in after the tab closes (ends only on Sign out,
    // a revoke from Sessions, or an admin password reset).
    token: localStorage.getItem(TOKEN_KEY) || '',
    session: null,
    view: 'dashboard',
    analyticsRange: RANGES.indexOf(localStorage.getItem(RANGE_KEY)) >= 0 ? localStorage.getItem(RANGE_KEY) : '24h',
    filters: { users: { search: '', status: '', admin: '' }, devices: { search: '', presence: '' }, audit: { search: '' } },
      // Latest /api/v1/admin/summary payload, used for the sidebar counts.
      // Empty until the server answers, so no figure is invented here.
      summary: {},
    refreshSeconds: 15,
    notifications: [],
    unread: 0,
    busy: false,
    rateLimitedUntil: 0,
    topology: null,
  };

  const $ = (id) => document.getElementById(id);

  // ------------------------------------------------------------------- API
  async function api(path, options) {
    const opts = options || {};
    const headers = Object.assign({ Accept: 'application/json' }, opts.headers || {});
    if (state.token) headers.Authorization = 'Bearer ' + state.token;
    let body = opts.body;
    if (body && typeof body !== 'string') {
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify(body);
    }
    const response = await fetch(path, { method: opts.method || 'GET', headers, body, signal: opts.signal });
    if (response.status === 401) {
      handleExpiry();
      throw new Error('Your admin session expired. Sign in again.');
    }
    if (response.status === 429) {
      // The server throttles admin traffic per source address. Back off rather
      // than hammering: the console keeps the data it already has on screen.
      const retryAfter = Number(response.headers.get('Retry-After')) || 60;
      noteRateLimit(retryAfter);
      const throttled = new Error('The server is rate limiting this session.');
      throttled.code = 429;
      throttled.retryAfter = retryAfter;
      throw throttled;
    }
    let data = null;
    const text = await response.text();
    if (text) {
      try { data = JSON.parse(text); } catch (err) { data = null; }
    }
    if (!response.ok) {
      const message = (data && (data.error || data.message)) || `Request failed (${response.status})`;
      const failure = new Error(message);
      failure.status = response.status; // Bundle 219: boot tells 5xx (retry) from other errors.
      throw failure;
    }
    return data;
  }

  // Mutating calls carry an idempotency key so a double submit (or a retry
  // after a dropped connection) cannot apply the same administrative action
  // twice. The server enforces this for recovery decisions.
  function newIdempotencyKey() {
    const bytes = new Uint8Array(16);
    if (window.crypto && window.crypto.getRandomValues) window.crypto.getRandomValues(bytes);
    else for (let i = 0; i < bytes.length; i += 1) bytes[i] = Math.floor(Math.random() * 256);
    return Array.from(bytes).map((b) => b.toString(16).padStart(2, '0')).join('');
  }

  function post(path, body) {
    return api(path, { method: 'POST', body: body, headers: { 'Idempotency-Key': newIdempotencyKey() } });
  }

  function noteRateLimit(seconds) {
    state.rateLimitedUntil = Date.now() + Math.min(300, Math.max(5, seconds)) * 1000;
    renderRateLimitBanner();
  }

  // Media endpoints need the bearer header, which <img>/<audio> elements
  // cannot send (the console has no cookies). Views therefore download the
  // bytes through this helper and render from same-tab blob: object URLs.
  async function blob(path) {
    const headers = { Accept: '*/*' };
    if (state.token) headers.Authorization = 'Bearer ' + state.token;
    const response = await fetch(path, { method: 'GET', headers });
    if (response.status === 401) {
      handleExpiry();
      throw new Error('Your admin session expired. Sign in again.');
    }
    if (!response.ok) throw new Error(`Media request failed (${response.status})`);
    return URL.createObjectURL(await response.blob());
  }

  function rateLimited() {
    return state.rateLimitedUntil > Date.now();
  }

  function renderRateLimitBanner() {
    const existing = document.getElementById('rate-limit-banner');
    if (!rateLimited()) {
      state.rateLimitedUntil = 0;
      if (existing) existing.remove();
      return;
    }
    const seconds = Math.ceil((state.rateLimitedUntil - Date.now()) / 1000);
    // Bundle 219 (mock web03): same bar as "Can't reach the server"; the login is kept.
    const text = `Trying again in ${seconds} s…`;
    if (existing) {
      existing.querySelector('.bar219-text span').textContent = text;
      return;
    }
    const banner = document.createElement('div');
    banner.id = 'rate-limit-banner';
    banner.className = 'bar219';
    banner.setAttribute('role', 'status');
    banner.innerHTML = ui.icon('i-pulse') + '<div class="bar219-text"><strong>Server is busy</strong><span></span></div>';
    banner.querySelector('.bar219-text span').textContent = text;
    const content = document.getElementById('view');
    content.parentElement.insertBefore(banner, content);
  }

  function can(permission) {
    const roles = PERMISSIONS[permission];
    if (!roles) return true;
    return roles.indexOf(state.session ? state.session.role : '') >= 0;
  }

  function permissionSummary() {
    const granted = Object.keys(PERMISSIONS).filter((p) => can(p));
    const labels = {
      read: 'view operational data', manageUsers: 'manage accounts', manageDevices: 'manage devices',
      manageSessions: 'revoke sessions', manageSettings: 'change server settings', backup: 'create database backups',
      manageRecovery: 'review recovery requests', manageNetworkPolicy: 'change transport policy',
      managePrivacy: 'review privacy policy', readConversations: 'read user conversations',
    };
    return 'This role may ' + granted.map((p) => labels[p] || p).join(', ') + '.';
  }

  // ---------------------------------------------------------------- toasts
  // Bundle 220: actions that now go to all phones say so.
  const SENT_220 = ' \u00b7 Sent to all phones \u00b7 see Admin actions';

  function toast(message, tone) {
    const host = $('toast-host');
    const node = document.createElement('div');
    node.className = 'toast' + (tone === 'error' ? ' error' : '');
    node.innerHTML = ui.icon(tone === 'error' ? 'i-alert' : 'i-check') + '<span></span>';
    node.querySelector('span').textContent = message;
    host.appendChild(node);
    setTimeout(() => { node.style.opacity = '0'; node.style.transition = 'opacity 160ms ease'; }, 2800);
    setTimeout(() => node.remove(), 3100);
  }

  // --------------------------------------------------------------- confirm
  function confirmDialog(options) {
    const o = options || {};
    return new Promise((resolve) => {
      const host = $('modal-host');
      host.innerHTML = `<div class="modal-scrim" role="dialog" aria-modal="true">
        <div class="modal">
          <h2>${ui.esc(o.title || 'Confirm')}</h2>
          <p>${ui.esc(o.message || '')}</p>
          <div class="modal-actions">
            <button class="btn" data-modal="cancel">${ui.esc(o.cancelLabel || 'Cancel')}</button>
            <button class="btn ${o.destructive ? 'btn-danger' : 'btn-primary'}" data-modal="confirm">${ui.esc(o.confirmLabel || 'Confirm')}</button>
          </div>
        </div>
      </div>`;
      const finish = (value) => { host.innerHTML = ''; resolve(value); };
      host.querySelector('[data-modal="cancel"]').addEventListener('click', () => finish(false));
      host.querySelector('[data-modal="confirm"]').addEventListener('click', () => finish(true));
      host.querySelector('.modal-scrim').addEventListener('click', (event) => {
        if (event.target.classList.contains('modal-scrim')) finish(false);
      });
      host.querySelector('[data-modal="confirm"]').focus();
    });
  }

  // Bundle 222: the one-time code the admin gives to the person (in person).
  function showCode222(code, expiresAt) {
    return new Promise((resolve) => {
      const host = $('modal-host');
      const until = expiresAt ? new Date(expiresAt) : null;
      host.innerHTML = `<div class="modal-scrim" role="dialog" aria-modal="true">
        <div class="modal">
          <h2>One-time code</h2>
          <p>Give this code to the person yourself (not by chat). They type it with a new password.</p>
          <div class="code222" data-code222>${ui.esc(code)}</div>
          <p class="type-caption">${until && !isNaN(until.getTime()) ? 'Works until ' + ui.esc(until.toLocaleString()) + ' \u00b7 ' : ''}Use "New code" if it ends.</p>
          <div class="modal-actions">
            <button class="btn" data-modal="copy">Copy</button>
            <button class="btn btn-primary" data-modal="confirm">Done</button>
          </div>
        </div>
      </div>`;
      const finish = () => { host.innerHTML = ''; resolve(); };
      host.querySelector('[data-modal="copy"]').addEventListener('click', () => {
        try { navigator.clipboard.writeText(code); toast('Code copied'); } catch (_) { /* no clipboard */ }
      });
      host.querySelector('[data-modal="confirm"]').addEventListener('click', finish);
      host.querySelector('[data-modal="confirm"]').focus();
    });
  }

  // ---------------------------------------------------------------- overlays
  function closeOverlays() {
    $('overlays').innerHTML = '';
    $('drawer-scrim').classList.add('hidden');
    $('app').classList.remove('drawer-open');
    const identity = $('identity-button');
    if (identity) identity.setAttribute('aria-expanded', 'false');
  }

  function renderNotifications() {
    const items = state.notifications;
    const host = $('overlays');
    if (!host.querySelector('[data-overlay="notifications"]')) return;
    host.innerHTML = `<div class="panel-pop" data-overlay="notifications">
      <div class="pop-head">
        <strong>Notifications</strong>
        <button class="btn btn-ghost btn-sm" data-nav="audit">${ui.icon('i-audit')}Audit log</button>
      </div>
      ${items.length
        ? `<div style="display:grid;gap:8px">${items.slice(0, 10).map((n) => ui.notificationItem(n)).join('')}</div>`
        : ui.emptyState({ icon: 'i-bell', title: 'Nothing to report', message: 'Security and operational events appear here as the server records them.' })}
    </div>`;
  }

  async function openNotifications() {
    closeOverlays();
    const host = $('overlays');
    host.innerHTML = `<div class="panel-pop" data-overlay="notifications">
      <div class="pop-head"><strong>Notifications</strong></div>${ui.skeletonPanel(2)}</div>`;
    try {
      const data = await api('/api/v1/admin/security-summary');
      const events = Array.isArray(data.recent_events) ? data.recent_events : [];
      state.notifications = events.slice(0, 10).map((e) => ({
        icon: ui.severityIcon(e.severity), title: fmt.titleCase(e.action), detail: e.detail,
        timestamp: e.created_at, severity: e.severity,
      }));
      const counts = data.counts || {};
      setUnread(Number(counts.critical_alerts_24h || 0) + Number(counts.failed_logins_24h || 0));
      renderNotifications();
    } catch (err) {
      host.innerHTML = `<div class="panel-pop" data-overlay="notifications">
        <div class="pop-head"><strong>Notifications</strong></div>${ui.errorState(err.message)}</div>`;
    }
  }

  function setUnread(count) {
    state.unread = count;
    const badge = $('notifications-count');
    badge.textContent = count > 99 ? '99+' : String(count);
    badge.classList.toggle('hidden', count <= 0);
  }

  function openIdentityMenu() {
    closeOverlays();
    const session = state.session || { username: '—', role: '—', expires_at: null };
    const host = $('overlays');
    host.innerHTML = `<div class="menu" role="menu" data-overlay="identity">
      <div class="menu-identity menu-identity217">
        ${ui.avatar216({ userId: session.user_id || '', name: session.username || '?', hasAvatar: !!session.has_avatar, admin: true, size: 40 })}
        <span class="menu-who"><strong>${ui.esc(session.username || '—')}</strong>
        <span>${ui.esc(fmt.titleCase(session.role))}${session.username ? ' · signed in until you sign out' : ''}</span></span>
      </div>
      <button class="menu-item" data-menu="refresh" role="menuitem">${ui.icon('i-refresh')}Refresh data</button>
      <button class="menu-item" data-menu="theme" role="menuitem">${ui.icon('i-sun')}Toggle light / dark</button>
      <button class="menu-item" data-menu="privacy" role="menuitem">${ui.icon('i-privacy')}Privacy & access</button>
      <div class="menu-divider" role="separator"></div>
      <button class="menu-item danger" data-menu="logout" role="menuitem">${ui.icon('i-logout')}Sign out</button>
    </div>`;
    $('identity-button').setAttribute('aria-expanded', 'true');
  }

  function renderSearchResults(query) {
    const host = $('overlays');
    const term = query.trim().toLowerCase();
    if (!term) { host.innerHTML = ''; return; }
    const matches = Object.keys(LL.views)
      .map((key) => LL.views[key])
      .filter((v) => v.title.toLowerCase().indexOf(term) >= 0 || v.subtitle.toLowerCase().indexOf(term) >= 0);
    const entityLinks = [
      { label: `Search users for “${query.trim()}”`, view: 'users', sub: 'Accounts and profiles', icon: 'i-users' },
      { label: `Search devices for “${query.trim()}”`, view: 'devices', sub: 'Registered hardware', icon: 'i-devices' },
    ];
    host.innerHTML = `<div class="search-panel" data-overlay="search">
      <div class="pop-head"><strong>Results for “${ui.esc(query.trim())}”</strong>
        <button class="btn btn-ghost btn-sm" data-overlay-close>${ui.icon('i-close')}Close</button></div>
      ${matches.length ? matches.map((m) => `<button class="search-result" data-nav="${ui.esc(m.id)}">
          ${ui.icon(m.icon)}<span class="grow"><strong>${ui.esc(m.title)}</strong><br><span class="sub">${ui.esc(m.subtitle)}</span></span>
          ${ui.icon('i-chevron-right')}
        </button>`).join('') : '<p class="type-caption">No module matches that term.</p>'}
      <div><span class="type-label">Go to</span></div>
      ${entityLinks.map((e) => `<button class="search-result" data-nav="${e.view}" data-prefill="${ui.esc(query.trim())}">
          ${ui.icon(e.icon)}<span class="grow"><strong>${ui.esc(e.label)}</strong><br><span class="sub">${ui.esc(e.sub)}</span></span>
          ${ui.icon('i-chevron-right')}
        </button>`).join('')}
    </div>`;
  }

  // ----------------------------------------------------------------- theme
  function applyTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem(THEME_KEY, theme);
    const button = $('theme-button');
    if (button) {
      button.innerHTML = ui.icon(theme === 'dark' ? 'i-sun' : 'i-moon');
      button.title = theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme';
      button.setAttribute('aria-label', button.title);
    }
    if (LL.charts && LL.charts.redrawAll) LL.charts.redrawAll();
  }

  function currentTheme() {
    return document.documentElement.getAttribute('data-theme') || 'light';
  }

  function preferredTheme() {
    const stored = localStorage.getItem(THEME_KEY);
    if (stored === 'light' || stored === 'dark') return stored;
    // Bundle 216: the panel is dark by default (same as mock v4).
    return 'dark';
  }

  // ---------------------------------------------------------------- topology
  function openTopology() {
    const host = $('modal-host');
    host.innerHTML = `<div class="modal-scrim" role="dialog" aria-modal="true">
      <div class="modal" style="width:min(860px,100%)">
        <div class="pop-head">
          <div><h2>Network topology</h2><span class="type-caption">Live presence: server, transports and registered devices</span></div>
          <button class="btn btn-icon" data-modal="close">${ui.icon('i-close')}</button>
        </div>
        <div class="skeleton w-60" style="height:18px"></div>
        <p class="type-caption">Loading device presence…</p>
      </div>
    </div>`;
    host.querySelector('[data-modal="close"]').addEventListener('click', () => { host.innerHTML = ''; });
    host.querySelector('.modal-scrim').addEventListener('click', (event) => {
      if (event.target.classList.contains('modal-scrim')) host.innerHTML = '';
    });

    Promise.all([api('/api/v1/admin/devices?limit=200'), api('/api/v1/admin/network')])
      .then(([devices, network]) => {
        const list = Array.isArray(devices.devices) ? devices.devices : [];
        const modal = host.querySelector('.modal');
        modal.innerHTML = `<div class="pop-head">
            <div><h2>Network topology</h2><span class="type-caption">Live presence from the server; no inferred links</span></div>
            <button class="btn btn-icon" data-modal="close">${ui.icon('i-close')}</button>
          </div>
          <div class="row gap-md" style="flex-wrap:wrap">
            ${ui.badge(`${list.filter((d) => d.online).length} online`, 'success', { dot: true })}
            ${ui.badge(`${list.length} registered`, 'neutral')}
            ${ui.badge(`${network.server_connected} server links`, 'primary')}
            ${ui.badge(`${network.lan_available} on LAN`, 'info')}
            ${ui.badge(`${network.mesh_available} mesh capable`, 'warning')}
          </div>
          <div class="chart-canvas-wrap" style="height:360px"><canvas id="topology-canvas"></canvas></div>
          <p class="type-caption">Devices are listed left and right of this server. A green ring and a solid line mean the device holds a live server session; a grey ring and a dashed line mean it is offline. Lines show the server link only.</p>`;
        modal.querySelector('[data-modal="close"]').addEventListener('click', () => { host.innerHTML = ''; });
        drawTopology($('topology-canvas'), list);
      })
      .catch((err) => {
        host.querySelector('.modal').innerHTML = `<div class="pop-head"><h2>Network topology</h2>
          <button class="btn btn-icon" data-modal="close">${ui.icon('i-close')}</button></div>${ui.errorState(err.message)}`;
        host.querySelector('[data-modal="close"]').addEventListener('click', () => { host.innerHTML = ''; });
      });
  }

  function drawTopology(canvas, devices) {
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const width = canvas.parentElement.clientWidth;

    // Devices flank the server: the first half of the list goes down the left
    // column, the rest down the right column. Online devices sort first so the
    // live links sit nearest the top of each column.
    const all = devices.slice().sort((a, b) => {
      if ((a.online ? 1 : 0) !== (b.online ? 1 : 0)) return a.online ? -1 : 1;
      return String(a.name || a.id || '').localeCompare(String(b.name || b.id || ''));
    });
    const leftCount = Math.ceil(all.length / 2);
    const rightCount = all.length - leftCount;
    const rows = Math.max(1, Math.max(leftCount, rightCount));

    const nodeR = 16;
    const serverR = 26;
    const rowGap = 52;
    const height = Math.max(360, rows * rowGap + 96);
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    canvas.style.width = width + 'px';
    canvas.style.height = height + 'px';
    canvas.parentElement.style.height = height + 'px';
    const ctx = canvas.getContext('2d');
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);

    const cx = width / 2;
    const cy = height / 2;
    const font = getComputedStyle(document.body).fontFamily;
    const primary = ui.token('primary', '#1e5bff');
    const success = ui.token('success', '#0f7b52');
    const muted = ui.token('text-tertiary', '#8b93a1');
    const border = ui.token('border', '#e2e6ec');
    const surface = ui.token('surface', '#ffffff');
    const text = ui.token('text-secondary', '#5b6472');
    const textStrong = ui.token('text-primary', '#111827');

    // hexToRgba is local and forgiving: it accepts '#rgb'/'#rrggbb' and falls
    // back to the input string when the colour is not a hex literal at all.
    const rgba = (hex, alpha) => {
      let m = /^#?([0-9a-f]{3})$/i.exec(String(hex).trim());
      if (m) m = [m[0], m[1].split('').map((c) => c + c).join('')];
      else m = /^#?([0-9a-f]{6})$/i.exec(String(hex).trim());
      if (!m) return String(hex);
      const n = parseInt(m[1], 16);
      return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${alpha})`;
    };

    const marginX = Math.max(150, Math.min(220, width * 0.24));
    const leftX = marginX;
    const rightX = width - marginX;
    const yFor = (index, count) => cy + (index - (count - 1) / 2) * rowGap;

    const drawLink = (x, y, onlineFlag) => {
      const dx = x - cx;
      const dy = y - cy;
      const dist = Math.hypot(dx, dy) || 1;
      const ux = dx / dist;
      const uy = dy / dist;
      ctx.beginPath();
      ctx.moveTo(cx + ux * (serverR + 6), cy + uy * (serverR + 6));
      ctx.lineTo(x - ux * (nodeR + 4), y - uy * (nodeR + 4));
      ctx.strokeStyle = onlineFlag ? rgba(success, 0.55) : muted;
      ctx.lineWidth = onlineFlag ? 2 : 1.4;
      ctx.setLineDash(onlineFlag ? [] : [4, 4]);
      ctx.stroke();
      ctx.setLineDash([]);
    };

    const drawDevice = (x, y, device, side) => {
      drawLink(x, y, !!device.online);
      // Visible ring: surface fill with a 3px coloured stroke, then a solid
      // centre dot. Green when the device holds a live server session.
      ctx.beginPath();
      ctx.arc(x, y, nodeR, 0, Math.PI * 2);
      ctx.fillStyle = surface;
      ctx.fill();
      ctx.lineWidth = 3;
      ctx.strokeStyle = device.online ? success : border;
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(x, y, 5, 0, Math.PI * 2);
      ctx.fillStyle = device.online ? success : muted;
      ctx.fill();
      // Label outside the ring: name, then the presence word beneath it.
      const label = String(device.name || device.id || '').slice(0, 22) || 'device';
      const lx = side === 'left' ? x - nodeR - 12 : x + nodeR + 12;
      ctx.textAlign = side === 'left' ? 'right' : 'left';
      ctx.textBaseline = 'middle';
      ctx.font = '600 11px ' + font;
      ctx.fillStyle = device.online ? textStrong : muted;
      ctx.fillText(label, lx, y - 7);
      ctx.font = '10px ' + font;
      ctx.fillStyle = device.online ? success : muted;
      ctx.fillText(device.online ? 'online' : 'offline', lx, y + 8);
    };

    all.forEach((device, index) => {
      if (index < leftCount) drawDevice(leftX, yFor(index, leftCount), device, 'left');
      else drawDevice(rightX, yFor(index - leftCount, rightCount), device, 'right');
    });

    // Server: halo ring plus a solid node, centred between the two columns.
    ctx.beginPath();
    ctx.arc(cx, cy, serverR + 8, 0, Math.PI * 2);
    ctx.strokeStyle = rgba(primary, 0.28);
    ctx.lineWidth = 3;
    ctx.stroke();
    ctx.beginPath();
    ctx.arc(cx, cy, serverR, 0, Math.PI * 2);
    ctx.fillStyle = primary;
    ctx.fill();
    ctx.fillStyle = '#ffffff';
    ctx.font = '600 11px ' + font;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText('Server', cx, cy);
    ctx.fillStyle = muted;
    ctx.font = '10px ' + font;
    ctx.fillText('this server', cx, cy + serverR + 18);

    if (!all.length) {
      ctx.fillStyle = muted;
      ctx.font = '12px ' + font;
      ctx.textAlign = 'center';
      ctx.fillText('No devices registered yet', cx, cy + serverR + 40);
    }
  }

  // ------------------------------------------------------------------ render
  function moduleList() {
    return Object.keys(LL.views).map((key) => LL.views[key]);
  }

  // A module may declare which server figure belongs beside its name, such as

  // Devices or active sessions. The number comes from the summary the shell

  // already fetched; while it is unknown the badge is simply absent.

  function navCount(view) {

    if (!view.navCount) return null;

    const value = state.summary ? state.summary[view.navCount] : null;

    return typeof value === 'number' && isFinite(value) ? value : null;

  }


  function renderNav() {
    const groups = {};
    moduleList().forEach((view) => {
      const group = view.group || 'Modules';
      groups[group] = groups[group] || [];
      groups[group].push(view);
    });
    const restricted = (view) => view.roles && view.roles.indexOf(state.session.role) < 0;
    $('nav').innerHTML = Object.keys(groups).map((group) => `
      <div class="nav-group">
        <div class="nav-group-title">${ui.esc(group)}</div>
        ${groups[group].map((view) => `<button class="nav-item ${view.id === state.view ? 'active' : ''}"
            data-nav="${ui.esc(view.id)}" title="${ui.esc(view.title)}">
          ${ui.icon(view.icon)}
          <span class="nav-label">${ui.esc(view.title)}</span>
          ${view.isNew ? '<span class="nav-new216">New</span>' : ''}
          ${view.roles ? `<span class="nav-meta" title="${restricted(view) ? 'Needs a higher level' : ''}">${ui.esc((view.roles || []).map((r) => fmt.titleCase(r)).join('/'))}</span>` : ''}
          ${navCount(view) !== null ? `<span class="nav-count">${navCount(view) > 999 ? '999+' : String(navCount(view))}</span>` : ''}

          ${view.id === 'security' && state.unread > 0 ? `<span class="nav-badge">${state.unread > 99 ? '99+' : state.unread}</span>` : ''}
        </button>`).join('')}
      </div>`).join('');
  }

  function permissionPanels() {
    return `${ui.note('This module needs a higher role',
      `Your admin session has the ${fmt.titleCase(state.session.role)} role, which cannot open this module. Ask an owner to sign in, or use a role with the required access.`,
      'i-shield')}`;
  }

  async function mountView(viewId, options) {
    const opts = options || {};
    const view = LL.views[viewId] || LL.views.dashboard;
    // Poll refreshes patch data in place: no full unmount, no chart teardown,
    // and never closing an overlay the operator may be using.
    const canPatch = opts.refresh && typeof view.refresh === 'function';
    state.view = viewId;
    if (!canPatch) closeOverlays();
    if (!canPatch) LL.charts.reset();

    const restricted = view.roles && view.roles.indexOf(state.session.role) < 0;
    $('crumb-view').textContent = view.title;
    $('crumb-sub').textContent = view.subtitle;
    document.title = `${view.title} · LocalLink Admin`;
    renderNav();
    if (!restricted) localStorage.setItem('locallink_admin_view', viewId);

    const target = $('view');
    if (restricted) {
      target.innerHTML = `<div class="section">${permissionPanels()}</div>`;
      return;
    }

    const context = {
      api,
      blob,
      state,
      session: state.session,
      role: state.session.role,
      can,
      permissionSummary,
      toast,
      confirm: confirmDialog,
      setRefreshSeconds(seconds) {
        const value = Number(seconds);
        if (isFinite(value) && value > 0) state.refreshSeconds = Math.min(120, Math.max(5, value));
      },
      refresh: () => mountView(state.view),
      navigate: (id) => mountView(id),
    };

    if (canPatch) {
      try {
        if (await view.refresh(target, context)) return;
      } catch (err) {
        // A failed patch falls through to the normal (quiet) mount path;
        // the operator loses nothing either way.
      }
    }

    const previous = target.innerHTML;
    if (!opts.quiet) target.innerHTML = ui.bootSkeleton219(viewId);
    if (rateLimited()) {
      renderRateLimitBanner();
      if (opts.quiet && previous) target.innerHTML = previous;
      else if (!opts.quiet) await view.mount(target, context);
      return;
    }
    try {
      await view.mount(target, context);
    } catch (err) {
      if (err && err.code === 429) {
        // Throttled: restore whatever was on screen instead of replacing real
        // data with an error panel, and say why it is stale.
        if (previous && previous.trim()) target.innerHTML = previous;
        renderRateLimitBanner();
        scheduleRateLimitRecovery();
        return;
      }
      if (state.booting219 && unreachable219(err)) {
        // Bundle 219: right after a refresh, a dropped connection keeps the
        // shimmer and the retry bar instead of a red error panel.
        target.innerHTML = ui.bootSkeleton219(viewId);
        serverUnreachable219();
        return;
      }
      target.innerHTML = ui.panel({ body: ui.errorState(err.message, state.view) });
    }
  }

  async function refreshAll() {
    if (state.busy) return;
    state.busy = true;
    const button = $('refresh-button');
    button.innerHTML = '<span class="spinner"></span>';
    try {
      await mountView(state.view, { quiet: true });
      await loadNotifications();
      toast('Data refreshed');
    } finally {
      state.busy = false;
      button.innerHTML = ui.icon('i-refresh');
    }
  }

  async function loadNotifications() {
    try {
      const data = await api('/api/v1/admin/security-summary');
      const counts = data.counts || {};
      setUnread(Number(counts.critical_alerts_24h || 0) + Number(counts.failed_logins_24h || 0));
      state.notifications = (Array.isArray(data.recent_events) ? data.recent_events : []).slice(0, 10).map((e) => ({
        icon: ui.severityIcon(e.severity), title: fmt.titleCase(e.action), detail: e.detail,
        timestamp: e.created_at, severity: e.severity,
      }));
      renderNav();
    } catch (err) {
      /* Notifications are non-critical: the console keeps working without them. */
    }
  }

  // -------------------------------------------------------------------- auth
  function showLogin() {
    $('boot219').classList.add('hidden');
    hideServerBar219();
    $('auth').classList.remove('hidden');
    $('app').classList.add('hidden');
    const username = $('username');
    if (username) username.focus();
  }

  // Bundle 219: the stored start page (falls back to the dashboard).
  function startView219() {
    const stored = localStorage.getItem('locallink_admin_view');
    return stored && LL.views[stored] ? stored : 'dashboard';
  }

  // Bundle 219: paint the real top bar, sidebar and titles from the saved
  // login at once, before the server answers (no login page flash).
  function paintShell219(start) {
    $('boot219').classList.add('hidden');
    $('auth').classList.add('hidden');
    $('app').classList.remove('hidden');
    const session = state.session || {};
    $('identity-name').textContent = session.username || '—';
    $('identity-role').textContent = fmt.titleCase(session.role || '') || '—';
    $('identity-initial').textContent = session.username ? session.username.charAt(0).toUpperCase() : '?';
    // Bundle 216: every signed-in admin wears crown A next to the name.
    $('identity-initial').classList.add('has-crown');
    if (!document.getElementById('identity-crown')) {
      $('identity-initial').insertAdjacentHTML('beforeend', `<span id="identity-crown" class="av216-crown">${ui.crown216(15)}</span>`);
    }
    const view = LL.views[start] || LL.views.dashboard;
    state.view = start;
    $('crumb-view').textContent = view.title;
    $('crumb-sub').textContent = view.subtitle;
    document.title = `${view.title} · LocalLink Admin`;
    renderNav();
  }

  function showApp() {
    const start = startView219();
    paintShell219(start);
    mountView(start);
    loadNotifications();
  }

  function handleExpiry() {
    state.token = '';
    state.session = null;
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(IDENTITY_KEY);
    showLogin();
    const error = $('login-error');
    error.hidden = false;
    $('login-error-text').textContent = 'Your admin session expired. Sign in again.';
  }

  function logout() {
    const token = state.token;
    state.token = '';
    state.session = null;
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(IDENTITY_KEY);
    closeOverlays();
    showLogin();
    if (token) {
      fetch('/api/v1/admin/logout', { method: 'POST', headers: { Authorization: 'Bearer ' + token } }).catch(() => {});
    }
  }

  $('login-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const submit = $('login-submit');
    const error = $('login-error');
    error.hidden = true;
    submit.disabled = true;
    submit.textContent = 'Signing in…';
    try {
      const data = await api('/api/v1/admin/login', {
        method: 'POST',
        body: { username: $('username').value, password: $('password').value },
      });
      state.token = data.token;
      state.session = Object.assign({}, data.admin, { expires_at: data.expires_at });
      localStorage.setItem(TOKEN_KEY, state.token);
      localStorage.setItem(IDENTITY_KEY, JSON.stringify(state.session));
      $('password').value = '';
      showApp();
    } catch (err) {
      error.hidden = false;
      $('login-error-text').textContent = err.message;
    } finally {
      submit.disabled = false;
      submit.textContent = 'Sign in';
    }
  });

  $('toggle-password').addEventListener('click', () => {
    const field = $('password');
    const showing = field.type === 'text';
    field.type = showing ? 'password' : 'text';
    $('toggle-password').title = showing ? 'Show password' : 'Hide password';
  });

  // ------------------------------------------------------------------ events
  document.addEventListener('click', async (event) => {
    // Clicks inside an overlay, or on the button that owns one, must not close it.
    // Bundle 217: the drawer button and the drawer itself must not shut the drawer.
    if (event.target.closest('.panel-pop, .menu, .search-panel, [data-overlay-trigger], #menu-toggle, #sidebar')) {
      // fall through to the button handlers below
    } else if (!event.target.closest('[data-nav],[data-range],[data-retry],[data-overlay-close],[data-menu],[data-user-status],[data-user-devices],[data-device-revoke],[data-device-rename],[data-device-policy],[data-session-revoke],[data-recovery],[data-action],[data-topology],[data-collapse],[data-clear-filters]')) {
      closeOverlays();
      return;
    }

    const target = event.target.closest('[data-nav],[data-range],[data-retry],[data-overlay-close],[data-menu],[data-user-status],[data-user-devices],[data-device-revoke],[data-device-rename],[data-device-policy],[data-session-revoke],[data-recovery],[data-action],[data-topology],[data-collapse],[data-clear-filters],[data-region]');
    if (!target) return;

    if (target.hasAttribute('data-nav')) {
      const prefill = target.getAttribute('data-prefill');
      const destination = target.getAttribute('data-nav');
      if (prefill && destination === 'users') state.filters.users.search = prefill;
      if (prefill && destination === 'devices') state.filters.devices.search = prefill;
      mountView(destination);
      return;
    }

    if (target.hasAttribute('data-range')) {
      const range = target.getAttribute('data-range');
      if (RANGES.indexOf(range) < 0) return;
      state.analyticsRange = range;
      localStorage.setItem(RANGE_KEY, range);
      mountView('dashboard');
      return;
    }

    if (target.hasAttribute('data-retry')) {
      mountView(target.getAttribute('data-retry'));
      return;
    }

    if (target.hasAttribute('data-overlay-close')) { closeOverlays(); return; }

    if (target.hasAttribute('data-menu')) {
      const action = target.getAttribute('data-menu');
      if (action === 'theme') applyTheme(currentTheme() === 'dark' ? 'light' : 'dark');
      if (action === 'refresh') { closeOverlays(); refreshAll(); }
      if (action === 'privacy') mountView('privacy');
      if (action === 'logout') logout();
      return;
    }

    if (target.hasAttribute('data-user-status')) {
      const id = target.getAttribute('data-user-status');
      const status = target.value;
      try {
        await post(`/api/v1/admin/user/status?id=${encodeURIComponent(id)}&status=${encodeURIComponent(status)}`);
        toast(`Account set to ${status}` + SENT_220);
        mountView('users');
      } catch (err) { toast(err.message, 'error'); }
      return;
    }

    if (target.hasAttribute('data-user-devices')) {
      const id = target.getAttribute('data-user-devices');
      state.filters.devices.search = '';
      const listMount = async () => {
        try {
          const data = await api('/api/v1/admin/devices?limit=200');
          const owned = (Array.isArray(data.devices) ? data.devices : []).filter((d) => d.user_id === id);
          const host = $('modal-host');
          host.innerHTML = `<div class="modal-scrim" role="dialog" aria-modal="true"><div class="modal" style="width:min(720px,100%)">
            <div class="pop-head"><h2>Devices for this account</h2>
              <button class="btn btn-icon" data-modal="close">${ui.icon('i-close')}</button></div>
            ${owned.length ? ui.table({
              columns: [{ label: 'Device' }, { label: 'Platform' }, { label: 'State' }, { label: 'Presence' }],
              rows: owned.map((d) => [
                `<div><span class="cell-primary">${ui.esc(d.name || '')}</span><div class="mono subtle">${ui.esc(d.id)}</div></div>`,
                ui.esc(d.platform || '—'), ui.statusBadge(d.status),
                ui.badge(d.online ? 'online' : 'offline', d.online ? 'success' : 'neutral', { dot: true }),
              ]),
            }) : ui.emptyState({ icon: 'i-devices', title: 'No devices', message: 'This account has no registered device.' })}
          </div></div>`;
          host.querySelector('[data-modal="close"]').addEventListener('click', () => { host.innerHTML = ''; });
        } catch (err) { toast(err.message, 'error'); }
      };
      listMount();
      return;
    }

    if (target.hasAttribute('data-device-revoke')) {
      const id = target.getAttribute('data-device-revoke');
      const confirmed = await confirmDialog({
        title: 'Revoke device', destructive: true, confirmLabel: 'Revoke device',
        message: 'The device loses its identity key and every active session. The action is written to the audit trail.',
      });
      if (!confirmed) return;
      try {
        await post(`/api/v1/admin/device/revoke?id=${encodeURIComponent(id)}`);
        toast('Device revoked' + SENT_220);
        mountView('devices');
      } catch (err) { toast(err.message, 'error'); }
      return;
    }

    if (target.hasAttribute('data-device-rename')) {
      const id = target.getAttribute('data-device-rename');
      const current = target.getAttribute('data-device-name') || '';
      const name = window.prompt('New device name', current);
      if (name === null) return;
      const trimmed = name.trim();
      if (!trimmed) { toast('A device name is required', 'error'); return; }
      try {
        await post(`/api/v1/admin/device/rename?id=${encodeURIComponent(id)}&name=${encodeURIComponent(trimmed)}`);
        toast('Device renamed');
        mountView('devices');
      } catch (err) { toast(err.message, 'error'); }
      return;
    }

    if (target.hasAttribute('data-device-policy')) {
      const id = target.getAttribute('data-device-policy');
      try {
        const current = await api(`/api/v1/admin/device/network-policy?id=${encodeURIComponent(id)}`);
        // Bundle 216: Default / Allowed / Always on / Always off (server: null / allowed / forced_on / forced_off).
        const words = { default: null, allowed: 'allowed', 'always on': 'forced_on', 'always off': 'forced_off' };
        const shown = { allowed: 'Allowed', forced_on: 'Always on', forced_off: 'Always off' };
        const now = current.device_wifi_radio_policy || current.wifi_radio_policy || '';
        const value = window.prompt(
          `Wi-Fi rule for this phone\nNow: ${shown[now] || 'Default'}\nType: Default / Allowed / Always on / Always off`,
          shown[now] || 'Default');
        if (value === null) return;
        const chosen = value.trim().toLowerCase();
        if (!Object.prototype.hasOwnProperty.call(words, chosen)) { toast('Type Default, Allowed, Always on or Always off', 'error'); return; }
        await post(`/api/v1/admin/device/network-policy?id=${encodeURIComponent(id)}`, {
          wifi_radio_policy: words[chosen],
        });
        toast('Wi-Fi rule saved' + SENT_220);
        mountView('devices');
      } catch (err) { toast(err.message, 'error'); }
      return;
    }

    if (target.hasAttribute('data-session-revoke')) {
      const id = target.getAttribute('data-session-revoke');
      const kind = target.getAttribute('data-session-kind');
      const confirmed = await confirmDialog({
        title: kind === 'admin' ? 'Revoke administrator session' : 'Revoke session',
        destructive: true, confirmLabel: 'Revoke',
        message: kind === 'admin'
          ? 'The administrator is signed out of the console immediately.'
          : 'The device must authenticate again before it can use the server.',
      });
      if (!confirmed) return;
      const path = kind === 'admin' ? '/api/v1/admin/admin-session/revoke' : '/api/v1/admin/session/revoke';
      try {
        await post(`${path}?id=${encodeURIComponent(id)}`);
        toast(kind === 'admin' ? 'Session revoked' : 'Session revoked' + SENT_220);
        mountView('sessions');
      } catch (err) { toast(err.message, 'error'); }
      return;
    }

    if (target.hasAttribute('data-recovery')) {
      const action = target.getAttribute('data-recovery');
      const id = target.getAttribute('data-id');
      const labels = { approve: 'Approve recovery', reissue: 'Reissue recovery code', reject: 'Reject recovery request' };
      const confirmed = await confirmDialog({
        title: labels[action] || 'Recovery action',
        destructive: action === 'reject', confirmLabel: labels[action] || 'Continue',
        message: action === 'reject'
          ? 'The requester is told the request was rejected. The decision is audited.'
          : 'The server issues a new recovery code path for the requester. The action is audited.',
      });
      if (!confirmed) return;
      try {
        const box = document.querySelector(`[data-reason-for="${CSS.escape(id || '')}"]`);
        const reason = (box && box.value.trim()) || 'Rejected by administrator';
        const body = action === 'reject' ? { id: id, reason: reason } : { id: id };
        // Bundle 222: the server reads id / reason from the address (the body
        // alone was refused), and the one-time code is now shown to the admin.
        const q = `?id=${encodeURIComponent(id || '')}${action === 'reject' ? `&reason=${encodeURIComponent(reason)}` : ''}`;
        const result = await post(`/api/v1/admin/recovery-requests/${action}${q}`, body);
        if (result && result.recovery_credential) {
          await showCode222(result.recovery_credential, result.expires_at);
        } else {
          toast('Recovery request updated');
        }
        mountView('recovery');
      } catch (err) { toast(err.message, 'error'); }
      return;
    }

    if (target.hasAttribute('data-action')) {
      const action = target.getAttribute('data-action');
      // Quick-action cards navigate; without this branch they sat there and
      // did nothing when clicked.
      if (action.indexOf('nav:') === 0) { mountView(action.slice(4)); return; }
      if (action === 'backup') {
        const confirmed = await confirmDialog({
          title: 'Create database backup', confirmLabel: 'Create backup',
          message: 'A consistent copy of the LocalLink database is written to the server backup directory.',
        });
        if (!confirmed) return;
        try {
          const result = await post('/api/v1/admin/backup');
          toast(result && result.path ? `Backup written to ${result.path}` : 'Backup completed');
          const note = document.querySelector('[data-backup-note]');
          if (note && result && result.path) note.textContent = result.path;
        } catch (err) { toast(err.message, 'error'); }
      }
      return;
    }

    if (target.hasAttribute('data-topology')) { openTopology(); return; }
    if (target.hasAttribute('data-collapse')) {
      const app = $('app');
      app.classList.toggle('collapsed');
      localStorage.setItem('locallink_admin_collapsed', app.classList.contains('collapsed') ? '1' : '0');
      return;
    }
    if (target.hasAttribute('data-clear-filters')) {
      if (state.filters[state.view]) {
        Object.keys(state.filters[state.view]).forEach((key) => { state.filters[state.view][key] = ''; });
      }
      mountView(state.view);
    }
  });

  // filters: debounced search input, immediate selects
  document.addEventListener('input', (event) => {
    const field = event.target.closest('[data-filter-search]');
    if (!field) return;
    if (!state.filters[state.view]) state.filters[state.view] = {};
    state.filters[state.view].search = field.value;
    clearTimeout(field._timer);
    field._timer = setTimeout(() => mountView(state.view), 280);
  });

  document.addEventListener('change', (event) => {
    const select = event.target.closest('[data-filter]');
    if (!select) return;
    if (!state.filters[state.view]) state.filters[state.view] = {};
    state.filters[state.view][select.getAttribute('data-filter')] = select.value;
    mountView(state.view);
  });

  // Bundle 214: make an app user an admin (level picker, Owner default),
  // change the level, or remove admin. Web owner only.
  function adminLevelDialog(name, current) {
    return new Promise((resolve) => {
      const levels = [
        ['owner', 'Owner', 'All admin tools. Can block users and set the app version offline'],
        ['operator', 'Operator', 'Same rights as web Operator (no offline block / app version)'],
        ['security', 'Security', 'Same rights as web Security (no offline block / app version)'],
        ['auditor', 'Auditor', 'Same rights as web Auditor'],
      ];
      const pick = current || 'owner';
      const host = $('modal-host');
      host.innerHTML = `<div class="modal-scrim" role="dialog" aria-modal="true">
        <div class="modal" data-admin-dialog>
          <h2>\u{1F451} ${current ? 'Admin level for' : 'Make'} ${ui.esc(name)}${current ? '' : ' an admin'}</h2>
          <p>They get admin tools in the LinkUp app and a crown on their photo that everyone can see. No password: their phone becomes admin by itself.</p>
          <div class="field"><label>Admin level</label>
            ${levels.map(([value, label, help]) => `<label style="display:flex;gap:8px;align-items:flex-start;padding:8px;border:1px solid var(--border);border-radius:8px;margin-top:6px">
              <input type="radio" name="admin_level" value="${value}" ${value === pick ? 'checked' : ''}>
              <span><strong>${label}${value === 'owner' ? ' (default)' : ''}</strong><br><span class="subtle" style="font-size:12px">${ui.esc(help)}</span></span>
            </label>`).join('')}
          </div>
          <p class="subtle" style="font-size:12px">Their phone gets it when it reaches the server. Other phones see the crown even offline (the card spreads phone to phone).</p>
          <div class="modal-actions">
            <button class="btn" data-modal="cancel">Cancel</button>
            <button class="btn btn-primary" data-modal="confirm">${current ? 'Save level' : 'Make admin'}</button>
          </div>
        </div>
      </div>`;
      const finish = (value) => { host.innerHTML = ''; resolve(value); };
      host.querySelector('[data-modal="cancel"]').addEventListener('click', () => finish(null));
      host.querySelector('[data-modal="confirm"]').addEventListener('click', () => {
        const chosen = host.querySelector('[name="admin_level"]:checked');
        finish(chosen ? chosen.value : 'owner');
      });
      host.querySelector('.modal-scrim').addEventListener('click', (event) => {
        if (event.target.classList.contains('modal-scrim')) finish(null);
      });
    });
  }
  document.addEventListener('click', async (event) => {
    const btn = event.target.closest('[data-user-admin]');
    if (!btn) return;
    const id = btn.getAttribute('data-user-admin');
    const name = btn.getAttribute('data-user-name') || 'this user';
    const mode = btn.getAttribute('data-admin-mode');
    let body = null;
    if (mode === 'remove') {
      const ok = await confirmDialog({
        title: `Remove admin from ${name}?`, destructive: true, confirmLabel: 'Remove admin',
        message: `${name} goes back to a normal user as soon as the phone reaches the server: the Admin row in the app disappears, the offline admin key stops working and the crown goes from every phone. Blocks made before stay (an owner can undo them).`,
      });
      if (!ok) return;
      body = { user_id: id, remove: true };
    } else if (btn.getAttribute('data-admin-pick')) {
      // Bundle 216: the W5 side panel picks the level on the card itself.
      const role = btn.getAttribute('data-admin-pick');
      const label = { owner: 'Owner', operator: 'Operator', security: 'Security', auditor: 'Auditor' }[role] || role;
      const ok = await confirmDialog({
        title: mode === 'level' ? `Change ${name} to ${label}?` : `Make ${name} an admin (${label})?`,
        confirmLabel: mode === 'level' ? 'Save level' : 'Make admin',
        message: 'Their phone gets it when it reaches the server. Other phones see the crown even offline.',
      });
      if (!ok) return;
      body = { user_id: id, role };
    } else {
      const role = await adminLevelDialog(name, mode === 'level' ? btn.getAttribute('data-user-role') : '');
      if (!role) return;
      body = { user_id: id, role };
    }
    try {
      await post('/api/v1/admin/user-admin', body);
      toast((body.remove ? `${name} is no longer an admin` : `${name} is now an admin`) + SENT_220);
      state.people216 = null;
      mountView('users');
    } catch (err) { toast(err.message, 'error'); }
  });

  // Bundle 213: App version page (save / remove the minimum version).
  async function saveVersionRule(body, note) {
    try {
      await post('/api/v1/admin/version-rule', body);
      toast('Rule saved for everyone' + SENT_220);
      mountView('appversion');
    } catch (err) {
      toast(err.message, 'error');
      if (note) note.textContent = err.message;
    }
  }
  document.addEventListener('submit', (event) => {
    const form = event.target.closest('[data-version-form]');
    if (!form) return;
    event.preventDefault();
    const when = (form.querySelector('[name="when"]:checked') || {}).value || 'warn';
    saveVersionRule({
      min_version: form.querySelector('[name="min_version"]').value.trim(),
      min_build: Number(form.querySelector('[name="min_build"]').value),
      mode: when === 'warn' ? 'warn' : 'stop',
      stop_days: when === 'days' ? Number(form.querySelector('[name="stop_days"]').value) : 0,
      very_old: form.querySelector('[name="very_old"]').checked,
    }, form.querySelector('[data-version-note]'));
  });
  document.addEventListener('click', (event) => {
    const btn = event.target.closest('[data-version-remove]');
    if (!btn) return;
    if (!window.confirm('Remove the minimum version for everyone?')) return;
    saveVersionRule({ remove: true }, null);
  });

  document.addEventListener('submit', async (event) => {
    const form = event.target.closest('[data-settings-form]');
    if (!form) return;
    event.preventDefault();
    const refresh = Number(form.querySelector('[name="dashboard_refresh_seconds"]').value);
    const threshold = Number(form.querySelector('[name="security_alert_threshold"]').value);
    const note = form.querySelector('[data-settings-note]');
    try {
      await post('/api/v1/admin/settings', {
        dashboard_refresh_seconds: refresh, security_alert_threshold: threshold,
      });
      toast('Settings saved');
      if (note) note.textContent = 'Saved to the server.';
      mountView('settings');
    } catch (err) {
      toast(err.message, 'error');
      if (note) note.textContent = err.message;
    }
  });

  $('refresh-button').addEventListener('click', refreshAll);
  $('theme-button').addEventListener('click', () => applyTheme(currentTheme() === 'dark' ? 'light' : 'dark'));
  $('notifications-button').addEventListener('click', openNotifications);
  $('identity-button').addEventListener('click', openIdentityMenu);

  const globalSearch = $('global-search');
  globalSearch.addEventListener('input', () => renderSearchResults(globalSearch.value));
  globalSearch.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      const term = globalSearch.value.trim();
      const matches = moduleList().filter((v) => v.title.toLowerCase().indexOf(term.toLowerCase()) >= 0);
      if (matches.length) mountView(matches[0].id);
    }
  });

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') { closeOverlays(); $('modal-host').innerHTML = ''; }
    const typing = ['INPUT', 'SELECT', 'TEXTAREA'].indexOf(event.target.tagName) >= 0;
    if (event.key === '/' && !typing) { event.preventDefault(); closeOverlays(); globalSearch.focus(); }
  });

  // Bundle 219: styles.css shows the menu button by width (also right after a
  // refresh); here we only close the drawer when the screen becomes wide.
  const compactMq219 = window.matchMedia ? window.matchMedia('(max-width: 1000px)') : null;
  function syncLayout219() {
    const compact = compactMq219 ? compactMq219.matches : window.innerWidth <= 1000;
    if (!compact) {
      $('app').classList.remove('drawer-open');
      $('drawer-scrim').classList.add('hidden');
    }
  }
  window.addEventListener('resize', syncLayout219);
  window.addEventListener('pageshow', syncLayout219);
  if (compactMq219 && compactMq219.addEventListener) compactMq219.addEventListener('change', syncLayout219);

  $('menu-toggle').addEventListener('click', () => {
    const app = $('app');
    const open = !app.classList.contains('drawer-open');
    app.classList.toggle('drawer-open', open);
    $('drawer-scrim').classList.toggle('hidden', !open);
  });
  const closeDrawer217 = () => {
    $('app').classList.remove('drawer-open');
    $('drawer-scrim').classList.add('hidden');
  };
  $('drawer-scrim').addEventListener('click', closeDrawer217);
  // Bundle 217: X in the drawer head and a left swipe close the drawer too.
  $('drawer-close').addEventListener('click', closeDrawer217);
  let swipe217 = null;
  $('sidebar').addEventListener('pointerdown', (ev) => { swipe217 = { x: ev.clientX, y: ev.clientY }; });
  $('sidebar').addEventListener('pointerup', (ev) => {
    if (!swipe217) return;
    const dx = ev.clientX - swipe217.x; const dy = ev.clientY - swipe217.y;
    swipe217 = null;
    if (dx < -60 && Math.abs(dy) < Math.abs(dx) && $('app').classList.contains('drawer-open')) closeDrawer217();
  });
  $('sidebar').addEventListener('pointercancel', () => { swipe217 = null; });

  // -------------------------------------------------------------- polling
  let pollTimer = null;
  let recoveryTimer = null;

  function scheduleRateLimitRecovery() {
    if (recoveryTimer) clearTimeout(recoveryTimer);
    const wait = Math.max(1000, (state.rateLimitedUntil || Date.now()) - Date.now());
    recoveryTimer = setTimeout(() => {
      renderRateLimitBanner();
      if (!rateLimited()) mountView(state.view, { quiet: true });
    }, wait + 500);
  }

  function pollIntervalMs() {
    // A dashboard refresh is eight requests, so the floor keeps a single
    // console well inside the server's admin budget even at the lowest
    // configured refresh interval.
    const seconds = Math.min(120, Math.max(30, state.refreshSeconds));
    return seconds * 1000;
  }

  // Sidebar counts ride along with the existing poll: one extra request, and a

  // failure leaves the previous figures in place rather than blanking them.

  async function refreshSummary() {

    try {

      state.summary = await api('/api/v1/admin/summary');

      renderNav();

    } catch (err) {

      // The notification panel already reports an unreachable server.

    }

  }


  function schedulePoll() {
    if (pollTimer) clearInterval(pollTimer);
    pollTimer = setInterval(() => {
      if (document.hidden || state.busy || rateLimited()) return;
      // Notifications are a single lightweight request and keep the badge
      // honest; the heavier view refresh only runs on the dashboard.
      loadNotifications();

      refreshSummary();
      if (state.view === 'dashboard') mountView('dashboard', { quiet: true, refresh: true });
      if (state.view === 'actions') mountView('actions', { quiet: true, refresh: true }); // Bundle 220
      if (state.view === 'conversations') mountView('conversations', { quiet: true, refresh: true }); // Bundle 221
    }, pollIntervalMs());
  }

  // The banner countdown is the only element that ticks every second.
  setInterval(() => { if (state.rateLimitedUntil) renderRateLimitBanner(); }, 1000);

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) return;
    if (state.booting219 && bootRetryAt219) { bootCheck219(); return; }
    if (rateLimited()) return;
    loadNotifications();
    if (state.view === 'dashboard') mountView('dashboard', { quiet: true });
  });

  // ------------------------------------------------------- Bundle 216 photos
  // Person cells carry data-avatar-user; the photo comes from the admin avatar
  // route with the bearer token and is shown from a blob: URL (cached).
  const avatarCache216 = new Map();
  async function hydrateAvatars216(root) {
    const spots = (root || document).querySelectorAll('[data-avatar-user]:not([data-avatar-done])');
    spots.forEach(async (spot) => {
      spot.setAttribute('data-avatar-done', '1');
      const id = spot.getAttribute('data-avatar-user');
      if (!id) return;
      let url = avatarCache216.get(id);
      if (url === undefined) {
        const pending = blob(`/api/v1/admin/users/${encodeURIComponent(id)}/avatar`)
          .then((url) => url || null).catch(() => null);
        avatarCache216.set(id, pending);
        url = pending;
      }
      const resolved = await url;
      avatarCache216.set(id, resolved);
      if (resolved) {
        spot.style.backgroundImage = `url("${resolved}")`;
        spot.classList.add('has-photo');
      }
    });
  }
  let hydrateTimer216 = 0;
  new MutationObserver(() => {
    clearTimeout(hydrateTimer216);
    hydrateTimer216 = setTimeout(() => hydrateAvatars216(document), 30);
  }).observe(document.body, { childList: true, subtree: true });

  document.addEventListener('click', async (event) => {
    const rule = event.target.closest('[data-wifi-rule][data-user-id]');
    if (rule) {
      const id = rule.getAttribute('data-user-id');
      const value = rule.getAttribute('data-wifi-rule');
      try {
        await post(`/api/v1/admin/user/network-policy?id=${encodeURIComponent(id)}`, { wifi_radio_policy: value || null });
        rule.parentElement.querySelectorAll('[data-wifi-rule]').forEach((b) => b.classList.toggle('on', b === rule));
        toast('Wi-Fi rule saved' + SENT_220);
      } catch (err) { toast(err.message, 'error'); }
      return;
    }
    const all = event.target.closest('[data-signout-all]');
    if (all) {
      const id = all.getAttribute('data-signout-all');
      const ok = await confirmDialog({
        title: 'Sign out all phones?', destructive: true, confirmLabel: 'Sign out all',
        message: 'Every phone of this person must sign in again before it can use the server.',
      });
      if (!ok) return;
      try {
        const data = await api('/api/v1/admin/sessions?limit=200');
        const mine = (Array.isArray(data.sessions) ? data.sessions : []).filter((x) => x.user_id === id && x.active);
        for (const x of mine) await post(`/api/v1/admin/session/revoke?id=${encodeURIComponent(x.id)}`);
        toast(`Signed out ${mine.length} phone${mine.length === 1 ? '' : 's'}` + (mine.length ? SENT_220 : ''));
      } catch (err) { toast(err.message, 'error'); }
    }
  });

  // ------------------------------------------------------------------- boot
  applyTheme(preferredTheme());
  const collapsed = localStorage.getItem('locallink_admin_collapsed') === '1';
  if (collapsed) $('app').classList.add('collapsed');

  // ------------------------------------------------------- Bundle 219 boot
  // Saved login: real shell + page-shaped shimmer at once, then check the
  // login. Only a 401 shows the login page; a dropped connection, a slow or
  // restarting server (5xx, no answer in 10 s) or a 429 keeps the panel with
  // a retry bar and never deletes the saved login.
  const RETRY_STEPS219 = [5, 10, 20, 30];
  let bootTry219 = 0;
  let bootTimer219 = null;
  let bootRetryAt219 = 0;

  function unreachable219(err) {
    if (!err) return false;
    if (err.name === 'AbortError' || err.name === 'TypeError') return true;
    return typeof err.status === 'number' && err.status >= 500;
  }

  function hideServerBar219() {
    const bar = document.getElementById('server-bar219');
    if (bar) bar.remove();
    bootRetryAt219 = 0;
  }

  function paintServerBar219() {
    if (!bootRetryAt219) return;
    const seconds = Math.max(0, Math.ceil((bootRetryAt219 - Date.now()) / 1000));
    const text = seconds > 0 ? `Trying again in ${seconds} s…` : 'Trying again…';
    let bar = document.getElementById('server-bar219');
    if (!bar) {
      bar = document.createElement('div');
      bar.id = 'server-bar219';
      bar.className = 'bar219';
      bar.setAttribute('role', 'status');
      bar.innerHTML = ui.icon('i-pulse') +
        '<div class="bar219-text"><strong>Can\'t reach the server</strong><span></span></div>' +
        '<button class="bar219-retry" type="button">Retry</button>';
      bar.querySelector('.bar219-retry').addEventListener('click', () => bootCheck219());
      const content = $('view');
      content.parentElement.insertBefore(bar, content);
    }
    bar.querySelector('.bar219-text span').textContent = text;
  }

  function scheduleBoot219(seconds) {
    if (bootTimer219) clearTimeout(bootTimer219);
    bootTimer219 = setTimeout(() => bootCheck219(), seconds * 1000);
  }

  function serverUnreachable219() {
    const seconds = RETRY_STEPS219[Math.min(bootTry219, RETRY_STEPS219.length - 1)];
    bootTry219 += 1;
    scheduleBoot219(seconds);
    bootRetryAt219 = Date.now() + seconds * 1000;
    paintServerBar219();
  }

  async function bootCheck219() {
    if (bootTimer219) { clearTimeout(bootTimer219); bootTimer219 = null; }
    if (!state.token) return;
    state.booting219 = true;
    const ctrl = typeof AbortController === 'function' ? new AbortController() : null;
    const killer = ctrl ? setTimeout(() => ctrl.abort(), 10000) : null;
    try {
      state.summary = await api('/api/v1/admin/summary', { signal: ctrl ? ctrl.signal : undefined });
    } catch (err) {
      if (!state.token) return; // 401: api() already showed the login page
      if (err && err.code === 429) {
        // Q1 = A: stay signed in; the "Server is busy" bar counts down.
        hideServerBar219();
        scheduleBoot219(Math.max(1, (state.rateLimitedUntil - Date.now()) / 1000) + 0.5);
        return;
      }
      if (unreachable219(err)) { serverUnreachable219(); return; }
      showLogin(); // any other answer: same as before 219
      return;
    } finally {
      if (killer) clearTimeout(killer);
    }
    hideServerBar219();
    renderRateLimitBanner();
    bootTry219 = 0;
    renderNav();
    await mountView(state.view);
    if (!document.getElementById('server-bar219')) state.booting219 = false;
    loadNotifications();
  }

  setInterval(paintServerBar219, 1000);

  (async function boot() {
    const token = state.token;
    if (!token) { showLogin(); return; }
    let identity = null;
    try { identity = JSON.parse(localStorage.getItem(IDENTITY_KEY) || 'null'); } catch (err) { identity = null; }
    state.session = identity || { username: 'Authenticated administrator', role: 'auditor' };
    const start = startView219();
    paintShell219(start);
    $('view').innerHTML = ui.bootSkeleton219(start);
    await bootCheck219();
  })();

  schedulePoll();

  // Expose a tiny surface for tests and for the views that need it.
  window.LL.app = {
    api, blob, can, toast, mountView, state, applyTheme, confirm: confirmDialog, openTopology,
    rateLimited, renderRateLimitBanner, schedulePoll,
  };
})();
