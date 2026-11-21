'use client';
import { childSales, childSalesValue } from '../sales';
import { useState } from 'react';
import type { MonthlyRecord } from '../types';
import { formatMoney } from './shared';

const sources: Record<string, string> = {
  sellersprite: '卖家精灵',
  manual: '人工表格',
  other: '其他来源',
  legacy_import: '历史导入'
};
const periods: Record<string, string> = {
  calendar_month: '固定月份',
  rolling_30d: '近30天',
  custom: '自定义周期'
};
const scopes: Record<string, string> = {
  child: '',
  parent: '父体汇总',
  unknown: ''
};
const groupKey = (row: MonthlyRecord) =>
  [row.source_type, row.period_type, row.sales_scope].join('|');
function Trend({
  months,
  values,
  label,
  money = false
}: {
  months: string[];
  values: (number | null)[];
  label: string;
  money?: boolean;
}) {
  const known = values.filter((value): value is number => value !== null);
  const max = Math.max(...known, 1);
  const x = (index: number) => 28 + (index * 264) / Math.max(months.length - 1, 1);
  const y = (value: number) => 90 - (value / max) * 62;
  return (
    <div className='rounded-lg border p-3'>
      <div className='mb-2 flex justify-between text-xs'>
        <span className='font-medium'>{label}</span>
        <span className='text-muted-foreground'>
          最高{' '}
          {known.length
            ? money
              ? formatMoney(String(Math.max(...known)))
              : Math.max(...known).toLocaleString()
            : '—'}
        </span>
      </div>
      <svg
        role='img'
        aria-label={label + '趋势，缺失月份断开'}
        viewBox='0 0 320 116'
        className='h-28 w-full text-primary'
      >
        <line x1='28' x2='292' y1='90' y2='90' stroke='currentColor' opacity='.15' />
        {values.map((value, index) =>
          value === null ? null : (
            <g key={months[index]}>
              {index > 0 && values[index - 1] !== null && (
                <line
                  x1={x(index - 1)}
                  y1={y(values[index - 1]!)}
                  x2={x(index)}
                  y2={y(value)}
                  stroke='currentColor'
                  strokeWidth='2'
                />
              )}
              <circle cx={x(index)} cy={y(value)} r='3' fill='currentColor'>
                <title>
                  {months[index]}：
                  {money ? '$' + formatMoney(String(value)) : value.toLocaleString()}
                </title>
              </circle>
            </g>
          )
        )}
        {[0, Math.floor((months.length - 1) / 2), months.length - 1]
          .filter((value, index, all) => value >= 0 && all.indexOf(value) === index)
          .map((index) => (
            <text
              key={index}
              x={x(index)}
              y='110'
              textAnchor='middle'
              fontSize='10'
              fill='currentColor'
            >
              {months[index]?.slice(2)}
            </text>
          ))}
      </svg>
      {!known.length && <p className='text-muted-foreground text-xs'>暂无可用数据</p>}
    </div>
  );
}
export default function OperatingTrends({ rows, total }: { rows: MonthlyRecord[]; total: number }) {
  const [selected, setSelected] = useState('');
  const groups = [...new Set(rows.map(groupKey))];
  const key = groups.includes(selected) ? selected : groups[0];
  const series = rows
    .filter((row) => groupKey(row) === key)
    .toSorted(
      (a, b) =>
        b.stat_month.localeCompare(a.stat_month) ||
        b.observed_on.localeCompare(a.observed_on) ||
        b.id.localeCompare(a.id)
    );
  const latest = series[0];
  if (!latest) return null;
  const months = Array.from({ length: 12 }, (_, index) => {
    const [year, month] = latest.stat_month.slice(0, 7).split('-').map(Number);
    return new Date(Date.UTC(year, month - 12 + index, 1)).toISOString().slice(0, 7);
  });
  const byMonth = new Map<string, MonthlyRecord>();
  for (const row of series)
    if (!byMonth.has(row.stat_month.slice(0, 7))) byMonth.set(row.stat_month.slice(0, 7), row);
  const prices = months.map((month) => {
    const value = byMonth.get(month)?.price_usd;
    return value == null ? null : Number(value);
  });
  const sales = months.map((month) =>
    byMonth.has(month) ? childSalesValue(byMonth.get(month)!) : null
  );
  return (
    <div className='space-y-3' data-operating-trends>
      {groups.length > 1 ? (
        <select
          aria-label='选择趋势数据'
          className='w-full rounded border bg-transparent p-2 text-xs'
          value={key}
          onChange={(event) => setSelected(event.target.value)}
        >
          {groups.map((group) => {
            const [source, period, scope] = group.split('|');
            return (
              <option key={group} value={group}>
                {[sources[source] || source, periods[period] || period, scopes[scope]]
                  .filter(Boolean)
                  .join(' · ')}
              </option>
            );
          })}
        </select>
      ) : (
        <p className='text-muted-foreground text-xs'>
          {sources[latest.source_type] || latest.source_type} ·{' '}
          {periods[latest.period_type] || latest.period_type}
        </p>
      )}
      <div className='grid grid-cols-3 gap-2'>
        {[
          ['最新数据', latest.stat_month.slice(0, 7)],
          ['最新价格 USD', formatMoney(latest.price_usd)],
          ['最新子体销量', childSales(latest)]
        ].map(([label, value]) => (
          <div key={label} className='bg-muted/50 rounded-lg p-3'>
            <p className='text-muted-foreground text-xs'>{label}</p>
            <p className='mt-1 font-semibold tabular-nums'>{value}</p>
          </div>
        ))}
      </div>
      {latest.period_type !== 'custom' && (
        <div className='grid gap-2 sm:grid-cols-2'>
          <Trend months={months} values={prices} label='价格 USD' money />
          <Trend months={months} values={sales} label='子体销量参考值（件）' />
        </div>
      )}
      <p className='text-muted-foreground text-xs'>
        {latest.period_type === 'custom'
          ? '自定义周期长短可能不同，请以明细起止日期逐条比较。'
          : '同月多次观察采用最近记录；缺失月份断线。曲线为子体参考值，购买量级不是精确销量，不能与父体估算直接相加核对。'}
        {total > rows.length && ' 趋势仅基于当前页数据，可翻页查看更多记录。'}
      </p>
    </div>
  );
}
