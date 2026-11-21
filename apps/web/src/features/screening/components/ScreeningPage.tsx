'use client';
/* oxlint-disable react/no-unstable-nested-components -- TanStack column renderers are stable useMemo callbacks. */
import { Fragment, useEffect, useMemo, useState } from 'react';
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
import type {
  ImportRecord,
  JobCreated,
  PageResult,
  Product,
  ReviewStatus,
  FamilyResult
} from '../types';
import { childSales } from '../sales';
import { useTableTextAlignment } from '../use-table-text-alignment';
import { countryCode, listingAge, shanghaiToday } from '../product-display';
import ImportDialog from './ImportDialog';
import ReviewPanel from './ReviewPanel';
import ProfitPanel from './ProfitPanel';
import PolicyPanel from './PolicyPanel';
import ProductParameters from './ProductParameters';
import FamilyPanel from './FamilyPanel';
import ParentMonthlyPanel from './ParentMonthlyPanel';
import MonthlyHistory, { periodLabels, sourceLabels } from './MonthlyHistory';
import type { MonthOption } from '../types';
import {
  Badge,
  Button,
  DataBlock,
  Field,
  Input,
  TableScroll,
  formatMoney,
  type Action
} from './shared';

const minimumColumnWidths: ColumnSizingState = {
  selection: 40,
  asin: 156,
  price_usd: 84,
  monthly_sales: 64,
  rating_reviews: 100,
  listing_date: 90,
  dimensions: 120,
  seller: 92,
  decision: 88,
  review_status: 72,
  profit: 88,
  actions: 56
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
  { id: 2, title: '批量复核' },
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
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [page, setPage] = useState(1);
  const [filters, setFilters] = useState<Record<string, string>>({ show_merged: 'false' });
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
  const [group, setGroup] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [jobs, setJobs] = useState<string[]>([]);
  const [showJob, setShowJob] = useState(true);
  const [activeJob, setActiveJob] = useState<string | null>(null);
  const query = new URLSearchParams({ ...filters, page: String(page) }).toString();
  const products = useScreeningQuery<PageResult<Product>>('/products?' + query, false, true);
  useEffect(() => {
    if (
      !products.data ||
      products.isPlaceholderData ||
      products.isFetching ||
      page * products.data.page_size >= products.data.total
    )
      return;
    const params = new URLSearchParams({ ...filters, page: String(page + 1) });
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
  const months = useScreeningQuery<{ items: MonthOption[] }>('/months');
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
    ([key, value]) => key !== 'sort' && value && (key !== 'show_merged' || value === 'true')
  ).length;
  function resetFilters() {
    setFilters({ show_merged: 'false' });
    setPage(1);
    setSelected({});
    setFamilyRows({});
    setGroup(null);
    setFocus(null);
    setShowAdvanced(false);
  }
  const selectedIds = Object.keys(selected).filter((id) => selected[id]);
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
  }
  const columns = useMemo<ColumnDef<Product>[]>(
    () => [
      {
        id: 'selection',
        enableResizing: false,
        size: 40,
        minSize: 40,
        maxSize: 40,
        header: ({ table }) => (
          <input
            aria-label='选择当前页代表商品'
            disabled={products.isPlaceholderData}
            type='checkbox'
            checked={table.getIsAllPageRowsSelected()}
            onChange={table.getToggleAllPageRowsSelectedHandler()}
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
          <div className='flex min-w-0 items-center gap-2'>
            {row.original.image_url && (
              <Image
                unoptimized
                width={64}
                height={64}
                src={row.original.image_url}
                alt=''
                className='h-14 w-14 shrink-0 rounded-md border bg-white object-contain'
                loading='lazy'
                referrerPolicy='no-referrer'
              />
            )}
            <div>
              <div className='flex flex-wrap items-center gap-1'>
                <button
                  className='break-all text-xs font-semibold underline-offset-4 hover:underline'
                  title='打开商品复核'
                  onClick={() => setFocus(row.original)}
                >
                  {row.original.asin}
                </button>
                <AsinTools asin={row.original.asin} />
                {(row.original.family_count || 1) > 1 && (
                  <Button
                    size='sm'
                    variant='ghost'
                    className='h-7 px-2 text-xs'
                    aria-label={`${familyRows[row.original.id] ? '收起' : '展开'} ${row.original.parent_asin} 的子体`}
                    aria-expanded={Boolean(familyRows[row.original.id])}
                    aria-controls={`family-${row.original.id}`}
                    onClick={() =>
                      setFamilyRows((old) => ({ ...old, [row.original.id]: !old[row.original.id] }))
                    }
                  >
                    {familyRows[row.original.id] ? (
                      <Icons.chevronDown className='size-3' />
                    ) : (
                      <Icons.chevronRight className='size-3' />
                    )}
                    {row.original.family_count} 个子体
                  </Button>
                )}
              </div>
              {(row.original.family_count || 1) > 1 && (
                <p className='text-muted-foreground text-xs'>
                  {row.original.sales_scope === 'child' && row.original.monthly_sales !== null
                    ? row.original.family_sales_pending
                      ? '已知销量代表 · 部分待确认'
                      : '最新一期销量代表'
                    : '代表商品 · 销量待确认'}
                </p>
              )}
              <p
                className='line-clamp-2 leading-5'
                title={row.original.title_zh || row.original.title}
              >
                {row.original.title_zh || row.original.title}
              </p>
              <p className='text-xs'>
                父体：{row.original.parent_asin || '未知'}{' '}
                {row.original.manual_category && `· 人工分类：${row.original.manual_category}`}
              </p>
              <p className='text-muted-foreground text-xs'>
                {row.original.stat_month?.slice(0, 7)} ·{' '}
                {periodLabels[row.original.period_type || ''] || '暂无分期数据'}
              </p>
            </div>
          </div>
        )
      },
      {
        accessorKey: 'price_usd',
        header: '价格 USD',
        cell: ({ row }) => (
          <PriceHistoryCard
            product={row.original}
            onCalculate={(product) => {
              setFocus(product);
              setTab(3);
            }}
          />
        )
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
        cell: ({ row }) => (
          <div>
            <Badge value={row.original.review_status} />
            <p className='max-w-40 text-xs'>{row.original.review_reason}</p>
            {row.original.has_reference && (
              <span className='text-muted-foreground text-xs'>有人工样本</span>
            )}
          </div>
        )
      },
      {
        id: 'profit',
        header: '预计单件利润',
        cell: ({ row }) => (
          <div>
            {row.original.profit_output?.profit_usd
              ? `$${row.original.profit_output.profit_usd}`
              : '待测算'}
            {row.original.profit_data_revision !== null &&
              (row.original.profit_data_revision !== row.original.data_revision ||
                row.original.profit_monthly_revision !== row.original.monthly_revision) && (
                <p className='text-xs'>来源已更新</p>
              )}
          </div>
        )
      },
      {
        id: 'actions',
        header: '操作',
        cell: ({ row }) => (
          <div className='flex flex-col items-stretch gap-1 [&>button]:h-7 [&>button]:px-1'>
            <Button size='sm' variant='ghost' onClick={() => setFocus(row.original)}>
              复核
            </Button>
            <Button
              size='sm'
              variant='ghost'
              onClick={() => {
                setFocus(row.original);
                setTab(3);
              }}
            >
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
    [familyRows, products.isPlaceholderData, today]
  );
  // oxlint-disable-next-line react/incompatible-library -- TanStack Table is intentionally kept outside React Compiler memoization.
  const table = useReactTable({
    data: products.data?.items || [],
    columns,
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
          <div className='flex items-center gap-2'>
            {tab === 0 && (
              <Button size='sm' onClick={() => setImportOpen(true)}>
                导入商品
              </Button>
            )}
            <Button
              size='sm'
              variant='outline'
              disabled={products.isFetching}
              onClick={() => void refresh()}
            >
              {products.isFetching ? '刷新中…' : '刷新'}
            </Button>
          </div>
        }
      >
        <div className='min-w-0 max-w-full space-y-4'>
          <nav className='flex flex-wrap gap-2 border-b pb-3' aria-label='初筛工作台'>
            {tabs.map(({ title, id: i }) => (
              <Button
                key={title}
                variant={tab === i ? 'default' : 'ghost'}
                onClick={() => setTab(i)}
              >
                {title}
              </Button>
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
              <div className='grid grid-cols-1 gap-2 sm:grid-cols-3'>
                {[
                  ['displayed', '商品数'],
                  ['products', '子体商品数'],
                  ['pending_displayed', '待复核']
                ].map(([key, title]) => (
                  <div
                    key={key}
                    className='bg-card flex items-center justify-between gap-2 rounded-lg border px-3 py-3'
                  >
                    <span className='text-muted-foreground text-xs'>{title}</span>
                    <p className='text-xl font-semibold tabular-nums'>
                      {counts.data?.[key]?.toLocaleString('zh-CN') ?? '—'}
                    </p>
                  </div>
                ))}
              </div>
              <section aria-label='商品筛选' className='bg-card space-y-3 rounded-lg border p-3'>
                <div className='flex items-center justify-between gap-3'>
                  <p className='text-muted-foreground text-xs'>
                    默认最新一期 · 同父体折叠展示 · 点击价格销量查看历史
                  </p>
                  <Button size='sm' variant='ghost' disabled={!filterCount} onClick={resetFilters}>
                    重置筛选{filterCount ? `（${filterCount}）` : ''}
                  </Button>
                </div>
                <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
                  <Field label='关键词 / ASIN'>
                    <Input
                      placeholder='搜索 ASIN 或商品标题'
                      value={filters.q || ''}
                      onChange={(e) => filter('q', e.target.value)}
                    />
                  </Field>
                  <Field label='人工结论'>
                    <select
                      className='h-9 w-full min-w-0 rounded-md border bg-background px-2 text-sm'
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
                      className='rounded border bg-transparent p-2'
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
                  <Field label='经营数据来源'>
                    <select
                      className='rounded border bg-transparent p-2'
                      value={filters.source_type || ''}
                      onChange={(e) => {
                        filter('source_type', e.target.value);
                        setFocus(null);
                        setSelected({});
                      }}
                    >
                      <option value=''>全部来源 · 最新一期</option>
                      {[...new Set((months.data?.items || []).map((m) => m.source_type))].map(
                        (source) => (
                          <option key={source} value={source}>
                            {sourceLabels[source] || source}
                          </option>
                        )
                      )}
                    </select>
                  </Field>
                </div>
                <div className='flex items-center justify-between gap-3'>
                  <label className='flex items-center gap-2 text-xs'>
                    <input
                      type='checkbox'
                      aria-label='展示全部子体'
                      checked={filters.show_merged === 'true'}
                      onChange={(e) => filter('show_merged', String(e.target.checked))}
                    />
                    展示全部子体
                  </label>
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
                    {' '}
                    <Field label='推荐类型'>
                      <select
                        className='rounded border bg-transparent p-2'
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
                        className='rounded border bg-transparent p-2'
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
                        className='rounded border bg-transparent p-2'
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
              <div className='bg-card flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2'>
                <span className='text-sm'>跨页已选 {selectedIds.length} 个</span>
                <Button variant='outline' onClick={() => setSelected({})}>
                  清空
                </Button>
                <Button disabled={!selectedIds.length} onClick={() => setTab(2)}>
                  批量复核
                </Button>
                <Button
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
                    className='bg-card w-full min-w-0 max-w-full max-h-[65vh] overflow-x-auto overflow-y-auto overscroll-x-contain rounded-lg border [contain:inline-size] [&_tbody_p]:text-center [&_tbody_p[data-multiline=true]]:text-left [&_tbody_p[data-multiline=true]]:justify-start [&_[data-auto-align]]:text-center [&_[data-auto-align][data-multiline=true]]:text-left'
                  >
                    <table
                      className='table-fixed text-center text-xs'
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
                      <thead className='bg-muted sticky top-0 z-10 [&_th]:bg-muted'>
                        {table.getHeaderGroups().map((g) => (
                          <tr key={g.id}>
                            {g.headers.map((h) => (
                              <th
                                scope='col'
                                key={h.id}
                                aria-sort={sortDirection(filters.sort, h.column.id)}
                                className='group relative whitespace-normal p-2 text-center align-middle has-[>button:not([data-column-resize])]:p-0'
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
                                    <span data-auto-align className='min-w-0'>
                                      {flexRender(h.column.columnDef.header, h.getContext())}
                                    </span>
                                    <span aria-hidden='true' className='text-muted-foreground'>
                                      <span className='group-aria-[sort=descending]:hidden'>↑</span>
                                      <span className='group-aria-[sort=ascending]:hidden'>↓</span>
                                    </span>
                                  </button>
                                ) : (
                                  <span data-auto-align className='min-w-0'>
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
                                'border-t',
                                familyRows[row.original.id] && 'bg-primary/[0.035]',
                                row.getIsSelected() && 'bg-muted/50'
                              )}
                            >
                              {row.getVisibleCells().map((cell) => (
                                <td
                                  key={cell.id}
                                  className='break-words px-2 py-2 align-middle [&>*]:min-w-0 [&>*]:max-w-full [&_p]:whitespace-normal'
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
              <div className='flex items-center gap-3'>
                <Button
                  variant='outline'
                  disabled={products.isPlaceholderData || page === 1}
                  onClick={() => setPage((v) => v - 1)}
                >
                  上一页
                </Button>
                <span className='text-sm'>
                  第 {page} 页 / 共 {products.data?.total || 0} 项
                </span>
                <Button
                  variant='outline'
                  disabled={products.isPlaceholderData || page * 50 >= (products.data?.total || 0)}
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
                      <Button size='sm' variant='outline' onClick={() => setFocus(p)}>
                        复核
                      </Button>
                    </div>
                  ))}
                </section>
              )}
              {focus && (
                <SingleReview
                  key={`${focus.id}:${focus.review_revision}`}
                  product={focus}
                  act={act}
                  busy={busy}
                  close={() => setFocus(null)}
                  onChooseFamily={(ids) => {
                    setSelected(Object.fromEntries(ids.map((id) => [id, true])));
                    setTab(2);
                  }}
                  onCalculate={(p) => {
                    setFocus(p);
                    setTab(3);
                  }}
                />
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
              key={`${focus?.id || 'none'}:${focus?.monthly_id || 'latest'}`}
              product={focus}
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

function AsinTools({ asin }: { asin: string }) {
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1800);
    return () => clearTimeout(timer);
  }, [copied]);
  return (
    <>
      <Button
        size='sm'
        variant='ghost'
        className='h-7 w-7 p-0'
        aria-label={`复制 ASIN ${asin}`}
        title={copied ? '已复制 ASIN' : '复制 ASIN'}
        onClick={async () => {
          setError(false);
          try {
            await navigator.clipboard.writeText(asin);
            setCopied(true);
          } catch {
            setError(true);
          }
        }}
      >
        {copied ? (
          <Icons.check className='size-3.5 text-emerald-600' />
        ) : (
          <Icons.ecomCopy className='size-3.5 text-muted-foreground' />
        )}
      </Button>
      <a
        href={`https://www.amazon.com/dp/${asin}`}
        target='_blank'
        rel='noopener noreferrer'
        aria-label={`在 Amazon 查看 ${asin}`}
        title='在 Amazon 查看'
        className='text-muted-foreground hover:bg-muted inline-flex size-7 items-center justify-center rounded-md'
      >
        <Icons.externalLink className='size-3.5' />
      </a>
      {copied && (
        <span role='status' className='sr-only'>
          ASIN 已复制
        </span>
      )}
      {error && (
        <span role='alert' className='text-destructive text-xs'>
          复制失败，请手动选择 ASIN
        </span>
      )}
    </>
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
      {table.getRowModel().rows.map((row) => (
        <tr
          key={row.id}
          data-family={product.id}
          className={cn('border-t bg-primary/[0.035]', row.getIsSelected() && 'bg-muted/50')}
        >
          {row.getVisibleCells().map((cell) => (
            <td
              key={cell.id}
              className='break-words px-2 py-2 align-middle [&>*]:min-w-0 [&>*]:max-w-full [&_p]:whitespace-normal'
            >
              {flexRender(cell.column.columnDef.cell, cell.getContext())}
            </td>
          ))}
        </tr>
      ))}
      <tr id={`family-${product.id}`} className='border-b-2 border-primary/20 bg-primary/[0.035]'>
        <td colSpan={columns.length} className='p-3'>
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
  return (
    <div className='min-w-56 space-y-1 text-xs leading-5 tabular-nums'>
      <p>
        <span className='text-muted-foreground'>商品：</span>
        {measurementDimensions(
          product.product_length_cm,
          product.product_width_cm,
          product.product_height_cm,
          product.product_dimensions || product.product_dimensions_original
        )}
      </p>
      <p>
        <span className='text-muted-foreground'>净重：</span>
        {measurementNumber(product.weight_g)}
        {product.weight_g != null && ' g'}
      </p>
      <p>
        <span className='text-muted-foreground'>包装：</span>
        {measurementDimensions(
          product.package_length_cm,
          product.package_width_cm,
          product.package_height_cm,
          product.package_dimensions || product.package_dimensions_original
        )}
      </p>
      <p>
        <span className='text-muted-foreground'>毛重：</span>
        {measurementNumber(product.package_weight_g)}
        {product.package_weight_g != null && ' g'}
      </p>
    </div>
  );
}

function SingleReview(props: Parameters<typeof SingleReviewContent>[0]) {
  const detail = useScreeningQuery<Product>(`/products/` + props.product.id);
  if (detail.isError) return <p role='alert'>{detail.error.message}</p>;
  if (!detail.data) return <p role='status'>正在加载商品详情…</p>;
  return (
    <SingleReviewContent
      {...props}
      product={detail.data}
      key={detail.data.id + ':' + detail.data.data_revision + ':' + detail.data.review_revision}
    />
  );
}

function SingleReviewContent({
  product,
  act,
  busy,
  close,
  onCalculate,
  onChooseFamily
}: {
  product: Product;
  act: Action;
  busy: boolean;
  close: () => void;
  onCalculate: (product: Product) => void;
  onChooseFamily: (ids: string[]) => void;
}) {
  const [status, setStatus] = useState<ReviewStatus>(
    product.review_status === 'pending' ? 'kept' : product.review_status
  );
  const [reason, setReason] = useState(product.review_reason);
  const [manualCategory, setManualCategory] = useState(product.manual_category || '');
  const [note, setNote] = useState(product.review_note);
  const history = useScreeningQuery<unknown>(`/products/${product.id}/history`);
  const observations = useScreeningQuery<unknown>(`/products/${product.id}/observations`);
  return (
    <section className='space-y-3 rounded border p-4'>
      <div className='flex justify-between'>
        <h3 className='font-semibold'>人工复核 · {product.asin}</h3>
        <Button variant='ghost' onClick={close}>
          关闭
        </Button>
      </div>
      <p>{product.title}</p>
      <div className='flex flex-wrap items-end gap-2'>
        <Field label='人工分类'>
          <Input value={manualCategory} onChange={(e) => setManualCategory(e.target.value)} />
        </Field>
        <Button
          variant='outline'
          disabled={busy || !manualCategory.trim()}
          onClick={() =>
            void act(async () => {
              await request(`/products/${product.id}/category`, 'PATCH', {
                expected_data_revision: product.data_revision,
                expected_review_revision: product.review_revision,
                category: manualCategory
              });
              close();
            }, '人工分类已保存')
          }
        >
          保存人工分类
        </Button>
      </div>
      <DataBlock title='商品规格' value={product.specs} />
      <ProductParameters product={product} act={act} busy={busy} close={close} />
      <FamilyPanel product={product} choose={onChooseFamily} />
      <MonthlyHistory product={product} onCalculate={onCalculate} />
      <div className='grid gap-3 md:grid-cols-3'>
        <Field label='人工结论'>
          <select
            className='rounded border bg-transparent p-2'
            value={status}
            onChange={(e) => setStatus(e.target.value as ReviewStatus)}
          >
            <option value='kept'>保留</option>
            <option value='excluded'>排除</option>
            <option value='unsure'>待确认</option>
          </select>
        </Field>
        <Field label='原因'>
          <Input value={reason} onChange={(e) => setReason(e.target.value)} />
        </Field>
        <Field label='备注'>
          <Input value={note} onChange={(e) => setNote(e.target.value)} />
        </Field>
      </div>
      <Button
        disabled={busy}
        onClick={() =>
          void act(async () => {
            await request(`/products/${product.id}/review`, 'PATCH', {
              expected_review_revision: product.review_revision,
              expected_data_revision: product.data_revision,
              status,
              reason,
              note
            });
            close();
          }, '人工结论已保存')
        }
      >
        保存复核结论
      </Button>
      <DataBlock title='商品完整资料（主表）' value={product} />
      <DataBlock title='操作历史' value={history.data} />
      <DataBlock title='来源历史' value={observations.data} />
    </section>
  );
}
