'use client';
import { useEffect, useRef, useState } from 'react';
import { useScreeningQuery } from '../queries';
import { download, request } from '../service';
import { importFields } from '../import-fields';
import { sourceLabels } from './MonthlyHistory';
import { defaultObservationDate, sourcePeriodFromFilename } from '../import-dates';
import type { ImportRecord, ImportRow, JobCreated, PageResult } from '../types';
import { Badge, Button, DataBlock, Field, Input, TableScroll, type Action } from './shared';

export default function ImportPanel({
  act,
  watch,
  busy
}: {
  act: Action;
  watch: (id: string) => void;
  busy: boolean;
}) {
  const batchHeading = useRef<HTMLHeadingElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [date, setDate] = useState(defaultObservationDate);
  const [period, setPeriod] = useState('');
  const [salesScope, setSalesScope] = useState('unknown');
  const [sourceType, setSourceType] = useState('sellersprite');
  const [periodType, setPeriodType] = useState('calendar_month');
  const [periodStart, setPeriodStart] = useState('');
  const [periodEnd, setPeriodEnd] = useState('');
  const [selected, setSelected] = useState<string | null>(null);
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const [page, setPage] = useState(1);
  const imports = useScreeningQuery<PageResult<ImportRecord>>('/imports', (data) =>
    Boolean(data?.items.some((item) => ['uploaded', 'parsing', 'importing'].includes(item.state)))
  );
  const preview = useScreeningQuery<PageResult<ImportRow>>(
    selected ? `/imports/${selected}/preview?page=${page}` : null,
    false
  );
  const active = imports.data?.items.find((item) => item.id === selected);
  const { refetch: refreshPreview } = preview;
  useEffect(() => {
    if (active?.id) batchHeading.current?.focus();
  }, [active?.id]);
  useEffect(() => {
    if (selected && active?.state && !['uploaded', 'parsing'].includes(active.state)) {
      void refreshPreview();
    }
  }, [selected, active?.state, refreshPreview]);
  const headers = [
    ...new Set([
      ...Object.values(mapping),
      ...(preview.data?.items.flatMap((row) => Object.keys(row.raw_data)) || [])
    ])
  ].toSorted();
  const editable = Boolean(active && ['preview', 'failed'].includes(active.state));
  const dirty = Boolean(
    active &&
    (JSON.stringify(Object.entries(mapping).toSorted()) !==
      JSON.stringify(Object.entries(active.mapping).toSorted()) ||
      date !== (active.observed_on || '') ||
      period !== active.source_period ||
      salesScope !== active.default_sales_scope ||
      sourceType !== active.source_type ||
      periodType !== active.period_type ||
      (periodType === 'custom' &&
        (periodStart !== active.period_start || periodEnd !== active.period_end)))
  );
  const stage = !active
    ? 1
    : ['completed', 'partial', 'importing'].includes(active.state) ||
        (active.state === 'preview' && Boolean(active.counts.valid) && !dirty)
      ? 3
      : 2;
  const nextAction =
    active?.state === 'preview'
      ? dirty
        ? '修改尚未应用，请展开下方设置，保存并重新解析。'
        : !active.counts.valid
          ? '没有可导入数据，请检查字段映射或下载错误报告。'
          : !active.observed_on ||
              (active.period_type === 'calendar_month' &&
                !/^\d{4}-(0[1-9]|1[0-2])$/.test(active.source_period))
            ? '请在下方设置中补全有效来源月份与观察日期，再保存。'
            : '检查预览无误后，点击确认导入。异常行会跳过，不影响有效数据。'
      : active?.state === 'failed'
        ? '本批次处理失败，请查看后台任务原因；修正设置后可重新解析。'
        : active?.state === 'partial'
          ? '本批次仅部分完成，请查看后台任务失败原因，并重试未完成部分。'
          : active?.state === 'completed'
            ? '本批次已完成，无需再次确认。'
            : active?.state === 'importing'
              ? '正在导入，完成后将自动更新状态，请勿重复操作。'
              : '正在解析文件，请等待预览结果。';
  const importMetadata = (
    <>
      <Field label='统计周期'>
        {periodType === 'custom' ? (
          <Input readOnly value='历史自定义周期' className='bg-muted/50' />
        ) : (
          <select
            className='min-h-11 rounded border bg-transparent p-2'
            value={periodType}
            onChange={(e) => {
              setPeriodType(e.target.value);
              setPeriodStart('');
              setPeriodEnd('');
            }}
          >
            <option value='calendar_month'>固定月份</option>
            <option value='rolling_30d'>近30天</option>
          </select>
        )}
      </Field>
      {periodType === 'custom' && (
        <>
          <Field label='统计开始'>
            <Input type='date' value={periodStart} readOnly />
          </Field>
          <Field label='统计结束'>
            <Input type='date' value={periodEnd} readOnly />
          </Field>
        </>
      )}
      <Field label='来源期（从文件名识别，可修改）'>
        <Input
          value={period}
          placeholder='例如 2026-10'
          onChange={(e) => setPeriod(e.target.value)}
        />
      </Field>
      <Field label='观察日期（默认今天，可修改）'>
        <Input type='date' value={date} onChange={(e) => setDate(e.target.value)} />
      </Field>
    </>
  );
  return (
    <div className='space-y-5'>
      <ol aria-label='导入步骤' className='grid grid-cols-3 gap-2'>
        {['上传文件', '预览检查', '确认导入'].map((label, index) => (
          <li
            key={label}
            aria-current={stage === index + 1 ? 'step' : undefined}
            className={
              stage === index + 1
                ? 'rounded-lg border border-primary bg-primary/5 p-3 text-sm font-semibold'
                : 'text-muted-foreground rounded-lg border p-3 text-sm'
            }
          >
            <span className='mr-2 font-mono'>{index + 1}</span>
            {label}
          </li>
        ))}
      </ol>
      <details open={!active} className='rounded-xl border bg-card p-4 sm:p-5'>
        <summary className='cursor-pointer font-semibold'>
          {active ? '导入新文件' : '上传商品文件'}
          <span className='text-muted-foreground ml-3 text-xs font-normal'>卖家精灵 · 美国站</span>
        </summary>
        <div className='mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
          <div className='sm:col-span-2 lg:col-span-3'>
            <Field label='原始文件（XLSX / CSV / ZIP，最多30MB）'>
              <Input
                aria-label='选择商品文件'
                type='file'
                accept='.xlsx,.csv,.zip'
                onChange={(e) => {
                  const selectedFile = e.target.files?.[0] || null;
                  setFile(selectedFile);
                  setSourceType('sellersprite');
                  setSalesScope('unknown');
                  setPeriodType((current) =>
                    current === 'rolling_30d' ? current : 'calendar_month'
                  );
                  setPeriodStart('');
                  setPeriodEnd('');
                  setSelected(null);
                  setMapping({});
                  setDate(defaultObservationDate());
                  setPeriod(selectedFile ? sourcePeriodFromFilename(selectedFile.name) : '');
                }}
              />
            </Field>
          </div>
          {!active && importMetadata}
          <Button
            disabled={busy || !file || Boolean(active)}
            onClick={() =>
              void act(async () => {
                if (!file) return;
                const data = new FormData();
                data.set('file', file);
                data.set('source_period', period);
                data.set('observed_on', date);
                data.set('default_sales_scope', 'unknown');
                data.set('source_type', 'sellersprite');
                data.set(
                  'period_type',
                  periodType === 'rolling_30d' ? 'rolling_30d' : 'calendar_month'
                );
                data.set('period_start', '');
                data.set('period_end', '');
                const job = await request<JobCreated & { import_id: string }>(
                  '/imports',
                  'POST',
                  data
                );
                setSelected(job.import_id);
                setMapping({});
                setPage(1);
                if (job.job_id) watch(job.job_id);
              }, '文件已接收，正在解析')
            }
          >
            上传并预览
          </Button>
        </div>
        <p className='text-muted-foreground mt-3 text-xs'>
          文件名自动识别来源月份，观察日期默认今天。上传后先检查预览，确认后才写入商品库。
        </p>
      </details>
      {imports.isError && <p role='alert'>{imports.error.message}</p>}
      {active && (
        <section
          aria-label='当前导入批次'
          className='space-y-4 rounded-xl border border-primary/25 bg-card p-4 sm:p-5'
        >
          <div className='flex flex-wrap items-start justify-between gap-3'>
            <div className='min-w-0 flex-1'>
              <p className='text-muted-foreground mb-1 text-xs'>当前批次</p>
              <h2 ref={batchHeading} tabIndex={-1} className='break-words text-lg font-semibold'>
                {active.file_name}
              </h2>
              <p className='text-muted-foreground mt-2 text-sm'>
                {sourceLabels[active.source_type] || active.source_type} ·{' '}
                {active.source_period || '来源期待填写'} · 观察日期 {active.observed_on || '待填写'}
              </p>
            </div>
            <Badge value={active.state} />
          </div>
          <div className='grid grid-cols-2 gap-3'>
            <div className='rounded-lg bg-muted/50 p-4'>
              <p className='text-muted-foreground text-xs'>
                {['completed', 'partial'].includes(active.state) ? '有效数据' : '可导入数据'}
              </p>
              <p className='mt-1 text-3xl font-semibold tabular-nums'>
                {active.counts.valid || 0}
                <span className='text-muted-foreground ml-1 text-sm font-normal'>行</span>
              </p>
            </div>
            <div className='rounded-lg border p-4'>
              <p className='text-muted-foreground text-xs'>异常数据</p>
              <p className='mt-1 text-3xl font-semibold tabular-nums'>
                {active.counts.invalid || 0}
                <span className='text-muted-foreground ml-1 text-sm font-normal'>行</span>
              </p>
              {Boolean(active.counts.invalid) && (
                <p className='text-destructive mt-1 text-xs'>
                  异常行不会导入，可下载报告定位修正。
                </p>
              )}
            </div>
          </div>
          <div className='space-y-3 rounded-lg border bg-muted/30 p-4'>
            <p role='status' className='text-sm font-medium'>
              {nextAction}
            </p>
            <div className='flex flex-wrap items-center gap-2'>
              <Button
                disabled={
                  busy ||
                  dirty ||
                  active.state !== 'preview' ||
                  !active.observed_on ||
                  (active.period_type === 'calendar_month' &&
                    !/^\d{4}-(0[1-9]|1[0-2])$/.test(active.source_period)) ||
                  !active.counts.valid
                }
                onClick={() =>
                  void act(async () => {
                    const job = await request<JobCreated>(`/imports/${active.id}/commit`, 'POST');
                    watch(job.job_id);
                  }, '导入任务已创建')
                }
              >
                确认导入
              </Button>
              {Boolean(active.counts.invalid) && (
                <Button
                  variant='ghost'
                  onClick={() =>
                    void act(() => download(`/imports/${active.id}/errors`, '导入错误.csv'))
                  }
                >
                  下载错误报告
                </Button>
              )}
            </div>
          </div>
          {dirty && editable && (
            <p role='status' className='text-sm text-amber-700 dark:text-amber-400'>
              日期或字段对应关系尚未应用，请先保存并重新解析，再确认导入。
            </p>
          )}
          {active.state === 'parsing' && (
            <p role='status' className='text-sm'>
              正在解析文件，完成后将自动更新预览。
            </p>
          )}
          {active.state === 'importing' && (
            <p role='status' className='text-sm'>
              正在写入商品及经营数据，后台任务中可查看进度。
            </p>
          )}
          {active.state === 'completed' && (
            <p role='status' className='text-sm text-green-700 dark:text-green-400'>
              导入完成，可前往商品库查看；自动初筛进度见后台任务。
            </p>
          )}
          {active.state === 'preview' && !active.counts.valid && (
            <p role='alert' className='text-sm text-destructive'>
              暂无有效行。请检查字段对应关系，修正后重新解析。
            </p>
          )}
          <details
            open={!active.counts.valid && active.state === 'preview'}
            className='rounded border p-3'
          >
            <summary className='cursor-pointer text-sm font-medium'>
              调整日期与字段对应关系（自动识别有误时使用）
            </summary>
            <fieldset disabled={!editable || busy} className='mt-4 space-y-4'>
              <legend className='sr-only'>导入日期与字段对应关系</legend>
              <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>{importMetadata}</div>
              <p className='text-muted-foreground text-xs'>
                ASIN、标题为必填数据；未指定的字段继续自动识别。修改后请保存并重新解析。
              </p>
              <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
                {importFields.map((field) => {
                  const auto = headers.find((header) =>
                    field.aliases.some((alias) => alias.toLowerCase() === header.toLowerCase())
                  );
                  return (
                    <Field
                      key={field.key}
                      label={field.label + (['asin', 'title'].includes(field.key) ? ' *' : '')}
                    >
                      <select
                        aria-label={'映射 ' + field.label}
                        disabled={!editable || busy}
                        className='w-full min-w-0 rounded border bg-transparent p-2 text-sm'
                        value={mapping[field.key] || ''}
                        onChange={(event) => {
                          const value = event.target.value;
                          setMapping((current) => {
                            const next = { ...current };
                            if (value) next[field.key] = value;
                            else delete next[field.key];
                            return next;
                          });
                        }}
                      >
                        <option value=''>
                          {auto ? '自动识别：' + auto : '自动识别（当前页未匹配）'}
                        </option>
                        {headers.map((header) => (
                          <option key={header} value={header}>
                            {header}
                          </option>
                        ))}
                      </select>
                    </Field>
                  );
                })}
              </div>
              <div className='mt-4'>
                {' '}
                <Button
                  variant='outline'
                  disabled={busy || !['preview', 'failed'].includes(active.state)}
                  onClick={() =>
                    void act(async () => {
                      const job = await request<JobCreated>(
                        `/imports/${active.id}/mapping`,
                        'PUT',
                        {
                          mapping,
                          default_sales_scope: salesScope,
                          source_type: sourceType,
                          period_type: periodType,
                          period_start: periodType === 'custom' ? periodStart || null : null,
                          period_end: periodType === 'custom' ? periodEnd || null : null,
                          observed_on: date || null,
                          source_period: period
                        }
                      );
                      watch(job.job_id);
                    }, '已更新字段映射，正在重新解析')
                  }
                >
                  保存日期与映射，重新解析
                </Button>
              </div>
            </fieldset>
          </details>
          {Boolean(active.counts.skipped_sheets?.length) && (
            <details className='rounded-lg border p-3 text-sm'>
              <summary className='cursor-pointer'>
                已跳过 {active.counts.skipped_sheets?.length} 个说明或非商品工作表
              </summary>
              {active.counts.skipped_sheets?.map((sheet, index) => (
                <p key={index} className='text-muted-foreground mt-2 text-xs'>
                  {sheet.member_path} · {sheet.sheet_name}：{sheet.reason}
                </p>
              ))}
            </details>
          )}
          <div className='flex flex-wrap items-baseline justify-between gap-2'>
            <h3 className='font-semibold'>数据预览</h3>
            <p className='text-muted-foreground text-xs'>
              每页最多 50 行 · 请核对 ASIN、标题、价格与销量
            </p>
          </div>
          {preview.isError && <p role='alert'>{preview.error.message}</p>}
          <TableScroll label='导入预览，可横向滚动'>
            <table className='w-full text-left text-sm'>
              <caption className='sr-only'>当前批次数据预览及逐行异常原因</caption>
              <thead className='bg-muted'>
                <tr>
                  {['来源行', 'ASIN / 标题', '价格 USD', '月销量', '检查结果'].map((label) => (
                    <th scope='col' className='p-2' key={label}>
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {preview.data?.items.map((row) => (
                  <tr key={row.id} className='border-t align-top'>
                    <td className='p-2 text-xs'>
                      {row.sheet_name || 'CSV'} · 第 {row.row_no} 行
                      <DataBlock
                        title='查看原始数据'
                        value={{ normalized: row.normalized_data, raw: row.raw_data }}
                      />
                    </td>
                    <td className='max-w-80 p-2'>
                      <p className='font-mono'>{String(row.normalized_data?.asin || '—')}</p>
                      <p className='line-clamp-2 text-xs'>
                        {String(row.normalized_data?.title || '—')}
                      </p>
                    </td>
                    <td className='p-2'>{String(row.normalized_data?.price_usd ?? '—')}</td>
                    <td className='p-2'>{String(row.normalized_data?.monthly_sales ?? '—')}</td>
                    <td className='p-2 text-xs'>
                      {row.errors.length ? (
                        <span className='text-destructive'>{row.errors.join('；')}</span>
                      ) : (
                        <span className='text-green-700 dark:text-green-400'>有效</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableScroll>
          <div className='flex flex-wrap items-center gap-3 text-sm'>
            <Button variant='outline' disabled={page === 1} onClick={() => setPage((v) => v - 1)}>
              上一页
            </Button>
            <span>
              第 {page} 页，共 {preview.data?.total || 0} 行
            </span>
            <Button
              variant='outline'
              disabled={page * 50 >= (preview.data?.total || 0)}
              onClick={() => setPage((v) => v + 1)}
            >
              下一页
            </Button>
          </div>
        </section>
      )}
      <section className='space-y-3 rounded-xl border bg-card p-4 sm:p-5'>
        <h2 className='font-semibold'>历史导入（{imports.data?.total || 0}）</h2>
        <TableScroll label='导入记录，可横向滚动' className='mt-3'>
          <table className='w-full text-left text-sm'>
            <caption className='sr-only'>导入文件或商品预览明细</caption>
            <thead>
              <tr className='bg-muted'>
                <th scope='col' className='p-3'>
                  文件
                </th>
                <th scope='col'>来源期 / 日期</th>
                <th scope='col'>状态</th>
                <th scope='col'>行数</th>
                <th scope='col'>操作</th>
              </tr>
            </thead>
            <tbody>
              {!imports.isLoading && !imports.data?.items.length && (
                <tr>
                  <td colSpan={5} className='p-8 text-center text-muted-foreground'>
                    暂无导入记录，选择卖家精灵文件开始导入。
                  </td>
                </tr>
              )}
              {imports.data?.items.map((item) => (
                <tr
                  key={item.id}
                  className={item.id === selected ? 'border-t bg-muted/40' : 'border-t'}
                >
                  <td className='max-w-80 break-words p-3 font-medium'>{item.file_name}</td>
                  <td>
                    {item.source_period} / {item.observed_on || '待填写'}
                  </td>
                  <td>
                    <Badge value={item.state} />
                  </td>
                  <td>
                    <span className='whitespace-nowrap'>有效 {item.counts.valid || 0}</span>
                    {Boolean(item.counts.invalid) && (
                      <span className='text-destructive block whitespace-nowrap'>
                        异常 {item.counts.invalid}
                      </span>
                    )}
                  </td>
                  <td className='space-x-2'>
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => {
                        setSelected(item.id);
                        setMapping(item.mapping);
                        setDate(item.observed_on || defaultObservationDate());
                        setPeriod(item.source_period || sourcePeriodFromFilename(item.file_name));
                        setSalesScope(item.default_sales_scope);
                        setSourceType(item.source_type);
                        setPeriodType(item.period_type);
                        setPeriodStart(item.period_start || '');
                        setPeriodEnd(item.period_end || '');
                        setPage(1);
                        void act(async () => {
                          const detail = await request<ImportRecord>(`/imports/${item.id}`);
                          if (detail.latest_job_id) watch(detail.latest_job_id);
                        });
                      }}
                    >
                      查看
                    </Button>
                    <Button
                      size='sm'
                      variant='ghost'
                      onClick={() =>
                        void act(() => download(`/imports/${item.id}/source`, item.file_name))
                      }
                    >
                      源文件
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableScroll>
      </section>
    </div>
  );
}
