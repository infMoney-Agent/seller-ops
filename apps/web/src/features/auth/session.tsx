'use client';

import { createContext, useContext } from 'react';

export type Role = 'owner' | 'member';
export type SellerSession = {
  id: number;
  name: string;
  email: string;
  workspaceId: number;
  role: Role | '';
  workspaces: { id: number; name: string; role: Role }[];
};

const SellerSessionContext = createContext<SellerSession | null>(null);

export function SellerSessionProvider({
  session,
  children
}: {
  session: SellerSession;
  children: React.ReactNode;
}) {
  return <SellerSessionContext.Provider value={session}>{children}</SellerSessionContext.Provider>;
}

export function useSellerSession() {
  const session = useContext(SellerSessionContext);
  if (!session) throw new Error('Missing dashboard session');
  return session;
}

export function useSellerUser() {
  const session = useSellerSession();
  return { fullName: session.name, emailAddresses: [{ emailAddress: session.email }] };
}

export async function request<T>(path: string, method = 'GET', body?: object): Promise<T> {
  const headers = new Headers(body ? { 'Content-Type': 'application/json' } : undefined);
  const workspace =
    document.querySelector<HTMLElement>('[data-seller-workspace]')?.dataset.sellerWorkspace;
  if (workspace) headers.set('X-Seller-Workspace', workspace);
  const response = await fetch(path, {
    method,
    cache: 'no-store',
    headers,
    body: body ? JSON.stringify(body) : undefined
  });
  if (!response.ok) {
    const data = await response.json().catch(() => null);
    throw new Error(data?.message ?? `Request failed (${response.status})`);
  }
  return response.status === 204 ? (undefined as T) : response.json();
}

export async function signOut() {
  await request('/api/auth/logout', 'POST');
  window.location.assign('/auth/sign-in');
}
