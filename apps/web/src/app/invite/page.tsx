'use client';

import { useEffect, useState } from 'react';
import { request, type SellerSession } from '@/features/auth/session';

type Invitation = {
  companyName: string;
  emailHint: string;
  expiresAt: string;
  organization: string;
};
const storageKey = 'seller-pending-company-invite';
export default function InvitePage() {
  const [version, setVersion] = useState(0);
  return <InviteContent key={version} retry={() => setVersion((old) => old + 1)} />;
}
function InviteContent({ retry }: { retry: () => void }) {
  const [invitation, setInvitation] = useState<Invitation | null>(null);
  const [session, setSession] = useState<SellerSession | null>(null);
  const [token, setToken] = useState('');
  const [message, setMessage] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const fragment = window.location.hash.slice(1);
        const pending = fragment || sessionStorage.getItem(storageKey) || '';
        if (!/^[A-Za-z0-9_-]{43}$/.test(pending)) {
          throw new Error('请使用公司管理员提供的邀请链接。');
        }
        sessionStorage.setItem(storageKey, pending);
        if (fragment)
          window.history.replaceState(
            window.history.state,
            '',
            window.location.pathname + window.location.search
          );
        const [info, account] = await Promise.all([
          request<Invitation>('/api/invitations/inspect', 'POST', { token: pending }),
          fetch('/api/auth/session', { cache: 'no-store' }).then(async (response) => {
            if (response.status === 401) return null;
            if (!response.ok) throw new Error('无法读取登录状态，请重试。');
            return response.json() as Promise<SellerSession>;
          })
        ]);
        if (!cancelled) {
          setToken(pending);
          setInvitation(info);
          setSession(account);
        }
      } catch (error) {
        if (!cancelled)
          setMessage(error instanceof Error ? error.message : '无法读取邀请，请重试。');
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, []);
  return (
    <main className='mx-auto mt-16 max-w-md space-y-5 rounded-xl border bg-card p-6'>
      <h1 className='text-2xl font-semibold'>加入公司</h1>
      {loading && (
        <p role='status' className='text-muted-foreground'>
          正在检查邀请…
        </p>
      )}
      {invitation && !loading && (
        <>
          <div className='space-y-2'>
            <p className='font-medium'>{invitation.companyName}</p>
            <p className='text-sm text-muted-foreground'>受邀邮箱：{invitation.emailHint}</p>
            <p className='text-sm text-muted-foreground'>请使用受邀邮箱注册或登录。</p>
          </div>
          {session ? (
            <>
              <p className='text-sm'>当前登录：{session.email}</p>
              <button
                type='button'
                disabled={busy}
                className='min-h-11 w-full rounded-md bg-primary px-4 py-2 text-primary-foreground disabled:opacity-50 focus-visible:outline-2'
                onClick={async () => {
                  setBusy(true);
                  setMessage('');
                  try {
                    await request('/api/invitations/accept', 'POST', { token });
                    sessionStorage.removeItem(storageKey);
                    window.location.replace('/dashboard');
                  } catch (error) {
                    setMessage(error instanceof Error ? error.message : '加入失败，请重试。');
                    setBusy(false);
                  }
                }}
              >
                {busy ? '正在加入…' : '接受邀请并进入工作台'}
              </button>
              <button
                type='button'
                className='min-h-11 underline focus-visible:outline-2'
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  try {
                    await request('/api/auth/logout', 'POST');
                    const query = new URLSearchParams({
                      return_to: '/invite',
                      organization: invitation.organization
                    });
                    window.location.assign('/api/auth/login?' + query.toString());
                  } catch (error) {
                    setMessage(String(error));
                    setBusy(false);
                  }
                }}
              >
                换个账号登录
              </button>
            </>
          ) : (
            <form action='/api/auth/login' method='get' className='flex flex-wrap gap-3'>
              <input type='hidden' name='return_to' value='/invite' />
              <input type='hidden' name='organization' value={invitation.organization} />
              <button
                type='button'
                onClick={() => window.location.assign('/register?invited=1#' + token)}
                className='inline-flex min-h-11 items-center rounded-md bg-primary px-4 py-2 text-primary-foreground focus-visible:outline-2'
              >
                注册并加入
              </button>
              <button
                type='submit'
                name='mode'
                value='login'
                className='inline-flex min-h-11 items-center rounded-md border px-4 py-2 focus-visible:outline-2'
              >
                已有账号，去登录
              </button>
            </form>
          )}
        </>
      )}
      {message && (
        <p role='alert' className='text-sm text-destructive'>
          {message}
        </p>
      )}
      {!loading && !invitation && (
        <button
          type='button'
          className='min-h-11 underline focus-visible:outline-2'
          onClick={retry}
        >
          重新检查邀请
        </button>
      )}
    </main>
  );
}
