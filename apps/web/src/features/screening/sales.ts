type Sales = {
  monthly_sales: number | null;
  sales_scope: string | null;
  sales_quantity_kind?: string;
};
export function childSales(value: Sales): string {
  if (value.sales_scope !== 'child' || value.monthly_sales == null) return '—';
  return (
    value.monthly_sales.toLocaleString('zh-CN') +
    (value.sales_quantity_kind === 'lower_bound' ? '+' : '')
  );
}
export function salesNote(value: Sales): string {
  return value.sales_scope !== 'child' || value.monthly_sales == null
    ? '子体销量缺失'
    : value.sales_quantity_kind === 'lower_bound'
      ? '购买量级'
      : value.sales_quantity_kind === 'single_variant_estimate'
        ? '单变体推定'
        : '';
}
