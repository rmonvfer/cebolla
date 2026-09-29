'use client';

import { useQuery } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { apiGet, type Row } from '@/lib/api';
import { DataTable } from '@/components/data-table';
import { PageTitle, Panel, SiteName, StatusPill } from '@/components/bits';
import { num, ago } from '@/lib/format';

export default function Clusters() {
  const { data } = useQuery({ queryKey: ['clusters'], queryFn: () => apiGet<Row>('clusters'), refetchInterval: 20_000 });
  const clusters = data?.clusters ?? [];

  const cols: ColumnDef<Row, any>[] = [
    { header: 'Service', cell: (c) => <SiteName id={c.row.original.id} title={c.row.original.title} onion={c.row.original.onion} /> },
    { header: 'State', cell: (c) => <StatusPill status={c.row.original.status} />, meta: { align: 'right' } },
  ];

  return (
    <div>
      <PageTitle
        title="Clusters"
        sub={data ? `Groups of services with near-identical homepages. Mirrors and phishing clones surface here.${data.computed_at ? ` Computed ${ago(data.computed_at)}.` : ''}` : 'Computing…'}
      />
      {data?.building && !clusters.length && <p className="text-sm text-muted-foreground">Computing clusters over every homepage…</p>}
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        {clusters.map((cl: Row, i: number) => (
          <Panel key={i} className="p-0">
            <div className="flex items-baseline justify-between p-4 pb-2">
              <h3 className="font-display text-[13px] font-semibold">{num(cl.size)} services</h3>
              <span className="text-[11px] text-muted-foreground">{cl.exact ? 'identical text' : 'near-identical text'}</span>
            </div>
            <DataTable columns={cols} data={cl.sites ?? []} />
            {cl.size > (cl.sites?.length ?? 0) && <div className="px-4 py-2 text-xs text-muted-foreground">…and {num(cl.size - cl.sites.length)} more</div>}
          </Panel>
        ))}
      </div>
    </div>
  );
}
