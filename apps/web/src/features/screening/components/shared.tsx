'use client';
import type { ComponentProps, ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
export { Button, Input };
export const labels: Record<string, string> = {
  pending: '待复核',
  kept: '保留',
  excluded: '排除',
  unsure: '待确认',
  passed: '通过',
  insufficient: '信息不足',
  regular: '常规',
  exploration: '探索',
  queued: '排队中',
  running: '执行中',
  succeeded: '完成',
  partial: '部分完成',
  failed: '失败',
  uploaded: '已上传',
  parsing: '解析中',
  preview: '待确认',
  importing: '导入中',
  completed: '已导入',
  applied: '已执行',
  undone: '已撤销'
};
export function Badge({ value }: { value: string | null }) {
  return (
    <span
      className={cn(
        'inline-flex items-center whitespace-nowrap rounded-md px-2 py-0.5 text-xs font-medium ring-1 ring-inset',
        value && ['kept', 'passed', 'succeeded', 'completed', 'applied'].includes(value)
          ? 'bg-emerald-50 text-emerald-700 ring-emerald-600/15 dark:bg-emerald-500/10 dark:text-emerald-300'
          : value && ['excluded', 'failed'].includes(value)
            ? 'bg-red-50 text-red-700 ring-red-600/15 dark:bg-red-500/10 dark:text-red-300'
            : value && ['pending', 'unsure', 'insufficient', 'partial'].includes(value)
              ? 'bg-amber-50 text-amber-700 ring-amber-600/15 dark:bg-amber-500/10 dark:text-amber-300'
              : value && ['queued', 'running', 'parsing', 'importing', 'preview'].includes(value)
                ? 'bg-blue-50 text-blue-700 ring-blue-600/15 dark:bg-blue-500/10 dark:text-blue-300'
                : 'bg-muted text-muted-foreground ring-border'
      )}
    >
      {value ? labels[value] || value : '未初筛'}
    </span>
  );
}
export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className='flex min-w-0 flex-col gap-1.5 text-sm'>
      <span className='text-muted-foreground text-xs font-medium'>{label}</span>
      {children}
    </label>
  );
}
export function DataBlock({ title, value }: { title: string; value: unknown }) {
  return (
    <details className='rounded border p-3'>
      <summary className='cursor-pointer text-sm'>{title}</summary>
      <pre className='mt-2 max-h-72 overflow-auto whitespace-pre-wrap text-xs'>
        {JSON.stringify(value, null, 2)}
      </pre>
    </details>
  );
}
export type Action = <T>(work: () => Promise<T>, success?: string) => Promise<T | undefined>;

export function formatMoney(value: string | null | undefined) {
  if (value == null || value === '') return '—';
  const number = Number(value);
  return Number.isFinite(number)
    ? number.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    : '—';
}

/* oxlint-disable jsx-a11y/no-noninteractive-tabindex -- A named overflow region needs a keyboard scroll target (Modern Web Guidance responsive-table). */
export function TableScroll({
  label,
  className,
  children,
  ...props
}: ComponentProps<'div'> & { label: string }) {
  return (
    <div
      {...props}
      role='region'
      aria-label={label}
      tabIndex={0}
      className={cn(
        'overflow-auto rounded border focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
        className
      )}
    >
      {children}
    </div>
  );
}

/* oxlint-enable jsx-a11y/no-noninteractive-tabindex */
