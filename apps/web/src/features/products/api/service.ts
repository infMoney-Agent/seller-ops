import { request } from '@/features/auth/session';
import { productsPath } from './paths';
import type {
  ProductFilters,
  ProductsResponse,
  ProductByIdResponse,
  ProductMutationPayload
} from './types';

export function getProducts(filters: ProductFilters) {
  return request<ProductsResponse>(productsPath(filters));
}
export function getProductById(id: number) {
  return request<ProductByIdResponse>(`/api/products/${id}`);
}
export function createProduct(data: ProductMutationPayload) {
  return request<ProductByIdResponse>('/api/products', 'POST', data);
}
export function updateProduct(id: number, data: ProductMutationPayload) {
  return request<ProductByIdResponse>(`/api/products/${id}`, 'PUT', data);
}
export function deleteProduct(id: number) {
  return request<{ success: boolean; message: string }>(`/api/products/${id}`, 'DELETE');
}
