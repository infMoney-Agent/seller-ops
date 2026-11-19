'use client';

import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Icons } from '@/components/icons';
import { productResearchApi } from '../api';
import { cn } from '@/lib/utils';
import type { ProductScore } from '../types';

const {
  ecomSearch: Search,
  ecomTrendingUp: TrendingUp,
  ecomStar: Star,
  ecomDollarSign: DollarSign,
  ecomBarChart3: BarChart3,
  ecomCheckCircle: CheckCircle,
  ecomXCircle: XCircle,
  ecomLoader2: Loader2
} = Icons;

const CATEGORIES = {
  electronics: '电子产品',
  home: '家居与厨房',
  sports: '运动与户外',
  toys: '玩具与游戏',
  beauty: '美妆与个护',
  health: '健康与家居护理',
  clothing: '服饰',
  pet: '宠物用品',
  tools: '工具与家装',
  office: '办公用品'
};

function ScoreBadge({ score }: { score: number }) {
  const color =
    score >= 7
      ? 'bg-emerald-500/20 text-emerald-300'
      : score >= 5
        ? 'bg-amber-500/20 text-amber-300'
        : 'bg-red-500/20 text-red-300';
  return (
    <span className={cn('text-xs font-bold px-2 py-0.5 rounded-full', color)}>
      {score.toFixed(1)}
    </span>
  );
}

export default function ProductResearchPage() {
  const [category, setCategory] = useState('electronics');
  const [limit, setLimit] = useState(30);
  const [minScore, setMinScore] = useState(6);

  const mutation = useMutation({
    mutationFn: () => productResearchApi.runResearch({ category, limit, min_score: minScore })
  });

  const results: ProductScore[] = mutation.data?.results || [];

  return (
    <div className='p-4 sm:p-8 max-w-7xl mx-auto'>
      <div className='mb-8'>
        <h1 className='text-2xl font-bold text-white flex items-center gap-2'>
          <Search className='w-6 h-6 text-blue-400' />
          选品研究
        </h1>
        <p className='text-slate-400 mt-1'>对亚马逊畅销商品的竞争、盈利潜力和趋势进行评分。</p>
      </div>

      {/* Controls */}
      <div className='flex flex-wrap gap-4 mb-6 p-5 bg-slate-900 rounded-xl border border-slate-800'>
        <div className='flex flex-col gap-1'>
          <label className='text-xs text-slate-400 font-medium'>商品类目</label>
          <select
            value={category}
            onChange={(e) => setCategory(e.target.value)}
            className='bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2 focus:ring-1 focus:ring-blue-500 outline-none'
          >
            {Object.entries(CATEGORIES).map(([c, label]) => (
              <option key={c} value={c}>
                {label}
              </option>
            ))}
          </select>
        </div>
        <div className='flex flex-col gap-1'>
          <label className='text-xs text-slate-400 font-medium'>扫描商品数</label>
          <select
            value={limit}
            onChange={(e) => setLimit(Number(e.target.value))}
            className='bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2 focus:ring-1 focus:ring-blue-500 outline-none'
          >
            {[20, 30, 50, 100].map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </div>
        <div className='flex flex-col gap-1'>
          <label className='text-xs text-slate-400 font-medium'>最低综合评分</label>
          <select
            value={minScore}
            onChange={(e) => setMinScore(Number(e.target.value))}
            className='bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2 focus:ring-1 focus:ring-blue-500 outline-none'
          >
            {[5, 6, 6.5, 7, 7.5].map((n) => (
              <option key={n} value={n}>
                {n}+
              </option>
            ))}
          </select>
        </div>
        <div className='flex items-end'>
          <button
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className='flex items-center gap-2 px-6 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white font-medium rounded-lg transition-colors'
          >
            {mutation.isPending ? (
              <Loader2 className='w-4 h-4 animate-spin' />
            ) : (
              <Search className='w-4 h-4' />
            )}
            {mutation.isPending ? '研究中…' : '开始选品'}
          </button>
        </div>
      </div>

      {/* Error */}
      {mutation.isError && (
        <div className='mb-4 p-4 bg-red-900/30 border border-red-700 rounded-xl text-red-300 text-sm'>
          出错了： {(mutation.error as Error).message}
        </div>
      )}

      {/* Results */}
      {results.length > 0 && (
        <div className='space-y-3'>
          <p className='text-slate-400 text-sm'>
            找到 {results.length} 件综合评分不低于 {minScore} 的商品
          </p>
          {results.map((p) => (
            <div
              key={p.asin}
              className='bg-slate-900 border border-slate-800 rounded-xl p-5 hover:border-slate-700 transition-colors'
            >
              <div className='flex flex-col sm:flex-row sm:items-start justify-between gap-3'>
                <div className='flex-1'>
                  <div className='flex items-center gap-2 flex-wrap mb-2'>
                    <span className='font-mono text-xs text-blue-400 bg-blue-900/30 px-2 py-0.5 rounded'>
                      {p.asin}
                    </span>
                    {p.recommended ? (
                      <span className='flex items-center gap-1 text-xs text-emerald-400'>
                        <CheckCircle className='w-3 h-3' /> 推荐
                      </span>
                    ) : (
                      <span className='flex items-center gap-1 text-xs text-slate-500'>
                        <XCircle className='w-3 h-3' /> 不推荐
                      </span>
                    )}
                    {p.tags.map((tag) => (
                      <span
                        key={tag}
                        className='text-xs bg-slate-800 text-slate-400 px-2 py-0.5 rounded-full'
                      >
                        {tag}
                      </span>
                    ))}
                  </div>
                  <h3 className='text-white font-medium text-sm leading-snug mb-3 line-clamp-2'>
                    {p.title}
                  </h3>
                  <p className='text-slate-400 text-xs leading-relaxed'>{p.ai_analysis}</p>
                </div>

                {/* Scores panel */}
                <div className='flex sm:flex-col gap-3 sm:min-w-40 text-right'>
                  <div className='flex sm:flex-row items-center justify-between gap-2'>
                    <span className='text-xs text-slate-500'>综合</span>
                    <ScoreBadge score={p.overall_score} />
                  </div>
                  <div className='flex sm:flex-row items-center justify-between gap-2'>
                    <span className='text-xs text-slate-500'>竞争</span>
                    <ScoreBadge score={p.competition_score} />
                  </div>
                  <div className='flex sm:flex-row items-center justify-between gap-2'>
                    <span className='text-xs text-slate-500'>盈利潜力</span>
                    <ScoreBadge score={p.profit_potential_score} />
                  </div>
                  <div className='flex sm:flex-row items-center justify-between gap-2'>
                    <span className='text-xs text-slate-500'>趋势</span>
                    <ScoreBadge score={p.trend_score} />
                  </div>
                </div>
              </div>

              {/* Bottom stats row */}
              <div className='flex flex-wrap gap-4 mt-4 pt-4 border-t border-slate-800 text-xs text-slate-500'>
                <span className='flex items-center gap-1'>
                  <DollarSign className='w-3 h-3' />${p.price.toFixed(2)}
                </span>
                {p.bsr_rank && (
                  <span className='flex items-center gap-1'>
                    <BarChart3 className='w-3 h-3' />
                    畅销排名第{p.bsr_rank.toLocaleString('zh-CN')}
                  </span>
                )}
                <span className='flex items-center gap-1'>
                  <Star className='w-3 h-3' />
                  {p.rating} 分（{p.review_count.toLocaleString('zh-CN')} 条评论）
                </span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
