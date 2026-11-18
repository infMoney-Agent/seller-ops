'use client';
import { useState } from 'react';
import { request } from '../service';
import type { Product } from '../types';
import { Button,Field,Input,type Action } from './shared';

const fields: [keyof Product,string][]=[['parent_asin','父 ASIN'],['material','材质'],['weight_g','产品净重 g'],['package_weight_g','包装后重量 g'],['package_length_cm','包装长 cm'],['package_width_cm','包装宽 cm'],['package_height_cm','包装高 cm'],['units_per_pack','每套数量'],['carton_units','装箱数量'],['carton_weight_g','外箱重量 g'],['carton_length_cm','外箱长 cm'],['carton_width_cm','外箱宽 cm'],['carton_height_cm','外箱高 cm']];
export default function ProductParameters({product,act,busy,close}:{product:Product;act:Action;busy:boolean;close:()=>void}) {
  const [values,setValues]=useState<Record<string,string>>(()=>Object.fromEntries(fields.map(([key])=>[key,product[key]==null?'':String(product[key])])));
  return <details className='space-y-3 rounded border p-3'><summary className='cursor-pointer font-semibold'>商品参数与父子关系</summary><p className='text-muted-foreground text-xs'>参数属于当前子 ASIN，重量统一为克，尺寸统一为厘米。修改后历史利润保留快照，并提示重新测算。</p><div className='grid gap-3 md:grid-cols-3'>{fields.map(([key,label])=><Field key={key} label={label}><Input type={key==='material'||key==='parent_asin'?'text':'number'} min='0' step={key==='units_per_pack'||key==='carton_units'?'1':'0.000001'} value={values[key] || ''} onChange={e=>setValues(old=>({...old,[key]:e.target.value}))} /></Field>)}</div><Button disabled={busy} onClick={()=>void act(async()=>{const body:Record<string,string|number|null>={expected_data_revision:product.data_revision};for(const [key] of fields){const value=values[key]?.trim();body[key]=!value?null:key==='parent_asin'?value.toUpperCase():key==='units_per_pack'||key==='carton_units'?Number(value):value;}await request(`/products/${product.id}/parameters`,'PATCH',body);close();},'商品参数已保存')}>保存商品参数</Button></details>;
}
