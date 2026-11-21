export type Json = string | number | boolean | null | Json[] | { [key: string]: Json };
export type ReviewStatus = 'pending' | 'kept' | 'excluded' | 'unsure';
export interface Product {
  family_count?: number;
  family_sales_pending?: boolean;
  id: string;
  asin: string;
  parent_asin: string | null;
  manual_category: string;
  listing_date?: string | null;
  material: string | null;
  product_length_cm: string | number | null;
  product_width_cm: string | number | null;
  product_height_cm: string | number | null;
  product_dimensions?: string | null;
  product_dimensions_original?: string | null;
  package_dimensions?: string | null;
  package_dimensions_original?: string | null;
  weight_g: string | number | null;
  package_weight_g: string | number | null;
  package_length_cm: string | number | null;
  package_width_cm: string | number | null;
  package_height_cm: string | number | null;
  units_per_pack: number | null;
  carton_units: number | null;
  carton_weight_g: string | number | null;
  carton_length_cm: string | number | null;
  carton_width_cm: string | number | null;
  carton_height_cm: string | number | null;
  sales_scope: 'child' | 'parent' | 'unknown' | null;
  sales_quantity_kind?: string;
  child_sales_raw?: string | null;
  reported_monthly_sales: number | null;
  title: string;
  title_zh: string | null;
  image_url: string | null;
  category: string | null;
  seller_name: string | null;
  seller_id?: string | null;
  seller_region: string | null;
  monthly_id: string | null;
  monthly_revision: number | null;
  stat_month: string | null;
  period_type: string | null;
  period_start: string | null;
  period_end: string | null;
  source_type: string | null;
  monthly_revenue: string | null;
  review_count: number | null;
  rating: string | null;
  bsr_rank: number | null;
  price_usd: string | null;
  monthly_sales: number | null;
  display_child_sales?: number | null;
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
  profit_monthly_revision: number | null;
}
export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}
export interface ImportRecord {
  latest_job_id?: string | null;
  id: string;
  file_name: string;
  state: string;
  observed_on: string | null;
  source_period: string;
  source_type: string;
  period_type: string;
  period_start: string | null;
  period_end: string | null;
  default_sales_scope: string;
  mapping: Record<string, string>;
  counts: {
    headers?: string[];
    total?: number;
    valid?: number;
    invalid?: number;
    skipped_sheets?: { member_path: string; sheet_name: string; reason: string }[];
  };
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
  action?: string;
  category?: string | null;
  scope?: string;
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
  record_kind?: 'calculated' | 'imported';
  imported_fba_fee_usd?: string | null;
  imported_gross_margin_rate?: string | null;
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

export interface MonthlyRecord {
  id: string;
  revision: number;
  sales_scope: string;
  parent_asin_snapshot: string | null;
  stat_month: string;
  period_type: string;
  period_start: string;
  period_end: string;
  observed_on: string;
  source_type: string;
  source_row_id: string;
  price_usd: string | null;
  monthly_sales: number | null;
  display_child_sales?: number | null;
  monthly_revenue: string | null;
  review_count: number | null;
  rating: string | null;
  bsr_rank: number | null;
  file_name: string;
}
export interface MonthOption {
  stat_month: string;
  period_type: string;
  source_type: string;
  products: number;
}

export interface ParentMonthly {
  id: string;
  parent_asin: string;
  stat_month: string;
  period_type: string;
  period_start: string;
  period_end: string;
  observed_on: string;
  source_type: string;
  monthly_sales: number | null;
  display_child_sales?: number | null;
  monthly_revenue: string | null;
}
export interface FamilyResult extends PageResult<Product> {
  parent_asin: string | null;
  known_child_sales_total: number | null;
  unknown_child_count: number;
  aggregate_label: string;
  parent_monthly: ParentMonthly[];
}

export interface ReviewTypeGroup {
  id: string;
  title: string;
  marketplace: string;
  products: number;
  pending: number;
  reviewed: number;
  samples: number;
}
export interface ReviewWorkspace {
  groups: PageResult<ReviewTypeGroup>;
  counts: { products: number; groups: number; pending: number };
  group: ReviewTypeGroup | null;
  items: Product[];
  page: number;
  page_size: number;
  samples: PageResult<Product>;
}

export interface ReviewCategory {
  id: string;
  name: string;
  path: string[];
  products: number;
  pending: number;
  children: ReviewCategory[];
}
