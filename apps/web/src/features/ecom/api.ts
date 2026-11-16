import type { AdReport, GeneratedListing, Monitor, ProductScore, ReviewAnalysis, Snapshot } from './types';

async function request<T>(path: string, method = 'GET', body?: object): Promise<T> {
  const response = await fetch('/api/ecom' + path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    cache: 'no-store'
  });
  if (response.status === 401) {
    window.location.assign('/auth/sign-in');
    throw new Error('Sign in to continue.');
  }
  if (!response.ok) {
    const result = await response.json().catch(() => null);
    throw new Error(typeof result?.detail === 'string' ? result.detail : 'The request could not be completed.');
  }
  return response.json();
}

export const productResearchApi = {
  runResearch: (params: { category: string; limit?: number; min_score?: number }) =>
    request<{ results: ProductScore[] }>('/product-research/research', 'POST', params)
};

export const listingApi = {
  generate: (params: { keyword: string; product_details?: object; language?: string }) =>
    request<GeneratedListing>('/listing/generate', 'POST', params),
  optimize: (params: { asin: string; language?: string }) =>
    request<GeneratedListing>('/listing/optimize', 'POST', params)
};

export const reviewsApi = {
  analyze: (params: { asin: string; max_pages?: number }) =>
    request<ReviewAnalysis>('/reviews/analyze', 'POST', params)
};

export const monitorApi = {
  list: () => request<{ monitors: Monitor[] }>('/monitor/list'),
  add: (params: { asin: string; label?: string; alert_rules?: object }) =>
    request('/monitor/add', 'POST', params),
  remove: (asin: string) => request('/monitor/remove/' + encodeURIComponent(asin), 'DELETE'),
  snapshots: (asin: string, limit: number) =>
    request<{ snapshots: Snapshot[] }>('/monitor/snapshots/' + encodeURIComponent(asin) + '?limit=' + limit),
  snapshot: (asin: string) => request('/monitor/snapshot/' + encodeURIComponent(asin), 'POST')
};

export const adsApi = {
  optimize: (params: { target_acos: number }) =>
    request<AdReport>('/ads/optimize?target_acos=' + encodeURIComponent(params.target_acos), 'POST')
};
