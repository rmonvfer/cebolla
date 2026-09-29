'use client';

import Link from 'next/link';
import { useState, type ReactNode } from 'react';
import { Check, Copy } from 'lucide-react';
import { cn } from '@/lib/utils';
import { num, shortOnion, STATUS_COLOR } from '@/lib/format';

export function StatusPill({ status }: { status: string }) {
  const c = STATUS_COLOR[status] ?? '#9a8ba6';
  return (
    <span
      className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2 py-0.5 text-[11px]"
      style={{ color: c, borderColor: `${c}55`, background: `${c}12` }}
    >
      <span className="size-1.5 rounded-full" style={{ background: c }} />
      {String(status).replace('_', ' ')}
    </span>
  );
}

export function Copyable({ value, children, className }: { value: string; children: ReactNode; className?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      onClick={(e) => {
        e.preventDefault();
        navigator.clipboard.writeText(value).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1000);
        });
      }}
      className={cn('group inline-flex items-center gap-1.5 text-left', className)}
      title="Copy"
    >
      {children}
      {done ? <Check className="size-3 text-up" /> : <Copy className="size-3 text-muted-foreground/50 opacity-0 transition-opacity group-hover:opacity-100" />}
    </button>
  );
}

export function OnionAddr({ onion }: { onion: string }) {
  return (
    <Copyable value={`${onion}.onion`}>
      <span className="mono text-[12px] text-muted-foreground">{shortOnion(onion)}</span>
    </Copyable>
  );
}

export function SiteName({ id, title, onion }: { id: number; title?: string; onion: string }) {
  return (
    <div className="min-w-0">
      <Link href={`/site/${id}`} className="block truncate font-medium text-foreground hover:text-primary">
        {title || <span className="text-muted-foreground/60">untitled service</span>}
      </Link>
      <OnionAddr onion={onion} />
    </div>
  );
}

export function PageTitle({ title, sub, right }: { title: ReactNode; sub?: ReactNode; right?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 className="font-display text-2xl font-semibold tracking-tight text-foreground">{title}</h1>
        {sub && <div className="mt-1 text-sm text-muted-foreground">{sub}</div>}
      </div>
      {right}
    </div>
  );
}

export function SectionLabel({ children, hint }: { children: ReactNode; hint?: ReactNode }) {
  return (
    <div className="mb-3 flex items-baseline gap-3 border-b border-border/60 pb-2">
      <h2 className="font-display text-sm font-semibold text-foreground">{children}</h2>
      {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
    </div>
  );
}

// Ledger strip: figures separated by hairlines, not identical cards.
export function Ledger({ items }: { items: { label: string; value: ReactNode; accent?: boolean }[] }) {
  return (
    <div className="grid grid-cols-2 divide-x divide-border/60 overflow-hidden rounded-lg border border-border/60 bg-card/40 sm:grid-cols-3 lg:grid-cols-6">
      {items.map((it, i) => (
        <div key={i} className="px-4 py-3">
          <div className={cn('mono text-xl tnum', it.accent ? 'text-primary' : 'text-foreground')}>{it.value}</div>
          <div className="mt-0.5 text-xs text-muted-foreground">{it.label}</div>
        </div>
      ))}
    </div>
  );
}

export function DistBar({ rows }: { rows: { label: ReactNode; value: number; color?: string }[] }) {
  const max = Math.max(1, ...rows.map((r) => r.value));
  return (
    <div className="flex flex-col gap-2">
      {rows.map((r, i) => (
        <div key={i} className="grid grid-cols-[7rem_1fr_auto] items-center gap-3 text-[13px]">
          <div className="truncate">{r.label}</div>
          <div className="h-2 rounded-full" style={{ width: `${(100 * r.value) / max}%`, minWidth: 2, background: r.color ?? 'var(--chart-1)' }} />
          <div className="mono tnum text-right text-muted-foreground">{num(r.value)}</div>
        </div>
      ))}
    </div>
  );
}

export function Panel({ children, className }: { children: ReactNode; className?: string }) {
  return <section className={cn('rounded-lg border border-border/60 bg-card/40 p-4', className)}>{children}</section>;
}
