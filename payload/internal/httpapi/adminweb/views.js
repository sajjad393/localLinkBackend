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
    subtitle: 'Operational overview',
    icon: 'i-dashboard',
    group: 'Operations',
    async mount(el, ctx) {
      const range = ctx.state.analyticsRange || '24h';
      const longRange = !!LONG_RANGES[range];

      el.innerHTML = `<div class="section">${ui.skeletonCards(8)}</div>`;

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

      // ---- header
      parts.push(pollRegion('header', `<div class="section-head">
        <div class="grow">
          <h1 class="type-page-title">Overview</h1>
          <span class="sub">${analytics.ok && analytics.data.generated_at
            ? 'Updated ' + ui.esc(fmt.relative(analytics.data.generated_at)) + ' · '
            : ''}Operational metadata from the LocalLink server.</span>
        </div>
        <div class="section-actions">
          ${ui.badge('Range: ' + ui.esc(range), 'primary')}
          ${ui.statusBadge(overall, { label: 'Overall: ' + ui.statusInfo(overall).label })}
        </div>
      </div>`))

      // ---- metric area
      const metricCards = [];
      if (summaryData) {
        metricCards.push(ui.metricCard({
          label: 'Total users', value: fmt.number(summaryData.users), icon: 'i-users',
          context: `+${fmt.number(num(totals.users))} ${windowLabel}`, sparkline: sparkValues(series.users),
        }));
        metricCards.push(ui.metricCard({
          label: 'Online devices', value: fmt.number(summaryData.online_devices), icon: 'i-wifi',
          context: `of ${fmt.number(summaryData.devices)} registered`,
        }));
        metricCards.push(ui.metricCard({
          label: 'Registered devices', value: fmt.number(summaryData.devices), icon: 'i-devices',
          context: `+${fmt.number(num(totals.devices))} ${windowLabel}`, sparkline: sparkValues(series.devices),
        }));
        metricCards.push(ui.metricCard({
          label: 'Messages', value: fmt.number(summaryData.messages), icon: 'i-activity',
          context: `+${fmt.number(num(totals.messages))} ${windowLabel}`, sparkline: sparkValues(series.messages),
        }));
        metricCards.push(ui.metricCard({
          label: 'Active calls', value: fmt.number(summaryData.active_calls), icon: 'i-call',
          context: `${fmt.number(num(totals.calls))} started ${windowLabel}`,
        }));
        metricCards.push(ui.metricCard({
          label: 'Active sessions', value: fmt.number(summaryData.active_sessions), icon: 'i-key',
          context: 'Authenticated device sessions',
        }));
        metricCards.push(ui.metricCard({
          label: 'Storage used', value: fmt.bytes(summaryData.file_storage_bytes), icon: 'i-database',
          context: `Database ${fmt.bytes(summaryData.db_bytes)}`,
        }));
        metricCards.push(ui.metricCard({
          label: 'Security alerts', value: fmt.number(summaryData.security_alerts), icon: 'i-shield',
          accent: num(summaryData.security_alerts) > 0 ? 'warning' : 'success',
          context: 'Events in the last 24 hours', sparkline: sparkValues(series.security_events),
        }));
      } else {
        metricCards.push(`<div class="state error" style="grid-column:1/-1">
          ${ui.icon('i-alert')}<strong>Metrics unavailable</strong>
          <p>${ui.esc(summary.error || 'The summary endpoint did not respond.')}</p>
          <button class="btn btn-sm" data-retry="dashboard">${ui.icon('i-refresh')}Retry</button></div>`);
      }
      parts.push(pollRegion('metrics', `<div class="grid grid-metrics">${metricCards.join('')}</div>`));

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
        ? chartCanvas({ type: 'line', options: { points: messagePoints, color: ui.token('primary', '#1e5bff'), height: 220, xLabelCount: 6, longRange } }, 220)
        : ui.emptyState({ icon: 'i-activity', title: 'No messages in this window', message: 'The server accepted no messages during the selected period.' });
      const byteChart = !runtimeAvailable
        ? ui.emptyState({ icon: 'i-activity', title: 'Traffic metrics unavailable', message: 'This server build does not expose the runtime sampler.' })
        : (bytePoints.length && !allZero(bytePoints)
          ? chartCanvas({ type: 'line', options: { points: bytePoints, color: ui.token('info', '#0b5fa5'), height: 200, xLabelCount: 6, longRange } }, 200)
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

  // ---------------------------------------------------------------------- users
  const users = {
    id: 'users',
    title: 'Users',
    subtitle: 'Accounts and profiles',
    icon: 'i-users',
    group: 'Operations',
      // Sidebar count from the server summary.
      navCount: 'users',
    async mount(el, ctx) {
      const query = ctx.state.filters.users || { search: '', status: '' };
      el.innerHTML = ui.skeletonPanel(6);
      const result = await settle(ctx.api('/api/v1/admin/users?limit=200'));
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Users', body: ui.errorState(result.error, 'users') }); return; }

      const all = Array.isArray(result.data.users) ? result.data.users : [];
      const search = (query.search || '').toLowerCase();
      const filtered = all.filter((u) => {
        if (search && String(u.username || '').toLowerCase().indexOf(search) < 0) return false;
        if (query.status && u.status !== query.status) return false;
        return true;
      });

      const rows = filtered.map((u) => [
        `<div><span class="cell-primary">${emptyText(u.username)}</span>
           <div class="mono subtle">${emptyText(u.id)}</div></div>`,
        ui.statusBadge(u.status),
        `<span>${fmt.number(u.devices)}</span>`,
        `<span>${fmt.number(u.online_devices)}</span>`,
        `<span class="subtle">${ui.esc(fmt.dateTime(u.created_at))}</span>`,
        tableActions([
          ctx.can('manageUsers')
            ? `<select class="input" data-user-status="${ui.esc(u.id)}" aria-label="Account status">
                 ${['active', 'suspended', 'disabled'].map((s) =>
                   `<option value="${s}" ${u.status === s ? 'selected' : ''}>${s}</option>`).join('')}
               </select>`
            : `<span class="subtle">Owner only</span>`,
          `<button class="btn btn-sm" data-user-devices="${ui.esc(u.id)}">Devices</button>`,
        ]),
      ]);

      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({ title: 'Users', subtitle: 'Account state, device counts and administrative actions', icon: 'i-users', actions: ui.badge(`${all.length} accounts`, 'neutral') })}
          ${ui.panel({
            pad0: true,
            body: `<div style="padding:16px 16px 0">${ui.filterBar({
              search: true,
              searchPlaceholder: 'Search username…',
              searchValue: query.search || '',
              selects: [{
                key: 'status', label: 'Status', value: query.status || '',
                options: [
                  { value: '', label: 'All statuses' },
                  { value: 'active', label: 'Active' },
                  { value: 'suspended', label: 'Suspended' },
                  { value: 'disabled', label: 'Disabled' },
                ],
              }],
            })}</div>
            <div style="padding:16px">${ui.table({
              columns: [
                { label: 'Username' }, { label: 'Status' }, { label: 'Devices', numeric: true },
                { label: 'Online', numeric: true }, { label: 'Created' }, { label: 'Actions' },
              ],
              rows,
              emptyTitle: 'No accounts match',
              emptyMessage: 'Adjust the search or status filter to see accounts.',
            })}</div>`,
          })}
        </div>`;
    },
  };

  // -------------------------------------------------------------------- devices
  const devices = {
    id: 'devices',
    title: 'Devices',
    subtitle: 'Registered hardware',
    icon: 'i-devices',
    group: 'Operations',
      // Sidebar count from the server summary.
      navCount: 'devices',
    async mount(el, ctx) {
      const query = ctx.state.filters.devices || { search: '', presence: '' };
      el.innerHTML = ui.skeletonPanel(6);
      const result = await settle(ctx.api('/api/v1/admin/devices?limit=200'));
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Devices', body: ui.errorState(result.error, 'devices') }); return; }

      const all = Array.isArray(result.data.devices) ? result.data.devices : [];
      const search = (query.search || '').toLowerCase();
      const filtered = all.filter((d) => {
        const haystack = `${d.name || ''} ${d.username || ''} ${d.id || ''}`.toLowerCase();
        if (search && haystack.indexOf(search) < 0) return false;
        if (query.presence === 'online' && !d.online) return false;
        if (query.presence === 'offline' && d.online) return false;
        return true;
      });

      const rows = filtered.map((d) => [
        `<div><span class="cell-primary">${emptyText(d.name)}</span>
           <div class="mono subtle">${emptyText(d.id)}</div></div>`,
        emptyText(d.username),
        emptyText(d.platform),
        ui.statusBadge(d.status),
        ui.badge(d.online ? 'online' : 'offline', d.online ? 'success' : 'neutral', { dot: true }),
        `<span class="subtle">${ui.esc(fmt.dateTime(d.created_at))}</span>`,
        tableActions([
          ctx.can('manageDevices') ? `<button class="btn btn-sm" data-device-rename="${ui.esc(d.id)}" data-device-name="${ui.esc(d.name)}">Rename</button>` : '',
          ctx.can('manageDevices') ? `<button class="btn btn-sm" data-device-policy="${ui.esc(d.id)}">Policy</button>` : '',
          ctx.can('manageDevices')
            ? `<button class="btn btn-sm btn-danger" data-device-revoke="${ui.esc(d.id)}" ${d.status === 'revoked' ? 'disabled' : ''}>Revoke</button>`
            : '',
        ]),
      ]);

      const online = all.filter((d) => d.online).length;
      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({
            title: 'Devices', subtitle: 'Presence, platform and transport policy for every registered device',
            icon: 'i-devices',
            actions: `${ui.badge(`${all.length} registered`, 'neutral')}${ui.badge(`${online} online`, 'success', { dot: true })}`,
          })}
          ${ui.panel({
            pad0: true,
            body: `<div style="padding:16px 16px 0">${ui.filterBar({
              search: true,
              searchPlaceholder: 'Search device, user or id…',
              searchValue: query.search || '',
              selects: [{
                key: 'presence', label: 'Presence', value: query.presence || '',
                options: [
                  { value: '', label: 'Any presence' },
                  { value: 'online', label: 'Online' },
                  { value: 'offline', label: 'Offline' },
                ],
              }],
            })}</div>
            <div style="padding:16px">${ui.table({
              columns: [
                { label: 'Device' }, { label: 'User' }, { label: 'Platform' }, { label: 'State' },
                { label: 'Presence' }, { label: 'Registered' }, { label: 'Actions' },
              ],
              rows,
              emptyTitle: 'No devices match',
              emptyMessage: 'Adjust the filters to see registered devices.',
            })}</div>`,
          })}
        </div>`;
    },
  };

  // ------------------------------------------------------------------- sessions
  const sessions = {
    id: 'sessions',
    title: 'Sessions',
    subtitle: 'Authenticated sessions',
    icon: 'i-key',
    group: 'Operations',
      // Sidebar count from the server summary.
      navCount: 'active_sessions',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const [userSessions, adminSessions] = await Promise.all([
        settle(ctx.api('/api/v1/admin/sessions?limit=200')),
        settle(ctx.api('/api/v1/admin/admin-sessions')),
      ]);

      const body = (result, id) => {
        if (!result.ok) return ui.errorState(result.error, 'sessions');
        const list = Array.isArray(result.data.sessions) ? result.data.sessions : [];
        return ui.table({
          columns: [
            { label: 'User' }, { label: 'Device' }, { label: 'Platform' }, { label: 'Last seen' },
            { label: 'Expires' }, { label: 'State' }, { label: 'Actions' },
          ],
          rows: list.map((s) => [
            emptyText(s.username || s.admin_username),
            emptyText(s.device_name),
            emptyText(s.platform),
            `<span class="subtle">${ui.esc(fmt.dateTime(s.last_seen_at))}</span>`,
            `<span class="subtle">${ui.esc(fmt.dateTime(s.expires_at))}</span>`,
            ui.badge(s.active ? 'active' : 'inactive', s.active ? 'success' : 'neutral', { dot: true }),
            tableActions([
              ctx.can('manageSessions')
                ? `<button class="btn btn-sm ${s.active ? 'btn-danger' : ''}" data-session-revoke="${ui.esc(s.id)}" data-session-kind="${id}" ${s.active ? '' : 'disabled'}>Revoke</button>`
                : '<span class="subtle">Owner only</span>',
            ]),
          ]),
          emptyTitle: 'No sessions',
          emptyMessage: 'No authenticated session is recorded for this view.',
        });
      };

      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({ title: 'Sessions', subtitle: 'Device sessions and administrator sessions currently known to the server', icon: 'i-key' })}
          <div class="split-1-1">
            ${ui.panel({ title: 'User sessions', body: body(userSessions, 'user') })}
            ${ui.panel({ title: 'Administrator sessions', subtitle: 'Your admin console sessions', body: body(adminSessions, 'admin') })}
          </div>
        </div>`;
    },
  };

  // -------------------------------------------------------------------- network
  const network = {
    id: 'network',
    title: 'Network & Mesh',
    subtitle: 'Topology and transports',
    icon: 'i-hub',
    group: 'Connectivity',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const [overview, deviceList, policy] = await Promise.all([
        settle(ctx.api('/api/v1/admin/network')),
        settle(ctx.api('/api/v1/admin/devices?limit=200')),
        settle(ctx.api('/api/v1/admin/privacy')),
      ]);

      const data = overview.ok ? overview.data : null;
      const devices = deviceList.ok && Array.isArray(deviceList.data.devices) ? deviceList.data.devices : [];
      const online = devices.filter((d) => d.online);
      const transports = data ? [
        { label: 'Server connected', value: num(data.server_connected), tone: 'success' },
        { label: 'LAN available', value: num(data.lan_available), tone: 'primary' },
        { label: 'Wi-Fi Direct', value: num(data.wifi_direct_connected), tone: 'info' },
        { label: 'Mesh capable', value: num(data.mesh_available), tone: 'warning' },
      ] : [];
      const maxTransport = Math.max(1, ...transports.map((t) => t.value));
      const transportBars = transports.map((t) => `
        <div class="health-row">
          <div class="health-main">
            <span class="label">${ui.esc(t.label)}</span>
            <span class="reading">${fmt.number(t.value)}</span>
          </div>
          <div class="meter"><span style="width:${((t.value / maxTransport) * 100).toFixed(1)}%;background:var(--${t.tone === 'primary' ? 'primary' : t.tone})"></span></div>
        </div>`).join('');

      const platformCounts = {};
      devices.forEach((d) => { const key = d.platform || 'unknown'; platformCounts[key] = (platformCounts[key] || 0) + 1; });
      const platformPoints = Object.keys(platformCounts).sort().map((k) => ({ label: k, value: platformCounts[k] }));

      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({
            title: 'Network & mesh', subtitle: 'Live transport coverage reported by devices',
            icon: 'i-hub',
            actions: `<button class="btn btn-sm" data-topology="1">${ui.icon('i-hub')}Open network topology</button>`,
          })}
          <div class="grid grid-3">
            ${ui.metricCard({ label: 'Devices online', value: fmt.number(data ? data.online_devices : online.length), icon: 'i-wifi', context: `${devices.length} registered` })}
            ${ui.metricCard({ label: 'Server links', value: fmt.number(data ? data.server_connected : 0), icon: 'i-server', context: 'Devices with a live server path' })}
            ${ui.metricCard({ label: 'Mesh capable', value: fmt.number(data ? data.mesh_available : 0), icon: 'i-hub', context: 'Peers able to relay for others' })}
            ${ui.metricCard({ label: 'Wi-Fi Direct', value: fmt.number(data ? data.wifi_direct_connected : 0), icon: 'i-transfer', context: 'Direct peer links' })}
            ${ui.metricCard({ label: 'Pending transfers', value: fmt.number(data ? data.pending_transfers : 0), icon: 'i-activity', context: 'Files waiting to complete' })}
            ${ui.metricCard({ label: 'Active calls', value: fmt.number(data ? data.active_calls : 0), icon: 'i-call', context: 'Calls in progress' })}
          </div>
          <div class="split-3-2">
            ${ui.panel({
              title: 'Transport coverage', subtitle: 'How connected devices currently reach each other',
              body: overview.ok ? transportBars : ui.errorState(overview.error, 'network'),
            })}
            ${ui.panel({
              title: 'Platforms in use', subtitle: 'Registered devices by platform',
              body: deviceList.ok
                ? (platformPoints.length
                  ? chartCanvas({ type: 'bar', options: { points: platformPoints, color: ui.token('primary', '#1e5bff'), height: 200 } }, 200)
                  : ui.emptyState({ icon: 'i-devices', title: 'No devices registered', message: 'Platform breakdown appears once devices register.' }))
                : ui.errorState(deviceList.error, 'network'),
            })}
          </div>
          ${ui.panel({
            title: 'Online devices', subtitle: 'Devices with an active server session',
            body: deviceList.ok
              ? ui.table({
                columns: [{ label: 'Device' }, { label: 'User' }, { label: 'Platform' }],
                rows: online.map((d) => [
                  `<div><span class="cell-primary">${emptyText(d.name)}</span><div class="mono subtle">${emptyText(d.id)}</div></div>`,
                  emptyText(d.username), emptyText(d.platform),
                ]),
                emptyTitle: 'No device is online',
                emptyMessage: 'Devices appear here while they hold an active server session.',
              })
              : ui.errorState(deviceList.error, 'network'),
          })}
          ${ui.panel({
            title: 'Transport policy reference', subtitle: 'Rules the server applies to client transports',
            body: policy.ok
              ? `<pre class="mono" style="white-space:pre-wrap;margin:0;color:var(--text-secondary)">${ui.esc(JSON.stringify(policy.data, null, 2))}</pre>`
              : ui.errorState(policy.error, 'network'),
          })}
        </div>`;
    },
  };

  // ---------------------------------------------------------------------- calls
  const calls = {
    id: 'calls',
    title: 'Calls',
    subtitle: 'Call sessions',
    icon: 'i-call',
    group: 'Connectivity',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const result = await settle(ctx.api('/api/v1/admin/calls?limit=200'));
      const list = result.ok && Array.isArray(result.data.calls) ? result.data.calls : [];
      const active = list.filter((c) => !c.ended_at);
      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({
            title: 'Calls', subtitle: 'Every call the server brokered, newest first', icon: 'i-call',
            actions: `${ui.badge(`${list.length} recorded`, 'neutral')}${ui.badge(`${active.length} in progress`, active.length ? 'info' : 'neutral', { dot: true })}`,
          })}
          ${ui.panel({
            pad0: true,
            body: `<div style="padding:16px">${!result.ok
              ? ui.errorState(result.error, 'calls')
              : ui.table({
                columns: [
                  { label: 'Started' }, { label: 'Caller' }, { label: 'Callee' }, { label: 'State' },
                  { label: 'Duration' }, { label: 'Transport' },
                ],
                rows: list.map((c) => {
                  const started = c.started_at ? new Date(c.started_at) : null;
                  const ended = c.ended_at ? new Date(c.ended_at) : null;
                  const seconds = started && ended ? (ended - started) / 1000 : null;
                  return [
                    `<span class="subtle">${ui.esc(fmt.dateTime(c.started_at))}</span>`,
                    emptyText(c.caller || c.caller_id),
                    emptyText(c.callee || c.callee_id),
                    c.ended_at ? ui.badge('ended', 'neutral', { dot: true }) : ui.badge('in progress', 'info', { dot: true }),
                    seconds === null ? '<span class="subtle">—</span>' : ui.esc(fmt.duration(seconds)),
                    emptyText(c.transport || c.mode),
                  ];
                }),
                emptyTitle: 'No calls recorded',
                emptyMessage: 'Calls appear here once the server brokers them.',
              })}</div>`,
          })}
        </div>`;
    },
  };

  // ------------------------------------------------------------------ transfers
  const transfers = {
    id: 'transfers',
    title: 'File Transfers',
    subtitle: 'Transfers and quota',
    icon: 'i-transfer',
    group: 'Connectivity',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const result = await settle(ctx.api('/api/v1/admin/transfers?limit=200'));
      const list = result.ok && Array.isArray(result.data.transfers) ? result.data.transfers : [];
      const pending = list.filter((t) => String(t.status || '').toLowerCase() !== 'complete');
      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({
            title: 'File transfers', subtitle: 'Transfer records and completion state', icon: 'i-transfer',
            actions: `${ui.badge(`${list.length} records`, 'neutral')}${ui.badge(`${pending.length} pending`, pending.length ? 'warning' : 'neutral')}`,
          })}
          ${ui.panel({
            pad0: true,
            body: `<div style="padding:16px">${!result.ok
              ? ui.errorState(result.error, 'transfers')
              : ui.table({
                columns: [
                  { label: 'Created' }, { label: 'File' }, { label: 'Size', numeric: true },
                  { label: 'From' }, { label: 'To' }, { label: 'Status' },
                ],
                rows: list.map((t) => [
                  `<span class="subtle">${ui.esc(fmt.dateTime(t.created_at))}</span>`,
                  emptyText(t.filename || t.name),
                  ui.esc(fmt.bytes(t.size_bytes || t.bytes)),
                  emptyText(t.sender || t.from_device),
                  emptyText(t.recipient || t.to_device),
                  ui.statusBadge(t.status),
                ]),
                emptyTitle: 'No transfers recorded',
                emptyMessage: 'Transfers appear here as devices exchange files.',
              })}</div>`,
          })}
        </div>`;
    },
  };

  // ------------------------------------------------------------------- security
  const security = {
    id: 'security',
    title: 'Security Center',
    subtitle: 'Alerts and policy',
    icon: 'i-shield',
    group: 'Trust & Compliance',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(6);
      const [summary, events] = await Promise.all([
        settle(ctx.api('/api/v1/admin/security-summary')),
        settle(ctx.api('/api/v1/admin/security-events?limit=200')),
      ]);

      const counts = summary.ok ? summary.data.counts || {} : {};
      const recent = summary.ok && Array.isArray(summary.data.recent_events) ? summary.data.recent_events : [];
      const adminActions = summary.ok && Array.isArray(summary.data.recent_admin_actions) ? summary.data.recent_admin_actions : [];
      const eventList = events.ok && Array.isArray(events.data.security_events) ? events.data.security_events : [];
      const critical = num(counts.critical_alerts_24h);
      const failed = num(counts.failed_logins_24h);

      const head = critical > 0
        ? ui.headline('danger', `${critical} critical alert${critical === 1 ? '' : 's'} in the last 24 hours`, 'i-alert')
        : (failed > 0
          ? ui.headline('warning', `${failed} failed sign-in attempt${failed === 1 ? '' : 's'} in the last 24 hours`, 'i-alert')
          : ui.headline('success', 'No critical security alerts in the last 24 hours', 'i-check'));

      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({ title: 'Security center', subtitle: 'Audited security events, account protection and administrator activity', icon: 'i-shield' })}
          ${head}
          <div class="grid grid-metrics">
            ${ui.metricCard({ label: 'Critical alerts 24h', value: fmt.number(counts.critical_alerts_24h), icon: 'i-alert', accent: critical ? 'danger' : 'success', context: 'Revocations and lockouts' })}
            ${ui.metricCard({ label: 'Failed logins 24h', value: fmt.number(counts.failed_logins_24h), icon: 'i-lock', accent: failed ? 'warning' : 'success', context: 'Rejected sign-in attempts' })}
            ${ui.metricCard({ label: 'Locked users', value: fmt.number(counts.locked_users), icon: 'i-ban', context: 'Accounts under lockout' })}
            ${ui.metricCard({ label: 'Users with failures', value: fmt.number(counts.users_with_failures), icon: 'i-users', context: 'Accounts with at least one failure' })}
            ${ui.metricCard({ label: 'Revoked devices', value: fmt.number(counts.revoked_devices), icon: 'i-devices', context: 'Devices no longer trusted' })}
            ${ui.metricCard({ label: 'Active sessions', value: fmt.number(counts.active_sessions), icon: 'i-key', context: 'Authenticated device sessions' })}
            ${ui.metricCard({ label: 'Admin sessions', value: fmt.number(counts.admin_sessions), icon: 'i-shield', context: 'Signed-in administrators' })}
            ${ui.metricCard({ label: 'Pending recoveries', value: fmt.number(counts.pending_recoveries), icon: 'i-restore', context: 'Recovery requests awaiting review' })}
          </div>
          <div class="split-3-2">
            ${ui.panel({
              title: 'Security events', subtitle: 'Newest first, straight from the security audit table',
              body: events.ok
                ? ui.table({
                  columns: [{ label: 'Time' }, { label: 'Action' }, { label: 'Severity' }, { label: 'User' }, { label: 'Device' }, { label: 'Detail' }],
                  rows: eventList.map((e) => [
                    `<span class="subtle nowrap">${ui.esc(fmt.dateTime(e.created_at))}</span>`,
                    `<span class="cell-primary">${ui.esc(fmt.titleCase(e.action))}</span>`,
                    ui.severityBadge(e.severity),
                    `<span class="mono subtle">${emptyText(e.user_id)}</span>`,
                    `<span class="mono subtle">${emptyText(e.device_id)}</span>`,
                    emptyText(e.detail),
                  ]),
                  emptyTitle: 'No security events',
                  emptyMessage: 'The security audit table has no rows yet.',
                })
                : ui.errorState(events.error, 'security'),
            })}
            ${ui.panel({
              title: 'Recent administrator activity', subtitle: 'Administrative actions recorded in the audit trail',
              body: summary.ok
                ? ui.timeline(adminActions.map((a) => ({
                  title: fmt.titleCase(a.action), detail: a.detail, timestamp: a.created_at,
                  severity: a.severity, icon: 'i-audit',
                })), { max: 10, emptyMessage: 'No administrator action has been recorded yet.' })
                : ui.errorState(summary.error, 'security'),
            })}
          </div>
          ${ui.panel({
            title: 'Latest security events', subtitle: 'Condensed feed of the eight most recent entries',
            body: summary.ok
              ? (recent.length
                ? `<div style="display:grid;gap:8px">${recent.slice(0, 8).map((e) => ui.notificationItem({
                  icon: ui.severityIcon(e.severity), title: fmt.titleCase(e.action), detail: e.detail,
                  timestamp: e.created_at, severity: e.severity,
                })).join('')}</div>`
                : ui.emptyState({ icon: 'i-shield', title: 'Nothing recorded', message: 'No security event has been recorded yet.' }))
              : ui.errorState(summary.error, 'security'),
          })}
        </div>`;
    },
  };

  // ---------------------------------------------------------------------- audit
  const audit = {
    id: 'audit',
    title: 'Audit Log',
    subtitle: 'Admin actions',
    icon: 'i-audit',
    group: 'Trust & Compliance',
    async mount(el, ctx) {
      const query = ctx.state.filters.audit || { search: '' };
      el.innerHTML = ui.skeletonPanel(6);
      const result = await settle(ctx.api('/api/v1/admin/logs?limit=200'));
      const list = result.ok && Array.isArray(result.data.logs) ? result.data.logs : [];
      const search = (query.search || '').toLowerCase();
      const filtered = list.filter((entry) => {
        if (!search) return true;
        const haystack = `${entry.action || ''} ${entry.admin_id || ''} ${entry.target_id || ''} ${entry.detail || ''}`.toLowerCase();
        return haystack.indexOf(search) >= 0;
      });

      const actionCounts = {};
      list.forEach((entry) => { const key = entry.action || 'unknown'; actionCounts[key] = (actionCounts[key] || 0) + 1; });
      const topActions = Object.keys(actionCounts)
        .map((k) => ({ label: fmt.titleCase(k), value: actionCounts[k] }))
        .sort((a, b) => b.value - a.value)
        .slice(0, 6);

      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({
            title: 'Audit log', subtitle: 'Every administrative action, newest first', icon: 'i-audit',
            actions: ui.badge(`${list.length} entries`, 'neutral'),
          })}
          ${topActions.length ? ui.panel({
            title: 'Most frequent actions', subtitle: 'Based on the entries currently loaded',
            body: chartCanvas({ type: 'bar', options: { points: topActions, color: ui.token('primary', '#1e5bff'), height: 180 } }, 180),
          }) : ''}
          ${ui.panel({
            pad0: true,
            body: `<div style="padding:16px 16px 0">${ui.filterBar({
              search: true, searchPlaceholder: 'Search action, admin, target…', searchValue: query.search || '',
            })}</div>
            <div style="padding:16px">${!result.ok
              ? ui.errorState(result.error, 'audit')
              : ui.table({
                columns: [{ label: 'Time' }, { label: 'Administrator' }, { label: 'Action' }, { label: 'Target' }, { label: 'Detail' }],
                rows: filtered.map((entry) => [
                  `<span class="subtle nowrap">${ui.esc(fmt.dateTime(entry.created_at))}</span>`,
                  `<span class="mono">${emptyText(entry.admin_id)}</span>`,
                  `<span class="cell-primary">${ui.esc(fmt.titleCase(entry.action))}</span>`,
                  `<span class="mono subtle">${emptyText(entry.target_id)}</span>`,
                  emptyText(entry.detail),
                ]),
                emptyTitle: 'No audit entries match',
                emptyMessage: 'Clear the search to see every recorded action.',
              })}</div>`,
          })}
        </div>`;
    },
  };

  // ------------------------------------------------------------------- recovery
  const recovery = {
    id: 'recovery',
    title: 'Recovery',
    subtitle: 'Account recovery queue',
    icon: 'i-restore',
    group: 'Trust & Compliance',
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const result = await settle(ctx.api('/api/v1/admin/recovery-requests?limit=200'));
      const list = result.ok && Array.isArray(result.data.requests || result.data.recovery_requests)
        ? (result.data.requests || result.data.recovery_requests)
        : [];
      const pending = list.filter((r) => String(r.status || '').toLowerCase() === 'pending');

      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({
            title: 'Recovery queue', subtitle: 'Account recovery requests and their review state', icon: 'i-restore',
            actions: `${ui.badge(`${pending.length} pending`, pending.length ? 'warning' : 'neutral')}${ui.badge(`${list.length} total`, 'neutral')}`,
          })}
          ${ui.panel({
            pad0: true,
            body: `<div style="padding:16px">${!result.ok
              ? ui.errorState(result.error, 'recovery')
              : ui.table({
                columns: [{ label: 'Requested' }, { label: 'User' }, { label: 'Device' }, { label: 'Reason' }, { label: 'State' }, { label: 'Actions' }],
                rows: list.map((r) => [
                  `<span class="subtle nowrap">${ui.esc(fmt.dateTime(r.created_at || r.requested_at))}</span>`,
                  `<span class="cell-primary">${emptyText(r.username || r.user_id)}</span>`,
                  emptyText(r.device_name || r.device_id),
                  emptyText(r.reason),
                  ui.statusBadge(r.status),
                  tableActions([
                    ctx.can('manageRecovery') && String(r.status || '').toLowerCase() === 'pending'
                      ? `<button class="btn btn-sm btn-primary" data-recovery="approve" data-id="${ui.esc(r.id)}">Approve</button>
                         <button class="btn btn-sm" data-recovery="reissue" data-id="${ui.esc(r.id)}">Reissue</button>
                         <button class="btn btn-sm btn-danger" data-recovery="reject" data-id="${ui.esc(r.id)}">Reject</button>`
                      : '<span class="subtle">—</span>',
                  ]),
                ]),
                emptyTitle: 'No recovery requests',
                emptyMessage: 'Requests appear here when a user starts account recovery.',
              })}</div>`,
          })}
          ${ui.note('Recovery approvals are audited',
            'Approving, reissuing or rejecting a recovery request writes an entry to the security audit trail and is visible in the audit log.',
            'i-shield')}
        </div>`;
    },
  };

  // ---------------------------------------------------------------- diagnostics
  const diagnostics = {
    id: 'diagnostics',
    title: 'Diagnostics',
    subtitle: 'Live subsystem health',
    icon: 'i-pulse',
    group: 'Platform',
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
    title: 'Admin Settings',
    subtitle: 'Server configuration',
    icon: 'i-settings',
    group: 'Platform',
    roles: ['owner'],
    async mount(el, ctx) {
      el.innerHTML = ui.skeletonPanel(5);
      const result = await settle(ctx.api('/api/v1/admin/settings'));
      if (!result.ok) { el.innerHTML = ui.panel({ title: 'Admin settings', body: ui.errorState(result.error, 'settings') }); return; }
      const data = result.data || {};

      el.innerHTML = `
        <div class="section">
          ${ui.sectionHead({ title: 'Admin settings', subtitle: 'Values the dashboard and alerting read from the server', icon: 'i-settings' })}
          ${ui.panel({
            title: 'Operational settings', subtitle: 'Changes are stored server-side and audited',
            body: `<form class="stack gap-lg" data-settings-form style="max-width:520px">
              <div class="field">
                <label for="setting-refresh">Dashboard refresh interval (seconds)</label>
                <input class="input" id="setting-refresh" name="dashboard_refresh_seconds" type="number" min="5" max="120" value="${ui.esc(data.dashboard_refresh_seconds)}">
                <span class="type-caption">Clamped to 5–120 seconds in the console.</span>
              </div>
              <div class="field">
                <label for="setting-threshold">Security alert threshold</label>
                <input class="input" id="setting-threshold" name="security_alert_threshold" type="number" min="0" value="${ui.esc(data.security_alert_threshold)}">
                <span class="type-caption">Alerts raised before the security card escalates.</span>
              </div>
              <div class="row gap-md">
                <button class="btn btn-primary" type="submit">Save settings</button>
                <span class="type-caption" data-settings-note></span>
              </div>
            </form>`,
          })}
          <div class="split-1-1">
            ${ui.panel({
              title: 'Server posture', subtitle: 'Read-only facts reported by the server',
              body: ui.statRow([
                { label: 'Read-only server config', value: data.read_only_server_config ? 'Yes' : 'No' },
                { label: 'Backup available', value: data.backup_available ? 'Yes' : 'No' },
                { label: 'Refresh interval', value: `${ui.esc(data.dashboard_refresh_seconds)}s` },
                { label: 'Alert threshold', value: ui.esc(data.security_alert_threshold) },
              ]),
            })}
            ${ui.panel({
              title: 'Database backup', subtitle: 'Writes a consistent copy of the database on the server',
              body: `<div class="stack gap-md">
                <p class="type-caption">The backup is created by the server and stored in its backup directory. The action is recorded in the audit trail.</p>
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
    subtitle: 'Message review',
    icon: 'i-message',
    group: 'Trust & Compliance',
    // Owner only. Reading what users said to each other is the most sensitive
    // action in this console, so the server refuses every other role and writes
    // an audit entry each time a conversation is opened.
    roles: ['owner'],
    async mount(el, ctx) {
      const scope = {
        mode: 'list', kind: '', a: '', b: '', groupId: '', groupName: '', memberCount: 0,
        participants: [], messages: [], cursor: '', hasMore: false, rows: [], error: '', loading: false,
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

      // One row per message: who wrote it, when, what they wrote, and how the
      // server handled it. Bodies are escaped like every other server value in
      // this console, and line breaks survive because CSS preserves them.
      function messageList(messages) {
        return `<div class="msg-thread">${messages.map((m) => `
          <div class="msg-row">
            <div class="msg-head">
              <strong>${ui.esc(m.sender_name || m.sender_id)}</strong>
              ${m.sender_username ? `<span class="msg-user">${ui.esc(m.sender_username)}</span>` : ''}
              <span class="msg-time">${ui.esc(fmt.dateTime(m.created_at))}</span>
            </div>
            <div class="msg-body">${ui.esc(m.body)}</div>
            <div class="msg-meta">
              ${ui.statusBadge(m.status || 'sent')}
              ${m.delivered_at ? `<span class="subtle">delivered ${ui.esc(fmt.dateTime(m.delivered_at))}</span>` : ''}
            </div>
          </div>`).join('')}</div>`;
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
          ? messageList(ordered)
            + (scope.hasMore
              ? `<div style="margin-top:12px"><button class="btn btn-ghost btn-sm" data-conversation-older>${ui.icon('i-chevron-down')}Load older messages</button></div>`
              : '')
            + `<div class="type-caption" style="margin-top:12px">Showing ${ui.esc(fmt.number(ordered.length))} message(s), newest first. This read was recorded in the audit log.</div>`
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
        }
      });

      await loadList();
    },
  };

  // -------------------------------------------------------------------- privacy
  const privacy = {
    id: 'privacy',
    title: 'Privacy & Access',
    subtitle: 'Data boundaries',
    icon: 'i-privacy',
    group: 'Platform',
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

  LL.views = { dashboard, users, devices, sessions, network, calls, transfers, security, audit, recovery, diagnostics, settings, privacy, conversations };

  // Modules that are restricted to specific roles (used by quick actions too).
  LL.moduleRoles = { settings: ['owner'], conversations: ['owner'] };

})();
