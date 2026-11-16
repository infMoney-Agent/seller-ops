export interface AdReport {
  campaign_count: number
  total_spend: number
  total_sales: number
  overall_acos: number
  overall_roas: number
  keyword_recommendations: Array<{
    keyword: string
    current_bid: number
    recommended_bid: number
    impressions: number
    clicks: number
    ctr: number
    spend: number
    sales: number
    acos: number
    action: string
    reason: string
  }>
  budget_recommendations: Array<{ campaign_name: string; current_budget: number; recommended_budget: number; reason: string }>
  negative_keyword_suggestions: string[]
  executive_summary: string
  estimated_monthly_savings: number
  estimated_monthly_sales_increase: number
}

export interface GeneratedListing {
  title: string
  bullet_points: string[]
  description: string
  search_terms: string[]
  subject_matter: string[]
  a_plus_draft: string
  seo_score: number
  character_counts: { title: number; description: number; search_terms: number }
}

export interface ProductScore {
  asin: string
  title: string
  price: number
  bsr_rank: number | null
  bsr_category: string
  review_count: number
  rating: number
  competition_score: number
  profit_potential_score: number
  trend_score: number
  overall_score: number
  ai_analysis: string
  recommended: boolean
  tags: string[]
}

export interface ReviewAnalysis {
  asin: string
  total_reviews: number
  avg_rating: number
  sentiment_breakdown: { positive: number; negative: number; neutral: number }
  top_pain_points: string[]
  top_praise_points: string[]
  improvement_suggestions: string[]
  common_keywords: Array<{ word: string; count: number; sentiment: string }>
  rating_distribution: Record<string, number>
  verified_purchase_ratio: number
  summary: string
  listing_recommendations: string[]
}

export interface Monitor {
  asin: string;
  label: string;
  platform: string;
  alert_rules: Record<string, number>;
}

export interface Snapshot {
  asin: string;
  title: string;
  price: number;
  bsr_rank: number | null;
  review_count: number;
  rating: number;
  timestamp: string;
  alert_triggered: boolean;
  alert_reason: string;
}
