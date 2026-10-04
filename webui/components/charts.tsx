'use client';

import { Area, AreaChart, CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts';
import { ChartConfig, ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart';
import type { PromLine } from '@/lib/api';
import { colorFor, compact } from '@/lib/format';
import { cn } from '@/lib/utils';

type Point = Record<string, number | null>;

// Merge Prometheus lines onto a shared time axis.
export function alignProm(series: PromLine[]): { data: Point[]; keys: string[] } {
  const keys = series.map((s) => s.name);
  const byT = new Map<number, Point>();
  for (const s of series) {
    for (const [t, v] of s.points) {
      const row = byT.get(t) ?? { t };
      row[s.name] = v;
      byT.set(t, row);
    }
  }
  const data = [...byT.values()].sort((a, b) => (a.t as number) - (b.t as number));
  return { data, keys };
}

// Rows [{t|day, ...}] -> chart points for the given numeric keys.
export function fromRows(rows: any[], xField: string, keys: string[]): { data: Point[]; keys: string[] } {
  const data = (rows ?? [])
    .map((r) => {
      const t = Math.floor(new Date(r[xField]).getTime() / 1000);
      const p: Point = { t };
      for (const k of keys) p[k] = r[k] == null ? null : Number(r[k]);
      return p;
    })
    .filter((p) => Number.isFinite(p.t as number));
  return { data, keys };
}

const cfgFor = (keys: string[]): ChartConfig =>
  Object.fromEntries(keys.map((k, i) => [k, { label: k, color: colorFor(k, i) }]));

const timeTick = (t: number) => {
  if (!Number.isFinite(t)) return '';
  const d = new Date(t * 1000);
  return d.getUTCHours().toString().padStart(2, '0') + ':' + d.getUTCMinutes().toString().padStart(2, '0');
};
const dayTick = (t: number) => {
  if (!Number.isFinite(t)) return '';
  return new Date(t * 1000).toISOString().slice(5, 10);
};
const labelFmt = (xMode: 'time' | 'day') => (v: unknown) => {
  const n = Number(v);
  return xMode === 'day' ? dayTick(n) : timeTick(n);
};

interface ChartProps {
  data: Point[];
  keys: string[];
  height?: number;
  yFmt?: (v: number) => string;
  xMode?: 'time' | 'day';
  className?: string;
}

export function StackedArea({ data, keys, height = 180, yFmt = compact, xMode = 'time', className }: ChartProps) {
  const config = cfgFor(keys);
  return (
    <ChartContainer config={config} className={cn('!block !aspect-auto w-full', className)} style={{ height, width: '100%' }}>
      <AreaChart data={data} margin={{ left: 4, right: 8, top: 8, bottom: 0 }}>
        <defs>
          {keys.map((k, i) => (
            <linearGradient key={k} id={`g-${k}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={colorFor(k, i)} stopOpacity={0.55} />
              <stop offset="100%" stopColor={colorFor(k, i)} stopOpacity={0.05} />
            </linearGradient>
          ))}
        </defs>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="t" type="number" scale="time" domain={['dataMin', 'dataMax']} tickFormatter={xMode === 'day' ? dayTick : timeTick} tickLine={false} axisLine={false} minTickGap={40} />
        <YAxis width={40} tickFormatter={yFmt} tickLine={false} axisLine={false} />
        <ChartTooltip content={<ChartTooltipContent labelFormatter={labelFmt(xMode)} />} />
        {keys.map((k, i) => (
          <Area key={k} dataKey={k} type="monotone" stackId="1" stroke={colorFor(k, i)} strokeWidth={1} fill={`url(#g-${k})`} isAnimationActive={false} />
        ))}
      </AreaChart>
    </ChartContainer>
  );
}

export function Lines({ data, keys, height = 180, yFmt = compact, xMode = 'time', className, fill }: ChartProps & { fill?: boolean }) {
  const config = cfgFor(keys);
  return (
    <ChartContainer config={config} className={cn('!block !aspect-auto w-full', className)} style={{ height, width: '100%' }}>
      <LineChart data={data} margin={{ left: 4, right: 8, top: 8, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="t" type="number" scale="time" domain={['dataMin', 'dataMax']} tickFormatter={xMode === 'day' ? dayTick : timeTick} tickLine={false} axisLine={false} minTickGap={40} />
        <YAxis width={40} tickFormatter={yFmt} tickLine={false} axisLine={false} />
        <ChartTooltip content={<ChartTooltipContent labelFormatter={labelFmt(xMode)} />} />
        {keys.map((k, i) => (
          <Line key={k} dataKey={k} type="monotone" stroke={colorFor(k, i)} strokeWidth={1.6} dot={false} connectNulls isAnimationActive={false} />
        ))}
      </LineChart>
    </ChartContainer>
  );
}
