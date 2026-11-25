'use client';
import { useEffect, useState } from 'react';
import Link from 'next/link';
import { request, signOut, type SellerSession } from './session';
type JoinStatus = {
  companyName: string;
  companyCode: string;
  state: 'pending' | 'approved' | 'rejected';
  note: string;
};
export default function CompanyRequiredActions() {
  const [message, setMessage] = useState('');
  const [status, setStatus] = useState<JoinStatus | null>(null);
  const [target, setTarget] = useState('');
  const [busy, setBusy] = useState(false);
  async function refresh() {
    const session = await request<SellerSession>('/api/auth/session');
    if (session.workspaceId) {
      window.location.replace('/dashboard');
      return;
    }
    const data = await request<{ request: JoinStatus | null }>('/api/auth/registration/status');
    setStatus(data.request);
  }
  useEffect(() => {
    let cancelled = false;
    void request<{ request: JoinStatus | null }>('/api/auth/registration/status')
      .then((data) => {
        if (!cancelled) setStatus(data.request);
      })
      .catch((error) => {
        if (!cancelled) setMessage(String(error));
      });
    return () => {
      cancelled = true;
    };
  }, []);
  return (
    <div className='space-y-3'>
      {status && (
        <div className='space-y-2 rounded-md border p-4' role='status'>
          <p>
            申请公司：{status.companyName}（{status.companyCode}）
          </p>
          <p>
            {status.state === 'pending'
              ? '等待公司管理员审批，批准前无法访问业务数据。'
              : status.state === 'rejected'
                ? '加入申请已被拒绝，可联系管理员后重新提交。'
                : '申请已批准，请刷新状态进入工作台。'}
          </p>
          {status.note && <p>{status.note}</p>}
          <button
            type='button'
            className='min-h-11 underline'
            disabled={busy}
            onClick={() => {
              setBusy(true);
              setMessage('');
              void refresh()
                .catch((error) => setMessage(String(error)))
                .finally(() => setBusy(false));
            }}
          >
            刷新审批状态
          </button>
        </div>
      )}
      {status?.state !== 'pending' && (
        <form
          className='space-y-2'
          onSubmit={(event) => {
            event.preventDefault();
            setBusy(true);
            setMessage('');
            void request('/api/auth/join-requests', 'POST', { target })
              .then(refresh)
              .catch((error) => setMessage(String(error)))
              .finally(() => setBusy(false));
          }}
        >
          <label htmlFor='join-company-target' className='block text-sm'>
            申请加入公司
          </label>
          <input
            id='join-company-target'
            className='min-h-11 w-full rounded border p-2'
            value={target}
            onChange={(event) => setTarget(event.target.value)}
            required
            maxLength={128}
            placeholder='公司名称或编号'
          />
          <button type='submit' className='min-h-11 rounded border px-4' disabled={busy}>
            提交加入申请
          </button>
        </form>
      )}
      <div className='flex flex-wrap gap-4'>
        <Link
          className='inline-flex min-h-11 items-center underline focus-visible:outline-2'
          href='/invite'
        >
          继续接受邀请
        </Link>
        <button
          type='button'
          className='min-h-11 underline focus-visible:outline-2'
          onClick={() => {
            void signOut().catch((error) => setMessage(String(error)));
          }}
        >
          退出登录
        </button>
      </div>
      {message && (
        <p role='alert' className='text-sm text-destructive'>
          {message}
        </p>
      )}
    </div>
  );
}
