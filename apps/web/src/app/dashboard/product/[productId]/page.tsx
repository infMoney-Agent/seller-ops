import { HydrationBoundary, dehydrate } from '@tanstack/react-query';
import { getQueryClient } from '@/lib/query-client';
import { productKeys } from '@/features/products/api/queries';
import { backendFetch, requireSession } from '@/lib/backend';
import type { ProductByIdResponse } from '@/features/products/api/types';
import PageContainer from '@/components/layout/page-container';
import ProductViewPage from '@/features/products/components/product-view-page';

export const metadata = {
  title: 'Dashboard : Product View'
};

type PageProps = { params: Promise<{ productId: string }> };

export default async function Page(props: PageProps) {
  const session = await requireSession();
  if (session.role !== 'owner' && (await props.params).productId === 'new') {
    return <p>Only workspace owners can create products.</p>;
  }
  const params = await props.params;
  const queryClient = getQueryClient();

  if (params.productId !== 'new') {
    const id = Number(params.productId);
    queryClient.setQueryData(
      productKeys.detail(id),
      await backendFetch<ProductByIdResponse>(`/api/products/${id}`)
    );
  }

  return (
    <PageContainer>
      <div className='flex-1 space-y-4'>
        <HydrationBoundary state={dehydrate(queryClient)}>
          <ProductViewPage productId={params.productId} />
        </HydrationBoundary>
      </div>
    </PageContainer>
  );
}
