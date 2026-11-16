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

const accountErrors: Record<string, string> = {
  'sign in required': '请先登录。',
  'session expired': '登录已过期，请重新登录。',
  'workspace access denied': '你没有访问此工作区的权限。',
  'Workspace changed; reload this page': '工作区已切换，请刷新页面。',
  'workspace not found': '未找到工作区。',
  'member not found': '未找到成员。',
  'invalid role': '无效的成员角色。',
  'workspace requires an owner': '工作区必须至少有一位所有者。',
  'invalid email': '邮箱地址无效。',
  'invitation not found': '未找到邀请。',
  'invitation unavailable': '此邀请已失效，或与当前登录邮箱不匹配。',
  'invalid request origin': '请求来源无效。',
  'invalid request': '请求参数无效。'
};

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
    throw new Error(accountErrors[data?.message] ?? `请求失败（${response.status}）`);
  }
  return response.status === 204 ? (undefined as T) : response.json();
}

export async function signOut() {
  await request('/api/auth/logout', 'POST');
  window.location.assign('/auth/sign-in');
}
