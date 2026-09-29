'use strict';

// Everything shown here is untrusted text scraped from onion services. It is
// only ever inserted after h() escapes it, and stored URLs are never turned
// into links: they are shown as text with a copy button.

const $app = document.getElementById('app');
const $q = document.getElementById('q');

const h = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const num = (n) => Number(n || 0).toLocaleString('en-US');
const date = (t) => (t ? new Date(t).toISOString().slice(0, 16).replace('T', ' ') : '—');
const day = (t) => (t ? new Date(t).toISOString().slice(0, 10) : '—');
const enc = encodeURIComponent;

function ago(t) {
  if (!t) return '—';
  const s = (Date.now() - new Date(t)) / 1000;
  if (s < 90) return `${Math.round(s)}s ago`;
  if (s < 5400) return `${Math.round(s / 60)}m ago`;
  if (s < 129600) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

const STATUS_COLOR = { up: '#5fd38d', flaky: '#e5c07b', down: '#f07f5e', dead: '#6b7482', unknown: '#4f5b6b', auth_gated: '#b18cf2' };
const STATUSES = ['up', 'flaky', 'down', 'dead', 'unknown', 'auth_gated'];

const short = (o) => `${o.slice(0, 12)}…${o.slice(-6)}.onion`;
const onion = (o) => `<span class="onion" title="${h(o)}.onion">${h(short(o))}</span><button class="copy" data-copy="${h(o)}.onion" title="Copy address">copy</button>`;
const badge = (st) => `<span class="badge s-${h(st)}">${h(String(st).replace('_', ' '))}</span>`;
const siteLink = (s) => `<a href="#/site/${Number(s.id)}">${s.title ? h(s.title) : '<span class="faint">(no title)</span>'}</a>`;
const siteCell = (s) => `${siteLink(s)}<div>${onion(s.onion)}</div>`;
const copyText = (v, label) => `<code>${h(label ?? v)}</code><button class="copy" data-copy="${h(v)}" title="Copy">copy</button>`;

async function api(path) {
  const r = await fetch(`/api/${path}`);
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || `HTTP ${r.status}`);
  return j;
}

function toast(msg) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => (t.hidden = true), 1400);
}

document.addEventListener('click', (e) => {
  const b = e.target.closest('[data-copy]');
  if (!b) return;
  e.preventDefault();
  navigator.clipboard.writeText(b.dataset.copy).then(() => toast('Copied'), () => toast('Copy failed'));
});

document.getElementById('qform').addEventListener('submit', (e) => {
  e.preventDefault();
  location.hash = `#/search?q=${enc($q.value.trim())}`;
});

function table(cols, rows, empty = 'Nothing yet.') {
  if (!rows || !rows.length) return `<div class="empty">${empty}</div>`;
  const head = cols.map((c) => `<th class="${c.n ? 'n' : ''}">${c.t}</th>`).join('');
  const body = rows.map((r) => `<tr>${cols.map((c) => `<td class="${c.n ? 'n' : ''} ${c.cls || ''}">${c.f(r)}</td>`).join('')}</tr>`).join('');
  return `<table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>`;
}

const panel = (title, body, cls = '') => `<section class="panel ${cls}"><h2>${title}</h2>${body}</section>`;
const stat = (k, v) => `<div class="stat"><div class="v">${v}</div><div class="k">${k}</div></div>`;

function pager(offset, count, size, base) {
  const prev = offset > 0 ? `<a href="${base}&offset=${Math.max(0, offset - size)}">← Previous</a>` : '';
  const next = count >= size ? `<a href="${base}&offset=${offset + size}">Next →</a>` : '';
  return prev || next ? `<div class="pager">${prev}<span>${num(offset + 1)}–${num(offset + count)}</span>${next}</div>` : '';
}

// ---- Router ----------------------------------------------------------------

const routes = { '': overview, search, sites, site, page, graph, analytics, entities, entity, clusters };
let cyInstances = [];
let uplots = [];

async function route() {
  const [path, qs] = location.hash.replace(/^#\/?/, '').split('?');
  const [name, arg] = path.split('/');
  const params = new URLSearchParams(qs || '');
  const view = routes[name] || overview;
  document.querySelectorAll('#nav a').forEach((a) => a.classList.toggle('on', a.dataset.r === (name || 'overview') || (name === 'site' && a.dataset.r === 'sites') || (name === 'entity' && a.dataset.r === 'entities')));
  cyInstances.forEach((c) => c.destroy());
  cyInstances = [];
  uplots.forEach((u) => u.destroy());
  uplots = [];
  window.__h = h; window.__num = num;
  $app.innerHTML = '<div class="loading">Loading…</div>';
  window.scrollTo(0, 0);
  try {
    await view(arg, params);
  } catch (err) {
    $app.innerHTML = `<div class="error">${h(err.message)}</div>`;
  }
}
window.addEventListener('hashchange', route);
route();

// ---- Overview --------------------------------------------------------------

async function overview() {
  const d = await api('overview');
  const t = d.totals;
  const statusTotal = d.status.reduce((a, s) => a + s.n, 0) || 1;
  const statusBar = `<div style="display:flex;height:10px;border-radius:5px;overflow:hidden;margin:6px 0 12px">${d.status
    .map((s) => `<span title="${h(s.status)}: ${num(s.n)}" style="width:${(100 * s.n) / statusTotal}%;background:${STATUS_COLOR[s.status]}"></span>`)
    .join('')}</div>`;
  const statusList = d.status.map((s) => `<a href="#/sites?status=${enc(s.status)}">${badge(s.status)}</a> <span class="muted">${num(s.n)}</span>`).join('&nbsp;&nbsp; ');

  const maxF = Math.max(1, ...d.activity.map((a) => a.fetches));
  const bars = `<div class="bars">${d.activity
    .map((a) => `<span title="${h(date(a.t))}: ${num(a.fetches)} fetches, ${num(a.ok)} ok" style="height:${(100 * a.fetches) / maxF}%;background:linear-gradient(to top, var(--up) ${(100 * a.ok) / Math.max(1, a.fetches)}%, var(--accent-2) 0)"></span>`)
    .join('')}</div><div class="legend" style="margin-top:6px"><span><i style="background:var(--up)"></i>ok</span><span><i style="background:var(--accent-2)"></i>other outcomes</span><span>hourly, last 48 h</span></div>`;

  const degCols = [
    { t: 'Site', f: siteCell },
    { t: 'Status', f: (r) => badge(r.status) },
    { t: 'Sites', n: 1, f: (r) => num(r.degree) },
  ];
  $app.innerHTML = `
    <div class="stats">
      ${stat('sites', num(t.sites))}${stat('pages', num(t.pages))}${stat('page versions', num(t.versions))}
      ${stat('links', num(t.links))}${stat('site edges', num(t.edges))}${stat('entities', num(t.entities))}${stat('queued URLs', num(t.frontier))}
    </div>
    <div class="grid g2">
      ${panel('Sites by status', statusBar + statusList)}
      ${panel('Crawl activity', bars)}
      ${panel('Hubs <span class="faint">· linked to by the most sites</span>', `<div class="scroll">${table(degCols, d.hubs)}</div>`)}
      ${panel('Directories <span class="faint">· link to the most sites</span>', `<div class="scroll">${table(degCols, d.directories)}</div>`)}
      ${panel('Recently discovered, online', `<div class="scroll">${table([{ t: 'Site', f: siteCell }, { t: 'First seen', f: (r) => ago(r.first_seen) }], d.recent)}</div>`)}
      ${panel('Fetch outcomes, last 24 h', table([{ t: 'Outcome', f: (r) => `<code>${h(r.outcome)}</code>` }, { t: 'Fetches', n: 1, f: (r) => num(r.n) }], d.outcomes))}
    </div>`;
}

// ---- Search ----------------------------------------------------------------

async function search(_, p) {
  const q = p.get('q') || '';
  const from = Number(p.get('from') || 0);
  $q.value = q;
  if (!q) {
    $app.innerHTML = '<div class="empty">Type a query in the search box. Full-text over every indexed page (title and text).</div>';
    $q.focus();
    return;
  }
  const d = await api(`search?q=${enc(q)}&from=${from}`);
  const snip = (s) => h(s).replace(/\u0001/g, '<mark>').replace(/\u0002/g, '</mark>');
  const hits = d.hits
    .map(
      (x) => `<div class="hit">
        <div class="t"><a href="#/page/${Number(x.page_id)}">${x.title ? h(x.title) : '<span class="faint">(no title)</span>'}</a></div>
        <div class="u">${onion(x.onion)} <a class="muted" href="#/site/${Number(x.site_id)}">site</a> <span class="faint mono">${h(x.url.replace(/^https?:\/\/[^/]+/, '') || '/')}</span></div>
        <div class="sn">${(x.snippet || []).map((s) => `…${snip(s)}…`).join(' ')}</div>
      </div>`,
    )
    .join('');
  const size = 25;
  const prev = from > 0 ? `<a href="#/search?q=${enc(q)}&from=${Math.max(0, from - size)}">← Previous</a>` : '';
  const next = from + size < d.total ? `<a href="#/search?q=${enc(q)}&from=${from + size}">Next →</a>` : '';
  $app.innerHTML = `
    <div class="head"><div><h1>“${h(q)}”</h1><div class="sub muted">${num(d.total)} pages</div></div></div>
    <div class="panel">${hits || '<div class="empty">No results.</div>'}<div class="pager">${prev}${next}</div></div>`;
}

// ---- Sites -----------------------------------------------------------------

async function sites(_, p) {
  const q = p.get('q') || '', status = p.get('status') || '', sort = p.get('sort') || 'indeg';
  const comp = p.get('component'), op = p.get('operator');
  const offset = Number(p.get('offset') || 0);
  const extra = (comp != null ? `&component=${enc(comp)}` : '') + (op != null ? `&operator=${enc(op)}` : '');
  const rows = await api(`sites?q=${enc(q)}&status=${enc(status)}&sort=${enc(sort)}&offset=${offset}${extra}`);
  const banner = comp != null ? `<div class="sub muted">Component #${h(comp)} · <a href="#/analytics">back to analytics</a></div>` : op != null ? `<div class="sub muted">Shared-identifier cluster #${h(op)} — sites sharing payment addresses, keys or emails (may include a common payment processor) · <a href="#/analytics">back to analytics</a></div>` : '';
  const opt = (v, cur, label) => `<option value="${h(v)}"${v === cur ? ' selected' : ''}>${h(label ?? v)}</option>`;
  $app.innerHTML = `
    <div class="head"><div><h1>Sites</h1>${banner}</div></div>
    <form id="sf" class="toolbar">
      <input name="q" value="${h(q)}" placeholder="Title or address prefix" style="width:260px">
      <select name="status">${opt('', status, 'any status')}${STATUSES.map((s) => opt(s, status)).join('')}</select>
      <select name="sort">${opt('rank', sort, 'PageRank')}${opt('indeg', sort, 'most linked-to')}${opt('outdeg', sort, 'most links out')}${opt('pages', sort, 'most pages')}${opt('recent', sort, 'newest')}${opt('seen', sort, 'recently online')}</select>
      <button>Apply</button>
    </form>
    <div class="panel">${table(
      [
        { t: 'Site', f: siteCell },
        { t: 'Status', f: (r) => badge(r.status) },
        { t: 'Linked from', n: 1, f: (r) => num(r.indeg) },
        { t: 'Links to', n: 1, f: (r) => num(r.outdeg) },
        { t: 'PageRank', n: 1, f: (r) => (r.pagerank != null ? (r.pagerank * 1e6).toFixed(1) : '—') },
        { t: 'Pages', n: 1, f: (r) => num(r.pages_fetched) },
        { t: 'First seen', f: (r) => day(r.first_seen) },
        { t: 'Last online', f: (r) => ago(r.last_ok) },
      ],
      rows,
      'No sites match.',
    )}${pager(offset, rows.length, 100, `#/sites?q=${enc(q)}&status=${enc(status)}&sort=${enc(sort)}${extra}`)}</div>`;
  document.getElementById('sf').addEventListener('submit', (e) => {
    e.preventDefault();
    const f = new FormData(e.target);
    location.hash = `#/sites?q=${enc(f.get('q'))}&status=${enc(f.get('status'))}&sort=${enc(f.get('sort'))}${extra}`;
  });
}

// ---- Site ------------------------------------------------------------------

async function site(id) {
  const d = await api(`site/${Number(id)}`);
  const s = d.site;

  // 90 days of liveness, one cell per day; grey = not checked that day.
  const byDay = Object.fromEntries(d.uptime.map((u) => [day(u.day), u]));
  let cells = '';
  for (let i = 89; i >= 0; i--) {
    const k = day(Date.now() - i * 86400000);
    const u = byDay[k];
    const c = !u ? 'var(--line)' : u.up === u.total ? 'var(--up)' : u.up === 0 ? 'var(--down)' : 'var(--flaky)';
    cells += `<span title="${k}${u ? `: ${u.up}/${u.total} answered` : ': not checked'}" style="background:${c}"></span>`;
  }

  const siteCols = [
    { t: 'Site', f: siteCell },
    { t: 'Status', f: (r) => badge(r.status) },
    { t: 'Links', n: 1, f: (r) => num(r.n) },
  ];
  const fact = (k, v) => `<tr><th>${k}</th><td>${v}</td></tr>`;

  $app.innerHTML = `
    <div class="head">
      <div>
        <h1>${s.title ? h(s.title) : '<span class="faint">(no title)</span>'}</h1>
        <div class="sub">${onion(s.onion)} ${badge(s.status)}
          ${s.component != null ? `<a href="#/sites?component=${Number(s.component)}&sort=rank" class="muted">component #${num(s.component)}</a>` : ''}
          ${s.operator != null ? `<a href="#/sites?operator=${Number(s.operator)}&sort=rank" style="color:var(--accent)">shared-id cluster #${num(s.operator)} →</a>` : ''}
          <a href="#/graph?site=${Number(s.id)}&hops=2">Open in graph →</a></div>
      </div>
    </div>
    <div class="stats">
      ${stat('linked from (sites)', num(s.indeg))}${stat('links to (sites)', num(s.outdeg))}${stat('pages', num(s.pages_fetched))}
      ${stat('first seen', day(s.first_seen))}${stat('last online', ago(s.last_ok))}
      ${s.pagerank != null ? stat('PageRank ×10⁶', (s.pagerank * 1e6).toFixed(1)) : ''}
    </div>
    <div class="grid g2">
      ${panel('Liveness, last 90 days', `<div class="uptime">${cells}</div>
        <table style="margin-top:12px">${fact('Discovered via', `<code>${h(s.discovered_via)}</code>`)}${fact('Last attempt', date(s.last_seen))}
        ${fact('Consecutive failures', num(s.consecutive_failures))}${fact('Next check', date(s.next_check_at))}${fact('Server header', s.server ? `<code>${h(s.server)}</code>` : '—')}</table>`)}
      ${panel('Neighbourhood', `<div id="cy-mini" class="cy-mini"></div>`, 'flush')}
      ${panel(`Links to <span class="faint">· ${num(d.out.length)} sites</span>`, `<div class="scroll">${table(siteCols, d.out, 'No onion links out.')}</div>`)}
      ${panel(`Linked from <span class="faint">· ${num(d.in.length)} sites</span>`, `<div class="scroll">${table(siteCols, d.in, 'No known sites link here.')}</div>`)}
      ${panel(`Pages <span class="faint">· ${num(d.pages.length)}</span>`, `<div class="scroll">${table(
        [
          { t: 'Path', cls: 'clip', f: (r) => `<a href="#/page/${Number(r.id)}" class="mono">${h(r.url.replace(/^https?:\/\/[^/]+/, '') || '/')}</a>` },
          { t: 'Title', cls: 'clip', f: (r) => h(r.title) },
          { t: 'HTTP', n: 1, f: (r) => num(r.last_status) },
          { t: 'Versions', n: 1, f: (r) => num(r.versions) },
          { t: 'Fetched', f: (r) => ago(r.last_fetched) },
        ],
        d.pages,
        'No pages stored.',
      )}</div>`)}
      ${panel('Entities', `<div class="scroll">${table(
        [
          { t: 'Kind', f: (r) => `<code>${h(r.kind)}</code>` },
          { t: 'Value', cls: 'clip', f: (r) => `<a class="mono" href="#/entity?kind=${enc(r.kind)}&value=${enc(r.value)}">${h(r.kind === 'onion' ? short(r.value) : r.value)}</a>` },
          { t: 'Sites', n: 1, f: (r) => num(r.sites) },
        ],
        d.entities,
        'No addresses, emails or keys found.',
      )}</div>`)}
      ${panel('Similar homepages <span class="faint">· SimHash distance</span>', `<div class="scroll">${table(
        [{ t: 'Site', f: siteCell }, { t: 'Status', f: (r) => badge(r.status) }, { t: 'Bits', n: 1, f: (r) => num(r.distance) }],
        d.similar,
        'No near-duplicates.',
      )}</div>`)}
      ${panel('Sites sharing its addresses/keys', `<div class="scroll">${table(
        [{ t: 'Site', f: siteCell }, { t: 'Status', f: (r) => badge(r.status) }, { t: 'Shared', f: (r) => `${num(r.n)} <span class="faint">${h(r.kinds)}</span>` }],
        d.shared,
        'None.',
      )}</div>`)}
      ${panel('Clearnet links <span class="faint">· recorded, never fetched</span>', `<div class="scroll">${table(
        [{ t: 'URL', cls: 'clip', f: (r) => copyText(r.url) }, { t: 'Links', n: 1, f: (r) => num(r.n) }],
        d.clearnet,
        'None.',
      )}</div>`)}
      ${panel('Recent fetches', `<div class="scroll">${table(
        [
          { t: 'When', f: (r) => date(r.ts) },
          { t: 'Outcome', f: (r) => `<code>${h(r.outcome)}</code>` },
          { t: 'HTTP', n: 1, f: (r) => (r.http_status ? num(r.http_status) : '') },
          { t: 'ms', n: 1, f: (r) => num(r.latency_ms) },
        ],
        d.fetches,
      )}</div>`)}
    </div>`;

  const g = await api(`graph?site=${Number(s.id)}&hops=1&max=60`);
  drawGraph(document.getElementById('cy-mini'), g, { center: Number(s.id), mini: true });
}

// ---- Page ------------------------------------------------------------------

async function page(id, p) {
  const v = p.get('v') || '';
  const d = await api(`page/${Number(id)}${v ? `?v=${Number(v)}` : ''}`);
  const pg = d.page;
  const cur = d.version;
  const path = pg.url.replace(/^https?:\/\/[^/]+/, '') || '/';
  const versions = d.versions
    .map((x) => `<tr><td><a class="${cur && x.id === cur.id ? 'on' : ''}" href="#/page/${Number(pg.id)}?v=${Number(x.id)}">${date(x.fetched_at)}</a></td><td class="n">${num(x.chars)}</td><td><code>${h(x.simhash)}</code></td></tr>`)
    .join('');
  $app.innerHTML = `
    <div class="head">
      <div>
        <div class="muted"><a href="#/site/${Number(pg.site_id)}">${pg.site_title ? h(pg.site_title) : h(short(pg.onion))}</a> / <span class="mono">${h(path)}</span></div>
        <h1 style="margin-top:4px">${cur && cur.title ? h(cur.title) : '<span class="faint">(no title)</span>'}</h1>
        <div class="sub">${copyText(pg.url, path)} <span class="muted">HTTP ${num(pg.last_status)} · depth ${num(pg.depth)} · first fetched ${date(pg.first_fetched)}</span></div>
      </div>
    </div>
    <div class="grid" style="grid-template-columns:minmax(0,2fr) minmax(300px,1fr)">
      <section class="panel"><h2>Text ${cur ? `<span class="faint">· version of ${date(cur.fetched_at)}</span>` : ''}</h2>
        ${cur ? `<pre class="text">${h(cur.text)}</pre>${cur.truncated ? '<div class="muted" style="margin-top:6px">Truncated at 300,000 characters.</div>' : ''}` : '<div class="empty">No stored version.</div>'}
      </section>
      <div class="grid" style="align-content:start">
        ${panel(`Versions <span class="faint">· ${num(d.versions.length)}</span>`, `<div class="scroll versions"><table><thead><tr><th>Fetched</th><th class="n">Chars</th><th>SimHash</th></tr></thead><tbody>${versions}</tbody></table></div>`)}
        ${panel('Entities', table([{ t: 'Kind', f: (r) => `<code>${h(r.kind)}</code>` }, { t: 'Value', cls: 'clip', f: (r) => `<a class="mono" href="#/entity?kind=${enc(r.kind)}&value=${enc(r.value)}">${h(r.value)}</a>` }], d.entities, 'None.'))}
      </div>
    </div>
    <div style="margin-top:16px">${panel(`Links <span class="faint">· ${num(d.links.length)}</span>`, `<div class="scroll">${table(
      [
        { t: 'Anchor', cls: 'clip', f: (r) => h(r.anchor) },
        { t: 'Target', cls: 'clip', f: (r) => (r.site_id ? `<a href="#/site/${Number(r.site_id)}">${r.site_title ? h(r.site_title) : 'site'}</a> ` : '') + copyText(r.url) },
        { t: '', f: (r) => (r.is_onion ? '<span class="faint">onion</span>' : '<span class="faint">clearnet</span>') },
      ],
      d.links,
      'No links.',
    )}</div>`)}</div>`;
}

// ---- Graph -----------------------------------------------------------------

function drawGraph(el, g, opts = {}) {
  // Sites whose links all point outside the selection would float as isolated dots.
  const known = new Set(g.nodes.map((n) => n.id));
  const edges = g.edges.filter((e) => known.has(e.s) && known.has(e.t));
  const linked = new Set(edges.flatMap((e) => [e.s, e.t]));
  g = { nodes: g.nodes.filter((n) => linked.has(n.id) || n.id === opts.center), edges };
  const maxIn = Math.max(1, ...g.nodes.map((n) => n.indeg));
  const labelled = new Set([...g.nodes].sort((a, b) => b.indeg - a.indeg).slice(0, opts.mini ? 12 : 40).map((n) => n.id));
  if (opts.center) labelled.add(opts.center);
  const trim = (t) => (t.length > 28 ? `${t.slice(0, 27)}…` : t);
  const elements = [
    ...g.nodes.map((n) => ({
      data: {
        id: String(n.id), sid: n.id, indeg: n.indeg, outdeg: n.outdeg, status: n.status, title: n.title, onion: n.onion,
        color: STATUS_COLOR[n.status] || '#888',
        label: labelled.has(n.id) ? trim(n.title || short(n.onion)) : '',
        center: n.id === opts.center ? 1 : 0,
      },
    })),
    ...g.edges.map((e) => ({ data: { id: `${e.s}-${e.t}`, source: String(e.s), target: String(e.t), w: e.w } })),
  ];
  const cy = cytoscape({
    container: el,
    elements,
    wheelSensitivity: 0.3,
    minZoom: 0.05,
    maxZoom: 4,
    style: [
      { selector: 'node', style: {
        'background-color': 'data(color)', 'border-width': 0,
        width: `mapData(indeg, 0, ${maxIn}, ${opts.mini ? 7 : 8}, ${opts.mini ? 30 : 56})`,
        height: `mapData(indeg, 0, ${maxIn}, ${opts.mini ? 7 : 8}, ${opts.mini ? 30 : 56})`,
        label: 'data(label)', color: '#c9d1db', 'font-size': opts.mini ? 9 : 10, 'text-valign': 'bottom', 'text-margin-y': 3,
        'text-outline-color': '#0e1116', 'text-outline-width': 2, 'min-zoomed-font-size': 7,
      } },
      { selector: 'node[center = 1]', style: { 'border-width': 3, 'border-color': '#c792ea', width: 34, height: 34 } },
      { selector: 'edge', style: {
        width: 'mapData(w, 1, 50, 0.6, 3)', 'line-color': '#3a4554', opacity: 0.55,
        'curve-style': 'straight', 'target-arrow-shape': 'triangle', 'target-arrow-color': '#3a4554', 'arrow-scale': 0.6,
      } },
      { selector: '.dim', style: { opacity: 0.12 } },
      { selector: 'node.hl', style: { 'border-width': 2, 'border-color': '#e8edf3', label: 'data(title)' } },
      { selector: 'edge.hl', style: { 'line-color': '#82aaff', 'target-arrow-color': '#82aaff', opacity: 0.9 } },
    ],
    layout: { name: 'cose', animate: false, randomize: true, nodeRepulsion: () => 9000, idealEdgeLength: () => 70, numIter: 1200, componentSpacing: 60 },
  });
  cyInstances.push(cy);

  cy.on('mouseover', 'node', (e) => {
    const n = e.target;
    cy.elements().addClass('dim');
    n.closedNeighborhood().removeClass('dim').addClass('hl');
  });
  cy.on('mouseout', 'node', () => cy.elements().removeClass('dim hl'));
  cy.on('tap', 'node', (e) => (opts.onSelect ? opts.onSelect(e.target.data()) : (location.hash = `#/site/${e.target.data('sid')}`)));
  return cy;
}

async function graph(_, p) {
  const siteID = Number(p.get('site') || 0);
  const hops = Number(p.get('hops') || 2);
  const max = Number(p.get('max') || (siteID ? 200 : 300));
  const g = await api(siteID ? `graph?site=${siteID}&hops=${hops}&max=${max}` : `graph?max=${max}`);
  const legend = STATUSES.map((s) => `<span><i style="background:${STATUS_COLOR[s]}"></i>${s.replace('_', ' ')}</span>`).join('');
  $app.innerHTML = `
    <form id="gf" class="toolbar">
      <select name="mode">
        <option value="top"${siteID ? '' : ' selected'}>Best-connected sites</option>
        <option value="site"${siteID ? ' selected' : ''}>Around one site</option>
      </select>
      <input name="site" type="number" min="1" placeholder="site id" value="${siteID || ''}" style="width:110px">
      <select name="hops"><option value="1"${hops === 1 ? ' selected' : ''}>1 hop</option><option value="2"${hops === 2 ? ' selected' : ''}>2 hops</option></select>
      <select name="max">${[100, 200, 300, 600, 1000].map((n) => `<option${n === max ? ' selected' : ''}>${n}</option>`).join('')}</select>
      <button>Draw</button>
      <span class="muted">${num(g.nodes.length)} sites · ${num(g.edges.length)} links</span>
      <span class="legend" style="margin-left:auto">${legend}<span>size = linked from</span></span>
    </form>
    <div class="graphwrap"><div id="cy"></div><div id="side" class="side" hidden></div></div>`;
  document.getElementById('gf').addEventListener('submit', (e) => {
    e.preventDefault();
    const f = new FormData(e.target);
    location.hash = f.get('mode') === 'site' && f.get('site') ? `#/graph?site=${enc(f.get('site'))}&hops=${enc(f.get('hops'))}&max=${enc(f.get('max'))}` : `#/graph?max=${enc(f.get('max'))}`;
  });
  if (!g.nodes.length) {
    document.getElementById('cy').innerHTML = '<div class="empty" style="padding:24px">No link data yet. The site graph refreshes every 10 minutes.</div>';
    return;
  }
  const side = document.getElementById('side');
  drawGraph(document.getElementById('cy'), g, {
    center: siteID || undefined,
    onSelect: (n) => {
      side.hidden = false;
      side.innerHTML = `
        <div style="font-weight:600;margin-bottom:4px">${n.title ? h(n.title) : '<span class="faint">(no title)</span>'}</div>
        <div>${onion(n.onion)}</div>
        <div style="margin:8px 0">${badge(n.status)} <span class="muted">linked from ${num(n.indeg)} · links to ${num(n.outdeg)}</span></div>
        <div class="toolbar" style="margin:0"><a href="#/site/${Number(n.sid)}">Open site →</a><a href="#/graph?site=${Number(n.sid)}&hops=${hops}&max=${max}">Center graph here</a></div>`;
    },
  });
}

// ---- Entities --------------------------------------------------------------

const KINDS = ['', 'btc', 'xmr', 'eth', 'email', 'pgp', 'onion'];

async function entities(_, p) {
  const kind = p.get('kind') || '', q = p.get('q') || '';
  const offset = Number(p.get('offset') || 0);
  const rows = await api(`entities?kind=${enc(kind)}&q=${enc(q)}&offset=${offset}`);
  $app.innerHTML = `
    <div class="head"><div><h1>Entities</h1><div class="sub muted">Identifiers extracted from page text, ranked by how many sites use them.</div></div></div>
    <div class="toolbar">
      <div class="tabs">${KINDS.map((k) => `<a class="${k === kind ? 'on' : ''}" href="#/entities?kind=${enc(k)}&q=${enc(q)}">${k || 'all'}</a>`).join('')}</div>
      <form id="ef"><input name="q" value="${h(q)}" placeholder="Filter values" style="width:240px"></form>
    </div>
    <div class="panel">${table(
      [
        { t: 'Kind', f: (r) => `<code>${h(r.kind)}</code>` },
        { t: 'Value', cls: 'clip', f: (r) => `<a class="mono" href="#/entity?kind=${enc(r.kind)}&value=${enc(r.value)}">${h(r.kind === 'onion' ? `${r.value}.onion` : r.value)}</a>` },
        { t: 'Sites', n: 1, f: (r) => num(r.sites) },
        { t: 'Mentions', n: 1, f: (r) => num(r.mentions) },
      ],
      rows,
    )}${pager(offset, rows.length, 200, `#/entities?kind=${enc(kind)}&q=${enc(q)}`)}</div>`;
  document.getElementById('ef').addEventListener('submit', (e) => {
    e.preventDefault();
    location.hash = `#/entities?kind=${enc(kind)}&q=${enc(new FormData(e.target).get('q'))}`;
  });
}

async function entity(_, p) {
  const kind = p.get('kind') || '', value = p.get('value') || '';
  const rows = await api(`entity?kind=${enc(kind)}&value=${enc(value)}`);
  $app.innerHTML = `
    <div class="head"><div>
      <div class="muted"><a href="#/entities?kind=${enc(kind)}">Entities</a> / <code>${h(kind)}</code></div>
      <h1 class="mono" style="margin-top:4px;word-break:break-all">${h(kind === 'onion' ? `${value}.onion` : value)}</h1>
      <div class="sub">${copyText(kind === 'onion' ? `${value}.onion` : value, 'copy value')} <span class="muted">on ${num(rows.length)} sites</span></div>
    </div></div>
    <div class="panel">${table(
      [
        { t: 'Site', f: siteCell },
        { t: 'Status', f: (r) => badge(r.status) },
        { t: 'Pages', n: 1, f: (r) => `<a href="#/page/${Number(r.page_id)}">${num(r.pages)}</a>` },
        { t: 'First seen', f: (r) => date(r.first_seen) },
        { t: 'Last seen', f: (r) => date(r.last_seen) },
      ],
      rows,
    )}</div>`;
}

// ---- Clusters --------------------------------------------------------------

async function clusters() {
  const d = await api('clusters');
  if (!d.clusters.length) {
    $app.innerHTML = `<div class="empty">${d.building ? 'Computing clusters over every homepage…' : 'No clusters yet.'}</div>`;
    if (d.building) setTimeout(() => location.hash === '#/clusters' && route(), 4000);
    return;
  }
  const total = d.clusters.reduce((a, c) => a + c.size, 0);
  $app.innerHTML = `
    <div class="head"><div><h1>Clusters</h1>
      <div class="sub muted">${num(d.clusters.length)} groups of sites with near-identical homepages (SimHash ≤ 3 bits), ${num(total)} sites in total.
      Mirrors and phishing clones show up here. Computed ${ago(d.computed_at)}.</div></div></div>
    ${d.clusters
      .map(
        (c) => `<section class="panel cluster"><h2>${num(c.size)} sites · ${c.exact ? 'identical text' : 'near-identical text'}</h2>
          <div class="scroll">${table([{ t: 'Site', f: siteCell }, { t: 'Status', f: (r) => badge(r.status) }], c.sites)}</div>
          ${c.size > c.sites.length ? `<div class="muted">…and ${num(c.size - c.sites.length)} more</div>` : ''}</section>`,
      )
      .join('')}`;
}

// ---- Analytics -------------------------------------------------------------

async function apiProm(series, hours) {
  try {
    const d = await api(`prom?series=${series}&hours=${hours}`);
    return d.series || [];
  } catch {
    return [];
  }
}

function mountChart(id, fn) {
  const el = document.getElementById(id);
  if (!el) return;
  try {
    const u = fn(el);
    if (u) uplots.push(u);
  } catch (e) {
    el.innerHTML = `<div class="empty">chart error</div>`;
  }
}

const chartBox = (id, title, hint) =>
  `<div class="chart"><h3>${title}</h3>${hint ? `<div class="hint">${hint}</div>` : ''}<div id="${id}"></div></div>`;

async function analytics(_, p) {
  const hours = Number(p.get('h') || 24);
  const ranges = [[1, '1h'], [6, '6h'], [24, '24h'], [72, '3d'], [168, '7d']];
  const [d, fetchRate, latency, latency95, workers, frontier, pagesRate, linksRate] = await Promise.all([
    api('analytics'),
    apiProm('fetch_rate', hours),
    apiProm('latency', hours),
    apiProm('latency_p95', hours),
    apiProm('workers', hours),
    apiProm('frontier', hours),
    apiProm('pages_rate', hours),
    apiProm('links_rate', hours),
  ]);
  const g = d.graph || {};

  // Manual index column (table() has no row index), so render top_rank directly.
  const rankRows = (d.top_rank || [])
    .map((r, i) => `<tr><td class="n muted">${i + 1}</td><td>${siteCell(r)}</td><td>${badge(r.status)}</td><td class="n">${num(r.indeg)}</td><td class="n">${(r.pagerank * 1e6).toFixed(1)}</td></tr>`)
    .join('');

  $app.innerHTML = `
    <div class="head">
      <div><h1>Analytics</h1><div class="sub muted">Live crawl telemetry (Prometheus) and structural analysis of the link graph.</div></div>
      <div class="rangesel">${ranges.map(([hh, lbl]) => `<a class="${hh === hours ? 'on' : ''}" href="#/analytics?h=${hh}">${lbl}</a>`).join('')}</div>
    </div>

    <div class="section-title"><h2>Live</h2><span class="hint">rolling rates over the selected window</span></div>
    <div class="chartgrid">
      ${chartBox('c-fetch', 'Fetch rate by outcome', 'requests/sec, stacked')}
      ${chartBox('c-latency', 'Fetch latency', 'successful fetches')}
      ${chartBox('c-through', 'Throughput', 'pages stored & links queued /sec')}
      ${chartBox('c-workers', 'Busy workers')}
      ${chartBox('c-frontier', 'Frontier size')}
    </div>

    <div class="section-title"><h2>History</h2><span class="hint">from the crawl database</span></div>
    <div class="chartgrid">
      ${chartBox('c-disc', 'Sites discovered', 'cumulative known sites')}
      ${chartBox('c-pages', 'Pages stored', 'cumulative')}
      ${chartBox('c-outcomes', 'Fetch outcomes', 'per hour, last 7 days, stacked')}
      ${chartBox('c-pct', 'Latency percentiles', 'per hour, last 3 days')}
    </div>

    <div class="section-title"><h2>Distributions</h2></div>
    <div class="grid g2">
      ${panel('Sites by status', distBars(d.status, 'status', 'n', { label: (r) => badge(r.status) }))}
      ${panel('Pages per site', distBars(d.site_sizes, 'bucket', 'sites', { color: () => 'var(--accent-2)' }))}
      ${panel('Crawl depth of stored pages', distBars(d.depth, 'depth', 'n', { color: () => 'var(--accent)', label: (r) => `depth ${num(r.depth)}` }))}
      ${panel('Entity kinds <span class="faint">· sites carrying each</span>', distBars(d.entity_kinds, 'kind', 'sites', { color: () => 'var(--up)', mono: true }))}
    </div>

    <div class="section-title"><h2>Graph analysis</h2>
      <span class="hint">${g.analysed ? `${num(g.analysed)} sites · ${num(g.components)} components · ${num(g.operators)} shared-id clusters · updated ${ago(g.updated_at)}` : 'not computed yet (runs every 15 min)'}</span></div>
    <div class="grid g2">
      ${panel('Most important sites <span class="faint">· PageRank</span>', `<div class="scroll"><table><thead><tr><th class="n">#</th><th>Site</th><th>Status</th><th class="n">Linked from</th><th class="n">PageRank ×10⁶</th></tr></thead><tbody>${rankRows || '<tr><td colspan="5" class="muted">pending…</td></tr>'}</tbody></table></div>`)}
      ${panel('Largest components <span class="faint">· weakly-connected</span>', `<div class="scroll">${table(
        [
          { t: 'Component', f: (r) => `<a href="#/sites?component=${Number(r.component)}&sort=rank">#${num(r.component)}</a>` },
          { t: 'Top site', cls: 'clip', f: (r) => h(r.top) },
          { t: 'Sites', n: 1, f: (r) => num(r.sites) },
        ],
        d.components,
        'pending…',
      )}</div>`)}
      ${panel('Shared-identifier clusters <span class="faint">· sites sharing payment addresses / keys / emails</span>', `<div class="scroll">${table(
        [
          { t: 'Cluster', f: (r) => `<a href="#/sites?operator=${Number(r.operator)}&sort=rank">#${num(r.operator)}</a>` },
          { t: 'Top site', cls: 'clip', f: (r) => h(r.top) },
          { t: 'Shared', f: (r) => `<span class="faint mono">${h(r.kinds || '')}</span>` },
          { t: 'Sites', n: 1, f: (r) => num(r.sites) },
        ],
        d.operators,
        'no clusters found',
      )}</div>`)}
    </div>`;

  // Instantiate charts (elements now in the DOM with a width).
  mountChart('c-fetch', (el) => stackedChart(el, fetchRate, { yfmt: fmtNum }));
  mountChart('c-latency', (el) => lineChart(el, [
    ...(latency.map((s) => ({ ...s, name: 'p50' }))),
    ...(latency95.map((s) => ({ ...s, name: 'p95' }))),
  ], { yfmt: (v) => v.toFixed(1) + 's' }));
  mountChart('c-through', (el) => lineChart(el, [
    ...(pagesRate.map((s) => ({ ...s, name: 'pages' }))),
    ...(linksRate.map((s) => ({ ...s, name: 'links' }))),
  ], { fill: true }));
  mountChart('c-workers', (el) => lineChart(el, workers.map((s) => ({ ...s, name: 'busy' })), { fill: true }));
  mountChart('c-frontier', (el) => lineChart(el, frontier, { yfmt: fmtNum }));

  mountChart('c-disc', (el) => lineChart(el, fromRows(d.discovery, 'day', [{ key: 'total', label: 'known sites' }]), { fill: true, yfmt: fmtNum }));
  mountChart('c-pages', (el) => lineChart(el, fromRows(d.pages, 'day', [{ key: 'total', label: 'pages' }]), { fill: true, yfmt: fmtNum }));
  mountChart('c-outcomes', (el) => stackedChart(el, fromRows(d.outcomes, 't', [
    { key: 'ok' }, { key: 'http_error' }, { key: 'offline' }, { key: 'timeout' }, { key: 'other' },
  ]), { yfmt: fmtNum }));
  mountChart('c-pct', (el) => lineChart(el, fromRows(d.latency, 't', [{ key: 'p50' }, { key: 'p90' }, { key: 'p99' }]), { yfmt: fmtMs }));

}
