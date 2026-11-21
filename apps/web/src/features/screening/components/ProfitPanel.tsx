'use client';
import { useEffect, useId, useState, type FormEvent, type ReactNode } from 'react';
import { useSellerSession } from '@/features/auth/session';
import { useScreeningQuery } from '../queries';
import { request } from '../service';
import { validateProfitDraft, type ProfitErrors } from '../profit-validation';
import type {
  JobCreated,
  Product,
  ProfitInputs,
  ProfitOutput,
  ProfitScenario,
  ProfitTemplate
} from '../types';
import { Button, DataBlock, Field, Input, TableScroll, type Action } from './shared';

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
function ValidatedField({
  label,
  field,
  prefix,
  error,
  children
}: {
  label: string;
  field: keyof ProfitInputs;
  prefix: string;
  error?: string;
  children: ReactNode;
}) {
  return (
    <div className='flex min-w-0 flex-col gap-1.5 text-sm'>
      <label htmlFor={prefix + '-' + field} className='text-muted-foreground text-xs font-medium'>
        {label}
      </label>
      {children}
      {error && (
        <p id={prefix + '-' + field + '-error'} className='text-destructive text-xs'>
          {error}
        </p>
      )}
    </div>
  );
}

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
  const formId = useId();
  const [validation, setValidation] = useState<{ scenario: number; fields: ProfitErrors } | null>(
    null
  );
  const [focusTarget, setFocusTarget] = useState<keyof ProfitInputs | null>(null);
  const [focusVersion, setFocusVersion] = useState(0);
  const [drafts, setDrafts] = useState<ProfitInputs[]>([empty()]);
  const [active, setActive] = useState(0);
  const [results, setResults] = useState<Calculated[]>([]);
  const [templateId, setTemplateId] = useState<string | null>(null);
  const templates = useScreeningQuery<{ items: ProfitTemplate[] }>('/profit/templates');
  const saved = useScreeningQuery<{ items: ProfitScenario[] }>(
    product ? `/products/${product.id}/profit-scenarios` : null
  );
  const input = drafts[active];
  const errors = validation?.scenario === active ? validation.fields : {};
  useEffect(() => {
    if (focusTarget) document.getElementById(formId + '-' + focusTarget)?.focus();
  }, [focusTarget, focusVersion, formId]);
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const action = (event.nativeEvent as SubmitEvent).submitter?.getAttribute('value') || 'compare';
    const targets =
      action === 'compare'
        ? drafts.map((draft, index) => ({ draft, index }))
        : [{ draft: input, index: active }];
    for (const { draft, index } of targets) {
      const nextErrors = validateProfitDraft(draft);
      if (index === active) {
        for (const control of event.currentTarget.querySelectorAll<HTMLInputElement>(
          'input[name]'
        )) {
          if (control.validity.badInput)
            nextErrors[control.name as keyof ProfitInputs] = '请输入有效数值。';
        }
      }
      const first = Object.keys(nextErrors)[0] as keyof ProfitInputs | undefined;
      if (first) {
        setValidation({ scenario: index, fields: nextErrors });
        setActive(index);
        setFocusTarget(first);
        setFocusVersion((version) => version + 1);
        return;
      }
    }
    setValidation(null);
    void act(
      async () => {
        if (action === 'compare') {
          if (!product) return;
          const response = await request<{ items: Calculated[] }>('/profit/calculate', 'POST', {
            product_id: product.id,
            expected_data_revision: product.data_revision,
            monthly_id: product.monthly_id,
            expected_monthly_revision: product.monthly_revision,
            scenarios: drafts
          });
          setResults(response.items);
        } else if (action === 'save') {
          if (!product) return;
          await request('/products/' + product.id + '/profit-scenarios', 'POST', {
            expected_data_revision: product.data_revision,
            monthly_id: product.monthly_id,
            expected_monthly_revision: product.monthly_revision,
            inputs: input,
            template_id: templateId
          });
        } else if (action === 'batch') {
          if (!selected.length) return;
          const job = await request<JobCreated>('/profit/batches', 'POST', {
            product_ids: selected,
            filters,
            inputs: input,
            template_id: templateId
          });
          watch(job.job_id);
        } else if (action === 'template' && session.role === 'owner') {
          await request('/profit/templates', 'POST', { name: input.name, params: input });
        }
      },
      action === 'save'
        ? '方案快照已保存'
        : action === 'batch'
          ? '批量测算已排队；空白售价采用各商品当前售价'
          : action === 'template'
            ? '公共模板新版本已保存'
            : undefined
    );
  }
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
    setValidation((current) => {
      if (!current || current.scenario !== active) return current;
      const next = { ...current.fields };
      delete next[key];
      return { ...current, fields: next };
    });
    setResults([]);
  }
  return (
    <form aria-label='利润测算方案' noValidate onSubmit={submit} className='space-y-4'>
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
      <DataBlock
        title='本子体参数参考'
        value={
          product && {
            父ASIN: product.parent_asin,
            每套数量: product.units_per_pack,
            包装重量g: product.package_weight_g,
            包装长cm: product.package_length_cm,
            包装宽cm: product.package_width_cm,
            包装高cm: product.package_height_cm
          }
        }
      />
      <p className='text-muted-foreground text-xs'>
        成本按一份可售商品（整套包装）填写；每套数量仅供参考，不自动倍乘采购价，平台费和运费仍需明确录入。
      </p>
      <p className='text-muted-foreground text-sm'>
        基于输入假设的预计利润。空白成本不会当作零；无费用请明确填 0。平台费和汇率由你核实填写。
      </p>
      <fieldset disabled={busy} className='min-w-0 space-y-4'>
        <legend className='sr-only'>利润方案参数与操作</legend>
        <div className='flex flex-wrap gap-2'>
          {drafts.map((d, i) => (
            <Button
              key={i}
              variant={active === i ? 'default' : 'outline'}
              type='button'
              aria-pressed={active === i}
              onClick={() => setActive(i)}
            >
              {d.name || `方案${i + 1}`}
            </Button>
          ))}
          <Button
            variant='outline'
            type='button'
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
        {Object.keys(errors).length > 0 && (
          <p role='alert' className='text-destructive text-sm'>
            请修正 {input.name || '当前方案'} 中标出的字段，再提交。
          </p>
        )}
        <div className='grid gap-3 rounded border p-4 md:grid-cols-3'>
          <ValidatedField label='方案名称' field='name' prefix={formId} error={errors.name}>
            <Input
              id={formId + '-name'}
              name='name'
              required
              aria-invalid={Boolean(errors.name)}
              aria-describedby={errors.name ? formId + '-name-error' : undefined}
              value={input.name}
              onChange={(e) => update('name', e.target.value)}
            />
          </ValidatedField>
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
            type='button'
            disabled={!product}
            onClick={() => product && update('sale_price_usd', product.price_usd || '')}
          >
            使用所选期售价
          </Button>
          {fields.map(([key, label]) => (
            <ValidatedField key={key} label={label} field={key} prefix={formId} error={errors[key]}>
              <Input
                type='number'
                id={formId + '-' + key}
                name={key}
                aria-invalid={Boolean(errors[key])}
                aria-describedby={errors[key] ? formId + '-' + key + '-error' : undefined}
                min={['sale_price_usd', 'fx_cny_per_usd'].includes(key) ? '0.000001' : '0'}
                max={['commission_rate', 'ad_rate', 'return_rate'].includes(key) ? '1' : undefined}
                step={key === 'monthly_units' ? '1' : 'any'}
                value={input[key] ?? ''}
                onChange={(e) => update(key, e.target.value)}
              />
            </ValidatedField>
          ))}
          <Field label='费用来源'>
            <Input
              value={input.fee_source}
              onChange={(e) => update('fee_source', e.target.value)}
            />
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
          <Button type='submit' name='profit_action' value='compare' disabled={!product || busy}>
            比较全部方案
          </Button>
          <Button
            type='submit'
            name='profit_action'
            value='save'
            variant='outline'
            disabled={!product || busy}
          >
            保存当前方案
          </Button>
          <Button
            type='submit'
            name='profit_action'
            value='batch'
            variant='outline'
            disabled={!selected.length || busy}
          >
            测算已选 {selected.length} 个商品
          </Button>
          {session.role === 'owner' && (
            <Button
              type='submit'
              name='profit_action'
              value='template'
              variant='outline'
              disabled={busy}
            >
              保存公共模板新版本
            </Button>
          )}
        </div>
      </fieldset>
      {results.length > 0 && (
        <TableScroll label='利润方案比较，可横向滚动'>
          <table className='w-full text-left text-sm'>
            <caption className='sr-only'>各利润方案的测算结果</caption>
            <thead className='bg-muted'>
              <tr>
                {['方案', '单件利润 USD', '利润率', '月利润 USD', '保本净售价 USD', '状态'].map(
                  (label) => (
                    <th scope='col' key={label} className='p-3'>
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
        </TableScroll>
      )}
      <h3 className='font-semibold'>利润记录与已保存方案</h3>
      {saved.data?.items.map((s) => (
        <DataBlock
          key={s.id}
          title={
            s.record_kind === 'imported'
              ? `卖家精灵来源估算 · FBA ${s.imported_fba_fee_usd ?? '未提供'} USD · 毛利率 ${s.imported_gross_margin_rate == null ? '未提供' : (Number(s.imported_gross_margin_rate) * 100).toFixed(2) + '%'}（非自行测算）`
              : `${s.name}${s.stale ? ' · 数据或模板已更新，请重新测算' : ''} · 单件利润 ${s.output.profit_usd ?? '待补充'} USD`
          }
          value={s}
        />
      ))}
    </form>
  );
}
