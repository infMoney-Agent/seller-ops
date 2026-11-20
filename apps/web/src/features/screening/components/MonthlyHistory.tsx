'use client';
import { useState } from 'react';
import { useScreeningQuery } from '../queries';
import type { MonthlyRecord, PageResult, Product } from '../types';
import { Button, TableScroll, formatMoney } from './shared';
import { childSales, salesNote } from '../sales';
import OperatingTrends from './OperatingTrends';

export const sourceLabels: Record<string, string> = {
  sellersprite: '卖家精灵',
  manual: '人工表格',
  other: '其他来源',
  legacy_import: '历史导入'
};

export const periodLabels: Record<string, string> = {
  calendar_month: '固定月份',
  rolling_30d: '近30天',
  custom: '自定义周期'
};

export default function MonthlyHistory({
  product,
  onCalculate,
  recentYear = false
}: {
  product: Product;
  recentYear?: boolean;
  onCalculate: (product: Product) => void;
}) {
  const [page, setPage] = useState(1);
  const history = useScreeningQuery<PageResult<MonthlyRecord>>(
    `/products/${product.id}/monthly?page=${page}${recentYear ? '&recent_months=12' : ''}`
  );
  return (
    <section className='space-y-3 rounded border p-3'>
      <h4 className='font-semibold'>{recentYear ? '近一年经营数据' : '月度经营数据'}</h4>
      <p className='text-muted-foreground text-xs'>
        {recentYear
          ? '截至该商品最新数据月份，展示近 12 个月已导入的数据；缺失月份不补零，不同来源和统计周期分别展示。'
          : '同一商品按来源和统计周期保存多期数据。修改经营数据不会覆盖人工结论。'}
      </p>
      {history.isLoading && <p role='status'>正在加载经营数据…</p>}
      {history.isError && <p role='alert'>{history.error.message}</p>}
      {history.data && history.data.items.length > 0 && (
        <OperatingTrends rows={history.data.items} total={history.data.total} />
      )}
      <TableScroll label='月度经营数据，可横向滚动'>
        <table className='w-full text-left text-sm'>
          <caption className='sr-only'>商品各月价格、销量及来源明细</caption>
          <thead>
            <tr>
              {[
                '月份 / 周期',
                '价格 USD',
                '子体销量',
                '销售额 USD',
                '评分 / 数量',
                'BSR',
                '来源 / 观察日期',
                '操作'
              ].map((x) => (
                <th scope='col' key={x} className='whitespace-nowrap p-2'>
                  {x}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {history.data?.items.map((row) => (
              <tr key={row.id} className='border-t'>
                <td className='whitespace-nowrap p-2'>
                  {row.stat_month.slice(0, 7)} · {periodLabels[row.period_type]}
                  <p className='text-muted-foreground text-xs'>
                    {row.period_start} ~ {row.period_end}
                  </p>
                </td>
                <td>{formatMoney(row.price_usd)}</td>
                <td>
                  {childSales(row)}
                  <p className='text-muted-foreground text-xs'>{salesNote(row)}</p>
                </td>
                <td>{formatMoney(row.monthly_revenue)}</td>
                <td>
                  {row.rating ?? '—'} / {row.review_count ?? '—'}
                </td>
                <td>{row.bsr_rank ?? '—'}</td>
                <td className='text-xs'>
                  {sourceLabels[row.source_type] || row.source_type}
                  <p>{row.observed_on}</p>
                  <details>
                    <summary className='cursor-pointer'>查看来源文件</summary>
                    <p className='max-w-48 break-words'>{row.file_name}</p>
                  </details>
                </td>
                <td>
                  <Button
                    size='sm'
                    variant='outline'
                    onClick={() =>
                      onCalculate({
                        ...product,
                        monthly_id: row.id,
                        monthly_revision: row.revision,
                        stat_month: row.stat_month,
                        period_type: row.period_type,
                        period_start: row.period_start,
                        period_end: row.period_end,
                        source_type: row.source_type,
                        price_usd: row.price_usd,
                        monthly_sales: row.monthly_sales,
                        monthly_revenue: row.monthly_revenue,
                        review_count: row.review_count,
                        rating: row.rating,
                        bsr_rank: row.bsr_rank
                      })
                    }
                  >
                    用此期测算
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </TableScroll>
      {!history.data?.total && !history.isLoading && !history.isError && (
        <p className='text-sm'>暂无月度数据</p>
      )}
      <div className='flex items-center gap-2'>
        <Button
          size='sm'
          variant='outline'
          disabled={page === 1}
          onClick={() => setPage((v) => v - 1)}
        >
          上一页数据
        </Button>
        <span className='text-xs'>
          第 {page} 页，共 {history.data?.total || 0} 条
        </span>
        <Button
          size='sm'
          variant='outline'
          disabled={page * 50 >= (history.data?.total || 0)}
          onClick={() => setPage((v) => v + 1)}
        >
          下一页数据
        </Button>
      </div>
    </section>
  );
}
