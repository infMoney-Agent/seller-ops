'use client';
import type { ReactNode } from 'react';
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
    <span className='bg-muted rounded px-2 py-1 text-xs'>
      {value ? labels[value] || value : '未初筛'}
    </span>
  );
}
export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className='flex min-w-0 flex-col gap-1 text-sm'>
      <span className='text-muted-foreground'>{label}</span>
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
