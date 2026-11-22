'use client';

import { useId, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Icons } from '@/components/icons';
import { reviewsApi } from '../api';
import { cn } from '@/lib/utils';
import type { ReviewAnalysis } from '../types';

const {
  ecomStar: Star,
  ecomThumbsUp: ThumbsUp,
  ecomThumbsDown: ThumbsDown,
  ecomLightbulb: Lightbulb,
  ecomLoader2: Loader2,
  ecomBarChart3: BarChart3
} = Icons;

export default function ReviewAnalyzerPage() {
  const fieldId = useId();
  const [asin, setAsin] = useState('');
  const [maxPages, setMaxPages] = useState(3);

  const mutation = useMutation({
    mutationFn: () => reviewsApi.analyze({ asin, max_pages: maxPages })
  });

  const data: ReviewAnalysis | undefined = mutation.data;

  return (
    <div className='p-4 sm:p-8 max-w-5xl mx-auto'>
      <div className='mb-8'>
        <h1 className='text-2xl font-bold text-white flex items-center gap-2'>
          <Star className='w-6 h-6 text-amber-400' />
          评论分析
        </h1>
        <p className='text-slate-400 mt-1'>从亚马逊评论中归纳顾客反馈与商品改进机会。</p>
      </div>

      {/* Input */}
      <div className='flex gap-4 flex-wrap p-5 bg-slate-900 rounded-xl border border-slate-800 mb-6'>
        <div className='flex-1 min-w-48'>
          <label
            id={fieldId + '-ecom-field-1-label'}
            htmlFor={fieldId + '-ecom-field-1'}
            className='text-xs text-slate-400 font-medium mb-1 block'
          >
            ASIN
          </label>
          <input
            aria-labelledby={fieldId + '-ecom-field-1-label'}
            id={fieldId + '-ecom-field-1'}
            value={asin}
            onChange={(e) => setAsin(e.target.value.toUpperCase())}
            placeholder='B01MFCXDYG'
            maxLength={10}
            className='w-full bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2.5 focus:ring-1 focus:ring-amber-500 outline-none placeholder:text-slate-600 font-mono'
          />
        </div>
        <div>
          <label
            id={fieldId + '-ecom-field-2-label'}
            htmlFor={fieldId + '-ecom-field-2'}
            className='text-xs text-slate-400 font-medium mb-1 block'
          >
            评论页数
          </label>
          <select
            aria-labelledby={fieldId + '-ecom-field-2-label'}
            id={fieldId + '-ecom-field-2'}
            value={maxPages}
            onChange={(e) => setMaxPages(Number(e.target.value))}
            className='bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2.5 focus:ring-1 focus:ring-amber-500 outline-none'
          >
            {[1, 3, 5, 10].map((n) => (
              <option key={n} value={n}>
                {n} 页（约 {n * 10} 条评论）
              </option>
            ))}
          </select>
        </div>
        <div className='flex items-end'>
          <button
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending || !asin}
            className='flex items-center gap-2 px-6 py-2 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-white font-medium rounded-lg transition-colors'
          >
            {mutation.isPending ? (
              <Loader2 className='w-4 h-4 animate-spin' />
            ) : (
              <Star className='w-4 h-4' />
            )}
            {mutation.isPending ? '分析中…' : '分析评论'}
          </button>
        </div>
      </div>

      {mutation.isError && (
        <div className='mb-4 p-4 bg-red-900/30 border border-red-700 rounded-xl text-red-300 text-sm'>
          {(mutation.error as Error).message}
        </div>
      )}

      {data && (
        <div className='space-y-4'>
          {/* Overview */}
          <div className='grid grid-cols-2 sm:grid-cols-4 gap-4'>
            {[
              { label: '评论总数', value: data.total_reviews.toLocaleString() },
              { label: '平均评分', value: `${data.avg_rating.toFixed(1)} ★` },
              {
                label: '已验证购买占比',
                value: `${(data.verified_purchase_ratio * 100).toFixed(0)}%`
              },
              {
                label: '好评占比',
                value: `${data.sentiment_breakdown.positive}%`,
                color: 'text-emerald-400'
              }
            ].map(({ label, value, color }) => (
              <div key={label} className='p-4 bg-slate-900 rounded-xl border border-slate-800'>
                <p className='text-xs text-slate-500 mb-1'>{label}</p>
                <p className={cn('text-2xl font-bold text-white', color)}>{value}</p>
              </div>
            ))}
          </div>

          {/* Summary */}
          <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
            <h3 className='text-white font-semibold mb-3 flex items-center gap-2'>
              <BarChart3 className='w-4 h-4 text-amber-400' /> 分析摘要
            </h3>
            <p className='text-slate-300 text-sm leading-relaxed'>{data.summary}</p>
          </div>

          {/* Sentiment grid */}
          <div className='grid grid-cols-1 sm:grid-cols-3 gap-4'>
            <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
              <h3 className='text-red-400 font-semibold mb-3 flex items-center gap-2 text-sm'>
                <ThumbsDown className='w-4 h-4' /> 主要差评痛点
              </h3>
              <ul className='space-y-2'>
                {data.top_pain_points.map((p, i) => (
                  <li key={i} className='text-slate-400 text-xs flex items-start gap-2'>
                    <span className='text-red-600 mt-0.5'>•</span>
                    {p}
                  </li>
                ))}
              </ul>
            </div>
            <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
              <h3 className='text-emerald-400 font-semibold mb-3 flex items-center gap-2 text-sm'>
                <ThumbsUp className='w-4 h-4' /> 主要好评亮点
              </h3>
              <ul className='space-y-2'>
                {data.top_praise_points.map((p, i) => (
                  <li key={i} className='text-slate-400 text-xs flex items-start gap-2'>
                    <span className='text-emerald-600 mt-0.5'>•</span>
                    {p}
                  </li>
                ))}
              </ul>
            </div>
            <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
              <h3 className='text-blue-400 font-semibold mb-3 flex items-center gap-2 text-sm'>
                <Lightbulb className='w-4 h-4' /> 商品改进建议
              </h3>
              <ul className='space-y-2'>
                {data.improvement_suggestions.map((s, i) => (
                  <li key={i} className='text-slate-400 text-xs flex items-start gap-2'>
                    <span className='text-blue-600 mt-0.5'>•</span>
                    {s}
                  </li>
                ))}
              </ul>
            </div>
          </div>

          {/* Listing recommendations */}
          <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
            <h3 className='text-purple-400 font-semibold mb-3 text-sm'>商品页面优化建议</h3>
            <ul className='space-y-2'>
              {data.listing_recommendations.map((r, i) => (
                <li key={i} className='text-slate-300 text-sm flex items-start gap-2'>
                  <span className='text-purple-400 font-bold mt-0.5'>{i + 1}.</span>
                  {r}
                </li>
              ))}
            </ul>
          </div>

          {/* Keywords */}
          <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
            <h3 className='text-white font-semibold mb-3 text-sm'>常见关键词</h3>
            <div className='flex flex-wrap gap-2'>
              {data.common_keywords.map(({ word, count, sentiment }) => (
                <span
                  key={word}
                  className={cn(
                    'text-xs px-2 py-1 rounded-full font-medium',
                    sentiment === 'positive'
                      ? 'bg-emerald-900/40 text-emerald-300'
                      : sentiment === 'negative'
                        ? 'bg-red-900/40 text-red-300'
                        : 'bg-slate-800 text-slate-400'
                  )}
                >
                  {word} <span className='opacity-60'>×{count}</span>
                </span>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
