'use client';
import Image from 'next/image';
import { memo, useCallback, useEffect, useMemo, useState } from 'react';
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type ColumnSizingState
} from '@tanstack/react-table';
import { Dialog } from '@/components/ui/dialog';
import { cn } from '@/lib/utils';
import { useScreeningQuery } from '../queries';
import { useProfitAutosave } from '../use-profit-autosave';
import { childSales } from '../sales';
import {
  commonProfitFields,
  initialProfitDraft,
  percentToRate,
  profitFieldLabels,
  profitNumber,
  rateToPercent,
  supplierLink,
  type ProfitRowDraft,
  type ProfitTableProduct
} from '../profit-table';
import type { PageResult, Product, ProfitInputs } from '../types';
import { Button, Field, Input, TableScroll, type Action } from './shared';
import BilingualProductTitle from './BilingualProductTitle';
import DataDialogContent from './DataDialogContent';
import ProfitScenarioEditor from './ProfitScenarioEditor';

const weight = (value: string | undefined) => (value ? profitNumber(value, 3) + ' kg' : '—');

const columns: Array<{ id: string; label: string; min: number; sort?: string }> = [
  { id: 'product', label: '商品', min: 220 },
  { id: 'specs', label: '售价与规格', min: 170, sort: 'price_usd' },
  { id: 'fba', label: 'FBA', min: 76, sort: 'fba_fee_usd' },
  { id: 'storage', label: '仓租', min: 88, sort: 'storage_usd' },
  { id: 'volume', label: '体积重', min: 76, sort: 'volumetric_weight_kg' },
  { id: 'billable', label: '计费重', min: 76, sort: 'billable_weight_kg' },
  { id: 'freight_rate', label: '头程单价', min: 78, sort: 'freight_rate_cny_per_kg' },
  { id: 'freight', label: '头程', min: 76, sort: 'freight_cny' },
  { id: 'purchase', label: '供货价', min: 88, sort: 'purchase_cny' },
  { id: 'supplier', label: '供应商链接', min: 120 },
  { id: 'commission_rate', label: '佣金比', min: 120, sort: 'commission_rate' },
  { id: 'commission', label: '佣金', min: 76, sort: 'commission_usd' },
  { id: 'profit', label: '单件利润 CNY', min: 112, sort: 'profit_cny' },
  { id: 'margin', label: '利润率', min: 76, sort: 'margin' },
  { id: 'actions', label: '状态', min: 88 }
];
type UpdateRow = (
  id: string,
  update: (old: ProfitRowDraft) => ProfitRowDraft,
  product: ProfitTableProduct
) => void;

export default function ProfitPanel({
  product,
  onClearFocus,
  selected,
  filters,
  act,
  watch,
  busy
}: {
  product: Product | null;
  onClearFocus: () => void;
  selected: string[];
  filters: Record<string, string>;
  act: Action;
  watch: (id: string) => void;
  busy: boolean;
}) {
  const [page, setPage] = useState(1),
    [pageSize, setPageSize] = useState(50),
    [search, setSearch] = useState(''),
    [query, setQuery] = useState(''),
    [sort, setSort] = useState(''),
    [onlySelected, setOnlySelected] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, ProfitRowDraft>>({});
  const autosave = useProfitAutosave(drafts, setDrafts);
  const [editor, setEditor] = useState<ProfitTableProduct | null>(null),
    [common, setCommon] = useState<Partial<ProfitInputs>>({});
  const [container, setContainer] = useState<HTMLDivElement | null>(null),
    [width, setWidth] = useState(0),
    [sizing, setSizing] = useState<ColumnSizingState>({});
  useEffect(() => {
    if (!container) return;
    const measure = () => setWidth(container.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(container);
    return () => observer.disconnect();
  }, [container]);
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
  if (query) params.set('q', query);
  if (sort) params.set('sort', sort);
  if (product) params.set('product_id', product.id);
  else if (onlySelected && selected.length) params.set('selected_ids', selected.join(','));
  const rows = useScreeningQuery<
    PageResult<ProfitTableProduct> & { referral_categories: Array<{ id: string; label: string }> }
  >('/profit/products?' + params, false, true);
  const data = rows.data?.items || [];
  const definitions = useMemo<ColumnDef<ProfitTableProduct>[]>(() => {
    const min = columns.reduce((sum, c) => sum + c.min, 0),
      extra = Math.max(0, width - min);
    return columns.map((c) => ({
      id: c.id,
      header: c.label,
      minSize: c.min,
      maxSize: 900,
      size:
        c.min +
        (c.id === 'product'
          ? extra * 0.5
          : c.id === 'specs'
            ? extra * 0.3
            : c.id === 'supplier'
              ? extra * 0.2
              : 0)
    }));
  }, [width]);
  // TanStack Table manages mutable sizing state; React Compiler must skip this call.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data,
    columns: definitions,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (p) => p.id,
    columnResizeMode: 'onChange',
    state: { columnSizing: sizing },
    onColumnSizingChange: setSizing
  });
  const updateRow = useCallback<UpdateRow>(
    (id, update, p) =>
      setDrafts((old) => ({ ...old, [id]: update(old[id] || initialProfitDraft(p)) })),
    []
  );
  const openEditor = useCallback((p: ProfitTableProduct) => setEditor(p), []);
  const editorDraft = editor ? drafts[editor.id] || initialProfitDraft(editor) : null;
  const total = rows.data?.total || 0;
  return (
    <div className='min-w-0 space-y-4' data-profit-table-panel>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-sm'>
          {product ? '当前商品' : onlySelected ? '已选商品' : '已保留商品'} ·{' '}
          {total.toLocaleString()} 个子体
        </p>
        <form
          className='flex flex-wrap items-center gap-2'
          role='search'
          aria-label='搜索利润商品'
          onSubmit={(e) => {
            e.preventDefault();
            setQuery(search.trim());
            setPage(1);
          }}
        >
          <Input
            aria-label='搜索 ASIN 或商品标题'
            placeholder='搜索 ASIN / 商品标题'
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className='w-56'
          />
          <Button type='submit' variant='outline'>
            搜索
          </Button>
          {product && (
            <Button type='button' variant='outline' onClick={onClearFocus}>
              查看全部保留商品
            </Button>
          )}
          {!product && selected.length > 0 && (
            <Button
              type='button'
              variant='outline'
              aria-pressed={onlySelected}
              onClick={() => {
                setOnlySelected(!onlySelected);
                setPage(1);
              }}
            >
              {onlySelected ? '查看已保留商品' : '仅看已选 ' + selected.length + ' 项'}
            </Button>
          )}
        </form>
      </div>
      <details className='rounded-lg border p-3'>
        <summary className='cursor-pointer text-sm font-medium'>公共测算参数</summary>
        <div className='mt-3 grid gap-3 sm:grid-cols-3 xl:grid-cols-5'>
          {commonProfitFields.map(([key, label, percent]) => (
            <Field key={key} label={label}>
              {percent ? (
                <PercentInput
                  label={label}
                  value={common[key] as string | null | undefined}
                  disabled={busy}
                  onChange={(v) => setCommon((old) => ({ ...old, [key]: v }))}
                />
              ) : (
                <Input
                  inputMode='decimal'
                  value={common[key] ?? ''}
                  disabled={busy}
                  onChange={(e) => setCommon((old) => ({ ...old, [key]: e.target.value || null }))}
                />
              )}
            </Field>
          ))}
        </div>
        <div className='mt-3 flex flex-wrap items-center gap-2'>
          <Button
            variant='outline'
            disabled={busy || !data.length}
            onClick={() => {
              setDrafts((old) => {
                const next = { ...old };
                for (const p of data) {
                  const d = old[p.id] || initialProfitDraft(p);
                  const patch = Object.fromEntries(
                    Object.entries(common).filter(
                      ([, value]) => value !== null && value !== undefined && value !== ''
                    )
                  );
                  next[p.id] = {
                    ...d,
                    inputs: { ...d.inputs, ...patch },
                    dirty: true,
                    output: null,
                    error: ''
                  };
                }
                return next;
              });
            }}
          >
            应用到本页
          </Button>
          <Button
            variant='outline'
            disabled={busy || !data.length}
            onClick={() => {
              setDrafts((old) => {
                const next = { ...old };
                for (const p of data) {
                  const d = old[p.id] || initialProfitDraft(p),
                    inputs = { ...d.inputs };
                  for (const key of [
                    'discount_usd',
                    'packaging_cny',
                    'other_landed_cny',
                    'other_usd',
                    'loss_per_return_usd'
                  ] as const)
                    if (inputs[key] === null || inputs[key] === '') inputs[key] = '0';
                  next[p.id] = { ...d, inputs, dirty: true, output: null, error: '' };
                }
                return next;
              });
            }}
          >
            空白可选费用填 0
          </Button>
          <span className='text-muted-foreground text-xs'>
            优惠、包装、其他费用及退货净损失；修改后按行保存。
          </span>
        </div>
      </details>
      <p className='text-xs text-muted-foreground'>
        每个子体单独测算，基于输入假设的预计利润。费用缺失显示待补充；排序按已保存参数。仓租按单件预计费用填写。
      </p>
      {rows.isError && (
        <p role='alert' className='text-destructive text-sm'>
          {rows.error.message}
        </p>
      )}
      <p role='status' className='text-muted-foreground text-xs'>
        {rows.isFetching
          ? rows.data
            ? '正在更新利润表…'
            : '正在加载利润表…'
          : total + ' 个商品 · 第 ' + page + ' 页'}
      </p>
      <TableScroll
        ref={setContainer}
        label='利润测算表，可独立横向和纵向滚动'
        aria-busy={rows.isFetching}
        className='bg-card max-h-[65vh] w-full min-w-0 max-w-full overflow-auto overscroll-contain rounded-lg border [contain:inline-size]'
      >
        <table
          className='table-fixed border-separate border-spacing-0 text-center text-xs'
          style={{ width: table.getTotalSize() }}
        >
          <caption className='sr-only'>
            已保留商品的售价规格、费用、供货信息和人民币利润测算
          </caption>
          <colgroup>
            {table.getVisibleLeafColumns().map((c) => (
              <col key={c.id} style={{ width: c.getSize() }} />
            ))}
          </colgroup>
          <thead className='sticky top-0 z-20 bg-muted'>
            {table.getHeaderGroups().map((group) => (
              <tr key={group.id}>
                {group.headers.map((header) => {
                  const spec = columns.find((c) => c.id === header.id)!;
                  const active = sort.replace(/^-/, '') === spec.sort;
                  return (
                    <th
                      scope='col'
                      key={header.id}
                      aria-sort={
                        active ? (sort.startsWith('-') ? 'descending' : 'ascending') : undefined
                      }
                      className={cn(
                        'relative whitespace-nowrap border-b bg-muted p-2 align-middle',
                        spec.sort && 'p-0',
                        ['product', 'specs', 'supplier'].includes(spec.id) && 'text-left',
                        spec.id === 'product' && 'sticky left-0 z-30'
                      )}
                    >
                      {spec.sort ? (
                        <button
                          type='button'
                          className='min-h-11 w-full px-2 font-semibold focus-visible:outline-2 focus-visible:outline-primary'
                          onClick={() => {
                            setSort(sort === spec.sort ? '-' + spec.sort : spec.sort!);
                            setPage(1);
                          }}
                        >
                          {flexRender(header.column.columnDef.header, header.getContext())}
                          <span aria-hidden='true' className='ms-1 text-muted-foreground'>
                            {active ? (sort.startsWith('-') ? '↓' : '↑') : '↕'}
                          </span>
                        </button>
                      ) : (
                        flexRender(header.column.columnDef.header, header.getContext())
                      )}
                      <button
                        type='button'
                        aria-label={'调整' + spec.label + '列宽'}
                        title='拖动调整列宽；双击还原'
                        data-column-resize
                        className='absolute inset-y-0 right-0 z-20 w-3 touch-none cursor-col-resize border-r-2 border-transparent hover:border-primary focus-visible:border-primary focus-visible:outline-2'
                        onClick={(e) => e.stopPropagation()}
                        onMouseDown={header.getResizeHandler()}
                        onTouchStart={header.getResizeHandler()}
                        onDoubleClick={() =>
                          setSizing((old) => {
                            const next = { ...old };
                            delete next[header.id];
                            return next;
                          })
                        }
                        onKeyDown={(e) => {
                          if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
                            e.preventDefault();
                            setSizing((old) => ({
                              ...old,
                              [header.id]: Math.max(
                                spec.min,
                                Math.min(
                                  900,
                                  header.getSize() + (e.key === 'ArrowRight' ? 16 : -16)
                                )
                              )
                            }));
                          } else if (e.key === 'Home') {
                            e.preventDefault();
                            setSizing((old) => {
                              const next = { ...old };
                              delete next[header.id];
                              return next;
                            });
                          }
                        }}
                      />
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {data.map((p) => (
              <ProfitTableRow
                referralCategories={rows.data?.referral_categories || []}
                key={p.id}
                product={p}
                draft={drafts[p.id]}
                update={updateRow}
                retry={() => autosave.retry(p.id)}
                flush={() => autosave.flush(p.id)}
                openEditor={openEditor}
                busy={busy || rows.isFetching}
              />
            ))}
          </tbody>
        </table>
        {!rows.isLoading && !data.length && (
          <p className='p-8 text-center text-sm text-muted-foreground'>
            暂无符合条件的已保留商品。
          </p>
        )}
      </TableScroll>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <Field label='每页展示'>
          <select
            aria-label='利润表每页条数'
            className='h-9 rounded border bg-background px-2'
            value={pageSize}
            onChange={(e) => {
              setPageSize(Number(e.target.value));
              setPage(1);
            }}
          >
            {[10, 20, 50, 100].map((n) => (
              <option key={n} value={n}>
                {n} 个
              </option>
            ))}
          </select>
        </Field>
        <div className='flex items-center gap-3'>
          <Button
            variant='outline'
            disabled={rows.isFetching || page <= 1}
            onClick={() => setPage(page - 1)}
          >
            上一页
          </Button>
          <span className='text-sm'>
            {page} / {Math.max(1, Math.ceil(total / pageSize))}
          </span>
          <Button
            variant='outline'
            disabled={rows.isFetching || page * pageSize >= total}
            onClick={() => setPage(page + 1)}
          >
            下一页
          </Button>
        </div>
      </div>
      {editor && editorDraft && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open && !busy) setEditor(null);
          }}
        >
          <DataDialogContent
            title={'测算参数与历史 · ' + editor.asin}
            description='编辑完整参数、比较方案或查看已保存记录'
            closeDisabled={busy}
          >
            <ProfitScenarioEditor
              product={{
                ...editor,
                data_revision: editorDraft.dataRevision,
                monthly_id: editorDraft.monthlyId,
                monthly_revision: editorDraft.monthlyRevision
              }}
              selected={selected}
              filters={filters}
              act={act}
              watch={watch}
              busy={busy}
              initialInputs={editorDraft.inputs}
              onSaved={(scenario) =>
                updateRow(
                  editor.id,
                  (old) => ({
                    ...old,
                    inputs: scenario.inputs,
                    output: scenario.output,
                    dirty: false,
                    saveState: 'saved',
                    error: ''
                  }),
                  editor
                )
              }
            />
          </DataDialogContent>
        </Dialog>
      )}
    </div>
  );
}

const ProfitTableRow = memo(function ProfitTableRow({
  product: p,
  referralCategories,
  draft: cached,
  update,
  retry,
  flush,
  openEditor,
  busy
}: {
  product: ProfitTableProduct;
  referralCategories: Array<{ id: string; label: string }>;
  draft?: ProfitRowDraft;
  update: UpdateRow;
  retry: () => void;
  flush: () => void;
  openEditor: (p: ProfitTableProduct) => void;
  busy: boolean;
}) {
  const draft = cached || initialProfitDraft(p),
    input = draft.inputs;
  const set = (key: keyof ProfitInputs, value: string | null) => {
    update(
      p.id,
      (old) => ({
        ...old,
        inputs: {
          ...old.inputs,
          ...(key === 'commission_rate'
            ? { commission_mode: value === null || value === '' ? 'auto' : 'manual' }
            : key === 'commission_category'
              ? { commission_mode: 'auto', commission_rate: null }
              : {}),
          [key]: value,
          ...(key === 'storage_usd'
            ? { storage_mode: value === null || value === '' ? 'auto' : 'manual' }
            : {})
        },
        dirty: true,
        output: null,
        error: ''
      }),
      p
    );
  };
  const disabled = busy;
  const field = (key: keyof ProfitInputs, label: string) => (
    <Input
      aria-label={p.asin + ' ' + label}
      inputMode='decimal'
      className='h-8 min-w-0 px-2 text-center text-xs'
      disabled={disabled}
      value={String(
        key === 'storage_usd' && input.storage_mode === 'auto'
          ? (draft.output?.storage_usd ?? input[key] ?? '')
          : (input[key] ?? '')
      )}
      onChange={(e) => set(key, e.target.value || null)}
    />
  );
  const output = draft.output,
    link = supplierLink(input.supplier_url);
  const td = 'border-b p-2 align-middle';
  return (
    <tr
      data-profit-row={p.id}
      className='last:[&_td]:border-b-0'
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) flush();
      }}
    >
      <td className={cn(td, 'sticky left-0 z-10 bg-card text-left')}>
        <div className='flex items-center gap-2'>
          {p.image_url && (
            <Image
              unoptimized
              src={p.image_url}
              alt=''
              width={52}
              height={64}
              loading='lazy'
              className='h-16 w-13 shrink-0 rounded border bg-white object-contain'
            />
          )}
          <div className='min-w-0'>
            <a
              href={'https://www.amazon.com/dp/' + p.asin}
              target='_blank'
              rel='noreferrer'
              className='block focus-visible:outline-2'
            >
              <BilingualProductTitle product={p} />
            </a>
            <p className='mt-1 text-[11px] text-muted-foreground'>
              {p.asin} · 子体销量 {childSales(p)}
            </p>
          </div>
        </div>
      </td>
      <td className={cn(td, 'text-left space-y-1')}>
        <div className='flex items-center gap-1'>
          {field('sale_price_usd', '售价 USD')}
          <span>USD</span>
        </div>
        <p className='text-[11px] text-muted-foreground'>
          {p.stat_month?.slice(0, 7) || '来源期未注明'} · 来源 ${p.price_usd ?? '—'}
        </p>
        <p className='font-medium'>商品规格</p>
        <p className='break-words text-[11px] text-muted-foreground'>
          {p.product_dimensions || p.product_dimensions_original || '—'} ·{' '}
          {p.weight_g ? profitNumber(p.weight_g, 1) + ' g' : '—'}
        </p>
        <p className='font-medium'>包装规格</p>
        <p className='break-words text-[11px] text-muted-foreground'>
          {p.package_dimensions || p.package_dimensions_original || '—'} ·{' '}
          {p.package_weight_g ? profitNumber(p.package_weight_g, 1) + ' g' : '—'}
        </p>
      </td>
      <td className={td}>
        {field('fba_fee_usd', 'FBA USD')}
        <p className='mt-1 text-[11px] text-muted-foreground'>USD / 单件</p>
      </td>
      <td className={td}>
        {field('storage_usd', '仓租 USD')}
        <Field label='预计在库天数'>{field('storage_days', '预计在库天数')}</Field>
        <p className='mt-1 text-[11px] text-muted-foreground'>
          {output?.storage_source === 'estimate'
            ? '自动估算 · ' + output.storage_month
            : output?.storage_source === 'import'
              ? '来源金额'
              : input.storage_mode === 'manual'
                ? '手填'
                : '待补充'}
        </p>
        {input.storage_mode === 'manual' && (
          <Button
            size='sm'
            variant='ghost'
            disabled={disabled}
            onClick={() => set('storage_usd', null)}
          >
            恢复自动
          </Button>
        )}
        <p className='mt-1 text-[11px] text-muted-foreground'>USD / 单件</p>
      </td>
      <td className={td}>
        {weight(output?.volumetric_weight_kg)}
        <p className='mt-1 break-words text-[11px] text-muted-foreground'>
          长 × 宽 × 高<br />
          {input.volumetric_divisor_cm3_per_kg
            ? '÷ ' + input.volumetric_divisor_cm3_per_kg
            : '分母待填写'}
        </p>
      </td>
      <td className={td}>
        {weight(output?.billable_weight_kg)}
        <p className='mt-1 text-[11px] text-muted-foreground'>
          包装实重与
          <br />
          体积重取大
        </p>
      </td>
      <td className={td}>
        {field('freight_rate_cny_per_kg', '头程单价 CNY/kg')}
        <p className='mt-1 text-[11px] text-muted-foreground'>CNY/kg</p>
      </td>
      <td className={td}>
        {profitNumber(output?.freight_cny ?? input.freight_cny)}
        <p className='mt-1 text-[11px] text-muted-foreground'>CNY</p>
      </td>
      <td className={td}>
        {field('purchase_cny', '供货价 CNY')}
        <p className='mt-1 text-[11px] text-muted-foreground'>CNY / 单件</p>
      </td>
      <td className={cn(td, 'text-left')}>
        <Input
          aria-label={p.asin + ' 供应商链接'}
          type='url'
          className='h-8 min-w-0 px-2 text-xs'
          disabled={disabled}
          value={input.supplier_url || ''}
          onChange={(e) => set('supplier_url', e.target.value)}
        />
        {link && (
          <a href={link} target='_blank' rel='noreferrer' className='mt-1 inline-block underline'>
            打开链接
          </a>
        )}
      </td>
      <td className={td}>
        <label className='sr-only' htmlFor={'commission-category-' + p.id}>
          佣金类目
        </label>
        <select
          id={'commission-category-' + p.id}
          aria-label={p.asin + ' 佣金类目'}
          className='mb-1 h-8 w-full min-w-0 rounded border bg-background px-1 text-xs'
          disabled={disabled}
          value={input.commission_category || output?.commission_category || ''}
          onChange={(e) => set('commission_category', e.target.value || null)}
        >
          <option value=''>自动匹配类目</option>
          {referralCategories.map((c) => (
            <option key={c.id} value={c.id}>
              {c.label}
            </option>
          ))}
        </select>
        <PercentInput
          label={p.asin + ' 佣金率 %'}
          value={
            input.commission_mode === 'auto'
              ? (output?.commission_rate ?? input.commission_rate)
              : input.commission_rate
          }
          disabled={disabled}
          onChange={(v) => set('commission_rate', v)}
        />
        <p className='mt-1 text-[11px] text-muted-foreground'>
          {input.commission_mode === 'manual'
            ? '手填 %'
            : output?.commission_source === 'auto'
              ? '自动 %'
              : '待匹配'}
        </p>
        {input.commission_mode === 'manual' && (
          <Button
            size='sm'
            variant='ghost'
            disabled={disabled}
            onClick={() => set('commission_rate', null)}
          >
            恢复自动
          </Button>
        )}
      </td>
      <td className={td}>
        {profitNumber(output?.commission_usd)}
        <p className='mt-1 text-[11px] text-muted-foreground'>USD</p>
      </td>
      <td
        className={cn(
          td,
          output?.state === 'complete' && Number(output.profit_cny) < 0 ? 'text-destructive' : ''
        )}
      >
        <p className='font-semibold'>
          {output?.state === 'complete' ? profitNumber(output.profit_cny) + ' CNY' : '待补充'}
        </p>
        <p className='mt-1 text-[11px] text-muted-foreground'>汇率 {input.fx_cny_per_usd || '—'}</p>
        {output?.state !== 'complete' && (
          <button
            type='button'
            className='mt-1 text-[11px] underline'
            onClick={() => openEditor(p)}
          >
            {output?.missing
              ?.map((key) => profitFieldLabels[key] || key)
              .slice(0, 3)
              .join('、') || '测算参数'}
            {(output?.missing?.length || 0) > 3 ? '等' : ''}
          </button>
        )}
      </td>
      <td className={cn(td, 'font-semibold')}>
        {output?.state === 'complete' && output.margin !== undefined
          ? profitNumber(Number(output.margin) * 100) + '%'
          : '—'}
      </td>
      <td className={td}>
        <div className='flex flex-col items-center gap-1'>
          <span
            role='status'
            className={cn(
              'text-[11px]',
              draft.saveState === 'failed' ? 'text-destructive' : 'text-muted-foreground'
            )}
          >
            {draft.saveState === 'saving'
              ? '保存中…'
              : draft.saveState === 'failed'
                ? '保存失败'
                : draft.dirty
                  ? '等待保存…'
                  : draft.saveState === 'saved' || p.scenario
                    ? '已保存'
                    : '未修改'}
          </span>
          {draft.saveState === 'failed' && (
            <Button size='sm' variant='outline' disabled={disabled} onClick={retry}>
              重试保存
            </Button>
          )}
          <Button
            size='sm'
            variant='ghost'
            disabled={disabled || draft.dirty}
            onClick={() => openEditor(p)}
          >
            参数 / 历史
          </Button>
        </div>
        {p.scenario_stale && <p className='mt-1 text-[11px] text-amber-700'>来源已更新</p>}
        {draft.error && (
          <p role='alert' className='mt-1 break-words text-[11px] text-destructive'>
            {draft.error}
          </p>
        )}
      </td>
    </tr>
  );
});

function PercentInput({
  value,
  onChange,
  onBlur,
  label,
  disabled
}: {
  value: string | null | undefined;
  onChange: (value: string | null) => void;
  onBlur?: () => void;
  label: string;
  disabled?: boolean;
}) {
  const [raw, setRaw] = useState(() => rateToPercent(value)),
    [focused, setFocused] = useState(false);
  return (
    <Input
      aria-label={label}
      inputMode='decimal'
      className='h-8 min-w-0 px-2 text-center text-xs'
      disabled={disabled}
      value={focused ? raw : rateToPercent(value)}
      onFocus={() => {
        setRaw(rateToPercent(value));
        setFocused(true);
      }}
      onChange={(e) => {
        setRaw(e.target.value);
        onChange(percentToRate(e.target.value));
      }}
      onBlur={() => {
        setFocused(false);
        onBlur?.();
      }}
    />
  );
}
