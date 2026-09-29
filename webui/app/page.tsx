'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import type { ColumnDef } from '@tanstack/react-table';
import { apiGet, prom, type Row } from '@/lib/api';
import { alignProm, StackedArea } from '@/components/charts';
import { DataTable } from '@/components/data-table';
import { DistBar, Ledger, Panel, PageTitle, SectionLabel, SiteName, StatusPill } from '@/components/bits';
import { compact, num, OUTCOME_COLOR } from '@/lib/format';
import { Skeleton } from '@/components/ui/skeleton';

const linkCols: ColumnDef<Row, any>[] = [
  { header: 'Service', cell: (c) => <SiteName id={c.row.original.id} title={c.row.original.title} onion={c.row.original.onion} /> },
  { header: 'State', cell: (c) => <StatusPill status={c.row.original.status} />, meta: { align: 'right' } },
  { header: 'Sites', accessorKey: 'degree', cell: (c) => <span className="mono tnum">{num(c.getValue())}</span>, meta: { align: 'right' } },
];

export default function Overview() {
  const ov = useQuery({ queryKey: ['overview'], queryFn: () => apiGet<Row>('overview') });
  const rate = useQuery({ queryKey: ['ov', 'rate'], queryFn: () => prom('fetch_rate', 6), refetchInterval: 20_000 });

  const t = ov.data?.totals ?? {};
  const heroData = alignProm(rate.data?.series ?? []);

  return (
    <div>
      <PageTitle
        title="Overview"
        sub="A live view of the crawled Tor hidden-service graph."
      />

      <SectionLabel hint="fetch rate by outcome, last 6 hours">Live crawl</SectionLabel>
      <Panel className="mb-6">
        {rate.isLoading ? <Skeleton className="h-[180px] w-full" /> : <StackedArea data={heroData.data} keys={heroData.keys} height={200} yFmt={(v) => compact(v) + '/s'} />}
      </Panel>

      {ov.isLoading ? (
        <Skeleton className="mb-6 h-20 w-full" />
      ) : (
        <Ledger
          items={[
            { label: 'services', value: compact(t.sites), accent: true },
            { label: 'pages', value: compact(t.pages) },
            { label: 'versions', value: compact(t.versions) },
            { label: 'links', value: compact(t.links) },
            { label: 'graph edges', value: compact(t.edges) },
            { label: 'queued', value: compact(t.frontier) },
          ]}
        />
      )}

      <div className="mt-8 grid grid-cols-1 gap-6 xl:grid-cols-2">
        <div>
          <SectionLabel hint="most other services point here">Hubs</SectionLabel>
          <Panel className="p-0">
            {ov.data && <DataTable columns={linkCols} data={ov.data.hubs ?? []} sortable />}
          </Panel>
        </div>
        <div>
          <SectionLabel hint="point at the most other services">Directories</SectionLabel>
          <Panel className="p-0">
            {ov.data && <DataTable columns={linkCols} data={ov.data.directories ?? []} sortable />}
          </Panel>
        </div>
        <div>
          <SectionLabel hint="newest services that answered">Recently found</SectionLabel>
          <Panel className="p-0">
            {ov.data && (
              <DataTable
                columns={[
                  { header: 'Service', cell: (c) => <SiteName id={c.row.original.id} title={c.row.original.title} onion={c.row.original.onion} /> },
                  { header: 'State', cell: (c) => <StatusPill status={c.row.original.status} />, meta: { align: 'right' } },
                ]}
                data={ov.data.recent ?? []}
              />
            )}
          </Panel>
        </div>
        <div>
          <SectionLabel hint="last 24 hours">Fetch outcomes</SectionLabel>
          <Panel>
            {ov.data && (
              <DistBar
                rows={(ov.data.outcomes ?? []).map((o: Row) => ({
                  label: <span className="mono text-[12px]">{o.outcome}</span>,
                  value: Number(o.n),
                  color: OUTCOME_COLOR[o.outcome] ?? 'var(--chart-1)',
                }))}
              />
            )}
          </Panel>
        </div>
      </div>
    </div>
  );
}
