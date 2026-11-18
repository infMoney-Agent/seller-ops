'use client';
import { useState } from 'react';
import { useScreeningQuery } from '../queries';
import type { FamilyResult,Product } from '../types';
import { Button,Badge,formatMoney } from './shared';
import { periodLabels,sourceLabels } from './MonthlyHistory';

export default function FamilyPanel({product,choose}:{product:Product;choose:(ids:string[])=>void}) {
  const [page,setPage]=useState(1);
  const params=new URLSearchParams({page:String(page),...(product.stat_month?{stat_month:product.stat_month.slice(0,7),period_type:product.period_type || 'calendar_month',source_type:product.source_type || ''}:{})});
  const result=useScreeningQuery<FamilyResult>(`/products/${product.id}/family?${params}`);
  if (!product.parent_asin) return <p className='text-muted-foreground text-xs'>尚无确认的父 ASIN；系统不会根据相似标题推断父子关系。</p>;
  return <section className='space-y-3 rounded border p-3'><div className='flex flex-wrap items-center justify-between gap-2'><h4 className='font-semibold'>同父体子体 · {product.parent_asin}</h4><Button size='sm' variant='outline' disabled={!result.data?.items.length} onClick={()=>choose(result.data?.items.map(p=>p.id) || [])}>选择本页子体进行批量处理</Button></div>{result.isError && <p role='alert'>{result.error.message}</p>}<p className='text-muted-foreground text-xs'>{result.data?.aggregate_label}：{result.data?.known_child_sales_total ?? '未确认'}；未知口径子体 {result.data?.unknown_child_count || 0} 个。</p><div className='overflow-auto'><table className='w-full text-left text-sm'><thead><tr>{['子 ASIN','规格','价格 USD','子体销量','销量口径','人工结论'].map(v=><th key={v} className='whitespace-nowrap p-2'>{v}</th>)}</tr></thead><tbody>{result.data?.items.map(p=><tr key={p.id} className='border-t'><td className='p-2'>{p.asin}</td><td>{p.title_zh || p.title}</td><td>{formatMoney(p.price_usd)}</td><td>{p.monthly_sales ?? '—'}</td><td>{p.sales_scope==='child'?'已确认子体':p.sales_scope==='parent'?'父体汇总，另表保存':'未知，不参与合计'}</td><td><Badge value={p.review_status} /></td></tr>)}</tbody></table></div><div className='flex gap-2'><Button size='sm' variant='outline' disabled={page===1} onClick={()=>setPage(v=>v-1)}>上一页子体</Button><span className='text-xs'>共 {result.data?.total || 0} 个子体</span><Button size='sm' variant='outline' disabled={page*50>=(result.data?.total || 0)} onClick={()=>setPage(v=>v+1)}>下一页子体</Button></div><h5 className='font-medium'>来源明确报告的父体销量</h5><p className='text-muted-foreground text-xs'>以下为父体汇总，不重复计入各子体。</p>{!result.data?.parent_monthly.length && <p className='text-sm'>暂无父体汇总数据</p>}{result.data?.parent_monthly.map(m=><p key={m.id} className='text-sm'>{m.stat_month.slice(0,7)} · {periodLabels[m.period_type]} · {sourceLabels[m.source_type] || m.source_type}：销量 {m.monthly_sales ?? '—'}，销售额 ${formatMoney(m.monthly_revenue)}</p>)}</section>;
}
