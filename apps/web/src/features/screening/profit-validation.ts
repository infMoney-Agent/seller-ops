import type { ProfitInputs } from './types';
export type ProfitErrors = Partial<Record<keyof ProfitInputs, string>>;
const rates = new Set(['commission_rate', 'ad_rate', 'return_rate']);
const positive = new Set(['sale_price_usd', 'fx_cny_per_usd', 'volumetric_divisor_cm3_per_kg']);
const textFields = new Set([
  'name',
  'fee_source',
  'note',
  'supplier_url',
  'commission_mode',
  'commission_category',
  'storage_mode',
  'storage_month'
]);
export function validateProfitDraft(input: ProfitInputs): ProfitErrors {
  const errors: ProfitErrors = {};
  if (!input.name.trim()) errors.name = '请填写方案名称。';
  for (const [field, value] of Object.entries(input)) {
    if (textFields.has(field) || value === null || value === '') continue;
    const key = field as keyof ProfitInputs;
    const number = Number(value);
    if (!Number.isFinite(number) || (typeof value === 'string' && !/^\d+(?:\.\d+)?$/.test(value))) {
      errors[key] = '请输入非负十进制数值，例如 0.15。';
    } else if (number < 0) {
      errors[key] = '不能小于 0。';
    } else if (positive.has(field) && number <= 0) {
      errors[key] = '必须大于 0。';
    } else if (rates.has(field) && number > 1) {
      errors[key] = '比例应在 0 至 1 之间，例如 0.15 表示 15%。';
    } else if (field === 'storage_days' && (number < 1 || number > 3650)) {
      errors.storage_days = '预计在库天数须为1至3650。';
    } else if (field === 'monthly_units' && !Number.isSafeInteger(number)) {
      errors.monthly_units = '月销量必须是可精确表示的非负整数。';
    }
  }
  if (
    input.sale_price_usd !== null &&
    input.discount_usd !== null &&
    !errors.sale_price_usd &&
    !errors.discount_usd &&
    Number(input.discount_usd) >= Number(input.sale_price_usd)
  ) {
    errors.discount_usd = '优惠金额必须小于售价。';
  }
  // Missing costs remain missing: the API returns an incomplete calculation, never zero cost.
  return errors;
}
