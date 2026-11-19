import type { ProductFilters } from './types';

export function productsPath(filters: ProductFilters) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined) params.set(key, String(value));
  }
  return `/api/products?${params}`;
}
