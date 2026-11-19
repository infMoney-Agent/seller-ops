'use client';
import { useEffect, useState } from 'react';
import { useScreeningQuery } from '../queries';
import { download, request } from '../service';
import { importFields } from '../import-fields';
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
      <div className='bg-card grid gap-3 rounded-lg border p-4 md:grid-cols-4'>
        <Field label='原始文件（XLSX / CSV / ZIP，最多30MB）'>
          <Input
            aria-label='选择商品文件'
            type='file'
            accept='.xlsx,.csv,.zip'
            onChange={(e) => {
              const selectedFile = e.target.files?.[0] || null;
              setFile(selectedFile);
              setSelected(null);
              setMapping({});
              setDate(defaultObservationDate());
              setPeriod(selectedFile ? sourcePeriodFromFilename(selectedFile.name) : '');
            }}
          />
        </Field>
        <Field label='数据来源'>
          <select
            className='rounded border bg-transparent p-2'
            value={sourceType}
            onChange={(e) => setSourceType(e.target.value)}
          >
            <option value='sellersprite'>卖家精灵</option>
            <option value='manual'>人工表格</option>
            <option value='other'>其他来源</option>
          </select>
        </Field>
        <Field label='统计周期'>
          <select
            className='rounded border bg-transparent p-2'
            value={periodType}
            onChange={(e) => {
              setPeriodType(e.target.value);
              setPeriodStart('');
              setPeriodEnd('');
            }}
          >
            <option value='calendar_month'>自然月</option>
            <option value='rolling_30d'>滚动30天</option>
            <option value='custom'>自定义周期</option>
          </select>
        </Field>
        {periodType === 'custom' && (
          <>
            <Field label='统计开始'>
              <Input
                type='date'
                value={periodStart}
                onChange={(e) => setPeriodStart(e.target.value)}
              />
            </Field>
            <Field label='统计结束'>
              <Input type='date' value={periodEnd} onChange={(e) => setPeriodEnd(e.target.value)} />
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
        <Button
          disabled={busy || !file}
          onClick={() =>
            void act(async () => {
              if (!file) return;
              const data = new FormData();
              data.set('file', file);
              data.set('source_period', period);
              data.set('observed_on', date);
              data.set('default_sales_scope', salesScope);
              data.set('source_type', sourceType);
              data.set('period_type', periodType);
              data.set('period_start', periodType === 'custom' ? periodStart : '');
              data.set('period_end', periodType === 'custom' ? periodEnd : '');
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
      <details className='rounded-lg border p-3'>
        <summary className='cursor-pointer text-sm font-medium'>高级导入设置</summary>
        <div className='mt-3 max-w-md'>
          {' '}
          <Field label='文件未注明时，销量属于'>
            <select
              className='rounded border bg-transparent p-2'
              value={salesScope}
              onChange={(e) => setSalesScope(e.target.value)}
            >
              <option value='unknown'>自动识别，无法确定时待确认</option>
              <option value='child'>当前子 ASIN</option>
              <option value='parent'>整个父体（需提供父 ASIN）</option>
            </select>
          </Field>
        </div>
        <p className='text-muted-foreground mt-2 text-xs'>
          优先使用文件中明确的子体或父体销量字段，仅在文件没有注明时使用此设置。
        </p>
      </details>
      <p className='text-muted-foreground text-sm'>
        站点：美国。重复 ASIN
        保留各月经营数据、来源历史和人工结论。自然月与滚动30天独立保存；更改月份或观察日期时，请先保存日期与映射。
      </p>
      {imports.isError && <p role='alert'>{imports.error.message}</p>}
      <details open={!active} className='rounded-lg border p-3'>
        <summary className='cursor-pointer text-sm font-medium'>
          导入记录（{imports.data?.total || 0}）
        </summary>
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
              {imports.data?.items.map((item) => (
                <tr key={item.id} className='border-t'>
                  <td className='p-3'>{item.file_name}</td>
                  <td>
                    {item.source_period} / {item.observed_on || '待填写'}
                  </td>
                  <td>
                    <Badge value={item.state} />
                  </td>
                  <td>
                    有效 {item.counts.valid || 0} / 无效 {item.counts.invalid || 0}
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
      </details>
      {active && (
        <section className='space-y-3 rounded border p-4'>
          <h2 className='font-semibold'>导入预览 · {active.file_name}</h2>
          {active.counts.skipped_sheets?.map((sheet, index) => (
            <p key={index} className='text-muted-foreground text-sm'>
              已跳过 {sheet.member_path} · {sheet.sheet_name}：{sheet.reason}
            </p>
          ))}
          <div className='grid grid-cols-3 gap-3 text-sm'>
            <div className='bg-muted/50 rounded p-3'>
              解析状态 <Badge value={active.state} />
            </div>
            <div className='rounded border p-3'>
              可导入 <strong>{active.counts.valid || 0}</strong> 行
            </div>
            <div className='rounded border p-3'>
              需修正 <strong>{active.counts.invalid || 0}</strong> 行
            </div>
          </div>
          <p className='text-muted-foreground text-xs'>
            先检查
            ASIN、标题及经营数据的对应列。未指定的字段继续自动识别；多工作表可在下方逐页核对。
          </p>
          <details
            open={!active.counts.valid && active.state === 'preview'}
            className='rounded border p-3'
          >
            <summary className='cursor-pointer text-sm font-medium'>
              调整字段对应关系（ASIN、标题为必填数据）
            </summary>
            <div className='mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
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
          </details>
          {dirty && editable && (
            <p role='status' className='text-sm text-amber-700 dark:text-amber-400'>
              日期或字段对应关系尚未应用，请先保存并重新解析，再确认导入。
            </p>
          )}
          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              disabled={busy || !['preview', 'failed'].includes(active.state)}
              onClick={() =>
                void act(async () => {
                  const job = await request<JobCreated>(`/imports/${active.id}/mapping`, 'PUT', {
                    mapping,
                    default_sales_scope: salesScope,
                    source_type: sourceType,
                    period_type: periodType,
                    period_start: periodType === 'custom' ? periodStart || null : null,
                    period_end: periodType === 'custom' ? periodEnd || null : null,
                    observed_on: date || null,
                    source_period: period
                  });
                  watch(job.job_id);
                }, '已更新字段映射，正在重新解析')
              }
            >
              保存日期与映射，重新解析
            </Button>
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
            <Button
              variant='ghost'
              onClick={() =>
                void act(() => download(`/imports/${active.id}/errors`, '导入错误.csv'))
              }
            >
              下载错误报告
            </Button>
          </div>
          {(!active.observed_on ||
            (active.period_type === 'calendar_month' && !active.source_period)) && (
            <p className='text-muted-foreground text-sm'>
              请先保存观察日期和来源期，再确认导入。无法从文件名识别月份时，请手动填写来源期。
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
          {preview.isError && <p role='alert'>{preview.error.message}</p>}
          <TableScroll label='导入预览，可横向滚动'>
            <table className='w-full text-left text-sm'>
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
          <div className='flex items-center gap-3'>
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
    </div>
  );
}
