import { cn } from '@/lib/utils';
import type { SelectHTMLAttributes } from 'react';

export function NativeSelect({ className, ...p }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn('h-9 rounded-md border border-input bg-card px-2 text-sm text-foreground outline-none focus:border-ring', className)}
      {...p}
    />
  );
}
