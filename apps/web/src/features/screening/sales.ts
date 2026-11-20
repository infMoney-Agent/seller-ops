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
