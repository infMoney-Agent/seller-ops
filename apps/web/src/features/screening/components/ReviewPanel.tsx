'use client';
import { useCallback, useEffect, useId, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useScreeningQuery } from '../queries';
import { request } from '../service';
import type {
  BatchRecord,
  BatchResult,
  PageResult,
  Product,
  ReviewWorkspace,
  ReviewCategory
} from '../types';
import { Badge, Button, Field, TableScroll, type Action } from './shared';
import ReviewProductCard from './ReviewProductCard';
import ReviewCategoryTree from './ReviewCategoryTree';
import BatchReviewDialog from './BatchReviewDialog';

export default function ReviewPanel({
  selected,
  filters,
  act,
  busy
}: {
  selected: string[];
  filters: Record<string, string>;
  act: Action;
  busy: boolean;
}) {
  const [categoriesOpen, setCategoriesOpen] = useState(true);
  const categoryID = useId();
  const [referenceContainer, setReferenceContainer] = useState<HTMLDivElement | null>(null);
  const [seed] = useState(selected);
  const [scope, setScope] = useState(seed.length ? 'selected' : 'filtered');
  const [categoryPath, setCategoryPath] = useState<string[]>([]);
  const [memberPage, setMemberPage] = useState(1);
  const [samplePage, setSamplePage] = useState(1);
  const params = new URLSearchParams(filters);
  params.delete('page');
  params.delete('page_size');
  params.delete('show_merged');
  params.delete('sort');
  params.set('member_page', String(memberPage));
  params.set('sample_page', String(samplePage));
  if (scope === 'selected') params.set('selected_ids', seed.join(','));
  const categoryParams = new URLSearchParams(params);
  for (const key of ['group_id', 'group_page', 'member_page', 'sample_page'])
    categoryParams.delete(key);
  if (categoryPath.length) params.set('category_path', JSON.stringify(categoryPath));
  const workspace = useScreeningQuery<ReviewWorkspace>('/review-workspace?' + params);
  const data = workspace.data;
  const categories = useScreeningQuery<{ items: ReviewCategory[] }>(
    '/review-categories?' + categoryParams
  );
  const leaves: ReviewCategory[] = [];
  function collect(nodes: ReviewCategory[]) {
    for (const node of nodes) {
      if (node.children.length) collect(node.children);
      else if (node.products > 0) leaves.push(node);
    }
  }
  collect(categories.data?.items || []);
  const [previousCategories, setPreviousCategories] = useState(categories.data);
  if (categories.data && categories.data !== previousCategories) {
    setPreviousCategories(categories.data);
    const containsSelection = (nodes: ReviewCategory[]): boolean =>
      nodes.some(
        (node) =>
          JSON.stringify(node.path) === JSON.stringify(categoryPath) ||
          containsSelection(node.children)
      );
    if (categoryPath.length && !containsSelection(categories.data.items)) {
      setCategoryPath(leaves[0]?.path || []);
      setMemberPage(1);
      setSamplePage(1);
    }
  }
  const currentIndex = leaves.findIndex(
    (node) => JSON.stringify(node.path) === JSON.stringify(categoryPath)
  );
  const descendantIndex = leaves.findIndex((node) =>
    categoryPath.every((part, i) => node.path[i] === part)
  );
  const previous = leaves[(currentIndex >= 0 ? currentIndex : descendantIndex) - 1];
  const next = leaves[currentIndex >= 0 ? currentIndex + 1 : descendantIndex];
  function selectCategory(path: string[]) {
    setCategoryPath(path);
    setMemberPage(1);
    setSamplePage(1);
  }
  return (
    <div className='@container min-w-0'>
      <div
        className='grid min-w-0 items-start gap-4 @3xl:grid-cols-[minmax(13rem,16rem)_minmax(0,1fr)]'
        data-review-workspace
      >
        <aside
          className='max-h-[calc(100dvh-7rem)] min-w-0 space-y-4 overflow-y-auto overscroll-contain @3xl:sticky @3xl:top-20 [scrollbar-gutter:stable]'
          data-review-category-sidebar
        >
          <div className='rounded-xl border bg-muted/30 p-3'>
            <div className='flex items-center justify-between gap-2'>
              <h3 className='font-semibold'>商品分类</h3>
              <Button
                size='sm'
                variant='outline'
                aria-expanded={categoriesOpen}
                aria-controls={categoryID}
                onClick={() => setCategoriesOpen(!categoriesOpen)}
              >
                {categoriesOpen ? '收起分类' : '展开分类'}
              </Button>
            </div>
            <div
              id={categoryID}
              hidden={!categoriesOpen}
              className='mt-2'
              data-review-category-content
            >
              <ReviewCategoryTree
                query={categoryParams.toString()}
                selected={categoryPath}
                busy={busy}
                onSelect={selectCategory}
              />
            </div>
          </div>
          <div ref={setReferenceContainer} />
        </aside>
        <div className='@container min-w-0 space-y-4' data-review-content>
          <section className='space-y-2 rounded-xl border bg-muted/30 p-3'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div className='flex items-center gap-2'>
                <Button
                  size='sm'
                  variant='outline'
                  disabled={busy || !previous}
                  onClick={() => previous && selectCategory(previous.path)}
                >
                  上一组
                </Button>
                <span className='text-xs text-muted-foreground'>
                  {currentIndex >= 0
                    ? currentIndex + 1 + ' / ' + leaves.length
                    : leaves.length + ' 个类目'}
                </span>
                <Button
                  size='sm'
                  variant='outline'
                  disabled={busy || !next}
                  onClick={() => next && selectCategory(next.path)}
                >
                  下一组
                </Button>
              </div>
            </div>
            <h3 className='break-words text-sm font-semibold' data-review-category-title>
              {categoryPath.length ? data?.group?.title || categoryPath.join(' / ') : '全部商品'}
            </h3>
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <p className='text-sm text-muted-foreground' role='status'>
                {data
                  ? data.counts.products.toLocaleString() +
                    ' 个子体 · 待复核 ' +
                    data.counts.pending.toLocaleString()
                  : '正在加载类目商品…'}
              </p>
            </div>
            <div className='flex flex-wrap items-end gap-3'>
              {seed.length > 0 && (
                <Field label='商品范围'>
                  <select
                    aria-label='复核商品范围'
                    className='h-10 rounded-md border bg-background px-3'
                    value={scope}
                    onChange={(e) => {
                      setScope(e.target.value);
                      setMemberPage(1);
                      setSamplePage(1);
                    }}
                  >
                    <option value='selected'>商品库已选 {seed.length} 项</option>
                    <option value='filtered'>当前筛选结果</option>
                  </select>
                </Field>
              )}
            </div>
          </section>
          {workspace.isError && (
            <p role='alert' className='text-destructive'>
              {workspace.error.message}
            </p>
          )}
          {workspace.isLoading && (
            <p role='status' className='rounded-xl border p-6 text-sm text-muted-foreground'>
              正在加载商品与参考样本…
            </p>
          )}
          {data?.group && (
            <GroupReview
              key={data.group.id + ':' + scope + ':' + JSON.stringify(categoryPath)}
              data={data}
              seed={seed}
              busy={busy || workspace.isFetching}
              act={act}
              memberPage={setMemberPage}
              samplePage={setSamplePage}
              referenceContainer={referenceContainer}
              familyQuery={params.toString()}
            />
          )}
          {data && !data.group && (
            <p className='rounded-xl border p-8 text-center text-sm text-muted-foreground'>
              当前范围暂无商品，请调整商品库筛选或先导入商品。
            </p>
          )}
          <ReviewHistory act={act} busy={busy} />
        </div>
      </div>
    </div>
  );
}

function GroupReview({
  data,
  seed,
  busy,
  act,
  memberPage,
  samplePage,
  referenceContainer,
  familyQuery
}: {
  data: ReviewWorkspace;
  seed: string[];
  busy: boolean;
  act: Action;
  memberPage: (page: number) => void;
  samplePage: (page: number) => void;
  referenceContainer: HTMLDivElement | null;
  familyQuery: string;
}) {
  const [selected, setSelected] = useState<string[]>(() =>
    data.items.filter((p) => seed.includes(p.id) && p.review_status === 'pending').map((p) => p.id)
  );
  const [reference, setReference] = useState<Product | null>(null);
  const [compare, setCompare] = useState<Product[]>([]);
  const [reviewOpen, setReviewOpen] = useState(false);
  const [visibleFamilies, setVisibleFamilies] = useState<Record<string, Product[]>>({});
  const familyVisible = useCallback((id: string, products: Product[]) => {
    setVisibleFamilies((old) => ({ ...old, [id]: products }));
  }, []);
  const items = data.items || [],
    samples = data.samples.items || [];
  const currentReference = samples.find((p) => p.id === reference?.id) || reference;
  const displayed = [
    ...new Map(
      items.flatMap((p) => [p, ...(visibleFamilies[p.id] || [])]).map((p) => [p.id, p])
    ).values()
  ];
  const eligible = displayed.filter((p) => p.review_status === 'pending');
  const allChecked = eligible.length > 0 && eligible.every((p) => selected.includes(p.id));
  function toggle(id: string, checked: boolean) {
    setSelected((old) =>
      checked ? [...old.filter((x) => x !== id), id].slice(0, 100) : old.filter((x) => x !== id)
    );
  }
  return (
    <div className='space-y-4' data-review-group={data.group?.id}>
      <div className='flex flex-wrap items-center justify-between gap-3 rounded-xl border px-4 py-2'>
        <div className='flex flex-wrap items-center gap-4'>
          <label className='flex min-h-10 items-center gap-2 text-sm'>
            <input
              type='checkbox'
              className='size-4 accent-primary'
              aria-label='全选本页可处理商品'
              disabled={busy || !eligible.length}
              checked={allChecked}
              ref={(node) => {
                if (node)
                  node.indeterminate = !allChecked && eligible.some((p) => selected.includes(p.id));
              }}
              onChange={(e) => {
                setSelected((old) =>
                  e.target.checked
                    ? [...new Set([...old, ...eligible.map((p) => p.id)])].slice(0, 100)
                    : old.filter((id) => !eligible.some((p) => p.id === id))
                );
              }}
            />
            全选本页可处理项
          </label>
          <Button
            size='sm'
            variant='ghost'
            disabled={!selected.length || busy}
            onClick={() => {
              setSelected([]);
            }}
          >
            清空勾选
          </Button>
        </div>
        <p className='text-xs text-muted-foreground'>
          本类目 {data.group?.products} 项 · 待复核 {data.group?.pending} · 本页可选{' '}
          {eligible.length} 项
        </p>
      </div>
      <div className='min-w-0 space-y-4'>
        {referenceContainer &&
          createPortal(
            <aside className='min-w-0 rounded-xl border bg-muted/20 p-3' aria-label='人工参考样本'>
              <details open>
                <summary className='cursor-pointer py-1 font-semibold'>
                  已复核的参考样本{' '}
                  <span className='text-xs font-normal text-muted-foreground'>
                    ({data.samples.total})
                  </span>
                </summary>
                {!samples.length && (
                  <p className='py-5 text-sm leading-relaxed text-muted-foreground'>
                    本类目暂无参考样本，可直接看图填写共同结论。
                  </p>
                )}
                <div className='max-h-[38dvh] space-y-3 overflow-y-auto overscroll-contain pt-3 [scrollbar-gutter:stable]'>
                  {samples.map((p) => (
                    <ReviewProductCard
                      key={p.id}
                      product={p}
                      compact
                      checked={currentReference?.id === p.id}
                    >
                      <Button
                        size='sm'
                        variant={currentReference?.id === p.id ? 'default' : 'outline'}
                        disabled={busy}
                        onClick={() => {
                          setReference(p);
                        }}
                      >
                        {currentReference?.id === p.id ? '当前参考样本' : '采用此样本'}
                      </Button>
                      <label className='flex min-h-8 items-center gap-2 text-xs'>
                        <input
                          type='checkbox'
                          className='size-4 accent-primary'
                          aria-label={'比较样本 ' + p.asin}
                          checked={compare.some((x) => x.id === p.id)}
                          disabled={busy}
                          onChange={(e) =>
                            setCompare((old) =>
                              e.target.checked
                                ? [...old.filter((x) => x.id !== p.id).slice(-1), p]
                                : old.filter((x) => x.id !== p.id)
                            )
                          }
                        />
                        对比
                      </label>
                    </ReviewProductCard>
                  ))}
                </div>
                {data.samples.total > 50 && (
                  <div className='mt-3 flex flex-wrap items-center justify-between gap-2'>
                    <Button
                      size='sm'
                      variant='outline'
                      disabled={busy || data.samples.page <= 1}
                      onClick={() => samplePage(data.samples.page - 1)}
                    >
                      前50个样本
                    </Button>
                    <span className='text-xs'>
                      {data.samples.page} / {Math.ceil(data.samples.total / 50)}
                    </span>
                    <Button
                      size='sm'
                      variant='outline'
                      disabled={busy || data.samples.page * 50 >= data.samples.total}
                      onClick={() => samplePage(data.samples.page + 1)}
                    >
                      后50个样本
                    </Button>
                  </div>
                )}
              </details>
            </aside>,
            referenceContainer
          )}
        <TableScroll
          label='本类目候选商品'
          className='max-h-[62dvh] min-w-0 space-y-4 overscroll-contain border-0 pe-2 [scrollbar-gutter:stable]'
        >
          <div>
            <h3 className='break-words text-lg font-semibold'>{data.group?.title}</h3>
            <p className='mt-1 text-xs text-muted-foreground'>
              同父体子体默认展开，排除时同步处理同父体待复核子体。
            </p>
          </div>
          {compare.length === 2 && (
            <section className='space-y-3 rounded-xl border p-3' aria-label='样本对比'>
              <div className='flex items-center justify-between gap-2'>
                <h4 className='text-sm font-semibold'>样本对比</h4>
                <Button size='sm' variant='ghost' onClick={() => setCompare([])}>
                  关闭对比
                </Button>
              </div>
              <div className='grid min-w-0 gap-3 @3xl:grid-cols-2'>
                {compare.map((p) => (
                  <ReviewProductCard key={p.id} product={p} />
                ))}
              </div>
            </section>
          )}
          {currentReference && (
            <section className='space-y-2 rounded-xl border border-emerald-500/30 bg-emerald-500/5 p-3'>
              <h4 className='text-sm font-semibold text-emerald-700 dark:text-emerald-300'>
                当前参考样本
              </h4>
              <ReviewProductCard product={currentReference} />
            </section>
          )}
          <div className='min-w-0 overflow-clip rounded-lg border' data-review-rows>
            {items.map((p) => (
              <ReviewFamilyCard
                key={p.id}
                product={p}
                query={familyQuery}
                selected={selected}
                busy={busy}
                toggle={toggle}
                onVisible={familyVisible}
              />
            ))}
          </div>
          <div className='flex flex-wrap items-center justify-between gap-2 rounded-xl border p-3'>
            <Button
              variant='outline'
              disabled={busy || data.page <= 1}
              onClick={() => memberPage(data.page - 1)}
            >
              上一页商品
            </Button>
            <span className='text-xs text-muted-foreground'>
              第 {data.page} /{' '}
              {Math.max(
                1,
                Math.ceil((data.group?.display_products ?? data.group?.products ?? 0) / 50)
              )}{' '}
              页 · 每页50个父体／独立商品
            </span>
            <Button
              variant='outline'
              disabled={
                busy ||
                data.page * 50 >= (data.group?.display_products ?? data.group?.products ?? 0)
              }
              onClick={() => memberPage(data.page + 1)}
            >
              下一页商品
            </Button>
          </div>
        </TableScroll>
      </div>
      <BatchReviewDialog
        open={reviewOpen}
        onClose={() => setReviewOpen(false)}
        selected={selected}
        reference={currentReference}
        excludeFamilies
        act={act}
        busy={busy}
        onApplied={() => {
          setSelected([]);
        }}
      />
      <div
        className='sticky bottom-0 z-10 flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-background/95 px-4 py-3 shadow-lg backdrop-blur-sm'
        data-review-toolbar
      >
        <div>
          <p className='font-semibold' role='status'>
            已选 {selected.length} / 100 项
          </p>
        </div>
        <Button disabled={busy || !selected.length} onClick={() => setReviewOpen(true)}>
          批量复核
        </Button>
      </div>
    </div>
  );
}

function ReviewFamilyCard({
  product,
  query,
  selected,
  busy,
  toggle,
  onVisible
}: {
  product: Product;
  query: string;
  selected: string[];
  busy: boolean;
  toggle: (id: string, checked: boolean) => void;
  onVisible: (id: string, products: Product[]) => void;
}) {
  const [open, setOpen] = useState((product.family_count || 1) > 1);
  const [page, setPage] = useState(1);
  const contentID = useId();
  const params = new URLSearchParams(query);
  params.set('family_product_id', product.id);
  params.set('member_page', String(page));
  const initialMembers = product.family_items;
  const family = useScreeningQuery<ReviewWorkspace>(
    open && (page > 1 || !initialMembers) ? '/review-workspace?' + params : null
  );
  const members = page === 1 && initialMembers ? initialMembers : family.data?.items;
  useEffect(() => {
    onVisible(product.id, open ? members || [] : []);
  }, [onVisible, product.id, open, members]);
  function card(p: Product, asinActions?: ReactNode) {
    return (
      <ReviewProductCard
        key={p.id}
        product={p}
        row
        asinActions={asinActions}
        checked={selected.includes(p.id)}
        disabled={
          busy ||
          p.review_status !== 'pending' ||
          (selected.length >= 100 && !selected.includes(p.id))
        }
        onCheck={(checked) => toggle(p.id, checked)}
      />
    );
  }
  return (
    <div className='min-w-0' data-review-family={product.id}>
      {card(
        product,
        (product.family_count || 1) > 1 ? (
          <Button
            size='sm'
            variant='ghost'
            className='h-7 gap-1 px-2 text-xs'
            aria-expanded={open}
            aria-controls={contentID}
            aria-label={(open ? '收起' : '展开') + ' ' + product.parent_asin + ' 的子体'}
            disabled={busy}
            onClick={() => setOpen(!open)}
          >
            {open ? '▾ 收起' : '▸'} {product.family_count} 个子体
          </Button>
        ) : null
      )}
      <div id={contentID} hidden={!open}>
        {family.isLoading && (
          <p role='status' className='p-3 text-sm text-muted-foreground'>
            正在加载同父体商品…
          </p>
        )}
        {family.isError && (
          <div role='alert' className='text-sm text-destructive'>
            {family.error.message}
            <Button variant='outline' size='sm' onClick={() => void family.refetch()}>
              重试
            </Button>
          </div>
        )}
        {members?.filter((p) => p.id !== product.id).map((p) => card(p))}
        {(product.family_count || 0) > 50 && (
          <div className='flex items-center justify-between gap-2'>
            <Button
              size='sm'
              variant='outline'
              disabled={busy || family.isFetching || page <= 1}
              onClick={() => setPage(page - 1)}
            >
              上一页子体
            </Button>
            <span className='text-xs'>
              {page} / {Math.ceil((product.family_count || 0) / 50)}
            </span>
            <Button
              size='sm'
              variant='outline'
              disabled={busy || family.isFetching || page * 50 >= (product.family_count || 0)}
              onClick={() => setPage(page + 1)}
            >
              下一页子体
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

function ReviewHistory({ act, busy }: { act: Action; busy: boolean }) {
  const history = useScreeningQuery<PageResult<BatchRecord>>('/review-batches');
  return (
    <details className='rounded-xl border p-4'>
      <summary className='cursor-pointer text-sm font-semibold'>批量操作历史</summary>
      <div className='mt-3 space-y-2'>
        {history.isError && <p role='alert'>{history.error.message}</p>}
        {history.data?.items.length === 0 && (
          <p className='text-sm text-muted-foreground'>暂无批量操作</p>
        )}
        {history.data?.items.map((b) => (
          <div
            key={b.id}
            className='flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3'
          >
            <div className='flex flex-wrap items-center gap-2 text-sm'>
              <span>{new Date(b.created_at).toLocaleString()}</span>
              <Badge value={b.state} />
              <span className='text-xs text-muted-foreground'>
                {b.result ? '成功 ' + b.result.success + ' · 跳过 ' + b.result.skipped : '尚未执行'}
              </span>
            </div>
            {b.state === 'applied' && (
              <Button
                size='sm'
                variant='outline'
                disabled={busy}
                onClick={() =>
                  void act(async () => {
                    await request<BatchResult>('/review-batches/' + b.id + '/undo', 'POST');
                  }, '已撤销可恢复的修改')
                }
              >
                撤销此批次
              </Button>
            )}
          </div>
        ))}
      </div>
    </details>
  );
}
