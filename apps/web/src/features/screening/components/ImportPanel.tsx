'use client';
import { useState } from 'react';
import { useScreeningQuery } from '../queries';
import { download, request } from '../service';
import type { ImportRecord, ImportRow, JobCreated, PageResult } from '../types';
import { Badge, Button, DataBlock, Field, Input, type Action } from './shared';

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
  const [date, setDate] = useState('');
  const [period, setPeriod] = useState('');
  const [salesScope,setSalesScope]=useState('unknown');
  const [sourceType, setSourceType] = useState('sellersprite');
  const [periodType, setPeriodType] = useState('calendar_month');
  const [periodStart, setPeriodStart] = useState('');
  const [periodEnd, setPeriodEnd] = useState('');
  const [selected, setSelected] = useState<string | null>(null);
  const [mapping, setMapping] = useState('{}');
  const [page, setPage] = useState(1);
  const imports = useScreeningQuery<PageResult<ImportRecord>>('/imports', true);
  const preview = useScreeningQuery<PageResult<ImportRow>>(
    selected ? `/imports/${selected}/preview?page=${page}` : null,
    true
  );
  const active = imports.data?.items.find((item) => item.id === selected);
  return (
    <div className='space-y-5'>
      <div className='bg-card grid gap-3 rounded-lg border p-4 md:grid-cols-4'>
        <Field label='原始文件（XLSX / CSV / ZIP，最多30MB）'>
          <Input
            aria-label='选择商品文件'
            type='file'
            accept='.xlsx,.csv,.zip'
            onChange={(e) => setFile(e.target.files?.[0] || null)}
          />
        </Field>
        <Field label='缺省销量口径'><select className='rounded border bg-transparent p-2' value={salesScope} onChange={e=>setSalesScope(e.target.value)}><option value='unknown'>未知（不当作子体销量）</option><option value='child'>已确认子体销量</option><option value='parent'>已确认父体汇总（必须有父ASIN）</option></select></Field>
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
        <Field label='统计口径'>
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
        <Field label='来源期'>
          <Input
            value={period}
            placeholder='例如 2026-10'
            onChange={(e) => setPeriod(e.target.value)}
          />
        </Field>
        <Field label='观察日期（确认导入前必填）'>
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
              data.set('default_sales_scope',salesScope);
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
              setPage(1);
              if (job.job_id) watch(job.job_id);
            }, '文件已接收，正在解析')
          }
        >
          上传并预览
        </Button>
      </div>
      <p className='text-muted-foreground text-sm'>
        站点：美国。重复 ASIN
        保留各月经营数据、来源历史和人工结论。自然月与滚动30天独立保存；更改月份或观察日期时，请先保存日期与映射。
      </p>
      {imports.isError && <p role='alert'>{imports.error.message}</p>}
      <div className='overflow-auto rounded border'>
        <table className='w-full text-left text-sm'>
          <thead>
            <tr className='bg-muted'>
              <th className='p-3'>文件</th>
              <th>来源期 / 日期</th>
              <th>状态</th>
              <th>行数</th>
              <th>操作</th>
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
                      setMapping(JSON.stringify(item.mapping, null, 2));
                      setDate(item.observed_on || '');
                      setPeriod(item.source_period);
                      setSalesScope(item.default_sales_scope);
                      setSourceType(item.source_type);
                      setPeriodType(item.period_type);
                      setPeriodStart(item.period_start || '');
                      setPeriodEnd(item.period_end || '');
                      setPage(1);
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
      </div>
      {active && (
        <section className='space-y-3 rounded border p-4'>
          <h2 className='font-semibold'>导入预览 · {active.file_name}</h2>
          <Field label='字段映射 JSON（标准字段 → 原始表头，例如 {"title":"商品标题"}）'>
            <textarea
              aria-label='字段映射'
              className='min-h-24 rounded border p-2 font-mono text-sm'
              value={mapping}
              onChange={(e) => setMapping(e.target.value)}
            />
          </Field>
          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              disabled={busy || !['preview', 'failed'].includes(active.state)}
              onClick={() =>
                void act(async () => {
                  const job = await request<JobCreated>(`/imports/${active.id}/mapping`, 'PUT', {
                    mapping: JSON.parse(mapping),
                  default_sales_scope:salesScope,
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
                busy || !['preview', 'partial'].includes(active.state) || !active.observed_on
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
          {preview.isError && <p role='alert'>{preview.error.message}</p>}
          {preview.data?.items.map((row) => (
            <DataBlock
              key={row.id}
              title={`${row.member_path} · ${row.sheet_name} 第${row.row_no}行 ${row.errors.join('；') || '有效'}`}
              value={{ normalized: row.normalized_data, raw: row.raw_data }}
            />
          ))}
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
