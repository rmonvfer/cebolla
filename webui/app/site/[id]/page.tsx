'use client';

import { use } from 'react';
import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import type { ColumnDef } from '@tanstack/react-table';
import { apiGet, type Row } from '@/lib/api';
import { GraphCanvas, type GNode } from '@/components/graph-canvas';
import { DataTable } from '@/components/data-table';
import { Copyable, Ledger, OnionAddr, Panel, PageTitle, SectionLabel, SiteName, StatusPill } from '@/components/bits';
import { compact, datetime, day, num, ago } from '@/lib/format';

function Liveness({ uptime }: { uptime: Row[] }) {
  const byDay = new Map(uptime.map((u) => [String(u.day).slice(0, 10), u]));
  const cells = [];
  for (let i = 89; i >= 0; i--) {
    const key = new Date(Date.now() - i * 86400000).toISOString().slice(0, 10);
    const u = byDay.get(key);
    const color = !u ? 'var(--border)' : u.up === u.total ? 'var(--up)' : u.up === 0 ? 'var(--down)' : 'var(--flaky)';
    cells.push(<span key={i} title={`${key}${u ? `: ${u.up}/${u.total} answered` : ': not checked'}`} className="h-6 flex-1 rounded-[2px]" style={{ background: color, minWidth: 2 }} />);
  }
  return <div className="flex items-end gap-px">{cells}</div>;
}

const siteCols: ColumnDef<Row, any>[] = [
  { header: 'Service', cell: (c) => <SiteName id={c.row.original.id} title={c.row.original.title} onion={c.row.original.onion} /> },
  { header: 'State', cell: (c) => <StatusPill status={c.row.original.status} />, meta: { align: 'right' } },
  { header: 'Links', cell: (c) => <span className="mono tnum">{num(c.row.original.n)}</span>, meta: { align: 'right' } },
];

export default function SitePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const { data: d } = useQuery({ queryKey: ['site', id], queryFn: () => apiGet<Row>(`site/${id}`) });
  const neigh = useQuery({ queryKey: ['site', id, 'graph'], queryFn: () => apiGet<{ nodes: GNode[]; edges: any[] }>(`graph?site=${id}&hops=1&max=60`) });

  if (!d) return <div className="text-sm text-muted-foreground">Loading…</div>;
  const s = d.site;

  return (
    <div>
      <PageTitle
        title={s.title || 'untitled service'}
        sub={
          <div className="flex flex-wrap items-center gap-3">
            <OnionAddr onion={s.onion} />
            <StatusPill status={s.status} />
            {s.component != null && <Link href={`/sites?component=${s.component}&sort=rank`} className="text-xs text-muted-foreground hover:text-foreground">component #{s.component}</Link>}
            {s.operator != null && <Link href={`/sites?operator=${s.operator}&sort=rank`} className="text-xs text-primary hover:underline">shared-id cluster #{s.operator}</Link>}
            <Link href={`/graph?site=${s.id}&hops=2`} className="text-xs text-accent hover:underline">open in graph</Link>
          </div>
        }
      />

      <Ledger
        items={[
          { label: 'linked from', value: compact(s.indeg), accent: true },
          { label: 'links out', value: compact(s.outdeg) },
          { label: 'pages', value: compact(s.pages_fetched) },
          { label: 'first seen', value: day(s.first_seen) },
          { label: 'last online', value: ago(s.last_ok) },
          { label: 'PageRank', value: s.pagerank != null ? (s.pagerank * 1e6).toFixed(1) : '—' },
        ]}
      />

      <div className="mt-8 grid grid-cols-1 gap-6 xl:grid-cols-2">
        <div>
          <SectionLabel hint="last 90 days">Liveness</SectionLabel>
          <Panel>
            <Liveness uptime={d.uptime ?? []} />
            <table className="mt-4 w-full text-[13px]">
              <tbody className="[&_td]:py-1 [&_th]:py-1 [&_th]:pr-6 [&_th]:text-left [&_th]:font-normal [&_th]:text-muted-foreground">
                <tr><th>Discovered via</th><td className="mono text-[12px]">{s.discovered_via}</td></tr>
                <tr><th>Last attempt</th><td>{datetime(s.last_seen)}</td></tr>
                <tr><th>Consecutive failures</th><td className="mono">{num(s.consecutive_failures)}</td></tr>
                <tr><th>Next check</th><td>{datetime(s.next_check_at)}</td></tr>
                <tr><th>Server header</th><td className="mono text-[12px]">{s.server || '—'}</td></tr>
              </tbody>
            </table>
          </Panel>
        </div>
        <div>
          <SectionLabel hint="one hop">Neighbourhood</SectionLabel>
          {neigh.data && <GraphCanvas nodes={neigh.data.nodes} edges={neigh.data.edges} center={Number(id)} height={300} labelTop={12} />}
        </div>

        <div>
          <SectionLabel hint={`${num((d.out ?? []).length)} services`}>Links to</SectionLabel>
          <Panel className="max-h-[420px] overflow-auto p-0"><DataTable columns={siteCols} data={d.out ?? []} empty="No onion links out." /></Panel>
        </div>
        <div>
          <SectionLabel hint={`${num((d.in ?? []).length)} services`}>Linked from</SectionLabel>
          <Panel className="max-h-[420px] overflow-auto p-0"><DataTable columns={siteCols} data={d.in ?? []} empty="No known services link here." /></Panel>
        </div>

        <div>
          <SectionLabel hint={`${num((d.pages ?? []).length)}`}>Pages</SectionLabel>
          <Panel className="max-h-[420px] overflow-auto p-0">
            <DataTable
              columns={[
                { header: 'Path', cell: (c) => <Link href={`/page/${c.row.original.id}`} className="mono block max-w-[22rem] truncate text-[12px] text-accent hover:underline">{String(c.row.original.url).replace(/^https?:\/\/[^/]+/, '') || '/'}</Link> },
                { header: 'HTTP', cell: (c) => <span className="mono tnum">{num(c.row.original.last_status)}</span>, meta: { align: 'right' } },
                { header: 'Versions', cell: (c) => <span className="mono tnum">{num(c.row.original.versions)}</span>, meta: { align: 'right' } },
                { header: 'Fetched', cell: (c) => <span className="text-muted-foreground">{ago(c.row.original.last_fetched)}</span>, meta: { align: 'right' } },
              ]}
              data={d.pages ?? []}
            />
          </Panel>
        </div>
        <div>
          <SectionLabel>Entities</SectionLabel>
          <Panel className="max-h-[420px] overflow-auto p-0">
            <DataTable
              columns={[
                { header: 'Kind', cell: (c) => <span className="mono text-[12px] text-muted-foreground">{c.row.original.kind}</span> },
                { header: 'Value', cell: (c) => <Link href={`/entity?kind=${c.row.original.kind}&value=${encodeURIComponent(c.row.original.value)}`} className="mono block max-w-[22rem] truncate text-[12px] text-accent hover:underline">{c.row.original.kind === 'onion' ? c.row.original.value : c.row.original.value}</Link> },
                { header: 'Services', cell: (c) => <span className="mono tnum text-primary">{num(c.row.original.sites)}</span>, meta: { align: 'right' } },
              ]}
              data={d.entities ?? []}
              empty="No addresses, emails or keys found."
            />
          </Panel>
        </div>

        <div>
          <SectionLabel hint="SimHash distance">Similar homepages</SectionLabel>
          <Panel className="max-h-[360px] overflow-auto p-0">
            <DataTable
              columns={[
                { header: 'Service', cell: (c) => <SiteName id={c.row.original.id} title={c.row.original.title} onion={c.row.original.onion} /> },
                { header: 'Bits', cell: (c) => <span className="mono tnum">{num(c.row.original.distance)}</span>, meta: { align: 'right' } },
              ]}
              data={d.similar ?? []}
              empty="No near-duplicates."
            />
          </Panel>
        </div>
        <div>
          <SectionLabel>Shares addresses / keys with</SectionLabel>
          <Panel className="max-h-[360px] overflow-auto p-0">
            <DataTable
              columns={[
                { header: 'Service', cell: (c) => <SiteName id={c.row.original.id} title={c.row.original.title} onion={c.row.original.onion} /> },
                { header: 'Shared', cell: (c) => <span><span className="mono tnum">{num(c.row.original.n)}</span> <span className="text-[11px] text-muted-foreground">{c.row.original.kinds}</span></span>, meta: { align: 'right' } },
              ]}
              data={d.shared ?? []}
              empty="None."
            />
          </Panel>
        </div>
      </div>
    </div>
  );
}
