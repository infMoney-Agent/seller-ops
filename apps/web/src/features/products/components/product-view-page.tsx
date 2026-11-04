'use client';

import { useSuspenseQuery } from '@tanstack/react-query';
import type { Product } from '../api/types';
import { useSellerSession } from '@/features/auth/session';
import { notFound } from 'next/navigation';
import ProductForm from './product-form';
import { productByIdOptions } from '../api/queries';

type TProductViewPageProps = {
  productId: string;
};

export default function ProductViewPage({ productId }: TProductViewPageProps) {
  if (productId === 'new') {
    return <ProductForm initialData={null} pageTitle='Create New Product' />;
  }

  return <EditProductView productId={Number(productId)} />;
}

function EditProductView({ productId }: { productId: number }) {
  const { role } = useSellerSession();
  const { data } = useSuspenseQuery(productByIdOptions(productId));

  if (!data?.success || !data?.product) {
    notFound();
  }

  if (role !== 'owner') {
    const product = data.product;
    return (
      <article className='space-y-2'>
        <h1 className='text-xl font-semibold'>{product.name}</h1>
        <p>
          {product.category} · {product.price}
        </p>
        <p>{product.description}</p>
      </article>
    );
  }

  return <ProductForm initialData={data.product as Product} pageTitle='Edit Product' />;
}
