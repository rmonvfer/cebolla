'use client';

import { Suspense } from 'react';
import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import type { ColumnDef } from '@tanstack/react-table';
import { apiGet, type Row } from '@/lib/api';
import { DataTable } from '@/components/data-table';
import { Copyable, Panel, PageTitle, SiteName, StatusPill } from '@/components/bits';
import { datetime, num } from '@/lib/format';

function EntityInner() {
  const sp = useSearchParams();
  const kind = sp.get('kind') ?? '';
  const value = sp.get('value') ?? '';
  const display = kind === 'onion' ? `${value}.onion` : value;
  const { data } = useQuery({ queryKey: ['entity', kind, value], queryFn: () => apiGet<Row[]>(`entity?kind=${kind}&value=${encodeURIComponent(value)}`) });

  const cols: ColumnDef<Row, any>[] = [
    { header: 'Service', cell: (c) => <SiteName id={c.row.original.id} title={c.row.original.title} onion={c.row.original.onion} /> },
    { header: 'State', cell: (c) => <StatusPill status={c.row.original.status} />, meta: { align: 'right' } },
    { header: 'Pages', cell: (c) => <Link href={`/page/${c.row.original.page_id}`} className="mono tnum text-accent hover:underline">{num(c.row.original.pages)}</Link>, meta: { align: 'right' } },
    { header: 'First seen', cell: (c) => <span className="text-muted-foreground">{datetime(c.row.original.first_seen)}</span>, meta: { align: 'right' } },
    { header: 'Last seen', cell: (c) => <span className="text-muted-foreground">{datetime(c.row.original.last_seen)}</span>, meta: { align: 'right' } },
  ];

  return (
    <div>
      <PageTitle
        title={<span className="mono break-all text-lg">{display}</span>}
        sub={<div className="flex items-center gap-3"><Link href={`/entities?kind=${kind}`} className="hover:text-foreground">{kind}</Link><Copyable value={display}><span className="text-xs text-muted-foreground">copy value</span></Copyable><span className="text-xs text-muted-foreground">on {num(data?.length ?? 0)} services</span></div>}
      />
      <Panel className="p-0"><DataTable columns={cols} data={data ?? []} /></Panel>
    </div>
  );
}

export default function Entity() {
  return <Suspense fallback={null}><EntityInner /></Suspense>;
}
