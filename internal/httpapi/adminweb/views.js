/* ===========================================================================
   LocalLink admin console — modules.

   Each view declares the role it needs, fetches only from the admin API and
   renders real server values. A section whose request failed shows its own
   error state with a retry; a section with no data shows an explicit empty
   state. No value shown is invented client-side.
   =========================================================================== */
window.LL = window.LL || {};
LL.views = {};

(function () {
  'use strict';

  const ui = LL.ui;
  const fmt = LL.fmt;

  // ------------------------------------------------------------- small helpers
  async function settle(promise) {
    try {
      return { ok: true, data: await promise };
    } catch (err) {
      return { ok: false, error: (err && err.message) || String(err) };
    }
  }

  function panelResult(result, options, render) {
    const o = options || {};
    if (!result.ok) {
      return ui.panel({
        title: o.title,
        subtitle: o.subtitle,
        body: ui.errorState(result.error, o.retryId),
      });
    }
    return render(result.data);
  }

  function num(value) {
    const n = Number(value);
    return isFinite(n) ? n : 0;
  }

  function emptyText(value) {
    return value === null || value === undefined || value === '' ? '—' : ui.esc(value);
  }

  const RANGE_LABELS = { '24h': 'last 24 hours', '7d': 'last 7 days', '30d': 'last 30 days', '90d': 'last 90 days' };
  const LONG_RANGES = { '30d': true, '90d': true };

  function rangeWindowLabel(range) {
    return RANGE_LABELS[range] || RANGE_LABELS['24h'];
  }

  function seriesToList(raw, longRange) {
    if (!Array.isArray(raw)) return [];
    return raw
      .filter((p) => p && typeof p === 'object')
      .map((p) => ({ label: fmt.bucket(p.t, longRange), value: num(p.v) }));
  }

  function sparkValues(raw) {
    if (!Array.isArray(raw)) return [];
    return raw.map((p) => num(p && p.v));
  }

  function paintSparklines(root) {
    root.querySelectorAll('[data-sparkline]').forEach((host) => {
      let values = [];
      try { values = JSON.parse(host.getAttribute('data-sparkline') || '[]'); } catch (err) { values = []; }
      if (!values.length) return;
      const canvas = document.createElement('canvas');
      host.appendChild(canvas);
      LL.charts.sparkline(canvas, values, ui.token('primary', '#1e5bff'));
    });
  }

  function afterRender(root) {
    paintSparklines(root);
    root.querySelectorAll('canvas[data-chart]').forEach((canvas) => {
      let spec = null;
      try { spec = JSON.parse(canvas.getAttribute('data-spec') || 'null'); } catch (err) { spec = null; }
      if (!spec) return;
      if (spec.type === 'line') LL.charts.line(canvas, spec.options);
      else if (spec.type === 'bar') LL.charts.bar(canvas, spec.options);
      else if (spec.type === 'donut') LL.charts.donut(canvas, spec.options);
      else if (spec.type === 'gauge') LL.charts.gauge(canvas, spec.options);
    });
  }

  function chartCanvas(spec, height) {
    return `<div class="chart-canvas-wrap" style="height:${height}px">
      <canvas data-chart="${spec.type}" data-spec='${JSON.stringify(spec).replace(/'/g, '&#39;')}'></canvas>
    </div>`;
  }

  function tableActions(buttons) {
    return `<div class="cell-actions">${buttons.join('')}</div>`;
  }

  // ------------------------------------------------------------------ dashboard
  // A poll refresh replaces only the regions whose content actually changed,
  // so every live piece of the dashboard carries an explicit marker.
  function pollRegion(id, html) {
    return `<div data-poll-region="${id}">${html}</div>`;
  }

  const dashboard = {
    id: 'dashboard',
    title: 'Dashboard',
    subtitle: 'How the server and phones are doing',
    icon: 'i-dashboard',
    group: 'People',
    async mount(el, ctx) {
      const range = ctx.state.analyticsRange || '24h';
      const longRange = !!LONG_RANGES[range];

      el.innerHTML = ui.bootSkeleton219('dashboard'); // Bundle 219

      const [analytics, health, security, activity, summary, network, settings] = await Promise.all([
        settle(ctx.api('/api/v1/admin/analytics?range=' + encodeURIComponent(range))),
        settle(ctx.api('/api/v1/admin/health')),
        settle(ctx.api('/api/v1/admin/security-summary')),
        settle(ctx.api('/api/v1/admin/activity?limit=40')),
        settle(ctx.api('/api/v1/admin/summary')),
        settle(ctx.api('/api/v1/admin/network')),
        settle(ctx.api('/api/v1/admin/settings')),
      ]);

      if (settings.ok && settings.data) {
        ctx.setRefreshSeconds(settings.data.dashboard_refresh_seconds);
      }

      const series = (analytics.ok && analytics.data.series) || {};
      const totals = (analytics.ok && analytics.data.totals) || {};
      const runtime = (analytics.ok && analytics.data.runtime) || { available: false };
      const summaryData = summary.ok ? summary.data : null;
      const networkData = network.ok ? network.data : null;
      const securityCounts = (security.ok && security.data.counts) || {};
      const securityEvents = (security.ok && security.data.recent_events) || [];
      const activityEvents = (activity.ok && activity.data.events) || [];
      const healthComponents = (health.ok && health.data.components) || [];
      const overall = health.ok ? health.data.overall : 'unknown';
      const windowLabel = rangeWindowLabel(range);

      const parts = [];

      // ---- Bundle 216 (W2): 4 tiles, the Map card and 'Needs a look'.
      parts.push(pollRegion('header', `<div class="bar216" style="justify-content:flex-end;margin:0">
          ${ui.statusBadge(overall, { label: 'Server: ' + ui.statusInfo(overall).label })}
        </div>`));
      if (summaryData) {
        const warnings = num(summaryData.security_alerts);
        const tile = (label, value, color) => `<div class="tile216"><small>${ui.esc(label)}</small><b style="color:${color}">${ui.esc(value)}</b></div>`;
        const tiles = `<div class="tiles216">
            ${tile('People', fmt.number(summaryData.users), 'var(--text-primary)')}
            ${tile('Phones online', `${fmt.number(summaryData.online_devices)} / ${fmt.number(summaryData.devices)}`, '#4ade80')}
            ${tile(range === '24h' ? 'Messages today' : `Messages (${range})`, fmt.number(num(totals.messages)), '#7aa7ff')}
            ${tile('Warnings', fmt.number(warnings), warnings ? '#fbbf24' : '#4ade80')}
          </div>`;
        const look = [];
        if (num(summaryData.pending_recoveries)) look.push([`${fmt.number(summaryData.pending_recoveries)} recovery request(s) waiting`, 'recovery', 'orange']);
        if (warnings) look.push([`${fmt.number(warnings)} warning(s) in the last 24 hours`, 'security', 'red']);
        if (num(securityCounts.locked_users)) look.push([`${fmt.number(securityCounts.locked_users)} locked account(s)`, 'security', 'grey']);
        const net = networkData || {};
        const mapCard = ui.panel({
          title: 'Map',
          body: `<div class="type-caption">${fmt.number(summaryData.online_devices)} phone(s) on the server right now${net.wifi_direct_connected ? ` \u00b7 ${fmt.number(net.wifi_direct_connected)} on Wi-Fi Direct` : ''}</div>
            <button class="btn btn-sm" style="margin-top:12px" data-nav="map">Open map \u203a</button>`,
        });
        const lookCard = ui.panel({
          title: 'Needs a look',
          body: look.length ? look.map(([text, view, tone]) => `<div class="map216-row" data-nav="${view}" style="cursor:pointer">
              <span>${ui.esc(text)}</span>${ui.pill216('Open', tone)}</div>`).join('')
            : '<div class="empty216">Nothing needs a look.</div>',
        });
        parts.push(pollRegion('metrics', `${tiles}<div class="split-1-1">${mapCard}${lookCard}</div>`));
      } else {
        parts.push(pollRegion('metrics', `<div class="state error">
          ${ui.icon('i-alert')}<strong>Numbers not available</strong>
          <p>${ui.esc(summary.error || 'The server did not answer.')}</p>
          <button class="btn btn-sm" data-retry="dashboard">${ui.icon('i-refresh')}Retry</button></div>`));
      }

      // ---- analytics
      const messagePoints = seriesToList(series.messages, longRange);
      const runtimeAvailable = !!runtime.available;
      const bytePoints = runtimeAvailable ? seriesToList(series.network_bytes, longRange) : [];
      const rangeSelector = ui.segmented([
        { value: '24h', label: '24H' }, { value: '7d', label: '7D' },
        { value: '30d', label: '30D' }, { value: '90d', label: '90D' },
      ], range, 'data-range');

      const allZero = (list) => list.length === 0 || list.every((p) => p.value === 0);
      const messageChart = messagePoints.length && !allZero(messagePoints)
        ? chartCanvas({ type: 'line', options: { points: messagePoints, color: ui.token('primary', '#1e5bff'), height: 220, xLabelCount: window.innerWidth <= 640 ? 4 : 6, longRange } }, 220)
        : ui.emptyState({ icon: 'i-activity', title: 'No messages in this window', message: 'The server accepted no messages during the selected period.' });
      const byteChart = !runtimeAvailable
        ? ui.emptyState({ icon: 'i-activity', title: 'Traffic metrics unavailable', message: 'This server build does not expose the runtime sampler.' })
        : (bytePoints.length && !allZero(bytePoints)
          ? chartCanvas({ type: 'line', options: { points: bytePoints, color: ui.token('info', '#0b5fa5'), height: 200, xLabelCount: window.innerWidth <= 640 ? 4 : 6, longRange } }, 200)
          : ui.emptyState({ icon: 'i-activity', title: 'No response traffic', message: 'No bytes were served during the selected period.' }));

      parts.push(pollRegion('analytics', `<section class="section">
        ${ui.sectionHead({ title: 'Activity analytics', subtitle: 'Rollups computed from platform tables and the runtime sampler', icon: 'i-activity', actions: rangeSelector })}
        <div class="split-3-2">
          ${ui.panel({ title: 'Message activity', subtitle: 'Messages accepted by the server per bucket', body: messageChart })}
          ${ui.panel({ title: 'Server traffic', subtitle: runtimeAvailable ? 'Response bytes served per bucket' : 'Requires the runtime metrics sampler', body: byteChart })}
        </div>
      </section>`))

      // ---- health
      const gaugeKeys = ['cpu', 'memory', 'storage'];
      const gaugeTones = { healthy: 'var(--success)', warning: 'var(--warning)', critical: 'var(--danger)', unknown: 'var(--text-tertiary)', offline: 'var(--text-secondary)' };
      const gauges = healthComponents.filter((c) => gaugeKeys.indexOf(c.key) >= 0);
      const gaugeHtml = gauges.map((c) => {
        const tone = ui.statusInfo(c.status).tone;
        const color = gaugeTones[ui.statusInfo(c.status).tone] || gaugeTones[tone];
        return `<div class="gauge">
          ${chartCanvas({ type: 'gauge', options: { value: num(c.value), max: 100, threshold: c.threshold != null ? num(c.threshold) : null, color: color, size: 132 } }, 132)}
          <span class="gauge-label">${ui.esc(c.label || c.key)}</span>
          <span class="gauge-sub">${ui.esc(c.unit === '%' && c.threshold != null ? 'threshold ' + num(c.threshold) + '%' : (c.unit ? fmt.number(c.value) + ' ' + c.unit : ''))}</span>
        </div>`;
      }).join('');

      const healthBody = !health.ok
        ? ui.errorState(health.error, 'dashboard')
        : (healthComponents.length
          ? `<div class="split-3-1">
              <div>${healthComponents.map(ui.healthRow).join('')}</div>
              ${gauges.length ? `<div class="gauge-grid" style="grid-template-columns:1fr">${gaugeHtml}</div>` : '<div></div>'}
            </div>`
          : ui.emptyState({ icon: 'i-pulse', title: 'No health readings', message: 'The server did not return subsystem readings for this request.' }));

      parts.push(pollRegion('health', `<section class="section">
        ${ui.sectionHead({ title: 'Server health', subtitle: 'Percentages are live readings; the tick marks the alert threshold', icon: 'i-pulse', actions: ui.statusBadge(overall) })}
        ${ui.panel({ body: healthBody })}
      </section>`))

      // ---- network + security
      let networkBody;
      if (!networkData) {
        networkBody = ui.errorState(network.error || 'The network endpoint did not respond.', 'dashboard');
      } else {
        const online = num(networkData.online_devices);
        const registered = num(networkData.devices);
        const offline = Math.max(0, registered - online);
        const segments = [
          { label: 'Server connected', value: num(networkData.server_connected), color: 'var(--success)' },
          { label: 'LAN available', value: num(networkData.lan_available), color: 'var(--primary)' },
          { label: 'Wi-Fi Direct', value: num(networkData.wifi_direct_connected), color: 'var(--info)' },
          { label: 'Mesh capable', value: num(networkData.mesh_available), color: 'var(--warning)' },
          { label: 'Offline / idle', value: offline, color: 'var(--border-strong)' },
        ];
        const withColors = segments.map((s) => Object.assign({}, s, { color: cssVarToHex(s.color) }));
        networkBody = `<div class="row gap-lg" style="align-items:center;flex-wrap:wrap">
            <div style="width:150px;height:150px">${chartCanvas({ type: 'donut', options: { segments: withColors, size: 148, centerValue: fmt.number(online), centerLabel: 'online' } }, 148)}</div>
            <div class="legend grow">${withColors.map((s) => `<div class="legend-item">
              <span class="legend-swatch" style="background:${s.color}"></span>
              <span>${ui.esc(s.label)}</span><span class="legend-value">${fmt.number(s.value)}</span></div>`).join('')}</div>
          </div>
          <div class="health-detail" style="margin-top:12px">
            ${ui.statRow([
              { label: 'Server links', value: fmt.number(networkData.server_connected) },
              { label: 'LAN', value: fmt.number(networkData.lan_available) },
              { label: 'Wi-Fi Direct', value: fmt.number(networkData.wifi_direct_connected) },
              { label: 'Mesh', value: fmt.number(networkData.mesh_available) },
              { label: 'Registered', value: fmt.number(registered) },
              { label: 'Pending transfers', value: fmt.number(networkData.pending_transfers) },
            ])}
          </div>`;
      }

      let securityBody;
      if (!security.ok) {
        securityBody = ui.errorState(security.error, 'dashboard');
      } else {
        const critical = num(securityCounts.critical_alerts_24h);
        const failed = num(securityCounts.failed_logins_24h);
        const head = critical > 0
          ? ui.headline('danger', `${critical} critical alert${critical === 1 ? '' : 's'} in 24h`, 'i-alert')
          : (failed > 0
            ? ui.headline('warning', `${failed} failed login${failed === 1 ? '' : 's'} in 24h`, 'i-alert')
            : ui.headline('success', 'No critical security alerts in 24h', 'i-check'));
        securityBody = `${head}
          <div style="margin-top:12px">${ui.statRow([
            { label: 'Failed logins 24h', value: fmt.number(failed) },
            { label: 'Locked users', value: fmt.number(securityCounts.locked_users) },
            { label: 'Revoked devices', value: fmt.number(securityCounts.revoked_devices) },
            { label: 'Active sessions', value: fmt.number(securityCounts.active_sessions) },
            { label: 'Pending recoveries', value: fmt.number(securityCounts.pending_recoveries) },
          ])}</div>
          ${securityEvents.length ? `<div style="margin-top:16px;display:grid;gap:8px">
            <span class="type-label">Recent security events</span>
            ${securityEvents.slice(0, 4).map((e) => ui.notificationItem({
              icon: ui.severityIcon(e.severity), title: fmt.titleCase(e.action), detail: e.detail,
              timestamp: e.created_at, severity: e.severity,
            })).join('')}
          </div>` : ''}`;
      }

      parts.push(pollRegion('netsec', `<div class="section">
        <div class="split-1-1">
          ${ui.panel({
            title: 'Live network', subtitle: 'Transport state reported by connected devices',
            actions: `<button class="btn btn-sm" data-nav="network">Open network topology</button>`,
            body: networkBody,
          })}
          ${ui.panel({
            title: 'Security overview', subtitle: 'Audited events, sessions and outstanding recovery work',
            actions: `<button class="btn btn-sm" data-nav="security">Security center</button>
                      <button class="btn btn-sm" data-nav="audit">Audit log</button>`,
            body: securityBody,
          })}
        </div>
      </div>`))

      // ---- activity
      const timelineEntries = activityEvents.map((e) => ({
        title: e.title, detail: e.detail, timestamp: e.created_at, severity: e.severity,
        icon: activityIcon(e.type),
      }));
      parts.push(pollRegion('activity', ui.panel({
        title: 'Recent activity',
        subtitle: 'Newest first, merged across users, devices, files, calls and audit sources',
        actions: `<button class="btn btn-sm" data-nav="audit">Open audit log</button>`,
        body: !activity.ok
          ? ui.errorState(activity.error, 'dashboard')
          : ui.timeline(timelineEntries, { max: 10, emptyMessage: 'No operational events have been recorded on this server yet.' }),
      })));

      // ---- quick actions
      const actions = [
        { id: 'users', label: 'Manage users', icon: 'i-users' },
        { id: 'devices', label: 'Manage devices', icon: 'i-devices' },
        { id: 'security', label: 'Review security', icon: 'i-flag' },
        { id: 'network', label: 'View network', icon: 'i-hub' },
        { id: 'audit', label: 'Audit log', icon: 'i-audit' },
        { id: 'recovery', label: 'Recovery queue', icon: 'i-restore' },
        { id: 'diagnostics', label: 'System health', icon: 'i-pulse' },
      ];
      if (ctx.can('manageSettings')) actions.push({ id: 'settings', label: 'Server settings', icon: 'i-settings' });
      if (ctx.can('backup')) actions.push({ id: 'backup', label: 'Create backup', icon: 'i-backup' });
      // Trim the list to what this role may actually open.
      const permitted = actions.filter((a) => !LL.moduleRoles[a.id] || LL.moduleRoles[a.id].indexOf(ctx.role) >= 0);
      parts.push(pollRegion('actions', `<section class="section">
        ${ui.sectionHead({ title: 'Quick actions', subtitle: 'Only actions your admin role permits', icon: 'i-flag' })}
        <div class="grid grid-actions">${permitted.map((a) => ui.quickAction({
          label: a.label, icon: a.icon, action: a.id === 'backup' ? 'backup' : 'nav:' + a.id,
        })).join('')}</div>
      </section>`))

      parts.push(pollRegion('note', ui.note('Message review is switched on',
        'The owner role can read message bodies in the Conversations module, and every conversation opened there is recorded in the audit log. Passwords, private keys, call audio, decrypted media and file contents stay out of this console.',
 'i-lock')));

      el.innerHTML = parts.join('');
      afterRender(el);
    },
    // Poll refresh: patch data into the live DOM instead of rebuilding the
    // whole dashboard. It renders the same markup through the real mount into
    // a detached tree (so the two render paths can never drift apart), then
    // swaps in only the regions whose content actually changed. Canvases,
    // scroll position and hover state in untouched regions simply survive.
    // Returns true when the refresh was applied; false asks the shell to do a
    // normal mount instead (first paint, or an error state).
    async refresh(el, ctx) {
      if (!el.querySelector('[data-poll-region]')) return false;
      const stage = window.document.createElement('div');
      await this.mount(stage, ctx);
      if (!stage.querySelector('[data-poll-region]')) return false;
      const liveRegions = {};
      el.querySelectorAll('[data-poll-region]').forEach((node) => {
        liveRegions[node.getAttribute('data-poll-region')] = node;
      });
      const changed = [];
      stage.querySelectorAll('[data-poll-region]').forEach((fresh) => {
        const id = fresh.getAttribute('data-poll-region');
        const live = liveRegions[id];
        if (!live) return;                     // structure shifted — leave it to a normal mount
        delete liveRegions[id];
        if (live.innerHTML !== fresh.innerHTML) {
          live.innerHTML = fresh.innerHTML;
          changed.push(live);
        }
      });
      if (changed.length) {
        if (LL.charts && typeof LL.charts.prune === 'function') LL.charts.prune();
        changed.forEach((region) => afterRender(region));
      }
      return true;
    },
  };

  function cssVarToHex(value) {
    const raw = String(value || '').trim();
    if (raw.startsWith('var(')) {
      const name = raw.slice(4, -1).trim();
      return ui.token(name, '#1e5bff');
    }
    return raw || '#1e5bff';
  }

  function activityIcon(type) {
    switch (type) {
      case 'user_registered': return 'i-users-add';
      case 'device_registered': return 'i-devices';
      case 'device_revoked': return 'i-ban';
      case 'file_uploaded': return 'i-transfer';
      case 'call_started': return 'i-call';
      case 'group_created': return 'i-users';
      case 'recovery_request': return 'i-restore';
      case 'security_event': return 'i-shield';
      case 'admin_action': return 'i-audit';
      default: return 'i-activity';
    }
  }

  // ------------------------------------------------------------ Bundle 216 kit
  // People lookup (name, photo, admin level, blocked) shared by the pages so a
  // crown or a grey photo looks the same everywhere. Cached for 30 seconds.
  async function people216(ctx) {
    const cache = ctx.state.people216;
    if (cache && Date.now() - cache.at < 30000) return cache.map;
    const map = {};
    try {
      const data = await ctx.api('/api/v1/admin/users?limit=200');
      (Array.isArray(data.users) ? data.users : []).forEach((u) => { map[u.id] = u; });
    } catch (err) {
      // Names still come from each list's own fields.
    }
    ctx.state.people216 = { at: Date.now(), map };
    return map;
  }

  function who216(map, userId, name, sub, extra) {
    const u = (userId && map[userId]) || {};
    return ui.person216(Object.assign({
      userId: userId,
      name: name || u.display_name || u.username || '—',
      sub: sub,
      hasAvatar: !!u.has_avatar,
      admin: !!u.admin_role,
      blocked: u.status === 'suspended',
    }, extra || {}));
  }

  const STATUS_216 = {
    active: ['Active', 'green', 'Can use LinkUp'],
    suspended: ['Suspended', 'orange', "Can't sign in for now"],
    disabled: ['Off', 'red', 'Account closed'],
  };
  function status216(s) {
    const x = STATUS_216[s] || [fmt.titleCase(s || 'unknown'), 'grey'];
    return ui.pill216(x[0], x[1]);
  }

  function linkNow216(d) {
    if (d.status === 'revoked') return ui.pill216('Removed', 'red');
    if (d.online || d.server_connected) return ui.pill216('Server', 'green');
    if (d.wifi_direct_connected) return ui.pill216('Wi-Fi Direct', 'orange');
    if (d.mesh_available) return ui.pill216('Mesh', 'purple');
    if (d.lan_available) return ui.pill216('Local Wi-Fi', 'teal');
    return ui.pill216('Offline', 'grey');
  }

  function head216(title, sub, right) {
    return `<div class="bar216" style="justify-content:space-between"><div><div style="font-size:15px;font-weight:600">${ui.esc(title)}</div>${sub ? `<div class="type-caption">${ui.esc(sub)}</div>` : ''}</div><div class="row gap-sm">${right || ''}</div></div>`;
  }

  const WIFI_RULES_216 = [
    { value: '', label: 'Default' }, { value: 'allowed', label: 'Allowed' },
    { value: 'forced_on', label: 'Always on' }, { value: 'forced_off', label: 'Always off' },
  ];

  // ---------------------------------------------------------------------- users
  // W4 + W5: chips instead of drop-downs, photo + crown + level in the row and
  // one Manage button that opens the side panel.
  const users = {
    id: 'users',
    title: 'Users',
    subtitle: 'People with an account. Make admin, change status.',
    icon: 'i-users',
    group: 'People',
    navCount: 'users',
    async mount(el, ctx) {
      const host = document.getElementById('modal-host');
      if (host && host.querySelector('.side216')) host.innerHTML = '';
      const query = ctx.state.filters.users || { search: '', status: '', admin: '' };
      el.innerHTML = ui.bootSkeleton219('users'); // Bundle 219
      const result = await settle(ctx.api('/api/v1/admin/users?limit=200'));
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Users', body: ui.errorState(result.error, 'users') }); return; }

      const all = Array.isArray(result.data.users) ? result.data.users : [];
      const map = {};
      all.forEach((u) => { map[u.id] = u; });
      ctx.state.people216 = { at: Date.now(), map };
      const search = (query.search || '').toLowerCase().replace(/^@/, '');
      const filtered = all.filter((u) => {
        const hay = `${u.display_name || ''} ${u.username || ''}`.toLowerCase();
        if (search && hay.indexOf(search) < 0) return false;
        if (query.status && u.status !== query.status) return false;
        // Bundle 214: Admins filter (key: 'admin').
        if (query.admin === 'yes' && !u.admin_role) return false;
        if (query.admin === 'no' && u.admin_role) return false;
        return true;
      });
      const adminCount = all.filter((u) => u.admin_role).length;
      const chip = query.admin === 'yes' ? 'admins' : (query.status || 'all');
      const columns = [
        { label: 'Person' }, { label: 'Admin' }, { label: 'Phones', numeric: true },
        { label: 'Status', status: true }, { label: 'Last seen' }, { label: '' },
      ].map((c) => (c.label === 'Admin' ? { label: 'Admin level' } : c));
      const rows = filtered.map((u) => [
        who216(map, u.id, u.display_name || u.username, '@' + (u.username || ''), { online: num(u.online_devices) > 0, size: 32 }),
        u.admin_role ? ui.pill216(ui.LEVELS_216[u.admin_role] || u.admin_role, 'gold') : '<span class="subtle">\u2014</span>',
        `<span>${fmt.number(u.devices)}</span>`,
        status216(u.status),
        `<span>${ui.esc(num(u.online_devices) > 0 ? 'Online now' : ui.when216(u.last_seen_at))}</span>`,
        `<button class="btn btn-sm" data-user-manage="${ui.esc(u.id)}">Manage</button>`,
      ]);

      el.innerHTML = `
        <div class="section">
          <div class="bar216" style="justify-content:space-between">
            <div class="bar216" style="margin:0">
              <input class="input search216" type="search" data-filter-search placeholder="Search name or @username" value="${ui.esc(query.search || '')}" aria-label="Search name or @username">
              ${ui.chips216([
                { value: 'all', label: 'All' }, { value: 'admins', label: 'Admins' }, { value: 'active', label: 'Active' },
                { value: 'suspended', label: 'Suspended' }, { value: 'disabled', label: 'Off' },
              ], chip, 'data-users-chip')}
            </div>
            <div class="row gap-sm" title="app admins">${ui.pill216(`${adminCount} admins`, 'gold')} ${ui.pill216(`${all.length} people`, 'grey')}</div>
          </div>
          ${ui.panel({
            pad0: true,
            body: `${rows.length ? `<div style="padding:0 4px">${ui.table({ columns, rows })}</div>` : ''}
              <div class="empty216">${rows.length ? '' : 'No one found? Try another name or filter.'}</div>`,
          })}
        </div>`;

      el.querySelectorAll('[data-users-chip]').forEach((b) => b.addEventListener('click', () => {
        const v = b.getAttribute('data-users-chip');
        ctx.state.filters.users = Object.assign({}, query, { admin: v === 'admins' ? 'yes' : '', status: ['active', 'suspended', 'disabled'].indexOf(v) >= 0 ? v : '' });
        ctx.refresh();
      }));
      el.querySelectorAll('[data-user-manage]').forEach((b) => b.addEventListener('click', () => {
        const u = map[b.getAttribute('data-user-manage')];
        if (u) manage216(u, ctx);
      }));
    },
  };

  // W5: the side panel. Buttons reuse the shell handlers (data-user-status,
  // data-user-admin with data-admin-mode="make" / "remove" / "level").
  // Bundle 218: date only ("5 Nov 2026") for the "Joined from offline" tag.
  function joinedDate218(iso) {
    const d = new Date(iso);
    return isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  }

  async function manage216(u, ctx) {
    const host = document.getElementById('modal-host');
    if (!host) return;
    const owner = ctx.can('manageUsers');
    const canRule = ctx.can('manageNetworkPolicy');
    const name = u.display_name || u.username || '';
    const data = `data-user-admin="${ui.esc(u.id)}" data-user-name="${ui.esc(name)}" data-user-role="${ui.esc(u.admin_role || '')}"`;
    const levels = [
      ['', 'Not admin', 'Normal user'],
      ['owner', 'Owner', 'Everything, also blocking and app version'],
      ['operator', 'Operator', 'Phones, Wi-Fi rules, recovery'],
      ['security', 'Security', 'Recovery requests, can look at the rest'],
      ['auditor', 'Auditor', 'Can only look'],
    ];
    const levelCard = ([value, label, help]) => {
      const on = (u.admin_role || '') === value;
      let attrs = 'disabled';
      if (owner && !on) {
        if (!value) attrs = `${data} data-admin-mode="remove"`;
        else if (u.admin_role) attrs = `${data} data-admin-mode="level" data-admin-pick="${value}"`;
        else attrs = `${data} data-admin-mode="make" data-admin-pick="${value}"`;
      }
      return `<button type="button" class="opt216 ${on ? 'on' : ''}" ${attrs}><b>${value ? ui.crown216(12) + ' ' : ''}${ui.esc(label)}</b><small>${ui.esc(help)}</small></button>`;
    };
    const statusCard = (s) => {
      const x = STATUS_216[s];
      const on = u.status === s;
      return `<button type="button" class="opt216 ${on ? 'on' : ''}" ${owner && !on ? `data-user-status="${ui.esc(u.id)}" value="${s}"` : 'disabled'}><b>${ui.esc(x[0])}</b><small>${ui.esc(x[2])}</small></button>`;
    };
    host.innerHTML = `<div class="side216-scrim" data-side216-close></div>
      <aside class="side216" role="dialog" aria-modal="true" aria-label="Manage ${ui.esc(name)}">
        <div class="side216-head">
          <button class="btn btn-icon side216-back" data-side216-close aria-label="Back">${ui.icon('i-chevron-left')}</button>
          <strong class="side216-title">Manage ${ui.esc(name)}</strong>
          <button class="btn btn-icon" data-side216-close aria-label="Close">${ui.icon('i-close')}</button>
        </div>
        <div class="side216-person">
          ${ui.person216({ userId: u.id, name, sub: '@' + (u.username || ''), hasAvatar: !!u.has_avatar, admin: !!u.admin_role, blocked: u.status === 'suspended', size: 48 })}
          ${u.joined_offline_at ? `<span class="tag218">Joined from offline \u00b7 ${ui.esc(joinedDate218(u.joined_offline_at))}</span>` : ''}
        </div>
        <h3>Status</h3>${['active', 'suspended', 'disabled'].map(statusCard).join('')}
        <h3>Admin level</h3>${levels.map(levelCard).join('')}
        ${owner ? '' : '<div class="type-caption">Only an Owner can change status or admin level.</div>'}
        <h3>Wi-Fi rule</h3>
        <div data-wifi-rule-host>${ui.chips216(WIFI_RULES_216, '__loading', 'disabled data-wifi-rule')}</div>
        <h3>Phones</h3>
        <button class="btn btn-sm" data-user-devices="${ui.esc(u.id)}">See phones (${fmt.number(u.devices)})</button>
        ${owner ? `<button class="btn btn-sm btn-danger side216-danger" data-signout-all="${ui.esc(u.id)}">Sign out all phones</button>` : ''}
      </aside>`;
    host.querySelectorAll('[data-side216-close]').forEach((b) => b.addEventListener('click', () => { host.innerHTML = ''; }));
    try {
      const policy = await ctx.api(`/api/v1/admin/user/network-policy?id=${encodeURIComponent(u.id)}`);
      const current = (policy && policy.user_wifi_radio_policy) || '';
      const box = host.querySelector('[data-wifi-rule-host]');
      if (box) {
        box.innerHTML = ui.chips216(WIFI_RULES_216, current, canRule ? `data-user-id="${ui.esc(u.id)}" data-wifi-rule` : 'disabled data-wifi-rule');
      }
    } catch (err) {
      const box = host.querySelector('[data-wifi-rule-host]');
      if (box) box.innerHTML = `<div class="type-caption">${ui.esc(err.message)}</div>`;
    }
  }

  // -------------------------------------------------------------------- devices
  const devices = {
    id: 'devices',
    title: 'Devices',
    subtitle: 'Every phone that signed in. Rename or remove.',
    icon: 'i-devices',
    group: 'People',
    navCount: 'devices',
    async mount(el, ctx) {
      const query = ctx.state.filters.devices || { search: '', presence: '' };
      el.innerHTML = ui.bootSkeleton219('devices'); // Bundle 219
      const [result, map] = await Promise.all([settle(ctx.api('/api/v1/admin/devices?limit=200')), people216(ctx)]);
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Devices', body: ui.errorState(result.error, 'devices') }); return; }
      const all = Array.isArray(result.data.devices) ? result.data.devices : [];
      const search = (query.search || '').toLowerCase();
      const filtered = all.filter((d) => {
        const hay = `${d.name || ''} ${d.username || ''} ${d.display_name || ''}`.toLowerCase();
        if (search && hay.indexOf(search) < 0) return false;
        if (query.presence === 'online' && !d.online) return false;
        if (query.presence === 'removed' && d.status !== 'revoked') return false;
        return true;
      });
      const manage = ctx.can('manageDevices');
      const rows = filtered.map((d) => [
        `<div class="p216-t"><b>${emptyText(d.name)}</b><small>${emptyText(d.platform)}</small></div>`,
        d.user_id ? who216(map, d.user_id, d.display_name || d.username, '@' + (d.username || ''), { size: 28 }) : '<span class="subtle">\u2014</span>',
        linkNow216(d),
        `<span>${ui.esc(d.online ? 'Online now' : ui.when216(d.last_seen_at))}</span>`,
        `<span>${d.app_build ? `build ${ui.esc(d.app_build)}` : '\u2014'}</span>`,
        tableActions(manage && d.status !== 'revoked' ? [
          `<button class="btn btn-sm" data-device-rename="${ui.esc(d.id)}" data-device-name="${ui.esc(d.name)}">Rename</button>`,
          `<button class="btn btn-sm" data-device-policy="${ui.esc(d.id)}">Wi-Fi rule</button>`,
          `<button class="btn btn-sm btn-danger" data-device-revoke="${ui.esc(d.id)}">Remove phone</button>`,
        ] : []),
      ]);
      const online = all.filter((d) => d.online).length;
      el.innerHTML = `
        <div class="section">
          <div class="bar216" style="justify-content:space-between">
            <div class="bar216" style="margin:0">
              <input class="input search216" type="search" data-filter-search placeholder="Search phone or person" value="${ui.esc(query.search || '')}" aria-label="Search phone or person">
              ${ui.chips216([{ value: '', label: 'All' }, { value: 'online', label: 'Online' }, { value: 'removed', label: 'Removed' }], query.presence || '', 'data-devices-chip')}
            </div>
            <div class="row gap-sm">${ui.pill216(`${online} online`, 'green')} ${ui.pill216(`${all.length} phones`, 'grey')}</div>
          </div>
          ${ui.panel({
            pad0: true,
            body: rows.length
              ? `<div style="padding:0 4px">${ui.table({ columns: [{ label: 'Phone' }, { label: 'Owner' }, { label: 'Link now', status: true }, { label: 'Last seen' }, { label: 'Build', hideTablet: true }, { label: '' }], rows })}</div>`
              : '<div class="empty216">No phones found. Try another name or filter.</div>',
          })}
        </div>`;
      el.querySelectorAll('[data-devices-chip]').forEach((b) => b.addEventListener('click', () => {
        ctx.state.filters.devices = Object.assign({}, query, { presence: b.getAttribute('data-devices-chip') });
        ctx.refresh();
      }));
    },
  };

  // ------------------------------------------------------------------- sessions
  const sessions = {
    id: 'sessions',
    title: 'Sessions',
    subtitle: 'Who is signed in right now',
    icon: 'i-key',
    group: 'People',
    navCount: 'active_sessions',
    async mount(el, ctx) {
      el.innerHTML = ui.bootSkeleton219('sessions'); // Bundle 219
      const [userSessions, adminSessions, map] = await Promise.all([
        settle(ctx.api('/api/v1/admin/sessions?limit=200')),
        settle(ctx.api('/api/v1/admin/admin-sessions')),
        people216(ctx),
      ]);
      const owner = ctx.can('manageSessions');
      const people = !userSessions.ok ? ui.errorState(userSessions.error, 'sessions') : (() => {
        const list = (Array.isArray(userSessions.data.sessions) ? userSessions.data.sessions : []).filter((s) => s.active);
        if (!list.length) return '<div class="empty216">No one is signed in.</div>';
        return ui.table({
          columns: [{ label: 'Person' }, { label: 'Phone' }, { label: 'Since' }, { label: 'Last used' }, { label: '' }],
          rows: list.map((s) => [
            who216(map, s.user_id, s.display_name || s.username, '@' + (s.username || ''), { size: 28 }),
            emptyText(s.device_name),
            `<span>${ui.esc(fmt.dateTime(s.created_at))}</span>`,
            `<span>${ui.esc(ui.when216(s.last_seen_at))}</span>`,
            owner ? `<button class="btn btn-sm" data-session-revoke="${ui.esc(s.id)}" data-session-kind="user">Sign out</button>` : '',
          ]),
        });
      })();
      const admins = !adminSessions.ok ? ui.errorState(adminSessions.error, 'sessions') : (() => {
        const list = (Array.isArray(adminSessions.data.sessions) ? adminSessions.data.sessions : []).filter((s) => s.active);
        if (!list.length) return '<div class="empty216">No admin is signed in.</div>';
        return ui.table({
          columns: [{ label: 'Admin' }, { label: 'Ends' }, { label: 'Since' }, { label: '' }],
          rows: list.map((s) => [
            ui.person216({ name: s.admin_username || s.username, sub: ui.LEVELS_216[s.role] || fmt.titleCase(s.role || ''), admin: true, size: 28 }),
            `<span>${ui.esc(fmt.dateTime(s.expires_at))}</span>`,
            `<span>${ui.esc(fmt.dateTime(s.created_at))}</span>`,
            s.current ? ui.pill216('This one', 'blue')
              : (owner ? `<button class="btn btn-sm" data-session-revoke="${ui.esc(s.id)}" data-session-kind="admin">Sign out</button>` : ''),
          ]),
        });
      })();
      el.innerHTML = `
        <div class="section">
          ${ui.panel({ title: 'People signed in', body: people })}
          ${ui.panel({ title: 'Admins signed in', body: admins })}
        </div>`;
    },
  };

  // -------------------------------------------------------------------- network
  // W8: 4 tiles and 'Phones right now' (Phone / Owner / Link now / Linked with)
  // from the link map (216h), so names and lines match the Map page.
  const network = {
    id: 'network',
    title: 'Network & mesh',
    subtitle: 'How phones reach each other',
    icon: 'i-wifi',
    group: 'Network',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const [links, net] = await Promise.all([settle(ctx.api('/api/v1/admin/link-map')), settle(ctx.api('/api/v1/admin/network'))]);
      if (!links.ok) { el.innerHTML = ui.panel({ title: 'Network & mesh', body: ui.errorState(links.error, 'network') }); return; }
      const nodes = Array.isArray(links.data.nodes) ? links.data.nodes : [];
      const edges = Array.isArray(links.data.edges) ? links.data.edges : [];
      const byId = {};
      nodes.forEach((n) => { byId[n.device_id] = n; });
      const nameOf = (n) => n.display_name || n.username || n.device_name || 'Phone';
      const peers = (id) => edges.filter((e) => e.from !== 'server' && (e.from === id || e.to === id))
        .map((e) => byId[e.from === id ? e.to : e.from]).filter(Boolean).map(nameOf);
      const reached = nodes.filter((n) => n.online || n.link);
      const count = (k) => nodes.filter((n) => n.link === k).length;
      const data = net.ok ? net.data : {};
      const tile = (label, value, color) => `<div class="tile216"><small>${ui.esc(label)}</small><b style="color:${color}">${ui.esc(fmt.number(value))}</b></div>`;
      el.innerHTML = `
        <div class="section">
          <div class="tiles216">
            ${tile('Phones online', reached.length, 'var(--text-primary)')}
            ${tile('On the server', count('server'), '#4ade80')}
            ${tile('Wi-Fi Direct', Math.max(count('wifi_direct'), num(data.wifi_direct_connected)), '#fbbf24')}
            ${tile('Mesh', Math.max(count('mesh'), num(data.mesh_available)), '#b49cff')}
          </div>
          ${ui.panel({
            title: 'Phones right now',
            body: reached.length ? ui.table({
              columns: [{ label: 'Phone' }, { label: 'Owner' }, { label: 'Link now' }, { label: 'Linked with' }],
              rows: reached.map((n) => {
                const l = n.blocked ? null : ui.LINKS_216[n.link];
                const with216 = peers(n.device_id);
                return [
                  `<b>${emptyText(n.device_name)}</b>`,
                  ui.person216({ userId: n.user_id, name: nameOf(n), hasAvatar: n.has_avatar, admin: !!n.admin_role, blocked: n.blocked, size: 28 }),
                  n.blocked ? ui.pill216('Blocked', 'red') : (l ? ui.pill216(l.label, l.tone) : ui.pill216('Offline', 'grey')),
                  with216.length ? ui.esc(with216.join(', ')) : '<span class="subtle">\u2014</span>',
                ];
              }),
            }) : '<div class="empty216">No phone is online right now.</div>',
          })}
          <div class="row gap-sm"><button class="btn btn-sm" data-nav="map">Open map \u203a</button></div>
        </div>`;
    },
  };

  // ---------------------------------------------------------------------- calls
  const calls = {
    id: 'calls',
    title: 'Calls',
    subtitle: 'Voice calls between phones',
    icon: 'i-call',
    group: 'Network',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const [result, map] = await Promise.all([settle(ctx.api('/api/v1/admin/calls?limit=200')), people216(ctx)]);
      const list = result.ok && Array.isArray(result.data.calls) ? result.data.calls : [];
      const state216 = (c) => {
        if (!c.ended_at) return ui.pill216('Live', 'blue');
        if (num(c.duration_seconds) > 0) return ui.pill216('Done', 'green');
        const r = String(c.end_reason || '').toLowerCase();
        if (r.indexOf('declin') >= 0 || r.indexOf('reject') >= 0) return ui.pill216('Declined', 'grey');
        return ui.pill216('Missed', 'orange');
      };
      el.innerHTML = `
        <div class="section">
          ${ui.panel({
            pad0: true,
            body: !result.ok ? `<div style="padding:16px">${ui.errorState(result.error, 'calls')}</div>`
              : (list.length ? `<div style="padding:0 4px">${ui.table({
                columns: [{ label: 'Call' }, { label: 'Type' }, { label: 'Length' }, { label: 'When' }, { label: '' }],
                rows: list.map((c) => [
                  who216(map, c.caller_user_id, c.caller_name || c.caller_id, `to ${c.callee_name || c.callee_id || ''}`, { size: 28 }),
                  'Voice',
                  num(c.duration_seconds) > 0 ? ui.esc(fmt.duration(num(c.duration_seconds))) : '<span class="subtle">\u2014</span>',
                  `<span>${ui.esc(ui.when216(c.started_at) === 'Online now' ? 'Just now' : ui.when216(c.started_at))}</span>`,
                  state216(c),
                ]),
              })}</div>` : '<div class="empty216">No calls yet.</div>'),
          })}
        </div>`;
    },
  };

  // ------------------------------------------------------------------ transfers
  const KIND_216 = { photo: 'Photo', video: 'Video', voice: 'Voice note', document: 'Document' };
  const transfers = {
    id: 'transfers',
    title: 'File transfers',
    subtitle: 'Files sent through the server (never the file names)',
    icon: 'i-transfer',
    group: 'Network',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const [result, summary, map] = await Promise.all([
        settle(ctx.api('/api/v1/admin/transfers?limit=200')),
        settle(ctx.api('/api/v1/admin/summary')),
        people216(ctx),
      ]);
      const list = result.ok && Array.isArray(result.data.transfers) ? result.data.transfers : [];
      const used = summary.ok ? fmt.bytes(num(summary.data.file_storage_bytes)) : '';
      el.innerHTML = `
        <div class="section">
          ${ui.panel({
            pad0: true,
            body: !result.ok ? `<div style="padding:16px">${ui.errorState(result.error, 'transfers')}</div>`
              : (list.length ? `<div style="padding:0 4px">${ui.table({
                columns: [{ label: 'Kind' }, { label: 'Sent by' }, { label: 'Size', numeric: true }, { label: 'When' }, { label: '' }],
                rows: list.map((t) => [
                  `<b>${ui.esc(KIND_216[t.kind] || 'Document')}</b>`,
                  t.owner_user_id ? who216(map, t.owner_user_id, t.owner_name, '', { size: 28 }) : emptyText(t.owner_name),
                  ui.esc(fmt.bytes(num(t.size))),
                  `<span>${ui.esc(fmt.dateTime(t.created_at))}</span>`,
                  String(t.status || '').toLowerCase() === 'complete' ? ui.pill216('Sent', 'green') : ui.pill216(fmt.titleCase(t.status || 'waiting'), 'grey'),
                ]),
              })}</div>` : '<div class="empty216">No files yet.</div>'),
          })}
          ${used ? `<div class="type-caption">${ui.esc(used)} used</div>` : ''}
        </div>`;
    },
  };

  // ------------------------------------------------------------------- security
  // Page 20 words: one line per warning, grouped wrong passwords.
  function securityRows216(events, threshold) {
    const out = [];
    const failed = {};
    events.forEach((e) => {
      const who = e.username ? '@' + e.username : (e.display_name || '');
      const when = fmt.dateTime(e.created_at);
      const phone = e.device_name ? ` \u00b7 ${e.device_name}` : '';
      const a = String(e.action || '');
      if (a === 'login_failed') {
        const key = `${e.user_id}|${String(e.created_at || '').slice(0, 10)}`;
        if (!failed[key]) { failed[key] = { n: 0, who, when, at: e.created_at }; out.push(failed[key]); }
        failed[key].n += 1;
        return;
      }
      let title = fmt.titleCase(a.replace(/_/g, ' '));
      let tag = ['Info', 'grey'];
      if (a === 'device_revoked') { title = 'Phone removed'; tag = ['Small', 'orange']; }
      else if (a === 'session_revoked' || a === 'admin_session_revoked') { title = 'Signed out by admin'; tag = ['Small', 'orange']; }
      else if (a === 'admin_user_status_changed') {
        const back = String(e.detail || '').indexOf('active') >= 0;
        title = back ? 'Account turned on again' : 'Account stopped';
        tag = back ? ['Done', 'green'] : ['Small', 'orange'];
      } else if (a === 'rotate_identity_key') title = 'New security key';
      else if (a === 'recovery_code_disable') { title = 'Recovery code turned off'; tag = ['Small', 'orange']; }
      else if (a === 'recovery_code_rotate') title = 'New recovery code';
      else if (a === 'login_success') title = 'Signed in';
      else if (a.indexOf('account_recovery_') === 0) {
        const step = a.slice('account_recovery_'.length);
        title = 'Recovery ' + ({ requested: 'asked', approved: 'approved', completed: 'finished', reissued: 'new code' }[step] || step.replace(/_/g, ' '));
      } else if (a.indexOf('account_transfer_') === 0) title = 'Moved to a new phone';
      out.push({ title, sub: `${who}${phone} \u00b7 ${when}`, tag });
    });
    return out.map((r) => {
      if (r.n === undefined) return r;
      const serious = r.n >= Math.max(1, threshold);
      return { title: `${r.n} wrong password${r.n === 1 ? '' : 's'}`, sub: `${r.who} \u00b7 ${r.when}`, tag: serious ? ['Serious', 'red'] : ['Small', 'orange'] };
    });
  }

  const security = {
    id: 'security',
    title: 'Security',
    subtitle: 'Wrong passwords and other warnings',
    icon: 'i-shield',
    group: 'Safety',
    async mount(el, ctx) {
      el.innerHTML = ui.bootSkeleton219('security'); // Bundle 219
      const [summary, events, settings] = await Promise.all([
        settle(ctx.api('/api/v1/admin/security-summary')),
        settle(ctx.api('/api/v1/admin/security-events?limit=200')),
        settle(ctx.api('/api/v1/admin/settings')),
      ]);
      const counts = summary.ok ? summary.data.counts || {} : {};
      const threshold = settings.ok ? num(settings.data.security_alert_threshold) || 1 : 1;
      const list = events.ok && Array.isArray(events.data.security_events) ? events.data.security_events : [];
      const weekAgo = Date.now() - 7 * 86400000;
      const rows = securityRows216(list.filter((e) => new Date(e.created_at).getTime() >= weekAgo), threshold);
      const serious = rows.filter((r) => r.tag[0] === 'Serious').length;
      const small = rows.filter((r) => r.tag[0] === 'Small').length;
      el.innerHTML = `
        <div class="section">
          <div class="tiles216" style="grid-template-columns:repeat(3,minmax(0,1fr))">
            <div class="tile216"><small>Serious</small><b style="color:#f87171">${fmt.number(serious)}</b></div>
            <div class="tile216"><small>Small</small><b style="color:#fbbf24">${fmt.number(small)}</b></div>
            <div class="tile216"><small>Locked</small><b>${fmt.number(counts.locked_users)}</b></div>
          </div>
          ${ui.panel({
            title: 'This week',
            body: !events.ok ? ui.errorState(events.error, 'security')
              : (rows.length ? rows.map((r) => `<div class="map216-row" style="min-height:64px;border-bottom:1px solid var(--border)">
                  <div class="p216-t"><b>${ui.esc(r.title)}</b><small>${ui.esc(r.sub)}</small></div>${ui.pill216(r.tag[0], r.tag[1])}</div>`).join('')
                : '<div class="empty216"><b>No warnings</b><br>Wrong passwords and other warnings show here.</div>'),
          })}
        </div>`;
    },
  };

  // ---------------------------------------------------------------------- audit
  // Page 21 sentences, names from the server (216h).
  function auditSentence216(e) {
    const admin = e.admin_name || 'An admin';
    const target = e.target_name || 'someone';
    const a = String(e.action || '');
    const d = String(e.detail || '');
    const level = (d.match(/role=([a-z]+)/) || [])[1];
    if (a === 'admin_block_block') return [`${admin} blocked ${target}`, 'red', 'blocks'];
    if (a === 'admin_block_unblock') return [`${admin} unblocked ${target}`, 'green', 'blocks'];
    if (a === 'version_rule') return [`${admin} set minimum version ${(d.match(/min_version=([^ ]+)/) || [])[1] || ''}`.trim(), 'purple', 'admins'];
    if (a.indexOf('user_admin') === 0 || a === 'admin_offline_cert') {
      if (a.indexOf('remove') >= 0) return [`${admin} removed admin from ${target}`, 'gold', 'admins'];
      return [`${admin} made ${target} an admin${level ? ` (${ui.LEVELS_216[level] || level})` : ''}`, 'gold', 'admins'];
    }
    if (a.indexOf('recovery') >= 0) {
      const verb = a.indexOf('reject') >= 0 ? 'rejected' : (a.indexOf('reissue') >= 0 ? 'made a new code for' : 'approved');
      return [`${admin} ${verb} recovery for ${target}`, 'orange', 'recovery'];
    }
    if (a === 'admin_conversation_viewed') return [`${admin} opened chat ${target}`, 'grey', ''];
    if (a === 'rename_device') return [`${admin} renamed ${target}`, 'grey', ''];
    if (a === 'revoke_device') return [`${admin} removed ${target}`, 'grey', ''];
    if (a === 'revoke_session') return [`${admin} signed out ${target}`, 'grey', ''];
    if (a === 'set_user_status') {
      const st = (d.match(/status=([a-z]+)/) || [])[1];
      return [`${admin} set ${target} to ${(STATUS_216[st] || [fmt.titleCase(st || 'new status')])[0]}`, 'grey', ''];
    }
    if (/^update_.*network_policy$/.test(a)) return [`${admin} changed the Wi-Fi rule of ${target}`, 'teal', ''];
    if (a === 'update_admin_settings') return [`${admin} changed admin settings`, 'grey', ''];
    if (a === 'backup_database') return [`${admin} made a backup`, 'grey', ''];
    if (a === 'admin_login') return [`${admin} signed in`, 'grey', ''];
    if (a === 'admin_logout' || a === 'logout') return [`${admin} signed out`, 'grey', ''];
    if (a === 'admin_permission_denied') return [`${admin} was stopped (no right)`, 'red', ''];
    const subject = e.admin_name || (e.admin_id ? admin : (e.target_name || 'Someone'));
    return [`${subject} · ${a.replace(/_/g, ' ')}`, 'grey', ''];
  }

  const audit = {
    id: 'audit',
    title: 'Audit log',
    subtitle: 'Everything admins did, newest first',
    icon: 'i-audit',
    group: 'Safety',
    async mount(el, ctx) {
      const query = ctx.state.filters.audit || { search: '' };
      const kind = query.kind || '';
      el.innerHTML = ui.bootSkeleton219('audit'); // Bundle 219
      const result = await settle(ctx.api('/api/v1/admin/logs?limit=200'));
      const list = result.ok && Array.isArray(result.data.logs) ? result.data.logs : [];
      const today = new Date(); today.setHours(0, 0, 0, 0);
      const yesterday = new Date(today.getTime() - 86400000);
      const groups = { Today: [], Yesterday: [], Earlier: [] };
      list.forEach((e) => {
        const s = auditSentence216(e);
        if (kind && s[2] !== kind) return;
        const t = new Date(e.created_at);
        const g = t >= today ? 'Today' : (t >= yesterday ? 'Yesterday' : 'Earlier');
        groups[g].push({ s, e });
      });
      const body = Object.keys(groups).filter((g) => groups[g].length).map((g) => `
        <div style="font-size:11.5px;font-weight:600;color:var(--text-tertiary);margin:10px 0 4px">${g}</div>
        ${groups[g].map(({ s, e }) => {
          const tone = ui.TONES_216[s[1]] || ui.TONES_216.grey;
          return `<div class="map216-row" style="justify-content:flex-start;gap:12px">
            <span style="width:10px;height:10px;border-radius:50%;background:${tone[1]};flex:0 0 10px"></span>
            <div class="p216-t"><b>${ui.esc(s[0])}</b><small>${ui.esc(fmt.dateTime(e.created_at))}${s[2] === 'blocks' ? ' \u00b7 every phone' : ''}</small></div></div>`;
        }).join('')}`).join('');
      el.innerHTML = `
        <div class="section">
          <div class="bar216">${ui.chips216([
            { value: '', label: 'All' }, { value: 'blocks', label: 'Blocks' }, { value: 'admins', label: 'Admins' }, { value: 'recovery', label: 'Recovery' },
          ], kind, 'data-audit-chip')}</div>
          ${ui.panel({ body: !result.ok ? ui.errorState(result.error, 'audit') : (body || '<div class="empty216">Nothing yet</div>') })}
        </div>`;
      el.querySelectorAll('[data-audit-chip]').forEach((b) => b.addEventListener('click', () => {
        ctx.state.filters.audit = Object.assign({}, query, { kind: b.getAttribute('data-audit-chip') });
        ctx.refresh();
      }));
    },
  };

  // ------------------------------------------------------------------- recovery
  const recovery = {
    id: 'recovery',
    title: 'Recovery',
    subtitle: 'People who lost their phone and asked to get back in',
    icon: 'i-restore',
    group: 'Safety',
    async mount(el, ctx) {
      el.innerHTML = ui.bootSkeleton219('recovery'); // Bundle 219
      const [result, map] = await Promise.all([settle(ctx.api('/api/v1/admin/recovery-requests?limit=200')), people216(ctx)]);
      const list = result.ok && Array.isArray(result.data.requests || result.data.recovery_requests)
        ? (result.data.requests || result.data.recovery_requests) : [];
      const pending = list.filter((r) => String(r.status || '').toLowerCase() === 'pending');
      const earlier = list.filter((r) => String(r.status || '').toLowerCase() !== 'pending');
      const can = ctx.can('manageRecovery');
      const card = (r) => ui.panel({
        body: `<div class="map216-row">${who216(map, r.user_id, r.display_name || r.username, `@${r.username || ''} \u00b7 new phone: ${r.target_device_name || '\u2014'}`, { size: 40 })}
            ${ui.pill216('Waiting', 'orange')}</div>
          <div class="type-caption" style="margin:6px 0 10px">Asked ${ui.esc(fmt.dateTime(r.created_at))} \u00b7 ends ${ui.esc(fmt.dateTime(r.expires_at))}</div>
          ${can ? `<textarea class="input" data-reason-for="${ui.esc(r.id)}" rows="2" maxlength="200" placeholder="Reason (only if you reject)" style="width:100%;height:auto;padding:8px"></textarea>
          <div class="row gap-sm" style="margin-top:10px">
            <button class="btn btn-primary" data-recovery="approve" data-id="${ui.esc(r.id)}">Approve</button>
            <button class="btn" data-recovery="reissue" data-id="${ui.esc(r.id)}">New code</button>
            <button class="btn btn-danger" data-recovery="reject" data-id="${ui.esc(r.id)}">Reject</button>
          </div>` : '<div class="type-caption">Your admin level can only look.</div>'}`,
      });
      const tone = { approved: ['Approved', 'green'], rejected: ['Rejected', 'red'], completed: ['Finished', 'green'], expired: ['Ended', 'grey'] };
      el.innerHTML = `
        <div class="section">
          ${!result.ok ? ui.panel({ body: ui.errorState(result.error, 'recovery') }) : ''}
          ${pending.map(card).join('')}
          ${result.ok && !pending.length ? ui.panel({ body: '<div class="empty216"><b>No one is waiting</b><br>Requests show here when someone starts recovery on a new phone.</div>' }) : ''}
          ${earlier.length ? ui.panel({
            title: 'Earlier',
            body: ui.table({
              columns: [{ label: 'Person' }, { label: 'New phone' }, { label: 'When' }, { label: '' }],
              rows: earlier.map((r) => {
                const t = tone[String(r.status || '').toLowerCase()] || [fmt.titleCase(r.status || ''), 'grey'];
                return [who216(map, r.user_id, r.display_name || r.username, '@' + (r.username || ''), { size: 28 }),
                  emptyText(r.target_device_name), `<span>${ui.esc(fmt.dateTime(r.created_at))}</span>`, ui.pill216(t[0], t[1])];
              }),
            }),
          }) : ''}
        </div>`;
    },
  };

  // ------------------------------------------------------------------------ map
  // W3: who is linked to whom, live. One node per phone (photo + name), lines
  // from what phones report (216g) plus their server link (216h link-map).
  const mapView = { x: 0, y: 0, k: 1, filter: 'all', pick: '' };

  function layout216(nodes, edges) {
    const W = 1000; const H = 700;
    const pos = { server: { x: W / 2, y: 110, fixed: true } };
    const online = nodes.filter((n) => n.online);
    const offline = nodes.filter((n) => !n.online);
    online.forEach((n, i) => {
      const a = Math.PI * (0.15 + 0.7 * ((i + 0.5) / Math.max(1, online.length)));
      pos[n.device_id] = { x: W / 2 - Math.cos(a) * 330, y: 110 + Math.sin(a) * 300 };
    });
    offline.forEach((n, i) => {
      pos[n.device_id] = { x: 120 + ((i + 0.5) / Math.max(1, offline.length)) * (W - 240), y: H - 90 };
    });
    // A few relaxation steps keep linked phones near each other without overlap.
    const ids = nodes.map((n) => n.device_id);
    for (let step = 0; step < 120; step += 1) {
      ids.forEach((a) => ids.forEach((b) => {
        if (a >= b) return;
        const p = pos[a]; const q = pos[b];
        const dx = q.x - p.x; const dy = q.y - p.y; const d = Math.max(1, Math.hypot(dx, dy));
        if (d < 120) { const f = (120 - d) / 2 / d; p.x -= dx * f; p.y -= dy * f; q.x += dx * f; q.y += dy * f; }
      }));
      edges.forEach((e) => {
        if (e.from === 'server' || !pos[e.from] || !pos[e.to]) return;
        const p = pos[e.from]; const q = pos[e.to];
        const dx = q.x - p.x; const dy = q.y - p.y; const d = Math.max(1, Math.hypot(dx, dy));
        if (d > 200) { const f = (d - 200) * 0.02 / d; p.x += dx * f; p.y += dy * f; q.x -= dx * f; q.y -= dy * f; }
      });
      ids.forEach((id) => { const p = pos[id]; p.x = Math.min(W - 60, Math.max(60, p.x)); p.y = Math.min(H - 70, Math.max(190, p.y)); });
    }
    return pos;
  }

  // Bundle 217 (§2.1): quadratic aerial path p -> q, bowing away from C.
  function arc217(p, q, C, k) {
    const dx = q.x - p.x; const dy = q.y - p.y;
    const len = Math.hypot(dx, dy) || 1;
    const mid = { x: (p.x + q.x) / 2, y: (p.y + q.y) / 2 };
    let n = { x: -dy / len, y: dx / len };
    const ox = mid.x - C.x; const oy = mid.y - C.y;
    if (Math.hypot(ox, oy) < 1) { if (n.y > 0) n = { x: -n.x, y: -n.y }; } else if (ox * n.x + oy * n.y < 0) n = { x: -n.x, y: -n.y };
    const c = { x: mid.x + n.x * k * len, y: mid.y + n.y * k * len };
    const f = (v) => Math.round(v * 10) / 10;
    return `M${f(p.x)} ${f(p.y)} Q${f(c.x)} ${f(c.y)} ${f(q.x)} ${f(q.y)}`;
  }

  // Bundle 217: fit all shown points into the stage (60 px sides/top, 80 px bottom).
  function fit217(points, stageW, stageH) {
    if (!points.length || !stageW || !stageH) return null;
    const xs = points.map((p) => p.x); const ys = points.map((p) => p.y);
    const minX = Math.min(...xs) - 60; const maxX = Math.max(...xs) + 60;
    const minY = Math.min(...ys) - 60; const maxY = Math.max(...ys) + 80;
    const bw = Math.max(1, maxX - minX); const bh = Math.max(1, maxY - minY);
    const k = Math.min(1.6, Math.max(0.4, Math.min(stageW / bw, stageH / bh)));
    return { k, x: (stageW - bw * k) / 2 - minX * k, y: (stageH - bh * k) / 2 - minY * k };
  }

  const map = {
    id: 'map',
    title: 'Map',
    subtitle: 'Who is linked to whom, live',
    icon: 'i-hub',
    group: 'Network',
    isNew: true,
    async refresh(el, ctx) { await this.mount(el, ctx, true); return true; },
    async mount(el, ctx, quiet) {
      if (!quiet) el.innerHTML = ui.bootSkeleton219('map'); // Bundle 219
      const result = await settle(ctx.api('/api/v1/admin/link-map'));
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Map', body: ui.errorState(result.error, 'map') }); return; }
      const nodes = Array.isArray(result.data.nodes) ? result.data.nodes : [];
      const edges = Array.isArray(result.data.edges) ? result.data.edges : [];
      const linkOf = (n) => (n.blocked ? 'blocked' : (n.link || 'offline'));
      const count = (k) => nodes.filter((n) => linkOf(n) === k).length;
      const chips = [{ value: 'all', label: `All ${nodes.length}` }];
      [['server', 'Server'], ['wifi_direct', 'Wi-Fi Direct'], ['mesh', 'Mesh'], ['lan', 'Local Wi-Fi'], ['blocked', 'Blocked']].forEach(([k, label]) => {
        const c = count(k);
        if (c || k === 'server') chips.push({ value: k, label: `${label} ${c}` });
      });
      const pos = layout216(nodes, edges);
      const shown = (n) => mapView.filter === 'all' || linkOf(n) === mapView.filter;
      // Bundle 217: aerial (flight-path) lines that bow away from the centre.
      const allPts = nodes.map((n) => pos[n.device_id]).filter(Boolean).concat([pos.server]);
      const C = allPts.reduce((a, p) => ({ x: a.x + p.x / allPts.length, y: a.y + p.y / allPts.length }), { x: 0, y: 0 });
      const lines = edges.map((e, i) => {
        const p = pos[e.from]; const q = pos[e.to];
        if (!p || !q) return '';
        const to = nodes.find((n) => n.device_id === e.to) || {};
        const from = nodes.find((n) => n.device_id === e.from) || {};
        const blocked = !!(to.blocked || from.blocked);
        const dim = !(mapView.filter === 'all' || (shown(to) && (e.from === 'server' || shown(from))));
        const d = arc217(p, q, C, 0.60);
        if (blocked) {
          return `<g class="link217 blocked${dim ? ' dim' : ''}"><path class="base" d="${d}"/></g>`;
        }
        const link = ui.LINKS_216[e.link] ? e.link : 'server';
        const color = ui.LINKS_216[link].color;
        const style = `--lc:${color};--lc2:${ui.mix217(color, '#ffffff', 0.5)};--lc3:${ui.mix217(color, '#000000', 0.12)}`;
        return `<g class="link217 l-${link}${dim ? ' dim' : ''}" style="${style}">
          <path class="glow" d="${d}"/><path class="base" d="${d}"/><path class="flow" id="lk${i}" d="${d}"/>
          <g class="dot"><circle r="5.5" class="halo"/><circle r="2.6" class="core" fill="#fff"/><animateMotion dur="3s" begin="-${((i * 0.69) % 3).toFixed(2)}s" repeatCount="indefinite"><mpath href="#lk${i}"/></animateMotion></g></g>`;
      }).join('');
      const people = {};
      nodes.forEach((n) => { if (n.user_id) people[n.user_id] = true; });
      const nodeHtml = nodes.map((n) => {
        const p = pos[n.device_id];
        const name = n.display_name || n.username || n.device_name || 'Phone';
        const cls = ['map216-node', shown(n) ? '' : 'dim', mapView.pick && mapView.pick === n.user_id ? 'sel' : ''].join(' ');
        return `<div class="${cls}" style="left:${p.x}px;top:${p.y}px" data-map-pick="${ui.esc(n.user_id || '')}" title="${ui.esc(n.device_name || '')}">
          ${ui.avatar216({ userId: n.user_id, name, hasAvatar: n.has_avatar, admin: !!n.admin_role, blocked: n.blocked, size: 48 })}
          <span class="tag">${ui.esc(name)}</span></div>`;
      }).join('');
      const sideList = nodes.filter((n) => !mapView.pick || n.user_id === mapView.pick);
      const legend = Object.keys(ui.LINKS_216).filter((k) => edges.some((e) => e.link === k))
        .map((k) => `<span><i style="background:${ui.LINKS_216[k].color}"></i>${ui.esc(ui.LINKS_216[k].label)}</span>`).join('')
        + (nodes.some((n) => n.blocked) ? '<span><i class="dash217"></i>Blocked \u00b7 relays only</span>' : '');
      el.innerHTML = `
        <div class="section">
          <div class="bar216">${ui.chips216(chips, mapView.filter, 'data-map-filter')}</div>
          <div class="map216">
            <div class="map217-wrap">
            <div class="map216-stage" data-map-stage>
              <div class="map216-world" data-map-world style="transform:translate(${mapView.x}px,${mapView.y}px) scale(${mapView.k})">
                <svg width="1000" height="700" aria-hidden="true">${lines}</svg>
                <div class="map216-node" style="left:${pos.server.x}px;top:${pos.server.y}px">
                  <span class="map216-server">${ui.icon('i-server')}</span><span class="tag" style="background:none">Server</span></div>
                ${nodeHtml}
              </div>
              <div class="map216-zoom"><button data-map-zoom="in" aria-label="Zoom in">+</button><button data-map-zoom="out" aria-label="Zoom out">\u2212</button><button data-map-zoom="fit" aria-label="Fit everyone">${ui.icon('i-fit')}</button></div>
              ${nodes.length ? '' : '<div class="empty216" style="position:absolute;left:50%;top:50%;transform:translate(-50%,-50%)">No phones yet.</div>'}
            </div>
            <div class="map216-legend">${legend}</div>
            </div>
            ${ui.panel({
              body: `<div class="map216-side"><h2>Phones (${sideList.length})</h2>
                ${sideList.map((n) => {
                  const k = linkOf(n);
                  const pill = k === 'blocked' ? ui.pill216('Blocked', 'red')
                    : (ui.LINKS_216[k] ? ui.pill216(ui.LINKS_216[k].label, ui.LINKS_216[k].tone) : ui.pill216('Offline', 'grey'));
                  return `<div class="map216-row">${ui.person216({ userId: n.user_id, name: n.display_name || n.username || n.device_name, sub: n.device_name, hasAvatar: n.has_avatar, admin: !!n.admin_role, blocked: n.blocked, size: 30 })}${pill}</div>`;
                }).join('')}
                <div class="type-caption" style="margin-top:14px">${mapView.pick ? '<a href="#" data-map-pick="">Show everyone</a>' : 'Click a person to see their phones.'}</div></div>`,
            })}
          </div>
        </div>`;

      const world = el.querySelector('[data-map-world]');
      const stage = el.querySelector('[data-map-stage]');
      const apply = () => { world.style.transform = `translate(${mapView.x}px,${mapView.y}px) scale(${mapView.k})`; };
      const zoomAt = (factor, cx, cy) => {
        const k = Math.min(3, Math.max(0.4, mapView.k * factor));
        mapView.x = cx - (cx - mapView.x) * (k / mapView.k);
        mapView.y = cy - (cy - mapView.y) * (k / mapView.k);
        mapView.k = k;
        apply();
      };
      // Bundle 217: one finger drags, two fingers pinch (pointer events).
      const pts = new Map();
      let drag = null; let pinch = null;
      const local = (ev) => { const r = stage.getBoundingClientRect(); return { x: ev.clientX - r.left, y: ev.clientY - r.top }; };
      const startDrag = () => {
        const first = pts.values().next().value;
        drag = first ? { x: first.x - mapView.x, y: first.y - mapView.y } : null;
      };
      const startPinch = () => {
        const [a, b] = Array.from(pts.values());
        pinch = { d0: Math.max(1, Math.hypot(b.x - a.x, b.y - a.y)), m0: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }, k0: mapView.k, x0: mapView.x, y0: mapView.y };
        drag = null;
      };
      stage.addEventListener('pointerdown', (ev) => {
        if (ev.target.closest('[data-map-pick],[data-map-zoom]')) return;
        pts.set(ev.pointerId, local(ev));
        try { stage.setPointerCapture(ev.pointerId); } catch (_) { /* old browsers */ }
        stage.classList.add('drag');
        if (pts.size === 2) startPinch(); else if (pts.size === 1) startDrag();
      });
      stage.addEventListener('pointermove', (ev) => {
        if (!pts.has(ev.pointerId)) return;
        pts.set(ev.pointerId, local(ev));
        if (pinch && pts.size >= 2) {
          const [a, b] = Array.from(pts.values());
          const d = Math.hypot(b.x - a.x, b.y - a.y); const m = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
          const k = Math.min(3, Math.max(0.4, pinch.k0 * d / pinch.d0));
          mapView.x = m.x - (pinch.m0.x - pinch.x0) * k / pinch.k0;
          mapView.y = m.y - (pinch.m0.y - pinch.y0) * k / pinch.k0;
          mapView.k = k; mapView.fitted = true; apply();
        } else if (drag) {
          const p = pts.get(ev.pointerId);
          mapView.x = p.x - drag.x; mapView.y = p.y - drag.y; mapView.fitted = true; apply();
        }
      });
      const lift = (ev) => {
        if (!pts.delete(ev.pointerId)) return;
        pinch = null;
        if (pts.size === 1) startDrag(); else if (pts.size === 0) { drag = null; stage.classList.remove('drag'); }
        else startPinch();
      };
      stage.addEventListener('pointerup', lift);
      stage.addEventListener('pointercancel', lift);
      stage.addEventListener('wheel', (ev) => {
        ev.preventDefault();
        const r = stage.getBoundingClientRect();
        mapView.fitted = true;
        zoomAt(ev.deltaY < 0 ? 1.12 : 1 / 1.12, ev.clientX - r.left, ev.clientY - r.top);
      }, { passive: false });
      const fit = () => {
        const box = fit217(nodes.filter(shown).map((n) => pos[n.device_id]).filter(Boolean).concat([pos.server]), stage.clientWidth, stage.clientHeight);
        if (!box) return;
        mapView.x = box.x; mapView.y = box.y; mapView.k = box.k; apply();
      };
      el.querySelectorAll('[data-map-zoom]').forEach((b) => b.addEventListener('click', () => {
        const r = stage.getBoundingClientRect();
        const z = b.getAttribute('data-map-zoom');
        if (z === 'fit') { mapView.fitted = false; fit(); return; }
        mapView.fitted = true;
        zoomAt(z === 'in' ? 1.25 : 0.8, r.width / 2, r.height / 2);
      }));
      // Motion stops while the tab is hidden (reduce-motion is handled in CSS).
      const svg = el.querySelector('.map216-world > svg');
      const motion = () => {
        const hidden = !!document.hidden;
        stage.classList.toggle('paused', hidden);
        if (svg && svg.pauseAnimations) { try { if (hidden) svg.pauseAnimations(); else svg.unpauseAnimations(); } catch (_) { /* jsdom */ } }
      };
      motion();
      if (mapView.offMotion) document.removeEventListener('visibilitychange', mapView.offMotion);
      mapView.offMotion = motion;
      document.addEventListener('visibilitychange', motion);
      if (mapView.offResize) window.removeEventListener('resize', mapView.offResize);
      let resizeTimer = 0;
      mapView.offResize = () => {
        clearTimeout(resizeTimer);
        resizeTimer = setTimeout(() => { if (!mapView.fitted && stage.isConnected) fit(); }, 150);
      };
      window.addEventListener('resize', mapView.offResize);
      el.querySelectorAll('[data-map-filter]').forEach((b) => b.addEventListener('click', () => { mapView.filter = b.getAttribute('data-map-filter'); mapView.fitted = false; this.mount(el, ctx, true); }));
      el.querySelectorAll('[data-map-pick]').forEach((b) => b.addEventListener('click', (ev) => {
        ev.preventDefault();
        const id = b.getAttribute('data-map-pick');
        mapView.pick = id && mapView.pick !== id ? id : '';
        this.mount(el, ctx, true);
      }));
      if (!quiet || !mapView.fitted) fit();
    },
  };

  // ---------------------------------------------------------------- diagnostics
  const diagnostics = {
    id: 'diagnostics',
    title: 'Diagnostics',
    subtitle: 'Server health right now',
    icon: 'i-pulse',
    group: 'System',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(6);
      const [health, runtime, integrity, production] = await Promise.all([
        settle(ctx.api('/api/v1/admin/health')),
        settle(ctx.api('/api/v1/admin/runtime-diagnostics')),
        settle(ctx.api('/api/v1/admin/database/integrity')),
        settle(ctx.api('/api/v1/admin/production-config')),
      ]);

      const components = health.ok && Array.isArray(health.data.components) ? health.data.components : [];
      const runtimeData = runtime.ok ? runtime.data : null;

      const runtimeStats = runtimeData
        ? Object.keys(runtimeData).filter((k) => typeof runtimeData[k] === 'number' || typeof runtimeData[k] === 'string').slice(0, 12)
          .map((k) => ({ label: fmt.titleCase(k), value: String(runtimeData[k]) }))
        : [];

      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({
            title: 'Diagnostics', subtitle: 'Server-reported health, runtime counters, database integrity and configuration posture',
            icon: 'i-pulse', actions: health.ok ? ui.statusBadge(health.data.overall, { label: 'Overall: ' + ui.statusInfo(health.data.overall).label }) : '',
          })}
          <div class="grid grid-metrics">
            ${runtimeData && runtimeData.uptime_seconds !== undefined ? ui.metricCard({ label: 'Uptime', value: fmt.duration(runtimeData.uptime_seconds), icon: 'i-server', context: 'Since last server start' }) : ''}
            ${runtimeData && runtimeData.requests !== undefined ? ui.metricCard({ label: 'Requests served', value: fmt.number(runtimeData.requests), icon: 'i-activity', context: 'Runtime sampler total' }) : ''}
            ${runtimeData && runtimeData.client_errors !== undefined ? ui.metricCard({ label: 'Client errors', value: fmt.number(runtimeData.client_errors), icon: 'i-alert', accent: num(runtimeData.client_errors) ? 'warning' : 'success', context: '4xx responses observed' }) : ''}
            ${runtimeData && runtimeData.bytes_out !== undefined ? ui.metricCard({ label: 'Bytes served', value: fmt.bytes(runtimeData.bytes_out), icon: 'i-transfer', context: 'Response bytes' }) : ''}
          </div>
          <div class="split-3-2">
            ${ui.panel({
              title: 'Subsystem health', subtitle: 'Every component the server reports',
              body: health.ok
                ? (components.length ? components.map(ui.healthRow).join('') : ui.emptyState({ icon: 'i-pulse', title: 'No readings', message: 'The server returned no components.' }))
                : ui.errorState(health.error, 'diagnostics'),
            })}
            ${ui.panel({
              title: 'Runtime counters', subtitle: 'Values the process exposes about itself',
              body: runtime.ok
                ? (runtimeStats.length ? ui.statRow(runtimeStats) : ui.emptyState({ icon: 'i-activity', title: 'Runtime sampler unavailable', message: 'This build does not expose a runtime sampler.' }))
                : ui.errorState(runtime.error, 'diagnostics'),
            })}
          </div>
          <div class="split-1-1">
            ${ui.panel({
              title: 'Database integrity', subtitle: 'Integrity check reported by the storage layer',
              body: integrity.ok
                ? `<pre class="mono" style="white-space:pre-wrap;margin:0;color:var(--text-secondary)">${ui.esc(JSON.stringify(integrity.data, null, 2))}</pre>`
                : ui.errorState(integrity.error, 'diagnostics'),
            })}
            ${ui.panel({
              title: 'Production configuration', subtitle: 'Posture checks the server performs on itself',
              body: production.ok
                ? `<pre class="mono" style="white-space:pre-wrap;margin:0;color:var(--text-secondary)">${ui.esc(JSON.stringify(production.data, null, 2))}</pre>`
                : ui.errorState(production.error, 'diagnostics'),
            })}
          </div>
        </div>`;
    },
  };

  // ------------------------------------------------------------------- settings
  const settings = {
    id: 'settings',
    title: 'Admin settings',
    subtitle: 'Refresh, alerts, backup and server limits',
    icon: 'i-settings',
    group: 'System',
    roles: ['owner'],
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const result = await settle(ctx.api('/api/v1/admin/settings'));
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Admin settings', body: ui.errorState(result.error, 'settings') }); return; }
      const data = result.data || {};

      el.innerHTML = `
        <div class="section">
          ${ui.panel({
            title: 'Settings', subtitle: 'Saved on the server and in the audit log',
            body: `<form class="stack gap-lg" data-settings-form style="max-width:520px">
              <div class="field">
                <label for="setting-refresh">Refresh pages every (seconds)</label>
                <input class="input" id="setting-refresh" name="dashboard_refresh_seconds" type="number" min="5" max="120" value="${ui.esc(data.dashboard_refresh_seconds)}">
                <span class="type-caption">5 to 120 seconds.</span>
              </div>
              <div class="field">
                <label for="setting-threshold">Wrong passwords before a serious warning</label>
                <input class="input" id="setting-threshold" name="security_alert_threshold" type="number" min="0" value="${ui.esc(data.security_alert_threshold)}">
                <span class="type-caption">Used by the Security page.</span>
              </div>
              <div class="row gap-md">
                <button class="btn btn-primary" type="submit">Save settings</button>
                <span class="type-caption" data-settings-note></span>
              </div>
            </form>`,
          })}
          <div class="split-1-1">
            ${ui.panel({
              title: 'Server limits', subtitle: 'Set on the server; shown here to read only',
              body: ui.statRow([
                { label: 'Biggest file', value: data.max_file_bytes ? ui.esc(fmt.bytes(data.max_file_bytes)) : '—' },
                { label: 'Space per phone', value: data.device_quota_bytes ? ui.esc(fmt.bytes(data.device_quota_bytes)) : '—' },
                { label: 'Secure link (TLS)', value: data.tls_enabled ? 'On' : 'Off' },
                { label: 'Server config', value: data.read_only_server_config ? 'Read only' : 'Can change' },
              ]),
            })}
            ${ui.panel({
              title: 'Database backup', subtitle: 'A full copy of the database, saved on the server',
              body: `<div class="stack gap-md">
                <p class="type-caption">${data.last_backup_at ? `Last: ${ui.esc(fmt.dateTime(data.last_backup_at))}${data.last_backup_bytes ? ` · ${ui.esc(fmt.bytes(data.last_backup_bytes))}` : ''}` : 'Last: never'}</p>
                <div class="row gap-md">
                  <button class="btn" data-action="backup" ${data.backup_available === false ? 'disabled' : ''}>${ui.icon('i-backup')}Create backup</button>
                  <span class="type-caption" data-backup-note></span>
                </div>
              </div>`,
            })}
          </div>
        </div>`;
    },
  };

  // ----------------------------------------------------------------- conversations
  const conversations = {
    id: 'conversations',
    title: 'Conversations',
    subtitle: 'Read chats (Owner only, saved in the audit log)',
    icon: 'i-message',
    group: 'Safety',
    // Owner only. Reading what users said to each other is the most sensitive
    // action in this console, so the server refuses every other role and writes
    // an audit entry each time a conversation is opened.
    roles: ['owner'],
    async mount(el, ctx) {
      const scope = {
        mode: 'list', kind: '', a: '', b: '', groupId: '', groupName: '', memberCount: 0,
        participants: [], messages: [], cursor: '', hasMore: false, rows: [], error: '', loading: false,
        objectUrls: [],
      };

      const root = document.createElement('div');
      el.innerHTML = '';
      el.appendChild(root);

      const personName = (p) => p.username || p.name || p.id || '—';
      const threadTitle = (row) => {
        if (row.kind === 'group') return row.group_name || row.group_id;
        const people = (row.participants || []).map(personName);
        return people.join('  ↔  ') || row.id;
      };

      // The thread renders like the phone app: bubbles against neutral
      // surfaces, 82% maximum width, a rounded corner that tightens on the
      // sender side, a 12-hour clock and the app's ✓ / ✓✓ delivery ticks. The
      // second participant of a direct conversation sits on the right; every
      // group sender stays on the left with the author name above the bubble.
      // Bodies and file names are escaped like every other server value in
      // this console, and line breaks survive because CSS preserves them.
      function time12(iso) {
        const date = new Date(iso);
        if (Number.isNaN(date.getTime())) return '';
        let hours = date.getHours();
        const suffix = hours >= 12 ? 'PM' : 'AM';
        hours = hours % 12 || 12;
        return `${hours}:${String(date.getMinutes()).padStart(2, '0')} ${suffix}`;
      }

      function tickHtml(status, showTicks) {
        if (!showTicks) return '';
        const value = String(status || 'sent').toLowerCase();
        const cls = value === 'read' ? ' read' : '';
        const glyph = value === 'sent' ? '✓' : '✓✓';
        return `<span class="chat-ticks${cls}" title="${ui.esc(value)}">${glyph}</span>`;
      }

      function sideFor(senderId) {
        if (scope.kind !== 'group' && scope.participants && scope.participants.length > 1) {
          return senderId === scope.participants[1].id ? 'out' : 'in';
        }
        return 'in';
      }

      function attachmentHtml(attachment) {
        const name = attachment.original_name || attachment.id || 'file';
        const meta = `${attachment.content_type || 'file'} · ${fmt.bytes ? fmt.bytes(attachment.size || 0) : `${attachment.size || 0} B`}`;
        if (attachment.is_image) {
          const thumb = attachment.thumbnail_url || attachment.download_url;
          return `<div class="chat-media is-loading" data-media-kind="image">
            <button class="chat-img-btn" type="button" data-chat-zoom="${ui.esc(attachment.download_url)}" aria-label="View image ${ui.esc(name)}">
              <img class="chat-img" alt="${ui.esc(name)}" data-blob-src="${ui.esc(thumb)}">
            </button>
            <span class="chat-att-name">${ui.esc(name)} · ${ui.esc(meta)}</span>
          </div>`;
        }
        if (attachment.is_audio) {
          return `<div class="chat-media is-loading" data-media-kind="audio">
            <audio controls preload="none" data-blob-src="${ui.esc(attachment.download_url)}"></audio>
            <span class="chat-att-name">${ui.esc(name)} · ${ui.esc(meta)}</span>
          </div>`;
        }
        return `<div class="chat-media" data-media-kind="file">
          <button class="chat-file" type="button" data-chat-file="${ui.esc(attachment.download_url)}" data-chat-name="${ui.esc(name)}">
            ${ui.icon('i-file')}
            <span class="chat-file-meta"><strong>${ui.esc(name)}</strong><span>${ui.esc(meta)}</span></span>
            ${ui.icon('i-download')}
          </button>
        </div>`;
      }

      // Honesty rule: if bytes cannot be fetched (file pruned from disk, or
      // media that only ever travelled device-to-device), the bubble says so
      // instead of hiding the attachment.
      function mediaUnavailableCard() {
        return `<div class="chat-media chat-media-missing">
          ${ui.icon('i-flag')}
          <span><strong>Not stored on this server</strong><br>
          <span class="type-caption">The file is not in server storage — it may have travelled device-to-device only, or been removed.</span></span>
        </div>`;
      }

      function bubbleThread(messages) {
        return `<div class="chat-thread">${messages.map((m) => {
          const side = sideFor(m.sender_id);
          const author = m.sender_name || m.sender_id || '—';
          const attachments = Array.isArray(m.attachments) ? m.attachments : [];
          return `<div class="chat-row ${side}">
            <div class="chat-bubble ${side}">
              ${scope.kind === 'group' ? `<div class="chat-author">${ui.esc(author)}${m.sender_username ? ` <span class="chat-user">${ui.esc(m.sender_username)}</span>` : ''}</div>` : ''}
              ${String(m.body || '') ? `<div class="chat-body">${ui.esc(m.body)}</div>` : ''}
              ${attachments.map(attachmentHtml).join('')}
              <div class="chat-meta">
                <span>${ui.esc(time12(m.created_at))}</span>
                ${tickHtml(m.status, side === 'out')}
              </div>
            </div>
          </div>`;
        }).join('')}</div>`;
      }

      function revokeMedia() {
        (scope.objectUrls || []).forEach((url) => URL.revokeObjectURL(url));
        scope.objectUrls = [];
      }

      // Hydration downloads attachment bytes with the Authorization header
      // (the console has no cookies, so plain element URLs would be refused)
      // and swaps every [data-blob-src] element to a same-tab object URL.
      function hydrateMedia() {
        root.querySelectorAll('[data-blob-src]').forEach((node) => {
          if (node.dataset.blobDone) return;
          node.dataset.blobDone = '1';
          const url = node.getAttribute('data-blob-src');
          const wrap = node.closest('.chat-media');
          ctx.blob(url).then((objectUrl) => {
            scope.objectUrls.push(objectUrl);
            if (node.tagName === 'AUDIO') {
              node.preload = 'metadata';
              node.src = objectUrl;
            } else {
              node.src = objectUrl;
            }
            if (wrap) wrap.classList.remove('is-loading');
          }).catch(() => {
            if (wrap) wrap.outerHTML = mediaUnavailableCard();
          });
        });
      }

      function openLightbox(url) {
        closeLightbox();
        const overlay = document.createElement('div');
        overlay.className = 'chat-lightbox';
        overlay.setAttribute('data-chat-lightbox', '');
        overlay.innerHTML = `<div class="skeleton w-60" style="height:18px"></div>`;
        overlay.addEventListener('click', closeLightbox);
        root.appendChild(overlay);
        ctx.blob(url).then((objectUrl) => {
          scope.objectUrls.push(objectUrl);
          if (!overlay.isConnected) { URL.revokeObjectURL(objectUrl); return; }
          overlay.innerHTML = `<img src="${objectUrl}" alt="Attachment full size">`;
        }).catch(() => {
          if (overlay.isConnected) overlay.innerHTML = `<div class="chat-media-missing">${ui.icon('i-flag')}<span><strong>Not stored on this server</strong></span></div>`;
        });
      }

      function closeLightbox() {
        const existing = root.querySelector('[data-chat-lightbox]');
        if (existing) existing.remove();
      }

      function downloadAttachment(button) {
        const url = button.getAttribute('data-chat-file');
        const name = button.getAttribute('data-chat-name') || 'download';
        button.disabled = true;
        ctx.blob(url).then((objectUrl) => {
          const anchor = document.createElement('a');
          anchor.href = objectUrl;
          anchor.download = name;
          document.body.appendChild(anchor);
          anchor.click();
          anchor.remove();
          setTimeout(() => URL.revokeObjectURL(objectUrl), 30000);
        }).catch((err) => {
          ctx.toast ? ctx.toast(err.message || 'Download failed', 'error') : alert(err.message);
        }).finally(() => { button.disabled = false; });
      }

      function auditNotice() {
        return ui.note('Reading is recorded',
          'The owner role can read message bodies on this server. Every conversation opened here is written to the audit log with your administrator name, the participants and the time.',
          'i-audit');
      }

      function listPanel() {
        if (!scope.rows.length) {
          return ui.panel({
            title: 'Conversations', subtitle: 'Nothing has been exchanged yet',
            body: ui.emptyState({
              icon: 'i-message',
              title: 'No messages on this server yet',
              message: 'Conversations appear here once devices start exchanging messages.',
            }),
          });
        }
        const total = scope.rows.reduce((sum, row) => sum + (row.message_count || 0), 0);
        return ui.panel({
          title: 'Conversations',
          subtitle: 'Metadata only — opening one returns its messages and is recorded',
          actions: `${ui.badge(`${scope.rows.length} thread(s)`, 'neutral')}${ui.badge(`${total} message(s)`, 'neutral')}`,
          pad0: true,
          body: `<div style="padding:16px">${ui.table({
            columns: [
              { label: 'Type' }, { label: 'People' }, { label: 'Messages', numeric: true },
              { label: 'Last activity' }, { label: '' },
            ],
            rows: scope.rows.map((row) => {
              const people = row.participants || [];
              const attrs = row.kind === 'group'
                ? `data-conversation-open data-kind="group" data-group="${ui.esc(row.group_id)}"`
                : `data-conversation-open data-kind="direct" data-a="${ui.esc(people[0] ? people[0].id : '')}" data-b="${ui.esc(people[1] ? people[1].id : '')}"`;
              return [
                row.kind === 'group' ? ui.badge('Group', 'neutral') : ui.badge('Direct', 'neutral'),
                ui.esc(threadTitle(row)),
                ui.esc(fmt.number(row.message_count || 0)),
                `<span class="subtle">${ui.esc(fmt.dateTime(row.last_message_at))}</span>`,
                `<button class="btn btn-ghost btn-sm" ${attrs}>${ui.icon('i-chevron-right')}Open</button>`,
              ];
            }),
            emptyTitle: 'No conversations',
            emptyMessage: '',
          })}</div>`,
        });
      }

      function threadPanel() {
        const ordered = scope.messages.slice().reverse();
        const title = scope.kind === 'group'
          ? (scope.groupName || scope.groupId)
          : (scope.participants || []).map(personName).join('  ↔  ');
        const subtitle = scope.kind === 'group'
          ? `Group conversation · ${fmt.number(scope.memberCount || 0)} member(s)`
          : 'Direct conversation';
        const body = ordered.length
          ? (scope.hasMore
              ? `<div style="margin-bottom:12px"><button class="btn btn-ghost btn-sm" data-conversation-older>${ui.icon('i-chevron-down')}Load older messages</button></div>`
              : '')
            + bubbleThread(ordered)
            + `<div class="type-caption" style="margin-top:12px">Showing ${ui.esc(fmt.number(ordered.length))} message(s), oldest at the top like the app. This read was recorded in the audit log.</div>`
          : ui.emptyState({
            icon: 'i-message',
            title: 'No messages in this conversation',
            message: 'The pairing exists but nothing has been exchanged yet.',
          });
        return ui.panel({
          title, subtitle,
          actions: ui.badge('Access recorded', 'success'),
          body,
        });
      }

      function draw() {
        revokeMedia();
        root.innerHTML = `<div class="section">
          ${ui.sectionHead({
            title: 'Conversations',
            subtitle: 'Review the messages exchanged on this server',
            icon: 'i-message',
            actions: scope.mode === 'thread'
              ? `<button class="btn btn-ghost btn-sm" data-conversation-back>${ui.icon('i-chevron-left')}All conversations</button>`
              : ui.badge('Owner only', 'warning'),
          })}
          ${auditNotice()}
          ${scope.loading
            ? ui.skeletonPanel(4)
            : (scope.error ? ui.panel({ body: ui.errorState(scope.error, 'conversations') })
              : (scope.mode === 'thread' ? threadPanel() : listPanel()))}
        </div>`;
        hydrateMedia();
      }

      async function loadList() {
        scope.loading = true;
        scope.error = '';
        draw();
        const result = await settle(ctx.api('/api/v1/admin/conversations?limit=100'));
        scope.loading = false;
        if (!result.ok) scope.error = result.error;
        else scope.rows = Array.isArray(result.data.conversations) ? result.data.conversations : [];
        draw();
      }

      function threadPath(cursor) {
        const base = scope.kind === 'group'
          ? `/api/v1/admin/group-conversation?group_id=${encodeURIComponent(scope.groupId)}`
          : `/api/v1/admin/conversation?user_a=${encodeURIComponent(scope.a)}&user_b=${encodeURIComponent(scope.b)}`;
        return `${base}&limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`;
      }

      async function openThread(attrs) {
        scope.mode = 'thread';
        scope.kind = attrs.kind;
        scope.a = attrs.a || '';
        scope.b = attrs.b || '';
        scope.groupId = attrs.group || '';
        scope.groupName = '';
        scope.memberCount = 0;
        scope.participants = [];
        scope.messages = [];
        scope.cursor = '';
        scope.hasMore = false;
        scope.error = '';
        scope.loading = true;
        draw();
        const result = await settle(ctx.api(threadPath('')));
        scope.loading = false;
        if (!result.ok) {
          // A refusal or a throttle is reported exactly as the server sent it.
          scope.error = result.error;
          draw();
          return;
        }
        scope.messages = Array.isArray(result.data.messages) ? result.data.messages : [];
        scope.cursor = result.data.next_cursor || '';
        scope.hasMore = !!result.data.has_more;
        scope.participants = result.data.participants || [];
        scope.groupName = result.data.group_name || '';
        scope.memberCount = result.data.member_count || 0;
        draw();
      }

      async function loadOlder() {
        if (!scope.cursor) return;
        scope.loading = true;
        draw();
        const result = await settle(ctx.api(threadPath(scope.cursor)));
        scope.loading = false;
        if (!result.ok) {
          scope.error = result.error;
          draw();
          return;
        }
        const older = Array.isArray(result.data.messages) ? result.data.messages : [];
        scope.messages = scope.messages.concat(older);
        scope.cursor = result.data.next_cursor || '';
        scope.hasMore = !!result.data.has_more;
        draw();
      }

      root.addEventListener('click', (event) => {
        const open = event.target.closest('[data-conversation-open]');
        if (open) {
          openThread({
            kind: open.getAttribute('data-kind') || 'direct',
            a: open.getAttribute('data-a') || '',
            b: open.getAttribute('data-b') || '',
            group: open.getAttribute('data-group') || '',
          });
          return;
        }
        if (event.target.closest('[data-conversation-back]')) {
          scope.mode = 'list';
          scope.error = '';
          draw();
          return;
        }
        if (event.target.closest('[data-conversation-older]')) {
          loadOlder();
          return;
        }
        const zoom = event.target.closest('[data-chat-zoom]');
        if (zoom) {
          openLightbox(zoom.getAttribute('data-chat-zoom'));
          return;
        }
        const file = event.target.closest('[data-chat-file]');
        if (file) {
          downloadAttachment(file);
        }
      });

      await loadList();
    },
  };

  // -------------------------------------------------------------------- privacy
  const privacy = {
    id: 'privacy',
    title: 'Privacy & access',
    subtitle: 'What admins can and cannot see',
    icon: 'i-privacy',
    group: 'System',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(4);
      const result = await settle(ctx.api('/api/v1/admin/privacy'));
      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({ title: 'Privacy & access', subtitle: 'What the console may read, and what remains private', icon: 'i-privacy' })}
          ${ui.note('Message review is switched on',
            'Message bodies are stored as users write them, and the owner role can read them in the Conversations module. Every conversation opened there is written to the audit log. Passwords, private keys, call audio, decrypted media and file contents remain unexposed.',
            'i-lock')}
          ${ui.panel({
            title: 'What this console can read',
            subtitle: 'Reported by the server, shown as reported',
            body: result.ok
              ? ui.table({
                columns: [{ label: 'Content' }, { label: 'Reachable here' }, { label: 'Detail' }],
                rows: [
                  ['Message bodies',
                    result.data.private_message_bodies_exposed ? ui.badge('Owner only', 'warning') : ui.badge('No', 'success'),
                    result.data.message_body_access_audited === false ? 'Reading is not recorded' : 'Every read is recorded in the audit log'],
                  ['Passwords', result.data.passwords_exposed ? ui.badge('Exposed', 'danger') : ui.badge('No', 'success'), 'Only hashes and salts are stored'],
                  ['Private keys', result.data.private_keys_exposed ? ui.badge('Exposed', 'danger') : ui.badge('No', 'success'), 'Device identity keys stay on the device'],
                  ['Call audio', result.data.private_call_audio_exposed ? ui.badge('Exposed', 'danger') : ui.badge('No', 'success'), 'The server relays signalling, not call media'],
                  ['File contents', result.data.private_file_contents_exposed ? ui.badge('Exposed', 'danger') : ui.badge('No', 'success'), 'Transfers carry their own attachment crypto'],
                ],
                emptyTitle: 'Nothing reported',
                emptyMessage: '',
              })
              : ui.errorState(result.error, 'privacy'),
          })}}
          ${ui.panel({
            title: 'Raw server response', subtitle: 'The full policy exactly as this server returned it',
            body: result.ok
              ? `<pre class="mono" style="white-space:pre-wrap;margin:0;color:var(--text-secondary)">${ui.esc(JSON.stringify(result.data, null, 2))}</pre>`
              : ui.errorState(result.error, 'privacy'),
          })}
          ${ui.panel({
            title: 'Your access', subtitle: 'The role attached to this admin session',
            body: ui.statRow([
              { label: 'Signed in as', value: ctx.session.username || '—' },
              { label: 'Role', value: fmt.titleCase(ctx.session.role) },
              { label: 'Session expires', value: fmt.dateTime(ctx.session.expires_at) },
            ]) + `<div class="type-caption" style="margin-top:12px">${ui.esc(ctx.permissionSummary())}</div>`,
          })}
        </div>`;
    },
  };

  // -------------------------------------------------------------- app version
  // Bundle 213: minimum app version (warn / stop old builds).
  const appversion = {
    id: 'appversion',
    title: 'App version',
    subtitle: 'Minimum app version for phones',
    icon: 'i-devices',
    group: 'System',
    roles: ['owner'],
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(4);
      const result = await settle(ctx.api('/api/v1/admin/version-rule'));
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'App version', body: ui.errorState(result.error, 'appversion') }); return; }
      const data = result.data || {};
      const rule = data.rule;
      const builds = Array.isArray(data.builds) ? data.builds : [];
      const ruleText = rule
        ? `Below build ${ui.esc(rule.min_build)} (${ui.esc(rule.min_version)}): ${rule.mode === 'warn' ? 'warn only' : 'stop from ' + ui.esc(rule.stop_at)}${rule.very_old ? ' · very old apps stopped' : ''} · by ${ui.esc(rule.by)}`
        : 'None set';
      // Bundle 217: ui.table so a phone shows each build as a card.
      const rows = builds.map((b) => [ui.esc(b.build), ui.esc(b.devices), ui.esc(b.last_seen)]);
      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({ title: 'App version', subtitle: 'Old phones get a warning, then can only read chats until they update', icon: 'i-devices' })}
          ${ui.panel({ title: 'Current rule', subtitle: 'Signed by the server; phones pass it on with no internet', body: `<p data-version-current>${ruleText}</p>` })}
          ${ui.panel({
            title: 'Minimum version', subtitle: 'Phones below it are warned or stopped',
            body: `<form class="stack gap-lg" data-version-form style="max-width:520px">
              <div class="field"><label for="ver-name">Version name (e.g. 3.0.54)</label>
                <input class="input" id="ver-name" name="min_version" value="${ui.esc(rule ? rule.min_version : '')}"></div>
              <div class="field"><label for="ver-build">Build number (e.g. 125)</label>
                <input class="input" id="ver-build" name="min_build" type="number" min="1" value="${ui.esc(rule ? rule.min_build : '')}"></div>
              <div class="field"><label>What happens</label>
                <label><input type="radio" name="when" value="warn"> Warn only</label>
                <label><input type="radio" name="when" value="now"> Stop now</label>
                <label><input type="radio" name="when" value="days" checked> Stop after
                  <input class="input" name="stop_days" type="number" min="1" max="30" value="3" style="width:80px;display:inline-block"> days</label></div>
              <label><input type="checkbox" name="very_old"> Also stop very old apps (before 3.0.54)</label>
              <div class="row gap-md">
                <button class="btn btn-primary" type="submit">Save rule</button>
                <button class="btn" type="button" data-version-remove>Remove rule</button>
                <span class="type-caption" data-version-note></span>
              </div>
            </form>`,
          })}
          ${ui.panel({ title: 'Phones by build', subtitle: 'Last 30 days', body: ui.table({ columns: [{ label: 'Build' }, { label: 'Phones' }, { label: 'Last seen' }], rows, emptyTitle: 'No builds yet', emptyMessage: 'No phone has sent its build yet (apps from 3.0.54 do).' }) })}
        </div>`;
    },
  };

  // Bundle 216: sidebar order People / Network / Safety / System.
  // ------------------------------------------------------------ admin actions
  // Bundle 220: every admin action goes to all phones. Pending until the
  // target phone does it and sends its signed Done; no time limit.
  const ACTION_WORDS_220 = {
    block: 'Block', unblock: 'Unblock', off: 'Off (account closed)', active: 'Active again',
    sign_out: 'Sign out', remove_phone: 'Remove phone', rename_phone: 'Rename phone',
  };
  const ACTION_PERM_220 = {
    block: 'manageUsers', unblock: 'manageUsers', off: 'manageUsers', active: 'manageUsers',
    wifi_rule: 'manageNetworkPolicy', sign_out: 'manageSessions', remove_phone: 'manageDevices',
    admin_badge: 'manageSettings',
  };
  function day220(iso) {
    const d = new Date(iso);
    return isNaN(d.getTime()) ? '' : d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
  }
  function made220(iso) {
    const d = new Date(iso);
    if (isNaN(d.getTime())) return '';
    const now = new Date();
    if (d.toDateString() === now.toDateString()) {
      return 'today ' + d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false });
    }
    return day220(iso);
  }
  function title220(a) {
    const d = String(a.detail || '');
    if (a.action === 'wifi_rule') {
      const v = d.replace(/^wifi=/, '');
      const rule = WIFI_RULES_216.filter((r) => r.value === v)[0];
      return 'Wi-Fi rule: ' + (rule ? rule.label : (v || 'Default'));
    }
    if (a.action === 'admin_badge') {
      if (d === 'admin removed') return 'Remove admin';
      const role = d.replace(/^role=/, '');
      return 'Make admin' + (role ? ' (' + (ui.LEVELS_216[role] || fmt.titleCase(role)) + ')' : '');
    }
    if (a.action === 'version_rule') {
      const m = /^(\S+) below (\S+)/.exec(d);
      if (!m) return 'Old app rule';
      return 'Old app ' + (m[1] === 'warn' ? 'Warn' : 'Stop') + ' (< ' + m[2] + ')';
    }
    return ACTION_WORDS_220[a.action] || fmt.titleCase(String(a.action || '').replace(/_/g, ' '));
  }
  function via220(v) {
    return v === 'server' ? 'via the server' : (v === 'carried' ? 'carried by phones' : '');
  }
  function status220(a) {
    const phones = Array.isArray(a.phones) ? a.phones : [];
    const need = num(a.need);
    const done = num(a.done);
    if (a.state === 'cancelled') return { pill: ui.pill216('Cancelled', 'grey'), sub: 'cancelled ' + day220(a.finished_at) };
    if (a.server_only) return { pill: ui.pill216('Done at once', 'green'), sub: 'server only' };
    if (a.action === 'version_rule') {
      const reached = ui.pill216('Reached ' + fmt.number(num(a.reached)) + ' phones', 'grey');
      return { pill: reached, sub: a.state === 'replaced' ? 'a newer rule took over' : 'no single target' };
    }
    if (a.state === 'done' || a.state === 'replaced') {
      const parts = [via220(a.via), need > 1 ? (done + ' of ' + need + ' phones') : ''].filter(Boolean);
      return { pill: ui.pill216('Done ' + day220(a.finished_at), 'green'), sub: parts.join(' \u00b7 ') };
    }
    if (need > 1 && done > 0) {
      const names = phones.map((p) => (p.name || 'Phone') + (p.result ? ' done' : ' pending'));
      return { pill: ui.pill216(done + ' of ' + need + ' phones', 'blue'), sub: names.join(' \u00b7 ') };
    }
    if (num(a.old_app) > 0 && done === 0) {
      return { pill: ui.pill216('Pending \u00b7 old app', 'red'), sub: "the app is too old to answer \u00b7 ask to update" };
    }
    return { pill: ui.pill216('Pending', 'orange'), sub: 'since ' + day220(a.made_at) + ' \u00b7 ' + (done ? done + ' reached' : 'no phone reached yet') };
  }

  const actions = {
    id: 'actions',
    title: 'Admin actions',
    subtitle: 'Every admin action goes to all phones. Pending until the target phone does it.',
    icon: 'i-flag',
    group: 'Safety',
    navCount: 'admin_actions_pending',
    // Every 30 s while open: patch in place (no shimmer).
    async refresh(el, ctx) {
      await actions.mount(el, ctx, true);
      return true;
    },
    async mount(el, ctx, quiet) {
      if (!quiet) el.innerHTML = ui.bootSkeleton219('actions');
      const result = await settle(ctx.api('/api/v1/admin/actions?state=all'));
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Admin actions', body: ui.errorState(result.error, 'actions') }); return; }
      const all = Array.isArray(result.data.actions) ? result.data.actions : [];
      const isDone = (a) => a.state === 'done' || a.state === 'replaced';
      const pending = all.filter((a) => a.state === 'pending' && a.action !== 'version_rule');
      const finished = all.filter(isDone);
      const tab = ctx.state.filters.actions220 || 'all';
      const list = tab === 'pending' ? pending : (tab === 'done' ? finished : all);
      const rows = list.map((a) => {
        const st = status220(a);
        const perm = ACTION_PERM_220[a.action];
        const canCancel = a.state === 'pending' && perm && ctx.can(perm) && (a.action !== 'admin_badge' && a.action !== 'version_rule' || ctx.role === 'owner');
        const by = (a.made_by || 'admin') + (a.offline ? ' (offline)' : ' (web)');
        return [
          `<div class="act220-t"><b>${ui.esc(title220(a))}</b></div>`,
          `<span>${ui.esc(a.label || '\u2014')}</span>`,
          `<div class="act220-s">${st.pill}${st.sub ? `<small>${ui.esc(st.sub)}</small>` : ''}</div>`,
          `<span class="subtle">${ui.esc(made220(a.made_at) + ' \u00b7 ' + by)}</span>`,
          canCancel ? `<button class="btn btn-sm" data-action220-cancel="${ui.esc(a.id)}">Cancel</button>` : '',
        ];
      });
      el.innerHTML = `
        <div class="section">
          <div class="bar216">
            ${ui.chips216([
              { value: 'all', label: 'All ' + all.length },
              { value: 'pending', label: 'Pending ' + pending.length },
              { value: 'done', label: 'Done ' + finished.length },
            ], tab, 'data-actions220-tab')}
          </div>
          ${ui.panel({
            pad0: true,
            body: rows.length
              ? `<div style="padding:0 4px">${ui.table({ columns: [{ label: 'Action' }, { label: 'For' }, { label: 'Status' }, { label: 'Made' }, { label: '' }], rows })}</div>`
              : `<div class="empty216">${tab === 'pending' ? 'Nothing is pending. Every action reached its phone.' : 'No admin actions yet.'}</div>`,
          })}
        </div>`;
      el.querySelectorAll('[data-actions220-tab]').forEach((b) => b.addEventListener('click', () => {
        ctx.state.filters.actions220 = b.getAttribute('data-actions220-tab');
        ctx.refresh();
      }));
      el.querySelectorAll('[data-action220-cancel]').forEach((b) => b.addEventListener('click', async () => {
        const id = b.getAttribute('data-action220-cancel');
        const ok = await ctx.confirm({
          title: 'Cancel this action?',
          message: 'Phones that have not done it yet will not do it. Phones that already did it keep the change.',
          confirmLabel: 'Cancel action', cancelLabel: 'Keep', destructive: true,
        });
        if (!ok) return;
        b.disabled = true;
        try {
          await ctx.api('/api/v1/admin/actions/' + encodeURIComponent(id) + '/cancel', { method: 'POST' });
          ctx.toast('Action cancelled', 'success');
        } catch (err) {
          ctx.toast(err.message || 'Could not cancel', 'error');
        }
        ctx.refresh();
      }));
    },
  };

  LL.views = { dashboard, users, devices, sessions, map, network, calls, transfers, recovery, security, audit, conversations, actions, appversion, diagnostics, settings, privacy };

  // Modules that are restricted to specific roles (used by quick actions too).
  LL.moduleRoles = { settings: ['owner'], conversations: ['owner'], appversion: ['owner'] };

})();
