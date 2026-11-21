'use client';
import { useEffect, useId, useState } from 'react';
import { useScreeningQuery } from '../queries';
import type { ReviewCategory } from '../types';
import { Button } from './shared';
import { cn } from '@/lib/utils';
export default function ReviewCategoryTree({
  query,
  selected,
  onSelect,
  busy
}: {
  query: string;
  selected: string[];
  onSelect: (path: string[]) => void;
  busy: boolean;
}) {
  const tree = useScreeningQuery<{ items: ReviewCategory[] }>('/review-categories?' + query);
  return (
    <nav
      aria-label='商品分类目录'
      className='min-w-0 rounded-xl border bg-muted/20 p-3 @4xl:sticky @4xl:top-4'
    >
      <h3 className='mb-2 font-semibold'>商品分类</h3>
      <Button
        variant={selected.length ? 'ghost' : 'secondary'}
        className='mb-2 w-full justify-start'
        disabled={busy}
        onClick={() => onSelect([])}
      >
        全部商品
      </Button>
      {tree.isLoading && (
        <p role='status' className='py-3 text-xs text-muted-foreground'>
          正在加载类目…
        </p>
      )}
      {tree.isError && (
        <p role='alert' className='text-xs text-destructive'>
          {tree.error.message}
        </p>
      )}
      <ul className='max-h-[65dvh] space-y-1 overflow-auto overscroll-contain text-xs'>
        {tree.data?.items.map((node) => (
          <CategoryBranch
            key={node.id}
            node={node}
            depth={0}
            selected={selected}
            onSelect={onSelect}
            busy={busy}
          />
        ))}
      </ul>
      {tree.data?.items.length === 0 && (
        <p className='p-2 text-xs text-muted-foreground'>当前范围没有商品</p>
      )}
    </nav>
  );
}
function CategoryBranch({
  node,
  depth,
  selected,
  onSelect,
  busy
}: {
  node: ReviewCategory;
  depth: number;
  selected: string[];
  onSelect: (p: string[]) => void;
  busy: boolean;
}) {
  const [open, setOpen] = useState(depth === 0);
  const id = useId();
  const active = JSON.stringify(selected) === JSON.stringify(node.path);
  const ancestor = node.path.every((p, i) => selected[i] === p);
  useEffect(() => {
    if (ancestor) setOpen(true);
  }, [ancestor]);
  return (
    <li>
      <div
        className={cn(
          'flex min-w-0 items-start rounded-md',
          active && 'bg-primary/10 text-primary'
        )}
      >
        {node.children.length > 0 ? (
          <button
            type='button'
            className='mt-0.5 size-7 shrink-0 rounded hover:bg-muted focus-visible:outline-2'
            aria-label={(open ? '收起' : '展开') + node.name}
            aria-expanded={open}
            aria-controls={id}
            onClick={() => setOpen(!open)}
          >
            {open ? '▾' : '▸'}
          </button>
        ) : (
          <span className='w-7 shrink-0' />
        )}
        <button
          type='button'
          aria-current={active ? 'true' : undefined}
          disabled={busy}
          className='min-h-8 min-w-0 flex-1 rounded px-1 py-1 text-left hover:bg-muted focus-visible:outline-2'
          onClick={() => onSelect(node.path)}
        >
          <span className='break-words leading-5'>{node.name}</span>
          <span className='ms-1 whitespace-nowrap text-[11px] text-muted-foreground'>
            {node.products} · 待 {node.pending}
          </span>
        </button>
      </div>
      {node.children.length > 0 && (
        <ul id={id} hidden={!open} className='ms-3 border-s ps-1'>
          {node.children.map((child) => (
            <CategoryBranch
              key={child.id}
              node={child}
              depth={depth + 1}
              selected={selected}
              onSelect={onSelect}
              busy={busy}
            />
          ))}
        </ul>
      )}
    </li>
  );
}
