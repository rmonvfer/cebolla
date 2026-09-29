'use client';

import { useQueries, useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import type { ColumnDef } from '@tanstack/react-table';
import { apiGet, prom, type PromMatrix, type Row } from '@/lib/api';
import { alignProm, fromRows, Lines, StackedArea } from '@/components/charts';
import { DataTable } from '@/components/data-table';
import { DistBar, Panel, PageTitle, SectionLabel, SiteName } from '@/components/bits';
import { StatusPill } from '@/components/bits';
import { compact, ms, num, STATUS_COLOR, ago } from '@/lib/format';
import { useUI } from '@/store/ui';
import { cn } from '@/lib/utils';

const RANGES = [[1, '1h'], [6, '6h'], [24, '24h'], [72, '3d'], [168, '7d']] as const;

function ChartCard({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <Panel>
      <div className="mb-1 flex items-baseline justify-between">
        <h3 className="font-display text-[13px] font-semibold text-foreground">{title}</h3>
        {hint && <span className="text-[11px] text-muted-foreground">{hint}</span>}
      </div>
      {children}
    </Panel>
  );
}

export default function Analytics() {
  const hours = useUI((s) => s.rangeHours);
  const setHours = useUI((s) => s.setRangeHours);

  const a = useQuery({ queryKey: ['analytics'], queryFn: () => apiGet<Row>('analytics'), refetchInterval: 60_000 });
  const series = ['fetch_rate', 'latency', 'latency_p95', 'workers', 'frontier', 'pages_rate', 'links_rate'];
  const live = useQueries({
    queries: series.map((s) => ({ queryKey: ['prom', s, hours], queryFn: () => prom(s, hours), refetchInterval: 20_000 })),
  });
  const P = (i: number): PromMatrix => (live[i].data as PromMatrix) ?? { series: [], step: 0 };
  const named = (m: PromMatrix, name: string) => m.series.map((s) => ({ ...s, name }));

  const fetchRate = alignProm(P(0).series);
  const latency = alignProm([...named(P(1), 'p50'), ...named(P(2), 'p95')]);
  const workers = alignProm(named(P(3), 'busy'));
  const frontier = alignProm(P(4).series);
  const through = alignProm([...named(P(5), 'pages'), ...named(P(6), 'links')]);

  const d = a.data ?? {};
  const g = d.graph ?? {};

  const rankCols: ColumnDef<Row, any>[] = [
    { header: '#', cell: (c) => <span className="mono tnum text-muted-foreground">{c.row.index + 1}</span>, meta: { align: 'right' } },
    { header: 'Service', cell: (c) => <SiteName id={c.row.original.id} title={c.row.original.title} onion={c.row.original.onion} /> },
    { header: 'In', cell: (c) => <span className="mono tnum">{num(c.row.original.indeg)}</span>, meta: { align: 'right' } },
    { header: 'PageRank', cell: (c) => <span className="mono tnum text-primary">{(c.row.original.pagerank * 1e6).toFixed(1)}</span>, meta: { align: 'right' } },
  ];

  return (
    <div>
      <PageTitle
        title="Analytics"
        sub="Live crawl telemetry and structural analysis of the link graph."
        right={
          <div className="flex gap-1">
            {RANGES.map(([h, label]) => (
              <button
                key={h}
                onClick={() => setHours(h)}
                className={cn('rounded-md border px-2.5 py-1 text-xs', hours === h ? 'border-primary/50 bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:text-foreground')}
              >
                {label}
              </button>
            ))}
          </div>
        }
      />

      <SectionLabel hint="rolling rates over the selected window">Live</SectionLabel>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2 2xl:grid-cols-3">
        <ChartCard title="Fetch rate" hint="by outcome, per second"><StackedArea data={fetchRate.data} keys={fetchRate.keys} yFmt={compact} /></ChartCard>
        <ChartCard title="Fetch latency" hint="successful fetches"><Lines data={latency.data} keys={latency.keys} yFmt={(v) => v.toFixed(0) + 's'} /></ChartCard>
        <ChartCard title="Throughput" hint="pages & links /s"><Lines data={through.data} keys={through.keys} fill /></ChartCard>
        <ChartCard title="Busy workers"><Lines data={workers.data} keys={workers.keys} /></ChartCard>
        <ChartCard title="Frontier size" hint="queued URLs"><Lines data={frontier.data} keys={frontier.keys} yFmt={compact} /></ChartCard>
      </div>

      <div className="mt-8">
        <SectionLabel hint="from the crawl database">History</SectionLabel>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2 2xl:grid-cols-4">
          <ChartCard title="Services discovered" hint="cumulative"><Lines data={fromRows(d.discovery, 'day', ['total']).data} keys={['total']} xMode="day" fill yFmt={compact} /></ChartCard>
          <ChartCard title="Pages stored" hint="cumulative"><Lines data={fromRows(d.pages, 'day', ['total']).data} keys={['total']} xMode="day" fill yFmt={compact} /></ChartCard>
          <ChartCard title="Fetch outcomes" hint="per hour, 7 days"><StackedArea data={fromRows(d.outcomes, 't', ['ok', 'http_error', 'offline', 'timeout', 'other']).data} keys={['ok', 'http_error', 'offline', 'timeout', 'other']} yFmt={compact} /></ChartCard>
          <ChartCard title="Latency percentiles" hint="per hour, 3 days"><Lines data={fromRows(d.latency, 't', ['p50', 'p90', 'p99']).data} keys={['p50', 'p90', 'p99']} yFmt={ms} /></ChartCard>
        </div>
      </div>

      <div className="mt-8">
        <SectionLabel>Distributions</SectionLabel>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
          <Panel><h3 className="mb-3 font-display text-[13px] font-semibold">Services by state</h3><DistBar rows={(d.status ?? []).map((r: Row) => ({ label: <StatusPill status={r.status} />, value: Number(r.n), color: STATUS_COLOR[r.status] }))} /></Panel>
          <Panel><h3 className="mb-3 font-display text-[13px] font-semibold">Pages per service</h3><DistBar rows={(d.site_sizes ?? []).map((r: Row) => ({ label: r.bucket, value: Number(r.sites) }))} /></Panel>
          <Panel><h3 className="mb-3 font-display text-[13px] font-semibold">Crawl depth</h3><DistBar rows={(d.depth ?? []).map((r: Row) => ({ label: `depth ${r.depth}`, value: Number(r.n), color: 'var(--chart-5)' }))} /></Panel>
          <Panel><h3 className="mb-3 font-display text-[13px] font-semibold">Entities</h3><DistBar rows={(d.entity_kinds ?? []).map((r: Row) => ({ label: <span className="mono text-[12px]">{r.kind}</span>, value: Number(r.sites), color: 'var(--chart-2)' }))} /></Panel>
        </div>
      </div>

      <div className="mt-8">
        <SectionLabel hint={g.analysed ? `${num(g.analysed)} services · ${num(g.components)} components · ${num(g.operators)} shared-id clusters · ${ago(g.updated_at)}` : 'runs every 15 min'}>
          Graph analysis
        </SectionLabel>
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <Panel className="p-0">
            <div className="p-4 pb-2"><h3 className="font-display text-[13px] font-semibold">Most important services</h3><span className="text-[11px] text-muted-foreground">by PageRank</span></div>
            {d.top_rank && <DataTable columns={rankCols} data={d.top_rank} />}
          </Panel>
          <div className="grid gap-4">
            <Panel className="p-0">
              <div className="p-4 pb-2"><h3 className="font-display text-[13px] font-semibold">Largest components</h3></div>
              <DataTable
                columns={[
                  { header: 'Component', cell: (c) => <Link href={`/sites?component=${c.row.original.component}&sort=rank`} className="mono text-primary hover:underline">#{c.row.original.component}</Link> },
                  { header: 'Top service', cell: (c) => <span className="block max-w-[16rem] truncate">{c.row.original.top}</span> },
                  { header: 'Sites', cell: (c) => <span className="mono tnum">{num(c.row.original.sites)}</span>, meta: { align: 'right' } },
                ]}
                data={d.components ?? []}
              />
            </Panel>
            <Panel className="p-0">
              <div className="p-4 pb-2"><h3 className="font-display text-[13px] font-semibold">Shared-identifier clusters</h3><span className="text-[11px] text-muted-foreground">common PGP key or email</span></div>
              <DataTable
                columns={[
                  { header: 'Cluster', cell: (c) => <Link href={`/sites?operator=${c.row.original.operator}&sort=rank`} className="mono text-primary hover:underline">#{c.row.original.operator}</Link> },
                  { header: 'Top service', cell: (c) => <span className="block max-w-[14rem] truncate">{c.row.original.top}</span> },
                  { header: 'Shared', cell: (c) => <span className="mono text-[11px] text-muted-foreground">{c.row.original.kinds}</span> },
                  { header: 'Sites', cell: (c) => <span className="mono tnum">{num(c.row.original.sites)}</span>, meta: { align: 'right' } },
                ]}
                data={d.operators ?? []}
              />
            </Panel>
          </div>
        </div>
      </div>
    </div>
  );
}
