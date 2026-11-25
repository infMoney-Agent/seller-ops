export const ecomRPCRoutes = [
  {
    name: 'ListResearchCategories',
    method: 'GET',
    path: 'product-research/categories',
    body: null,
    response: 'CategoryResult'
  },
  {
    name: 'ResearchProducts',
    method: 'POST',
    path: 'product-research/research',
    body: 'ResearchInput',
    response: 'ResearchResult'
  },
  {
    name: 'ScoreProduct',
    method: 'GET',
    path: 'product-research/score/{asin}',
    body: null,
    response: 'Score'
  },
  {
    name: 'ListListingLanguages',
    method: 'GET',
    path: 'listing/languages',
    body: null,
    response: 'LanguageResult'
  },
  {
    name: 'GenerateListing',
    method: 'POST',
    path: 'listing/generate',
    body: 'ListingInput',
    response: 'ListingResult'
  },
  {
    name: 'OptimizeListing',
    method: 'POST',
    path: 'listing/optimize',
    body: 'ListingInput',
    response: 'ListingResult'
  },
  {
    name: 'AnalyzeReviews',
    method: 'POST',
    path: 'reviews/analyze',
    body: 'ReviewInput',
    response: 'ReviewResult'
  },
  {
    name: 'AnalyzeReviewsLegacy',
    method: 'GET',
    path: 'reviews/analyze/{asin}',
    body: null,
    response: 'ReviewResult'
  },
  {
    name: 'OptimizeAds',
    method: 'POST',
    path: 'ads/optimize',
    body: null,
    response: 'AdsResult'
  },
  {
    name: 'ListMonitors',
    method: 'GET',
    path: 'monitor/list',
    body: null,
    response: 'MonitorsResult'
  },
  {
    name: 'AddMonitor',
    method: 'POST',
    path: 'monitor/add',
    body: 'MonitorInput',
    response: 'MonitorActionResult'
  },
  {
    name: 'RemoveMonitor',
    method: 'DELETE',
    path: 'monitor/remove/{asin}',
    body: null,
    response: 'MonitorActionResult'
  },
  {
    name: 'ListSnapshots',
    method: 'GET',
    path: 'monitor/snapshots/{asin}',
    body: null,
    response: 'SnapshotsResult'
  },
  {
    name: 'CreateSnapshot',
    method: 'POST',
    path: 'monitor/snapshot/{asin}',
    body: null,
    response: 'Snapshot'
  }
] as const;
