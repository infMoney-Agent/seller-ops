'use client';
import { useState } from 'react';
import { useSellerSession } from '@/features/auth/session';
import { useScreeningQuery } from '../queries';
import { request } from '../service';
import type {
  JobCreated,
  Product,
  ProfitInputs,
  ProfitOutput,
  ProfitScenario,
  ProfitTemplate
} from '../types';
import { Button, DataBlock, Field, Input, type Action } from './shared';

const fields: [keyof ProfitInputs, string][] = [
  ['sale_price_usd', '售价 USD'],
  ['discount_usd', '单件优惠 USD'],
  ['purchase_cny', '采购价 CNY'],
  ['packaging_cny', '包装 CNY'],
  ['freight_cny', '头程 CNY'],
  ['other_landed_cny', '其他落地成本 CNY'],
  ['fx_cny_per_usd', '汇率（1 USD = CNY）'],
  ['commission_rate', '佣金率（0至1）'],
  ['fba_fee_usd', 'FBA 履约费 USD'],
  ['storage_usd', '单件仓储费 USD'],
  ['ad_rate', '广告费率（0至1）'],
  ['return_rate', '退货率（0至1）'],
  ['loss_per_return_usd', '每次退货净损失 USD'],
  ['other_usd', '其他费用 USD'],
  ['monthly_units', '预计月销量']
];
const empty = (): ProfitInputs => ({
  name: '基准',
  sale_price_usd: null,
  discount_usd: null,
  purchase_cny: null,
  packaging_cny: null,
  freight_cny: null,
  other_landed_cny: null,
  fx_cny_per_usd: null,
  commission_rate: null,
  fba_fee_usd: null,
  storage_usd: null,
  ad_rate: null,
  return_rate: null,
  loss_per_return_usd: null,
  other_usd: null,
  monthly_units: null,
  fee_source: '',
  note: ''
});
type Calculated = { inputs: ProfitInputs; output: ProfitOutput };

export default function ProfitPanel({
  product,
  selected,
  filters,
  act,
  watch,
  busy
}: {
  product: Product | null;
  selected: string[];
  filters: Record<string, string>;
  act: Action;
  watch: (id: string) => void;
  busy: boolean;
}) {
  const session = useSellerSession();
  const [drafts, setDrafts] = useState<ProfitInputs[]>([empty()]);
  const [active, setActive] = useState(0);
  const [results, setResults] = useState<Calculated[]>([]);
  const [templateId, setTemplateId] = useState<string | null>(null);
  const templates = useScreeningQuery<{ items: ProfitTemplate[] }>('/profit/templates');
  const saved = useScreeningQuery<{ items: ProfitScenario[] }>(
    product ? `/products/${product.id}/profit-scenarios` : null
  );
  const input = drafts[active];
  function update(key: keyof ProfitInputs, value: string) {
    setDrafts((old) =>
      old.map((d, i) =>
        i === active
          ? {
              ...d,
              [key]:
                key === 'monthly_units'
                  ? value === ''
                    ? null
                    : Number(value)
                  : value === ''
                    ? null
                    : value
            }
          : d
      )
    );
    setResults([]);
  }
  return (
    <div className='space-y-4'>
      <p>
        当前商品：
        {product
          ? `${product.asin} · ${product.title_zh || product.title}`
          : '请在商品库点击“测算”选择商品'}
      </p>
      <p className='text-muted-foreground text-sm'>
        测算数据来源：{product?.stat_month?.slice(0, 7) || '未选择月份'} ·{' '}
        {product?.source_type || '手动录入'}。批量测算采用商品列表选择的月份，默认各商品最新一期。
      </p>
      <DataBlock title='本子体参数参考' value={product && {父ASIN:product.parent_asin,每套数量:product.units_per_pack,包装重量g:product.package_weight_g,包装长cm:product.package_length_cm,包装宽cm:product.package_width_cm,包装高cm:product.package_height_cm}} /><p className='text-muted-foreground text-xs'>成本按一份可售商品（整套包装）填写；每套数量仅供参考，不自动倍乘采购价，平台费和运费仍需明确录入。</p>
      <p className='text-muted-foreground text-sm'>
        基于输入假设的预计利润。空白成本不会当作零；无费用请明确填 0。平台费和汇率由你核实填写。
      </p>
      <div className='flex flex-wrap gap-2'>
        {drafts.map((d, i) => (
          <Button
            key={i}
            variant={active === i ? 'default' : 'outline'}
            onClick={() => setActive(i)}
          >
            {d.name || `方案${i + 1}`}
          </Button>
        ))}
        <Button
          variant='outline'
          disabled={drafts.length >= 20}
          onClick={() => {
            setDrafts((old) => [...old, { ...input, name: `方案${old.length + 1}` }]);
            setActive(drafts.length);
            setResults([]);
          }}
        >
          复制为新方案
        </Button>
      </div>
      <div className='grid gap-3 rounded border p-4 md:grid-cols-3'>
        <Field label='方案名称'>
          <Input value={input.name} onChange={(e) => update('name', e.target.value)} />
        </Field>
        <Field label='公共模板'>
          <select
            className='rounded border bg-transparent p-2'
            value={templateId || ''}
            onChange={(e) => {
              const t = templates.data?.items.find((v) => v.id === e.target.value);
              setTemplateId(t?.id || null);
              if (t) {
                setDrafts((old) => old.map((d, i) => (i === active ? { ...t.params } : d)));
                setResults([]);
              }
            }}
          >
            <option value=''>手动输入</option>
            {templates.data?.items.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name} v{t.version}
              </option>
            ))}
          </select>
        </Field>
        <Button
          variant='outline'
          disabled={!product}
          onClick={() => product && update('sale_price_usd', product.price_usd || '')}
        >
          使用所选期售价
        </Button>
        {fields.map(([key, label]) => (
          <Field key={key} label={label}>
            <Input
              type='number'
              min='0'
              step={key === 'monthly_units' ? '1' : '0.000001'}
              value={input[key] ?? ''}
              onChange={(e) => update(key, e.target.value)}
            />
          </Field>
        ))}
        <Field label='费用来源'>
          <Input value={input.fee_source} onChange={(e) => update('fee_source', e.target.value)} />
        </Field>
        <Field label='备注'>
          <Input value={input.note} onChange={(e) => update('note', e.target.value)} />
        </Field>
      </div>
      <p className='text-muted-foreground text-xs'>
        每次退货净损失：相对于正常订单的净减少额，包含收入退款、逆向物流与可回收价值，避免再次全额计入已扣除的采购成本。佣金率和广告费率输入小数，例如
        0.15 表示 15%。
      </p>
      <div className='flex flex-wrap gap-2'>
        <Button
          disabled={!product || busy}
          onClick={() =>
            void act(async () => {
              if (!product) return;
              const r = await request<{ items: Calculated[] }>('/profit/calculate', 'POST', {
                product_id: product.id,
                expected_data_revision: product.data_revision,
                monthly_id: product.monthly_id,
                expected_monthly_revision: product.monthly_revision,
                scenarios: drafts
              });
              setResults(r.items);
            })
          }
        >
          比较全部方案
        </Button>
        <Button
          variant='outline'
          disabled={!product || busy}
          onClick={() =>
            void act(async () => {
              if (!product) return;
              await request(`/products/${product.id}/profit-scenarios`, 'POST', {
                expected_data_revision: product.data_revision,
                monthly_id: product.monthly_id,
                expected_monthly_revision: product.monthly_revision,
                inputs: input,
                template_id: templateId
              });
            }, '方案快照已保存')
          }
        >
          保存当前方案
        </Button>
        <Button
          variant='outline'
          disabled={!selected.length || busy}
          onClick={() =>
            void act(async () => {
              const job = await request<JobCreated>('/profit/batches', 'POST', {
                product_ids: selected,
                filters,
                inputs: input,
                template_id: templateId
              });
              watch(job.job_id);
            }, '批量测算已排队；空白售价采用各商品当前售价')
          }
        >
          测算已选 {selected.length} 个商品
        </Button>
        {session.role === 'owner' && (
          <Button
            variant='outline'
            disabled={busy}
            onClick={() =>
              void act(
                () => request('/profit/templates', 'POST', { name: input.name, params: input }),
                '公共模板新版本已保存'
              )
            }
          >
            保存公共模板新版本
          </Button>
        )}
      </div>
      {results.length > 0 && (
        <div className='overflow-auto rounded border'>
          <table className='w-full text-left text-sm'>
            <thead className='bg-muted'>
              <tr>
                {['方案', '单件利润 USD', '利润率', '月利润 USD', '保本净售价 USD', '状态'].map(
                  (label) => (
                    <th key={label} className='p-3'>
                      {label}
                    </th>
                  )
                )}
              </tr>
            </thead>
            <tbody>
              {results.map((r, i) => (
                <tr key={i} className='border-t'>
                  <td className='p-3'>{r.inputs.name}</td>
                  <td>{r.output.profit_usd ?? '—'}</td>
                  <td>
                    {r.output.margin ? `${(Number(r.output.margin) * 100).toFixed(2)}%` : '—'}
                  </td>
                  <td>{r.output.monthly_profit_usd ?? '—'}</td>
                  <td>{r.output.break_even_price_usd ?? '不可计算'}</td>
                  <td>
                    {r.output.state === 'incomplete'
                      ? `待补充：${r.output.missing?.join('、')}`
                      : '测算完成'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <h3 className='font-semibold'>已保存方案</h3>
      {saved.data?.items.map((s) => (
        <DataBlock
          key={s.id}
          title={`${s.name}${s.stale ? ' · 数据或模板已更新，请重新测算' : ''} · 单件利润 ${s.output.profit_usd ?? '待补充'} USD`}
          value={s}
        />
      ))}
    </div>
  );
}
