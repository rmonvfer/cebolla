'use client';

import { Suspense, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import type { ColumnDef } from '@tanstack/react-table';
import { apiGet, type Row } from '@/lib/api';
import { DataTable } from '@/components/data-table';
import { PageTitle, Panel, Copyable } from '@/components/bits';
import { num } from '@/lib/format';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';

const KINDS = ['', 'btc', 'xmr', 'eth', 'email', 'pgp', 'onion'];

function EntitiesInner() {
  const router = useRouter();
  const params = useSearchParams();
  const kind = params.get('kind') ?? '';
  const q0 = params.get('q') ?? '';
  const offset = Number(params.get('offset') ?? 0);
  const [q, setQ] = useState(q0);

  const key = `entities?kind=${kind}&q=${encodeURIComponent(q0)}&offset=${offset}`;
  const { data } = useQuery({ queryKey: [key], queryFn: () => apiGet<Row[]>(key) });

  const cols: ColumnDef<Row, any>[] = [
    { header: 'Kind', cell: (c) => <span className="mono text-[12px] text-muted-foreground">{c.row.original.kind}</span> },
    {
      header: 'Value',
      cell: (c) => {
        const v = c.row.original.kind === 'onion' ? `${c.row.original.value}.onion` : c.row.original.value;
        return (
          <Link href={`/entity?kind=${c.row.original.kind}&value=${encodeURIComponent(c.row.original.value)}`} className="mono block max-w-[32rem] truncate text-[12px] text-accent hover:underline">
            {v}
          </Link>
        );
      },
    },
    { header: 'Services', cell: (c) => <span className="mono tnum text-primary">{num(c.row.original.sites)}</span>, meta: { align: 'right' } },
    { header: 'Mentions', cell: (c) => <span className="mono tnum">{num(c.row.original.mentions)}</span>, meta: { align: 'right' } },
  ];

  return (
    <div>
      <PageTitle title="Entities" sub="Payment addresses, keys and emails found in page text, ranked by how many services use them." />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="flex flex-wrap gap-1">
          {KINDS.map((k) => (
            <Link key={k} href={`/entities?kind=${k}`} className={cn('rounded-md border px-2.5 py-1 text-xs', k === kind ? 'border-primary/50 bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:text-foreground')}>
              {k || 'all'}
            </Link>
          ))}
        </div>
        <form onSubmit={(e) => { e.preventDefault(); router.push(`/entities?kind=${kind}&q=${encodeURIComponent(q)}`); }}>
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filter values" className="w-56" />
        </form>
      </div>
      <Panel className="p-0"><DataTable columns={cols} data={data ?? []} /></Panel>
      <div className="mt-4 flex items-center gap-4 text-sm text-muted-foreground">
        {offset > 0 && <Link href={`/entities?kind=${kind}&q=${encodeURIComponent(q0)}&offset=${Math.max(0, offset - 200)}`} className="hover:text-foreground">← Previous</Link>}
        {(data?.length ?? 0) >= 200 && <Link href={`/entities?kind=${kind}&q=${encodeURIComponent(q0)}&offset=${offset + 200}`} className="hover:text-foreground">Next →</Link>}
      </div>
    </div>
  );
}

export default function Entities() {
  return <Suspense fallback={null}><EntitiesInner /></Suspense>;
}
