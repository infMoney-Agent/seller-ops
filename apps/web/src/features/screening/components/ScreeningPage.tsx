'use client';
/* oxlint-disable react/no-unstable-nested-components -- TanStack column renderers are stable useMemo callbacks. */
import { useEffect, useMemo, useState } from 'react';
import Image from 'next/image';
import { useQueryClient } from '@tanstack/react-query';
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type RowSelectionState
} from '@tanstack/react-table';
import PageContainer from '@/components/layout/page-container';
import { useSellerSession } from '@/features/auth/session';
import { cn } from '@/lib/utils';
import { useJob, useScreeningQuery } from '../queries';
import { download, request } from '../service';
import type { ImportRecord, JobCreated, PageResult, Product, ReviewStatus } from '../types';
import ImportPanel from './ImportPanel';
import ReviewPanel from './ReviewPanel';
import ProfitPanel from './ProfitPanel';
import PolicyPanel from './PolicyPanel';
import { Badge, Button, DataBlock, Field, Input, type Action } from './shared';

const tabs = ['商品库', '导入记录', '批量复核', '利润测算', '规则与历史'];
export default function ScreeningPage() {
  const session = useSellerSession();
  const client = useQueryClient();
  const [tab, setTab] = useState(0);
  const [page, setPage] = useState(1);
  const [filters, setFilters] = useState<Record<string, string>>({ show_merged: 'false' });
  const [selected, setSelected] = useState<RowSelectionState>({});
  const [focus, setFocus] = useState<Product | null>(null);
  const [group, setGroup] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [jobs, setJobs] = useState<string[]>([]);
  const [activeJob, setActiveJob] = useState<string | null>(null);
  const query = new URLSearchParams({ ...filters, page: String(page) }).toString();
  const products = useScreeningQuery<PageResult<Product>>('/products?' + query);
  const counts = useScreeningQuery<Record<string, number>>(
    '/summary?' + new URLSearchParams(filters)
  );
  const imports = useScreeningQuery<PageResult<ImportRecord>>('/imports');
  const [groupPage, setGroupPage] = useState(1);
  const expanded = useScreeningQuery<PageResult<Product>>(
    group ? `/groups/${group}/products?page=${groupPage}` : null
  );
  const job = useJob(activeJob);
  const selectedIds = Object.keys(selected).filter((id) => selected[id]);
  const refresh = async () => {
    await client.invalidateQueries({ queryKey: ['screening', session.workspaceId] });
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
  function watch(id: string) {
    setJobs((old) => [...new Set([id, ...old])]);
    setActiveJob(id);
  }
  useEffect(() => {
    setSelected({});
    setFocus(null);
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
        header: ({ table }) => (
          <input
            aria-label='选择当前页'
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
          <div className='flex max-w-md gap-3'>
            {row.original.image_url && (
              <Image
                unoptimized
                width={64}
                height={64}
                src={row.original.image_url}
                alt=''
                className='h-16 w-16 rounded bg-white object-contain'
                loading='lazy'
                referrerPolicy='no-referrer'
              />
            )}
            <div>
              <button
                className='font-medium underline underline-offset-4'
                onClick={() => setFocus(row.original)}
              >
                {row.original.asin}
              </button>
              <p className='line-clamp-2'>{row.original.title_zh || row.original.title}</p>
              <p className='text-muted-foreground text-xs'>
                {row.original.seller_name} · {row.original.seller_region || '地区未知'}
              </p>
            </div>
          </div>
        )
      },
      {
        accessorKey: 'price_usd',
        header: '价格 / 月销量',
        cell: ({ row }) => (
          <span>
            ${row.original.price_usd ?? '—'} / {row.original.monthly_sales ?? '—'}
          </span>
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
              row.original.profit_data_revision !== row.original.data_revision && (
                <p className='text-xs'>来源已更新</p>
              )}
          </div>
        )
      },
      {
        id: 'actions',
        header: '操作',
        cell: ({ row }) => (
          <div className='flex gap-1'>
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
    []
  );
  // oxlint-disable-next-line react/incompatible-library -- TanStack Table is intentionally kept outside React Compiler memoization.
  const table = useReactTable({
    data: products.data?.items || [],
    columns,
    state: { rowSelection: selected },
    onRowSelectionChange: setSelected,
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    enableRowSelection: true
  });
  return (
    <div className='w-full min-w-0 max-w-full overflow-x-hidden'>
      <PageContainer
        pageTitle='产品初筛'
        pageDescription='导入、筛选、人工复核与利润测算，保留每次判断的来源。'
        pageHeaderAction={
          <Button variant='outline' onClick={() => void refresh()}>
            刷新
          </Button>
        }
      >
        <div className='space-y-4'>
          <nav className='flex flex-wrap gap-2 border-b pb-3' aria-label='初筛工作台'>
            {tabs.map((title, i) => (
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
          {activeJob && (
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
                {['failed', 'partial'].includes(job.data?.state || '') && (
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
              <div className='grid grid-cols-2 gap-3 md:grid-cols-4'>
                {[
                  ['source_rows', '来源行数'],
                  ['products', '去重商品'],
                  ['displayed', '折叠展示'],
                  ['pending', '待复核']
                ].map(([key, title]) => (
                  <div key={key} className='rounded-lg border p-4'>
                    <span className='text-muted-foreground text-sm'>{title}</span>
                    <p className='mt-1 text-2xl font-semibold'>{counts.data?.[key] ?? '—'}</p>
                  </div>
                ))}
              </div>
              <div className='grid gap-3 rounded border p-3 md:grid-cols-4'>
                <Field label='关键词 / ASIN'>
                  <Input value={filters.q || ''} onChange={(e) => filter('q', e.target.value)} />
                </Field>
                <Field label='人工结论'>
                  <select
                    className='rounded border bg-transparent p-2'
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
                <label className='flex items-center gap-2 text-sm'>
                  <input
                    type='checkbox'
                    checked={filters.show_merged === 'true'}
                    onChange={(e) => filter('show_merged', String(e.target.checked))}
                  />{' '}
                  展示全部归并商品
                </label>
              </div>
              <div className='flex flex-wrap items-center gap-2'>
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
              {products.isLoading ? (
                <p role='status'>正在加载商品…</p>
              ) : (
                <div className='overflow-auto rounded border'>
                  <table className='w-full text-left text-sm'>
                    <thead className='bg-muted'>
                      {table.getHeaderGroups().map((g) => (
                        <tr key={g.id}>
                          {g.headers.map((h) => (
                            <th key={h.id} className='whitespace-nowrap p-3'>
                              {flexRender(h.column.columnDef.header, h.getContext())}
                            </th>
                          ))}
                        </tr>
                      ))}
                    </thead>
                    <tbody>
                      {table.getRowModel().rows.map((row) => (
                        <tr
                          key={row.id}
                          className={cn('border-t', row.getIsSelected() && 'bg-muted/50')}
                        >
                          {row.getVisibleCells().map((cell) => (
                            <td key={cell.id} className='p-3 align-top'>
                              {flexRender(cell.column.columnDef.cell, cell.getContext())}
                            </td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  {!products.data?.items.length && (
                    <div className='p-10 text-center'>
                      <p>还没有符合条件的商品</p>
                      <Button variant='link' onClick={() => setTab(1)}>
                        导入商品文件
                      </Button>
                    </div>
                  )}
                </div>
              )}
              <div className='flex items-center gap-3'>
                <Button
                  variant='outline'
                  disabled={page === 1}
                  onClick={() => setPage((v) => v - 1)}
                >
                  上一页
                </Button>
                <span className='text-sm'>
                  第 {page} 页 / 共 {products.data?.total || 0} 项
                </span>
                <Button
                  variant='outline'
                  disabled={page * 50 >= (products.data?.total || 0)}
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
                />
              )}
            </>
          )}
          {tab === 1 && <ImportPanel act={act} watch={watch} busy={busy} />}
          {tab === 2 && <ReviewPanel selected={selectedIds} act={act} busy={busy} />}
          {tab === 3 && (
            <ProfitPanel
              key={focus?.id || 'none'}
              product={focus}
              selected={selectedIds}
              act={act}
              watch={watch}
              busy={busy}
            />
          )}
          {tab === 4 && <PolicyPanel selected={selectedIds} act={act} watch={watch} busy={busy} />}
        </div>
      </PageContainer>
    </div>
  );
}

function SingleReview({
  product,
  act,
  busy,
  close
}: {
  product: Product;
  act: Action;
  busy: boolean;
  close: () => void;
}) {
  const [status, setStatus] = useState<ReviewStatus>(
    product.review_status === 'pending' ? 'kept' : product.review_status
  );
  const [reason, setReason] = useState(product.review_reason);
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
      <DataBlock title='商品规格' value={product.specs} />
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
      <DataBlock title='操作历史' value={history.data} />
      <DataBlock title='来源历史' value={observations.data} />
    </section>
  );
}
