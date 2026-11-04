import { HydrationBoundary, dehydrate } from '@tanstack/react-query';
import { getQueryClient } from '@/lib/query-client';
import { searchParamsCache } from '@/lib/searchparams';
import { productKeys } from '../api/queries';
import { productsPath } from '../api/paths';
import type { ProductsResponse } from '../api/types';
import { backendFetch, requireSession } from '@/lib/backend';
import { ProductTable } from './product-tables';

export default async function ProductListingPage() {
  const session = await requireSession();
  if (!session.workspaceId) return <p>Select a workspace to view products.</p>;
  const page = searchParamsCache.get('page');
  const search = searchParamsCache.get('name');
  const pageLimit = searchParamsCache.get('perPage');
  const categories = searchParamsCache.get('category');
  const sort = searchParamsCache.get('sort');

  const filters = {
    page,
    limit: pageLimit,
    ...(search && { search }),
    ...(categories && { categories }),
    ...(sort && { sort })
  };

  const queryClient = getQueryClient();

  queryClient.setQueryData(
    productKeys.list(filters),
    await backendFetch<ProductsResponse>(productsPath(filters), session.workspaceId)
  );

  return (
    <HydrationBoundary state={dehydrate(queryClient)}>
      <ProductTable />
    </HydrationBoundary>
  );
}
