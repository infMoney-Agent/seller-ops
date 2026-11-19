'use client';

import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Icons } from '@/components/icons';
import { monitorApi } from '../api';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer
} from 'recharts';
import { cn } from '@/lib/utils';
import { format } from 'date-fns';

const {
  ecomEye: Eye,
  ecomPlus: Plus,
  ecomTrash2: Trash2,
  ecomRefreshCw: RefreshCw,
  ecomBell: Bell,
  ecomTrendingUp: TrendingUp,
  ecomTrendingDown: TrendingDown,
  ecomLoader2: Loader2
} = Icons;

export default function CompetitorMonitorPage() {
  const qc = useQueryClient();
  const [newAsin, setNewAsin] = useState('');
  const [newLabel, setNewLabel] = useState('');
  const [selectedAsin, setSelectedAsin] = useState<string | null>(null);
  const [alertRules, setAlertRules] = useState({ price_drop_pct: 10, bsr_change_pct: 20 });

  const {
    data: monitorsData,
    isLoading,
    error: listError
  } = useQuery({
    queryKey: ['monitors'],
    queryFn: monitorApi.list,
    refetchInterval: 30000
  });

  const { data: snapshotsData, error: snapshotError } = useQuery({
    queryKey: ['snapshots', selectedAsin],
    queryFn: () => monitorApi.snapshots(selectedAsin!, 200),
    enabled: !!selectedAsin
  });

  const addMutation = useMutation({
    mutationFn: () => monitorApi.add({ asin: newAsin, label: newLabel, alert_rules: alertRules }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['monitors'] });
      setNewAsin('');
      setNewLabel('');
    }
  });

  const removeMutation = useMutation({
    mutationFn: (asin: string) => monitorApi.remove(asin),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['monitors'] })
  });

  const snapshotMutation = useMutation({
    mutationFn: (asin: string) => monitorApi.snapshot(asin),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['snapshots', selectedAsin] })
  });

  const monitors = monitorsData?.monitors || [];
  const snapshots = (snapshotsData?.snapshots || []).reverse();

  const error =
    listError ||
    snapshotError ||
    addMutation.error ||
    removeMutation.error ||
    snapshotMutation.error;
  const chartData = snapshots.map((s) => ({
    time: format(new Date(s.timestamp), 'M月d日 HH:mm'),
    price: s.price,
    bsr: s.bsr_rank
  }));

  return (
    <div className='p-4 sm:p-8 max-w-7xl mx-auto'>
      {error && (
        <p role='alert' className='mb-4 rounded-lg bg-red-950 p-3 text-red-200'>
          {error.message}
        </p>
      )}
      <div className='mb-8'>
        <h1 className='text-2xl font-bold text-white flex items-center gap-2'>
          <Eye className='w-6 h-6 text-emerald-400' />
          竞品监控
        </h1>
        <p className='text-slate-400 mt-1'>追踪价格、畅销排名和评论变化，每小时自动检查异常。</p>
      </div>

      <div className='grid grid-cols-1 xl:grid-cols-3 gap-6'>
        {/* Left: Add + list */}
        <div className='xl:col-span-1 space-y-4'>
          {/* Add form */}
          <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
            <h3 className='text-white font-semibold mb-4 text-sm'>添加要监控的 ASIN</h3>
            <div className='space-y-3'>
              <input
                value={newAsin}
                onChange={(e) => setNewAsin(e.target.value.toUpperCase())}
                placeholder='ASIN（例如 B01MFCXDYG）'
                maxLength={10}
                className='w-full bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2.5 focus:ring-1 focus:ring-emerald-500 outline-none placeholder:text-slate-600 font-mono'
              />
              <input
                value={newLabel}
                onChange={(e) => setNewLabel(e.target.value)}
                placeholder='备注（可选）'
                className='w-full bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2.5 focus:ring-1 focus:ring-emerald-500 outline-none placeholder:text-slate-600'
              />
              <div className='grid grid-cols-2 gap-2'>
                <div>
                  <label className='text-xs text-slate-500 block mb-1'>降价预警阈值（%）</label>
                  <input
                    type='number'
                    value={alertRules.price_drop_pct}
                    onChange={(e) =>
                      setAlertRules((r) => ({ ...r, price_drop_pct: Number(e.target.value) }))
                    }
                    className='w-full bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2 focus:ring-1 focus:ring-emerald-500 outline-none'
                  />
                </div>
                <div>
                  <label className='text-xs text-slate-500 block mb-1'>畅销排名变化阈值（%）</label>
                  <input
                    type='number'
                    value={alertRules.bsr_change_pct}
                    onChange={(e) =>
                      setAlertRules((r) => ({ ...r, bsr_change_pct: Number(e.target.value) }))
                    }
                    className='w-full bg-slate-800 border border-slate-700 text-white text-sm rounded-lg px-3 py-2 focus:ring-1 focus:ring-emerald-500 outline-none'
                  />
                </div>
              </div>
              <button
                onClick={() => addMutation.mutate()}
                disabled={addMutation.isPending || !newAsin}
                className='w-full flex items-center justify-center gap-2 py-2 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors'
              >
                {addMutation.isPending ? (
                  <Loader2 className='w-4 h-4 animate-spin' />
                ) : (
                  <Plus className='w-4 h-4' />
                )}
                添加监控
              </button>
            </div>
          </div>

          {/* Monitors list */}
          <div className='space-y-2'>
            {isLoading && <div className='text-slate-500 text-sm'>加载中…</div>}
            {monitors.map((m) => (
              <div
                key={m.asin}
                onClick={() => setSelectedAsin(m.asin)}
                className={cn(
                  'p-4 rounded-xl border cursor-pointer transition-all',
                  selectedAsin === m.asin
                    ? 'bg-emerald-900/30 border-emerald-600'
                    : 'bg-slate-900 border-slate-800 hover:border-slate-700'
                )}
              >
                <div className='flex items-center justify-between'>
                  <div>
                    <p className='font-mono text-sm text-white'>{m.asin}</p>
                    {m.label && <p className='text-xs text-slate-400 mt-0.5'>{m.label}</p>}
                  </div>
                  <div className='flex gap-1'>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        snapshotMutation.mutate(m.asin);
                      }}
                      className='p-1.5 text-slate-500 hover:text-emerald-400 transition-colors'
                      aria-label='立即记录'
                      title='立即记录'
                    >
                      {snapshotMutation.isPending ? (
                        <Loader2 className='w-4 h-4 animate-spin' />
                      ) : (
                        <RefreshCw className='w-4 h-4' />
                      )}
                    </button>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        removeMutation.mutate(m.asin);
                      }}
                      className='p-1.5 text-slate-500 hover:text-red-400 transition-colors'
                    >
                      <Trash2 className='w-4 h-4' />
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Right: Charts */}
        <div className='xl:col-span-2'>
          {!selectedAsin ? (
            <div className='h-64 flex items-center justify-center text-slate-600 border border-slate-800 rounded-xl'>
              选择 ASIN 查看价格和畅销排名历史。
            </div>
          ) : (
            <div className='space-y-4'>
              {/* Alerts */}
              {snapshots
                .filter((s) => s.alert_triggered)
                .slice(-3)
                .map((s, i) => (
                  <div
                    key={i}
                    className='flex items-center gap-2 p-3 bg-amber-900/30 border border-amber-700 rounded-lg text-amber-300 text-sm'
                  >
                    <Bell className='w-4 h-4 flex-shrink-0' />
                    {s.alert_reason}
                  </div>
                ))}

              {/* Price chart */}
              <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
                <h3 className='text-white font-semibold mb-4 text-sm flex items-center gap-2'>
                  <TrendingUp className='w-4 h-4 text-emerald-400' /> 价格历史 · {selectedAsin}
                </h3>
                {chartData.length === 0 ? (
                  <p className='text-slate-500 text-sm py-8 text-center'>
                    还没有记录，点击刷新图标立即记录。
                  </p>
                ) : (
                  <ResponsiveContainer width='100%' height={200}>
                    <LineChart data={chartData}>
                      <CartesianGrid strokeDasharray='3 3' stroke='#1e293b' />
                      <XAxis dataKey='time' tick={{ fontSize: 11, fill: '#64748b' }} />
                      <YAxis tick={{ fontSize: 11, fill: '#64748b' }} />
                      <Tooltip
                        contentStyle={{
                          backgroundColor: '#0f172a',
                          border: '1px solid #1e293b',
                          borderRadius: 8
                        }}
                      />
                      <Line
                        type='monotone'
                        dataKey='price'
                        name='价格（美元）'
                        stroke='#10b981'
                        strokeWidth={2}
                        dot={false}
                      />
                    </LineChart>
                  </ResponsiveContainer>
                )}
              </div>

              {/* BSR chart */}
              <div className='p-5 bg-slate-900 rounded-xl border border-slate-800'>
                <h3 className='text-white font-semibold mb-4 text-sm flex items-center gap-2'>
                  <TrendingDown className='w-4 h-4 text-blue-400' /> 畅销排名历史
                </h3>
                {chartData.length === 0 ? (
                  <p className='text-slate-500 text-sm py-8 text-center'>暂无数据。</p>
                ) : (
                  <ResponsiveContainer width='100%' height={200}>
                    <LineChart data={chartData}>
                      <CartesianGrid strokeDasharray='3 3' stroke='#1e293b' />
                      <XAxis dataKey='time' tick={{ fontSize: 11, fill: '#64748b' }} />
                      <YAxis reversed tick={{ fontSize: 11, fill: '#64748b' }} />
                      <Tooltip
                        contentStyle={{
                          backgroundColor: '#0f172a',
                          border: '1px solid #1e293b',
                          borderRadius: 8
                        }}
                      />
                      <Line
                        type='monotone'
                        dataKey='bsr'
                        name='畅销排名'
                        stroke='#3b82f6'
                        strokeWidth={2}
                        dot={false}
                      />
                    </LineChart>
                  </ResponsiveContainer>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
