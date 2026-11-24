import { cookies } from 'next/headers';
import { notFound, redirect } from 'next/navigation';
import type { SellerSession } from '@/features/auth/session';

export function backendURL(path: string) {
  return (process.env.SELLER_API_URL || 'http://127.0.0.1:8080') + path;
}

export async function backendFetch<T>(path: string): Promise<T> {
  const response = await fetch(backendURL(path), {
    headers: {
      cookie: (await cookies()).toString()
    },
    cache: 'no-store'
  });
  if (response.status === 401) redirect('/auth/sign-in');
  if (response.status === 404) notFound();
  if (!response.ok) throw new Error(`Backend request failed (${response.status})`);
  return response.json();
}

export function requireSession() {
  return backendFetch<SellerSession>('/api/auth/session');
}

export function isAllowedAppOrigin(request: Request) {
  const origins = (process.env.SELLER_APP_ORIGIN || new URL(request.url).origin)
    .split(',')
    .map((origin) => origin.trim())
    .filter(Boolean);
  return origins.includes(request.headers.get('origin') || '');
}
