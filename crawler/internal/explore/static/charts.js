'use strict';
// uPlot chart helpers, dark theme. Loaded before app.js.

const AXIS = { stroke: '#5b6675', grid: { stroke: '#ffffff0e', width: 1 }, ticks: { stroke: '#ffffff0e' }, font: '11px system-ui', size: 42 };
const CH = {
  ok: '#5fd38d', http_error: '#82aaff', offline: '#6b7482', timeout: '#f07f5e', other: '#c792ea',
  desc_not_found: '#6b7482', conn_error: '#e06c75', intro_failed: '#e5c07b', content_type: '#56b6c2',
  filter: '#f07f5e', blocklist: '#e5c07b',
  up: '#5fd38d', flaky: '#e5c07b', down: '#f07f5e', dead: '#6b7482', unknown: '#4f5b6b', auth_gated: '#b18cf2',
  p50: '#5fd38d', p90: '#82aaff', p99: '#f07f5e',
};
const palette = ['#82aaff', '#5fd38d', '#e5c07b', '#f07f5e', '#c792ea', '#56b6c2', '#e06c75', '#98c379'];
const colorFor = (name, i) => CH[name] || palette[i % palette.length];

// Align many {name, points:[[t,v]]} onto one x grid.
function align(series) {
  const xset = new Set();
  series.forEach((s) => (s.points || []).forEach((p) => xset.add(p[0])));
  const xs = [...xset].sort((a, b) => a - b);
  const xi = new Map(xs.map((x, i) => [x, i]));
  const ys = series.map((s) => {
    const a = new Array(xs.length).fill(null);
    (s.points || []).forEach((p) => (a[xi.get(p[0])] = p[1]));
    return a;
  });
  return { xs, ys, names: series.map((s) => s.name) };
}

// Rows [{t/day, ...}] -> series for the given numeric fields.
function fromRows(rows, xField, fields) {
  return fields.map((f) => ({
    name: f.label || f.key,
    key: f.key,
    points: rows.map((r) => [Math.floor(new Date(r[xField]).getTime() / 1000), r[f.key] == null ? null : Number(r[f.key])]),
  }));
}

function sizeOf(el, height) {
  return { width: Math.max(200, el.clientWidth), height };
}

// Multi-line chart. opts: {height, fill, yfmt, spanGaps}
function lineChart(el, series, opts = {}) {
  const { xs, ys, names } = align(series);
  const height = opts.height || 190;
  const uSeries = [{}].concat(
    names.map((n, i) => ({
      label: n, stroke: colorFor(n, i), width: 1.6, spanGaps: opts.spanGaps !== false,
      points: { show: false }, fill: opts.fill ? colorFor(n, i) + '22' : undefined,
      value: (u, v) => (v == null ? '–' : (opts.yfmt ? opts.yfmt(v) : fmtNum(v))),
    })),
  );
  const u = new uPlot({
    ...sizeOf(el, height), cursor: { y: false }, legend: { live: true },
    scales: { x: { time: true } },
    axes: [{ ...AXIS }, { ...AXIS, size: 48, values: (u, sp) => sp.map((v) => (opts.yfmt ? opts.yfmt(v) : fmtNum(v))) }],
    series: uSeries,
  }, [xs, ...ys], el);
  return u;
}

// Stacked-area chart from aligned category series.
function stackedChart(el, series, opts = {}) {
  const { xs, ys, names } = align(series);
  const n = xs.length;
  const cum = ys.map(() => new Array(n).fill(0));
  for (let t = 0; t < n; t++) {
    let acc = 0;
    for (let i = 0; i < ys.length; i++) { acc += ys[i][t] || 0; cum[i][t] = acc; }
  }
  // Draw largest cumulative first (back), smallest last (front).
  const order = ys.map((_, i) => i).reverse();
  const data = [xs, ...order.map((i) => cum[i])];
  const uSeries = [{}].concat(order.map((i) => ({
    label: names[i], stroke: colorFor(names[i], i), fill: colorFor(names[i], i) + '55',
    width: 1, points: { show: false }, value: (u, v) => (v == null ? '–' : fmtNum(v)),
  })));
  return new uPlot({
    ...sizeOf(el, opts.height || 190), cursor: { y: false }, legend: { live: true },
    scales: { x: { time: true } },
    axes: [{ ...AXIS }, { ...AXIS, size: 48, values: (u, sp) => sp.map(fmtNum) }],
    series: uSeries,
  }, data, el);
}

const fmtNum = (v) => {
  if (v == null) return '–';
  const a = Math.abs(v);
  if (a >= 1e6) return (v / 1e6).toFixed(1) + 'M';
  if (a >= 1e3) return (v / 1e3).toFixed(1) + 'k';
  if (a >= 10 || a === 0) return String(Math.round(v));
  if (a >= 1) return v.toFixed(1);
  return v.toPrecision(2);
};
const fmtMs = (v) => (v == null ? '–' : v >= 1000 ? (v / 1000).toFixed(1) + 's' : Math.round(v) + 'ms');

// Simple horizontal-bar distribution.
function distBars(rows, labelKey, valKey, opts = {}) {
  const max = Math.max(1, ...rows.map((r) => Number(r[valKey])));
  const color = opts.color || ((r) => colorFor(r[labelKey], 0));
  return `<div class="dist">${rows
    .map((r) => `<div class="row"><span class="${opts.mono ? 'mono' : ''}">${opts.label ? opts.label(r) : window.__h(r[labelKey])}</span>
      <span class="bar" style="width:${(100 * Number(r[valKey])) / max}%;background:${color(r)}"></span>
      <span class="n">${window.__num(r[valKey])}</span></div>`)
    .join('')}</div>`;
}
