'use client';
import { useEffect, useRef, useState } from 'react';
import { useScreeningQuery } from '../queries';
import { download, request } from '../service';
import { sourceLabels } from './MonthlyHistory';
import { defaultObservationDate, sourcePeriodFromFilename } from '../import-dates';
import type { ImportRecord, ImportRow, JobCreated, PageResult } from '../types';
import { Badge, Button, DataBlock, Field, Input, TableScroll, type Action } from './shared';

export default function ImportPanel({
  act,
  watch,
  busy,
  onRemoved
}: {
  act: Action;
  watch: (id: string) => void;
  busy: boolean;
  onRemoved?: (id: string) => void;
}) {
  const inFlight = useRef(false);
  const uploadController = useRef<AbortController | null>(null);
  useEffect(() => () => uploadController.current?.abort(), []);
  const run: Action = async (work, message) => {
    if (inFlight.current) return;
    inFlight.current = true;
    try {
      return await act(work, message);
    } finally {
      inFlight.current = false;
    }
  };
  const batchHeading = useRef<HTMLHeadingElement>(null);
  const [files, setFiles] = useState<File[]>([]);
  const [uploads, setUploads] = useState<{ name: string; id?: string; error?: string }[]>([]);
  const [removal, setRemoval] = useState<{
    products: number;
    exclusive_products: number;
    source_rows: number;
  } | null>(null);
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);
  const [date, setDate] = useState(defaultObservationDate);
  const [period, setPeriod] = useState('');
  const [periodType, setPeriodType] = useState('calendar_month');
  const [selected, setSelected] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [historyPage, setHistoryPage] = useState(1);
  const imports = useScreeningQuery<PageResult<ImportRecord>>(
    '/imports?page=' + historyPage,
    (data) =>
      Boolean(data?.items.some((item) => ['uploaded', 'parsing', 'importing'].includes(item.state)))
  );
  const preview = useScreeningQuery<PageResult<ImportRow>>(
    selected ? `/imports/${selected}/preview?page=${page}` : null,
    false
  );
  const detail = useScreeningQuery<ImportRecord>(selected ? '/imports/' + selected : null, (data) =>
    Boolean(data && ['uploaded', 'parsing', 'importing'].includes(data.state))
  );
  const active = detail.data || imports.data?.items.find((item) => item.id === selected);
  const { refetch: refreshPreview } = preview;
  useEffect(() => {
    if (active?.id) batchHeading.current?.focus();
  }, [active?.id]);
  useEffect(() => {
    if (selected && active?.state && !['uploaded', 'parsing'].includes(active.state)) {
      void refreshPreview();
    }
  }, [selected, active?.state, refreshPreview]);
  const stage = !active
    ? 1
    : ['completed', 'partial', 'importing'].includes(active.state) ||
        (active.state === 'preview' && Boolean(active.counts.valid))
      ? 3
      : 2;
  const nextAction =
    active?.state === 'preview'
      ? !active.counts.valid
        ? '没有可导入数据，请下载错误报告查看原因。标准导出文件无法识别时，应修复后端解析规则。'
        : !active.observed_on ||
            (active.period_type === 'calendar_month' &&
              !/^\d{4}-(0[1-9]|1[0-2])$/.test(active.source_period))
          ? '来源月份或观察日期不完整，请重新上传并填写。'
          : '检查预览无误后，点击确认导入。异常行会跳过，不影响有效数据。'
      : active?.state === 'failed'
        ? '本批次处理失败，请查看错误提示或下载错误报告。'
        : active?.state === 'partial'
          ? '本批次仅部分完成，请查看失败原因及导入记录。'
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
            }}
          >
            <option value='calendar_month'>固定月份</option>
            <option value='rolling_30d'>近30天</option>
          </select>
        )}
      </Field>
      <Field
        label={
          files.length > 1 ? '来源期（无法从文件名识别时使用）' : '来源期（从文件名识别，可修改）'
        }
      >
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
            <Field label='原始文件（可多选，每个最多30MB，XLSX / CSV / ZIP）'>
              <Input
                aria-label='选择商品文件'
                type='file'
                multiple
                disabled={busy}
                accept='.xlsx,.csv,.zip'
                onChange={(e) => {
                  const selectedFiles = Array.from(e.target.files || []);
                  setFiles(selectedFiles);
                  setUploads([]);
                  const selectedFile = selectedFiles[0];
                  setPeriodType((current) =>
                    current === 'rolling_30d' ? current : 'calendar_month'
                  );
                  setSelected(null);
                  setDate(defaultObservationDate());
                  setPeriod(
                    selectedFiles.length === 1 && selectedFile
                      ? sourcePeriodFromFilename(selectedFile.name)
                      : ''
                  );
                  setHistoryPage(1);
                }}
              />
            </Field>
          </div>
          {!active && importMetadata}
          <Button
            disabled={busy || !files.length || files.length > 100 || Boolean(active)}
            onClick={() =>
              void run(async () => {
                const controller = new AbortController();
                uploadController.current = controller;
                const results: { name: string; id?: string; error?: string }[] = [];
                for (const file of files) {
                  if (controller.signal.aborted) break;
                  try {
                    if (file.size > 30 * 1024 * 1024) throw Error('文件超过30MB');
                    const data = new FormData();
                    data.set('file', file);
                    data.set(
                      'source_period',
                      files.length === 1 ? period : sourcePeriodFromFilename(file.name) || period
                    );
                    data.set('observed_on', date);
                    data.set('source_type', 'sellersprite');
                    data.set('default_sales_scope', 'unknown');
                    data.set(
                      'period_type',
                      periodType === 'rolling_30d' ? 'rolling_30d' : 'calendar_month'
                    );
                    const result = await request<JobCreated & { import_id: string }>(
                      '/imports',
                      'POST',
                      data,
                      controller.signal
                    );
                    results.push({ name: file.name, id: result.import_id });
                    if (result.job_id) watch(result.job_id);
                  } catch (error) {
                    results.push({
                      name: file.name,
                      error: error instanceof Error ? error.message : '上传失败'
                    });
                  }
                  setUploads([...results]);
                }
                setSelected(results.find((item) => item.id)?.id || null);
                setPage(1);
              })
            }
          >
            上传并预览
          </Button>
        </div>
        <p className='text-muted-foreground mt-3 text-xs'>
          文件名自动识别来源月份，观察日期默认今天。上传后先检查预览，确认后才写入商品库。
        </p>
      </details>
      {files.length > 100 && (
        <p role='alert' className='text-destructive text-sm'>
          一次最多选择100个文件。
        </p>
      )}
      {files.length > 1 && (
        <p className='text-muted-foreground text-xs'>
          已选择 {files.length} 个文件，每个文件最多30MB，分别从文件名识别月份。
        </p>
      )}
      {uploads.length > 0 && (
        <section className='space-y-2 rounded border p-3'>
          <h3 className='font-semibold'>本次文件（{uploads.length}）</h3>
          {uploads.map((item, index) => (
            <div key={index} className='flex flex-wrap items-center justify-between gap-2 text-sm'>
              <span className='min-w-0 flex-1 break-words'>{item.name}</span>
              {item.error && (
                <span role='alert' className='text-destructive'>
                  {item.error}
                </span>
              )}
              {item.id && (
                <Button
                  size='sm'
                  variant='outline'
                  disabled={busy}
                  onClick={() => {
                    setSelected(item.id || null);
                    setPage(1);
                  }}
                >
                  查看预览
                </Button>
              )}
            </div>
          ))}
          {uploads.filter((item) => item.id).length > 1 && (
            <Button
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  const updated = [...uploads];
                  for (let index = 0; index < updated.length; index++) {
                    const item = updated[index];
                    if (!item.id) continue;
                    try {
                      const batch = await request<ImportRecord>('/imports/' + item.id);
                      if (
                        batch.state === 'completed' ||
                        batch.state === 'importing' ||
                        batch.state === 'partial'
                      )
                        continue;
                      if (batch.state !== 'preview') throw Error('尚未解析完成，请稍后再确认');
                      if (!batch.counts.valid) throw Error('没有有效数据');
                      const result = await request<JobCreated>(
                        '/imports/' + item.id + '/commit',
                        'POST'
                      );
                      if (result.job_id) watch(result.job_id);
                      updated[index] = { ...item, error: undefined };
                    } catch (error) {
                      updated[index] = {
                        ...item,
                        error: error instanceof Error ? error.message : '确认失败'
                      };
                    }
                  }
                  setUploads(updated);
                }, '已提交可导入的文件')
              }
            >
              确认本次全部文件
            </Button>
          )}
        </section>
      )}
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
                  active.state !== 'preview' ||
                  !active.observed_on ||
                  (active.period_type === 'calendar_month' &&
                    !/^\d{4}-(0[1-9]|1[0-2])$/.test(active.source_period)) ||
                  !active.counts.valid
                }
                onClick={() =>
                  void run(async () => {
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
                    void run(() => download(`/imports/${active.id}/errors`, '导入错误.csv'))
                  }
                >
                  下载错误报告
                </Button>
              )}
            </div>
          </div>
          {active.state === 'parsing' && (
            <p role='status' className='text-sm'>
              正在解析文件，完成后将自动更新预览。
            </p>
          )}
          {active.state === 'importing' && (
            <p role='status' className='text-sm'>
              正在写入商品及经营数据，可关闭卡片，稍后在历史导入记录查看结果。
            </p>
          )}
          {active.state === 'completed' && (
            <p role='status' className='text-sm text-green-700 dark:text-green-400'>
              导入完成，可前往商品库查看；后续处理会自动完成。
            </p>
          )}
          {active.state === 'preview' && !active.counts.valid && (
            <p role='alert' className='text-sm text-destructive'>
              暂无有效行，请下载错误报告查看原因。
            </p>
          )}
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
      <details className='bg-card rounded-lg border p-3'>
        <summary className='cursor-pointer font-semibold'>
          历史导入（{imports.data?.total || 0}）
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
                  <td className='p-3 align-top'>
                    <div className='flex w-64 items-center gap-2 whitespace-nowrap'>
                      <Button
                        size='sm'
                        className='h-8 px-3'
                        variant='outline'
                        onClick={() => {
                          setSelected(item.id);
                          setDate(item.observed_on || defaultObservationDate());
                          setPeriod(item.source_period || sourcePeriodFromFilename(item.file_name));
                          setPeriodType(item.period_type);
                          setPage(1);
                          void run(async () => {
                            const detail = await request<ImportRecord>(`/imports/${item.id}`);
                            if (detail.latest_job_id) watch(detail.latest_job_id);
                          });
                        }}
                      >
                        查看
                      </Button>
                      <Button
                        size='sm'
                        className='h-8 px-3'
                        variant='outline'
                        onClick={() =>
                          void run(() => download(`/imports/${item.id}/source`, item.file_name))
                        }
                      >
                        源文件
                      </Button>
                      <Button
                        size='sm'
                        aria-expanded={pendingDelete === item.id}
                        variant='destructive'
                        className='h-8 border-destructive/20 px-3'
                        disabled={
                          pendingDelete === item.id ||
                          busy ||
                          ['uploaded', 'parsing', 'importing'].includes(item.state)
                        }
                        onClick={() => {
                          setPendingDelete(item.id);
                          setRemoval(null);
                          void run(async () => {
                            const impact = await request<{
                              products: number;
                              exclusive_products: number;
                              source_rows: number;
                            }>('/imports/' + item.id + '/removal-preview');
                            setRemoval(impact);
                          });
                        }}
                      >
                        删除导入
                      </Button>
                    </div>
                    {pendingDelete === item.id && (
                      <div className='mt-3 w-64 space-y-3 rounded-lg border border-destructive/20 bg-destructive/5 p-3'>
                        <p className='text-xs leading-5'>
                          将撤销本批经营数据；
                          {removal
                            ? '涉及 ' +
                              removal.products +
                              ' 个商品，其中 ' +
                              removal.exclusive_products +
                              ' 个无其他来源，将移出商品库。'
                            : '正在核对影响范围…'}
                        </p>
                        <div className='flex items-center gap-2'>
                          <Button
                            size='sm'
                            variant='destructive'
                            className='h-8 px-3'
                            disabled={busy || !removal}
                            onClick={() =>
                              void run(async () => {
                                await request('/imports/' + item.id, 'DELETE');
                                onRemoved?.(item.id);
                                setPendingDelete(null);
                                setUploads((old) => old.filter((upload) => upload.id !== item.id));
                                if (selected === item.id) setSelected(null);
                              }, '已撤销本批导入数据')
                            }
                          >
                            确认删除数据
                          </Button>
                          <Button
                            size='sm'
                            variant='outline'
                            className='h-8 px-3'
                            onClick={() => setPendingDelete(null)}
                          >
                            取消
                          </Button>
                        </div>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableScroll>
        {(imports.data?.total || 0) > 50 && (
          <div className='mt-3 flex items-center gap-3 text-sm'>
            <Button
              size='sm'
              variant='outline'
              disabled={historyPage === 1}
              onClick={() => setHistoryPage((v) => v - 1)}
            >
              上一页记录
            </Button>
            <span>第 {historyPage} 页</span>
            <Button
              size='sm'
              variant='outline'
              disabled={historyPage * 50 >= (imports.data?.total || 0)}
              onClick={() => setHistoryPage((v) => v + 1)}
            >
              下一页记录
            </Button>
          </div>
        )}
      </details>
    </div>
  );
}
