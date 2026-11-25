type Sales = {
  monthly_sales: number | null;
  sales_scope: string | null;
  sales_quantity_kind?: string;
  display_child_sales?: number | null;
};
export function childSalesValue(value: Sales): number | null {
  if (value.display_child_sales !== undefined) return value.display_child_sales;
  return value.sales_scope === 'child' ? value.monthly_sales : null;
}
export function childSales(value: Sales): string {
  const sales = childSalesValue(value);
  if (sales == null) return '—';
  return (
    sales.toLocaleString('zh-CN') +
    (value.sales_scope === 'child' &&
    value.monthly_sales != null &&
    value.sales_quantity_kind === 'lower_bound'
      ? '+'
      : '')
  );
}
