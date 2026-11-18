export type Json = string | number | boolean | null | Json[] | { [key: string]: Json };
export type ReviewStatus = 'pending' | 'kept' | 'excluded' | 'unsure';
export interface Product {
  id: string;
  asin: string;
  title: string;
  title_zh: string | null;
  image_url: string | null;
  category: string | null;
  seller_name: string | null;
  seller_region: string | null;
  price_usd: string | null;
  monthly_sales: number | null;
  specs: Record<string, Json>;
  decision: string | null;
  recommendation: string | null;
  direction: string | null;
  group_key: string | null;
  review_status: ReviewStatus;
  review_reason: string;
  review_note: string;
  review_revision: number;
  data_revision: number;
  has_reference: boolean;
  reasons: string[] | null;
  profit_output: ProfitOutput | null;
  profit_data_revision: number | null;
}
export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}
export interface ImportRecord {
  id: string;
  file_name: string;
  state: string;
  observed_on: string | null;
  source_period: string;
  mapping: Record<string, string>;
  counts: { total?: number; valid?: number; invalid?: number };
}
export interface ImportRow {
  id: string;
  member_path: string;
  sheet_name: string;
  row_no: number;
  errors: string[];
  raw_data: Record<string, Json>;
  normalized_data: Record<string, Json> | null;
}
export interface Job {
  id: string;
  kind: string;
  state: string;
  progress: number;
  error: { message: string } | null;
  result: Record<string, Json> | null;
}
export interface JobCreated {
  job_id: string;
  state: string;
  import_id?: string;
  run_id?: string;
}
export interface Target {
  id: string;
  asin?: string;
  title?: string;
  eligible?: boolean;
  skip_reason?: string;
  success?: boolean;
  reason?: string;
}
export interface BatchPreview {
  id: string;
  expires_at: string;
  targets: Target[];
  status: string;
  reason: string;
  note: string;
}
export interface BatchResult {
  id: string;
  success: number;
  skipped: number;
  items: Target[];
}
export interface BatchRecord {
  id: string;
  state: string;
  created_at: string;
  payload: BatchPreview;
  result: BatchResult | null;
  undo_result: BatchResult | null;
}
export interface Policy {
  id: string;
  name: string;
  version: number;
  active: boolean;
  config: Record<string, Json>;
}
export interface ProfitOutput {
  state: string;
  missing?: string[];
  formula_version: string;
  profit_usd?: string;
  margin?: string;
  roi?: string | null;
  monthly_profit_usd?: string;
  break_even_price_usd?: string | null;
  break_even_ad_rate?: string;
}
export interface ProfitInputs {
  name: string;
  sale_price_usd: string | null;
  discount_usd: string | null;
  purchase_cny: string | null;
  packaging_cny: string | null;
  freight_cny: string | null;
  other_landed_cny: string | null;
  fx_cny_per_usd: string | null;
  commission_rate: string | null;
  fba_fee_usd: string | null;
  storage_usd: string | null;
  ad_rate: string | null;
  return_rate: string | null;
  loss_per_return_usd: string | null;
  other_usd: string | null;
  monthly_units: number | null;
  fee_source: string;
  note: string;
}
export interface ProfitScenario {
  id: string;
  name: string;
  inputs: ProfitInputs;
  output: ProfitOutput;
  stale: boolean;
}
export interface ProfitTemplate {
  id: string;
  name: string;
  version: number;
  params: ProfitInputs;
}
