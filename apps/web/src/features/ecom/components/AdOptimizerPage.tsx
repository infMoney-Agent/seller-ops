'use client';

import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Icons } from '@/components/icons';
import { adsApi } from '../api';
import { cn } from '@/lib/utils';
import type { AdReport } from '../types';

const {
  ecomMegaphone: Megaphone,
  ecomLoader2: Loader2,
  ecomTrendingUp: TrendingUp,
  ecomTrendingDown: TrendingDown,
  ecomArrowUp: ArrowUp,
  ecomArrowDown: ArrowDown,
  ecomPause: Pause,
  ecomPlusCircle: PlusCircle,
  ecomXCircle: XCircle
} = Icons;

const ACTION_STYLES: Record<string, string> = {
  raise: 'text-emerald-400 bg-emerald-900/30',
  lower: 'text-amber-400 bg-amber-900/30',
  pause: 'text-red-400 bg-red-900/30',
  add: 'text-blue-400 bg-blue-900/30',
  negate: 'text-slate-400 bg-slate-800'
};

const ACTION_LABELS: Record<string, string> = {
  raise: '提高出价',
  lower: '降低出价',
  pause: '暂停投放',
  add: '添加关键词',
  negate: '添加为否定关键词',
  keep: '维持出价'
};

const ACTION_ICONS: Record<string, React.ElementType> = {
  raise: ArrowUp,
  lower: ArrowDown,
  pause: Pause,
  add: PlusCircle,
  negate: XCircle
};

export default function AdOptimizerPage() {
  const [targetAcos, setTargetAcos] = useState(25);

  const mutation = useMutation({
    mutationFn: () => adsApi.optimize({ target_acos: targetAcos })
  });

  const report: AdReport | undefined = mutation.data;

  return (
    <div className='p-4 sm:p-8 max-w-6xl mx-auto'>
      <div className='mb-8'>
        <h1 className='text-2xl font-bold text-white flex items-center gap-2'>
          <Megaphone className='w-6 h-6 text-rose-400' />
          广告优化
        </h1>
        <p className='text-slate-400 mt-1'>分析按点击付费的广告活动，提供关键词出价建议。</p>
      </div>

      {/* Controls */}
      <div className='flex gap-4 items-end flex-wrap p-5 bg-slate-900 rounded-xl border border-slate-800 mb-6'>
        <div>
          <label className='text-xs text-slate-400 font-medium mb-1 block'>
            目标广告销售成本（ACoS %）
          </label>
          <input
            type='number'
            value={targetAcos}
            onChange={(e) => setTargetAcos(Number(e.target.value))}
            min={5}
            max={80}
            className='w-32 bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2.5 focus:ring-1 focus:ring-rose-500 outline-none'
          />
        </div>
        <button
          onClick={() => mutation.mutate()}
          disabled={mutation.isPending}
          className='flex items-center gap-2 px-6 py-2 bg-rose-600 hover:bg-rose-500 disabled:opacity-50 text-white font-medium rounded-lg transition-colors'
        >
          {mutation.isPending ? (
            <Loader2 className='w-4 h-4 animate-spin' />
          ) : (
            <Megaphone className='w-4 h-4' />
          )}
          {mutation.isPending ? '分析中…' : '开始优化'}
        </button>
      </div>

      {mutation.isError && (
        <div className='mb-4 p-4 bg-red-900/30 border border-red-700 rounded-xl text-red-300 text-sm'>
          {(mutation.error as Error).message}
        </div>
      )}

      {report && (
        <div className='space-y-4'>
          {/* KPIs */}
          <div className='grid grid-cols-2 sm:grid-cols-4 gap-4'>
            {[
              { label: '广告活动数', value: report.campaign_count.toString() },
              { label: '广告总花费', value: `$${report.total_spend.toFixed(0)}` },
              { label: '广告销售额', value: `$${report.total_sales.toFixed(0)}` },
              {
                label: '整体 ACoS',
                value: `${report.overall_acos.toFixed(1)}%`,
                color: report.overall_acos <= targetAcos ? 'text-emerald-400' : 'text-red-400'
              }
            ].map(({ label, value, color }) => (
              <div key={label} className='p-4 bg-slate-900 rounded-xl border border-slate-800'>
                <p className='text-xs text-slate-500 mb-1'>{label}</p>
                <p className={cn('text-2xl font-bold text-white', color)}>{value}</p>
              </div>
            ))}
          </div>

          {/* Savings potential */}
          <div className='grid grid-cols-1 sm:grid-cols-2 gap-4'>
            <div className='p-4 bg-emerald-900/20 border border-emerald-800/40 rounded-xl flex items-center gap-3'>
              <TrendingDown className='w-8 h-8 text-emerald-400 flex-shrink-0' />
              <div>
                <p className='text-xs text-emerald-500'>预计每月节省</p>
                <p className='text-xl font-bold text-emerald-300'>
                  ${report.estimated_monthly_savings.toFixed(0)}
                </p>
              </div>
            </div>
            <div className='p-4 bg-blue-900/20 border border-blue-800/40 rounded-xl flex items-center gap-3'>
              <TrendingUp className='w-8 h-8 text-blue-400 flex-shrink-0' />
              <div>
                <p className='text-xs text-blue-500'>预计销售增长</p>
                <p className='text-xl font-bold text-blue-300'>
                  ${report.estimated_monthly_sales_increase.toFixed(0)}/mo
                </p>
              </div>
            </div>
          </div>

          {/* Summary */}
          <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
            <h3 className='text-white font-semibold mb-2 text-sm'>分析摘要</h3>
            <p className='text-slate-300 text-sm leading-relaxed'>{report.executive_summary}</p>
          </div>

          {/* Keyword recommendations */}
          {report.keyword_recommendations.length > 0 && (
            <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
              <h3 className='text-white font-semibold mb-4 text-sm'>关键词出价建议</h3>
              <div className='overflow-x-auto'>
                <table className='w-full text-sm'>
                  <thead>
                    <tr className='text-xs text-slate-500 border-b border-slate-800'>
                      <th className='text-left pb-2 pr-4'>关键词</th>
                      <th className='text-right pb-2 pr-4'>当前出价</th>
                      <th className='text-right pb-2 pr-4'>建议出价</th>
                      <th className='text-right pb-2 pr-4'>ACoS</th>
                      <th className='text-left pb-2 pr-4'>操作</th>
                      <th className='text-left pb-2'>原因</th>
                    </tr>
                  </thead>
                  <tbody className='divide-y divide-slate-800'>
                    {report.keyword_recommendations.map((kw, i) => {
                      const ActionIcon = ACTION_ICONS[kw.action] || TrendingUp;
                      return (
                        <tr key={i} className='hover:bg-slate-800/50 transition-colors'>
                          <td className='py-2.5 pr-4 text-white font-mono text-xs'>{kw.keyword}</td>
                          <td className='py-2.5 pr-4 text-slate-400 text-right'>
                            ${kw.current_bid.toFixed(2)}
                          </td>
                          <td className='py-2.5 pr-4 text-white font-semibold text-right'>
                            ${kw.recommended_bid.toFixed(2)}
                          </td>
                          <td
                            className={cn(
                              'py-2.5 pr-4 text-right font-semibold',
                              kw.acos <= targetAcos ? 'text-emerald-400' : 'text-red-400'
                            )}
                          >
                            {kw.acos.toFixed(1)}%
                          </td>
                          <td className='py-2.5 pr-4'>
                            <span
                              className={cn(
                                'inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full font-medium',
                                ACTION_STYLES[kw.action]
                              )}
                            >
                              <ActionIcon className='w-3 h-3' />
                              {ACTION_LABELS[kw.action] || '其他建议'}
                            </span>
                          </td>
                          <td className='py-2.5 text-slate-400 text-xs'>{kw.reason}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Negative keywords */}
          {report.negative_keyword_suggestions.length > 0 && (
            <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
              <h3 className='text-white font-semibold mb-3 text-sm'>建议添加的否定关键词</h3>
              <div className='flex flex-wrap gap-2'>
                {report.negative_keyword_suggestions.map((kw, i) => (
                  <span
                    key={i}
                    className='text-xs bg-red-900/30 text-red-300 px-2 py-1 rounded-full'
                  >
                    {kw}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
