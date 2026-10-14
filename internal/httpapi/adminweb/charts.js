/* ===========================================================================
   LocalLink admin console — canvas charts.

   The same primitives the Flutter admin surface uses (line/area, bar, donut,
   gauge, sparkline), drawn on canvas with device-pixel-ratio scaling. Colours
   are read from the live CSS custom properties so charts follow the theme.
   Animation is skipped when the operator prefers reduced motion.
   =========================================================================== */
window.LL = window.LL || {};

(function () {
  'use strict';

  const registry = [];

  function reducedMotion() {
    try {
      return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    } catch (err) {
      return false;
    }
  }

  function prepare(canvas, cssHeight) {
    if (!canvas || !canvas.getContext) return null;
    const dpr = window.devicePixelRatio || 1;
    const width = canvas.parentElement ? canvas.parentElement.clientWidth : canvas.clientWidth;
    if (!width) return null;
    const height = cssHeight || canvas.getAttribute('data-height') || 200;
    canvas.width = Math.max(1, Math.floor(width * dpr));
    canvas.height = Math.max(1, Math.floor(height * dpr));
    canvas.style.width = width + 'px';
    canvas.style.height = height + 'px';
    canvas.setAttribute('data-height', String(height));
    const ctx = canvas.getContext('2d');
    // A browser (or test harness) without a 2d context must not break the view:
    // the surrounding panel keeps its numbers and only the plot is skipped.
    if (!ctx) return null;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    return { ctx, width, height };
  }

  function register(canvas, draw) {
    registry.push({ canvas, draw });
    if (typeof ResizeObserver !== 'undefined' && canvas.parentElement) {
      const observer = new ResizeObserver(() => {
        if (!document.body.contains(canvas)) { observer.disconnect(); return; }
        draw(canvas, 1);
      });
      observer.observe(canvas.parentElement);
    }
  }

  function niceCeil(value) {
    if (value <= 0) return 1;
    const magnitude = Math.pow(10, Math.floor(Math.log10(value)));
    const normalised = value / magnitude;
    let stepped;
    if (normalised <= 1) stepped = 1;
    else if (normalised <= 2) stepped = 2;
    else if (normalised <= 2.5) stepped = 2.5;
    else if (normalised <= 5) stepped = 5;
    else stepped = 10;
    return stepped * magnitude;
  }

  function gridColour() {
    return LL.ui.token('chart-grid', '#e8ebf0');
  }
  function axisColour() {
    return LL.ui.token('text-tertiary', '#8b93a1');
  }

  function drawGrid(ctx, width, height, top, bottom, maxValue, steps) {
    const count = steps || 4;
    ctx.save();
    ctx.strokeStyle = gridColour();
    ctx.lineWidth = 1;
    ctx.font = '10px ' + getComputedStyle(document.body).fontFamily;
    ctx.fillStyle = axisColour();
    ctx.textAlign = 'right';
    ctx.textBaseline = 'middle';
    for (let i = 0; i <= count; i += 1) {
      const ratio = i / count;
      const y = Math.round(bottom - (bottom - top) * ratio) + 0.5;
      ctx.beginPath();
      ctx.moveTo(38, y);
      ctx.lineTo(width - 4, y);
      ctx.stroke();
      ctx.fillText(LL.fmt.number(maxValue * ratio), 32, y);
    }
    ctx.restore();
  }

  function drawXLabels(ctx, points, width, height, top, bottom, count, longRange) {
    const max = Math.max(1, count || 6);
    const stride = Math.max(1, Math.ceil(points.length / max));
    ctx.save();
    ctx.fillStyle = axisColour();
    ctx.font = '10px ' + getComputedStyle(document.body).fontFamily;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'top';
    for (let i = 0; i < points.length; i += stride) {
      const x = 38 + ((width - 46) * (points.length === 1 ? 0.5 : i / (points.length - 1)));
      ctx.fillText(String(points[i].label || ''), Math.min(width - 20, Math.max(20, x)), bottom + 6);
    }
    ctx.restore();
  }

  // ------------------------------------------------------------------- line
  function line(canvas, options, progress) {
    const o = options || {};
    const points = o.points || [];
    const prepared = prepare(canvas, o.height);
    if (!prepared) return;
    const { ctx, width, height } = prepared;
    const top = 12;
    const bottom = height - 22;

    if (!points.length) {
      ctx.save();
      ctx.fillStyle = axisColour();
      ctx.font = '12px ' + getComputedStyle(document.body).fontFamily;
      ctx.textAlign = 'center';
      ctx.fillText('No data for this period', width / 2, height / 2);
      ctx.restore();
      return;
    }

    const values = points.map((p) => Number(p.value) || 0);
    const maxValue = niceCeil(Math.max(1, Math.max.apply(null, values)));
    const color = o.color || LL.ui.token('primary', '#1e5bff');

    if (o.showGrid !== false) drawGrid(ctx, width, height, top, bottom, maxValue, 4);

    const span = width - 46;
    const xAt = (i) => 38 + span * (points.length === 1 ? 0.5 : i / (points.length - 1));
    const yAt = (v) => bottom - (bottom - top) * (v / maxValue);
    const ratio = typeof progress === 'number' ? progress : 1;
    const shown = Math.max(1, Math.ceil(points.length * ratio));

    // area
    if (o.showArea !== false) {
      const gradient = ctx.createLinearGradient(0, top, 0, bottom);
      gradient.addColorStop(0, hexToRgba(color, 0.22));
      gradient.addColorStop(1, hexToRgba(color, 0.02));
      ctx.beginPath();
      ctx.moveTo(xAt(0), bottom);
      for (let i = 0; i < shown; i += 1) ctx.lineTo(xAt(i), yAt(values[i]));
      ctx.lineTo(xAt(shown - 1), bottom);
      ctx.closePath();
      ctx.fillStyle = gradient;
      ctx.fill();
    }

    // stroke
    ctx.beginPath();
    for (let i = 0; i < shown; i += 1) {
      const x = xAt(i);
      const y = yAt(values[i]);
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    }
    ctx.strokeStyle = color;
    ctx.lineWidth = 2;
    ctx.lineJoin = 'round';
    ctx.lineCap = 'round';
    ctx.stroke();

    // last point marker
    if (ratio >= 1) {
      const lastX = xAt(points.length - 1);
      const lastY = yAt(values[points.length - 1]);
      ctx.beginPath();
      ctx.arc(lastX, lastY, 3.2, 0, Math.PI * 2);
      ctx.fillStyle = color;
      ctx.fill();
      ctx.beginPath();
      ctx.arc(lastX, lastY, 6, 0, Math.PI * 2);
      ctx.fillStyle = hexToRgba(color, 0.18);
      ctx.fill();
    }

    drawXLabels(ctx, points, width, height, top, bottom, o.xLabelCount || 6, o.longRange);
  }

  // -------------------------------------------------------------------- bar
  function bar(canvas, options) {
    const o = options || {};
    const points = o.points || [];
    const prepared = prepare(canvas, o.height);
    if (!prepared) return;
    const { ctx, width, height } = prepared;
    const top = 12;
    const bottom = height - 22;

    if (!points.length) {
      ctx.save();
      ctx.fillStyle = axisColour();
      ctx.font = '12px ' + getComputedStyle(document.body).fontFamily;
      ctx.textAlign = 'center';
      ctx.fillText('No data for this period', width / 2, height / 2);
      ctx.restore();
      return;
    }

    const values = points.map((p) => Number(p.value) || 0);
    const maxValue = niceCeil(Math.max(1, Math.max.apply(null, values)));
    const color = o.color || LL.ui.token('primary', '#1e5bff');
    drawGrid(ctx, width, height, top, bottom, maxValue, 4);

    const span = width - 46;
    const slot = span / points.length;
    const barWidth = Math.max(2, Math.min(28, slot * 0.62));
    ctx.fillStyle = color;
    for (let i = 0; i < points.length; i += 1) {
      const h = (bottom - top) * (values[i] / maxValue);
      const x = 38 + slot * i + (slot - barWidth) / 2;
      roundedRect(ctx, x, bottom - h, barWidth, h, Math.min(3, barWidth / 2));
      ctx.fill();
    }
    drawXLabels(ctx, points, width, height, top, bottom, o.xLabelCount || 6, o.longRange);
  }

  function roundedRect(ctx, x, y, w, h, r) {
    const radius = Math.min(r, w / 2, Math.max(0, h / 2));
    ctx.beginPath();
    ctx.moveTo(x + radius, y);
    ctx.lineTo(x + w - radius, y);
    ctx.quadraticCurveTo(x + w, y, x + w, y + radius);
    ctx.lineTo(x + w, y + h);
    ctx.lineTo(x, y + h);
    ctx.lineTo(x, y + radius);
    ctx.quadraticCurveTo(x, y, x + radius, y);
    ctx.closePath();
  }

  // ------------------------------------------------------------------ donut
  function donut(canvas, options) {
    const o = options || {};
    const segments = (o.segments || []).filter((s) => Number(s.value) > 0);
    const size = o.size || 148;
    const prepared = prepare(canvas, size);
    if (!prepared) return;
    const { ctx, width, height } = prepared;
    const cx = width / 2;
    const cy = height / 2;
    const radius = Math.min(width, height) / 2 - 6;
    const thickness = Math.max(12, radius * 0.32);
    const total = segments.reduce((sum, s) => sum + Number(s.value), 0);

    if (!total) {
      ctx.beginPath();
      ctx.arc(cx, cy, radius - thickness / 2, 0, Math.PI * 2);
      ctx.strokeStyle = gridColour();
      ctx.lineWidth = thickness;
      ctx.stroke();
    } else {
      let start = -Math.PI / 2;
      segments.forEach((segment) => {
        const sweep = (Number(segment.value) / total) * Math.PI * 2;
        ctx.beginPath();
        ctx.arc(cx, cy, radius - thickness / 2, start, start + sweep - 0.02);
        ctx.strokeStyle = segment.color;
        ctx.lineWidth = thickness;
        ctx.lineCap = 'butt';
        ctx.stroke();
        start += sweep;
      });
    }

    ctx.save();
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = LL.ui.token('text-primary', '#111827');
    ctx.font = '700 20px ' + getComputedStyle(document.body).fontFamily;
    ctx.fillText(String(o.centerValue || ''), cx, cy - 6);
    ctx.fillStyle = LL.ui.token('text-tertiary', '#8b93a1');
    ctx.font = '11px ' + getComputedStyle(document.body).fontFamily;
    ctx.fillText(String(o.centerLabel || ''), cx, cy + 12);
    ctx.restore();
  }

  // ------------------------------------------------------------------ gauge
  function gauge(canvas, options) {
    const o = options || {};
    const size = o.size || 132;
    const prepared = prepare(canvas, size);
    if (!prepared) return;
    const { ctx, width, height } = prepared;
    const cx = width / 2;
    const cy = height / 2;
    const radius = Math.min(width, height) / 2 - 8;
    const thickness = 9;
    const start = Math.PI * 0.75;
    const sweep = Math.PI * 1.5;
    const ratio = Math.max(0, Math.min(1, (Number(o.value) || 0) / (o.max || 100)));

    ctx.beginPath();
    ctx.arc(cx, cy, radius, start, start + sweep);
    ctx.strokeStyle = gridColour();
    ctx.lineWidth = thickness;
    ctx.lineCap = 'round';
    ctx.stroke();

    ctx.beginPath();
    ctx.arc(cx, cy, radius, start, start + sweep * ratio);
    ctx.strokeStyle = o.color || LL.ui.token('primary', '#1e5bff');
    ctx.lineWidth = thickness;
    ctx.lineCap = 'round';
    ctx.stroke();

    if (o.threshold != null) {
      const angle = start + sweep * Math.max(0, Math.min(1, o.threshold / (o.max || 100)));
      const inner = radius - thickness / 2 - 3;
      const outer = radius + thickness / 2 + 3;
      ctx.beginPath();
      ctx.moveTo(cx + Math.cos(angle) * inner, cy + Math.sin(angle) * inner);
      ctx.lineTo(cx + Math.cos(angle) * outer, cy + Math.sin(angle) * outer);
      ctx.strokeStyle = axisColour();
      ctx.lineWidth = 2;
      ctx.stroke();
    }

    ctx.save();
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = LL.ui.token('text-primary', '#111827');
    ctx.font = '700 17px ' + getComputedStyle(document.body).fontFamily;
    ctx.fillText(`${Math.round(Number(o.value) || 0)}%`, cx, cy - 2);
    ctx.restore();
  }

  // -------------------------------------------------------------- sparkline
  function sparkline(canvas, values, color) {
    const prepared = prepare(canvas, 28);
    if (!prepared) return;
    const { ctx, width, height } = prepared;
    const list = (values || []).map((v) => Number(v) || 0);
    if (list.length < 2) return;
    const max = Math.max(1, Math.max.apply(null, list));
    const min = Math.min.apply(null, list);
    const span = max - min || 1;
    const xAt = (i) => (width * i) / (list.length - 1);
    const yAt = (v) => height - 3 - (height - 6) * ((v - min) / span);
    const stroke = color || LL.ui.token('primary', '#1e5bff');

    ctx.beginPath();
    ctx.moveTo(xAt(0), height);
    list.forEach((v, i) => ctx.lineTo(xAt(i), yAt(v)));
    ctx.lineTo(xAt(list.length - 1), height);
    ctx.closePath();
    const gradient = ctx.createLinearGradient(0, 0, 0, height);
    gradient.addColorStop(0, hexToRgba(stroke, 0.24));
    gradient.addColorStop(1, hexToRgba(stroke, 0.02));
    ctx.fillStyle = gradient;
    ctx.fill();

    ctx.beginPath();
    list.forEach((v, i) => (i ? ctx.lineTo(xAt(i), yAt(v)) : ctx.moveTo(xAt(i), yAt(v))));
    ctx.strokeStyle = stroke;
    ctx.lineWidth = 1.6;
    ctx.lineJoin = 'round';
    ctx.stroke();
  }

  function hexToRgba(color, alpha) {
    const value = String(color || '').trim();
    if (value.startsWith('#')) {
      let hex = value.slice(1);
      if (hex.length === 3) hex = hex.split('').map((c) => c + c).join('');
      const int = parseInt(hex, 16);
      const r = (int >> 16) & 255;
      const g = (int >> 8) & 255;
      const b = int & 255;
      return `rgba(${r}, ${g}, ${b}, ${alpha})`;
    }
    if (value.startsWith('rgb')) {
      return value.replace(/rgba?\(([^)]+)\)/, (_, inner) => {
        const parts = inner.split(',').map((p) => p.trim()).slice(0, 3);
        return `rgba(${parts.join(', ')}, ${alpha})`;
      });
    }
    return `rgba(30, 91, 255, ${alpha})`;
  }

  function animate(canvas, draw, duration) {
    if (reducedMotion()) { draw(canvas, 1); return; }
    const total = duration || 520;
    const started = performance.now();
    function frame(now) {
      if (!document.body.contains(canvas)) return;
      const progress = Math.min(1, (now - started) / total);
      draw(canvas, 1 - Math.pow(1 - progress, 3));
      if (progress < 1) requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  }

  const charts = {
    line: (canvas, options) => { animate(canvas, (c, p) => line(c, options, p)); register(canvas, (c) => line(c, options, 1)); },
    lineStatic: (canvas, options) => line(canvas, options, 1),
    bar: (canvas, options) => { bar(canvas, options); register(canvas, (c) => bar(c, options)); },
    donut: (canvas, options) => { donut(canvas, options); register(canvas, (c) => donut(c, options)); },
    gauge: (canvas, options) => { gauge(canvas, options); register(canvas, (c) => gauge(c, options)); },
    sparkline: (canvas, values, color) => { sparkline(canvas, values, color); register(canvas, (c) => sparkline(c, values, color)); },
    redrawAll: () => registry.forEach((entry) => {
      if (document.body.contains(entry.canvas)) entry.draw(entry.canvas);
    }),
    // The poll refresh replaces only changed regions; canvases that left the
    // DOM are dropped here instead of accumulating forever.
    prune: () => { for (let i = registry.length - 1; i >= 0; i--) if (!document.body.contains(registry[i].canvas)) registry.splice(i, 1); },
    reset: () => { registry.length = 0; },
    hexToRgba,
  };

  LL.charts = charts;
})();
