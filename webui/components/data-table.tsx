'use client';

import {
  type ColumnDef,
  type SortingState,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table';
import { useState } from 'react';
import { ChevronDown, ChevronUp } from 'lucide-react';
import { cn } from '@/lib/utils';

interface Props {
  columns: ColumnDef<any, any>[];
  data: any[];
  sortable?: boolean;
  empty?: string;
  className?: string;
}

export function DataTable({ columns, data, sortable = false, empty = 'Nothing here yet.', className }: Props) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const table = useReactTable({
    data,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: sortable ? getSortedRowModel() : undefined,
  });

  if (!data.length) return <div className="px-1 py-8 text-sm text-muted-foreground">{empty}</div>;

  return (
    <div className={cn('overflow-x-auto', className)}>
      <table className="w-full border-collapse text-[13px]">
        <thead>
          {table.getHeaderGroups().map((hg) => (
            <tr key={hg.id} className="border-b border-border/60">
              {hg.headers.map((h) => {
                const canSort = sortable && h.column.getCanSort();
                const dir = h.column.getIsSorted();
                const align = (h.column.columnDef.meta as any)?.align === 'right';
                return (
                  <th
                    key={h.id}
                    onClick={canSort ? h.column.getToggleSortingHandler() : undefined}
                    className={cn(
                      'whitespace-nowrap px-3 py-2 text-xs font-medium text-muted-foreground',
                      align ? 'text-right' : 'text-left',
                      canSort && 'cursor-pointer select-none hover:text-foreground',
                    )}
                  >
                    <span className={cn('inline-flex items-center gap-1', align && 'flex-row-reverse')}>
                      {flexRender(h.column.columnDef.header, h.getContext())}
                      {dir === 'asc' && <ChevronUp className="size-3" />}
                      {dir === 'desc' && <ChevronDown className="size-3" />}
                    </span>
                  </th>
                );
              })}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr key={row.id} className="border-b border-border/40 transition-colors hover:bg-muted/30">
              {row.getVisibleCells().map((cell) => {
                const align = (cell.column.columnDef.meta as any)?.align === 'right';
                return (
                  <td key={cell.id} className={cn('px-3 py-2 align-middle', align && 'text-right')}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
