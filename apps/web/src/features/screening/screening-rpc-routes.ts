// Explicit browser route to generated RPC mapping. File endpoints are excluded.
export const screeningRPCRoutes = [
  {
    name: 'ListProducts',
    method: 'GET',
    path: 'products',
    kind: 'query',
    body: null,
    response: 'PageProduct'
  },
  {
    name: 'GetSummary',
    method: 'GET',
    path: 'summary',
    kind: 'query',
    body: null,
    response: 'SummaryRecord'
  },
  {
    name: 'GetProduct',
    method: 'GET',
    path: 'products/{id}',
    kind: 'query',
    body: null,
    response: 'Product'
  },
  {
    name: 'GetProductFamily',
    method: 'GET',
    path: 'products/{id}/family',
    kind: 'query',
    body: null,
    response: 'FamilyResult'
  },
  {
    name: 'ListProductMonthly',
    method: 'GET',
    path: 'products/{id}/monthly',
    kind: 'query',
    body: null,
    response: 'PageMonthlyRecord'
  },
  {
    name: 'ListProductObservations',
    method: 'GET',
    path: 'products/{id}/observations',
    kind: 'query',
    body: null,
    response: 'PageImportRow'
  },
  {
    name: 'GetObservationSource',
    method: 'GET',
    path: 'products/{id}/observations/{row_id}',
    kind: 'query',
    body: null,
    response: 'ObservationSource'
  },
  {
    name: 'ListProductHistory',
    method: 'GET',
    path: 'products/{id}/history',
    kind: 'query',
    body: null,
    response: 'PageProductEvent'
  },
  {
    name: 'ListProductReferences',
    method: 'GET',
    path: 'products/{id}/references',
    kind: 'query',
    body: null,
    response: 'ProductItems'
  },
  {
    name: 'ListProfitScenarios',
    method: 'GET',
    path: 'products/{id}/profit-scenarios',
    kind: 'query',
    body: null,
    response: 'PageProfitScenario'
  },
  {
    name: 'ListGroupProducts',
    method: 'GET',
    path: 'groups/{group_key}/products',
    kind: 'query',
    body: null,
    response: 'PageProduct'
  },
  {
    name: 'ListParentMonthly',
    method: 'GET',
    path: 'parents/monthly',
    kind: 'query',
    body: null,
    response: 'PageParentMonthly'
  },
  {
    name: 'ListMonths',
    method: 'GET',
    path: 'months',
    kind: 'query',
    body: null,
    response: 'MonthItems'
  },
  {
    name: 'ListTrademarkOptions',
    method: 'GET',
    path: 'trademark-options',
    kind: 'query',
    body: null,
    response: 'TrademarkItems'
  },
  {
    name: 'ListReviewCategories',
    method: 'GET',
    path: 'review-categories',
    kind: 'query',
    body: null,
    response: 'CategoryItems'
  },
  {
    name: 'GetReviewWorkspace',
    method: 'GET',
    path: 'review-workspace',
    kind: 'query',
    body: null,
    response: 'ReviewWorkspace'
  },
  {
    name: 'ListReviewBatches',
    method: 'GET',
    path: 'review-batches',
    kind: 'query',
    body: null,
    response: 'PageBatchRecord'
  },
  {
    name: 'GetReviewBatch',
    method: 'GET',
    path: 'review-batches/{id}',
    kind: 'query',
    body: null,
    response: 'BatchRecord'
  },
  {
    name: 'ListPolicies',
    method: 'GET',
    path: 'policies',
    kind: 'query',
    body: null,
    response: 'PolicyItems'
  },
  {
    name: 'ListProfitTemplates',
    method: 'GET',
    path: 'profit/templates',
    kind: 'query',
    body: null,
    response: 'TemplateItems'
  },
  {
    name: 'ListProfitProducts',
    method: 'GET',
    path: 'profit/products',
    kind: 'query',
    body: null,
    response: 'ProfitProducts'
  },
  {
    name: 'GetScreenRun',
    method: 'GET',
    path: 'screen-runs/{id}',
    kind: 'query',
    body: null,
    response: 'JobRecord'
  },
  {
    name: 'ReviewProduct',
    method: 'PATCH',
    path: 'products/{id}/review',
    kind: 'command',
    body: 'ReviewInput',
    response: 'Product'
  },
  {
    name: 'UpdateProductCategory',
    method: 'PATCH',
    path: 'products/{id}/category',
    kind: 'command',
    body: 'CategoryInput',
    response: 'Product'
  },
  {
    name: 'UpdateProductParameters',
    method: 'PATCH',
    path: 'products/{id}/parameters',
    kind: 'command',
    body: 'ParametersInput',
    response: 'Product'
  },
  {
    name: 'SaveProfitScenario',
    method: 'POST',
    path: 'products/{id}/profit-scenarios',
    kind: 'command',
    body: 'SaveProfitInput',
    response: 'ProfitScenario'
  },
  {
    name: 'CalculateProfit',
    method: 'POST',
    path: 'profit/calculate',
    kind: 'command',
    body: 'CalculateProfitInput',
    response: 'CalculateProfitResult'
  },
  {
    name: 'CreateReviewPreview',
    method: 'POST',
    path: 'review-batches/preview',
    kind: 'command',
    body: 'ReviewBatchInput',
    response: 'BatchPreview'
  },
  {
    name: 'ApplyReviewBatch',
    method: 'POST',
    path: 'review-batches/{id}/apply',
    kind: 'command',
    body: null,
    response: 'BatchResult'
  },
  {
    name: 'UndoReviewBatch',
    method: 'POST',
    path: 'review-batches/{id}/undo',
    kind: 'command',
    body: null,
    response: 'BatchResult'
  },
  {
    name: 'CreatePolicy',
    method: 'POST',
    path: 'policies',
    kind: 'command',
    body: 'ConfigInput',
    response: 'Policy'
  },
  {
    name: 'CreateProfitTemplate',
    method: 'POST',
    path: 'profit/templates',
    kind: 'command',
    body: 'ConfigInput',
    response: 'ProfitTemplate'
  },
  {
    name: 'TestPolicy',
    method: 'POST',
    path: 'policies/{id}/test',
    kind: 'command',
    body: 'PolicyTestInput',
    response: 'PolicyTestResult'
  },
  {
    name: 'ActivatePolicy',
    method: 'POST',
    path: 'policies/{id}/activate',
    kind: 'command',
    body: 'PolicyTestInput',
    response: 'ScreenJob'
  },
  {
    name: 'CreateScreenRun',
    method: 'POST',
    path: 'screen-runs',
    kind: 'command',
    body: 'SelectionInput',
    response: 'ScreenJob'
  },
  {
    name: 'CreateExport',
    method: 'POST',
    path: 'exports',
    kind: 'command',
    body: 'SelectionInput',
    response: 'ScreenJob'
  },
  {
    name: 'CreateProfitBatch',
    method: 'POST',
    path: 'profit/batches',
    kind: 'command',
    body: 'SelectionInput',
    response: 'ScreenJob'
  }
] as const;
