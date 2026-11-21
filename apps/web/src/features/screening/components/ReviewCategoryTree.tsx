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
  const stopResize = useRef<() => void>(() => {});
  useEffect(() => () => stopResize.current(), []);
  function startResize(startY: number, doc: Document) {
    stopResize.current();
    const initialHeight = categoryList.current?.getBoundingClientRect().height || height;
    const previousSelection = doc.documentElement.style.userSelect;
    const previousCursor = doc.documentElement.style.cursor;
    doc.documentElement.style.userSelect = 'none';
    doc.documentElement.style.cursor = 'row-resize';
    const move = (event: MouseEvent) => resizeHeight(initialHeight + event.clientY - startY);
    const touchMove = (event: TouchEvent) => {
      if (event.touches.length === 1) {
        event.preventDefault();
        resizeHeight(initialHeight + event.touches[0].clientY - startY);
      }
    };
    const end = () => {
      doc.removeEventListener('mousemove', move);
      doc.removeEventListener('mouseup', end);
      doc.removeEventListener('touchmove', touchMove);
      doc.removeEventListener('touchend', end);
      doc.removeEventListener('touchcancel', end);
      doc.defaultView?.removeEventListener('blur', end);
      doc.documentElement.style.userSelect = previousSelection;
      doc.documentElement.style.cursor = previousCursor;
      stopResize.current = () => {};
    };
    doc.addEventListener('mousemove', move);
    doc.addEventListener('mouseup', end);
    doc.addEventListener('touchmove', touchMove, { passive: false });
    doc.addEventListener('touchend', end);
    doc.addEventListener('touchcancel', end);
    doc.defaultView?.addEventListener('blur', end);
    stopResize.current = end;
  }
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
        className='min-h-[120px] max-h-[640px] space-y-1 overflow-auto overscroll-contain text-xs [scrollbar-gutter:stable]'
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
      <button
        type='button'
        data-category-resize
        aria-label='拖动调整分类高度，上下方向键微调'
        title='上下拖动调整高度；双击还原'
        className='mt-1 flex h-4 w-full touch-none cursor-row-resize select-none items-center justify-center rounded border-b-2 border-transparent hover:border-primary focus-visible:border-primary focus-visible:outline-2 [-webkit-user-select:none]'
        onMouseDown={(event) => {
          if (event.button === 0) {
            event.preventDefault();
            startResize(event.clientY, event.currentTarget.ownerDocument);
          }
        }}
        onTouchStart={(event) => {
          if (event.touches.length === 1)
            startResize(event.touches[0].clientY, event.currentTarget.ownerDocument);
        }}
        onDoubleClick={() => resizeHeight(240)}
        onKeyDown={(event) => {
          if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
            event.preventDefault();
            resizeHeight(height + (event.key === 'ArrowDown' ? 20 : -20));
          } else if (event.key === 'Home') {
            event.preventDefault();
            resizeHeight(240);
          }
        }}
      >
        <span aria-hidden='true' className='h-0.5 w-10 rounded bg-muted-foreground/30' />
      </button>
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
