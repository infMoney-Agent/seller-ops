import type { JsonObject } from '@bufbuild/protobuf';
import type {
  ImportSettings,
  ImportJobSubmission,
  DeleteImportResponse
} from '@/gen/sellerops/screening/v1/import_pb';
export function importSettings(body: JsonObject) {
  const result: {
    legacyFields: JsonObject;
    clearFields: string[];
    mapping?: JsonObject;
    sourcePeriod?: string;
    observedOn?: string;
    sourceType?: string;
    periodType?: string;
    periodStart?: string;
    periodEnd?: string;
    defaultSalesScope?: string;
  } = { legacyFields: { ...body }, clearFields: [] };
  const fields = {
    source_period: 'sourcePeriod',
    observed_on: 'observedOn',
    source_type: 'sourceType',
    period_type: 'periodType',
    period_start: 'periodStart',
    period_end: 'periodEnd',
    default_sales_scope: 'defaultSalesScope'
  } as const;
  for (const [key, name] of Object.entries(fields)) {
    const value = body[key];
    if (typeof value === 'string') {
      result[name as (typeof fields)[keyof typeof fields]] = value;
      delete result.legacyFields[key];
    } else if (value === null) {
      result.clearFields.push(key);
      delete result.legacyFields[key];
    }
  }
  const mapping = body.mapping;
  if (mapping !== null && typeof mapping === 'object' && !Array.isArray(mapping)) {
    result.mapping = mapping;
    delete result.legacyFields.mapping;
  } else if (mapping === null) {
    result.clearFields.push('mapping');
    delete result.legacyFields.mapping;
  }
  return result satisfies Partial<ImportSettings>;
}
export function legacySubmission(item: ImportJobSubmission | undefined) {
  if (!item) throw new Error('Missing task submission');
  const result: Record<string, unknown> = {
    ...item.legacyFields,
    job_id: item.jobId,
    state: item.state
  };
  if (item.importId !== undefined) result.import_id = item.importId;
  return result;
}
export function legacyDeleteImport(item: DeleteImportResponse) {
  const result: Record<string, unknown> = { id: item.id, deleted: item.deleted };
  for (const [key, value] of [
    ['removed_products', item.removedProducts],
    ['retained_products', item.retainedProducts],
    ['removed_monthly', item.removedMonthly]
  ] as const) {
    if (value !== undefined) {
      const n = Number(value);
      if (!Number.isSafeInteger(n) || n < 0) throw new Error('Unsafe deletion count');
      result[key] = n;
    }
  }
  return result;
}
