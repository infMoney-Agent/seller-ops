import {
  ImportState,
  type ImportRecord,
  type ListImportsResponse
} from '@/gen/sellerops/screening/v1/import_pb';
const states: Partial<Record<ImportState, string>> = {
  [ImportState.UPLOADED]: 'uploaded',
  [ImportState.PARSING]: 'parsing',
  [ImportState.PREVIEW]: 'preview',
  [ImportState.IMPORTING]: 'importing',
  [ImportState.COMPLETED]: 'completed',
  [ImportState.PARTIAL]: 'partial',
  [ImportState.FAILED]: 'failed'
};
function object(value: ImportRecord['counts']): Record<string, unknown> {
  return value ? { ...value } : {};
}
export function legacyImport(item: ImportRecord): Record<string, unknown> {
  const state = states[item.state];
  if (!state) throw new Error('Unsupported import state');
  return {
    ...object(item.legacyFields),
    id: item.id,
    file_name: item.fileName,
    state,
    marketplace: item.marketplace,
    source_period: item.sourcePeriod,
    observed_on: item.observedOn ?? null,
    source_type: item.sourceType,
    period_type: item.periodType,
    period_start: item.periodStart ?? null,
    period_end: item.periodEnd ?? null,
    default_sales_scope: item.defaultSalesScope,
    counts: object(item.counts),
    mapping: object(item.mapping)
  };
}
export function legacyImportList(result: ListImportsResponse) {
  if (!result.pagination) throw new Error('Missing import pagination');
  const total = Number(result.pagination.total);
  if (!Number.isSafeInteger(total) || total < 0)
    throw new Error('Import total exceeds JSON safe integer range');
  return {
    items: result.items.map(legacyImport),
    page: result.pagination.page,
    page_size: result.pagination.pageSize,
    total
  };
}
// Match the old strconv.Atoi defaults for ordinary query inputs.
export function importPagination(params: URLSearchParams) {
  const parse = (name: string) => {
    const value = params.get(name) || '';
    if (!/^[+-]?\d+$/.test(value)) return 0;
    const n = Number(value);
    if (!Number.isSafeInteger(n)) return 0;
    return n;
  };
  const p = parse('page'),
    n = parse('page_size');
  if (p > 2147483647) throw new RangeError('页码超出支持范围');
  return { page: Math.max(1, p), pageSize: n < 1 ? 50 : Math.min(100, n) };
}
