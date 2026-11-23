import type { Product, ProfitInputs, ProfitOutput, ProfitScenario } from './types';

export interface ProfitTableProduct extends Product {
  scenario: ProfitScenario | null;
  scenario_stale: boolean;
  imported_fees: { fba_fee_usd: string | null; gross_margin_rate: string | null };
  draft_inputs: ProfitInputs;
  preview_output: ProfitOutput;
  calculation_error?: string;
}
export interface ProfitRowDraft {
  inputs: ProfitInputs;
  dataRevision: number;
  monthlyId: string | null;
  monthlyRevision: number | null;
  dirty: boolean;
  saveState?: 'queued' | 'saving' | 'saved' | 'failed';
  output: ProfitOutput | null;
  error: string;
}
export function initialProfitDraft(product: ProfitTableProduct): ProfitRowDraft {
  return {
    inputs: { ...product.draft_inputs },
    dataRevision: product.data_revision,
    monthlyId: product.monthly_id,
    monthlyRevision: product.monthly_revision,
    dirty: false,
    output: product.preview_output,
    error: product.calculation_error || ''
  };
}
export const profitFieldLabels: Record<string, string> = {
  sale_price_usd: '售价',
  discount_usd: '优惠',
  purchase_cny: '供货价',
  packaging_cny: '包装费',
  freight_cny: '头程',
  other_landed_cny: '其他落地成本',
  fx_cny_per_usd: '汇率',
  commission_rate: '佣金率',
  fba_fee_usd: 'FBA',
  storage_usd: '仓租',
  storage_days: '预计在库天数',
  ad_rate: '广告费率',
  return_rate: '退货率',
  loss_per_return_usd: '退货净损失',
  other_usd: '其他费用',
  recalculate: '重新测算'
};
export const commonProfitFields: Array<[keyof ProfitInputs, string, boolean?]> = [
  ['storage_days', '预计在库天数'],
  ['fx_cny_per_usd', '汇率 CNY/USD'],
  ['ad_rate', '广告费率 %', true],
  ['return_rate', '退货率 %', true],
  ['loss_per_return_usd', '退货净损失 USD'],
  ['volumetric_divisor_cm3_per_kg', '体积重分母 cm³/kg'],
  ['discount_usd', '单件优惠 USD'],
  ['packaging_cny', '包装费 CNY'],
  ['other_landed_cny', '其他落地成本 CNY'],
  ['other_usd', '其他费用 USD']
];
// Percent editing only moves decimal digits; all financial arithmetic stays in Go.
export function rateToPercent(value: string | null | undefined): string {
  if (!value) return '';
  if (!/^\d+(?:\.\d+)?$/.test(value)) return value;
  const [whole, decimal = ''] = value.split('.'),
    digits = whole + decimal.padEnd(2, '0');
  const cut = whole.length + 2,
    integer = digits.slice(0, cut).replace(/^0+(?=\d)/, '');
  const fraction = digits.slice(cut).replace(/0+$/, '');
  return integer + (fraction ? '.' + fraction : '');
}
export function percentToRate(value: string): string | null {
  if (value === '') return null;
  if (!/^\d+(?:\.\d*)?$/.test(value)) return value;
  const [whole, fraction = ''] = value.split('.'),
    digits = whole.padStart(3, '0');
  return (
    digits.slice(0, -2).replace(/^0+(?=\d)/, '') +
    '.' +
    (digits.slice(-2) + fraction).replace(/0+$/, '').padEnd(1, '0')
  );
}
export function supplierLink(value: string | undefined): string | null {
  try {
    const url = new URL(value || '');
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password
      ? url.href
      : null;
  } catch {
    return null;
  }
}
export function profitNumber(value: string | number | null | undefined, digits = 2): string {
  if (value === null || value === undefined || value === '') return '—';
  const n = Number(value);
  return Number.isFinite(n)
    ? n.toLocaleString('en-US', { maximumFractionDigits: digits, minimumFractionDigits: digits })
    : '—';
}
