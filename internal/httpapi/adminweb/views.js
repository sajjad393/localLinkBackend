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
  // TODO 290: the Users level pill has the colour of that level's crown.
  const LEVEL_TONE_290 = { owner: 'gold', operator: 'blue', security: 'red', auditor: 'grey' };

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
      // Bundle 274 (D-2): the Live network card is drawn from the link map
      // (each phone once) and kept live by the admin socket.
      const linkMap = await settle(ctx.api('/api/v1/admin/link-map'));

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
      if (!networkData && !linkMap.ok) {
        networkBody = ui.errorState(network.error || 'The network endpoint did not respond.', 'dashboard');
      } else {
        networkBody = `<div data-live274-net>${liveNetwork274(linkMap.ok ? linkMap.data : null, networkData)}</div>`;
      }
      if (el.isConnected) {
        // Bundle 274 (D-4): only the Live network card changes on a live frame.
        ctx.live((msg) => {
          const host = window.document.querySelector('#view [data-live274-net]');
          if (!host || ctx.state.view !== 'dashboard') return;
          const html = liveNetwork274(msg, networkData);
          if (host.innerHTML === html) return;
          host.innerHTML = html;
          if (LL.charts && typeof LL.charts.prune === 'function') LL.charts.prune();
          afterRender(host);
        });
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
            title: 'Live network', subtitle: 'Each phone once, by how it is linked now',
            actions: `${ctx.liveBadge ? ctx.liveBadge() : ''}<button class="btn btn-sm" data-nav="network">Open network topology</button>`,
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
        'The server keeps a message only until it is delivered (+7 days). The Owner can ask the phones for a chat in the Conversations module; every request and every read is written to the audit log. Passwords, private keys and call audio stay out of this console.',
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
      // Bundle 274 (D-1): ui.token adds the '--' itself; passing '--success'
      // looked up '----success' and every colour fell back to blue.
      const name = raw.slice(4, -1).trim().replace(/^--/, '');
      return ui.token(name, '#1e5bff');
    }
    return raw || '#1e5bff';
  }

  // Bundle 274 (D-2): Live network card from the link map. The ring counts
  // each phone ONCE by how it is linked now (on the server, else the way it
  // is reached, else offline), so the slices add up to the phones. The row
  // under it counts LINKS by kind. Same colours as the maps.
  const OFFLINE274 = '#98A2B3';
  function phoneWays274(data) {
    const nodes = data && Array.isArray(data.nodes) ? data.nodes : [];
    const edges = data && Array.isArray(data.edges) ? data.edges : [];
    const ways = { server: 0, lan: 0, wifi_direct: 0, mesh: 0, offline: 0 };
    nodes.forEach((n) => {
      const k = n.online ? 'server' : (ways[n.link] !== undefined && n.link !== 'offline' ? n.link : 'offline');
      ways[k] += 1;
    });
    const links = { server: 0, lan: 0, wifi_direct: 0, mesh: 0 };
    edges.forEach((e) => { if (links[e.link] !== undefined) links[e.link] += 1; });
    return { nodes, edges, ways, links, online: nodes.filter((n) => n.online || (n.link && n.link !== 'offline')).length };
  }

  function liveNetwork274(data, networkData) {
    const w = phoneWays274(data);
    const L = ui.LINKS_216;
    const segments = [
      { label: 'On the server', value: w.ways.server, color: L.server.color },
      { label: 'Local Wi-Fi only', value: w.ways.lan, color: L.lan.color },
      { label: 'Wi-Fi Direct only', value: w.ways.wifi_direct, color: L.wifi_direct.color },
      { label: 'Mesh only', value: w.ways.mesh, color: L.mesh.color },
      { label: 'Offline', value: w.ways.offline, color: OFFLINE274 },
    ];
    const pending = networkData ? networkData.pending_transfers : 0;
    return `<div class="row gap-lg" style="align-items:center;flex-wrap:wrap">
        <div style="width:150px;height:150px">${chartCanvas({ type: 'donut', options: { segments, size: 148, centerValue: fmt.number(w.online), centerLabel: 'online' } }, 148)}</div>
        <div class="legend grow">${segments.map((s) => `<div class="legend-item">
          <span class="legend-swatch" style="background:${s.color}"></span>
          <span>${ui.esc(s.label)}</span><span class="legend-value">${fmt.number(s.value)}</span></div>`).join('')}</div>
      </div>
      <div class="health-detail" style="margin-top:12px">
        ${ui.statRow([
          { label: 'Server links', value: fmt.number(w.links.server) },
          { label: 'Wi-Fi links', value: fmt.number(w.links.lan) },
          { label: 'Wi-Fi Direct links', value: fmt.number(w.links.wifi_direct) },
          { label: 'Mesh links', value: fmt.number(w.links.mesh) },
          { label: 'Phones', value: fmt.number(w.nodes.length) },
          { label: 'Pending transfers', value: fmt.number(pending) },
        ])}
      </div>`;
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
      admin: u.admin_role || false,
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
      // Bundle 222: plus offline accounts phones saw nearby ("Not on server yet").
      const [result, dupResult] = await Promise.all([
        settle(ctx.api('/api/v1/admin/users?limit=200&include=not_on_server')),
        settle(ctx.api('/api/v1/admin/duplicates')), // Bundle 235 (an old server = no list)
      ]);
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Users', body: ui.errorState(result.error, 'users') }); return; }

      const all = Array.isArray(result.data.users) ? result.data.users : [];
      const nosAll = Array.isArray(result.data.not_on_server) ? result.data.not_on_server : [];
      // Bundle 238: offline rows not seen for 30 days hide behind "Show hidden (N)".
      const nos = nosAll.filter((x) => !x.hidden);
      const nosHidden = nosAll.filter((x) => x.hidden);
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
      const dups = dupResult.ok && Array.isArray(dupResult.data.duplicates) ? dupResult.data.duplicates : [];
      const chip = query.dups ? 'dups' : (query.nos ? 'not_on_server' : (query.admin === 'yes' ? 'admins' : (query.status || 'all')));
      const nosMatch = (x) => {
        const hay = `${x.name || ''} ${x.username || ''}`.toLowerCase();
        return !search || hay.indexOf(search) >= 0;
      };
      const nosOn = (query.nos || chip === 'all');
      const hiddenMatch238 = nosOn ? nosHidden.filter(nosMatch) : [];
      const nosShown = nosOn ? nos.filter(nosMatch).concat(query.hidden238 ? hiddenMatch238 : []) : [];
      const columns = [
        { label: 'Person' }, { label: 'Admin' }, { label: 'Phones', numeric: true },
        { label: 'Status', status: true }, { label: 'Last seen' }, { label: '' },
      ].map((c) => (c.label === 'Admin' ? { label: 'Admin level' } : c));
      const nosRows222 = nosShown.map((x) => [
        ui.person216({ userId: '', name: ui.name276({ display_name: x.name, username: x.username }), sub: `@${x.username || ''} (may change if taken)${x.model ? ' \u00b7 ' + x.model : ''}`, size: 32 }),
        '<span class="subtle">\u2014</span>',
        '<span>1</span>',
        `${ui.pill216(x.hidden ? 'Hidden \u00b7 not seen 30+ days' : 'Not on server yet', 'grey')}${num(x.carriers) > 0 ? `<div class="type-caption nos222">Join box carried by ${fmt.number(x.carriers)} ${num(x.carriers) === 1 ? 'phone' : 'phones'} \u00b7 waiting for a phone with internet</div>` : ''}`,
        `<span>${ui.esc(`${x.seen_by || ''} \u00b7 ${LINK_WORDS_222[x.link] || x.link || ''}`)}</span><div class="type-caption">${ui.esc(ui.when216(x.last_seen))}</div>`,
        '<span class="subtle" title="No actions until the account joins">\u2014</span>',
      ]);
      const rows = (query.nos || query.dups ? [] : filtered).map((u) => [
        who216(map, u.id, ui.name276(u), '@' + (u.username || ''), { online: num(u.online_devices) > 0, size: 32 }),
        u.admin_role ? ui.pill216(ui.LEVELS_216[u.admin_role] || u.admin_role, LEVEL_TONE_290[ui.crownRole290(u.admin_role)]) : '<span class="subtle">\u2014</span>',
        `<span>${fmt.number(u.devices)}</span>`,
        status216(u.status),
        `<span>${ui.esc(num(u.online_devices) > 0 ? 'Online now' : ui.when216(u.last_seen_at))}</span>`,
        `<button class="btn btn-sm" data-user-manage="${ui.esc(u.id)}">Manage</button>`,
      ]).concat(query.dups ? [] : nosRows222);
      const dupPanel = (chip === 'all' || chip === 'dups') && dups.length ? dupPanel235(dups, map, ctx.can('manageUsers')) : '';

      el.innerHTML = `
        <div class="section">
          <div class="bar216" style="justify-content:space-between">
            <div class="bar216" style="margin:0">
              <input class="input search216" type="search" data-filter-search placeholder="Search name or @username" value="${ui.esc(query.search || '')}" aria-label="Search name or @username">
              ${ui.chips216([
                { value: 'all', label: 'All' }, { value: 'admins', label: 'Admins' }, { value: 'active', label: 'Active' },
                { value: 'suspended', label: 'Suspended' }, { value: 'disabled', label: 'Off' },
                { value: 'not_on_server', label: `Not on server ${nos.length}` },
              ].concat(dups.length || chip === 'dups' ? [{ value: 'dups', label: `Waiting for proof ${dups.length}` }] : []), chip, 'data-users-chip')}
            </div>
            <div class="row gap-sm" title="app admins">${ui.pill216(`${adminCount} admins`, 'gold')} ${ui.pill216(`${all.length} people`, 'grey')}${nos.length ? ' ' + ui.pill216(`${nos.length} not on server yet`, 'grey') : ''}</div>
          </div>
          ${dupPanel}
          ${chip === 'dups' ? (dups.length ? '' : ui.panel({ body: '<div class="empty216">No phone is waiting for proof.</div>' })) : ui.panel({
            pad0: true,
            body: `${rows.length ? `<div style="padding:0 4px">${ui.table({ columns, rows })}</div>` : ''}
              <div class="empty216">${rows.length ? '' : 'No one found? Try another name or filter.'}</div>${hiddenMatch238.length ? `
              <label class="row gap-sm hidden238" style="padding:10px 18px 14px;cursor:pointer;align-items:center">
                <input type="checkbox" data-users-hidden238 ${query.hidden238 ? 'checked' : ''}>
                <span>Show hidden (${fmt.number(hiddenMatch238.length)})</span>
                <span class="type-caption subtle">Offline accounts not seen for 30 days</span>
              </label>` : ''}`,
          })}
        </div>`;

      el.querySelectorAll('[data-users-chip]').forEach((b) => b.addEventListener('click', () => {
        const v = b.getAttribute('data-users-chip');
        ctx.state.filters.users = Object.assign({}, query, { dups: v === 'dups', nos: v === 'not_on_server', admin: v === 'admins' ? 'yes' : '', status: ['active', 'suspended', 'disabled'].indexOf(v) >= 0 ? v : '' });
        ctx.refresh();
      }));
      // Bundle 238: Show hidden (N).
      el.querySelectorAll('[data-users-hidden238]').forEach((b) => b.addEventListener('change', () => {
        ctx.state.filters.users = Object.assign({}, query, { hidden238: !!b.checked });
        ctx.refresh();
      }));
      // Bundle 235: New owner... / Dismiss.
      el.querySelectorAll('[data-dup235-owner]').forEach((b) => b.addEventListener('click', async () => {
        const d = dups.find((x) => x.id === b.getAttribute('data-dup235-owner'));
        if (!d) return;
        const ok = await newOwnerDialog235(d, map);
        if (!ok) return;
        try {
          await ctx.api('/api/v1/admin/duplicates/new-owner', { method: 'POST', body: { id: d.id, confirm: d.new_username } });
          ctx.toast(`Phone given to @${d.new_username}`, 'success');
        } catch (err) {
          ctx.toast(err.message || 'Could not free the phone', 'error');
        }
        ctx.refresh();
      }));
      el.querySelectorAll('[data-dup235-dismiss]').forEach((b) => b.addEventListener('click', async () => {
        b.disabled = true;
        try {
          await ctx.api('/api/v1/admin/duplicates/dismiss', { method: 'POST', body: { id: b.getAttribute('data-dup235-dismiss') } });
          ctx.toast('Hidden. The new account stays blocked.', 'success');
        } catch (err) {
          ctx.toast(err.message || 'Could not hide', 'error');
        }
        ctx.refresh();
      }));
      el.querySelectorAll('[data-user-manage]').forEach((b) => b.addEventListener('click', () => {
        const u = map[b.getAttribute('data-user-manage')];
        if (u) manage216(u, ctx);
      }));
    },
  };

  // Bundle 235: "Waiting for proof" - a new account tried to join on a phone
  // that already has an account. Only the server's refusals are listed, so a
  // line says when it tried (not which phone saw it).
  function dupPanel235(dups, map, canManage) {
    const columns = [{ label: 'New account' }, { label: 'Phone' }, { label: 'Old account (kept)' }, { label: 'Status', status: true }, { label: '' }];
    const rows = dups.map((d) => {
      const old = map[d.old_user_id] || {};
      const dev = String(d.device_id || '');
      const short = dev.length > 8 ? '\u2026' + dev.slice(-6) : dev;
      return [
        ui.person216({ userId: '', name: ui.name276({ display_name: d.new_name, username: d.new_username }), sub: `@${d.new_username || ''} \u00b7 not on server`, size: 32 }),
        `<span>${ui.esc(d.device_name || 'Phone')}</span><div class="type-caption">${ui.esc(short)} \u00b7 same hardware</div>`,
        who216(map, d.old_user_id, d.old_name || old.display_name || d.old_username, `@${d.old_username || old.username || ''}${old.devices ? ' \u00b7 ' + fmt.number(old.devices) + (num(old.devices) === 1 ? ' phone' : ' phones') : ''}`, { size: 32 }),
        `${ui.pill216('Blocked \u00b7 waiting', 'orange')}<div class="type-caption">Tried to join ${ui.esc(ui.when216(d.last_seen))}${num(d.tries) > 1 ? ' \u00b7 ' + fmt.number(d.tries) + ' tries' : ''}</div>`,
        canManage
          ? `<div class="row gap-sm"><button class="btn btn-sm" data-dup235-owner="${ui.esc(d.id)}">New owner\u2026</button><button class="btn btn-sm" data-dup235-dismiss="${ui.esc(d.id)}">Dismiss</button></div>`
          : '<span class="subtle">\u2014</span>',
      ];
    });
    return ui.panel({
      pad0: true,
      body: `<div style="padding:16px 18px 4px"><strong>Waiting for proof</strong>
          <div class="type-caption">A new account tried to join on a phone that already has an account. It stays blocked until the person proves the old account.</div></div>
        <div style="padding:0 4px">${ui.table({ columns, rows })}</div>
        <div class="type-caption" style="padding:8px 18px 14px">Only Owner admins (Manage users) see the buttons. Dismiss hides the line; the block stays.</div>`,
    });
  }

  // Bundle 235: the admin types the new name; no undo.
  function newOwnerDialog235(d, map) {
    return new Promise((resolve) => {
      const host = document.getElementById('modal-host');
      if (!host) { resolve(false); return; }
      const old = map[d.old_user_id] || {};
      const oldName = d.old_name || old.display_name || d.old_username || 'the old account';
      const nu = String(d.new_username || '');
      host.innerHTML = `<div class="modal-scrim" role="dialog" aria-modal="true">
        <div class="modal">
          <h2>${ui.esc(`Give ${oldName}'s phone to a new person?`)}</h2>
          <p>${ui.esc(`@${d.old_username || ''} keeps the account, the password and the chats, but loses this phone (${d.device_name || 'Phone'}). @${nu} can then join from this phone, and friends' phones stop blocking it. There is no undo.`)}</p>
          <label class="type-caption" for="dup235-confirm" style="display:block;margin:10px 0 6px;font-weight:600">${ui.esc(`Type ${nu} to confirm`)}</label>
          <input class="input" id="dup235-confirm" data-dup235-input autocomplete="off" style="width:100%">
          <div class="modal-actions">
            <button class="btn" data-modal="cancel">Cancel</button>
            <button class="btn btn-danger" data-modal="confirm" disabled>${ui.esc(`Give phone to @${nu}`)}</button>
          </div>
        </div>
      </div>`;
      const input = host.querySelector('[data-dup235-input]');
      const go = host.querySelector('[data-modal="confirm"]');
      const finish = (value) => { host.innerHTML = ''; resolve(value); };
      input.addEventListener('input', () => {
        go.disabled = input.value.trim().replace(/^@/, '').toLowerCase() !== nu.toLowerCase();
      });
      host.querySelector('[data-modal="cancel"]').addEventListener('click', () => finish(false));
      go.addEventListener('click', () => { if (!go.disabled) finish(true); });
      host.querySelector('.modal-scrim').addEventListener('click', (event) => {
        if (event.target.classList.contains('modal-scrim')) finish(false);
      });
      input.focus();
    });
  }

  // Bundle 222: link words for "Seen by".
  const LINK_WORDS_222 = { wifi_direct: 'Wi-Fi Direct', mesh: 'Mesh', lan: 'Local Wi-Fi', router: 'Local Wi-Fi', server: 'Server' };

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
    const name = ui.name276(u);
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
      return `<button type="button" class="opt216 ${on ? 'on' : ''}" ${attrs}><b>${value ? ui.crown216(12, value) + ' ' : ''}${ui.esc(label)}</b><small>${ui.esc(help)}</small></button>`;
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
          ${ui.person216({ userId: u.id, name, sub: '@' + (u.username || ''), hasAvatar: !!u.has_avatar, admin: u.admin_role || false, blocked: u.status === 'suspended', size: 48 })}
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
        d.user_id ? who216(map, d.user_id, ui.name276(d), '@' + (d.username || ''), { size: 28 }) : '<span class="subtle">\u2014</span>',
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
            who216(map, s.user_id, ui.name276(s), '@' + (s.username || ''), { size: 28 }),
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
            ui.person216({ name: s.admin_username || s.username, sub: ui.LEVELS_216[s.role] || fmt.titleCase(s.role || ''), admin: s.role || true, size: 28 }),
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
      const data = net.ok ? net.data : {};
      this.draw(el, ctx, links.data, data);
      // Bundle 274 (W-1): redraw on every live frame while this page is open.
      if (el.isConnected) {
        ctx.live((msg) => {
          if (ctx.state.view !== 'network' || !el.isConnected) return;
          this.draw(el, ctx, msg, data);
        });
      }
    },
    // Bundle 272 (Part C): tiles count real links (N-1), "Link now" shows the
    // server AND the local way (N-2), Local Wi-Fi tile + the way next to each
    // name in "Linked with" (N-3). Names never show the phone's default name (276).
    draw(el, ctx, linkData, data) {
      const nodes = Array.isArray(linkData.nodes) ? linkData.nodes : [];
      const edges = Array.isArray(linkData.edges) ? linkData.edges : [];
      const byId = {};
      nodes.forEach((n) => { byId[n.device_id] = n; });
      const nameOf = (n) => ui.name276(n);
      const WAY = { lan: 'Wi-Fi', wifi_direct: 'Wi-Fi Direct', mesh: 'Mesh' };
      const localEdges = (id) => edges.filter((e) => e.from !== 'server' && e.to !== 'server' && (e.from === id || e.to === id) && WAY[e.link]);
      const peers = (id) => localEdges(id).map((e) => {
        const other = byId[e.from === id ? e.to : e.from];
        return other ? `${nameOf(other)} (${WAY[e.link]})` : '';
      }).filter(Boolean);
      const waysOf = (id) => {
        const out = [];
        localEdges(id).forEach((e) => { if (out.indexOf(e.link) < 0) out.push(e.link); });
        return out;
      };
      const reached = nodes.filter((n) => n.online || n.link);
      const hasWay = (n, k) => n.link === k || waysOf(n.device_id).indexOf(k) >= 0;
      const count = (k) => nodes.filter((n) => hasWay(n, k)).length;
      const tile = (label, value, color) => `<div class="tile216"><small>${ui.esc(label)}</small><b style="color:${color}">${ui.esc(fmt.number(value))}</b></div>`;
      const L = ui.LINKS_216;
      el.innerHTML = `
        <div class="section">
          <div class="bar216" style="justify-content:flex-end;margin:0 0 8px">${ctx.liveBadge ? ctx.liveBadge() : ''}</div>
          <div class="tiles216">
            ${tile('Phones online', reached.length, 'var(--text-primary)')}
            ${tile('On the server', nodes.filter((n) => n.online).length, L.server.color)}
            ${tile('Local Wi-Fi', count('lan'), L.lan.color)}
            ${tile('Wi-Fi Direct', Math.max(count('wifi_direct'), num(data.wifi_direct_connected)), L.wifi_direct.color)}
            ${tile('Mesh', count('mesh'), L.mesh.color)}
          </div>
          ${ui.panel({
            title: 'Phones right now',
            body: reached.length ? ui.table({
              columns: [{ label: 'Phone' }, { label: 'Owner' }, { label: 'Link now' }, { label: 'Linked with' }],
              rows: reached.map((n) => {
                const local = waysOf(n.device_id);
                let way;
                if (n.blocked) way = ui.pill216('Blocked', 'red');
                else {
                  // "Server + Wi-Fi": the server first, then every local way.
                  const keys = n.online ? ['server'] : (n.link && L[n.link] ? [n.link] : []);
                  local.forEach((k) => { if (keys.indexOf(k) < 0 && L[k]) keys.push(k); });
                  way = keys.length ? keys.map((k) => ui.pill216(L[k].label, L[k].tone)).join(' ') : ui.pill216('Offline', 'grey');
                }
                const with216 = peers(n.device_id);
                return [
                  `<b>${ui.esc(ui.phone276(n.device_name, nameOf(n)))}</b>`,
                  ui.person216({ userId: n.user_id, name: nameOf(n), hasAvatar: n.has_avatar, admin: n.admin_role || false, blocked: n.blocked, size: 28 }),
                  way,
                  with216.length ? ui.esc(with216.join(', ')) : '<span class="subtle">\u2014</span>',
                ];
              }),
            }) : '<div class="empty216">No phone is online right now.</div>',
          })}
          <div class="row gap-sm"><button class="btn btn-sm" data-nav="map">Open map \u203a</button></div>
        </div>`;
      if (ctx.liveSync) ctx.liveSync();
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
      const who = ui.name276(e); // Bundle 276: name first, username without @
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
    subtitle: 'Password help requests and lost phones',
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
      // Bundle 222: kinds. "No code" = forgot password without the recovery
      // code (waits with no time limit; Approve gives a short code to read out).
      const noCode222 = (r) => r.kind === 'no_code';
      const card = (r) => ui.panel({
        body: `<div class="map216-row">${who216(map, r.user_id, ui.name276(r), `@${r.username || ''} \u00b7 ${noCode222(r) ? 'phone' : 'new phone'}: ${r.target_device_name || '\u2014'}`, { size: 40 })}
            ${noCode222(r) ? ui.pill216('No code', 'orange') : ui.pill216('Waiting', 'orange')}</div>
          <div class="type-caption" style="margin:6px 0 10px">Asked ${ui.esc(fmt.dateTime(r.created_at))} \u00b7 ${noCode222(r) ? 'forgot password, no recovery code \u00b7 waits until you answer' : 'ends ' + ui.esc(fmt.dateTime(r.expires_at))}</div>
          ${can ? `<textarea class="input" data-reason-for="${ui.esc(r.id)}" rows="2" maxlength="200" placeholder="Reason (only if you reject)" style="width:100%;height:auto;padding:8px"></textarea>
          <div class="row gap-sm" style="margin-top:10px">
            <button class="btn btn-primary" data-recovery="approve" data-id="${ui.esc(r.id)}">Approve</button>
            <button class="btn" data-recovery="reissue" data-id="${ui.esc(r.id)}">New code</button>
            <button class="btn btn-danger" data-recovery="reject" data-id="${ui.esc(r.id)}">Reject</button>
          </div>` : '<div class="type-caption">Your admin level can only look.</div>'}`,
      });
      const tone = { approved: ['Approved', 'green'], rejected: ['Rejected', 'red'], completed: ['Finished', 'green'], expired: ['Ended', 'grey'] };
      const kind222 = { no_code: ['No code', 'orange'], reset_code: ['Reset with code', 'green'], move: ['Lost phone', 'grey'] };
      el.innerHTML = `
        <div class="section">
          ${!result.ok ? ui.panel({ body: ui.errorState(result.error, 'recovery') }) : ''}
          ${pending.map(card).join('')}
          ${result.ok && !pending.length ? ui.panel({ body: '<div class="empty216"><b>No one is waiting</b><br>Requests show here when someone forgets the password without the recovery code, or starts recovery on a new phone.</div>' }) : ''}
          ${earlier.length ? ui.panel({
            title: 'Earlier',
            body: ui.table({
              columns: [{ label: 'Person' }, { label: 'Kind' }, { label: 'Phone' }, { label: 'When' }, { label: '' }],
              rows: earlier.map((r) => {
                const t = tone[String(r.status || '').toLowerCase()] || [fmt.titleCase(r.status || ''), 'grey'];
                const k = kind222[r.kind || 'move'] || kind222.move;
                const done = r.kind === 'reset_code' ? '<span class="type-caption">Done by the user (audit only)</span>' : ui.pill216(t[0], t[1]);
                return [who216(map, r.user_id, ui.name276(r), '@' + (r.username || ''), { size: 28 }),
                  ui.pill216(k[0], k[1]), emptyText(r.target_device_name), `<span>${ui.esc(fmt.dateTime(r.created_at))}</span>`, done];
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

  // Bundle 292 (TODO_292, mock web_map_292): server top centre, online
  // phones in rows under it (130 px apart, a small 26 px bow, a new row when
  // the stage width at 100 % is full), offline phones in rows under an
  // "OFFLINE" line. The world size comes from the rows (was a fixed 1000 x 700
  // with the offline row at the very bottom, so the default zoom fell to 0.5).
  // pos.box (not enumerable) = the world size + the bounds apart291 keeps to.
  function layout216(nodes, edges, stageW) {
    const GAP = 130; const ROW = 112; const BOW = 26;
    const sw = Math.max(320, Number(stageW) || 794);
    const perRow = Math.max(2, Math.floor((sw - 60) / GAP));
    // Bundle 293: 2 in a row (phones): no stagger, so the world is never wider than the stage.
    const stagger = perRow >= 3; const ROW_OFF = stagger ? ROW : 124;
    const W = Math.max(sw, Math.min(Math.max(1, nodes.length), perRow) * GAP + 60 + (stagger ? GAP / 2 : 0));
    const cx = W / 2;
    const pos = { server: { x: cx, y: 58, fixed: true } };
    const online = nodes.filter((n) => n.online);
    const offline = nodes.filter((n) => !n.online);
    let y = 58 + 148;
    const put = (list, bow) => {
      let above = [];
      for (let r = 0; r * perRow < list.length; r += 1) {
        const row = list.slice(r * perRow, (r + 1) * perRow);
        const w = (row.length - 1) * GAP;
        let xs = row.map((_, i) => cx - w / 2 + i * GAP);
        // a row under another never puts a phone right under one (stagger).
        if (stagger && xs.some((x) => above.some((a) => Math.abs(a - x) < 1))) xs = xs.map((x) => x + GAP / 2);
        row.forEach((n, i) => {
          const t = row.length > 1 ? (i / (row.length - 1)) * 2 - 1 : 0;
          pos[n.device_id] = { x: xs[i], y: y + (bow ? BOW * (1 - t * t) : 0) };
        });
        above = xs;
        y += bow ? ROW + BOW : ROW_OFF;
      }
    };
    put(online, true);
    if (offline.length) { y += BOW; put(offline, false); }
    const H = Math.max(y - ROW + 70, 58 + 148 + 70);
    Object.defineProperty(pos, 'box', { value: { W, H, top: 58 + 90, bottom: H - 40 }, enumerable: false });
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

  // Bundle 292 (TODO_292): the default zoom. Everybody's fit, kept between
  // 100 % and 140 % so names stay readable (12 px or more). When everybody
  // does not fit at 100 %: 100 %, centred, the top 18 px from the edge
  // (the "+N more" pill tells who is outside). The fit button keeps fit217.
  function fit292(points, stageW, stageH) {
    if (!points.length || !stageW || !stageH) return null;
    const xs = points.map((p) => p.x); const ys = points.map((p) => p.y);
    const minX = Math.min(...xs) - 70; const maxX = Math.max(...xs) + 70;
    const minY = Math.min(...ys) - 50; const maxY = Math.max(...ys) + 80;
    const bw = Math.max(1, maxX - minX); const bh = Math.max(1, maxY - minY);
    // Bundle 293 (TODO_293 A8): phone browsers (map 640 px or less) start a
    // little zoomed out: 80 - 90 % instead of 100 - 140 % (computer unchanged).
    if (stageW <= 640) {
      const wantP = Math.min(0.9, Math.min(stageW / bw, stageH / bh));
      const kp = Math.max(0.8, wantP);
      return { k: kp, x: (stageW - bw * kp) / 2 - minX * kp, y: wantP < 0.8 ? 18 - minY * kp : (stageH - bh * kp) / 2 - minY * kp };
    }
    const want = Math.min(1.4, Math.min(stageW / bw, stageH / bh));
    const over = want < 1;
    const k = Math.max(1, want);
    return { k, x: (stageW - bw * k) / 2 - minX * k, y: over ? 18 - minY * k : (stageH - bh * k) / 2 - minY * k };
  }

  // Bundle 292: how many of these points are outside the visible stage.
  function outside292(points, view, stageW, stageH) {
    return points.filter((p) => {
      const x = p.x * view.k + view.x; const y = p.y * view.k + view.y;
      return x < 0 || x > stageW || y < 0 || y > stageH - 40;
    }).length;
  }

  // Bundle 291 (TODO_291 B): no phone on top of another, no line under a
  // third phone's photo, no two lines on top of each other.
  // B-1: push phones apart (kept spots stay unless two kept ones overlap).
  function apart291(pos, ids, kept) {
    // Bundle 292: the bounds of the 292 layout (pos.box); old callers 1000 x 700.
    const box = pos.box || { W: 1000, H: 700, top: 190, bottom: 630 };
    const W = box.W; const top = box.top; const bottom = box.bottom;
    const gap = Math.min(120, Math.max(64, Math.sqrt(((W - 120) * Math.max(120, bottom - top)) / Math.max(1, ids.length)) * 0.7));
    const srv = pos.server;
    for (let step = 0; step < 400; step += 1) {
      let moved = false;
      for (let i = 0; i < ids.length; i += 1) {
        for (let j = i + 1; j < ids.length; j += 1) {
          const a = ids[i]; const b = ids[j]; const p = pos[a]; const q = pos[b];
          if (!p || !q) continue;
          let dx = q.x - p.x; let dy = q.y - p.y; let d = Math.hypot(dx, dy);
          if (d >= gap - 0.5) continue;
          if (d < 1) { const ang = (i + j * 2.399963) % (Math.PI * 2); dx = Math.cos(ang); dy = Math.sin(ang); d = 1; }
          const push = (gap - d) / d * (step < 300 ? 0.6 : 1);
          const ka = kept.has(a); const kb = kept.has(b);
          const wa = ka && !kb ? 0 : (!ka && kb ? 1 : 0.5);
          p.x -= dx * push * wa; p.y -= dy * push * wa;
          q.x += dx * push * (1 - wa); q.y += dy * push * (1 - wa);
          moved = true;
        }
      }
      ids.forEach((id) => {
        const p = pos[id];
        if (!p) return;
        if (srv) {
          const dx = p.x - srv.x; const dy = p.y - srv.y; const d = Math.max(1, Math.hypot(dx, dy));
          if (d < gap) { p.x += (dx / d) * (gap - d); p.y += (dy / d) * (gap - d); moved = true; }
        }
        p.x = Math.min(W - 60, Math.max(60, p.x)); p.y = Math.min(bottom, Math.max(top, p.y));
      });
      if (!moved) break;
    }
    // Still on top of someone (a crowded map): the nearest free spot on a grid,
    // new phones first.
    const far = (id, x, y) => ids.every((o) => o === id || !pos[o] || Math.hypot(pos[o].x - x, pos[o].y - y) >= gap - 1)
      && (!srv || Math.hypot(srv.x - x, srv.y - y) >= gap - 1);
    const order = ids.filter((id) => !kept.has(id)).concat(ids.filter((id) => kept.has(id)));
    order.forEach((id) => {
      const p = pos[id];
      if (!p || far(id, p.x, p.y)) return;
      let best = null;
      for (let y = top; y <= bottom; y += gap * 0.5) {
        for (let x = 60; x <= W - 60; x += gap * 0.5) {
          if (!far(id, x, y)) continue;
          const dd = Math.hypot(x - p.x, y - p.y);
          if (!best || dd < best.dd) best = { x, y, dd };
        }
      }
      if (best) { p.x = best.x; p.y = best.y; }
    });
    return pos;
  }

  // Same control point as arc217 (kept in step with it).
  function ctl291(p, q, C, k) {
    const dx = q.x - p.x; const dy = q.y - p.y;
    const len = Math.hypot(dx, dy) || 1;
    const mid = { x: (p.x + q.x) / 2, y: (p.y + q.y) / 2 };
    let n = { x: -dy / len, y: dx / len };
    const ox = mid.x - C.x; const oy = mid.y - C.y;
    if (Math.hypot(ox, oy) < 1) { if (n.y > 0) n = { x: -n.x, y: -n.y }; } else if (ox * n.x + oy * n.y < 0) n = { x: -n.x, y: -n.y };
    return { x: mid.x + n.x * k * len, y: mid.y + n.y * k * len };
  }

  function bez291(p, c, q, t) {
    const u = 1 - t;
    return { x: u * u * p.x + 2 * u * t * c.x + t * t * q.x, y: u * u * p.y + 2 * u * t * c.y + t * t * q.y };
  }

  // The centre mirrored through the middle of p-q: arc217 then bows the other way.
  function flip291(p, q, C) {
    const mid = { x: (p.x + q.x) / 2, y: (p.y + q.y) / 2 };
    if (Math.hypot(mid.x - C.x, mid.y - C.y) < 1) return { x: mid.x, y: mid.y - 10 };
    return { x: 2 * mid.x - C.x, y: 2 * mid.y - C.y };
  }

  // B-3: first try the usual bow; if it runs under a third phone (34 px) or
  // lies on another line (middles < 16 px), try the other side / other bows.
  function route291(p, q, C, ks, first, obstacles, mids) {
    const R = 34; const M = 16;
    const tries = [];
    ks.forEach((k) => { tries.push({ C, k }); tries.push({ C: flip291(p, q, C), k }); });
    let best = null;
    for (let i = 0; i < tries.length; i += 1) {
      const t = tries[i];
      const c = ctl291(p, q, t.C, t.k);
      let clear = Infinity;
      for (let s = 1; s < 24; s += 1) {
        const b = bez291(p, c, q, s / 24);
        obstacles.forEach((o) => { clear = Math.min(clear, Math.hypot(b.x - o.x, b.y - o.y)); });
      }
      const m = bez291(p, c, q, 0.5);
      let near = Infinity;
      mids.forEach((o) => { near = Math.min(near, Math.hypot(m.x - o.x, m.y - o.y)); });
      const score = Math.min(clear / R, near / M);
      if (!best || score > best.score + 1e-9) best = { score, i, m, t };
      if (clear >= R && near >= M) { best = { score, i, m, t }; break; }
    }
    mids.push(best.m);
    return best.i === 0 ? first : arc217(p, q, best.t.C, best.t.k);
  }

  const map = {
    id: 'map',
    title: 'Map',
    subtitle: 'Who is linked to whom, live',
    icon: 'i-hub',
    group: 'Network',
    isNew: true,
    async refresh(el, ctx) { await this.mount(el, ctx, true); return true; },
    async mount(el, ctx, quiet, liveData) {
      if (!quiet) el.innerHTML = ui.bootSkeleton219('map'); // Bundle 219
      // Bundle 274: a live frame brings the map itself (no extra request).
      const result = liveData ? { ok: true, data: liveData } : await settle(ctx.api('/api/v1/admin/link-map'));
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Map', body: ui.errorState(result.error, 'map') }); return; }
      const nodes = Array.isArray(result.data.nodes) ? result.data.nodes : [];
      const edges = Array.isArray(result.data.edges) ? result.data.edges : [];
      const linkOf = (n) => (n.blocked ? 'blocked' : (n.link || 'offline'));
      // Bundle 291 (B-2): one fixed order (server phones, other links,
      // blocked, offline; then name) so the map and the list never jump.
      const rank291 = (n) => { const k = linkOf(n); return k === 'server' ? 0 : (k === 'offline' ? 3 : (k === 'blocked' ? 2 : 1)); };
      nodes.sort((a, b) => rank291(a) - rank291(b) || ui.name276(a).localeCompare(ui.name276(b)) || String(a.device_id).localeCompare(String(b.device_id)));
      const count = (k) => nodes.filter((n) => linkOf(n) === k).length;
      const chips = [{ value: 'all', label: `All ${nodes.length}` }];
      [['server', 'Server'], ['wifi_direct', 'Wi-Fi Direct'], ['mesh', 'Mesh'], ['lan', 'Local Wi-Fi'], ['blocked', 'Blocked']].forEach(([k, label]) => {
        const c = count(k);
        if (c || k === 'server') chips.push({ value: k, label: `${label} ${c}` });
      });
      mapView.last = result.data;
      // Bundle 292: rows wrap at the stage width (last measured; first draw: page - side panel).
      const guessW = (el.clientWidth || 1070) - (window.innerWidth > 1100 ? 288 : 0) - 2;
      const usedW = mapView.stageW || guessW;
      const pos = layout216(nodes, edges, usedW);
      // Bundle 274 (W-2): a redraw keeps phones where they were (same online
      // state); only new phones get a new spot, only the lines change.
      const keep = mapView.pos || {};
      const kept291 = new Set();
      nodes.forEach((n) => { const o = keep[n.device_id]; if (o && o.online === !!n.online) { pos[n.device_id] = { x: o.x, y: o.y }; kept291.add(n.device_id); } });
      apart291(pos, nodes.map((n) => n.device_id), kept291); // Bundle 291 (B-1)
      mapView.pos = {};
      nodes.forEach((n) => { const q = pos[n.device_id]; if (q) mapView.pos[n.device_id] = { x: q.x, y: q.y, online: !!n.online }; });
      const shown = (n) => mapView.filter === 'all' || linkOf(n) === mapView.filter;
      // Bundle 217: aerial (flight-path) lines that bow away from the centre.
      const allPts = nodes.map((n) => pos[n.device_id]).filter(Boolean).concat([pos.server]);
      const C = allPts.reduce((a, p) => ({ x: a.x + p.x / allPts.length, y: a.y + p.y / allPts.length }), { x: 0, y: 0 });
      // Bundle 272 (M-3): phone<->phone lines bow away from the centre of the
      // phones only, with a smaller bow; server lines are unchanged.
      const phonePts = nodes.map((n) => pos[n.device_id]).filter(Boolean);
      const CP = phonePts.length ? phonePts.reduce((a, p) => ({ x: a.x + p.x / phonePts.length, y: a.y + p.y / phonePts.length }), { x: 0, y: 0 }) : C;
      const mids291 = [];
      // Bundle 293: points just outside the map edge; route291 keeps 34 px from them,
      // so a line never leaves the map (a free 217 line inside it is unchanged).
      const edge293 = [];
      if (pos.box) {
        for (let x = 0; x <= pos.box.W; x += 30) edge293.push({ x, y: -24 }, { x, y: pos.box.H + 24 });
        for (let y = 0; y <= pos.box.H; y += 30) edge293.push({ x: -24, y }, { x: pos.box.W + 24, y });
      }
      const lines = edges.map((e, i) => {
        const p = pos[e.from]; const q = pos[e.to];
        if (!p || !q) return '';
        // Bundle 291 (B-3): every other phone (and the server, for phone lines) is in the way.
        const in291 = nodes.filter((n) => n.device_id !== e.from && n.device_id !== e.to).map((n) => pos[n.device_id]).filter(Boolean);
        if (e.from !== 'server' && pos.server) in291.push(pos.server);
        in291.push(...edge293); // Bundle 293: lines stay inside the map
        const to = nodes.find((n) => n.device_id === e.to) || {};
        const from = nodes.find((n) => n.device_id === e.from) || {};
        const blocked = !!(to.blocked || from.blocked);
        const dim = !(mapView.filter === 'all' || (shown(to) && (e.from === 'server' || shown(from))));
        const first = e.from === 'server' ? arc217(p, q, C, 0.60) : arc217(p, q, CP, 0.25);
        const d = e.from === 'server' ? route291(p, q, C, [0.60, 0.35, 0.85, 0.15], first, in291, mids291)
          : route291(p, q, CP, [0.25, 0.45, 0.10], first, in291, mids291);
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
        const name = ui.name276(n); // Bundle 276
        const cls = ['map216-node', shown(n) ? '' : 'dim', n.online ? '' : 'off292', mapView.pick && mapView.pick === n.user_id ? 'sel' : ''].join(' ');
        return `<div class="${cls}" style="left:${p.x}px;top:${p.y}px" data-map-pick="${ui.esc(n.user_id || '')}" title="${ui.esc(ui.phone276(n.device_name, name))}">
          ${ui.avatar216({ userId: n.user_id, name, hasAvatar: n.has_avatar, admin: n.admin_role || false, blocked: n.blocked, size: 48 })}
          <span class="tag">${ui.esc(name)}</span></div>`;
      }).join('');
      // Bundle 292: the "OFFLINE" line, 52 px over the highest offline phone,
      // only when it stays under every online phone.
      const onY = nodes.filter((n) => n.online && pos[n.device_id]).map((n) => pos[n.device_id].y);
      const offPts = nodes.filter((n) => !n.online && pos[n.device_id]).map((n) => pos[n.device_id]);
      let offLine = '';
      if (offPts.length) {
        const ly = Math.min(...offPts.map((p) => p.y)) - 52;
        const allX = nodes.map((n) => pos[n.device_id]).filter(Boolean).map((p) => p.x);
        const lx = Math.min(...allX) - 50; const lw = Math.max(...allX) + 50 - lx;
        if (!onY.length || ly > Math.max(...onY) + 40) offLine = `<div class="map292-off" style="left:${lx}px;top:${ly}px;width:${lw}px"><span>OFFLINE</span></div>`;
      }
      const sideList = nodes.filter((n) => !mapView.pick || n.user_id === mapView.pick);
      const legend = Object.keys(ui.LINKS_216).filter((k) => edges.some((e) => e.link === k))
        .map((k) => `<span><i style="background:${ui.LINKS_216[k].color}"></i>${ui.esc(ui.LINKS_216[k].label)}</span>`).join('')
        + (nodes.some((n) => n.blocked) ? '<span><i class="dash217"></i>Blocked \u00b7 relays only</span>' : '');
      const legendCount293 = (legend.match(/<span>/g) || []).length;
      el.innerHTML = `
        <div class="section">
          <div class="bar216">${ui.chips216(chips, mapView.filter, 'data-map-filter')}${ctx.liveBadge ? ctx.liveBadge() : ''}<div class="map216-legend">${legend}</div></div>
          <div class="map216">
            <div class="map217-wrap">
            <div class="map216-stage" data-map-stage>
              <div class="map216-world" data-map-world style="width:${pos.box.W}px;height:${pos.box.H}px;transform:translate(${mapView.x}px,${mapView.y}px) scale(${mapView.k})">
                <svg width="${pos.box.W}" height="${pos.box.H}" aria-hidden="true">${lines}</svg>${offLine}
                <div class="map216-node" style="left:${pos.server.x}px;top:${pos.server.y}px">
                  <span class="map216-server">${ui.icon('i-server')}</span><span class="tag" style="background:none">Server</span></div>
                ${nodeHtml}
              </div>
              <div class="map216-zoom"><button data-map-zoom="in" aria-label="Zoom in">+</button><button data-map-zoom="out" aria-label="Zoom out">\u2212</button><button data-map-zoom="fit" aria-label="Fit everyone">${ui.icon('i-fit')}</button><span class="map292-pct" data-map-pct>${Math.round(mapView.k * 100)}%</span></div>
              <div class="map292-more" data-map-more hidden></div>
              ${nodes.length ? '' : '<div class="empty216" style="position:absolute;left:50%;top:50%;transform:translate(-50%,-50%)">No phones yet.</div>'}
            </div>
            </div>
            ${ui.panel({
              body: `<div class="map216-side"><h2>Phones (${sideList.length})</h2><div class="map292-rows">
                ${sideList.map((n) => {
                  const k = linkOf(n);
                  const pill = k === 'blocked' ? ui.pill216('Blocked', 'red')
                    : (ui.LINKS_216[k] ? ui.pill216(ui.LINKS_216[k].label, ui.LINKS_216[k].tone) : ui.pill216('Offline', 'grey'));
                  return `<div class="map216-row">${ui.person216({ userId: n.user_id, name: ui.name276(n), sub: ui.phone276(n.device_name, ui.name276(n)), hasAvatar: n.has_avatar, admin: n.admin_role || false, blocked: n.blocked, size: 30 })}${pill}</div>`;
                }).join('')}</div>
                <div class="type-caption" style="margin-top:14px">${mapView.pick ? '<a href="#" data-map-pick="">Show everyone</a>' : 'Click a person to see their phones.'}</div></div>`,
            })}
          </div>
        </div>`;

      const world = el.querySelector('[data-map-world]');
      const stage = el.querySelector('[data-map-stage]');
      // Bundle 293: a legend with one line type is hidden on phones (CSS .one).
      const legendEl293 = el.querySelector('.map216-legend');
      if (legendEl293) legendEl293.classList.toggle('one', legendCount293 < 2);
      // Bundle 293: phones / tablets (one column): the map fits the visible
      // screen and 64 px of the Phones list peeks under it, so the page can be
      // scrolled from there. Kept per screen width (the address bar coming and
      // going does not resize it). Computer: the 292 CSS height.
      const fitHeight293 = () => {
        if (window.innerWidth > 1100) { stage.style.height = ''; return; }
        if (mapView.h293 && mapView.w293 === window.innerWidth) { stage.style.height = mapView.h293 + 'px'; return; }
        const sc = (document.scrollingElement && document.scrollingElement.scrollTop) || 0;
        const top = stage.getBoundingClientRect().top + sc + (document.body.scrollTop || 0);
        mapView.w293 = window.innerWidth;
        mapView.h293 = Math.max(300, Math.round(window.innerHeight - top - 64));
        stage.style.height = mapView.h293 + 'px';
      };
      fitHeight293();
      // Bundle 292: the zoom text and the "+N more" pill follow every move.
      const pct = el.querySelector('[data-map-pct]');
      const more = el.querySelector('[data-map-more]');
      mapView.stageW = stage.clientWidth || mapView.stageW;
      // First draw with a guessed width that gives another row length: lay out once more.
      const row292 = (w) => Math.max(2, Math.floor((Math.max(320, w) - 60) / 130));
      const relayout = !mapView.relaid && !!stage.clientWidth && row292(usedW) !== row292(stage.clientWidth);
      mapView.relaid = relayout;
      const apply = () => {
        world.style.transform = `translate(${mapView.x}px,${mapView.y}px) scale(${mapView.k})`;
        if (pct) pct.textContent = `${Math.round(mapView.k * 100)}%`;
        if (more) {
          const n = outside292(nodes.filter(shown).map((x) => pos[x.device_id]).filter(Boolean), mapView, stage.clientWidth, stage.clientHeight);
          more.hidden = n === 0;
          more.textContent = !n ? '' : (window.innerWidth <= 640 ? `+${n} more \u00b7 drag the map to see them` // Bundle 293: short on phones
            : `+${n} more \u00b7 drag the map, or press the fit button to see everyone`);
        }
      };
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
        // Bundle 274: a live frame that came during a drag is drawn now.
        if (pts.size === 0 && mapView.pendingLive) { const m = mapView.pendingLive; mapView.pendingLive = null; this.mount(el, ctx, true, m); }
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
      // Bundle 292: the default view = fit292 (100 - 140 %), unless the fit
      // button asked for everybody (fitAll, until a filter change or a new visit).
      const autoFit = () => {
        if (mapView.fitAll) { fit(); return; }
        const box = fit292(nodes.filter(shown).map((n) => pos[n.device_id]).filter(Boolean).concat([pos.server]), stage.clientWidth, stage.clientHeight);
        if (!box) return;
        mapView.x = box.x; mapView.y = box.y; mapView.k = box.k; apply();
      };
      el.querySelectorAll('[data-map-zoom]').forEach((b) => b.addEventListener('click', () => {
        const r = stage.getBoundingClientRect();
        const z = b.getAttribute('data-map-zoom');
        b.blur(); // Bundle 293: no ring left on the button after a tap
        if (z === 'fit') { mapView.fitted = false; mapView.fitAll = true; fit(); return; }
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
        resizeTimer = setTimeout(() => { if (!stage.isConnected) return; fitHeight293(); if (!mapView.fitted) autoFit(); }, 150);
      };
      window.addEventListener('resize', mapView.offResize);
      el.querySelectorAll('[data-map-filter]').forEach((b) => b.addEventListener('click', () => { mapView.filter = b.getAttribute('data-map-filter'); mapView.fitted = false; mapView.fitAll = false; this.mount(el, ctx, true, mapView.last); }));
      el.querySelectorAll('[data-map-pick]').forEach((b) => b.addEventListener('click', (ev) => {
        ev.preventDefault();
        const id = b.getAttribute('data-map-pick');
        mapView.pick = id && mapView.pick !== id ? id : '';
        this.mount(el, ctx, true, mapView.last);
      }));
      if (!quiet) mapView.fitAll = false;
      if (!quiet || !mapView.fitted) autoFit(); else apply(); // Bundle 292
      // Bundle 274 (W-1): redraw on every live frame while the Map is open.
      if (!quiet && !liveData && el.isConnected) {
        ctx.live((msg) => {
          if (ctx.state.view !== 'map' || !el.isConnected) return;
          const st = el.querySelector('[data-map-stage]');
          if (st && st.classList.contains('drag')) { mapView.pendingLive = msg; return; }
          this.mount(el, ctx, true, msg);
        });
      }
      if (ctx.liveSync) ctx.liveSync();
      if (relayout) { mapView.pos = {}; this.mount(el, ctx, true, mapView.last); } // Bundle 292
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
    // Bundle 221: progress of answers refreshes quietly while open.
    async refresh(el) {
      if (!el._conv221) return false;
      await el._conv221.poll();
      return true;
    },
    async mount(el, ctx) {
      el._conv221 = null;
      const scope = {
        mode: 'list', kind: '', a: '', b: '', groupId: '', groupName: '', memberCount: 0,
        participants: [], messages: [], cursor: '', hasMore: false, rows: [], error: '', loading: false,
        objectUrls: [],
        // Bundle 221
        requests: [], tab: 'all', shown: [], viewId: '', view: null, viewReq: null, sheet: null, del: null, menu: null, topMenu: false,
      };

      const root = document.createElement('div');
      el.innerHTML = '';
      el.appendChild(root);

      const personName = (p) => ui.name276({ display_name: p.name && p.name !== p.id ? p.name : '', username: p.username }); // Bundle 276
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
          const author = ui.name276({ display_name: m.sender_name, username: m.sender_username }); // Bundle 276
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

      // ------------------------------------------------ Bundle 221: chats come from the phones
      // The server keeps a message only until it is delivered (+7 days), so
      // the list is who / how many / when (conv_index) and reading a chat
      // means asking the phones in it. "N on server" still opens the old
      // server view for what is left there.
      function auditNotice() {
        return `<div class="info221">${ui.icon('i-info')}<span>The server keeps a message only until it is delivered (+7 days). To read a chat, ask the phones in it. Reading is recorded in the Audit log.</span></div>`;
      }

      // Display name first in the new list ("Sajjad & Sumaira").
      const shownName = (p) => ui.name276({ display_name: p.name && p.name !== p.id ? p.name : '', username: p.username }); // Bundle 276
      const ROUTE221 = { server: 'Server', direct: 'Wi-Fi Direct', wifi_direct: 'Wi-Fi Direct', mesh: 'Mesh', lan: 'Local Wi-Fi' };

      function chatKey(c) {
        if (!c || !c.kind) return '';
        return c.kind === 'group' ? 'g:' + c.group_id : 'd:' + (c.devices || []).slice().sort().join(',');
      }

      function liveRequest(q) {
        return q && q.action === 'get_chat' && !q.closed && q.state !== 'cancelled' && q.chat && q.chat.kind;
      }

      function requestFor(row) {
        const ids = row.kind === 'group' ? [] : (row.participants || []).map((p) => p.id);
        return scope.requests.filter((q) => liveRequest(q) && q.chat.kind === row.kind
          && (row.kind === 'group'
            ? q.chat.group_id === row.group_id
            : ids.length === 2 && ids.every((id) => (q.chat.devices || []).indexOf(id) >= 0)))[0] || null;
      }

      function entries() {
        const list = scope.rows.map((row) => ({ row, q: requestFor(row) }));
        const seen = {};
        list.forEach((e) => { if (e.q) seen[chatKey(e.q.chat)] = 1; });
        // Requests for chats the server never saw (Wi-Fi Direct / mesh only).
        scope.requests.forEach((q) => {
          if (!liveRequest(q) || seen[chatKey(q.chat)]) return;
          seen[chatKey(q.chat)] = 1;
          list.push({ row: null, q });
        });
        return list;
      }

      function partsDone(q) { return q && num(q.of) > 0 && num(q.parts) >= num(q.of); }

      function statusOf(q) {
        if (!q) return '';
        if (partsDone(q)) return ui.pill216('Ready', 'green');
        if (num(q.parts) > 0) return ui.pill216(`${q.parts} of ${q.of} parts`, 'blue');
        if (num(q.old_app) > 0) return ui.pill216('Pending \u00b7 old app', 'red');
        return ui.pill216('Pending', 'orange');
      }

      function lastWhen(iso) {
        const d = new Date(iso);
        if (Number.isNaN(d.getTime())) return '\u2014';
        const today = new Date(); today.setHours(0, 0, 0, 0);
        const hm = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false });
        if (d >= today) return 'Today ' + hm;
        if (d >= new Date(today.getTime() - 86400000)) return 'Yesterday ' + time12(iso);
        return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
      }

      function listPanel() {
        const all = entries();
        const kindOf = (e) => (e.row ? e.row.kind : e.q.chat.kind);
        const priv = all.filter((e) => kindOf(e) !== 'group');
        const groups = all.filter((e) => kindOf(e) === 'group');
        const tab = scope.tab || 'all';
        const shown = tab === 'private' ? priv : (tab === 'groups' ? groups : all);
        scope.shown = shown;
        const rows = shown.map((e, i) => {
          const r = e.row;
          const q = e.q;
          let who; let sub; let kind; let avatars;
          if (r && r.kind === 'group') {
            kind = 'group';
            who = r.group_name || r.group_id;
            sub = `${fmt.number(r.member_count || 0)} members`;
            avatars = ui.avatar216({ name: who, size: 40 });
          } else if (r) {
            kind = 'direct';
            const people = r.participants || [];
            who = people.map(shownName).join(' & ');
            sub = people.filter((p) => p.username).map((p) => '@' + p.username).join(' \u00b7 ');
            avatars = `<span class="pair221">${people.slice(0, 2).map((p) => ui.avatar216({ name: shownName(p), size: 28 })).join('')}</span>`;
          } else {
            kind = q.chat.kind;
            who = q.label || 'Chat';
            sub = 'not on the server list';
            avatars = ui.avatar216({ name: who, size: 40 });
          }
          let count = '\u2014';
          if (r) {
            const people = r.participants || [];
            const open = r.kind === 'group'
              ? `data-conversation-open data-kind="group" data-group="${ui.esc(r.group_id)}"`
              : `data-conversation-open data-kind="direct" data-a="${ui.esc(people[0] ? people[0].id : '')}" data-b="${ui.esc(people[1] ? people[1].id : '')}"`;
            count = `<b>${ui.esc(fmt.number(r.message_count || 0))}</b>${num(r.on_server) > 0
              ? `<button class="link221" type="button" ${open}>${ui.esc(fmt.number(r.on_server))} on server</button>`
              : '<small class="subtle">0 on server</small>'}`;
          }
          let button;
          if (!q) button = `<button class="btn btn-sm" type="button" data-conv221-ask="${i}">Request from phones</button>`;
          else if (num(q.parts) > 0) button = `<button class="btn btn-primary btn-sm" type="button" data-conv221-open="${ui.esc(q.id)}">Open</button>`;
          else button = `<button class="btn btn-sm" type="button" data-conv221-open="${ui.esc(q.id)}">View request</button>`;
          return [
            `<div class="p216">${avatars}<span class="p216-t"><b>${ui.esc(who)}</b><small>${ui.esc(sub)}</small></span></div>`,
            ui.pill216(kind === 'group' ? 'Group' : 'Private', 'grey'),
            `<div class="cnt221">${count}</div>`,
            `<span class="subtle">${ui.esc(r ? lastWhen(r.last_message_at) : made220(q.made_at))}</span>`,
            statusOf(q),
            `<div class="act221">${button}</div>`,
          ];
        });
        return `<div class="top221">
            ${ui.chips216([
              { value: 'all', label: 'All ' + all.length },
              { value: 'private', label: 'Private ' + priv.length },
              { value: 'groups', label: 'Groups ' + groups.length },
            ], tab, 'data-conv221-tab')}
            <span class="hint221">Pick 2 people or a group (also chats the server never saw)</span>
            <button class="btn btn-primary" type="button" data-conv221-new>New request</button>
          </div>
          ${ui.panel({
            pad0: true,
            body: rows.length
              ? `<div style="padding:0 4px">${ui.table({
                columns: [{ label: 'Chat' }, { label: 'Kind' }, { label: 'Messages' }, { label: 'Last message' }, { label: 'Status' }, { label: '' }],
                rows,
              })}</div>`
              : '<div class="empty216">No chats on the server list yet. Use New request to ask the phones of any 2 people or a group.</div>',
          })}`;
      }

      // ---------------------------------------------------------------- viewer
      function dayLabel(iso) {
        const d = new Date(iso);
        return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
      }

      function rangeText(from, to) {
        if (!from && !to) return 'Full chat';
        return `${dayLabel(from + 'T12:00:00')} - ${dayLabel(to + 'T12:00:00')}`;
      }

      function delWhen(iso) {
        const d = new Date(iso);
        if (Number.isNaN(d.getTime())) return '';
        const hm = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false });
        return `${dayLabel(iso)} ${hm}`;
      }

      function sideOf(m) {
        const v = scope.view || {};
        if (v.chat && v.chat.kind === 'group') return 'in';
        if (m.sender && v.from_device && m.sender === v.from_device) return 'in';
        if (m.sender_name && v.from_name && m.sender_name === v.from_name) return 'in';
        return 'out';
      }

      function fileRequest(fileId) {
        return scope.requests.filter((q) => q.action === 'get_file' && q.file_id === fileId && q.state !== 'cancelled' && !q.closed)[0] || null;
      }

      function att221(a, m) {
        const isImage = /^image\//.test(String(a.type || ''));
        const word = isImage ? 'photo' : 'file';
        const thumb = a.thumb && /^[A-Za-z0-9+/=_-]+$/.test(a.thumb)
          ? `<img class="th221" alt="${ui.esc(a.name || word)}" src="data:${a.thumb_type === 'image/png' ? 'image/png' : 'image/jpeg'};base64,${a.thumb}">`
          : `<span class="th221 none">${ui.icon(isImage ? 'i-image' : 'i-file')}<small>${ui.esc(a.name || word)}</small></span>`;
        const f = a.file_id ? fileRequest(a.file_id) : null;
        let action;
        if (a.file_gone) action = '<span class="gone221">File removed after 60 days \u00b7 thumbnail only</span>';
        else if (partsDone(f)) action = `<button class="btn btn-sm" type="button" data-conv221-show="${ui.esc(f.id)}" data-image="${isImage ? '1' : ''}" data-name="${ui.esc(a.name || word)}">Show full ${word}</button>`;
        else if (f) action = `<span class="subtle">${num(f.parts) ? `${f.parts} of ${f.of} parts` : 'Asked the phones \u00b7 waiting'}</span>`;
        else if (a.file_id) action = `<button class="btn btn-sm" type="button" data-conv221-file="${ui.esc(a.file_id)}" data-mid="${ui.esc(m.id || '')}" data-name="${ui.esc(a.name || '')}">Get full ${word}</button>`;
        else action = '';
        return `<div class="att221">${thumb}${action}</div>`;
      }

      function bubble221(m, i) {
        const side = sideOf(m);
        const route = ROUTE221[m.route] ? `<span class="tag221">${ROUTE221[m.route]}</span>` : '';
        const cls = ['chat-bubble', side, 'b221', m.deleted_at ? 'hid221' : '', m.outside ? 'margin221' : ''].filter(Boolean).join(' ');
        const menu = scope.menu === i
          ? `<div class="menu221" role="menu">
              ${m.body ? `<button type="button" data-conv221-copy="${i}">Copy text</button>` : ''}
              <button type="button" class="danger" data-conv221-delmsg="${i}">Delete completely</button>
            </div>` : '';
        return `<div class="chat-row ${side}"><div class="${cls}">
            <div class="head221"><b class="who221">${ui.esc(ui.name276({ display_name: m.sender_name, username: m.sender_username }))}</b>${route}<span class="t221">${ui.esc(time12(m.created_at))}</span>
              <button class="kebab221" type="button" data-conv221-menu="${i}" aria-label="Message menu"><i></i><i></i><i></i></button></div>
            ${m.body ? `<div class="chat-body">${ui.esc(m.body)}</div>` : ''}
            ${(Array.isArray(m.attachments) ? m.attachments : []).map((a) => att221(a, m)).join('')}
            ${m.deleted_at ? `<span class="del221">Deleted by user \u00b7 ${ui.esc(delWhen(m.deleted_at))}</span>` : ''}
            ${menu}
          </div></div>`;
      }

      function thread221(messages) {
        let lastDay = '';
        let inMargin = false;
        return `<div class="chat-thread t221wrap">${messages.map((m, i) => {
          let head = '';
          const day = dayLabel(m.created_at);
          if (day && day !== lastDay) { head += `<div class="day221"><span>${ui.esc(day)}</span></div>`; lastDay = day; }
          if (m.outside && !inMargin) head += '<div class="chip221">Outside the dates (phone clock)</div>';
          inMargin = !!m.outside;
          return head + bubble221(m, i);
        }).join('')}</div>`;
      }

      function progress221() {
        const v = scope.view || {};
        if (v.complete) return '';
        if (num(v.of) > 0) {
          const pct = Math.max(2, Math.min(100, Math.round(100 * num(v.parts) / num(v.of))));
          return `<div class="prog221"><b>${v.parts} of ${v.of} parts \u00b7 the rest is on the way</b><span class="bar"><i style="width:${pct}%"></i></span></div>`;
        }
        const q = scope.viewReq || {};
        return `<div class="prog221"><b>${num(q.old_app) > 0 ? 'Pending \u00b7 old app: the phones in this chat must update to 3.0.61 to answer' : 'Pending \u00b7 waiting for a phone in this chat'}</b><small>The request goes to every phone, friends or not. No time limit.</small></div>`;
      }

      function countFor(kind) {
        const v = scope.view || {};
        const msgs = Array.isArray(v.messages) ? v.messages.filter((m) => !m.outside) : [];
        if (kind === 'message') return 1;
        if (!v.complete || !msgs.length && !v.of) return null;
        const covers = (fromDay) => !v.from || v.from <= fromDay;
        if (kind === 'full') return v.from ? null : msgs.length;
        if (kind === '7' || kind === '30') {
          const days = Number(kind);
          const cut = new Date(Date.now() - days * 86400000);
          const cutDay = cut.toISOString().slice(0, 10);
          if (!covers(cutDay)) return null;
          return msgs.filter((m) => String(m.created_at) >= cut.toISOString()).length;
        }
        if (kind === 'dates') {
          const d = scope.del || {};
          if (!d.from || !d.to || !covers(d.from)) return null;
          return msgs.filter((m) => { const day = String(m.created_at).slice(0, 10); return day >= d.from && day <= d.to; }).length;
        }
        return null;
      }

      function deletePanel() {
        const d = scope.del;
        const v = scope.view || {};
        const devices = v.chat && Array.isArray(v.chat.devices) ? v.chat.devices.length : 0;
        const options = [
          d.msg != null ? { value: 'message', label: 'This message' } : null,
          { value: 'full', label: 'Full chat' },
          { value: '7', label: 'Last 7 days' },
          { value: '30', label: 'Last 30 days' },
          { value: 'dates', label: 'Choose dates' },
        ].filter(Boolean);
        const n = countFor(d.scope);
        const button = n == null ? 'Delete completely' : `Delete ${fmt.number(n)} message${n === 1 ? '' : 's'} completely`;
        return `<div class="del221p">
          <div class="del221h"><span class="ic">${ui.icon('i-trash-user')}</span><b>Delete completely</b></div>
          ${options.map((o) => {
            const c = countFor(o.value);
            return `<label class="radio221"><input type="radio" name="del221" value="${o.value}" ${d.scope === o.value ? 'checked' : ''} data-conv221-scope>
              <span>${o.label}</span><small>${c == null ? 'count unknown' : fmt.number(c) + ' msg'}</small></label>${o.value === 'dates'
                ? `<span class="dates221"><input type="date" aria-label="From" data-conv221-dfrom value="${ui.esc(d.from || '')}"><span>to</span><input type="date" aria-label="To" data-conv221-dto value="${ui.esc(d.to || '')}"></span>` : ''}`;
          }).join('')}
          <p class="subtle">Removed from every phone (${devices ? devices + ' phones' : 'all phones of the chat'}), also the copies users deleted. Can't be undone.${n == null && d.scope !== 'message' ? ' Count unknown \u2014 this copy does not cover it.' : ''}</p>
          <div class="row221"><button class="btn" type="button" data-conv221-delx>Cancel</button>
            <button class="btn btn-danger del221go" type="button" data-conv221-delgo>${ui.esc(button)}</button></div>
        </div>`;
      }

      function viewerPanel() {
        const v = scope.view || {};
        const q = scope.viewReq || {};
        const messages = Array.isArray(v.messages) ? v.messages : [];
        const title = q.label || (v.chat && v.chat.kind === 'group' ? 'Group' : 'Chat');
        const sub = [rangeText(v.from || q.from, v.to || q.to), v.from_name ? `from ${v.from_name}'s phone` : '', 'this copy is deleted 24 h after you read it'].filter(Boolean).join(' \u00b7 ');
        const notes = [
          v.trimmed ? 'Big chat: the phone left out the oldest thumbnails (5 MB limit).' : '',
          num(v.broken) ? `${v.broken} part(s) could not be opened.` : '',
        ].filter(Boolean).map((t) => `<div class="note221">${ui.esc(t)}</div>`).join('');
        const top = scope.topMenu
          ? `<div class="menu221 top" role="menu">
              ${messages.length ? '<button type="button" class="danger" data-conv221-delopen>Delete completely\u2026</button>' : ''}
              <button type="button" data-conv221-again>Ask the phones again</button>
              ${q.state === 'pending' && !num(v.parts) ? '<button type="button" data-conv221-cancel>Cancel request</button>' : ''}
            </div>` : '';
        const body = messages.length
          ? thread221(messages)
          : `<div class="empty216">${num(v.parts) ? 'This part has no messages in the dates.' : 'No phone has answered yet.'}</div>`;
        return `<div class="vhead221">
            <button class="iconbtn221" type="button" data-conv221-back aria-label="Back">${ui.icon('i-chevron-left')}</button>
            <div class="vt221"><b>${ui.esc(title)}</b><small>${ui.esc(sub)}</small></div>
            ${num(v.parts) > 0 ? '<button class="btn" type="button" data-conv221-close>Close</button>' : ''}
            <span class="topwrap221"><button class="iconbtn221 boxed" type="button" data-conv221-top aria-label="More"><span class="kebab221 on"><i></i><i></i><i></i></span></button>${top}</span>
          </div>
          <div class="viewer221 ${scope.del ? 'has-side' : ''}">
            <div class="vmain221">${notes}${body}${progress221()}</div>
            ${scope.del ? `<div class="vside221">${deletePanel()}</div>` : ''}
          </div>`;
      }

      // ------------------------------------------------------- request sheet
      function sheet221() {
        const s = scope.sheet;
        if (!s) return '';
        const option = (list, value, label) => list.map((x) => `<option value="${ui.esc(x.id)}" ${x.id === value ? 'selected' : ''}>${ui.esc(label(x))}</option>`).join('');
        const userLabel = (u) => { const n = ui.name276(u); return u.username && n !== u.username ? `${n} (@${u.username})` : n; }; // Bundle 276
        let who;
        if (s.fixed) {
          who = `<div class="fixed221"><b>${ui.esc(s.fixed.label)}</b><small>${s.fixed.kind === 'group' ? 'Group' : 'Private chat'}</small></div>`;
        } else if (s.loading) {
          who = '<div class="skeleton w-60" style="height:18px"></div>';
        } else {
          who = `${ui.chips216([{ value: 'direct', label: 'Private chat' }, { value: 'group', label: 'Group' }], s.kind, 'data-conv221-kind')}
            ${s.kind === 'group'
              ? `<label class="field221">Group<select data-conv221-g><option value="">Pick a group</option>${option(s.groups, s.group, (g) => `${g.name} (${g.members} members)`)}</select></label>`
              : `<label class="field221">Person 1<select data-conv221-a><option value="">Pick a person</option>${option(s.users, s.a, userLabel)}</select></label>
                 <label class="field221">Person 2<select data-conv221-b><option value="">Pick a person</option>${option(s.users, s.b, userLabel)}</select></label>`}`;
        }
        return `<div class="sheet221-bg" data-conv221-sheetbg>
          <div class="sheet221" role="dialog" aria-label="Request from phones">
            <h3>Request from phones</h3>
            ${who}
            <div class="lab221">Which messages</div>
            ${ui.chips216([
              { value: 'full', label: 'Full chat' }, { value: '7', label: 'Last 7 days' },
              { value: '30', label: 'Last 30 days' }, { value: 'dates', label: 'Choose dates' },
            ], s.range, 'data-conv221-range')}
            ${s.range === 'dates' ? `<div class="dates221 wide"><label>From<input type="date" data-conv221-from value="${ui.esc(s.from)}"></label><label>To<input type="date" data-conv221-to value="${ui.esc(s.to)}"></label></div>` : ''}
            <p class="subtle">Every phone gets the request, friends or not. A phone in the chat sends its copy back in parts (also messages users deleted). One extra day on both sides. Pending until a phone is reached. Saved in the Audit log.</p>
            <div class="row221"><button class="btn" type="button" data-conv221-sheetx>Cancel</button>
              <button class="btn btn-primary" type="button" data-conv221-send ${s.sending ? 'disabled' : ''}>Send request</button></div>
          </div>
        </div>`;
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
        let main;
        if (scope.loading) main = ui.skeletonPanel(4);
        else if (scope.error) main = ui.panel({ body: ui.errorState(scope.error, 'conversations') });
        else if (scope.mode === 'thread') {
          main = `<div class="vhead221"><button class="btn btn-ghost btn-sm" type="button" data-conversation-back>${ui.icon('i-chevron-left')}All conversations</button>
            <div class="vt221"><b>Server copy</b><small>Only messages not yet delivered (+7 days) are still on the server</small></div></div>${threadPanel()}`;
        } else if (scope.mode === 'viewer') main = viewerPanel();
        else main = listPanel();
        root.innerHTML = `<div class="section conv221">
          ${scope.mode === 'viewer' ? '' : auditNotice()}
          ${main}
          ${sheet221()}
        </div>`;
        hydrateMedia();
      }

      async function loadRequests() {
        const result = await settle(ctx.api('/api/v1/admin/chat-requests'));
        if (result.ok) scope.requests = Array.isArray(result.data.requests) ? result.data.requests : [];
        return result;
      }

      async function loadList(quiet) {
        if (!quiet) { scope.loading = true; scope.error = ''; draw(); }
        const [result] = await Promise.all([settle(ctx.api('/api/v1/admin/conversations?limit=100')), loadRequests()]);
        scope.loading = false;
        if (!result.ok) { if (!quiet) scope.error = result.error; } else scope.rows = Array.isArray(result.data.conversations) ? result.data.conversations : [];
        draw();
      }

      async function loadViewer(quiet) {
        if (!quiet) { scope.loading = true; scope.error = ''; draw(); }
        await loadRequests();
        scope.viewReq = scope.requests.filter((q) => q.id === scope.viewId)[0] || scope.viewReq || {};
        const result = await settle(ctx.api(`/api/v1/admin/chat-requests/${encodeURIComponent(scope.viewId)}/answer`));
        scope.loading = false;
        if (!result.ok) {
          if (quiet) return;
          ctx.toast(result.error || 'Could not open the answer', 'error');
          scope.mode = 'list';
          await loadList();
          return;
        }
        scope.view = result.data || {};
        draw();
      }

      function openViewer(id) {
        scope.mode = 'viewer';
        scope.viewId = id;
        scope.view = null;
        scope.viewReq = null;
        scope.del = null;
        scope.menu = null;
        scope.topMenu = false;
        loadViewer();
      }

      async function openSheet(fixed) {
        scope.sheet = { fixed: fixed || null, kind: 'direct', a: '', b: '', group: '', range: 'full', from: '', to: '', users: [], groups: [], loading: !fixed, sending: false };
        draw();
        if (fixed) return;
        const [users, groups] = await Promise.all([
          settle(ctx.api('/api/v1/admin/users?limit=200')),
          settle(ctx.api('/api/v1/admin/groups?limit=200')),
        ]);
        if (!scope.sheet) return;
        scope.sheet.users = users.ok && Array.isArray(users.data.users) ? users.data.users : [];
        scope.sheet.groups = groups.ok && Array.isArray(groups.data.groups) ? groups.data.groups : [];
        scope.sheet.loading = false;
        draw();
      }

      function chatBody(chat) {
        if (!chat) return {};
        if (chat.kind === 'group') return { kind: 'group', group_id: chat.group_id };
        const ids = (chat.users && chat.users.length === 2) ? chat.users : (chat.devices || []);
        return { kind: 'direct', user_a: ids[0] || '', user_b: ids[1] || '' };
      }

      async function sendSheet() {
        const s = scope.sheet;
        let body;
        if (s.fixed) body = Object.assign({}, s.fixed.body);
        else if (s.kind === 'group') body = { kind: 'group', group_id: s.group };
        else body = { kind: 'direct', user_a: s.a, user_b: s.b };
        if (!s.fixed && (s.kind === 'group' ? !s.group : (!s.a || !s.b))) { ctx.toast(s.kind === 'group' ? 'Pick a group' : 'Pick 2 people', 'error'); return; }
        if (!s.fixed && s.kind !== 'group' && s.a === s.b) { ctx.toast('Pick 2 different people', 'error'); return; }
        if (s.range === 'dates' && (!s.from || !s.to || s.from > s.to)) { ctx.toast('Pick the dates (from before to)', 'error'); return; }
        body.range = s.range;
        if (s.range === 'dates') { body.from = s.from; body.to = s.to; }
        s.sending = true;
        draw();
        try {
          const out = await ctx.api('/api/v1/admin/chat-requests', { method: 'POST', body });
          ctx.toast(`Sent to all phones \u00b7 ${num(out && out.phones)} in the chat \u00b7 Pending until one is reached`, 'success');
          scope.sheet = null;
          await loadList(true);
        } catch (err) {
          s.sending = false;
          ctx.toast(err.message || 'Could not send the request', 'error');
          draw();
        }
      }

      async function sendDelete() {
        const d = scope.del;
        const v = scope.view || {};
        const body = chatBody(v.chat || (scope.viewReq || {}).chat);
        if (!body.kind) { ctx.toast('This chat is not known on the server', 'error'); return; }
        if (d.scope === 'message') {
          const m = (v.messages || [])[d.msg];
          if (!m) return;
          body.scope = 'message';
          body.ids = [m.id];
        } else if (d.scope === 'full') body.scope = 'full';
        else {
          body.scope = 'range';
          body.range = d.scope;
          if (d.scope === 'dates') {
            if (!d.from || !d.to || d.from > d.to) { ctx.toast('Pick the dates (from before to)', 'error'); return; }
            body.from = d.from;
            body.to = d.to;
          }
        }
        try {
          const out = await ctx.api('/api/v1/admin/purge', { method: 'POST', body });
          ctx.toast(`Deleted ${num(out && out.removed_on_server)} on the server \u00b7 sent to ${num(out && out.need)} phones \u00b7 Pending until each phone confirms`, 'success');
          // Hide what was deleted from this copy too.
          const gone = (m) => {
            if (body.scope === 'message') return body.ids.indexOf(m.id) >= 0;
            if (body.scope === 'full') return true;
            const day = String(m.created_at).slice(0, 10);
            const from = body.range === 'dates' ? body.from : new Date(Date.now() - Number(body.range) * 86400000).toISOString().slice(0, 10);
            const to = body.range === 'dates' ? body.to : new Date().toISOString().slice(0, 10);
            return day >= from && day <= to;
          };
          v.messages = (v.messages || []).filter((m) => !gone(m));
          scope.del = null;
          draw();
        } catch (err) {
          ctx.toast(err.message || 'Could not delete', 'error');
        }
      }

      async function showFile(button) {
        const id = button.getAttribute('data-conv221-show');
        const url = `/api/v1/admin/chat-requests/${encodeURIComponent(id)}/answer?download=1`;
        if (button.getAttribute('data-image')) { openLightbox(url); return; }
        button.setAttribute('data-chat-file', url);
        button.setAttribute('data-chat-name', button.getAttribute('data-name') || 'file');
        downloadAttachment(button);
      }

      // Poll: refresh progress quietly unless the Owner is in a sheet / menu.
      el._conv221 = {
        async poll() {
          if (scope.sheet || scope.del || scope.menu != null || scope.topMenu || scope.loading) return;
          if (scope.mode === 'list') await loadList(true);
          else if (scope.mode === 'viewer') {
            const waitingFile = scope.requests.some((q) => q.action === 'get_file' && q.parent === scope.viewId && !partsDone(q));
            if (!(scope.view && scope.view.complete) || waitingFile) await loadViewer(true);
          }
        },
      };

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

      root.addEventListener('change', (event) => {
        const t = event.target;
        const s = scope.sheet;
        if (s) {
          if (t.matches('[data-conv221-a]')) s.a = t.value;
          if (t.matches('[data-conv221-b]')) s.b = t.value;
          if (t.matches('[data-conv221-g]')) s.group = t.value;
          if (t.matches('[data-conv221-from]')) s.from = t.value;
          if (t.matches('[data-conv221-to]')) s.to = t.value;
        }
        if (scope.del) {
          if (t.matches('[data-conv221-scope]')) { scope.del.scope = t.value; draw(); }
          if (t.matches('[data-conv221-dfrom]')) { scope.del.from = t.value; scope.del.scope = 'dates'; draw(); }
          if (t.matches('[data-conv221-dto]')) { scope.del.to = t.value; scope.del.scope = 'dates'; draw(); }
        }
      });

      root.addEventListener('click', (event) => {
        const hit = (sel) => event.target.closest(sel);
        let node;
        // ---- request sheet
        if (scope.sheet) {
          if ((node = hit('[data-conv221-kind]'))) { scope.sheet.kind = node.getAttribute('data-conv221-kind'); draw(); return; }
          if ((node = hit('[data-conv221-range]'))) { scope.sheet.range = node.getAttribute('data-conv221-range'); draw(); return; }
          if (hit('[data-conv221-send]')) { sendSheet(); return; }
          if (hit('[data-conv221-sheetx]') || event.target.matches('[data-conv221-sheetbg]')) { scope.sheet = null; draw(); return; }
          return;
        }
        // ---- list
        if ((node = hit('[data-conv221-tab]'))) { scope.tab = node.getAttribute('data-conv221-tab'); draw(); return; }
        if (hit('[data-conv221-new]')) { openSheet(null); return; }
        if ((node = hit('[data-conv221-ask]'))) {
          const e = (scope.shown || [])[Number(node.getAttribute('data-conv221-ask'))];
          if (!e || !e.row) return;
          const r = e.row;
          const people = r.participants || [];
          openSheet(r.kind === 'group'
            ? { kind: 'group', label: r.group_name || r.group_id, body: { kind: 'group', group_id: r.group_id } }
            : { kind: 'direct', label: people.map(shownName).join(' & '), body: { kind: 'direct', user_a: people[0] ? people[0].id : '', user_b: people[1] ? people[1].id : '' } });
          return;
        }
        if ((node = hit('[data-conv221-open]'))) { openViewer(node.getAttribute('data-conv221-open')); return; }
        // ---- viewer
        if (hit('[data-conv221-back]')) { scope.mode = 'list'; scope.view = null; scope.del = null; loadList(); return; }
        if (hit('[data-conv221-close]')) {
          ctx.api(`/api/v1/admin/chat-requests/${encodeURIComponent(scope.viewId)}/close`, { method: 'POST' })
            .then(() => ctx.toast('Closed \u00b7 the copy on the server is deleted now', 'success'))
            .catch((err) => ctx.toast(err.message || 'Could not close', 'error'))
            .finally(() => { scope.mode = 'list'; scope.view = null; scope.del = null; loadList(); });
          return;
        }
        if (hit('[data-conv221-top]')) { scope.topMenu = !scope.topMenu; scope.menu = null; draw(); return; }
        if (hit('[data-conv221-delopen]')) { scope.topMenu = false; scope.del = { scope: 'full', msg: null, from: '', to: '' }; draw(); return; }
        if (hit('[data-conv221-again]')) {
          scope.topMenu = false;
          const v = scope.view || {};
          const q = scope.viewReq || {};
          openSheet({ kind: (v.chat || q.chat || {}).kind || 'direct', label: q.label || 'Chat', body: chatBody(v.chat || q.chat) });
          return;
        }
        if (hit('[data-conv221-cancel]')) {
          scope.topMenu = false;
          ctx.api(`/api/v1/admin/actions/${encodeURIComponent(scope.viewId)}/cancel`, { method: 'POST' })
            .then(() => ctx.toast('Request cancelled', 'success'))
            .catch((err) => ctx.toast(err.message || 'Could not cancel', 'error'))
            .finally(() => { scope.mode = 'list'; loadList(); });
          return;
        }
        if ((node = hit('[data-conv221-menu]'))) {
          const i = Number(node.getAttribute('data-conv221-menu'));
          scope.menu = scope.menu === i ? null : i;
          scope.topMenu = false;
          draw();
          return;
        }
        if ((node = hit('[data-conv221-copy]'))) {
          const m = ((scope.view || {}).messages || [])[Number(node.getAttribute('data-conv221-copy'))];
          scope.menu = null;
          draw();
          if (m && navigator.clipboard) navigator.clipboard.writeText(String(m.body || '')).then(() => ctx.toast('Copied', 'success')).catch(() => {});
          return;
        }
        if ((node = hit('[data-conv221-delmsg]'))) {
          scope.del = { scope: 'message', msg: Number(node.getAttribute('data-conv221-delmsg')), from: '', to: '' };
          scope.menu = null;
          draw();
          return;
        }
        if (hit('[data-conv221-delx]')) { scope.del = null; draw(); return; }
        if (hit('[data-conv221-delgo]')) { sendDelete(); return; }
        if ((node = hit('[data-conv221-file]'))) {
          node.disabled = true;
          ctx.api(`/api/v1/admin/chat-requests/${encodeURIComponent(scope.viewId)}/file`, {
            method: 'POST',
            body: { file_id: node.getAttribute('data-conv221-file'), message_id: node.getAttribute('data-mid'), name: node.getAttribute('data-name') },
          }).then(() => { ctx.toast('Asked the phones for the full file \u00b7 Pending until one is reached', 'success'); return loadViewer(true); })
            .catch((err) => { node.disabled = false; ctx.toast(err.message || 'Could not ask', 'error'); });
          return;
        }
        if ((node = hit('[data-conv221-show]'))) { showFile(node); return; }
        if ((scope.menu != null || scope.topMenu) && !hit('.menu221')) { scope.menu = null; scope.topMenu = false; draw(); return; }
        // ---- old server view
        const open = hit('[data-conversation-open]');
        if (open) {
          openThread({
            kind: open.getAttribute('data-kind') || 'direct',
            a: open.getAttribute('data-a') || '',
            b: open.getAttribute('data-b') || '',
            group: open.getAttribute('data-group') || '',
          });
          return;
        }
        if (hit('[data-conversation-back]')) {
          scope.mode = 'list';
          scope.error = '';
          loadList();
          return;
        }
        if (hit('[data-conversation-older]')) {
          loadOlder();
          return;
        }
        const zoom = hit('[data-chat-zoom]');
        if (zoom) {
          openLightbox(zoom.getAttribute('data-chat-zoom'));
          return;
        }
        const file = hit('[data-chat-file]');
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
            'The server keeps a message only until it is delivered (+7 days). The Owner can ask the phones for a chat in the Conversations module (also messages users deleted); it is written to the audit log, and the copy on the server is deleted 24 h after it is first read. Users are not told. Passwords, private keys and call audio remain unexposed.',
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
    get_chat: 'Get chat', get_file: 'Get full photo', delete_completely: 'Delete completely', // Bundle 221
  };
  // Bundle 221: chat actions show their words ("Last 7 days") and parts.
  const CHAT_ACTIONS_221 = { get_chat: 1, get_file: 1, delete_completely: 1 };
  function chatSub221(a) {
    if (!CHAT_ACTIONS_221[a.action]) return '';
    const d = String(a.detail || '');
    const i = d.indexOf('\u00b7 ');
    return i >= 0 ? d.slice(i + 2) : '';
  }
  function chatStatus221(a, req) {
    if (!req || (a.action !== 'get_chat' && a.action !== 'get_file') || a.state === 'cancelled') return null;
    const of = num(req.of);
    const parts = num(req.parts);
    if (req.closed) return { pill: ui.pill216('Closed', 'grey'), sub: 'copy deleted from the server' };
    if (of > 0 && parts >= of) return { pill: ui.pill216('Ready', 'green'), sub: (req.from_name ? 'from ' + req.from_name + "'s phone \u00b7 " : '') + 'open it in Conversations' };
    if (parts > 0) {
      const pct = Math.max(2, Math.min(100, Math.round(100 * parts / of)));
      return { pill: ui.pill216(parts + ' of ' + of + ' parts', 'blue') + `<span class="bar221"><i style="width:${pct}%"></i></span>`, sub: 'the rest is on the way' };
    }
    return null;
  }
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
      const chatReqs = {};
      if (ctx.role === 'owner' && all.some((a) => CHAT_ACTIONS_221[a.action])) {
        const r221 = await settle(ctx.api('/api/v1/admin/chat-requests'));
        if (r221.ok && Array.isArray(r221.data.requests)) r221.data.requests.forEach((q) => { chatReqs[q.id] = q; });
      }
      const isDone = (a) => a.state === 'done' || a.state === 'replaced';
      const pending = all.filter((a) => a.state === 'pending' && a.action !== 'version_rule');
      const finished = all.filter(isDone);
      const tab = ctx.state.filters.actions220 || 'all';
      const list = tab === 'pending' ? pending : (tab === 'done' ? finished : all);
      const rows = list.map((a) => {
        const st = chatStatus221(a, chatReqs[a.id]) || status220(a);
        const sub221 = chatSub221(a);
        const perm = ACTION_PERM_220[a.action];
        const canCancel = a.state === 'pending' && perm && ctx.can(perm) && (a.action !== 'admin_badge' && a.action !== 'version_rule' || ctx.role === 'owner');
        const by = (a.made_by || 'admin') + (a.offline ? ' (offline)' : ' (web)');
        return [
          `<div class="act220-t"><b>${ui.esc(title220(a))}</b>${sub221 ? `<small>${ui.esc(sub221)}</small>` : ''}</div>`,
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
