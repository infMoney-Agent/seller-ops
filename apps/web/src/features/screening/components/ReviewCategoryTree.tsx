'use client';
import { useEffect, useId, useRef, useState } from 'react';
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
  const [height, setHeight] = useState(240);
  const categoryList = useRef<HTMLUListElement>(null);
  useEffect(() => {
    const list = categoryList.current;
    if (!list) return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setHeight(Math.round(entry.contentRect.height));
    });
    observer.observe(list);
    return () => observer.disconnect();
  }, []);
  const [expandedPath, setExpandedPath] = useState<string[] | null>(null);
  const [previousSelection, setPreviousSelection] = useState(JSON.stringify(selected));
  if (previousSelection !== JSON.stringify(selected)) {
    setPreviousSelection(JSON.stringify(selected));
    setExpandedPath(selected);
  }
  const openPath =
    expandedPath ?? tree.data?.items.find((node) => node.children.length)?.path ?? [];
  function resizeHeight(value: number) {
    setHeight(Math.max(120, Math.min(640, value)));
  }
  function selectCategory(path: string[]) {
    setExpandedPath(path);
    onSelect(path);
  }

  return (
    <nav aria-label='商品分类目录' className='min-w-0'>
      <h3 className='mb-2 font-semibold'>商品分类</h3>
      <Button
        variant={selected.length ? 'ghost' : 'secondary'}
        className='mb-2 justify-start'
        disabled={busy}
        onClick={() => selectCategory([])}
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
      <ul
        data-review-category-list
        ref={categoryList}
        style={{ height }}
        className='min-h-[120px] max-h-[640px] resize-y space-y-1 overflow-auto overscroll-contain text-xs [scrollbar-gutter:stable]'
      >
        {tree.data?.items.map((node) => (
          <CategoryBranch
            key={node.id}
            node={node}
            openPath={openPath}
            onToggle={setExpandedPath}
            selected={selected}
            onSelect={selectCategory}
            busy={busy}
          />
        ))}
      </ul>
      <div className='mt-2 flex items-center gap-1'>
        <button
          type='button'
          aria-label='降低分类高度'
          className='size-7 shrink-0 rounded hover:bg-muted focus-visible:outline-2'
          disabled={height <= 120}
          onClick={() => resizeHeight(height - 40)}
        >
          −
        </button>
        <span className='flex-1 text-center text-[11px] text-muted-foreground'>
          拖动右下角调整高度
        </span>
        <button
          type='button'
          aria-label='增加分类高度'
          className='size-7 shrink-0 rounded hover:bg-muted focus-visible:outline-2'
          disabled={height >= 640}
          onClick={() => resizeHeight(height + 40)}
        >
          +
        </button>
      </div>
      <span className='sr-only' role='status'>
        分类区域高度 {Math.round(height)} 像素
      </span>
      {tree.data?.items.length === 0 && (
        <p className='p-2 text-xs text-muted-foreground'>当前范围没有商品</p>
      )}
    </nav>
  );
}
function CategoryBranch({
  node,
  openPath,
  onToggle,
  selected,
  onSelect,
  busy
}: {
  node: ReviewCategory;
  openPath: string[];
  onToggle: (path: string[]) => void;
  selected: string[];
  onSelect: (p: string[]) => void;
  busy: boolean;
}) {
  const id = useId();
  const active = JSON.stringify(selected) === JSON.stringify(node.path);
  const open = node.path.every((part, index) => openPath[index] === part);
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
            aria-label={(open ? '收起' : '展开') + (node.label || node.name)}
            aria-expanded={open}
            aria-controls={id}
            onClick={() => onToggle(open ? node.path.slice(0, -1) : node.path)}
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
          <span className='break-words leading-5'>{node.label || node.name}</span>
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
              openPath={openPath}
              onToggle={onToggle}
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
