import {
  ImportRowState,
  JobState,
  type GetImportResponse,
  type ListImportRowsResponse,
  type ImportPreviewRow,
  type GetJobResponse,
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
export function legacyImportList(result: ListImportsResponse | ListImportRowsResponse) {
  if (!result.pagination) throw new Error('Missing import pagination');
  const total = Number(result.pagination.total);
  if (!Number.isSafeInteger(total) || total < 0)
    throw new Error('Import total exceeds JSON safe integer range');
  return {
    items: result.items.map((item) =>
      'fileName' in item ? legacyImport(item) : legacyImportRow(item)
    ),
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

const rowStates: Partial<Record<ImportRowState, string>> = {
  [ImportRowState.VALID]: 'valid',
  [ImportRowState.INVALID]: 'invalid',
  [ImportRowState.COMMITTED]: 'committed'
};
export function legacyImportRow(item: ImportPreviewRow): Record<string, unknown> {
  const state = rowStates[item.state];
  if (!state) throw new Error('Unsupported import row state');
  return {
    ...object(item.legacyFields),
    id: item.id,
    import_id: item.importId,
    member_path: item.memberPath,
    sheet_name: item.sheetName,
    row_no: item.rowNo,
    product_id: item.productId ?? null,
    state,
    raw_data: object(item.rawData)
  };
}
export function legacyImportDetail(result: GetImportResponse) {
  if (!result.item) throw new Error('Missing import detail');
  return { ...legacyImport(result.item), latest_job_id: result.latestJobId ?? null };
}
const jobStates: Partial<Record<JobState, string>> = {
  [JobState.QUEUED]: 'queued',
  [JobState.RUNNING]: 'running',
  [JobState.SUCCEEDED]: 'succeeded',
  [JobState.PARTIAL]: 'partial',
  [JobState.FAILED]: 'failed',
  [JobState.PREVIEW]: 'preview',
  [JobState.APPLIED]: 'applied',
  [JobState.UNDONE]: 'undone'
};
export function legacyJob(result: GetJobResponse) {
  const item = result.item;
  if (!item) throw new Error('Missing job detail');
  const state = jobStates[item.state];
  if (!state) throw new Error('Unsupported job state');
  return {
    ...object(item.legacyFields),
    id: item.id,
    kind: item.kind,
    state,
    progress: item.progress,
    attempts: item.attempts
  };
}
