import { cookies } from 'next/headers';
import { notFound, redirect } from 'next/navigation';
import type { SellerSession } from '@/features/auth/session';

export function backendURL(path: string) {
  return (process.env.SELLER_API_URL || 'http://127.0.0.1:8080') + path;
}

export async function backendFetch<T>(path: string, workspaceId?: number): Promise<T> {
  const response = await fetch(backendURL(path), {
    headers: {
      cookie: (await cookies()).toString(),
      ...(workspaceId ? { 'X-Seller-Workspace': String(workspaceId) } : {})
    },
    cache: 'no-store'
  });
  if (response.status === 401) redirect('/auth/sign-in');
  if (response.status === 404) notFound();
  if (response.status === 409) redirect('/dashboard/workspaces');
  if (!response.ok) throw new Error(`Backend request failed (${response.status})`);
  return response.json();
}

export function requireSession() {
  return backendFetch<SellerSession>('/api/auth/session');
}
