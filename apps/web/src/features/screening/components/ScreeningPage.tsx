'use client';
/* oxlint-disable react/no-unstable-nested-components -- TanStack column renderers are stable useMemo callbacks. */
import {
  Fragment,
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState
} from 'react';
import Image from 'next/image';
import { useQueryClient } from '@tanstack/react-query';
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type ColumnSizingState,
  type OnChangeFn,
  type RowSelectionState
} from '@tanstack/react-table';
import PageContainer from '@/components/layout/page-container';
import { Icons } from '@/components/icons';
import { Dialog, DialogTrigger } from '@/components/ui/dialog';
import DataDialogContent from './DataDialogContent';
import { useSellerSession } from '@/features/auth/session';
import { cn } from '@/lib/utils';
import { useJob, useScreeningQuery } from '../queries';
import { download, request } from '../service';
import type { ImportRecord, JobCreated, PageResult, Policy, Product, FamilyResult } from '../types';
import { childSales } from '../sales';
import { productTrademark, trademarkOptionLabel } from '../trademark';
import { useTableTextAlignment } from '../use-table-text-alignment';
import { countryCode, listingAge, shanghaiToday } from '../product-display';
import AsinTools from './AsinTools';
import ImportDialog from './ImportDialog';
import ReviewDrawer from './ReviewDrawer';
import ReviewPanel from './ReviewPanel';
import BilingualProductTitle from './BilingualProductTitle';
import BatchReviewDialog from './BatchReviewDialog';
import ProfitPanel from './ProfitPanel';
import PolicyPanel from './PolicyPanel';
import ParentMonthlyPanel from './ParentMonthlyPanel';
import MonthlyHistory, { periodLabels } from './MonthlyHistory';
import {
  Badge,
  Button,
  DataBlock,
  Field,
  Input,
  TableScroll,
  formatMoney,
  labels,
  type Action
} from './shared';

const ChildRowContext = createContext<{ isChild: boolean; index: number }>({
  isChild: false,
  index: 0
});

function stickyColumnClass(columnId: string, isHeader: boolean, isSelected = false) {
  const bodyBg = isSelected ? 'bg-muted' : 'bg-card group-hover/row:bg-muted';
  if (columnId === 'selection') {
    return cn('md:sticky md:left-0', isHeader ? 'bg-muted md:z-40' : cn(bodyBg, 'md:z-10'));
  }
  if (columnId === 'asin') {
    return cn('md:sticky md:left-10', isHeader ? 'bg-muted md:z-40' : cn(bodyBg, 'md:z-10'));
  }
  if (columnId === 'actions') {
    return cn('md:sticky md:right-0', isHeader ? 'bg-muted md:z-40' : cn(bodyBg, 'md:z-10'));
  }
  return undefined;
}

const minimumColumnWidths: ColumnSizingState = {
  selection: 40,
  asin: 164,
  price_usd: 104,
  monthly_sales: 88,
  rating_reviews: 120,
  listing_date: 90,
  dimensions: 124,
  seller: 92,
  decision: 96,
  review_status: 92,
  margin: 84,
  profit: 96,
  actions: 64
};
function fittedColumnWidths(width: number): ColumnSizingState {
  const minimum = Object.values(minimumColumnWidths).reduce((a, b) => a + b, 0);
  const extra = Math.max(0, Math.floor(width) - minimum);
  const weights: ColumnSizingState = {
    asin: 5,
    dimensions: 2,
    seller: 1,
    decision: 1,
    review_status: 1,
    price_usd: 1,
    profit: 1
  };
  const result = { ...minimumColumnWidths };
  let assigned = 0;
  for (const [key, weight] of Object.entries(weights)) {
    const addition = Math.floor((extra * weight) / 12);
    result[key] += addition;
    assigned += addition;
  }
  result.asin += extra - assigned;
  return result;
}

const sortFields: Record<string, string> = {
  price_usd: 'price_usd',
  monthly_sales: 'monthly_sales',
  rating_reviews: 'rating',
  margin: 'margin',
  profit: 'profit_usd'
};
function sortDirection(
  sort: string | undefined,
  column: string
): 'ascending' | 'descending' | undefined {
  const field = sortFields[column];
  return field && sort === field
    ? 'ascending'
    : field && sort === '-' + field
      ? 'descending'
      : undefined;
}
const tabs = [
  { id: 0, title: '商品库' },
  { id: 2, title: '分组复核' },
  { id: 3, title: '利润测算' },
  { id: 4, title: '规则与历史' },
  { id: 5, title: '父体数据' }
];
export default function ScreeningPage() {
  const session = useSellerSession();
  const [today, setToday] = useState('');
  useEffect(() => {
    const refresh = () => setToday(shanghaiToday());
    refresh();
    const timer = setInterval(refresh, 60000);
    return () => clearInterval(timer);
  }, []);
  const client = useQueryClient();
  const [tab, setTab] = useState(0);
  const [importOpen, setImportOpen] = useState(false);
  const [batchReviewOpen, setBatchReviewOpen] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);
  const [filters, setFilters] = useState<Record<string, string>>({});
  const [selected, setSelected] = useState<RowSelectionState>({});
  const [columnSizing, setColumnSizing] = useState<ColumnSizingState>({});
  const [tableContainer, setTableContainer] = useState<HTMLDivElement | null>(null);
  const [tableWidth, setTableWidth] = useState(0);
  useTableTextAlignment(tableContainer);
  useEffect(() => {
    if (!tableContainer) return;
    const measure = () => setTableWidth(Math.floor(tableContainer.clientWidth));
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(tableContainer);
    return () => observer.disconnect();
  }, [tableContainer]);
  const defaultColumnWidths = useMemo(() => fittedColumnWidths(tableWidth), [tableWidth]);
  function resetColumnWidth(id: string) {
    setColumnSizing((old) => {
      const next = { ...old };
      delete next[id];
      return next;
    });
  }
  const [familyRows, setFamilyRows] = useState<Record<string, boolean>>({});
  const [focus, setFocus] = useState<Product | null>(null);
  const [reviewOpen, setReviewOpen] = useState(false);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const openReview = useCallback((product: Product) => {
    setFocus(product);
    setReviewOpen(true);
  }, []);
  const openProfit = useCallback((product: Product) => {
    setReviewOpen(false);
    setFocus(product);
    setTab(3);
  }, []);
  useEffect(() => {
    if (tab !== 0) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable)
      ) {
        return;
      }
      if (
        document.querySelector(
          'dialog[open], [data-slot="dialog-content"][data-open], [data-slot="sheet-content"]'
        )
      ) {
        return;
      }
      event.preventDefault();
      searchInputRef.current?.focus();
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [tab]);
  const [group, setGroup] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [jobs, setJobs] = useState<string[]>([]);
  const [showJob, setShowJob] = useState(true);
  const [activeJob, setActiveJob] = useState<string | null>(null);
  const query = new URLSearchParams({
    ...filters,
    show_merged: 'false',
    page: String(page),
    page_size: String(pageSize)
  }).toString();
  const products = useScreeningQuery<PageResult<Product>>('/products?' + query, false, true);
  useEffect(() => {
    if (
      !products.data ||
      products.isPlaceholderData ||
      products.isFetching ||
      page * products.data.page_size >= products.data.total
    )
      return;
    const params = new URLSearchParams({
      ...filters,
      show_merged: 'false',
      page: String(page + 1),
      page_size: String(pageSize)
    });
    const path = '/products?' + params;
    const timer = setTimeout(
      () =>
        void client.prefetchQuery({
          queryKey: ['screening', session.workspaceId, path],
          queryFn: ({ signal }) =>
            request<PageResult<Product>>(path, 'GET', undefined, signal, 'low'),
          staleTime: 60000
        }),
      300
    );
    return () => clearTimeout(timer);
  }, [
    client,
    filters,
    page,
    pageSize,
    products.data,
    products.isPlaceholderData,
    products.isFetching,
    session.workspaceId
  ]);
  const counts = useScreeningQuery<Record<string, number>>(
    '/summary?' +
      new URLSearchParams(
        Object.fromEntries(Object.entries(filters).filter(([key]) => key !== 'sort'))
      )
  );
  const imports = useScreeningQuery<PageResult<ImportRecord>>('/imports');
  const policies = useScreeningQuery<{ items: Policy[] }>('/policies');
  const activePolicy = policies.data?.items.find((item) => item.active);
  const trademarks = useScreeningQuery<{
    items: { value: string; label: string; count: number }[];
  }>(
    '/trademark-options?' +
      new URLSearchParams(
        Object.fromEntries(
          Object.entries(filters).filter(([key]) => key !== 'trademark' && key !== 'sort')
        )
      )
  );
  const [groupPage, setGroupPage] = useState(1);
  const expanded = useScreeningQuery<PageResult<Product>>(
    group ? `/groups/${group}/products?page=${groupPage}&${new URLSearchParams(filters)}` : null
  );
  const job = useJob(activeJob);
  const advancedKeys = [
    'recommendation',
    'category',
    'seller_region',
    'direction',
    'child_sales_min',
    'profit_min',
    'profit_max',
    'import_id',
    'has_reference'
  ];
  const advancedCount = advancedKeys.filter((key) => Boolean(filters[key])).length;
  const filterCount = Object.entries(filters).filter(
    ([key, value]) => key !== 'sort' && value
  ).length;
  function resetFilters() {
    setFilters({});
    setPage(1);
    setSelected({});
    setFamilyRows({});
    setGroup(null);
    setFocus(null);
    setReviewOpen(false);
    setShowAdvanced(false);
  }
  const selectedIds = Object.keys(selected).filter((id) => selected[id]);
  const pageSelectionIds = useMemo(() => {
    const ids = new Set<string>();
    for (const product of products.data?.items ?? []) {
      ids.add(product.id);
      for (const member of product.family_member_ids ?? []) ids.add(member);
    }
    return [...ids];
  }, [products.data?.items]);
  const pageSelectedCount = pageSelectionIds.filter((id) => selected[id]).length;
  const hiddenSelectedVariantCount = useMemo(() => {
    let total = 0;
    for (const product of products.data?.items ?? []) {
      if (familyRows[product.id]) continue;
      for (const memberId of product.family_member_ids ?? []) {
        if (memberId !== product.id && selected[memberId]) total += 1;
      }
    }
    return total;
  }, [familyRows, products.data?.items, selected]);
  const expandablePageIds = useMemo(
    () => (products.data?.items ?? []).filter((p) => (p.family_count || 1) > 1).map((p) => p.id),
    [products.data?.items]
  );
  const allFamiliesExpanded =
    expandablePageIds.length > 0 && expandablePageIds.every((id) => Boolean(familyRows[id]));
  const toggleExpandAllFamilies = useCallback(() => {
    setFamilyRows((old) => {
      const next = { ...old };
      for (const id of expandablePageIds) {
        if (allFamiliesExpanded) delete next[id];
        else next[id] = true;
      }
      return next;
    });
  }, [allFamiliesExpanded, expandablePageIds]);
  const allPageSelected =
    pageSelectionIds.length > 0 && pageSelectedCount === pageSelectionIds.length;
  const toggleAllPage = useCallback(() => {
    setSelected((old) => {
      const next = { ...old };
      for (const id of pageSelectionIds) {
        if (allPageSelected) delete next[id];
        else next[id] = true;
      }
      return next;
    });
  }, [allPageSelected, pageSelectionIds]);
  const refresh = async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ['screening', session.workspaceId] }),
      client.invalidateQueries({ queryKey: ['screening-job', session.workspaceId] })
    ]);
  };
  const act: Action = async (work, success) => {
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const value = await work();
      if (success) setMessage(success);
      await refresh();
      return value;
    } catch (e) {
      setError(e instanceof Error ? e.message : '操作失败');
      return undefined;
    } finally {
      setBusy(false);
    }
  };
  function watch(id: string, visible = true) {
    setShowJob(visible);
    setJobs((old) => [...new Set([id, ...old])]);
    setActiveJob(id);
  }
  useEffect(() => {
    setSelected({});
    setFamilyRows({});
    setFocus(null);
    setReviewOpen(false);
    setImportOpen(false);
    setGroup(null);
    setJobs([]);
    setActiveJob(null);
    setPage(1);
  }, [session.workspaceId]);
  useEffect(() => {
    if (job.data && !['queued', 'running'].includes(job.data.state)) {
      void client.invalidateQueries({ queryKey: ['screening', session.workspaceId] });
      const next = job.data.result?.screen;
      if (
        next &&
        typeof next === 'object' &&
        !Array.isArray(next) &&
        typeof next.job_id === 'string'
      ) {
        setJobs((old) => [...new Set([next.job_id as string, ...old])]);
        setActiveJob(next.job_id);
      }
    }
  }, [job.data, client, session.workspaceId]);
  function filter(key: string, value: string) {
    setFilters((old) => ({ ...old, [key]: value }));
    setPage(1);
    setFamilyRows({});
  }
  const columns = useMemo<ColumnDef<Product>[]>(
    () => [
      {
        id: 'selection',
        enableResizing: false,
        size: 40,
        minSize: 40,
        maxSize: 40,
        header: () => (
          <SelectAllCheckbox
            label={`全选本页商品及同父体子体（共 ${pageSelectionIds.length} 项）`}
            disabled={products.isPlaceholderData || pageSelectionIds.length === 0}
            checked={allPageSelected}
            indeterminate={pageSelectedCount > 0 && !allPageSelected}
            onChange={toggleAllPage}
          />
        ),
        cell: ({ row }) => (
          <input
            aria-label={`选择 ${row.original.asin}`}
            type='checkbox'
            checked={row.getIsSelected()}
            onChange={row.getToggleSelectedHandler()}
          />
        )
      },
      {
        accessorKey: 'asin',
        header: '商品',
        cell: ({ row }) => (
          <ProductIdentityCell
            product={row.original}
            familyRows={familyRows}
            setFamilyRows={setFamilyRows}
            onOpenReview={openReview}
          />
        )
      },
      {
        accessorKey: 'price_usd',
        header: '价格 USD',
        cell: ({ row }) => <PriceHistoryCard product={row.original} onCalculate={openProfit} />
      },
      {
        accessorKey: 'monthly_sales',
        header: '子体销量',
        cell: ({ row }) => (
          <div className='space-y-1 tabular-nums'>
            <p>{childSales(row.original)}</p>
          </div>
        )
      },
      {
        id: 'rating_reviews',
        header: '评分 / 评分数',
        cell: ({ row }) => (
          <p className='whitespace-nowrap tabular-nums'>
            {row.original.rating == null
              ? '—'
              : Number(row.original.rating).toLocaleString('zh-CN', {
                  minimumFractionDigits: 1,
                  maximumFractionDigits: 2
                })}
            {' / '}
            {row.original.review_count?.toLocaleString('zh-CN') ?? '—'}
          </p>
        )
      },
      {
        accessorKey: 'listing_date',
        header: '上架时间',
        cell: ({ row }) => (
          <div className='space-y-1 tabular-nums'>
            <p>{row.original.listing_date?.trim() || '—'}</p>
            {listingAge(row.original.listing_date, today) != null && (
              <p className='text-muted-foreground text-xs'>
                距今 {listingAge(row.original.listing_date, today)?.toLocaleString('zh-CN')} 天
              </p>
            )}
          </div>
        )
      },
      {
        id: 'dimensions',
        header: '尺寸 / 重量',
        cell: ({ row }) => <ProductMeasurements product={row.original} />
      },
      {
        id: 'seller',
        header: '卖家',
        cell: ({ row }) => (
          <div className='min-w-36 max-w-52 space-y-1 text-xs leading-5'>
            <p className='break-words text-sm font-medium'>{row.original.seller_name || '—'}</p>
            <p className='text-muted-foreground flex flex-wrap items-center justify-center gap-1'>
              所属地：
              {countryCode(row.original.seller_region) && (
                <Image
                  unoptimized
                  src={'/images/flags/' + countryCode(row.original.seller_region) + '.svg'}
                  width={18}
                  height={12}
                  alt=''
                  aria-hidden='true'
                  className='inline-block h-3 w-[18px] shrink-0 rounded-[1px] object-contain'
                />
              )}
              {row.original.seller_region || '—'}
            </p>
            {row.original.seller_id && (
              <p className='text-muted-foreground break-all'>ID：{row.original.seller_id}</p>
            )}
          </div>
        )
      },
      {
        accessorKey: 'decision',
        header: '机器 / 推荐',
        cell: ({ row }) => (
          <div className='space-y-2'>
            <Badge value={row.original.decision} />
            <Badge value={row.original.recommendation} />
            <p className='max-w-48 text-xs'>{row.original.reasons?.join('；')}</p>
          </div>
        )
      },
      {
        accessorKey: 'review_status',
        header: '人工结论',
        cell: ({ row }) => {
          const isPending = row.original.review_status === 'pending';
          return (
            <div className='space-y-1'>
              <button
                type='button'
                title='点击修改人工复核结论'
                onClick={() => openReview(row.original)}
                className={cn(
                  'inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset transition-colors',
                  row.original.review_status === 'kept'
                    ? 'bg-emerald-50 text-emerald-700 ring-emerald-600/20 hover:bg-emerald-100/80 dark:bg-emerald-500/10 dark:text-emerald-300 dark:hover:bg-emerald-500/20'
                    : row.original.review_status === 'excluded'
                      ? 'bg-red-50 text-red-700 ring-red-600/20 hover:bg-red-100/80 dark:bg-red-500/10 dark:text-red-300 dark:hover:bg-red-500/20'
                      : 'bg-amber-50 text-amber-700 ring-amber-600/25 hover:bg-amber-100/80 dark:bg-amber-500/10 dark:text-amber-300 dark:hover:bg-amber-500/20'
                )}
              >
                {isPending && (
                  <span
                    aria-hidden='true'
                    className='size-1.5 animate-pulse rounded-full bg-amber-500'
                  />
                )}
                <span>{labels[row.original.review_status] || row.original.review_status}</span>
              </button>
              {row.original.review_reason && (
                <p className='text-muted-foreground mx-auto max-w-40 text-xs'>
                  {row.original.review_reason}
                </p>
              )}
              {row.original.has_reference && (
                <span className='text-muted-foreground block text-xs'>有人工样本</span>
              )}
            </div>
          );
        }
      },
      {
        id: 'margin',
        header: '利润率',
        cell: ({ row }) => {
          const margin = row.original.profit_output?.margin;
          return margin !== undefined && margin !== null && margin !== '' ? (
            <span
              className={cn(
                'font-mono font-semibold tabular-nums',
                Number(margin) >= 0.25 && 'text-emerald-600 dark:text-emerald-400'
              )}
            >
              {(Number(margin) * 100).toFixed(2)}%
            </span>
          ) : (
            <button
              type='button'
              onClick={() => openProfit(row.original)}
              className='text-muted-foreground hover:text-foreground text-xs underline decoration-dotted underline-offset-2'
            >
              待测算
            </button>
          );
        }
      },
      {
        id: 'profit',
        header: '预计利润',
        cell: ({ row }) => (
          <div className='space-y-0.5'>
            {row.original.profit_output?.profit_usd ? (
              <span className='font-mono font-semibold tabular-nums'>
                ${row.original.profit_output.profit_usd}
              </span>
            ) : (
              <button
                type='button'
                onClick={() => openProfit(row.original)}
                className='text-muted-foreground hover:text-foreground text-xs underline decoration-dotted underline-offset-2'
              >
                待测算
              </button>
            )}
            {row.original.profit_data_revision !== null &&
              (row.original.profit_data_revision !== row.original.data_revision ||
                row.original.profit_monthly_revision !== row.original.monthly_revision) && (
                <p className='text-amber-600 dark:text-amber-400 text-xs'>来源已更新</p>
              )}
          </div>
        )
      },
      {
        id: 'actions',
        header: '操作',
        cell: ({ row }) => (
          <div className='flex flex-col items-stretch gap-1 [&>button]:h-7 [&>button]:px-2 [&>button]:text-xs'>
            <Button
              size='sm'
              variant={row.original.review_status === 'pending' ? 'default' : 'outline'}
              onClick={() => openReview(row.original)}
            >
              复核
            </Button>
            <Button size='sm' variant='ghost' onClick={() => openProfit(row.original)}>
              测算
            </Button>
            {row.original.group_key && (
              <Button
                size='sm'
                variant='ghost'
                onClick={() => {
                  setGroup(row.original.group_key);
                  setGroupPage(1);
                }}
              >
                同组
              </Button>
            )}
          </div>
        )
      }
    ],
    [
      allPageSelected,
      familyRows,
      openProfit,
      openReview,
      pageSelectedCount,
      pageSelectionIds,
      products.isPlaceholderData,
      today,
      toggleAllPage
    ]
  );
  const sizedColumns = useMemo(
    () =>
      columns.map((column) => ({
        ...column,
        minSize:
          minimumColumnWidths[
            column.id ?? ('accessorKey' in column ? String(column.accessorKey) : '')
          ] ?? 40
      })),
    [columns]
  );
  // oxlint-disable-next-line react/incompatible-library -- TanStack Table is intentionally kept outside React Compiler memoization.
  const table = useReactTable({
    data: products.data?.items || [],
    columns: sizedColumns,
    state: { rowSelection: selected, columnSizing: { ...defaultColumnWidths, ...columnSizing } },
    onColumnSizingChange: (updater) =>
      setColumnSizing((old) => {
        const current = { ...defaultColumnWidths, ...old };
        const next = typeof updater === 'function' ? updater(current) : updater;
        return Object.fromEntries(
          Object.entries(next).filter(([key, value]) => value !== defaultColumnWidths[key])
        );
      }),
    defaultColumn: { minSize: 40, maxSize: 900 },
    columnResizeMode: 'onChange',
    enableColumnResizing: true,
    onRowSelectionChange: setSelected,
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    enableRowSelection: true
  });
  return (
    <div className='w-full min-w-0 max-w-full'>
      <PageContainer
        pageTitle='产品初筛'
        pageDescription='导入、筛选、人工复核与利润测算，保留每次判断的来源。'
        pageHeaderAction={
          <div className='flex flex-wrap items-center justify-end gap-2'>
            <button
              type='button'
              title='查看或调整初筛规则版本'
              onClick={() => setTab(4)}
              className='bg-muted/70 text-foreground hover:border-primary/40 hidden items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors sm:inline-flex'
            >
              <span aria-hidden='true' className='size-1.5 rounded-full bg-emerald-500' />
              <span>
                {activePolicy
                  ? `规则库 ${activePolicy.name} v${activePolicy.version} · 机筛后人工复核`
                  : '规则库默认策略 · 机筛后人工复核'}
              </span>
            </button>
            <Button
              size='sm'
              variant='outline'
              disabled={products.isFetching}
              onClick={() => void refresh()}
            >
              <Icons.ecomRefreshCw
                aria-hidden='true'
                className={cn('size-3.5', products.isFetching && 'animate-spin')}
              />
              {products.isFetching ? '刷新中…' : '刷新'}
            </Button>
            {tab === 0 && (
              <Button size='sm' onClick={() => setImportOpen(true)}>
                <Icons.ecomPlus aria-hidden='true' className='size-3.5' />
                导入商品
              </Button>
            )}
          </div>
        }
      >
        <div className='min-w-0 max-w-full space-y-4'>
          <nav
            className='bg-muted/60 inline-flex flex-wrap items-center gap-1 rounded-xl border p-1'
            aria-label='初筛工作台'
          >
            {tabs.map(({ title, id: i }) => (
              <button
                key={title}
                type='button'
                onClick={() => setTab(i)}
                className={cn(
                  'rounded-lg px-3.5 py-1.5 text-xs font-medium transition-all',
                  tab === i
                    ? 'bg-primary text-primary-foreground font-semibold shadow-2xs'
                    : 'text-muted-foreground hover:text-foreground hover:bg-background/60'
                )}
              >
                {title}
              </button>
            ))}
          </nav>
          {error && (
            <div role='alert' className='border-destructive text-destructive rounded border p-3'>
              {error}
            </div>
          )}
          {message && (
            <div role='status' className='bg-muted rounded p-3 text-sm'>
              {message}
            </div>
          )}
          {activeJob && showJob && (
            <section className='space-y-2 rounded border p-3'>
              <div className='flex flex-wrap items-center gap-3'>
                <strong className='text-sm'>后台任务</strong>
                <select
                  aria-label='查看任务'
                  value={activeJob}
                  className='max-w-72 rounded border bg-transparent p-1 text-xs'
                  onChange={(e) => setActiveJob(e.target.value)}
                >
                  {jobs.map((id, i) => (
                    <option key={id} value={id}>
                      任务 {jobs.length - i}
                    </option>
                  ))}
                </select>
                <Badge value={job.data?.state || 'queued'} />
                <span>{job.data?.progress || 0}%</span>
                {job.data?.state === 'succeeded' && job.data.kind === 'export' && (
                  <Button
                    size='sm'
                    onClick={() =>
                      void act(() => download(`/exports/${activeJob}/download`, '产品初筛.xlsx'))
                    }
                  >
                    下载 Excel
                  </Button>
                )}
                {job.data?.state === 'failed' && (
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={busy}
                    onClick={() =>
                      void act(() => request(`/jobs/${activeJob}/retry`, 'POST'), '任务已重新排队')
                    }
                  >
                    重试
                  </Button>
                )}
              </div>
              {job.isError && <p role='alert'>{job.error.message}</p>}
              {job.data?.error && (
                <p role='alert' className='text-destructive text-sm'>
                  {job.data.error.message}
                </p>
              )}
              {job.data?.result && <DataBlock title='任务处理明细' value={job.data.result} />}
            </section>
          )}
          {tab === 0 && (
            <>
              <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
                <div className='bg-card rounded-xl border p-4 shadow-2xs'>
                  <div className='flex items-center justify-between gap-2'>
                    <span className='text-muted-foreground text-xs font-medium'>商品数</span>
                    <span className='bg-muted text-muted-foreground rounded-md px-2 py-0.5 text-[11px]'>
                      当前筛选结果
                    </span>
                  </div>
                  <p className='mt-2 font-mono text-2xl font-bold tabular-nums'>
                    {counts.data?.displayed?.toLocaleString('zh-CN') ?? '—'}
                  </p>
                  <p className='text-muted-foreground mt-1 text-xs'>按父体折叠聚合计数</p>
                </div>

                <div className='bg-card rounded-xl border p-4 shadow-2xs'>
                  <div className='flex items-center justify-between gap-2'>
                    <span className='text-muted-foreground text-xs font-medium'>子体商品数</span>
                    <span className='bg-muted text-muted-foreground rounded-md px-2 py-0.5 text-[11px]'>
                      全部变体
                    </span>
                  </div>
                  <p className='mt-2 font-mono text-2xl font-bold tabular-nums'>
                    {counts.data?.products?.toLocaleString('zh-CN') ?? '—'}
                  </p>
                  <p className='text-muted-foreground mt-1 text-xs'>含未展开变体（尺寸 / 颜色）</p>
                </div>

                <div className='from-amber-500/[0.08] via-card to-card rounded-xl border border-amber-500/35 bg-gradient-to-br p-4 shadow-2xs'>
                  <div className='flex items-center justify-between gap-2'>
                    <span className='inline-flex items-center gap-1.5 text-xs font-semibold text-amber-700 dark:text-amber-300'>
                      <span
                        aria-hidden='true'
                        className='size-2 animate-pulse rounded-full bg-amber-500'
                      />
                      <span>待复核</span>
                    </span>
                    <button
                      type='button'
                      onClick={() =>
                        filter(
                          'review_status',
                          filters.review_status === 'pending' ? '' : 'pending'
                        )
                      }
                      className='text-xs font-semibold text-amber-700 underline-offset-2 hover:underline dark:text-amber-300'
                    >
                      {filters.review_status === 'pending' ? '显示全部 →' : '仅看待复核 →'}
                    </button>
                  </div>
                  <p className='mt-2 font-mono text-2xl font-bold tabular-nums text-amber-700 dark:text-amber-300'>
                    {counts.data?.pending_displayed?.toLocaleString('zh-CN') ?? '—'}
                  </p>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    优先处理机器通过但尚未人工确认的商品
                  </p>
                </div>
              </div>
              <section
                aria-label='商品筛选'
                className='bg-card space-y-3 rounded-xl border p-4 shadow-2xs'
              >
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <p className='text-muted-foreground text-xs'>
                      默认最新一期 · 同父体折叠展示 · 点击价格查看分期历史
                    </p>
                    {filterCount > 0 && (
                      <span className='inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300'>
                        已应用筛选条件
                      </span>
                    )}
                  </div>
                  <Button size='sm' variant='ghost' disabled={!filterCount} onClick={resetFilters}>
                    重置筛选{filterCount ? `（${filterCount}）` : ''}
                  </Button>
                </div>
                <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
                  <Field label='关键词 / ASIN'>
                    <div className='relative flex items-center'>
                      <Icons.search
                        aria-hidden='true'
                        className='text-muted-foreground pointer-events-none absolute left-2.5 size-4'
                      />
                      <Input
                        ref={searchInputRef}
                        placeholder='搜索 ASIN 或商品标题'
                        className='pr-8 pl-8'
                        value={filters.q || ''}
                        onChange={(e) => filter('q', e.target.value)}
                      />
                      <kbd
                        aria-hidden='true'
                        className='bg-muted text-muted-foreground pointer-events-none absolute right-2 hidden rounded border px-1.5 py-0.5 font-mono text-[10px] sm:inline-block'
                      >
                        /
                      </kbd>
                    </div>
                  </Field>
                  <Field label='人工结论'>
                    <select
                      className='bg-background h-9 w-full min-w-0 rounded-md border px-2.5 text-sm'
                      value={filters.review_status || ''}
                      onChange={(e) => filter('review_status', e.target.value)}
                    >
                      <option value=''>全部</option>
                      {(['pending', 'kept', 'excluded', 'unsure'] as const).map((v, i) => (
                        <option key={v} value={v}>
                          {['待复核', '保留', '排除', '待确认'][i]}
                        </option>
                      ))}
                    </select>
                  </Field>
                  <Field label='机器结果'>
                    <select
                      className='bg-background h-9 w-full min-w-0 rounded-md border px-2.5 text-sm'
                      value={filters.machine || ''}
                      onChange={(e) => filter('machine', e.target.value)}
                    >
                      <option value=''>全部</option>
                      <option value='passed'>通过</option>
                      <option value='excluded'>排除</option>
                      <option value='insufficient'>信息不足</option>
                      <option value='unscreened'>未初筛</option>
                    </select>
                  </Field>
                  <Field label='商标分类'>
                    <select
                      name='trademark'
                      className='bg-background h-9 w-full min-w-0 rounded-md border px-2.5 text-sm'
                      value={filters.trademark || ''}
                      onChange={(e) => {
                        filter('trademark', e.target.value);
                        setFocus(null);
                        setSelected({});
                      }}
                    >
                      <option value=''>全部商标分类</option>
                      {filters.trademark &&
                        !trademarks.data?.items.some(
                          (item) => item.value === filters.trademark
                        ) && (
                          <option value={filters.trademark}>
                            {trademarkOptionLabel(filters.trademark)}
                          </option>
                        )}
                      {(trademarks.data?.items || []).map((item) => (
                        <option key={item.value} value={item.value}>
                          {trademarkOptionLabel(item.value)} · {item.count.toLocaleString('zh-CN')}
                        </option>
                      ))}
                    </select>
                  </Field>
                </div>
                <div className='flex items-center justify-end gap-3'>
                  <Button
                    size='sm'
                    variant='ghost'
                    aria-expanded={showAdvanced}
                    aria-controls='screening-advanced-filters'
                    onClick={() => setShowAdvanced((v) => !v)}
                  >
                    {showAdvanced ? '收起筛选' : '更多筛选'}
                    {advancedCount > 0 && (
                      <span className='bg-primary/10 text-primary rounded px-1.5 text-xs'>
                        {advancedCount}
                      </span>
                    )}
                    <Icons.chevronDown
                      className={cn('size-4 transition-transform', showAdvanced && 'rotate-180')}
                    />
                  </Button>
                </div>
                {showAdvanced && (
                  <div
                    id='screening-advanced-filters'
                    className='grid gap-3 border-t pt-3 sm:grid-cols-2 xl:grid-cols-4'
                  >
                    <Field label='推荐类型'>
                      <select
                        className='bg-background h-9 w-full min-w-0 rounded-md border px-2.5 text-sm'
                        value={filters.recommendation || ''}
                        onChange={(e) => filter('recommendation', e.target.value)}
                      >
                        <option value=''>全部</option>
                        <option value='regular'>常规</option>
                        <option value='exploration'>探索</option>
                      </select>
                    </Field>
                    {(
                      [
                        ['category', '类目'],
                        ['seller_region', '卖家地区'],
                        ['direction', '产品方向'],
                        ['child_sales_min', '最低子体销量（仅确定达到）'],
                        ['profit_min', '最低利润 USD'],
                        ['profit_max', '最高利润 USD']
                      ] as const
                    ).map(([key, title]) => (
                      <Field key={key} label={title}>
                        <Input
                          value={filters[key] || ''}
                          onChange={(e) => filter(key, e.target.value)}
                        />
                      </Field>
                    ))}
                    <Field label='来源批次'>
                      <select
                        className='bg-background h-9 w-full min-w-0 rounded-md border px-2.5 text-sm'
                        value={filters.import_id || ''}
                        onChange={(e) => filter('import_id', e.target.value)}
                      >
                        <option value=''>全部批次</option>
                        {imports.data?.items.map((item) => (
                          <option key={item.id} value={item.id}>
                            {item.file_name} · {item.source_period}
                          </option>
                        ))}
                      </select>
                    </Field>
                    <Field label='人工样本'>
                      <select
                        className='bg-background h-9 w-full min-w-0 rounded-md border px-2.5 text-sm'
                        value={filters.has_reference || ''}
                        onChange={(e) => filter('has_reference', e.target.value)}
                      >
                        <option value=''>全部</option>
                        <option value='true'>有样本</option>
                        <option value='false'>无样本</option>
                      </select>
                    </Field>
                  </div>
                )}
              </section>
              <div className='bg-card flex flex-wrap items-center justify-between gap-2.5 rounded-xl border px-3.5 py-2.5 shadow-2xs'>
                <div className='flex flex-wrap items-center gap-2'>
                  <p role='status' className='text-xs font-medium sm:text-sm'>
                    本页已选 {pageSelectedCount} / {pageSelectionIds.length} 项 · 跨页已选{' '}
                    {selectedIds.length} 项
                  </p>
                  {hiddenSelectedVariantCount > 0 && (
                    <span className='rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300'>
                      含 {hiddenSelectedVariantCount} 个未展开子体
                    </span>
                  )}
                  <Button size='sm' variant='outline' onClick={() => setSelected({})}>
                    清空
                  </Button>
                  <Button
                    size='sm'
                    disabled={!selectedIds.length}
                    onClick={() => setBatchReviewOpen(true)}
                  >
                    批量复核
                  </Button>
                </div>

                <div className='flex flex-wrap items-center gap-2'>
                  {expandablePageIds.length > 0 && (
                    <Button size='sm' variant='outline' onClick={toggleExpandAllFamilies}>
                      {allFamiliesExpanded ? '全部收起子体' : '全部展开子体'}
                    </Button>
                  )}
                  <button
                    type='button'
                    onClick={() => {
                      const active =
                        filters.machine === 'passed' && filters.review_status === 'pending';
                      setFilters((old) => ({
                        ...old,
                        machine: active ? '' : 'passed',
                        review_status: active ? '' : 'pending'
                      }));
                      setPage(1);
                      setFamilyRows({});
                    }}
                    className={cn(
                      'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-xs font-medium transition-all',
                      filters.machine === 'passed' && filters.review_status === 'pending'
                        ? 'border-amber-500 bg-amber-500/20 text-amber-800 ring-2 ring-amber-500/25 dark:text-amber-200'
                        : 'border-amber-500/35 bg-amber-500/[0.08] text-amber-700 hover:bg-amber-500/15 dark:text-amber-300'
                    )}
                  >
                    <span aria-hidden='true' className='size-1.5 rounded-full bg-amber-500' />
                    <span>仅看机器通过待人工复核</span>
                  </button>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={busy}
                    onClick={() =>
                      void act(async () => {
                        const job = await request<JobCreated>(
                          '/exports',
                          'POST',
                          selectedIds.length ? { product_ids: selectedIds } : { filters }
                        );
                        watch(job.job_id);
                      }, '导出已排队')
                    }
                  >
                    {selectedIds.length ? '导出已选商品' : '导出筛选结果'}
                  </Button>
                </div>
              </div>
              {products.isError && <p role='alert'>{products.error.message}</p>}
              {products.isFetching && products.data && (
                <p role='status' className='text-muted-foreground text-xs'>
                  正在更新列表，暂时保留上次结果…
                </p>
              )}
              {products.isLoading ? (
                <div
                  role='status'
                  className='text-muted-foreground animate-pulse rounded-lg border p-8 text-center text-sm'
                >
                  正在加载商品…
                </div>
              ) : (
                <fieldset className='w-full min-w-0 max-w-full'>
                  <legend className='sr-only'>商品列表操作</legend>
                  <TableScroll
                    label='商品列表，可横向和纵向滚动'
                    data-screening-table
                    ref={setTableContainer}
                    aria-busy={products.isFetching}
                    className='screening-scroll bg-card w-full min-w-0 max-w-full max-h-[65vh] overflow-x-auto overflow-y-auto overscroll-x-contain rounded-xl border shadow-2xs [contain:inline-size] [&_td[data-fixed-align=left]]:text-left [&_td[data-fixed-align=left]_p]:text-left [&_tbody_p]:text-center [&_tbody_p[data-multiline=true]]:text-left [&_tbody_p[data-multiline=true]]:justify-start [&_[data-auto-align]]:text-center [&_[data-auto-align][data-multiline=true]]:text-left'
                  >
                    <table
                      className='border-separate border-spacing-0 table-fixed text-center text-xs'
                      style={{ width: table.getTotalSize() }}
                    >
                      <colgroup>
                        {table.getVisibleLeafColumns().map((column) => (
                          <col key={column.id} style={{ width: column.getSize() }} />
                        ))}
                      </colgroup>
                      <caption className='sr-only'>
                        商品列表：商品信息、价格销量、尺寸重量、卖家、筛选与复核结果
                      </caption>
                      <thead className='bg-muted sticky top-0 z-30 [&_th]:bg-muted'>
                        {table.getHeaderGroups().map((g) => (
                          <tr key={g.id}>
                            {g.headers.map((h) => (
                              <th
                                scope='col'
                                key={h.id}
                                aria-sort={sortDirection(filters.sort, h.column.id)}
                                className={cn(
                                  'group relative whitespace-nowrap border-b p-2 text-center align-middle has-[>button:not([data-column-resize])]:p-0',
                                  stickyColumnClass(h.column.id, true)
                                )}
                              >
                                {sortFields[h.column.id] ? (
                                  <button
                                    type='button'
                                    className='inline-flex min-h-12 w-full items-center justify-center gap-1 p-2 text-center font-semibold focus-visible:outline-2 focus-visible:outline-primary'
                                    onClick={() => {
                                      const field = sortFields[h.column.id];
                                      setFilters((old) => ({
                                        ...old,
                                        sort: old.sort === field ? '-' + field : field
                                      }));
                                      setPage(1);
                                    }}
                                  >
                                    <span className='min-w-0 whitespace-nowrap'>
                                      {flexRender(h.column.columnDef.header, h.getContext())}
                                    </span>
                                    <span aria-hidden='true' className='text-muted-foreground'>
                                      <span className='group-aria-[sort=descending]:hidden'>↑</span>
                                      <span className='group-aria-[sort=ascending]:hidden'>↓</span>
                                    </span>
                                  </button>
                                ) : (
                                  <span className='min-w-0 whitespace-nowrap'>
                                    {flexRender(h.column.columnDef.header, h.getContext())}
                                  </span>
                                )}
                                {h.column.getCanResize() && (
                                  <button
                                    type='button'
                                    data-column-resize
                                    aria-label={
                                      '调整' +
                                      (typeof h.column.columnDef.header === 'string'
                                        ? h.column.columnDef.header
                                        : h.column.id) +
                                      '列宽'
                                    }
                                    title='拖动调整列宽；双击还原；左右方向键微调'
                                    className='absolute inset-y-0 right-0 z-20 w-3 touch-none cursor-col-resize select-none border-r-2 border-transparent hover:border-primary focus-visible:border-primary focus-visible:outline-2 focus-visible:outline-primary'
                                    onClick={(e) => e.stopPropagation()}
                                    onMouseDown={h.getResizeHandler()}
                                    onTouchStart={h.getResizeHandler()}
                                    onDoubleClick={() => resetColumnWidth(h.column.id)}
                                    onKeyDown={(e) => {
                                      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
                                        e.preventDefault();
                                        setColumnSizing((old) => ({
                                          ...old,
                                          [h.column.id]: Math.max(
                                            minimumColumnWidths[h.column.id] ?? 40,
                                            Math.min(
                                              900,
                                              h.column.getSize() +
                                                (e.key === 'ArrowRight' ? 16 : -16)
                                            )
                                          )
                                        }));
                                      } else if (e.key === 'Home') {
                                        e.preventDefault();
                                        resetColumnWidth(h.column.id);
                                      }
                                    }}
                                  />
                                )}
                              </th>
                            ))}
                          </tr>
                        ))}
                      </thead>
                      <tbody inert={products.isPlaceholderData}>
                        {table.getRowModel().rows.map((row) => (
                          <Fragment key={row.id}>
                            <tr
                              className={cn(
                                'group/row bg-card transition-colors hover:bg-muted',
                                row.getIsSelected() && 'bg-muted'
                              )}
                            >
                              {row.getVisibleCells().map((cell) => (
                                <td
                                  key={cell.id}
                                  data-fixed-align={
                                    ['asin', 'dimensions'].includes(cell.column.id)
                                      ? 'left'
                                      : undefined
                                  }
                                  className={cn(
                                    'break-words border-b px-2 py-2.5 align-middle [&>*]:min-w-0 [&>*]:max-w-full [&_p]:whitespace-normal',
                                    stickyColumnClass(cell.column.id, false, row.getIsSelected())
                                  )}
                                >
                                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                                </td>
                              ))}
                            </tr>
                            {familyRows[row.original.id] && (
                              <FamilyChildren
                                product={row.original}
                                columns={columns}
                                selected={selected}
                                onSelectionChange={setSelected}
                              />
                            )}
                          </Fragment>
                        ))}
                      </tbody>
                    </table>
                    {!products.isError && !products.data?.items.length && (
                      <div className='p-10 text-center'>
                        <p className='font-medium'>
                          {filterCount ? '没有符合筛选条件的商品' : '还没有商品'}
                        </p>
                        <p className='text-muted-foreground mt-1 text-sm'>
                          {filterCount ? '调整筛选条件后重试。' : '导入商品文件，开始初筛与复核。'}
                        </p>
                        <Button
                          variant='link'
                          onClick={() => (filterCount ? resetFilters() : setImportOpen(true))}
                        >
                          {filterCount ? '重置筛选' : '导入商品文件'}
                        </Button>
                      </div>
                    )}
                  </TableScroll>
                </fieldset>
              )}
              <div className='flex flex-wrap items-center gap-3' data-products-pagination>
                <label htmlFor='screening-page-size' className='text-sm'>
                  每页
                </label>
                <select
                  id='screening-page-size'
                  name='page_size'
                  value={pageSize}
                  className='h-9 rounded-md border bg-background px-2 text-sm'
                  onChange={(event) => {
                    setPageSize(Number(event.target.value));
                    setPage(1);
                    setFamilyRows({});
                  }}
                >
                  {[10, 20, 50, 100].map((size) => (
                    <option key={size} value={size}>
                      {size} 条
                    </option>
                  ))}
                </select>
                <Button
                  variant='outline'
                  disabled={products.isPlaceholderData || page === 1}
                  onClick={() => setPage((v) => v - 1)}
                >
                  上一页
                </Button>
                <span className='text-sm'>
                  第 {page} / {Math.max(1, Math.ceil((products.data?.total || 0) / pageSize))} 页 ·
                  共 {products.data?.total || 0} 项
                </span>
                <Button
                  variant='outline'
                  disabled={
                    products.isPlaceholderData || page * pageSize >= (products.data?.total || 0)
                  }
                  onClick={() => setPage((v) => v + 1)}
                >
                  下一页
                </Button>
              </div>
              {group && (
                <section className='space-y-2 rounded border p-4'>
                  <div className='flex justify-between'>
                    <h3>同组商品</h3>
                    <div className='flex items-center gap-2'>
                      <Button
                        size='sm'
                        variant='outline'
                        disabled={groupPage === 1}
                        onClick={() => setGroupPage((v) => v - 1)}
                      >
                        上一页同组
                      </Button>
                      <span className='text-sm'>
                        第 {groupPage} 页 / 共 {expanded.data?.total || 0} 个
                      </span>
                      <Button
                        size='sm'
                        variant='outline'
                        disabled={groupPage * 50 >= (expanded.data?.total || 0)}
                        onClick={() => setGroupPage((v) => v + 1)}
                      >
                        下一页同组
                      </Button>
                    </div>
                    <Button variant='ghost' onClick={() => setGroup(null)}>
                      关闭
                    </Button>
                  </div>
                  {expanded.data?.items.map((p) => (
                    <div
                      key={p.id}
                      className='flex items-center justify-between gap-2 border-t py-2'
                    >
                      <label className='text-sm'>
                        <input
                          type='checkbox'
                          aria-label={`选择商品 ${p.asin}`}
                          checked={Boolean(selected[p.id])}
                          onChange={(e) =>
                            setSelected((old) => ({ ...old, [p.id]: e.target.checked }))
                          }
                        />{' '}
                        {p.asin} · {p.title}
                      </label>
                      <Button size='sm' variant='outline' onClick={() => openReview(p)}>
                        复核
                      </Button>
                    </div>
                  ))}
                </section>
              )}
            </>
          )}
          {tab === 2 && (
            <ReviewPanel
              key={session.workspaceId}
              selected={selectedIds}
              filters={filters}
              act={act}
              busy={busy}
            />
          )}
          {tab === 3 && (
            <ProfitPanel
              key={`${session.workspaceId}:${focus?.id || 'none'}:${focus?.monthly_id || 'latest'}`}
              product={focus}
              onClearFocus={() => setFocus(null)}
              selected={selectedIds}
              filters={filters}
              act={act}
              watch={watch}
              busy={busy}
            />
          )}
          {tab === 5 && <ParentMonthlyPanel />}
          {tab === 4 && <PolicyPanel selected={selectedIds} act={act} watch={watch} busy={busy} />}
        </div>
      </PageContainer>
      <ReviewDrawer
        open={reviewOpen}
        product={focus}
        act={act}
        busy={busy}
        onClose={() => setReviewOpen(false)}
        onChooseFamily={(ids) => {
          setSelected(Object.fromEntries(ids.map((id) => [id, true])));
          setTab(2);
        }}
        onCalculate={openProfit}
      />
      <BatchReviewDialog
        key={'batch-' + session.workspaceId}
        open={batchReviewOpen}
        onClose={() => setBatchReviewOpen(false)}
        selected={selectedIds}
        act={act}
        busy={busy}
        onApplied={() => setSelected({})}
      />
      <ImportDialog
        key={session.workspaceId}
        open={importOpen}
        onClose={() => setImportOpen(false)}
        act={act}
        watch={(id) => watch(id, false)}
        busy={busy}
        error={error}
        message={message}
        job={showJob ? undefined : job.data}
        onRemoved={(id) => {
          setSelected({});
          setFocus(null);
          setReviewOpen(false);
          setFamilyRows({});
          setGroup(null);
          setFilters((old) => {
            if (old.import_id !== id) return old;
            const next = { ...old };
            delete next.import_id;
            return next;
          });
        }}
      />
    </div>
  );
}

function ProductIdentityCell({
  product,
  familyRows,
  setFamilyRows,
  onOpenReview
}: {
  product: Product;
  familyRows: Record<string, boolean>;
  setFamilyRows: React.Dispatch<React.SetStateAction<Record<string, boolean>>>;
  onOpenReview: (product: Product) => void;
}) {
  const childCtx = useContext(ChildRowContext);
  const expanded = Boolean(familyRows[product.id]);
  const hasFamily = !childCtx.isChild && (product.family_count || 1) > 1;
  return (
    <div
      className={cn(
        'flex min-w-0 items-start gap-2.5',
        childCtx.isChild && 'border-primary/30 border-l-2 pl-2.5'
      )}
    >
      {product.image_url ? (
        <Image
          unoptimized
          width={52}
          height={52}
          src={product.image_url}
          alt=''
          className='size-12 shrink-0 rounded-lg border bg-white object-contain'
          loading='lazy'
          referrerPolicy='no-referrer'
        />
      ) : (
        <div
          aria-hidden='true'
          className='bg-muted/60 text-muted-foreground flex size-12 shrink-0 items-center justify-center rounded-lg border'
        >
          <Icons.product className='size-4 opacity-60' />
        </div>
      )}
      <div className='min-w-0 flex-1 space-y-1'>
        <div className='flex flex-wrap items-center gap-1'>
          {childCtx.isChild && (
            <span className='bg-primary/10 text-primary rounded px-1.5 py-0.5 text-[11px] font-medium'>
              子体 {childCtx.index}
            </span>
          )}
          <button
            type='button'
            className='break-all font-mono text-xs font-semibold tracking-tight underline-offset-4 hover:underline'
            title='打开商品复核抽屉'
            onClick={() => onOpenReview(product)}
          >
            {product.asin}
          </button>
          <AsinTools asin={product.asin} />
          <span
            className='bg-muted text-muted-foreground rounded border px-1.5 py-0.5 text-[11px]'
            title={productTrademark(product).title}
          >
            {productTrademark(product).label}
          </span>
        </div>
        <BilingualProductTitle product={product} compact />
        <div className='flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs'>
          {hasFamily ? (
            <button
              type='button'
              aria-label={`${expanded ? '收起' : '展开'} ${product.parent_asin} 的子体`}
              aria-expanded={expanded}
              aria-controls={`family-${product.id}`}
              onClick={() => setFamilyRows((old) => ({ ...old, [product.id]: !old[product.id] }))}
              className='bg-muted/70 hover:border-primary/40 hover:bg-muted inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium transition-colors'
            >
              <span className='text-muted-foreground font-mono'>
                父体: {product.parent_asin || '未知'}
              </span>
              <span aria-hidden='true' className='text-muted-foreground'>
                |
              </span>
              {expanded ? (
                <Icons.chevronDown aria-hidden='true' className='size-3' />
              ) : (
                <Icons.chevronRight aria-hidden='true' className='size-3' />
              )}
              <span>
                {expanded ? '收起' : '展开'} {product.family_count} 个子体
              </span>
            </button>
          ) : (
            <span className='text-muted-foreground font-mono text-[11px]'>
              父体：{product.parent_asin || '未知'}
            </span>
          )}
          {product.manual_category && (
            <span className='text-muted-foreground text-[11px]'>
              · 人工分类：{product.manual_category}
            </span>
          )}
        </div>
        <p className='text-muted-foreground text-[11px]'>
          {product.stat_month?.slice(0, 7)} ·{' '}
          {periodLabels[product.period_type || ''] || '暂无分期数据'}
        </p>
      </div>
    </div>
  );
}

function SelectAllCheckbox({
  label,
  checked,
  indeterminate,
  disabled,
  onChange
}: {
  label: string;
  checked: boolean;
  indeterminate: boolean;
  disabled: boolean;
  onChange: () => void;
}) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate;
  }, [indeterminate]);
  return (
    <input
      ref={ref}
      aria-label={label}
      type='checkbox'
      disabled={disabled}
      checked={checked}
      onChange={onChange}
    />
  );
}

function FamilyChildren({
  product,
  columns,
  selected,
  onSelectionChange
}: {
  product: Product;
  columns: ColumnDef<Product>[];
  selected: RowSelectionState;
  onSelectionChange: OnChangeFn<RowSelectionState>;
}) {
  const [page, setPage] = useState(1);
  const family = useScreeningQuery<FamilyResult>(`/products/${product.id}/family?page=${page}`);
  const children = useMemo(
    () => family.data?.items.filter((p) => p.id !== product.id) || [],
    [family.data?.items, product.id]
  );
  // Child products share the exact column renderers and selection behavior of the main table.
  // oxlint-disable-next-line react/incompatible-library -- TanStack renders the shared product columns.
  const table = useReactTable({
    data: children,
    columns,
    state: { rowSelection: selected },
    onRowSelectionChange: onSelectionChange,
    getRowId: (p) => p.id,
    getCoreRowModel: getCoreRowModel(),
    enableRowSelection: true
  });
  return (
    <>
      {table.getRowModel().rows.map((row, rowIndex) => (
        <ChildRowContext.Provider
          key={row.id}
          value={{ isChild: true, index: (page - 1) * 50 + rowIndex + 1 }}
        >
          <tr
            data-family={product.id}
            className={cn(
              'group/row bg-card transition-colors hover:bg-muted',
              row.getIsSelected() && 'bg-muted'
            )}
          >
            {row.getVisibleCells().map((cell) => (
              <td
                key={cell.id}
                data-fixed-align={
                  ['asin', 'dimensions'].includes(cell.column.id) ? 'left' : undefined
                }
                className={cn(
                  'break-words border-b px-2 py-2.5 align-middle [&>*]:min-w-0 [&>*]:max-w-full [&_p]:whitespace-normal',
                  stickyColumnClass(cell.column.id, false, row.getIsSelected())
                )}
              >
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </td>
            ))}
          </tr>
        </ChildRowContext.Provider>
      ))}
      <tr id={`family-${product.id}`} className='bg-primary/[0.035]'>
        <td colSpan={columns.length} className='border-primary/20 border-b-2 p-3'>
          {family.isLoading && <p role='status'>正在加载同父体商品…</p>}
          {family.isError && (
            <div role='alert'>
              {family.error.message}
              <Button size='sm' variant='outline' onClick={() => void family.refetch()}>
                重试
              </Button>
            </div>
          )}
          {family.data && (
            <div className='flex flex-wrap items-center gap-2 text-xs text-muted-foreground'>
              <span>
                同父体 {product.parent_asin} · 共 {family.data.total} 个商品，各自展示最新一期
              </span>
              {family.data.total > 50 && (
                <>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={page === 1}
                    onClick={() => setPage((p) => p - 1)}
                  >
                    上一页同父体商品
                  </Button>
                  <span>第 {page} 页</span>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={page * 50 >= family.data.total}
                    onClick={() => setPage((p) => p + 1)}
                  >
                    下一页同父体商品
                  </Button>
                </>
              )}
            </div>
          )}
        </td>
      </tr>
    </>
  );
}

function PriceHistoryCard({
  product,
  onCalculate
}: {
  product: Product;
  onCalculate: (product: Product) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className='space-y-1'>
      <p className='whitespace-nowrap'>${formatMoney(product.price_usd)}</p>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger className='text-primary text-xs underline underline-offset-2'>
          查看详细数据
        </DialogTrigger>
        <DataDialogContent
          title={'经营数据 · ' + product.asin}
          description={product.title_zh || product.title}
          wide
        >
          {open && (
            <MonthlyHistory
              product={product}
              recentYear
              onCalculate={(p) => {
                setOpen(false);
                onCalculate(p);
              }}
            />
          )}
        </DataDialogContent>
      </Dialog>
    </div>
  );
}

const measurementNumber = (value: string | number | null | undefined) =>
  value == null || value === '' || !Number.isFinite(Number(value))
    ? '—'
    : Number(value).toLocaleString('zh-CN', { maximumFractionDigits: 2 });
const measurementDimensions = (
  length: Product['weight_g'],
  width: Product['weight_g'],
  height: Product['weight_g'],
  original?: string | null
) =>
  [length, width, height].every((v) => v != null && v !== '')
    ? [length, width, height].map(measurementNumber).join(' × ') + ' cm'
    : original || '—';
function ProductMeasurements({ product }: { product: Product }) {
  const [open, setOpen] = useState(false);
  const prodDim = measurementDimensions(
    product.product_length_cm,
    product.product_width_cm,
    product.product_height_cm,
    product.product_dimensions || product.product_dimensions_original
  );
  const pkgDim = measurementDimensions(
    product.package_length_cm,
    product.package_width_cm,
    product.package_height_cm,
    product.package_dimensions || product.package_dimensions_original
  );
  return (
    <div className='space-y-1 text-xs leading-5 tabular-nums'>
      <p className='break-words'>
        <span className='text-muted-foreground'>本体：</span>
        {prodDim}
      </p>
      <div className='text-muted-foreground flex flex-wrap items-center gap-x-1.5 gap-y-0.5'>
        <span>
          净重 {measurementNumber(product.weight_g)}
          {product.weight_g != null && ' g'}
        </span>
        {product.package_weight_g != null && (
          <span>· 毛重 {measurementNumber(product.package_weight_g)} g</span>
        )}
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger className='text-primary cursor-pointer text-xs underline underline-offset-2'>
            详情
          </DialogTrigger>
          <DataDialogContent
            title={'尺寸与重量明细 · ' + product.asin}
            description={product.title_zh || product.title}
          >
            <div className='grid gap-3 sm:grid-cols-2'>
              {[
                ['商品本体尺寸', prodDim],
                ['外包装尺寸', pkgDim],
                [
                  '商品净重',
                  measurementNumber(product.weight_g) + (product.weight_g != null ? ' g' : '')
                ],
                [
                  '含包装毛重',
                  measurementNumber(product.package_weight_g) +
                    (product.package_weight_g != null ? ' g' : '')
                ]
              ].map(([label, val]) => (
                <div key={label} className='bg-muted/40 space-y-1 rounded-xl border p-3'>
                  <div className='text-muted-foreground text-xs'>{label}</div>
                  <div className='font-mono text-sm font-semibold tabular-nums'>{val}</div>
                </div>
              ))}
            </div>
          </DataDialogContent>
        </Dialog>
      </div>
    </div>
  );
}
