'use client';
import { useState } from 'react';
import { useScreeningQuery } from '../queries';
import type { PageResult,ParentMonthly } from '../types';
import { Button,Field,Input,formatMoney } from './shared';
import { periodLabels,sourceLabels } from './MonthlyHistory';
export default function ParentMonthlyPanel() {
  const [parent,setParent]=useState('');const [month,setMonth]=useState('');const [page,setPage]=useState(1);
  const params=new URLSearchParams({page:String(page),...(parent.length===10?{parent_asin:parent.toUpperCase()}:{}),...(month?{stat_month:month}:{})});
  const data=useScreeningQuery<PageResult<ParentMonthly>>('/parents/monthly?'+params);
  return <div className='space-y-4'><p className='text-muted-foreground text-sm'>父体销量独立保存。这里不展示子体销量的重复拷贝，也不将不同来源或不同周期的数字相加。</p><div className='grid gap-3 md:grid-cols-3'><Field label='父 ASIN 筛选'><Input value={parent} maxLength={10} onChange={e=>{setParent(e.target.value);setPage(1);}} /></Field><Field label='父体统计月份'><Input type='month' value={month} onChange={e=>{setMonth(e.target.value);setPage(1);}} /></Field></div>{data.isError && <p role='alert'>{data.error.message}</p>}<div className='overflow-auto rounded border'><table className='w-full text-left text-sm'><thead><tr>{['父 ASIN','月份 / 口径','来源','父体销量','父体销售额 USD','观察日期'].map(v=><th key={v} className='whitespace-nowrap p-3'>{v}</th>)}</tr></thead><tbody>{data.data?.items.map(row=><tr key={row.id} className='border-t'><td className='p-3'>{row.parent_asin}</td><td>{row.stat_month.slice(0,7)} · {periodLabels[row.period_type]}<p className='text-xs'>{row.period_start} ~ {row.period_end}</p></td><td>{sourceLabels[row.source_type] || row.source_type}</td><td>{row.monthly_sales ?? '—'}</td><td>{formatMoney(row.monthly_revenue)}</td><td>{row.observed_on}</td></tr>)}</tbody></table>{!data.data?.items.length && <p className='p-5 text-sm'>暂无明确的父体统计数据</p>}</div><div className='flex items-center gap-2'><Button variant='outline' disabled={page===1} onClick={()=>setPage(v=>v-1)}>上一页父体</Button><span className='text-sm'>共 {data.data?.total || 0} 条</span><Button variant='outline' disabled={page*50>=(data.data?.total || 0)} onClick={()=>setPage(v=>v+1)}>下一页父体</Button></div></div>;
}
